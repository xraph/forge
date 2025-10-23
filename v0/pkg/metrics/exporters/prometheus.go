package exporters

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	metrics "github.com/xraph/forge/pkg/metrics/core"
)

// =============================================================================
// PROMETHEUS EXPORTER
// =============================================================================

// PrometheusExporter exports metrics in Prometheus format
type PrometheusExporter struct {
	namespace string
	subsystem string
	labels    map[string]string
	stats     *PrometheusStats
}

// PrometheusStats contains statistics about the Prometheus exporter
type PrometheusStats struct {
	ExportsTotal    int64     `json:"exports_total"`
	LastExportTime  time.Time `json:"last_export_time"`
	LastExportSize  int       `json:"last_export_size"`
	ErrorsTotal     int64     `json:"errors_total"`
	MetricsExported int64     `json:"metrics_exported"`
}

// PrometheusConfig contains configuration for the Prometheus exporter
type PrometheusConfig struct {
	Namespace       string            `yaml:"namespace" json:"namespace"`
	Subsystem       string            `yaml:"subsystem" json:"subsystem"`
	Labels          map[string]string `yaml:"labels" json:"labels"`
	IncludeHelp     bool              `yaml:"include_help" json:"include_help"`
	IncludeType     bool              `yaml:"include_type" json:"include_type"`
	TimestampFormat string            `yaml:"timestamp_format" json:"timestamp_format"`
}

// DefaultPrometheusConfig returns default Prometheus configuration
func DefaultPrometheusConfig() *PrometheusConfig {
	return &PrometheusConfig{
		Namespace:       "forge",
		Subsystem:       "",
		Labels:          make(map[string]string),
		IncludeHelp:     true,
		IncludeType:     true,
		TimestampFormat: "unix",
	}
}

// NewPrometheusExporter creates a new Prometheus exporter
func NewPrometheusExporter() metrics.Exporter {
	return NewPrometheusExporterWithConfig(DefaultPrometheusConfig())
}

// NewPrometheusExporterWithConfig creates a new Prometheus exporter with configuration
func NewPrometheusExporterWithConfig(config *PrometheusConfig) metrics.Exporter {
	return &PrometheusExporter{
		namespace: config.Namespace,
		subsystem: config.Subsystem,
		labels:    config.Labels,
		stats:     &PrometheusStats{},
	}
}

// =============================================================================
// EXPORTER INTERFACE IMPLEMENTATION
// =============================================================================

// Export exports metrics in Prometheus format
func (pe *PrometheusExporter) Export(metrics map[string]interface{}) ([]byte, error) {
	pe.stats.ExportsTotal++
	pe.stats.LastExportTime = time.Now()

	var output strings.Builder

	// Write header comment
	output.WriteString("# Metrics exported by Forge Framework\n")
	output.WriteString(fmt.Sprintf("# Timestamp: %s\n", time.Now().Format(time.RFC3339)))
	output.WriteString("\n")

	// Group metrics by name for better organization
	groupedMetrics := pe.groupMetrics(metrics)

	// Sort metric names for consistent output
	names := make([]string, 0, len(groupedMetrics))
	for name := range groupedMetrics {
		names = append(names, name)
	}
	sort.Strings(names)

	// Export each metric group
	for _, name := range names {
		metricGroup := groupedMetrics[name]
		if err := pe.exportMetricGroup(&output, name, metricGroup); err != nil {
			pe.stats.ErrorsTotal++
			return nil, fmt.Errorf("failed to export metric group %s: %w", name, err)
		}
		pe.stats.MetricsExported++
	}

	result := []byte(output.String())
	pe.stats.LastExportSize = len(result)

	return result, nil
}

// Format returns the export format
func (pe *PrometheusExporter) Format() string {
	return "prometheus"
}

