package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/xraph/forge/internal/logger"
)

// memExporter keeps a clone of every record it is handed.
type memExporter struct {
	mu   sync.Mutex
	recs []sdklog.Record
}

func (e *memExporter) Export(_ context.Context, recs []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := range recs {
		e.recs = append(e.recs, recs[i].Clone())
	}

	return nil
}

func (*memExporter) Shutdown(context.Context) error   { return nil }
func (*memExporter) ForceFlush(context.Context) error { return nil }

func (e *memExporter) records() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]sdklog.Record, len(e.recs))
	copy(out, e.recs)

	return out
}

// newTestSink returns a bridge sink that exports synchronously into memory.
func newTestSink(t *testing.T, level string) (*logSink, *memExporter) {
	t.Helper()

	exp := &memExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exp)))

	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	return newLogSink(lp, level), exp
}

func attrsOf(rec *sdklog.Record) map[string]attribute.Value {
	out := make(map[string]attribute.Value)

	rec.WalkAttributes(func(kv attribute.KeyValue) bool {
		out[string(kv.Key)] = kv.Value

		return true
	})

	return out
}

func onlyRecord(t *testing.T, exp *memExporter) sdklog.Record {
	t.Helper()

	recs := exp.records()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}

	return recs[0]
}

func TestBridgeSeverityAndBody(t *testing.T) {
	cases := []struct {
		level logger.LogLevel
		num   otellog.Severity
		text  string
	}{
		{logger.LevelDebug, 5, "DEBUG"},
		{logger.LevelInfo, 9, "INFO"},
		{logger.LevelWarn, 13, "WARN"},
		{logger.LevelError, 17, "ERROR"},
		{logger.LevelFatal, 21, "FATAL"},
	}

	for _, tc := range cases {
		t.Run(string(tc.level), func(t *testing.T) {
			sink, exp := newTestSink(t, "")
			ts := time.Date(2026, 10, 21, 12, 30, 0, 123456789, time.UTC)

			sink.Emit(logger.Record{Time: ts, Level: tc.level, Message: "msg " + string(tc.level)})

			rec := onlyRecord(t, exp)
			if rec.Severity() != tc.num {
				t.Errorf("severity = %d, want %d", rec.Severity(), tc.num)
			}

			if rec.SeverityText() != tc.text {
				t.Errorf("severity text = %q, want %q", rec.SeverityText(), tc.text)
			}

			if got := rec.Body(); got.Type() != attribute.STRING || got.AsString() != "msg "+string(tc.level) {
				t.Errorf("body = %v, want the message", got)
			}

			if !rec.Timestamp().Equal(ts) {
				t.Errorf("timestamp = %v, want %v", rec.Timestamp(), ts)
			}

			if rec.ObservedTimestamp().IsZero() {
				t.Error("observed timestamp is zero")
			}
		})
	}
}

type testStringer struct{}

func (testStringer) String() string { return "stringer-text" }

type testStruct struct {
	A int
	B string
}

func TestBridgeFieldTypes(t *testing.T) {
	sink, exp := newTestSink(t, "")
	when := time.Date(2026, 10, 21, 8, 0, 0, 5, time.FixedZone("x", 3600))
	st := testStruct{A: 1, B: "two"}

	sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelInfo, Message: "types", Fields: []logger.Field{
		logger.String("s", "text"),
		logger.Int64("i", -42),
		logger.Uint64("u", 1<<63+1),
		logger.Uint64("small", 7),
		logger.Float64("f", 2.5),
		logger.Bool("b", true),
		logger.Duration("d", 1500*time.Millisecond),
		logger.Time("t", when),
		logger.Error(errors.New("boom")),
		logger.Strings("ss", []string{"x", "y"}),
		logger.Stringer("str", testStringer{}),
		logger.Any("any", st),
	}})

	got := attrsOf(new(onlyRecord(t, exp)))

	want := map[string]attribute.Value{
		"s":     attribute.StringValue("text"),
		"i":     attribute.Int64Value(-42),
		"u":     attribute.StringValue("9223372036854775809"),
		"small": attribute.Int64Value(7),
		"f":     attribute.Float64Value(2.5),
		"b":     attribute.BoolValue(true),
		"d":     attribute.StringValue("1.5s"),
		"t":     attribute.StringValue(when.UTC().Format(time.RFC3339Nano)),
		"error": attribute.StringValue("boom"),
		"ss":    attribute.StringSliceValue([]string{"x", "y"}),
		"str":   attribute.StringValue("stringer-text"),
		"any":   attribute.StringValue(fmt.Sprint(st)),
	}

	if len(got) != len(want) {
		t.Errorf("got %d attributes, want %d: %v", len(got), len(want), got)
	}

	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("attribute %q missing", k)

			continue
		}

		if g.Type() != w.Type() || g.String() != w.String() {
			t.Errorf("%s = %s(%s), want %s(%s)", k, g.Type(), g.String(), w.Type(), w.String())
		}
	}
}

