package dispatcher

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// MetricsEmitter ships dispatch metrics to a backend. The Phase 4 expansion
// of this file adds the full DispatchInfo struct and the noop default. For
// Phase 1, only the interface and the noop are needed.
type MetricsEmitter interface {
	RecordDispatch(ctx context.Context, contributor, intent string, version int, kind contract.Kind, latency time.Duration, errCode contract.ErrorCode)
}

// NoopMetricsEmitter discards all dispatch metrics.
type NoopMetricsEmitter struct{}

func (NoopMetricsEmitter) RecordDispatch(_ context.Context, _, _ string, _ int, _ contract.Kind, _ time.Duration, _ contract.ErrorCode) {
}
