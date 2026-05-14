package dispatcher

import (
	"context"
	"strconv"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

const (
	dispatchTotalMetric    = "forge_dashboard_dispatch_total"
	dispatchDurationMetric = "forge_dashboard_dispatch_duration_seconds"
)

// PrometheusMetricsEmitter records dispatch metrics into a forge.Metrics
// registry. Counters and histograms are created lazily on first emission
// (forge.Metrics.Counter / .Histogram are get-or-create). Pass nil to
// disable — the emitter then becomes a noop.
type PrometheusMetricsEmitter struct {
	metrics forge.Metrics
}

// NewPrometheusMetricsEmitter returns an emitter that writes to m.
// If m is nil, the emitter is a noop.
func NewPrometheusMetricsEmitter(m forge.Metrics) *PrometheusMetricsEmitter {
	return &PrometheusMetricsEmitter{metrics: m}
}

// RecordDispatch implements MetricsEmitter.
func (e *PrometheusMetricsEmitter) RecordDispatch(_ context.Context, contributor, intent string, version int, kind contract.Kind, latency time.Duration, errCode contract.ErrorCode) {
	if e.metrics == nil {
		return
	}

	labels := map[string]string{
		"contributor": contributor,
		"intent":      intent,
		"version":     strconv.Itoa(version),
		"kind":        string(kind),
	}

	if hist := e.metrics.Histogram(dispatchDurationMetric); hist != nil {
		hist.WithLabels(labels).Observe(latency.Seconds())
	}

	counterLabels := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		counterLabels[k] = v
	}
	// error_code is empty on success — Prometheus is fine with empty label values.
	counterLabels["error_code"] = string(errCode)

	if cnt := e.metrics.Counter(dispatchTotalMetric); cnt != nil {
		cnt.WithLabels(counterLabels).Inc()
	}
}

// Compile-time assertion.
var _ MetricsEmitter = (*PrometheusMetricsEmitter)(nil)
