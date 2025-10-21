package shared

import (
	"context"
	"time"
)

// MetricsConfig configures metrics collection
type MetricsConfig struct {
	Enabled     bool
	MetricsPath string
	Namespace   string
}

// Metrics provides telemetry collection
type Metrics interface {
	// Counter metrics (monotonically increasing)
	// Labels should be provided as key-value pairs: "key1", "value1", "key2", "value2"
	Counter(name string, labels ...string) Counter

	// Gauge metrics (can go up or down)
	// Labels should be provided as key-value pairs: "key1", "value1", "key2", "value2"
	Gauge(name string, labels ...string) Gauge

	// Histogram metrics (distributions)
	// Labels should be provided as key-value pairs: "key1", "value1", "key2", "value2"
	Histogram(name string, labels ...string) Histogram

	// Export metrics (Prometheus format by default)
	Export() ([]byte, error)
}

// Counter tracks monotonically increasing values
type Counter interface {
	Inc()
	Add(delta float64)
	WithLabels(labels map[string]string) Counter
}

// Gauge tracks values that can go up or down
type Gauge interface {
	Set(value float64)
	Inc()
	Dec()
	Add(delta float64)
	WithLabels(labels map[string]string) Gauge
}

// Histogram tracks distributions of values
type Histogram interface {
	Observe(value float64)
	ObserveDuration(start time.Time)
	WithLabels(labels map[string]string) Histogram
}

// Context wraps http.Request with convenience methods
type Context interface {
	Context() context.Context
	Set(key string, value any)
	Get(key string) any
}
