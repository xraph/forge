package exporters

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	metrics "github.com/xraph/forge/v0/pkg/metrics/core"
)

// =============================================================================
// INFLUXDB EXPORTER
// =============================================================================

// InfluxExporter exports metrics in InfluxDB line protocol format
type InfluxExporter struct {
	config *InfluxConfig
	stats  *InfluxStats
}

// InfluxConfig contains configuration for the InfluxDB exporter
type InfluxConfig struct {
	Database        string            `yaml:"database" json:"database"`
	RetentionPolicy string            `yaml:"retention_policy" json:"retention_policy"`
	Precision       string            `yaml:"precision" json:"precision"`
	GlobalTags      map[string]string `yaml:"global_tags" json:"global_tags"`
	FieldMapping    map[string]string `yaml:"field_mapping" json:"field_mapping"`
	TagMapping      map[string]string `yaml:"tag_mapping" json:"tag_mapping"`
}

// InfluxStats contains statistics about the InfluxDB exporter
type InfluxStats struct {
	ExportsTotal    int64     `json:"exports_total"`
	LastExportTime  time.Time `json:"last_export_time"`
	LastExportSize  int       `json:"last_export_size"`
	ErrorsTotal     int64     `json:"errors_total"`
	MetricsExported int64     `json:"metrics_exported"`
	LinesGenerated  int64     `json:"lines_generated"`
}

// InfluxPoint represents a single InfluxDB point
type InfluxPoint struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]interface{}
	Timestamp   time.Time
}

// DefaultInfluxConfig returns default InfluxDB configuration
func DefaultInfluxConfig() *InfluxConfig {
	return &InfluxConfig{
		Database:        "metrics",
		RetentionPolicy: "autogen",
		Precision:       "ns",
		GlobalTags:      make(map[string]string),
		FieldMapping:    make(map[string]string),
		TagMapping:      make(map[string]string),
	}
}

// NewInfluxExporter creates a new InfluxDB exporter
func NewInfluxExporter() metrics.Exporter {
	return NewInfluxExporterWithConfig(DefaultInfluxConfig())
}

// NewInfluxExporterWithConfig creates a new InfluxDB exporter with configuration
func NewInfluxExporterWithConfig(config *InfluxConfig) metrics.Exporter {
	return &InfluxExporter{
		config: config,
		stats:  &InfluxStats{},
	}
}

// =============================================================================
// EXPORTER INTERFACE IMPLEMENTATION
// =============================================================================

// Export exports metrics in InfluxDB line protocol format
func (ie *InfluxExporter) Export(metrics map[string]interface{}) ([]byte, error) {
	ie.stats.ExportsTotal++
	ie.stats.LastExportTime = time.Now()

	var lines []string
	timestamp := time.Now()

	// Convert metrics to InfluxDB points
	for name, value := range metrics {
		points := ie.convertMetricToPoints(name, value, timestamp)
		for _, point := range points {
			line := ie.formatPoint(point)
			if line != "" {
				lines = append(lines, line)
				ie.stats.LinesGenerated++
			}
		}
		ie.stats.MetricsExported++
	}

	// Join all lines
	result := strings.Join(lines, "\n")
	if len(lines) > 0 {
		result += "\n" // Add final newline
	}

	data := []byte(result)
	ie.stats.LastExportSize = len(data)

	return data, nil
}

// Format returns the export format
func (ie *InfluxExporter) Format() string {
	return "influx"
}

