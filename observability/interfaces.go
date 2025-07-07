package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

// Tracer represents the distributed tracing interface
type Tracer interface {
	// Span operations
	StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, Span)
	SpanFromContext(ctx context.Context) Span

	// Trace operations
	Extract(ctx context.Context, carrier interface{}) context.Context
	Inject(ctx context.Context, carrier interface{}) error

	// Configuration
	SetGlobalTracer()
	Shutdown(ctx context.Context) error
}

// Span represents a distributed tracing span
type Span interface {
	// Span lifecycle
	End()
	Finish()

	// Span data
	SetName(name string)
	SetStatus(code StatusCode, description string)
	AddEvent(name string, attrs ...Attribute)
	RecordError(err error, attrs ...Attribute)

	// Attributes
	SetAttribute(key string, value interface{})
	SetAttributes(attrs ...Attribute)

	// Span context
	Context() trace.SpanContext
	IsRecording() bool

	// Baggage (for cross-service data)
	SetBaggageItem(key, value string)
	GetBaggageItem(key string) string
}

// Metrics represents the metrics collection interface
type Metrics interface {
	// Counter operations
	Counter(name string, labels ...Label) Counter
	IncrementCounter(name string, value float64, labels ...Label)

	// Gauge operations
	Gauge(name string, labels ...Label) Gauge
	SetGauge(name string, value float64, labels ...Label)
	IncrementGauge(name string, value float64, labels ...Label)
	DecrementGauge(name string, value float64, labels ...Label)

	// Histogram operations
	Histogram(name string, buckets []float64, labels ...Label) Histogram
	ObserveHistogram(name string, value float64, labels ...Label)

	// Summary operations
	Summary(name string, objectives map[float64]float64, labels ...Label) Summary
	ObserveSummary(name string, value float64, labels ...Label)

	// HTTP middleware
	HTTPMiddleware() func(http.Handler) http.Handler

	// Server
	StartServer(ctx context.Context) error
	StopServer(ctx context.Context) error

	// Registry
	Register(collector Collector) error
	Unregister(collector Collector) error

	// Export
	Export(ctx context.Context) error
	Gather() ([]*MetricFamily, error)
}

// Counter represents a monotonically increasing metric
type Counter interface {
	Inc()
	Add(value float64)
	Get() float64
}

// Gauge represents a metric that can go up and down
type Gauge interface {
	Set(value float64)
	Inc()
	Dec()
	Add(value float64)
	Sub(value float64)
	Get() float64
}

// Histogram represents a metric that samples observations
type Histogram interface {
	Observe(value float64)
	ObserveWithContext(ctx context.Context, value float64)
	Timer() Timer
}

// Summary represents a metric that samples observations with quantiles
type Summary interface {
	Observe(value float64)
	ObserveWithContext(ctx context.Context, value float64)
	Timer() Timer
}

// Timer represents a timing utility
type Timer interface {
	ObserveDuration()
}

// Health represents the health checking interface
type Health interface {
	// Check registration
	RegisterCheck(name string, check HealthCheck) error
	UnregisterCheck(name string) error

	// Health checking
	Check(ctx context.Context) HealthStatus
	CheckSingle(ctx context.Context, name string) HealthCheckResult

	// HTTP handlers
	Handler() http.HandlerFunc
	ReadinessHandler() http.HandlerFunc
	LivenessHandler() http.HandlerFunc

	// Configuration
	SetTimeout(timeout time.Duration)
	SetInterval(interval time.Duration)
}

// HealthCheck represents a single health check
type HealthCheck interface {
	Check(ctx context.Context) error
	Name() string
	Description() string
	Timeout() time.Duration
}

// Configuration types

// TracingConfig represents tracing configuration
type TracingConfig struct {
	Enabled        bool   `mapstructure:"enabled" yaml:"enabled"`
	ServiceName    string `mapstructure:"service_name" yaml:"service_name"`
	Endpoint       string `mapstructure:"endpoint" yaml:"endpoint"`
	ServiceVersion string `mapstructure:"service_version" yaml:"service_version"`
	Environment    string `mapstructure:"environment" yaml:"environment"`

	// Sampling
	SampleRate   float64 `mapstructure:"sample_rate" yaml:"sample_rate"`
	AlwaysSample bool    `mapstructure:"always_sample" yaml:"always_sample"`
	NeverSample  bool    `mapstructure:"never_sample" yaml:"never_sample"`

	// Exporters
	Exporters []ExporterConfig `mapstructure:"exporters" yaml:"exporters"`

	// Resource attributes
	ResourceAttributes map[string]string `mapstructure:"resource_attributes" yaml:"resource_attributes"`

	// Batch processor
	BatchTimeout       time.Duration `mapstructure:"batch_timeout" yaml:"batch_timeout"`
	ExportTimeout      time.Duration `mapstructure:"export_timeout" yaml:"export_timeout"`
	MaxExportBatchSize int           `mapstructure:"max_export_batch_size" yaml:"max_export_batch_size"`
	MaxQueueSize       int           `mapstructure:"max_queue_size" yaml:"max_queue_size"`
}

