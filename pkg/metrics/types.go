package metrics

import (
	"time"

	metriccore "github.com/xraph/forge/pkg/metrics/core"
)

// =============================================================================
// METRIC INTERFACES
// =============================================================================

// Counter represents a counter metric
type Counter = metriccore.Counter

// Gauge represents a gauge metric
type Gauge = metriccore.Gauge

// Histogram represents a histogram metric
type Histogram = metriccore.Histogram

// Timer represents a timer metric
type Timer = metriccore.Timer

// CustomCollector defines interface for custom metrics collectors
type CustomCollector = metriccore.CustomCollector

// =============================================================================
// METRIC IMPLEMENTATIONS
// =============================================================================

// NewCounter creates a new counter
func NewCounter() Counter {
	return metriccore.NewCounter()
}

// NewGauge creates a new gauge
func NewGauge() Gauge {
	return metriccore.NewGauge()
}

// NewHistogram creates a new histogram with default buckets
func NewHistogram() Histogram {
	return metriccore.NewHistogramWithBuckets(DefaultBuckets)
}

// NewHistogramWithBuckets creates a new histogram with custom buckets
func NewHistogramWithBuckets(buckets []float64) Histogram {
	return metriccore.NewHistogramWithBuckets(buckets)
}

// DefaultBuckets provides default histogram buckets
var DefaultBuckets = metriccore.DefaultBuckets

// NewTimer creates a new timer
func NewTimer() Timer {
	return metriccore.NewTimer()
}

// =============================================================================
// METRIC METADATA
// =============================================================================

// MetricType represents the type of metric
type MetricType = metriccore.MetricType

const (
	MetricTypeCounter   = metriccore.MetricTypeCounter
	MetricTypeGauge     = metriccore.MetricTypeGauge
	MetricTypeHistogram = metriccore.MetricTypeHistogram
	MetricTypeTimer     = metriccore.MetricTypeTimer
)

// MetricMetadata contains metadata about a metric
type MetricMetadata struct {
	Name        string            `json:"name"`
	Type        MetricType        `json:"type"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	Unit        string            `json:"unit"`
	Created     time.Time         `json:"created"`
	Updated     time.Time         `json:"updated"`
}

// MetricValue represents a metric value with metadata
type MetricValue struct {
	Metadata  *MetricMetadata `json:"metadata"`
	Value     interface{}     `json:"value"`
	Timestamp time.Time       `json:"timestamp"`
}

// ExportFormat represents the format for metrics export
type ExportFormat = metriccore.ExportFormat

const (
	ExportFormatPrometheus = metriccore.ExportFormatPrometheus
	ExportFormatJSON       = metriccore.ExportFormatJSON
	ExportFormatInflux     = metriccore.ExportFormatInflux
	ExportFormatStatsD     = metriccore.ExportFormatStatsD
)

// =============================================================================
// METRIC SAMPLE
// =============================================================================

// MetricSample represents a single metric sample
type MetricSample struct {
	Name      string            `json:"name"`
	Type      MetricType        `json:"type"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags"`
	Timestamp time.Time         `json:"timestamp"`
	Unit      string            `json:"unit"`
}

// HistogramSample represents a histogram sample
type HistogramSample struct {
	Name      string             `json:"name"`
	Count     uint64             `json:"count"`
	Sum       float64            `json:"sum"`
	Buckets   map[float64]uint64 `json:"buckets"`
	Tags      map[string]string  `json:"tags"`
	Timestamp time.Time          `json:"timestamp"`
}

// TimerSample represents a timer sample
type TimerSample struct {
	Name      string            `json:"name"`
	Count     uint64            `json:"count"`
	Mean      time.Duration     `json:"mean"`
	Min       time.Duration     `json:"min"`
	Max       time.Duration     `json:"max"`
	P50       time.Duration     `json:"p50"`
	P95       time.Duration     `json:"p95"`
	P99       time.Duration     `json:"p99"`
	Tags      map[string]string `json:"tags"`
	Timestamp time.Time         `json:"timestamp"`
}

// Exporter defines the interface for metrics export
type Exporter = metriccore.Exporter

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// TagsToString converts tags map to string representation
func TagsToString(tags map[string]string) string {
	return metriccore.TagsToString(tags)
}

// ParseTags parses tags from string array
func ParseTags(tags ...string) map[string]string {
	return metriccore.ParseTags(tags...)
}

// MergeTags merges multiple tag maps
func MergeTags(tagMaps ...map[string]string) map[string]string {
	return metriccore.MergeTags(tagMaps...)
}

// ValidateMetricName validates metric name format
func ValidateMetricName(name string) bool {
	return metriccore.ValidateMetricName(name)
}

// NormalizeMetricName normalizes metric name
func NormalizeMetricName(name string) string {
	return metriccore.NormalizeMetricName(name)
}