// Stats returns exporter statistics
func (ie *InfluxExporter) Stats() interface{} {
	return ie.stats
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// convertMetricToPoints converts a metric to InfluxDB points
func (ie *InfluxExporter) convertMetricToPoints(name string, value interface{}, timestamp time.Time) []InfluxPoint {
	baseName, tags := ie.parseMetricName(name)

	// Merge with global tags
	allTags := ie.mergeTags(ie.config.GlobalTags, tags)

	// Apply tag mapping
	mappedTags := ie.applyTagMapping(allTags)

	switch v := value.(type) {
	case float64:
		return []InfluxPoint{ie.createSimplePoint(baseName, mappedTags, v, timestamp)}
	case int64:
		return []InfluxPoint{ie.createSimplePoint(baseName, mappedTags, v, timestamp)}
	case uint64:
		return []InfluxPoint{ie.createSimplePoint(baseName, mappedTags, v, timestamp)}
	case map[string]interface{}:
		return ie.convertComplexMetric(baseName, mappedTags, v, timestamp)
	default:
		// Convert unsupported types to string
		return []InfluxPoint{ie.createSimplePoint(baseName, mappedTags, fmt.Sprintf("%v", v), timestamp)}
	}
}

// createSimplePoint creates a simple InfluxDB point
func (ie *InfluxExporter) createSimplePoint(measurement string, tags map[string]string, value interface{}, timestamp time.Time) InfluxPoint {
	fields := map[string]interface{}{
		"value": value,
	}

	// Apply field mapping
	if mapped, exists := ie.config.FieldMapping[measurement]; exists {
		fields = map[string]interface{}{
			mapped: value,
		}
	}

	return InfluxPoint{
		Measurement: measurement,
		Tags:        tags,
		Fields:      fields,
		Timestamp:   timestamp,
	}
}

// convertComplexMetric converts complex metrics (histogram, timer) to InfluxDB points
func (ie *InfluxExporter) convertComplexMetric(baseName string, tags map[string]string, value map[string]interface{}, timestamp time.Time) []InfluxPoint {
	var points []InfluxPoint

	// Handle histogram metrics
	if buckets, ok := value["buckets"].(map[float64]uint64); ok {
		points = append(points, ie.convertHistogram(baseName, tags, value, buckets, timestamp)...)
	} else if count, ok := value["count"].(uint64); ok {
		// Handle timer or counter metrics
		points = append(points, ie.convertTimer(baseName, tags, value, count, timestamp)...)
	} else {
		// Handle gauge-like metrics
		points = append(points, ie.convertGaugeMetric(baseName, tags, value, timestamp)...)
	}

	return points
}

// convertHistogram converts histogram metrics to InfluxDB points
func (ie *InfluxExporter) convertHistogram(baseName string, tags map[string]string, value map[string]interface{}, buckets map[float64]uint64, timestamp time.Time) []InfluxPoint {
	var points []InfluxPoint

	// Create histogram summary point
	fields := make(map[string]interface{})

	if count, ok := value["count"].(uint64); ok {
		fields["count"] = count
	}

	if sum, ok := value["sum"].(float64); ok {
		fields["sum"] = sum
	}

	if mean, ok := value["mean"].(float64); ok {
		fields["mean"] = mean
	}

	if len(fields) > 0 {
		points = append(points, InfluxPoint{
			Measurement: baseName,
			Tags:        tags,
			Fields:      fields,
			Timestamp:   timestamp,
		})
	}

	// Create bucket points
	for bucket, count := range buckets {
		bucketTags := ie.mergeTags(tags, map[string]string{
			"le": ie.formatFloat(bucket),
		})

		points = append(points, InfluxPoint{
			Measurement: baseName + "_bucket",
			Tags:        bucketTags,
			Fields: map[string]interface{}{
				"count": count,
			},
			Timestamp: timestamp,
		})
	}

	return points
}

// convertTimer converts timer metrics to InfluxDB points
func (ie *InfluxExporter) convertTimer(baseName string, tags map[string]string, value map[string]interface{}, count uint64, timestamp time.Time) []InfluxPoint {
	var points []InfluxPoint

	// Create timer summary point
	fields := map[string]interface{}{
		"count": count,
	}

	// Add duration fields
	if mean, ok := value["mean"].(time.Duration); ok {
		fields["mean"] = mean.Nanoseconds()
		fields["mean_seconds"] = mean.Seconds()
	}

	if min, ok := value["min"].(time.Duration); ok {
		fields["min"] = min.Nanoseconds()
		fields["min_seconds"] = min.Seconds()
	}

	if max, ok := value["max"].(time.Duration); ok {
		fields["max"] = max.Nanoseconds()
		fields["max_seconds"] = max.Seconds()
	}

	// Add percentiles
	percentiles := map[string]string{
		"p50": "0.5",
		"p95": "0.95",
		"p99": "0.99",
	}

	for key, _ := range percentiles {
		if val, ok := value[key].(time.Duration); ok {
			fields[fmt.Sprintf("p%s", strings.TrimPrefix(key, "p"))] = val.Nanoseconds()
			fields[fmt.Sprintf("p%s_seconds", strings.TrimPrefix(key, "p"))] = val.Seconds()
		}
	}

	points = append(points, InfluxPoint{
		Measurement: baseName,
		Tags:        tags,
		Fields:      fields,
		Timestamp:   timestamp,
	})

	return points
}

// convertGaugeMetric converts gauge-like metrics to InfluxDB points
func (ie *InfluxExporter) convertGaugeMetric(baseName string, tags map[string]string, value map[string]interface{}, timestamp time.Time) []InfluxPoint {
	var points []InfluxPoint

	// Convert all fields in the map
	fields := make(map[string]interface{})

	for k, v := range value {
		switch val := v.(type) {
		case float64, int64, uint64, int, uint, string, bool:
			fields[k] = val
		case time.Duration:
			fields[k] = val.Nanoseconds()
			fields[k+"_seconds"] = val.Seconds()
		default:
			fields[k] = fmt.Sprintf("%v", val)
		}
	}

	if len(fields) > 0 {
		points = append(points, InfluxPoint{
			Measurement: baseName,
			Tags:        tags,
			Fields:      fields,
			Timestamp:   timestamp,
		})
	}

	return points
}

// formatPoint formats an InfluxDB point to line protocol
func (ie *InfluxExporter) formatPoint(point InfluxPoint) string {
	if len(point.Fields) == 0 {
		return ""
	}

	var line strings.Builder

	// Write measurement name
	line.WriteString(ie.escapeMeasurement(point.Measurement))

	// Write tags
	if len(point.Tags) > 0 {
		line.WriteString(",")
		ie.writeTags(&line, point.Tags)
	}

	// Write separator
	line.WriteString(" ")

	// Write fields
	ie.writeFields(&line, point.Fields)

	// Write timestamp
	line.WriteString(" ")
	line.WriteString(ie.formatTimestamp(point.Timestamp))

	return line.String()
}

// writeTags writes tags to the line protocol
func (ie *InfluxExporter) writeTags(line *strings.Builder, tags map[string]string) {
	// Sort tags for consistent output
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	first := true
	for _, key := range keys {
		if !first {
			line.WriteString(",")
		}
		first = false

		line.WriteString(ie.escapeTagKey(key))
		line.WriteString("=")
		line.WriteString(ie.escapeTagValue(tags[key]))
	}
}

// writeFields writes fields to the line protocol
func (ie *InfluxExporter) writeFields(line *strings.Builder, fields map[string]interface{}) {
	// Sort fields for consistent output
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	first := true
	for _, key := range keys {
		if !first {
			line.WriteString(",")
		}
		first = false

		line.WriteString(ie.escapeFieldKey(key))
		line.WriteString("=")
		line.WriteString(ie.formatFieldValue(fields[key]))
	}
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// parseMetricName parses a metric name and extracts tags
func (ie *InfluxExporter) parseMetricName(fullName string) (string, map[string]string) {
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

// mergeTags merges multiple tag maps
func (ie *InfluxExporter) mergeTags(tagMaps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, tags := range tagMaps {
		for k, v := range tags {
			result[k] = v
		}
	}
	return result
}

// applyTagMapping applies tag mapping configuration
func (ie *InfluxExporter) applyTagMapping(tags map[string]string) map[string]string {
	if len(ie.config.TagMapping) == 0 {
		return tags
	}

	result := make(map[string]string)
	for k, v := range tags {
		if mapped, exists := ie.config.TagMapping[k]; exists {
			result[mapped] = v
		} else {
			result[k] = v
		}
	}
	return result
}

// formatTimestamp formats timestamp according to precision
func (ie *InfluxExporter) formatTimestamp(t time.Time) string {
	switch ie.config.Precision {
	case "s":
		return strconv.FormatInt(t.Unix(), 10)
	case "ms":
		return strconv.FormatInt(t.UnixMilli(), 10)
	case "us":
		return strconv.FormatInt(t.UnixMicro(), 10)
	case "ns":
		return strconv.FormatInt(t.UnixNano(), 10)
	default:
		return strconv.FormatInt(t.UnixNano(), 10)
	}
}

// formatFieldValue formats a field value for InfluxDB
func (ie *InfluxExporter) formatFieldValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return `"` + ie.escapeStringValue(v) + `"`
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.FormatInt(int64(v), 10) + "i"
	case int64:
		return strconv.FormatInt(v, 10) + "i"
	case uint64:
		return strconv.FormatUint(v, 10) + "u"
	case float64:
		return ie.formatFloat(v)
	case float32:
		return ie.formatFloat(float64(v))
	default:
		return `"` + ie.escapeStringValue(fmt.Sprintf("%v", v)) + `"`
	}
}

// formatFloat formats a float value for InfluxDB
func (ie *InfluxExporter) formatFloat(value float64) string {
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

// escapeMeasurement escapes measurement name for InfluxDB
func (ie *InfluxExporter) escapeMeasurement(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, ",", "\\,"), " ", "\\ ")
}