// ExporterConfig represents trace exporter configuration
type ExporterConfig struct {
	Type     string            `mapstructure:"type" yaml:"type"` // jaeger, otlp, zipkin, stdout
	Endpoint string            `mapstructure:"endpoint" yaml:"endpoint"`
	Headers  map[string]string `mapstructure:"headers" yaml:"headers"`
	Insecure bool              `mapstructure:"insecure" yaml:"insecure"`

	// Jaeger specific
	AgentHost string `mapstructure:"agent_host" yaml:"agent_host"`
	AgentPort int    `mapstructure:"agent_port" yaml:"agent_port"`

	// OTLP specific
	Compression string `mapstructure:"compression" yaml:"compression"`

	// File specific
	FilePath string `mapstructure:"file_path" yaml:"file_path"`
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	Enabled     bool   `mapstructure:"enabled" yaml:"enabled"`
	ServiceName string `mapstructure:"service_name" yaml:"service_name"`
	Namespace   string `mapstructure:"namespace" yaml:"namespace"`
	Subsystem   string `mapstructure:"subsystem" yaml:"subsystem"`

	// HTTP server
	Host string `mapstructure:"host" yaml:"host"`
	Port int    `mapstructure:"port" yaml:"port"`
	Path string `mapstructure:"path" yaml:"path"`

	// Collection
	Interval time.Duration `mapstructure:"interval" yaml:"interval"`
	Timeout  time.Duration `mapstructure:"timeout" yaml:"timeout"`

	// Labels
	DefaultLabels map[string]string `mapstructure:"default_labels" yaml:"default_labels"`

	// Features
	EnableGo       bool `mapstructure:"enable_go" yaml:"enable_go"`
	EnableProcess  bool `mapstructure:"enable_process" yaml:"enable_process"`
	EnableRuntime  bool `mapstructure:"enable_runtime" yaml:"enable_runtime"`
	EnableHTTP     bool `mapstructure:"enable_http" yaml:"enable_http"`
	EnableDatabase bool `mapstructure:"enable_database" yaml:"enable_database"`
	EnableCache    bool `mapstructure:"enable_cache" yaml:"enable_cache"`

	// Exporters
	Exporters []MetricsExporterConfig `mapstructure:"exporters" yaml:"exporters"`
}

// MetricsExporterConfig represents metrics exporter configuration
type MetricsExporterConfig struct {
	Type     string            `mapstructure:"type" yaml:"type"` // prometheus, otlp, stdout
	Endpoint string            `mapstructure:"endpoint" yaml:"endpoint"`
	Headers  map[string]string `mapstructure:"headers" yaml:"headers"`
	Interval time.Duration     `mapstructure:"interval" yaml:"interval"`
	Timeout  time.Duration     `mapstructure:"timeout" yaml:"timeout"`
}

// HealthConfig represents health check configuration
type HealthConfig struct {
	Enabled bool   `mapstructure:"enabled" yaml:"enabled"`
	Host    string `mapstructure:"host" yaml:"host"`
	Port    int    `mapstructure:"port" yaml:"port"`
	Path    string `mapstructure:"path" yaml:"path"`

	// Check configuration
	Timeout  time.Duration `mapstructure:"timeout" yaml:"timeout"`
	Interval time.Duration `mapstructure:"interval" yaml:"interval"`

	// Endpoints
	LivenessPath  string `mapstructure:"liveness_path" yaml:"liveness_path"`
	ReadinessPath string `mapstructure:"readiness_path" yaml:"readiness_path"`

	// Response format
	Format         string `mapstructure:"format" yaml:"format"` // json, text
	IncludeDetails bool   `mapstructure:"include_details" yaml:"include_details"`
}

// Supporting types

// SpanOption represents options for creating spans
type SpanOption interface {
	Apply(*SpanConfig)
}

// SpanConfig represents span configuration
type SpanConfig struct {
	Kind       SpanKind
	Attributes []Attribute
	Timestamp  time.Time
	Parent     trace.SpanContext
}

// SpanKind represents the kind of span
type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

// StatusCode represents span status codes
type StatusCode int

const (
	StatusCodeUnset StatusCode = iota
	StatusCodeOK
	StatusCodeError
)

// Attribute represents a key-value attribute
type Attribute struct {
	Key   string
	Value interface{}
}

// Label represents a metric label
type Label struct {
	Name  string
	Value string
}

// Collector represents a metrics collector
type Collector = prometheus.Collector

// MetricDesc represents a metric description
type MetricDesc struct {
	Name        string
	Help        string
	Type        MetricType
	Labels      []string
	ConstLabels map[string]string
}