// Stats returns exporter statistics
func (pe *PrometheusExporter) Stats() interface{} {
	return pe.stats
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// groupMetrics groups metrics by base name
func (pe *PrometheusExporter) groupMetrics(metrics map[string]interface{}) map[string][]metricEntry {
	grouped := make(map[string][]metricEntry)

	for fullName, value := range metrics {
		baseName, tags := pe.parseMetricName(fullName)

		entry := metricEntry{
			Name:  baseName,
			Value: value,
			Tags:  tags,
		}

		grouped[baseName] = append(grouped[baseName], entry)
	}

	return grouped
}

// metricEntry represents a single metric entry
type metricEntry struct {
	Name  string
	Value interface{}
	Tags  map[string]string
}

// parseMetricName parses a metric name and extracts tags
func (pe *PrometheusExporter) parseMetricName(fullName string) (string, map[string]string) {
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
	if strings.HasSuffix(tagsStr, "}") {
		tagsStr = tagsStr[:len(tagsStr)-1]
	}

	// Parse tags
	tags := make(map[string]string)
	if tagsStr != "" {
		pairs := strings.Split(tagsStr, ",")
		for _, pair := range pairs {
			if kv := strings.SplitN(pair, "=", 2); len(kv) == 2 {
				key := strings.TrimSpace(kv[0])
				value := strings.TrimSpace(kv[1])
				tags[key] = value
			}
		}
	}

	return baseName, tags
}

// exportMetricGroup exports a group of metrics with the same base name
func (pe *PrometheusExporter) exportMetricGroup(output *strings.Builder, baseName string, entries []metricEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Generate full metric name with namespace/subsystem
	fullName := pe.buildFullName(baseName)

	// Determine metric type from first entry
	metricType := pe.inferMetricType(entries[0].Value)

	// Write HELP comment
	output.WriteString(fmt.Sprintf("# HELP %s %s\n", fullName, pe.generateHelpText(baseName, metricType)))

	// Write TYPE comment
	output.WriteString(fmt.Sprintf("# TYPE %s %s\n", fullName, metricType))

	// Export each entry
	for _, entry := range entries {
		if err := pe.exportMetricEntry(output, fullName, entry); err != nil {
			return fmt.Errorf("failed to export metric entry: %w", err)
		}
	}

	output.WriteString("\n")
	return nil
}

// exportMetricEntry exports a single metric entry
func (pe *PrometheusExporter) exportMetricEntry(output *strings.Builder, fullName string, entry metricEntry) error {
	switch value := entry.Value.(type) {
	case float64:
		pe.writeMetricLine(output, fullName, entry.Tags, value)
	case int64:
		pe.writeMetricLine(output, fullName, entry.Tags, float64(value))
	case uint64:
		pe.writeMetricLine(output, fullName, entry.Tags, float64(value))
	case map[string]interface{}:
		return pe.exportComplexMetric(output, fullName, entry.Tags, value)
	default:
		return fmt.Errorf("unsupported metric value type: %T", value)
	}

	return nil
}

// exportComplexMetric exports complex metrics (histogram, timer)
func (pe *PrometheusExporter) exportComplexMetric(output *strings.Builder, fullName string, tags map[string]string, value map[string]interface{}) error {
	// Handle histogram metrics
	if buckets, ok := value["buckets"].(map[float64]uint64); ok {
		return pe.exportHistogram(output, fullName, tags, value, buckets)
	}

	// Handle timer metrics
	if count, ok := value["count"].(uint64); ok {
		return pe.exportTimer(output, fullName, tags, value, count)
	}

	// Handle gauge-like metrics
	if val, ok := value["value"]; ok {
		return pe.exportMetricEntry(output, fullName, metricEntry{
			Name:  fullName,
			Value: val,
			Tags:  tags,
		})
	}

	return fmt.Errorf("unsupported complex metric format")
}

// exportHistogram exports histogram metrics
func (pe *PrometheusExporter) exportHistogram(output *strings.Builder, fullName string, tags map[string]string, value map[string]interface{}, buckets map[float64]uint64) error {
	// Export histogram buckets
	bucketKeys := make([]float64, 0, len(buckets))
	for bucket := range buckets {
		bucketKeys = append(bucketKeys, bucket)
	}
	sort.Float64s(bucketKeys)

	for _, bucket := range bucketKeys {
		bucketTags := pe.mergeTags(tags, map[string]string{"le": pe.formatFloat(bucket)})
		pe.writeMetricLine(output, fullName+"_bucket", bucketTags, float64(buckets[bucket]))
	}

	// Export +Inf bucket
	infTags := pe.mergeTags(tags, map[string]string{"le": "+Inf"})
	if count, ok := value["count"].(uint64); ok {
		pe.writeMetricLine(output, fullName+"_bucket", infTags, float64(count))
	}

	// Export histogram sum
	if sum, ok := value["sum"].(float64); ok {
		pe.writeMetricLine(output, fullName+"_sum", tags, sum)
	}

	// Export histogram count
	if count, ok := value["count"].(uint64); ok {
		pe.writeMetricLine(output, fullName+"_count", tags, float64(count))
	}

	return nil
}

// exportTimer exports timer metrics
func (pe *PrometheusExporter) exportTimer(output *strings.Builder, fullName string, tags map[string]string, value map[string]interface{}, count uint64) error {
	// Export timer count
	pe.writeMetricLine(output, fullName+"_count", tags, float64(count))

	// Export timer sum (if available)
	if sum, ok := value["sum"].(time.Duration); ok {
		pe.writeMetricLine(output, fullName+"_sum", tags, sum.Seconds())
	}

	// Export percentiles as separate metrics
	percentiles := []string{"p50", "p95", "p99"}
	for _, p := range percentiles {
		if val, ok := value[p].(time.Duration); ok {
			percentileTags := pe.mergeTags(tags, map[string]string{"quantile": pe.percentileToQuantile(p)})
			pe.writeMetricLine(output, fullName, percentileTags, val.Seconds())
		}
	}

	return nil
}

// writeMetricLine writes a single metric line
func (pe *PrometheusExporter) writeMetricLine(output *strings.Builder, name string, tags map[string]string, value float64) {
	output.WriteString(name)

	// Write labels
	if len(tags) > 0 || len(pe.labels) > 0 {
		output.WriteString("{")
		pe.writeLabels(output, pe.mergeTags(pe.labels, tags))
		output.WriteString("}")
	}

	// Write value
	output.WriteString(" ")
	output.WriteString(pe.formatFloat(value))

	// Write timestamp (optional)
	output.WriteString(" ")
	output.WriteString(strconv.FormatInt(time.Now().UnixMilli(), 10))

	output.WriteString("\n")
}

// writeLabels writes labels to output
func (pe *PrometheusExporter) writeLabels(output *strings.Builder, labels map[string]string) {
	if len(labels) == 0 {
		return
	}

	// Sort labels for consistent output
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	first := true
	for _, key := range keys {
		if !first {
			output.WriteString(",")
		}
		first = false

		output.WriteString(pe.sanitizeLabelName(key))
		output.WriteString("=\"")
		output.WriteString(pe.escapeLabelValue(labels[key]))
		output.WriteString("\"")
	}
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// buildFullName builds the full metric name with namespace and subsystem
func (pe *PrometheusExporter) buildFullName(baseName string) string {
	parts := []string{}

	if pe.namespace != "" {
		parts = append(parts, pe.namespace)
	}

	if pe.subsystem != "" {
		parts = append(parts, pe.subsystem)
	}

	parts = append(parts, baseName)

	return strings.Join(parts, "_")
}

// inferMetricType infers the Prometheus metric type from the value
func (pe *PrometheusExporter) inferMetricType(value interface{}) string {
	switch v := value.(type) {
	case map[string]interface{}:
		if _, ok := v["buckets"]; ok {
			return "histogram"
		}
		if _, ok := v["count"]; ok {
			return "summary"
		}
		return "gauge"
	case float64, int64, uint64:
		return "gauge"
	default:
		return "gauge"
	}
}

// generateHelpText generates help text for a metric
func (pe *PrometheusExporter) generateHelpText(baseName, metricType string) string {
	switch metricType {
	case "counter":
		return fmt.Sprintf("Total number of %s", baseName)
	case "gauge":
		return fmt.Sprintf("Current value of %s", baseName)
	case "histogram":
		return fmt.Sprintf("Histogram of %s", baseName)
	case "summary":
		return fmt.Sprintf("Summary of %s", baseName)
	default:
		return fmt.Sprintf("Metric %s", baseName)
	}
}

// mergeTags merges multiple tag maps
func (pe *PrometheusExporter) mergeTags(tagMaps ...map[string]string) map[string]string {
	result := make(map[string]string)

	for _, tags := range tagMaps {
		for k, v := range tags {
			result[k] = v
		}
	}

	return result
}

// sanitizeLabelName sanitizes a label name for Prometheus
func (pe *PrometheusExporter) sanitizeLabelName(name string) string {
	// Replace invalid characters with underscores
	result := strings.Builder{}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' {
			result.WriteRune(char)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}

// escapeLabelValue escapes a label value for Prometheus
func (pe *PrometheusExporter) escapeLabelValue(value string) string {
	// Escape special characters
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\t", "\\t")
	return value
}

// formatFloat formats a float value for Prometheus
func (pe *PrometheusExporter) formatFloat(value float64) string {
	if value != value { // NaN
		return "NaN"
	}
	if value == math.Inf(1) { // +Inf
		return "+Inf"
	}
	if value == math.Inf(-1) { // -Inf
		return "-Inf"
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// percentileToQuantile converts percentile name to quantile value
func (pe *PrometheusExporter) percentileToQuantile(percentile string) string {
	switch percentile {
	case "p50":
		return "0.5"
	case "p90":
		return "0.9"
	case "p95":
		return "0.95"
	case "p99":
		return "0.99"
	case "p999":
		return "0.999"
	default:
		return "0.5"
	}
}