// escapeTagKey escapes tag key for InfluxDB
func (ie *InfluxExporter) escapeTagKey(key string) string {
	key = strings.ReplaceAll(key, ",", "\\,")
	key = strings.ReplaceAll(key, "=", "\\=")
	key = strings.ReplaceAll(key, " ", "\\ ")
	return key
}

// escapeTagValue escapes tag value for InfluxDB
func (ie *InfluxExporter) escapeTagValue(value string) string {
	value = strings.ReplaceAll(value, ",", "\\,")
	value = strings.ReplaceAll(value, "=", "\\=")
	value = strings.ReplaceAll(value, " ", "\\ ")
	return value
}

// escapeFieldKey escapes field key for InfluxDB
func (ie *InfluxExporter) escapeFieldKey(key string) string {
	key = strings.ReplaceAll(key, ",", "\\,")
	key = strings.ReplaceAll(key, "=", "\\=")
	key = strings.ReplaceAll(key, " ", "\\ ")
	return key
}

// escapeStringValue escapes string field value for InfluxDB
func (ie *InfluxExporter) escapeStringValue(value string) string {
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, `\`, `\\`)
	return value
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// ExportMetricsToInflux exports metrics to InfluxDB format with default configuration
func ExportMetricsToInflux(metrics map[string]interface{}) ([]byte, error) {
	exporter := NewInfluxExporter()
	return exporter.Export(metrics)
}

// ExportMetricsToInfluxWithConfig exports metrics to InfluxDB format with custom configuration
func ExportMetricsToInfluxWithConfig(metrics map[string]interface{}, config *InfluxConfig) ([]byte, error) {
	exporter := NewInfluxExporterWithConfig(config)
	return exporter.Export(metrics)
}

// CreateInfluxPoint creates a new InfluxDB point
func CreateInfluxPoint(measurement string, tags map[string]string, fields map[string]interface{}, timestamp time.Time) InfluxPoint {
	return InfluxPoint{
		Measurement: measurement,
		Tags:        tags,
		Fields:      fields,
		Timestamp:   timestamp,
	}
}

// ValidateInfluxConfig validates InfluxDB configuration
func ValidateInfluxConfig(config *InfluxConfig) error {
	if config.Database == "" {
		return fmt.Errorf("database name is required")
	}

	validPrecisions := map[string]bool{
		"ns": true,
		"us": true,
		"ms": true,
		"s":  true,
	}

	if !validPrecisions[config.Precision] {
		return fmt.Errorf("invalid precision: %s, must be one of: ns, us, ms, s", config.Precision)
	}

	return nil
}
