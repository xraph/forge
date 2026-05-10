package dispatcher

import (
	"context"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// LoggerAuditEmitter writes audit records as info-level structured logs
// via a forge.Logger. Each AuditRecord field is emitted as a discrete log
// field so log aggregators can filter by `audit=true` cheaply. Pass a nil
// logger to disable — the emitter then becomes a noop.
type LoggerAuditEmitter struct {
	logger forge.Logger
}

// NewLoggerAuditEmitter returns an emitter that writes via logger. Pass nil
// to disable (the emitter becomes a noop).
func NewLoggerAuditEmitter(logger forge.Logger) *LoggerAuditEmitter {
	return &LoggerAuditEmitter{logger: logger}
}

// Emit implements contract.AuditEmitter.
func (e *LoggerAuditEmitter) Emit(_ context.Context, rec contract.AuditRecord) {
	if e.logger == nil {
		return
	}
	e.logger.Info("dashboard contract audit",
		forge.Bool("audit", true),
		forge.String("contributor", rec.Contributor),
		forge.String("intent", rec.Intent),
		forge.Int("version", rec.IntentVersion),
		forge.String("subject", rec.Subject),
		forge.String("user", rec.User),
		forge.String("result", rec.Result),
		forge.Int64("latency_ms", rec.LatencyMs),
		forge.String("correlation_id", rec.CorrelationID),
		forge.Time("time", rec.Time),
	)
}

// Compile-time assertion.
var _ contract.AuditEmitter = (*LoggerAuditEmitter)(nil)
