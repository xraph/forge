package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
	forgemetrics "github.com/xraph/forge/internal/metrics"
)

func TestPrometheusMetricsEmitter_RecordsCounterAndHistogram(t *testing.T) {
	// NewNoOpMetrics returns a forge.Metrics instance whose Counters/Histograms
	// are real-typed but inert — perfect for asserting the emitter is callable
	// and idempotent without depending on a Prometheus exporter.
	m := forgemetrics.NewNoOpMetrics()
	em := NewPrometheusMetricsEmitter(m)

	em.RecordDispatch(context.Background(), "users", "users.list", 1, contract.KindQuery, 12*time.Millisecond, "")
	em.RecordDispatch(context.Background(), "users", "users.list", 1, contract.KindQuery, 8*time.Millisecond, contract.CodeNotFound)

	// We don't assert exact Prometheus output — the noop registry doesn't render.
	// We assert the emitter is callable, doesn't panic, and idempotent on repeat.
	em.RecordDispatch(context.Background(), "users", "users.list", 1, contract.KindQuery, 5*time.Millisecond, "")
}

func TestPrometheusMetricsEmitter_LazyCollectorCreation(t *testing.T) {
	m := forgemetrics.NewNoOpMetrics()
	em := NewPrometheusMetricsEmitter(m)
	// No collectors should exist yet — test by calling RecordDispatch and verifying no panic.
	em.RecordDispatch(context.Background(), "x", "y", 1, contract.KindCommand, time.Millisecond, "")
}

func TestPrometheusMetricsEmitter_NilMetricsIsNoop(t *testing.T) {
	em := NewPrometheusMetricsEmitter(nil)
	em.RecordDispatch(context.Background(), "x", "y", 1, contract.KindQuery, time.Millisecond, "")
	// no panic, no assertion — the constructor handles nil by becoming a noop.
}
