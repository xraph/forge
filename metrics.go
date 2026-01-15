package forge

import (
	"github.com/xraph/forge/internal/metrics"
	"github.com/xraph/forge/internal/shared"
)

// Metrics provides telemetry collection.
type Metrics = shared.Metrics

// Counter tracks monotonically increasing values.
type Counter = shared.Counter

// Gauge tracks values that can go up or down.
type Gauge = shared.Gauge

// Histogram tracks distributions of values.
type Histogram = shared.Histogram

// MetricType represents the type of metric.
type MetricType = shared.MetricType

// Metric type constants.
const (
	MetricTypeCounter   = shared.MetricTypeCounter
	MetricTypeGauge     = shared.MetricTypeGauge
	MetricTypeHistogram = shared.MetricTypeHistogram
	MetricTypeTimer     = shared.MetricTypeTimer
)

// MetricsConfig configures metrics collection.
type MetricsConfig = shared.MetricsConfig

// MetricsCollection configures metrics collection.
type MetricsCollection = shared.MetricsCollection

// MetricsLimits configures metrics limits.
type MetricsLimits = shared.MetricsLimits

// MetricsExporterConfig configures metrics exporter.
type MetricsFeatures = shared.MetricsFeatures

// MetricsExporterFormat configures metrics exporter format.
type MetricOption = shared.MetricOption

// DefaultMetricsConfig returns default metrics configuration.
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled: true,
		Features: shared.MetricsFeatures{
			SystemMetrics:  false,
			RuntimeMetrics: false,
			HTTPMetrics:    false,
		},
		Exporters: make(map[string]shared.MetricsExporterConfig[map[string]any]),
		Collection: shared.MetricsCollection{
			Interval:  10 * 1000000000, // 10 seconds in nanoseconds
			Namespace: "forge",
			Path:      "/_/metrics",
			DefaultTags: map[string]string{
				"environment": "development",
				"framework":   "forge",
			},
		},
		Limits: shared.MetricsLimits{
			MaxMetrics: 10000,
			BufferSize: 1000,
		},
	}
}

// NewNoOpMetrics creates a no-op metrics collector.
func NewNoOpMetrics() Metrics {
	return metrics.NewNoOpMetrics()
}

// NewMetrics creates a new metrics instance.
func NewMetrics(config *metrics.CollectorConfig, logger Logger) Metrics {
	return metrics.New(config, logger)
}
