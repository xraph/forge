package common

import (
	"time"
)

// ExportFormat represents the format for metrics export
type ExportFormat string

const (
	ExportFormatPrometheus ExportFormat = "prometheus"
	ExportFormatJSON       ExportFormat = "json"
	ExportFormatInflux     ExportFormat = "influx"
	ExportFormatStatsD     ExportFormat = "statsd"
)

// MetricType represents the type of metric
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeTimer     MetricType = "timer"
)

// Metrics defines the interface for metrics collection
type Metrics interface {
	Service

	Counter(name string, tags ...string) Counter
	Gauge(name string, tags ...string) Gauge
	Histogram(name string, tags ...string) Histogram
	Timer(name string, tags ...string) Timer

	// Custom collector management
	RegisterCollector(collector CustomCollector) error
	UnregisterCollector(name string) error
	GetCollectors() []CustomCollector

	// Metrics retrieval
	GetMetrics() map[string]interface{}
	GetMetricsByType(metricType MetricType) map[string]interface{}
	GetMetricsByTag(tagKey, tagValue string) map[string]interface{}

	// Export functionality
	Export(format ExportFormat) ([]byte, error)
	ExportToFile(format ExportFormat, filename string) error

	// Management
	Reset() error
	ResetMetric(name string) error

	// Statistics
	GetStats() CollectorStats
}

// =============================================================================
// METRIC INTERFACES
// =============================================================================

// Counter represents a counter metric
type Counter interface {
	Inc()
	Dec()
	Add(value float64)
	Get() float64
	Reset()
}

// Gauge represents a gauge metric
type Gauge interface {
	Set(value float64)
	Inc()
	Dec()
	Add(value float64)
	Get() float64
	Reset()
}

// Histogram represents a histogram metric
type Histogram interface {
	Observe(value float64)
	GetBuckets() map[float64]uint64
	GetCount() uint64
	GetSum() float64
	GetMean() float64
	GetPercentile(percentile float64) float64
	Reset()
}

// Timer represents a timer metric
type Timer interface {
	Record(duration time.Duration)
	Time() func()
	GetCount() uint64
	GetMean() time.Duration
	GetPercentile(percentile float64) time.Duration
	GetMin() time.Duration
	GetMax() time.Duration
	Reset()
}

// CustomCollector defines interface for custom metrics collectors
type CustomCollector interface {
	Name() string
	Collect() map[string]interface{}
	Reset()
}

// =============================================================================
// EXPORTER INTERFACE
// =============================================================================

// Exporter defines the interface for metrics export
type Exporter interface {
	// Export exports metrics in the specific format
	Export(metrics map[string]interface{}) ([]byte, error)

	// Format returns the export format identifier
	Format() string

	// Stats returns exporter statistics
	Stats() interface{}
}

// CollectorStats contains statistics about the metrics collector
type CollectorStats struct {
	Name               string                 `json:"name"`
	Started            bool                   `json:"started"`
	StartTime          time.Time              `json:"start_time"`
	Uptime             time.Duration          `json:"uptime"`
	MetricsCreated     int64                  `json:"metrics_created"`
	MetricsCollected   int64                  `json:"metrics_collected"`
	CustomCollectors   int                    `json:"custom_collectors"`
	ActiveMetrics      int                    `json:"active_metrics"`
	LastCollectionTime time.Time              `json:"last_collection_time"`
	Errors             []string               `json:"errors"`
	ExporterStats      map[string]interface{} `json:"exporter_stats"`
}
