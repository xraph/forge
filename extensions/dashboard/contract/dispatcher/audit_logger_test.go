package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// jsonBufferLogger is a minimal forge.Logger that JSON-encodes every Info call
// (message + fields) to a buffer. We use a custom logger instead of
// forge.NewLogger because the production LoggingConfig does not accept an
// io.Writer destination — it routes to stdout/stderr only. The behavior contract
// from SLICE_B_PLAN Phase 5 is "a logger that writes JSON to a buffer; assert
// the buffer contains expected JSON-encoded fields", which this satisfies.
type jsonBufferLogger struct {
	mu  sync.Mutex
	buf *bytes.Buffer
}

func newJSONBufferLogger(buf *bytes.Buffer) *jsonBufferLogger {
	return &jsonBufferLogger{buf: buf}
}

func (l *jsonBufferLogger) writeFields(level, msg string, fields []forge.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	row := map[string]any{
		"level": level,
		"msg":   msg,
	}
	for _, f := range fields {
		row[f.Key()] = f.Value()
	}
	enc := json.NewEncoder(l.buf)
	_ = enc.Encode(row)
}

func (l *jsonBufferLogger) Debug(msg string, fields ...forge.Field) { l.writeFields("debug", msg, fields) }
func (l *jsonBufferLogger) Info(msg string, fields ...forge.Field)  { l.writeFields("info", msg, fields) }
func (l *jsonBufferLogger) Warn(msg string, fields ...forge.Field)  { l.writeFields("warn", msg, fields) }
func (l *jsonBufferLogger) Error(msg string, fields ...forge.Field) { l.writeFields("error", msg, fields) }
func (l *jsonBufferLogger) Fatal(msg string, fields ...forge.Field) { l.writeFields("fatal", msg, fields) }

func (l *jsonBufferLogger) Debugf(string, ...any) {}
func (l *jsonBufferLogger) Infof(string, ...any)  {}
func (l *jsonBufferLogger) Warnf(string, ...any)  {}
func (l *jsonBufferLogger) Errorf(string, ...any) {}
func (l *jsonBufferLogger) Fatalf(string, ...any) {}

func (l *jsonBufferLogger) With(_ ...forge.Field) forge.Logger     { return l }
func (l *jsonBufferLogger) WithContext(_ context.Context) forge.Logger {
	return l
}
func (l *jsonBufferLogger) Named(_ string) forge.Logger { return l }
func (l *jsonBufferLogger) Sugar() forge.SugarLogger    { return nil }
func (l *jsonBufferLogger) Sync() error                 { return nil }

func TestLoggerAuditEmitter_EmitsStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	logger := newJSONBufferLogger(&buf)
	em := NewLoggerAuditEmitter(logger)

	em.Emit(context.Background(), contract.AuditRecord{
		Time:          time.Now(),
		Contributor:   "users",
		Intent:        "user.disable",
		IntentVersion: 2,
		Subject:       "u_42",
		User:          "admin@example.com",
		Result:        "ok",
		LatencyMs:     12,
		CorrelationID: "req_x",
	})

	out := buf.String()
	for _, want := range []string{
		`"audit":true`,
		`"contributor":"users"`,
		`"intent":"user.disable"`,
		`"version":2`,
		`"subject":"u_42"`,
		`"user":"admin@example.com"`,
		`"result":"ok"`,
		`"latency_ms":12`,
		`"correlation_id":"req_x"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("audit log missing %q in output: %s", want, out)
		}
	}
}

func TestLoggerAuditEmitter_NilLoggerIsNoop(t *testing.T) {
	em := NewLoggerAuditEmitter(nil)
	// Must not panic.
	em.Emit(context.Background(), contract.AuditRecord{})
}
