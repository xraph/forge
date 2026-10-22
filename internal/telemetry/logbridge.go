package telemetry

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/logger"
)

const (
	// flushCap bounds Flush, which Fatal calls just before the process exits.
	// It is shorter than shutdownCap so a dead collector can't hold a dying
	// process for long.
	flushCap = 2 * time.Second

	// maxScopes caps the per-name scope cache. Named with an unbounded name
	// (a user ID per request) would otherwise grow it forever. Past the cap a
	// line goes out under defaultScope and carries its name as logger.name.
	maxScopes = 256

	// defaultScope is the instrumentation scope of lines from an unnamed
	// logger, and of named lines once the cache is full.
	defaultScope = "forge"

	// panicValue replaces a field value whose Error or String panicked.
	panicValue = "<panic>"

	// maxPooledAttrs keeps an unusually wide line from pinning a big slice in
	// the pool.
	maxPooledAttrs = 256
)

// logSink turns go-utils log lines into OTel log records. It implements
// logger.Sink and logger.Flusher, and Emit is safe for concurrent use.
type logSink struct {
	lp       *sdklog.LoggerProvider
	minLevel int
	root     otellog.Logger

	mu     sync.RWMutex
	scopes map[string]otellog.Logger
}

// Fatal flushes a sink only through logger.Flusher, so losing Flush must not
// compile.
var (
	_ logger.Sink    = (*logSink)(nil)
	_ logger.Flusher = (*logSink)(nil)
)

var attrPool = sync.Pool{
	New: func() any {
		s := make([]attribute.KeyValue, 0, 16)

		return &s
	},
}

// newLogSink returns a sink that emits into lp. level is a LogsSettings.Level:
// lines below it are dropped before any conversion, and "" keeps every line.
func newLogSink(lp *sdklog.LoggerProvider, level string) *logSink {
	return &logSink{
		lp:       lp,
		minLevel: levelRank(logger.LogLevel(level)),
		root:     lp.Logger(defaultScope),
		scopes:   make(map[string]otellog.Logger),
	}
}

// Emit implements logger.Sink.
func (s *logSink) Emit(rec logger.Record) {
	if levelRank(rec.Level) < s.minLevel {
		return
	}

	lg, cached := s.scopeLogger(rec.Logger)

	ap := attrPool.Get().(*[]attribute.KeyValue)
	attrs := (*ap)[:0]

	if !cached {
		attrs = setAttr(attrs, attribute.String("logger.name", rec.Logger))
	}

	attrs = appendCaller(attrs, rec.Caller)

	for _, f := range rec.Fields {
		attrs = setAttr(attrs, attribute.KeyValue{Key: attribute.Key(f.Key()), Value: fieldValue(f)})
	}

	ctx := context.Background()

	var sc trace.SpanContextConfig

	if attrs, sc.TraceID, sc.SpanID = takeTraceIDs(attrs); sc.TraceID.IsValid() {
		ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(sc))
	}

	var r otellog.Record

	r.SetTimestamp(rec.Time)
	r.SetObservedTimestamp(time.Now())
	r.SetSeverity(severityOf(rec.Level))
	r.SetSeverityText(severityText(rec.Level))
	r.SetBody(attribute.StringValue(rec.Message))
	r.AddAttributes(attrs...)

	lg.Emit(ctx, r)

	// The record copied the attributes, so the slice can go back. Clearing it
	// drops the references to field values.
	if cap(attrs) <= maxPooledAttrs {
		clear(attrs)
		*ap = attrs[:0]
		attrPool.Put(ap)
	}
}

// Flush implements logger.Flusher. It exports queued records, waiting at most
// flushCap.
func (s *logSink) Flush() {
	ctx, cancel := context.WithTimeout(context.Background(), flushCap)
	defer cancel()

	_ = s.lp.ForceFlush(ctx)
}

