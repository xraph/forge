package forge

import (
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

// MetricsConfig configures metrics collection
type MetricsConfig struct {
	Enabled     bool
	MetricsPath string
	Namespace   string
}

// DefaultMetricsConfig returns default metrics configuration
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:     true,
		MetricsPath: "/_/metrics",
		Namespace:   "forge",
	}
}
