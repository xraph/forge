package exporters

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/v2/internal/shared"
)

// =============================================================================
// STATSD EXPORTER
// =============================================================================

// StatsDExporter exports metrics in StatsD format
type StatsDExporter struct {
	config *StatsDConfig
	stats  *StatsDStats
}

// StatsDConfig contains configuration for the StatsD exporter
type StatsDConfig struct {
	Prefix        string            `yaml:"prefix" json:"prefix"`
	Suffix        string            `yaml:"suffix" json:"suffix"`
	TagFormat     string            `yaml:"tag_format" json:"tag_format"` // "datadog", "influx", "graphite"
	SampleRate    float64           `yaml:"sample_rate" json:"sample_rate"`
	GlobalTags    map[string]string `yaml:"global_tags" json:"global_tags"`
	MaxPacketSize int               `yaml:"max_packet_size" json:"max_packet_size"`
	FlushInterval time.Duration     `yaml:"flush_interval" json:"flush_interval"`
	IncludeHost   bool              `yaml:"include_host" json:"include_host"`
	Hostname      string            `yaml:"hostname" json:"hostname"`
}

// StatsDStats contains statistics about the StatsD exporter
type StatsDStats struct {
	ExportsTotal     int64     `json:"exports_total"`
	LastExportTime   time.Time `json:"last_export_time"`
	LastExportSize   int       `json:"last_export_size"`
	ErrorsTotal      int64     `json:"errors_total"`
	MetricsExported  int64     `json:"metrics_exported"`
	PacketsGenerated int64     `json:"packets_generated"`
}

// StatsDMetric represents a metric in StatsD format
type StatsDMetric struct {
	Name       string
	Value      string
	Type       string
	SampleRate float64
	Tags       map[string]string
}

// DefaultStatsDConfig returns default StatsD configuration
func DefaultStatsDConfig() *StatsDConfig {
	return &StatsDConfig{
		Prefix:        "",
		Suffix:        "",
		TagFormat:     "datadog",
		SampleRate:    1.0,
		GlobalTags:    make(map[string]string),
		MaxPacketSize: 1432, // Common MTU size minus headers
		FlushInterval: time.Second,
		IncludeHost:   true,
		Hostname:      "",
	}
}

// NewStatsDExporter creates a new StatsD exporter
func NewStatsDExporter() shared.Exporter {
	return NewStatsDExporterWithConfig(DefaultStatsDConfig())
}

// NewStatsDExporterWithConfig creates a new StatsD exporter with configuration
func NewStatsDExporterWithConfig(config *StatsDConfig) shared.Exporter {
	return &StatsDExporter{
		config: config,
		stats:  &StatsDStats{},
	}
}

// =============================================================================
// EXPORTER INTERFACE IMPLEMENTATION
// =============================================================================

// Export exports metrics in StatsD format
func (se *StatsDExporter) Export(metrics map[string]interface{}) ([]byte, error) {
	se.stats.ExportsTotal++
	se.stats.LastExportTime = time.Now()

	var lines []string

	// Convert metrics to StatsD format
	for name, value := range metrics {
		statsdMetrics := se.convertMetricToStatsD(name, value)
		for _, metric := range statsdMetrics {
			line := se.formatMetric(metric)
			if line != "" {
				lines = append(lines, line)
			}
		}
		se.stats.MetricsExported++
	}

	// Join all lines
	result := strings.Join(lines, "\n")
	if len(lines) > 0 {
		result += "\n" // Add final newline
	}

	data := []byte(result)
	se.stats.LastExportSize = len(data)
	se.stats.PacketsGenerated = int64(len(lines))

	return data, nil
}

// Format returns the export format
func (se *StatsDExporter) Format() string {
	return "statsd"
}

