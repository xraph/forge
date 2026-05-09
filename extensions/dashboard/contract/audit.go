package contract

import (
	"context"
	"fmt"
	"io"
	"time"
)

// AuditRecord is one auditable command invocation.
type AuditRecord struct {
	Time          time.Time
	Contributor   string
	Intent        string
	IntentVersion int
	Subject       string         // resource id when known
	User          string         // user identity (subject from UserInfo)
	Result        string         // ok | error
	LatencyMs     int64
	Payload       map[string]any // pre-redaction; subject to per-intent redaction list
	CorrelationID string
}

// AuditEmitter ships audit records to durable storage. Slice (b) wires the
// chronicle implementation; slice (a) ships log-based and noop variants.
type AuditEmitter interface {
	Emit(ctx context.Context, rec AuditRecord)
}

// NoopAuditEmitter is the disabled-audit implementation.
type NoopAuditEmitter struct{}

func (NoopAuditEmitter) Emit(_ context.Context, _ AuditRecord) {}

// NewLogAuditEmitter returns an emitter that writes a stable line format to w.
// Suitable for development and as a fallback when no chronicle backend is wired.
func NewLogAuditEmitter(w io.Writer) AuditEmitter {
	return &logAuditEmitter{w: w}
}

type logAuditEmitter struct {
	w io.Writer
}

func (e *logAuditEmitter) Emit(_ context.Context, rec AuditRecord) {
	fmt.Fprintf(e.w,
		"audit ts=%s contributor=%s intent=%s v=%d subject=%s user=%s result=%s latencyMs=%d corr=%s\n",
		rec.Time.UTC().Format(time.RFC3339Nano),
		rec.Contributor, rec.Intent, rec.IntentVersion,
		rec.Subject, rec.User, rec.Result, rec.LatencyMs, rec.CorrelationID,
	)
}
