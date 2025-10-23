package forge

import (
	"github.com/xraph/forge/v2/internal/metrics"
	"github.com/xraph/forge/v2/internal/shared"
)

// Metrics provides telemetry collection
type Metrics = shared.Metrics

// Counter tracks monotonically increasing values
type Counter = shared.Counter

// Gauge tracks values that can go up or down
type Gauge = shared.Gauge

// Histogram tracks distributions of values
type Histogram = shared.Histogram

// MetricType represents the type of metric
type MetricType = shared.MetricType

// Metric type constants
const (
	MetricTypeCounter   = shared.MetricTypeCounter
	MetricTypeGauge     = shared.MetricTypeGauge
	MetricTypeHistogram = shared.MetricTypeHistogram
	MetricTypeTimer     = shared.MetricTypeTimer
)

// MetricsConfig configures metrics collection
type MetricsConfig = shared.MetricsConfig

// DefaultMetricsConfig returns default metrics configuration
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:              true,
		MetricsPath:          "/_/metrics",
		Namespace:            "forge",
		CollectionInterval:   10 * 1000000000, // 10 seconds in nanoseconds
		EnableSystemMetrics:  false,           // Disable by default for performance
		EnableRuntimeMetrics: false,           // Disable by default for performance
		EnableHTTPMetrics:    false,           // Disable by default for performance
		MaxMetrics:           10000,
		BufferSize:           1000,
		DefaultTags:          make(map[string]string),
	}
}

// NewNoOpMetrics creates a no-op metrics collector
func NewNoOpMetrics() Metrics {
	return metrics.NewNoOpMetrics()
}

// NewMetrics creates a new metrics instance
func NewMetrics(config *metrics.CollectorConfig, logger Logger) Metrics {
	return metrics.New(config, logger)
}