// MetricFamily represents a family of metrics
type MetricFamily struct {
	Name    string
	Help    string
	Type    MetricType
	Metrics []Metric
}

// Metric represents a single metric
type Metric interface {
	Name() string
	Type() MetricType
	Value() float64
	Labels() map[string]string
	Timestamp() time.Time
}

// MetricType represents the type of a metric
type MetricType int

const (
	MetricTypeCounter MetricType = iota
	MetricTypeGauge
	MetricTypeHistogram
	MetricTypeSummary
)

// HealthStatus represents overall health status
type HealthStatus struct {
	Status  HealthStatusLevel            `json:"status"`
	Checks  map[string]HealthCheckResult `json:"checks"`
	Details map[string]interface{}       `json:"details,omitempty"`

	// Metadata
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
	Version   string        `json:"version,omitempty"`
}

// HealthCheckResult represents the result of a single health check
type HealthCheckResult struct {
	Status    HealthStatusLevel      `json:"status"`
	Message   string                 `json:"message,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Duration  time.Duration          `json:"duration"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// HealthStatusLevel represents health status levels
type HealthStatusLevel string

const (
	HealthStatusUp      HealthStatusLevel = "UP"
	HealthStatusDown    HealthStatusLevel = "DOWN"
	HealthStatusWarning HealthStatusLevel = "WARNING"
	HealthStatusUnknown HealthStatusLevel = "UNKNOWN"
)

// Instrumentation helpers

// HTTPInstrumentation provides HTTP instrumentation utilities
type HTTPInstrumentation interface {
	// Middleware
	Middleware() func(http.Handler) http.Handler

	// Manual instrumentation
	InstrumentHandler(name string, handler http.HandlerFunc) http.HandlerFunc
	InstrumentRoundTripper(name string, rt http.RoundTripper) http.RoundTripper

	// Metrics
	RequestDuration() Histogram
	RequestCount() Counter
	RequestSize() Histogram
	ResponseSize() Histogram
	ActiveRequests() Gauge
}

// DatabaseInstrumentation provides database instrumentation utilities
type DatabaseInstrumentation interface {
	// SQL instrumentation
	InstrumentSQL(query string) func(error)

	// Connection pool metrics
	PoolMetrics(poolName string) DatabasePoolMetrics

	// Query metrics
	QueryDuration(operation string) Histogram
	QueryCount(operation string) Counter
	QueryErrors(operation string) Counter
}

// DatabasePoolMetrics represents database pool metrics
type DatabasePoolMetrics interface {
	SetMaxOpenConnections(n int)
	SetOpenConnections(n int)
	SetInUseConnections(n int)
	SetIdleConnections(n int)
	IncrementWaitCount()
	ObserveWaitDuration(d time.Duration)
}

// CacheInstrumentation provides cache instrumentation utilities
type CacheInstrumentation interface {
	// Cache operations
	Hit(cacheName string)
	Miss(cacheName string)
	Set(cacheName string, size int)
	Delete(cacheName string)
	Error(cacheName string, operation string)

	// Duration tracking
	ObserveDuration(cacheName, operation string, duration time.Duration)

	// Size tracking
	ObserveSize(cacheName string, size int)
}

// Error types
var (
	ErrTracingNotEnabled  = fmt.Errorf("tracing not enabled")
	ErrMetricsNotEnabled  = fmt.Errorf("metrics not enabled")
	ErrHealthCheckFailed  = fmt.Errorf("health check failed")
	ErrInvalidExporter    = fmt.Errorf("invalid exporter")
	ErrSpanNotFound       = fmt.Errorf("span not found")
	ErrMetricNotFound     = fmt.Errorf("metric not found")
	ErrCheckNotRegistered = fmt.Errorf("health check not registered")
)

// Standard health checks

// DatabaseHealthCheck creates a health check for database connectivity
type DatabaseHealthCheck struct {
	name     string
	database interface{ Ping(context.Context) error }
	timeout  time.Duration
}

// RedisHealthCheck creates a health check for Redis connectivity
type RedisHealthCheck struct {
	name    string
	client  interface{ Ping(context.Context) error }
	timeout time.Duration
}

// HTTPHealthCheck creates a health check for HTTP endpoint
type HTTPHealthCheck struct {
	name     string
	url      string
	timeout  time.Duration
	client   *http.Client
	expected int // expected status code
}

// FileSystemHealthCheck creates a health check for file system access
type FileSystemHealthCheck struct {
	name    string
	path    string
	timeout time.Duration
}

// MemoryHealthCheck creates a health check for memory usage
type MemoryHealthCheck struct {
	name      string
	threshold int64 // in bytes
	timeout   time.Duration
}

// DiskSpaceHealthCheck creates a health check for disk space
type DiskSpaceHealthCheck struct {
	name      string
	path      string
	threshold int64 // minimum free space in bytes
	timeout   time.Duration
}
