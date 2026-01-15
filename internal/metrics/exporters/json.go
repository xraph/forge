package exporters

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/metrics"
)

// =============================================================================
// JSON EXPORTER
// =============================================================================

// JSONExporter exports metrics in JSON format.
type JSONExporter struct {
	config *JSONConfig
	stats  *JSONStats
}

// JSONConfig contains configuration for the JSON exporter.
type JSONConfig struct {
	Pretty           bool   `json:"pretty"            yaml:"pretty"`
	IncludeMetadata  bool   `json:"include_metadata"  yaml:"include_metadata"`
	IncludeTimestamp bool   `json:"include_timestamp" yaml:"include_timestamp"`
	TimestampFormat  string `json:"timestamp_format"  yaml:"timestamp_format"`
	Namespace        string `json:"namespace"         yaml:"namespace"`
}

// JSONStats contains statistics about the JSON exporter.
type JSONStats struct {
	ExportsTotal    int64     `json:"exports_total"`
	LastExportTime  time.Time `json:"last_export_time"`
	LastExportSize  int       `json:"last_export_size"`
	ErrorsTotal     int64     `json:"errors_total"`
	MetricsExported int64     `json:"metrics_exported"`
}

// JSONMetric represents a metric in JSON format.
type JSONMetric struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Value       any               `json:"value"`
	Tags        map[string]string `json:"tags,omitempty"`
	Timestamp   *time.Time        `json:"timestamp,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	Unit        string            `json:"unit,omitempty"`
	Description string            `json:"description,omitempty"`
}

// JSONExport represents the complete JSON export.
type JSONExport struct {
	Metadata *ExportMetadata `json:"metadata,omitempty"`
	Metrics  map[string]any  `json:"metrics"`
	Summary  *ExportSummary  `json:"summary,omitempty"`
}

// ExportMetadata contains metadata about the export.
type ExportMetadata struct {
	Timestamp time.Time `json:"timestamp"`
	Namespace string    `json:"namespace,omitempty"`
	Version   string    `json:"version"`
	Exporter  string    `json:"exporter"`
	Format    string    `json:"format"`
	System    string    `json:"system"`
}

// ExportSummary contains summary information about the export.
type ExportSummary struct {
	TotalMetrics int                 `json:"total_metrics"`
	MetricTypes  map[string]int      `json:"metric_types"`
	Tags         map[string][]string `json:"tags,omitempty"`
	Namespaces   []string            `json:"namespaces,omitempty"`
}

// DefaultJSONConfig returns default JSON configuration.
func DefaultJSONConfig() *JSONConfig {
	return &JSONConfig{
		Pretty:           true,
		IncludeMetadata:  true,
		IncludeTimestamp: true,
		TimestampFormat:  time.RFC3339,
		Namespace:        "",
	}
}

// NewJSONExporter creates a new JSON exporter.
func NewJSONExporter() shared.Exporter {
	return NewJSONExporterWithConfig(DefaultJSONConfig())
}

// NewJSONExporterWithConfig creates a new JSON exporter with configuration.
func NewJSONExporterWithConfig(config *JSONConfig) shared.Exporter {
	return &JSONExporter{
		config: config,
		stats:  &JSONStats{},
	}
}

// =============================================================================
// EXPORTER INTERFACE IMPLEMENTATION
// =============================================================================

// Export exports metrics in JSON format.
func (je *JSONExporter) Export(metrics map[string]any) ([]byte, error) {
	je.stats.ExportsTotal++
	je.stats.LastExportTime = time.Now()

	// Create export structure
	export := &JSONExport{
		Metrics: make(map[string]any),
	}

	// Add metadata if enabled
	if je.config.IncludeMetadata {
		export.Metadata = &ExportMetadata{
			Timestamp: time.Now(),
			Namespace: je.config.Namespace,
			Version:   "1.0.0",
			Exporter:  "json",
			Format:    "application/json",
			System:    "forge-framework",
		}
	}

	// Process metrics
	summary := &ExportSummary{
		MetricTypes: make(map[string]int),
		Tags:        make(map[string][]string),
		Namespaces:  make([]string, 0),
	}

	for name, value := range metrics {
		processedMetric := je.processMetric(name, value)
		export.Metrics[name] = processedMetric

		// Update summary
		summary.TotalMetrics++
		je.updateSummary(summary, name, processedMetric)
		je.stats.MetricsExported++
	}

	// Add summary if metadata is enabled
	if je.config.IncludeMetadata {
		export.Summary = summary
	}

	// Marshal to JSON
	var (
		data []byte
		err  error
	)

	if je.config.Pretty {
		data, err = json.MarshalIndent(export, "", "  ")
	} else {
		data, err = json.Marshal(export)
	}

	if err != nil {
		je.stats.ErrorsTotal++

		return nil, err
	}

	je.stats.LastExportSize = len(data)

	return data, nil
}

// Format returns the export format.
func (je *JSONExporter) Format() string {
	return "json"
}

// Stats returns exporter statistics.
func (je *JSONExporter) Stats() metrics.ExporterStats {
	return metrics.ExporterStats{
		ExportCount:     je.stats.ExportsTotal,
		LastExportTime:  je.stats.LastExportTime,
		LastError:       "",
		BytesExported:   0,
		LastSuccessTime: je.stats.LastExportTime,
		LastErrorTime:   je.stats.LastExportTime,
		ErrorCount:      je.stats.ErrorsTotal,
	}
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// processMetric processes a single metric for JSON export.
func (je *JSONExporter) processMetric(name string, value any) any {
	// Parse metric name and tags
	baseName, tags := je.parseMetricName(name)
	metricType := je.inferMetricType(value)

	// Create JSON metric
	jsonMetric := &JSONMetric{
		Name:  baseName,
		Type:  metricType,
		Value: je.processValue(value),
		Tags:  tags,
	}

	// Add timestamp if enabled
	if je.config.IncludeTimestamp {
		now := time.Now()
		jsonMetric.Timestamp = &now
	}

	// Add metadata if enabled
	if je.config.IncludeMetadata {
		jsonMetric.Metadata = je.generateMetadata(baseName, metricType)
		jsonMetric.Unit = je.inferUnit(baseName, metricType)
		jsonMetric.Description = je.generateDescription(baseName, metricType)
	}

	return jsonMetric
}

// processValue processes a metric value for JSON export.
func (je *JSONExporter) processValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		// Process complex metrics (histogram, timer)
		return je.processComplexValue(v)
	case float64, int64, uint64, int, uint:
		// Simple numeric values
		return v
	default:
		// Convert to string for unsupported types
		return fmt.Sprintf("%v", v)
	}
}

// processComplexValue processes complex metric values.
func (je *JSONExporter) processComplexValue(value map[string]any) any {
	result := make(map[string]any)

	// Copy all values
	for k, v := range value {
		switch k {
		case "buckets":
			// Convert histogram buckets to more readable format
			if buckets, ok := v.(map[float64]uint64); ok {
				result[k] = je.convertBuckets(buckets)
			} else {
				result[k] = v
			}
		case "count", "sum", "mean", "min", "max":
			// Keep numeric values as-is
			result[k] = v
		case "p50", "p95", "p99":
			// Convert duration values to readable format
			if duration, ok := v.(time.Duration); ok {
				result[k] = map[string]any{
					"nanoseconds": duration.Nanoseconds(),
					"seconds":     duration.Seconds(),
					"readable":    duration.String(),
				}
			} else {
				result[k] = v
			}
		default:
			result[k] = v
		}
	}

	return result
}

// convertBuckets converts histogram buckets to JSON-friendly format.
func (je *JSONExporter) convertBuckets(buckets map[float64]uint64) []map[string]any {
	result := make([]map[string]any, 0, len(buckets))

	// Sort buckets by upper bound
	bounds := make([]float64, 0, len(buckets))
	for bound := range buckets {
		bounds = append(bounds, bound)
	}

	sort.Float64s(bounds)

	for _, bound := range bounds {
		result = append(result, map[string]any{
			"upper_bound": bound,
			"count":       buckets[bound],
		})
	}

	return result
}

// parseMetricName parses a metric name and extracts tags.
func (je *JSONExporter) parseMetricName(fullName string) (string, map[string]string) {
	// Parse format: metric_name{tag1="value1",tag2="value2"}
	if !strings.Contains(fullName, "{") {
		return fullName, nil
	}

	// Find the opening brace
	braceIndex := strings.Index(fullName, "{")
	if braceIndex == -1 {
		return fullName, nil
	}

	baseName := fullName[:braceIndex]
	tagsStr := fullName[braceIndex+1:]

	// Remove closing brace
	tagsStr = strings.TrimSuffix(tagsStr, "}")

	// Parse tags
	tags := make(map[string]string)

	if tagsStr != "" {
		pairs := strings.SplitSeq(tagsStr, ",")
		for pair := range pairs {
			if kv := strings.SplitN(pair, "=", 2); len(kv) == 2 {
				key := strings.TrimSpace(kv[0])
				value := strings.TrimSpace(kv[1])
				tags[key] = value
			}
		}
	}

	return baseName, tags
}

// inferMetricType infers the metric type from the value.
func (je *JSONExporter) inferMetricType(value any) string {
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v["buckets"]; ok {
			return "histogram"
		}

		if _, ok := v["count"]; ok {
			if _, ok := v["mean"]; ok {
				return "timer"
			}

			return "counter"
		}

		return "gauge"
	case float64, int64, uint64, int, uint:
		return "gauge"
	default:
		return "unknown"
	}
}

// inferUnit infers the unit for a metric.
func (je *JSONExporter) inferUnit(name, metricType string) string {
	name = strings.ToLower(name)

	switch metricType {
	case "timer":
		return "seconds"
	case "counter":
		if strings.Contains(name, "bytes") {
			return "bytes"
		}

		if strings.Contains(name, "requests") {
			return "requests"
		}

		return "count"
	case "gauge":
		if strings.Contains(name, "memory") || strings.Contains(name, "bytes") {
			return "bytes"
		}

		if strings.Contains(name, "cpu") || strings.Contains(name, "percent") {
			return "percent"
		}

		if strings.Contains(name, "connections") {
			return "connections"
		}

		return "value"
	case "histogram":
		if strings.Contains(name, "duration") || strings.Contains(name, "latency") {
			return "seconds"
		}

		if strings.Contains(name, "size") || strings.Contains(name, "bytes") {
			return "bytes"
		}

		return "value"
	}

	return "value"
}

// generateDescription generates a description for a metric.
func (je *JSONExporter) generateDescription(name, metricType string) string {
	switch metricType {
	case "counter":
		return "Counter metric tracking " + name
	case "gauge":
		return "Gauge metric measuring " + name
	case "histogram":
		return fmt.Sprintf("Histogram metric for %s distribution", name)
	case "timer":
		return fmt.Sprintf("Timer metric for %s duration", name)
	default:
		return "Metric for " + name
	}
}

// generateMetadata generates metadata for a metric.
func (je *JSONExporter) generateMetadata(name, metricType string) map[string]any {
	return map[string]any{
		"created_at": time.Now().Format(je.config.TimestampFormat),
		"type":       metricType,
		"namespace":  je.config.Namespace,
		"exporter":   "json",
	}
}

// updateSummary updates the export summary with metric information.
func (je *JSONExporter) updateSummary(summary *ExportSummary, name string, metric any) {
	// Extract metric type
	var metricType string
	if jsonMetric, ok := metric.(*JSONMetric); ok {
		metricType = jsonMetric.Type

		// Update metric type counts
		summary.MetricTypes[metricType]++

		// Update tags
		for tagKey, tagValue := range jsonMetric.Tags {
			if values, exists := summary.Tags[tagKey]; exists {
				// Check if value already exists
				found := slices.Contains(values, tagValue)

				if !found {
					summary.Tags[tagKey] = append(values, tagValue)
				}
			} else {
				summary.Tags[tagKey] = []string{tagValue}
			}
		}
	}

	// Update namespaces
	if je.config.Namespace != "" {
		found := slices.Contains(summary.Namespaces, je.config.Namespace)

		if !found {
			summary.Namespaces = append(summary.Namespaces, je.config.Namespace)
		}
	}
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// ExportMetricsToJSON exports metrics to JSON format with default configuration.
func ExportMetricsToJSON(metrics map[string]any) ([]byte, error) {
	exporter := NewJSONExporter()

	return exporter.Export(metrics)
}

// ExportMetricsToJSONWithConfig exports metrics to JSON format with custom configuration.
func ExportMetricsToJSONWithConfig(metrics map[string]any, config *JSONConfig) ([]byte, error) {
	exporter := NewJSONExporterWithConfig(config)

	return exporter.Export(metrics)
}

// CompactJSONExport creates a compact JSON export without metadata.
func CompactJSONExport(metrics map[string]any) ([]byte, error) {
	config := &JSONConfig{
		Pretty:           false,
		IncludeMetadata:  false,
		IncludeTimestamp: false,
	}

	return ExportMetricsToJSONWithConfig(metrics, config)
}

// PrettyJSONExport creates a pretty-printed JSON export with full metadata.
func PrettyJSONExport(metrics map[string]any) ([]byte, error) {
	config := &JSONConfig{
		Pretty:           true,
		IncludeMetadata:  true,
		IncludeTimestamp: true,
		TimestampFormat:  time.RFC3339,
	}

	return ExportMetricsToJSONWithConfig(metrics, config)
}

// JSONExportWithNamespace creates a JSON export with a specific namespace.
func JSONExportWithNamespace(metrics map[string]any, namespace string) ([]byte, error) {
	config := &JSONConfig{
		Pretty:           true,
		IncludeMetadata:  true,
		IncludeTimestamp: true,
		TimestampFormat:  time.RFC3339,
		Namespace:        namespace,
	}

	return ExportMetricsToJSONWithConfig(metrics, config)
}
