package metrics

import (
	"context"
	"time"

	"github.com/xraph/go-utils/metrics"
)

// NewNoOpMetrics creates a no-op metrics collector that implements the full
// Metrics interface but performs no actual metric collection or storage.
// Useful for testing, benchmarking, or when metrics are disabled.
func NewNoOpMetrics() Metrics {
	return &noOpMetrics{}
}

// noOpMetrics is a true no-op implementation that doesn't track any metrics.
type noOpMetrics struct{}

func (n *noOpMetrics) Name() string {
	return "noop-metrics"
}

func (n *noOpMetrics) Start(ctx context.Context) error {
	return nil
}

func (n *noOpMetrics) Stop(ctx context.Context) error {
	return nil
}

func (n *noOpMetrics) Health(ctx context.Context) error {
	return nil
}

func (n *noOpMetrics) Counter(name string, opts ...metrics.MetricOption) metrics.Counter {
	return &noOpCounter{}
}

func (n *noOpMetrics) Gauge(name string, opts ...metrics.MetricOption) metrics.Gauge {
	return &noOpGauge{}
}

func (n *noOpMetrics) Histogram(name string, opts ...metrics.MetricOption) metrics.Histogram {
	return &noOpHistogram{}
}

func (n *noOpMetrics) Timer(name string, opts ...metrics.MetricOption) metrics.Timer {
	return &noOpTimer{}
}

func (n *noOpMetrics) Summary(name string, opts ...metrics.MetricOption) metrics.Summary {
	return &noOpSummary{}
}

func (n *noOpMetrics) Export(format metrics.ExportFormat) ([]byte, error) {
	return []byte{}, nil
}

func (n *noOpMetrics) ExportToFile(format metrics.ExportFormat, filename string) error {
	return nil
}

func (n *noOpMetrics) RegisterCollector(collector metrics.CustomCollector) error {
	return nil
}

func (n *noOpMetrics) UnregisterCollector(name string) error {
	return nil
}

func (n *noOpMetrics) ListCollectors() []metrics.CustomCollector {
	return []metrics.CustomCollector{}
}

func (n *noOpMetrics) ListMetrics() map[string]any {
	return map[string]any{}
}

func (n *noOpMetrics) ListMetricsByType(metricType metrics.MetricType) map[string]any {
	return map[string]any{}
}

func (n *noOpMetrics) ListMetricsByTag(tagKey, tagValue string) map[string]any {
	return map[string]any{}
}

func (n *noOpMetrics) Stats() metrics.CollectorStats {
	return metrics.CollectorStats{
		Name:    "noop-metrics",
		Started: false,
	}
}

func (n *noOpMetrics) Reset() error {
	return nil
}

func (n *noOpMetrics) ResetMetric(name string) error {
	return nil
}

func (n *noOpMetrics) Reload(config *metrics.MetricsConfig) error {
	return nil
}

// noOpCounter is a no-op counter implementation.
type noOpCounter struct{}

func (n *noOpCounter) Inc()                                                     {}
func (n *noOpCounter) Add(delta float64)                                        {}
func (n *noOpCounter) AddWithExemplar(delta float64, exemplar metrics.Exemplar) {}
func (n *noOpCounter) Value() float64                                           { return 0 }
func (n *noOpCounter) Timestamp() time.Time                                     { return time.Time{} }
func (n *noOpCounter) Exemplars() []metrics.Exemplar                            { return nil }
func (n *noOpCounter) Describe() metrics.MetricMetadata                         { return metrics.MetricMetadata{} }
func (n *noOpCounter) WithLabels(labels map[string]string) metrics.Counter      { return n }
func (n *noOpCounter) Reset() error                                             { return nil }

// noOpGauge is a no-op gauge implementation.
type noOpGauge struct{}

