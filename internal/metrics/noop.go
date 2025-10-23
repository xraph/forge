package metrics

import (
	"context"
	"time"

	"github.com/xraph/forge/internal/metrics/internal"
	"github.com/xraph/forge/internal/shared"
)

// =============================================================================
// NO-OP METRICS COLLECTOR
// =============================================================================

// noOpMetrics implements Metrics interface with no-op operations for testing
// and scenarios where metrics collection is disabled
type noOpMetrics struct{}

// NewNoOpMetrics creates a no-op metrics collector that implements the full
// Metrics interface but performs no actual metric collection or storage.
// Useful for testing, benchmarking, or when metrics are disabled.
func NewNoOpMetrics() Metrics {
	return &noOpMetrics{}
}

// =============================================================================
// SERVICE LIFECYCLE IMPLEMENTATION (NO-OP)
// =============================================================================

func (m *noOpMetrics) Name() string {
	return "noop-metrics"
}

func (m *noOpMetrics) Dependencies() []string {
	return []string{}
}

func (m *noOpMetrics) Start(ctx context.Context) error {
	return nil
}

func (m *noOpMetrics) Stop(ctx context.Context) error {
	return nil
}

func (m *noOpMetrics) Health(ctx context.Context) error {
	return nil
}

// =============================================================================
// METRIC CREATION METHODS (NO-OP)
// =============================================================================

func (m *noOpMetrics) Counter(name string, labels ...string) internal.Counter {
	return &noOpCounter{}
}

func (m *noOpMetrics) Gauge(name string, labels ...string) internal.Gauge {
	return &noOpGauge{}
}

func (m *noOpMetrics) Histogram(name string, labels ...string) internal.Histogram {
	return &noOpHistogram{}
}

func (m *noOpMetrics) Timer(name string, labels ...string) internal.Timer {
	return &noOpTimer{}
}

// =============================================================================
// EXPORT FUNCTIONALITY (NO-OP)
// =============================================================================

func (m *noOpMetrics) Export(format internal.ExportFormat) ([]byte, error) {
	return []byte{}, nil
}

func (m *noOpMetrics) ExportToFile(format internal.ExportFormat, filename string) error {
	return nil
}

// =============================================================================
// CUSTOM COLLECTOR MANAGEMENT (NO-OP)
// =============================================================================

func (m *noOpMetrics) RegisterCollector(collector internal.CustomCollector) error {
	return nil
}

func (m *noOpMetrics) UnregisterCollector(name string) error {
	return nil
}

func (m *noOpMetrics) GetCollectors() []internal.CustomCollector {
	return []internal.CustomCollector{}
}

// =============================================================================
// METRICS RETRIEVAL (NO-OP)
// =============================================================================

func (m *noOpMetrics) GetMetrics() map[string]interface{} {
	return make(map[string]interface{})
}

func (m *noOpMetrics) GetMetricsByType(metricType internal.MetricType) map[string]interface{} {
	return make(map[string]interface{})
}

func (m *noOpMetrics) GetMetricsByTag(tagKey, tagValue string) map[string]interface{} {
	return make(map[string]interface{})
}

// =============================================================================
// MANAGEMENT METHODS (NO-OP)
// =============================================================================

func (m *noOpMetrics) Reset() error {
	return nil
}

func (m *noOpMetrics) ResetMetric(name string) error {
	return nil
}

// =============================================================================
// STATISTICS (NO-OP)
// =============================================================================

func (m *noOpMetrics) GetStats() internal.CollectorStats {
	return internal.CollectorStats{
		Name:               "noop-metrics",
		Started:            false,
		StartTime:          time.Time{},
		Uptime:             0,
		MetricsCreated:     0,
		MetricsCollected:   0,
		CustomCollectors:   0,
		ActiveMetrics:      0,
		LastCollectionTime: time.Time{},
		Errors:             []string{},
		ExporterStats:      make(map[string]interface{}),
	}
}

// =============================================================================
// RELOAD CONFIGURATION (NO-OP)
// =============================================================================

func (m *noOpMetrics) Reload(config *shared.MetricsConfig) error {
	return nil
}

// =============================================================================
// NO-OP COUNTER IMPLEMENTATION
// =============================================================================

// noOpCounter implements Counter interface with no-op operations
type noOpCounter struct{}

func (c *noOpCounter) Inc()                                               {}
func (c *noOpCounter) Dec()                                               {}
func (c *noOpCounter) Add(delta float64)                                  {}
func (c *noOpCounter) Get() float64                                       { return 0 }
func (c *noOpCounter) WithLabels(labels map[string]string) shared.Counter { return c }
func (c *noOpCounter) Reset() error                                       { return nil }

// =============================================================================
// NO-OP GAUGE IMPLEMENTATION
// =============================================================================

// noOpGauge implements Gauge interface with no-op operations
type noOpGauge struct{}

func (g *noOpGauge) Set(value float64)                                {}
func (g *noOpGauge) Inc()                                             {}
func (g *noOpGauge) Dec()                                             {}
func (g *noOpGauge) Add(delta float64)                                {}
func (g *noOpGauge) Get() float64                                     { return 0 }
func (g *noOpGauge) WithLabels(labels map[string]string) shared.Gauge { return g }
func (g *noOpGauge) Reset() error                                     { return nil }

// =============================================================================
// NO-OP HISTOGRAM IMPLEMENTATION
// =============================================================================

// noOpHistogram implements Histogram interface with no-op operations
type noOpHistogram struct{}

func (h *noOpHistogram) Observe(value float64)                                {}
func (h *noOpHistogram) ObserveDuration(start time.Time)                      {}
func (h *noOpHistogram) GetBuckets() map[float64]uint64                       { return make(map[float64]uint64) }
func (h *noOpHistogram) GetCount() uint64                                     { return 0 }
func (h *noOpHistogram) GetSum() float64                                      { return 0 }
func (h *noOpHistogram) GetMean() float64                                     { return 0 }
func (h *noOpHistogram) GetPercentile(percentile float64) float64             { return 0 }
func (h *noOpHistogram) WithLabels(labels map[string]string) shared.Histogram { return h }
func (h *noOpHistogram) Reset() error                                         { return nil }

// =============================================================================
// NO-OP TIMER IMPLEMENTATION
// =============================================================================

// noOpTimer implements Timer interface with no-op operations
type noOpTimer struct{}

func (t *noOpTimer) Record(duration time.Duration)                  {}
func (t *noOpTimer) Time() func()                                   { return func() {} }
func (t *noOpTimer) GetCount() uint64                               { return 0 }
func (t *noOpTimer) GetMean() time.Duration                         { return 0 }
func (t *noOpTimer) GetPercentile(percentile float64) time.Duration { return 0 }
func (t *noOpTimer) GetMin() time.Duration                          { return 0 }
func (t *noOpTimer) GetMax() time.Duration                          { return 0 }
func (t *noOpTimer) Get() time.Duration                             { return 0 }
func (t *noOpTimer) Reset()                                         {}