// Stats returns exporter statistics
func (se *StatsDExporter) Stats() interface{} {
	return se.stats
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// convertMetricToStatsD converts a metric to StatsD format
func (se *StatsDExporter) convertMetricToStatsD(name string, value interface{}) []StatsDMetric {
	baseName, tags := se.parseMetricName(name)

	// Merge with global tags
	allTags := se.mergeTags(se.config.GlobalTags, tags)

	// Add hostname if enabled
	if se.config.IncludeHost {
		hostname := se.config.Hostname
		if hostname == "" {
			hostname = "localhost" // Default hostname
		}
		allTags["host"] = hostname
	}

	switch v := value.(type) {
	case float64:
		return []StatsDMetric{se.createGaugeMetric(baseName, v, allTags)}
	case int64:
		return []StatsDMetric{se.createGaugeMetric(baseName, float64(v), allTags)}
	case uint64:
		return []StatsDMetric{se.createGaugeMetric(baseName, float64(v), allTags)}
	case map[string]interface{}:
		return se.convertComplexMetric(baseName, allTags, v)
	default:
		// Convert unsupported types to gauge with string value
		if str := fmt.Sprintf("%v", v); str != "" {
			if val, err := strconv.ParseFloat(str, 64); err == nil {
				return []StatsDMetric{se.createGaugeMetric(baseName, val, allTags)}
			}
		}
		return []StatsDMetric{}
	}
}

// createGaugeMetric creates a gauge metric
func (se *StatsDExporter) createGaugeMetric(name string, value float64, tags map[string]string) StatsDMetric {
	return StatsDMetric{
		Name:       se.buildMetricName(name),
		Value:      se.formatFloat(value),
		Type:       "g",
		SampleRate: se.config.SampleRate,
		Tags:       tags,
	}
}

// createCounterMetric creates a counter metric
func (se *StatsDExporter) createCounterMetric(name string, value float64, tags map[string]string) StatsDMetric {
	return StatsDMetric{
		Name:       se.buildMetricName(name),
		Value:      se.formatFloat(value),
		Type:       "c",
		SampleRate: se.config.SampleRate,
		Tags:       tags,
	}
}

// createHistogramMetric creates a histogram metric
func (se *StatsDExporter) createHistogramMetric(name string, value float64, tags map[string]string) StatsDMetric {
	return StatsDMetric{
		Name:       se.buildMetricName(name),
		Value:      se.formatFloat(value),
		Type:       "h",
		SampleRate: se.config.SampleRate,
		Tags:       tags,
	}
}

// createTimerMetric creates a timer metric
func (se *StatsDExporter) createTimerMetric(name string, value float64, tags map[string]string) StatsDMetric {
	return StatsDMetric{
		Name:       se.buildMetricName(name),
		Value:      se.formatFloat(value),
		Type:       "ms",
		SampleRate: se.config.SampleRate,
		Tags:       tags,
	}
}

// createSetMetric creates a set metric
func (se *StatsDExporter) createSetMetric(name string, value string, tags map[string]string) StatsDMetric {
	return StatsDMetric{
		Name:       se.buildMetricName(name),
		Value:      value,
		Type:       "s",
		SampleRate: se.config.SampleRate,
		Tags:       tags,
	}
}

// convertComplexMetric converts complex metrics (histogram, timer) to StatsD format
func (se *StatsDExporter) convertComplexMetric(baseName string, tags map[string]string, value map[string]interface{}) []StatsDMetric {
	var metrics []StatsDMetric

	// Handle histogram metrics
	if buckets, ok := value["buckets"].(map[float64]uint64); ok {
		metrics = append(metrics, se.convertHistogram(baseName, tags, value, buckets)...)
	} else if count, ok := value["count"].(uint64); ok {
		// Handle timer or counter metrics
		metrics = append(metrics, se.convertTimer(baseName, tags, value, count)...)
	} else {
		// Handle gauge-like metrics
		metrics = append(metrics, se.convertGaugeMetric(baseName, tags, value)...)
	}

	return metrics
}

// convertHistogram converts histogram metrics to StatsD format
func (se *StatsDExporter) convertHistogram(baseName string, tags map[string]string, value map[string]interface{}, buckets map[float64]uint64) []StatsDMetric {
	var metrics []StatsDMetric

	// Export histogram summary
	if count, ok := value["count"].(uint64); ok {
		metrics = append(metrics, se.createCounterMetric(baseName+".count", float64(count), tags))
	}

	if sum, ok := value["sum"].(float64); ok {
		metrics = append(metrics, se.createGaugeMetric(baseName+".sum", sum, tags))
	}

	if mean, ok := value["mean"].(float64); ok {
		metrics = append(metrics, se.createGaugeMetric(baseName+".mean", mean, tags))
	}

	// Export histogram buckets as individual metrics
	for bucket, count := range buckets {
		bucketName := fmt.Sprintf("%s.bucket.%s", baseName, se.formatFloat(bucket))
		metrics = append(metrics, se.createCounterMetric(bucketName, float64(count), tags))
	}

	return metrics
}

// convertTimer converts timer metrics to StatsD format
func (se *StatsDExporter) convertTimer(baseName string, tags map[string]string, value map[string]interface{}, count uint64) []StatsDMetric {
	var metrics []StatsDMetric

	// Export timer count
	metrics = append(metrics, se.createCounterMetric(baseName+".count", float64(count), tags))

	// Export timer durations
	if mean, ok := value["mean"].(time.Duration); ok {
		metrics = append(metrics, se.createTimerMetric(baseName+".mean", mean.Seconds()*1000, tags))
	}

	if min, ok := value["min"].(time.Duration); ok {
		metrics = append(metrics, se.createTimerMetric(baseName+".min", min.Seconds()*1000, tags))
	}

	if max, ok := value["max"].(time.Duration); ok {
		metrics = append(metrics, se.createTimerMetric(baseName+".max", max.Seconds()*1000, tags))
	}

	// Export percentiles
	percentiles := []string{"p50", "p95", "p99"}
	for _, p := range percentiles {
		if val, ok := value[p].(time.Duration); ok {
			metricName := fmt.Sprintf("%s.%s", baseName, p)
			metrics = append(metrics, se.createTimerMetric(metricName, val.Seconds()*1000, tags))
		}
	}

	return metrics
}

// convertGaugeMetric converts gauge-like metrics to StatsD format
func (se *StatsDExporter) convertGaugeMetric(baseName string, tags map[string]string, value map[string]interface{}) []StatsDMetric {
	var metrics []StatsDMetric

	for k, v := range value {
		metricName := fmt.Sprintf("%s.%s", baseName, k)

		switch val := v.(type) {
		case float64:
			metrics = append(metrics, se.createGaugeMetric(metricName, val, tags))
		case int64:
			metrics = append(metrics, se.createGaugeMetric(metricName, float64(val), tags))
		case uint64:
			metrics = append(metrics, se.createGaugeMetric(metricName, float64(val), tags))
		case time.Duration:
			metrics = append(metrics, se.createGaugeMetric(metricName, val.Seconds()*1000, tags))
		case bool:
			value := 0.0
			if val {
				value = 1.0
			}
			metrics = append(metrics, se.createGaugeMetric(metricName, value, tags))
		case string:
			metrics = append(metrics, se.createSetMetric(metricName, val, tags))
		default:
			if str := fmt.Sprintf("%v", val); str != "" {
				if floatVal, err := strconv.ParseFloat(str, 64); err == nil {
					metrics = append(metrics, se.createGaugeMetric(metricName, floatVal, tags))
				} else {
					metrics = append(metrics, se.createSetMetric(metricName, str, tags))
				}
			}
		}
	}

	return metrics
}

// formatMetric formats a StatsD metric
func (se *StatsDExporter) formatMetric(metric StatsDMetric) string {
	var parts []string

	// Metric name and value
	parts = append(parts, fmt.Sprintf("%s:%s|%s", metric.Name, metric.Value, metric.Type))

	// Sample rate
	if metric.SampleRate < 1.0 {
		parts = append(parts, fmt.Sprintf("@%s", se.formatFloat(metric.SampleRate)))
	}

	// Tags
	if len(metric.Tags) > 0 {
		tagStr := se.formatTags(metric.Tags)
		if tagStr != "" {
			parts = append(parts, tagStr)
		}
	}

	return strings.Join(parts, "|")
}

// formatTags formats tags according to the configured format
func (se *StatsDExporter) formatTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}

	// Sort tags for consistent output
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var parts []string

	switch se.config.TagFormat {
	case "datadog":
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s:%s", key, tags[key]))
		}
		return "#" + strings.Join(parts, ",")
	case "influx":
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", key, tags[key]))
		}
		return "#" + strings.Join(parts, ",")
	case "graphite":
		// Graphite doesn't support tags, so we encode them in the metric name
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s_%s", key, tags[key]))
		}
		return strings.Join(parts, ".")
	default:
		// Default to datadog format
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s:%s", key, tags[key]))
		}
		return "#" + strings.Join(parts, ",")
	}
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// parseMetricName parses a metric name and extracts tags
func (se *StatsDExporter) parseMetricName(fullName string) (string, map[string]string) {
	if !strings.Contains(fullName, "{") {
		return fullName, make(map[string]string)
	}

	braceIndex := strings.Index(fullName, "{")
	if braceIndex == -1 {
		return fullName, make(map[string]string)
	}

	baseName := fullName[:braceIndex]
	tagsStr := fullName[braceIndex+1:]

	if strings.HasSuffix(tagsStr, "}") {
		tagsStr = tagsStr[:len(tagsStr)-1]
	}

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

// buildMetricName builds the full metric name with prefix and suffix
func (se *StatsDExporter) buildMetricName(baseName string) string {
	parts := []string{}

	if se.config.Prefix != "" {
		parts = append(parts, se.config.Prefix)
	}

	parts = append(parts, baseName)

	if se.config.Suffix != "" {
		parts = append(parts, se.config.Suffix)
	}

	return strings.Join(parts, ".")
}

// mergeTags merges multiple tag maps
func (se *StatsDExporter) mergeTags(tagMaps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, tags := range tagMaps {
		for k, v := range tags {
			result[k] = v
		}
	}
	return result
}

// formatFloat formats a float value for StatsD
func (se *StatsDExporter) formatFloat(value float64) string {
	if value != value { // NaN
		return "0"
	}
	if math.IsInf(value, 0) {
		return "0"
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// sanitizeMetricName sanitizes metric name for StatsD
func (se *StatsDExporter) sanitizeMetricName(name string) string {
	// Replace invalid characters with underscores
	result := strings.Builder{}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '.' || char == '-' {
			result.WriteRune(char)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}

// sanitizeTagValue sanitizes tag value for StatsD
func (se *StatsDExporter) sanitizeTagValue(value string) string {
	// Remove characters that could break StatsD parsing
	value = strings.ReplaceAll(value, ":", "_")
	value = strings.ReplaceAll(value, "|", "_")
	value = strings.ReplaceAll(value, "@", "_")
	value = strings.ReplaceAll(value, "#", "_")
	value = strings.ReplaceAll(value, ",", "_")
	value = strings.ReplaceAll(value, "=", "_")
	return value
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// ExportMetricsToStatsD exports metrics to StatsD format with default configuration
func ExportMetricsToStatsD(metrics map[string]interface{}) ([]byte, error) {
	exporter := NewStatsDExporter()
	return exporter.Export(metrics)
}

// ExportMetricsToStatsDWithConfig exports metrics to StatsD format with custom configuration
func ExportMetricsToStatsDWithConfig(metrics map[string]interface{}, config *StatsDConfig) ([]byte, error) {
	exporter := NewStatsDExporterWithConfig(config)
	return exporter.Export(metrics)
}

// ValidateStatsDConfig validates StatsD configuration
func ValidateStatsDConfig(config *StatsDConfig) error {
	if config.SampleRate <= 0 || config.SampleRate > 1 {
		return fmt.Errorf("sample rate must be between 0 and 1, got %f", config.SampleRate)
	}

	validTagFormats := map[string]bool{
		"datadog":  true,
		"influx":   true,
		"graphite": true,
	}

	if !validTagFormats[config.TagFormat] {
		return fmt.Errorf("invalid tag format: %s, must be one of: datadog, influx, graphite", config.TagFormat)
	}

	if config.MaxPacketSize <= 0 {
		return fmt.Errorf("max packet size must be positive, got %d", config.MaxPacketSize)
	}

	return nil
}

// SplitIntoPackets splits StatsD metrics into packets respecting max packet size
func SplitIntoPackets(data []byte, maxPacketSize int) [][]byte {
	if len(data) <= maxPacketSize {
		return [][]byte{data}
	}

	lines := strings.Split(string(data), "\n")
	var packets [][]byte
	var currentPacket strings.Builder

	for _, line := range lines {
		if line == "" {
			continue
		}

		// Check if adding this line would exceed packet size
		if currentPacket.Len() > 0 && currentPacket.Len()+len(line)+1 > maxPacketSize {
			// Finalize current packet
			packets = append(packets, []byte(currentPacket.String()))
			currentPacket.Reset()
		}

		// Add line to current packet
		if currentPacket.Len() > 0 {
			currentPacket.WriteString("\n")
		}
		currentPacket.WriteString(line)
	}

	// Add final packet if not empty
	if currentPacket.Len() > 0 {
		packets = append(packets, []byte(currentPacket.String()))
	}

	return packets
}