// scopeLogger returns the OTel logger for a Named path. cached is false when
// the cache is full and name had to fall back to defaultScope.
func (s *logSink) scopeLogger(name string) (lg otellog.Logger, cached bool) {
	if name == "" {
		return s.root, true
	}

	s.mu.RLock()
	lg, ok := s.scopes[name]
	full := len(s.scopes) >= maxScopes
	s.mu.RUnlock()

	if ok {
		return lg, true
	}

	// The cache never shrinks, so once it's full an uncached name falls back
	// without the write lock. A per-request name would otherwise take it on
	// every line.
	if full {
		return s.root, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if lg, ok := s.scopes[name]; ok {
		return lg, true
	}

	if len(s.scopes) >= maxScopes {
		return s.root, false
	}

	lg = s.lp.Logger(name)
	s.scopes[name] = lg

	return lg, true
}

// setAttr appends kv, or overwrites the value of an earlier attribute with the
// same key, so the last value wins as it does in the encoder's output.
func setAttr(attrs []attribute.KeyValue, kv attribute.KeyValue) []attribute.KeyValue {
	for i := range attrs {
		if attrs[i].Key == kv.Key {
			attrs[i].Value = kv.Value

			return attrs
		}
	}

	return append(attrs, kv)
}

// appendCaller splits "dir/file.go:123" into code.file.path and
// code.line.number. A caller that doesn't split cleanly goes in
// code.file.path whole.
func appendCaller(attrs []attribute.KeyValue, caller string) []attribute.KeyValue {
	if caller == "" {
		return attrs
	}

	if i := strings.LastIndexByte(caller, ':'); i > 0 {
		if line, err := strconv.ParseInt(caller[i+1:], 10, 64); err == nil {
			attrs = setAttr(attrs, attribute.String("code.file.path", caller[:i]))

			return setAttr(attrs, attribute.Int64("code.line.number", line))
		}
	}

	return setAttr(attrs, attribute.String("code.file.path", caller))
}

// takeTraceIDs removes trace_id and span_id from attrs when both are valid,
// non-zero hex IDs, and returns them. Otherwise attrs is unchanged and the IDs
// are zero.
func takeTraceIDs(attrs []attribute.KeyValue) ([]attribute.KeyValue, trace.TraceID, trace.SpanID) {
	ti, si := -1, -1

	for i := range attrs {
		switch attrs[i].Key {
		case "trace_id":
			ti = i
		case "span_id":
			si = i
		}
	}

	if ti < 0 || si < 0 || attrs[ti].Value.Type() != attribute.STRING || attrs[si].Value.Type() != attribute.STRING {
		return attrs, trace.TraceID{}, trace.SpanID{}
	}

	tid, err := trace.TraceIDFromHex(attrs[ti].Value.AsString())
	if err != nil {
		return attrs, trace.TraceID{}, trace.SpanID{}
	}

	sid, err := trace.SpanIDFromHex(attrs[si].Value.AsString())
	if err != nil {
		return attrs, trace.TraceID{}, trace.SpanID{}
	}

	out := attrs[:0]

	for i := range attrs {
		if i != ti && i != si {
			out = append(out, attrs[i])
		}
	}

	clear(attrs[len(out):])

	return out, tid, sid
}

// fieldValue converts a go-utils field value into an attribute value. A value
// whose Error or String panics becomes panicValue.
func fieldValue(f logger.Field) (v attribute.Value) {
	defer func() {
		if recover() != nil {
			v = attribute.StringValue(panicValue)
		}
	}()

	switch x := f.Value().(type) {
	case nil:
		return attribute.Value{}
	case string:
		return attribute.StringValue(x)
	case int64:
		return attribute.Int64Value(x)
	case uint64:
		if x <= math.MaxInt64 {
			return attribute.Int64Value(int64(x))
		}

		return attribute.StringValue(strconv.FormatUint(x, 10))
	case float64:
		return attribute.Float64Value(x)
	case bool:
		return attribute.BoolValue(x)
	case time.Duration:
		return attribute.StringValue(x.String())
	case time.Time:
		return attribute.StringValue(x.UTC().Format(time.RFC3339Nano))
	case error:
		return attribute.StringValue(x.Error())
	case fmt.Stringer:
		return attribute.StringValue(x.String())
	case []string:
		return attribute.StringSliceValue(x)
	default:
		return attribute.StringValue(fmt.Sprint(x))
	}
}

// levelRank orders levels for the export filter. "" ranks with debug, so an
// unset filter keeps every line, and an unknown level ranks with info.
func levelRank(l logger.LogLevel) int {
	switch l {
	case logger.LevelDebug, "":
		return 0
	case logger.LevelWarn:
		return 2
	case logger.LevelError:
		return 3
	case logger.LevelFatal:
		return 4
	default:
		return 1
	}
}

func severityOf(l logger.LogLevel) otellog.Severity {
	switch l {
	case logger.LevelDebug:
		return otellog.SeverityDebug
	case logger.LevelWarn:
		return otellog.SeverityWarn
	case logger.LevelError:
		return otellog.SeverityError
	case logger.LevelFatal:
		return otellog.SeverityFatal
	default:
		return otellog.SeverityInfo
	}
}

func severityText(l logger.LogLevel) string {
	switch l {
	case logger.LevelDebug:
		return "DEBUG"
	case logger.LevelWarn:
		return "WARN"
	case logger.LevelError:
		return "ERROR"
	case logger.LevelFatal:
		return "FATAL"
	default:
		return "INFO"
	}
}