func (n *noOpGauge) Set(value float64)                                 {}
func (n *noOpGauge) Inc()                                              {}
func (n *noOpGauge) Dec()                                              {}
func (n *noOpGauge) Add(delta float64)                                 {}
func (n *noOpGauge) Sub(delta float64)                                 {}
func (n *noOpGauge) SetToCurrentTime()                                 {}
func (n *noOpGauge) Value() float64                                    { return 0 }
func (n *noOpGauge) Timestamp() time.Time                              { return time.Time{} }
func (n *noOpGauge) Describe() metrics.MetricMetadata                  { return metrics.MetricMetadata{} }
func (n *noOpGauge) WithLabels(labels map[string]string) metrics.Gauge { return n }
func (n *noOpGauge) Reset() error                                      { return nil }

// noOpHistogram is a no-op histogram implementation.
type noOpHistogram struct{}

func (n *noOpHistogram) Observe(value float64)                                        {}
func (n *noOpHistogram) ObserveWithExemplar(value float64, exemplar metrics.Exemplar) {}
func (n *noOpHistogram) Count() uint64                                                { return 0 }
func (n *noOpHistogram) Sum() float64                                                 { return 0 }
func (n *noOpHistogram) Mean() float64                                                { return 0 }
func (n *noOpHistogram) StdDev() float64                                              { return 0 }
func (n *noOpHistogram) Min() float64                                                 { return 0 }
func (n *noOpHistogram) Max() float64                                                 { return 0 }
func (n *noOpHistogram) Percentile(p float64) float64                                 { return 0 }
func (n *noOpHistogram) Quantile(q float64) float64                                   { return 0 }
func (n *noOpHistogram) Buckets() map[float64]uint64                                  { return map[float64]uint64{} }
func (n *noOpHistogram) Exemplars() []metrics.Exemplar                                { return nil }
func (n *noOpHistogram) Describe() metrics.MetricMetadata                             { return metrics.MetricMetadata{} }
func (n *noOpHistogram) WithLabels(labels map[string]string) metrics.Histogram        { return n }
func (n *noOpHistogram) Reset() error                                                 { return nil }

// noOpTimer is a no-op timer implementation.
type noOpTimer struct{}

func (n *noOpTimer) Record(duration time.Duration)                                        {}
func (n *noOpTimer) RecordWithExemplar(duration time.Duration, exemplar metrics.Exemplar) {}
func (n *noOpTimer) Time() func()                                                         { return func() {} }
func (n *noOpTimer) Count() uint64                                                        { return 0 }
func (n *noOpTimer) Value() time.Duration                                                 { return 0 }
func (n *noOpTimer) Sum() time.Duration                                                   { return 0 }
func (n *noOpTimer) Mean() time.Duration                                                  { return 0 }
func (n *noOpTimer) StdDev() time.Duration                                                { return 0 }
func (n *noOpTimer) Min() time.Duration                                                   { return 0 }
func (n *noOpTimer) Max() time.Duration                                                   { return 0 }
func (n *noOpTimer) Percentile(p float64) time.Duration                                   { return 0 }
func (n *noOpTimer) Quantile(q float64) time.Duration                                     { return 0 }
func (n *noOpTimer) Exemplars() []metrics.Exemplar                                        { return nil }
func (n *noOpTimer) Describe() metrics.MetricMetadata                                     { return metrics.MetricMetadata{} }
func (n *noOpTimer) WithLabels(labels map[string]string) metrics.Timer                    { return n }
func (n *noOpTimer) Reset() error                                                         { return nil }

// noOpSummary is a no-op summary implementation.
type noOpSummary struct{}

func (n *noOpSummary) Observe(value float64)                               {}
func (n *noOpSummary) Count() uint64                                       { return 0 }
func (n *noOpSummary) Sum() float64                                        { return 0 }
func (n *noOpSummary) Mean() float64                                       { return 0 }
func (n *noOpSummary) Quantile(q float64) float64                          { return 0 }
func (n *noOpSummary) Min() float64                                        { return 0 }
func (n *noOpSummary) Max() float64                                        { return 0 }
func (n *noOpSummary) StdDev() float64                                     { return 0 }
func (n *noOpSummary) Describe() metrics.MetricMetadata                    { return metrics.MetricMetadata{} }
func (n *noOpSummary) WithLabels(labels map[string]string) metrics.Summary { return n }
func (n *noOpSummary) Reset() error                                        { return nil }
