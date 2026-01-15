package shared

import (
	"github.com/xraph/go-utils/metrics"
)

// ExportFormat represents the format for metrics export.
type ExportFormat = metrics.ExportFormat

const (
	ExportFormatPrometheus metrics.ExportFormat = metrics.ExportFormatPrometheus
	ExportFormatJSON       metrics.ExportFormat = metrics.ExportFormatJSON
	ExportFormatInflux     metrics.ExportFormat = metrics.ExportFormatInflux
	ExportFormatStatsD     metrics.ExportFormat = metrics.ExportFormatStatsD
)

// MetricType represents the type of metric.
type MetricType = metrics.MetricType

const (
	MetricTypeCounter   MetricType = metrics.MetricTypeCounter
	MetricTypeGauge     MetricType = metrics.MetricTypeGauge
	MetricTypeHistogram MetricType = metrics.MetricTypeHistogram
	MetricTypeTimer     MetricType = metrics.MetricTypeTimer
)

// MetricsStorageConfig contains storage configuration.
type MetricsStorageConfig[T any] = metrics.MetricsStorageConfig[T]

// MetricsExporterConfig contains configuration for exporters.
type MetricsExporterConfig[T any] = metrics.MetricsExporterConfig[T]

// MetricsConfig configures metrics collection.
type MetricsConfig = metrics.MetricsConfig

// MetricsFeatures contains features for metrics collection.
type MetricsFeatures = metrics.MetricsFeatures

// MetricsCollection contains collection configuration.
type MetricsCollection = metrics.MetricsCollection

// MetricsLimits contains limits for metrics collection.
type MetricsLimits = metrics.MetricsLimits

// Metrics provides telemetry collection.
type Metrics = metrics.Metrics

// Counter tracks monotonically increasing values.
type Counter = metrics.Counter

// Gauge tracks values that can go up or down.
type Gauge = metrics.Gauge

// Timer represents a timer metric.
type Timer = metrics.Timer

// Histogram tracks distributions of values.
type Histogram = metrics.Histogram

// CustomCollector defines interface for custom metrics collectors.
type CustomCollector = metrics.CustomCollector

// MetricOption defines an option for creating a metric.
type MetricOption = metrics.MetricOption

// =============================================================================
// EXPORTER INTERFACE
// =============================================================================

// Exporter defines the interface for metrics export.
type Exporter = metrics.Exporter

// CollectorStats contains statistics about the metrics collector.
type CollectorStats = metrics.CollectorStats