func TestBridgeNilErrorIsEmpty(t *testing.T) {
	sink, exp := newTestSink(t, "")

	sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelInfo, Message: "nil", Fields: []logger.Field{
		logger.Error(nil),
	}})

	v, ok := attrsOf(new(onlyRecord(t, exp)))["error"]
	if !ok {
		t.Fatal("error attribute missing")
	}

	if v.Type() != attribute.EMPTY {
		t.Fatalf("error = %s(%s), want an empty value", v.Type(), v.String())
	}
}

const (
	testTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	testSpanID  = "00f067aa0ba902b7"
)

func TestBridgeTraceIDsBecomeSpanContext(t *testing.T) {
	sink, exp := newTestSink(t, "")

	sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelInfo, Message: "traced", Fields: []logger.Field{
		logger.String("trace_id", testTraceID),
		logger.String("span_id", testSpanID),
		logger.String("other", "kept"),
	}})

	rec := onlyRecord(t, exp)
	if rec.TraceID().String() != testTraceID {
		t.Errorf("trace id = %s, want %s", rec.TraceID(), testTraceID)
	}

	if rec.SpanID().String() != testSpanID {
		t.Errorf("span id = %s, want %s", rec.SpanID(), testSpanID)
	}

	attrs := attrsOf(&rec)
	for _, k := range []string{"trace_id", "span_id"} {
		if _, ok := attrs[k]; ok {
			t.Errorf("%s is still an attribute", k)
		}
	}

	if attrs["other"].AsString() != "kept" {
		t.Errorf("other = %v, want kept", attrs["other"])
	}
}

func TestBridgeBadTraceIDsStayAttributes(t *testing.T) {
	cases := map[string][]logger.Field{
		"bad trace id": {logger.String("trace_id", "req-123"), logger.String("span_id", testSpanID)},
		"no span id":   {logger.String("trace_id", testTraceID)},
		"zero ids": {
			logger.String("trace_id", "00000000000000000000000000000000"),
			logger.String("span_id", "0000000000000000"),
		},
	}

	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			sink, exp := newTestSink(t, "")

			sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelInfo, Message: "m", Fields: fields})

			rec := onlyRecord(t, exp)
			if rec.TraceID().IsValid() || rec.SpanID().IsValid() {
				t.Errorf("record ids = %s/%s, want zero", rec.TraceID(), rec.SpanID())
			}

			attrs := attrsOf(&rec)
			for _, f := range fields {
				v, ok := attrs[f.Key()]
				if !ok {
					t.Errorf("%s dropped from attributes", f.Key())

					continue
				}

				if v.AsString() != f.Value() {
					t.Errorf("%s = %q, want %q", f.Key(), v.AsString(), f.Value())
				}
			}
		})
	}
}

