package shared

import "time"

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

// StorageConfig contains storage configuration
type MetricsStorageConfig[T any] struct {
	Type   string `yaml:"type" json:"type"`
	Config T      `yaml:"config" json:"config"`
}

// ExporterConfig contains configuration for exporters
type MetricsExporterConfig[T any] struct {
	Enabled  bool          `yaml:"enabled" json:"enabled"`
	Interval time.Duration `yaml:"interval" json:"interval"`
	Config   T             `yaml:"config" json:"config"`
}

// MetricsConfig configures metrics collection
type MetricsConfig struct {
	Enabled     bool
	MetricsPath string
	Namespace   string

	EnableSystemMetrics  bool                                                     `yaml:"enable_system_metrics" json:"enable_system_metrics"`
	EnableRuntimeMetrics bool                                                     `yaml:"enable_runtime_metrics" json:"enable_runtime_metrics"`
	EnableHTTPMetrics    bool                                                     `yaml:"enable_http_metrics" json:"enable_http_metrics"`
	CollectionInterval   time.Duration                                            `yaml:"collection_interval" json:"collection_interval"`
	StorageConfig        *MetricsStorageConfig[map[string]interface{}]            `yaml:"storage" json:"storage"`
	Exporters            map[string]MetricsExporterConfig[map[string]interface{}] `yaml:"exporters" json:"exporters"`
	DefaultTags          map[string]string                                        `yaml:"default_tags" json:"default_tags"`
	MaxMetrics           int                                                      `yaml:"max_metrics" json:"max_metrics"`
	BufferSize           int                                                      `yaml:"buffer_size" json:"buffer_size"`
}

// Metrics provides telemetry collection
type Metrics interface {
	Service
	HealthChecker

	// Counter metrics (monotonically increasing)
	// Labels should be provided as key-value pairs: "key1", "value1", "key2", "value2"
	Counter(name string, labels ...string) Counter

	// Gauge metrics (can go up or down)
	// Labels should be provided as key-value pairs: "key1", "value1", "key2", "value2"
	Gauge(name string, labels ...string) Gauge

	// Histogram metrics (distributions)
	// Labels should be provided as key-value pairs: "key1", "value1", "key2", "value2"
	Histogram(name string, labels ...string) Histogram

	// Timer metrics (time taken to complete an operation)
	// Labels should be provided as key-value pairs: "key1", "value1", "key2", "value2"
	Timer(name string, labels ...string) Timer

	// Export metrics (Prometheus format by default)
	Export(format ExportFormat) ([]byte, error)

	// Export to file
	ExportToFile(format ExportFormat, filename string) error

	// Register a custom collector
	RegisterCollector(collector CustomCollector) error

	// Unregister a custom collector
	UnregisterCollector(name string) error

	// Get all custom collectors
	GetCollectors() []CustomCollector

	// Reset metrics
	Reset() error

	// Reset metric
	ResetMetric(name string) error

	// Get metrics
	GetMetrics() map[string]interface{}

	// Get metrics by type
	GetMetricsByType(metricType MetricType) map[string]interface{}

	// Get metrics by tag
	GetMetricsByTag(tagKey, tagValue string) map[string]interface{}

	// Get stats
	GetStats() CollectorStats

	// Reload reloads the metrics configuration at runtime
	Reload(config *MetricsConfig) error
}

// Counter tracks monotonically increasing values
type Counter interface {
	Inc()
	Add(delta float64)
	Get() float64
	WithLabels(labels map[string]string) Counter
	Reset() error
}

// Gauge tracks values that can go up or down
type Gauge interface {
	Set(value float64)
	Inc()
	Dec()
	Add(delta float64)
	Get() float64
	WithLabels(labels map[string]string) Gauge
	Reset() error
}

// Histogram tracks distributions of values
type Histogram interface {
	Observe(value float64)
	ObserveDuration(start time.Time)
	GetBuckets() map[float64]uint64
	GetCount() uint64
	GetSum() float64
	GetMean() float64
	GetPercentile(percentile float64) float64
	WithLabels(labels map[string]string) Histogram
	Reset() error
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
	Get() time.Duration
	Reset()
}

// CustomCollector defines interface for custom metrics collectors
type CustomCollector interface {
	Name() string
	Collect() map[string]interface{}
	// WithLabels(labels map[string]string) CustomCollector
	// Get() map[string]interface{}
	Reset() error
	IsEnabled() bool
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