func TestBridgeScopeAndCaller(t *testing.T) {
	sink, exp := newTestSink(t, "")

	sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelInfo, Message: "a",
		Logger: "app.router", Caller: "router/x.go:42"})
	sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelInfo, Message: "b", Caller: "weird"})

	recs := exp.records()
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}

	if got := recs[0].InstrumentationScope().Name; got != "app.router" {
		t.Errorf("scope = %q, want app.router", got)
	}

	a := attrsOf(&recs[0])
	if a["code.file.path"].AsString() != "router/x.go" {
		t.Errorf("code.file.path = %v, want router/x.go", a["code.file.path"])
	}

	if v := a["code.line.number"]; v.Type() != attribute.INT64 || v.AsInt64() != 42 {
		t.Errorf("code.line.number = %s(%s), want 42", v.Type(), v.String())
	}

	if got := recs[1].InstrumentationScope().Name; got != "forge" {
		t.Errorf("scope = %q, want forge", got)
	}

	b := attrsOf(&recs[1])
	if b["code.file.path"].AsString() != "weird" {
		t.Errorf("code.file.path = %v, want weird", b["code.file.path"])
	}

	if _, ok := b["code.line.number"]; ok {
		t.Error("code.line.number set for a caller with no line")
	}
}

func TestBridgeLevelFilter(t *testing.T) {
	sink, exp := newTestSink(t, "warn")

	for _, lv := range []logger.LogLevel{logger.LevelInfo, logger.LevelWarn, logger.LevelError} {
		sink.Emit(logger.Record{Time: time.Now(), Level: lv, Message: string(lv)})
	}

	recs := exp.records()
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}

	if recs[0].Body().AsString() != "warn" || recs[1].Body().AsString() != "error" {
		t.Fatalf("kept %q and %q, want warn and error", recs[0].Body().AsString(), recs[1].Body().AsString())
	}
}

func TestBridgeDuplicateKeysKeepLast(t *testing.T) {
	sink, exp := newTestSink(t, "")

	sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelInfo, Message: "dup", Fields: []logger.Field{
		logger.Int("k", 1),
		logger.Int("k", 2),
	}})

	rec := onlyRecord(t, exp)

	var vals []int64

	rec.WalkAttributes(func(kv attribute.KeyValue) bool {
		if kv.Key == "k" {
			vals = append(vals, kv.Value.AsInt64())
		}

		return true
	})

	if len(vals) != 1 || vals[0] != 2 {
		t.Fatalf("k = %v, want exactly [2]", vals)
	}
}

func TestBridgeScopeCacheIsBounded(t *testing.T) {
	sink, exp := newTestSink(t, "")

	const names = 300

	for i := range names {
		sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelInfo, Message: "m",
			Logger: "user." + strconv.Itoa(i)})
	}

	sink.mu.RLock()
	cached := len(sink.scopes)
	sink.mu.RUnlock()

	if cached > maxScopes {
		t.Fatalf("scope cache holds %d loggers, want at most %d", cached, maxScopes)
	}

	recs := exp.records()
	if len(recs) != names {
		t.Fatalf("got %d records, want %d", len(recs), names)
	}

	for i, rec := range recs {
		name := "user." + strconv.Itoa(i)
		scope := rec.InstrumentationScope().Name
		attr, hasAttr := attrsOf(&rec)["logger.name"]

		if i < maxScopes {
			if scope != name || hasAttr {
				t.Fatalf("record %d: scope %q, logger.name %v; want scope %q and no attribute", i+1, scope, attr, name)
			}

			continue
		}

		if scope != defaultScope || attr.AsString() != name {
			t.Fatalf("record %d: scope %q, logger.name %q; want %q and %q", i+1, scope, attr.AsString(), defaultScope, name)
		}
	}
}

type panicError struct{}

func (panicError) Error() string { panic("Error exploded") }

type panicStringer struct{}

func (panicStringer) String() string { panic("String exploded") }

func TestBridgePanickingValues(t *testing.T) {
	sink, exp := newTestSink(t, "")

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Emit panicked: %v", r)
			}
		}()

		sink.Emit(logger.Record{Time: time.Now(), Level: logger.LevelError, Message: "panics", Fields: []logger.Field{
			logger.Error(panicError{}),
			logger.Stringer("str", panicStringer{}),
			logger.String("ok", "fine"),
		}})
	}()

	attrs := attrsOf(new(onlyRecord(t, exp)))
	for _, k := range []string{"error", "str"} {
		if attrs[k].AsString() != "<panic>" {
			t.Errorf("%s = %q, want <panic>", k, attrs[k].AsString())
		}
	}

	if attrs["ok"].AsString() != "fine" {
		t.Errorf("ok = %q, want fine", attrs["ok"].AsString())
	}
}
