package examples

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	metrics "github.com/xraph/forge/internal/metrics/internal"
)

// ProductionMetricsExample demonstrates production-grade label usage
type ProductionMetricsExample struct {
	common      *metrics.CommonLabels
	registry    *metrics.LabelRegistry
	cardinality *metrics.LabelCardinality
	pathCache   map[string]string // Cache normalized paths
}

// NewProductionMetricsExample creates a new example instance
func NewProductionMetricsExample() *ProductionMetricsExample {
	hostname, _ := os.Hostname()

	return &ProductionMetricsExample{
		common: &metrics.CommonLabels{
			Service:     "api-gateway",
			Environment: getEnv("ENVIRONMENT", "development"),
			Version:     getEnv("VERSION", "1.0.0"),
			Instance:    hostname,
			Region:      getEnv("AWS_REGION", "us-east-1"),
			Zone:        getEnv("AWS_AZ", "us-east-1a"),
		},
		registry:    metrics.NewLabelRegistry(),
		cardinality: metrics.NewLabelCardinality(10000),
		pathCache:   make(map[string]string),
	}
}

// RecordHTTPRequest demonstrates proper HTTP metric labeling
func (p *ProductionMetricsExample) RecordHTTPRequest(
	method, path string,
	statusCode int,
	duration time.Duration,
) {
	// Normalize path to prevent cardinality explosion
	normalizedPath := p.normalizePath(path)

	// Create bounded labels
	labels := map[string]string{
		"method": method,
		"path":   normalizedPath,
		"status": statusCodeCategory(statusCode),
	}

	// Validate and sanitize
	sanitized, err := metrics.ValidateAndSanitizeTags(labels)
	if err != nil {
		fmt.Printf("Label validation failed: %v\n", err)
		return
	}

	// Check cardinality before creating metric
	metricName := "http_requests_total"
	if !p.cardinality.Check(metricName, sanitized) {
		fmt.Printf("Warning: Creating metric would exceed cardinality limit\n")
		// Fallback: use aggregated labels
		sanitized = map[string]string{
			"method": method,
			"status": statusCodeCategory(statusCode),
		}
	}

	// Record cardinality
	if err := p.cardinality.Record(metricName, sanitized); err != nil {
		fmt.Printf("Cardinality error: %v\n", err)
	}

	// Record label usage for monitoring
	for key, value := range sanitized {
		p.registry.RecordLabel(key, value)
	}

	// Add common labels
	finalLabels := metrics.AddCommonLabels(sanitized, p.common)

	// Log formatted labels for debugging
	fmt.Printf("Metric: %s %s\n", metricName,
		metrics.FormatLabelsPrometheus(finalLabels))

	// In real usage, you would increment your actual metric here
	// counter := collector.Counter(metricName, flattenLabels(finalLabels)...)
	// counter.Inc()
}

// RecordDatabaseQuery demonstrates database metric labeling
func (p *ProductionMetricsExample) RecordDatabaseQuery(
	operation, table, status string,
	duration time.Duration,
) {
	labels := map[string]string{
		"operation": operation, // SELECT, INSERT, UPDATE, DELETE
		"table":     table,
		"status":    status, // success, error, timeout
	}

	// Validate
	sanitized, err := metrics.ValidateAndSanitizeTags(labels)
	if err != nil {
		fmt.Printf("Invalid DB labels: %v\n", err)
		return
	}

	// Add common labels
	finalLabels := metrics.AddCommonLabels(sanitized, p.common)

	// Record for multiple formats
	fmt.Printf("Prometheus: db_query_duration_seconds%s\n",
		metrics.FormatLabelsPrometheus(finalLabels))
	fmt.Printf("InfluxDB: db_query_duration_seconds,%s value=%f\n",
		metrics.FormatLabelsInflux(finalLabels), duration.Seconds())
}

// RecordCacheOperation demonstrates cache metric labeling
func (p *ProductionMetricsExample) RecordCacheOperation(operation, status string) {
	labels := map[string]string{
		"operation": operation, // get, set, delete, expire
		"status":    status,    // hit, miss, error
	}

	sanitized, _ := metrics.ValidateAndSanitizeTags(labels)
	finalLabels := metrics.AddCommonLabels(sanitized, p.common)

	fmt.Printf("Cache metric: cache_operations_total%s\n",
		metrics.FormatLabelsPrometheus(finalLabels))
}

// GetCardinalityStats returns current cardinality statistics
func (p *ProductionMetricsExample) GetCardinalityStats() {
	fmt.Printf("\n=== Cardinality Statistics ===\n")
	fmt.Printf("Total label combinations: %d\n", p.cardinality.GetCardinality())
	fmt.Printf("Limit: %d\n", metrics.MaxLabelCardinality)

	// Get high cardinality labels
	highCard := p.registry.GetHighCardinalityLabels()
	if len(highCard) > 0 {
		fmt.Printf("\nHigh Cardinality Labels:\n")
		for _, label := range highCard {
			fmt.Printf("  - %s\n", label)
		}
	}

	// Get detailed stats
	fmt.Printf("\n=== Label Usage Statistics ===\n")
	stats := p.registry.GetLabelStats()

	// Sort by value count
	type labelStat struct {
		key   string
		count int
	}
	var sortedStats []labelStat
	for key, meta := range stats {
		sortedStats = append(sortedStats, labelStat{key, meta.ValueCount})
	}
	sort.Slice(sortedStats, func(i, j int) bool {
		return sortedStats[i].count > sortedStats[j].count
	})

	for _, stat := range sortedStats {
		meta := stats[stat.key]
		fmt.Printf("Label: %-20s Values: %-6d High-Card: %-5v Samples: %v\n",
			stat.key, meta.ValueCount, meta.IsHighCardinality, meta.SampleValues)
	}
}

// DemonstrateValidation shows label validation examples
func (p *ProductionMetricsExample) DemonstrateValidation() {
	fmt.Printf("\n=== Label Validation Examples ===\n")

	testCases := []struct {
		name   string
		labels map[string]string
	}{
		{
			name: "Valid labels",
			labels: map[string]string{
				"service":     "api",
				"environment": "production",
				"region":      "us-east-1",
			},
		},
		{
			name: "Invalid - starts with digit",
			labels: map[string]string{
				"123abc": "value",
			},
		},
		{
			name: "Invalid - reserved label",
			labels: map[string]string{
				"__name__": "my_metric",
			},
		},
		{
			name: "Invalid - special characters",
			labels: map[string]string{
				"label-with-dash": "value",
			},
		},
		{
			name: "Too many labels",
			labels: func() map[string]string {
				m := make(map[string]string)
				for i := 0; i < 25; i++ {
					m[fmt.Sprintf("label_%d", i)] = fmt.Sprintf("value_%d", i)
				}
				return m
			}(),
		},
	}

	for _, tc := range testCases {
		fmt.Printf("\nTest: %s\n", tc.name)
		sanitized, err := metrics.ValidateAndSanitizeTags(tc.labels)
		if err != nil {
			fmt.Printf("  ❌ Validation error: %v\n", err)
		} else {
			fmt.Printf("  ✅ Valid labels: %v\n", sanitized)
		}
	}
}

// DemonstrateFormatting shows format-specific label output
func (p *ProductionMetricsExample) DemonstrateFormatting() {
	fmt.Printf("\n=== Format-Specific Label Examples ===\n")

	labels := map[string]string{
		"method":      "GET",
		"path":        "/api/users",
		"status":      "200",
		"service":     "api-gateway",
		"environment": "production",
	}

	fmt.Printf("\nPrometheus Format:\n")
	fmt.Printf("  http_requests_total%s 42\n",
		metrics.FormatLabelsPrometheus(labels))

	fmt.Printf("\nInfluxDB Format:\n")
	fmt.Printf("  http_requests_total,%s value=42\n",
		metrics.FormatLabelsInflux(labels))

	fmt.Printf("\nJSON Format:\n")
	jsonLabels := metrics.FormatLabelsJSON(labels)
	fmt.Printf("  %+v\n", jsonLabels)

	// Demonstrate sanitization
	problematicLabels := map[string]string{
		"user:email": "user@example.com",
		"query|data": "select * from users",
		"path space": "value with spaces",
	}

	fmt.Printf("\nSanitized for StatsD:\n")
	for key, value := range problematicLabels {
		sKey, sValue := metrics.SanitizeLabelForStatsD(key, value)
		fmt.Printf("  %s=%s -> %s=%s\n", key, value, sKey, sValue)
	}
}

// Helper functions

func (p *ProductionMetricsExample) normalizePath(path string) string {
	// Check cache first
	if cached, ok := p.pathCache[path]; ok {
		return cached
	}

	// Normalize common patterns
	normalized := path

	// UUID pattern: /api/users/123e4567-e89b-12d3-a456-426614174000
	uuidRegex := regexp.MustCompile(`/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	normalized = uuidRegex.ReplaceAllString(normalized, "/{uuid}")

	// Numeric ID pattern: /api/users/123
	numericRegex := regexp.MustCompile(`/\d+`)
	normalized = numericRegex.ReplaceAllString(normalized, "/{id}")

	// Cache result
	if len(p.pathCache) < 1000 { // Limit cache size
		p.pathCache[path] = normalized
	}

	return normalized
}

func statusCodeCategory(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "unknown"
	}
}

func flattenLabels(labels map[string]string) []string {
	result := make([]string, 0, len(labels)*2)
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		result = append(result, k, labels[k])
	}
	return result
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// RunExample runs all demonstration functions
func RunExample() {
	example := NewProductionMetricsExample()

	fmt.Println("=== Production-Grade Metrics Labels Example ===\n")

	// Simulate HTTP requests
	fmt.Println("Recording HTTP requests...")
	example.RecordHTTPRequest("GET", "/api/users/123", 200, 50*time.Millisecond)
	example.RecordHTTPRequest("POST", "/api/users", 201, 100*time.Millisecond)
	example.RecordHTTPRequest("GET", "/api/users/456", 200, 45*time.Millisecond)
	example.RecordHTTPRequest("DELETE", "/api/users/789", 204, 30*time.Millisecond)
	example.RecordHTTPRequest("GET", "/api/orders/abc-def-123", 404, 10*time.Millisecond)

	// Simulate database queries
	fmt.Println("\nRecording database queries...")
	example.RecordDatabaseQuery("SELECT", "users", "success", 15*time.Millisecond)
	example.RecordDatabaseQuery("INSERT", "orders", "success", 25*time.Millisecond)
	example.RecordDatabaseQuery("UPDATE", "users", "error", 50*time.Millisecond)

	// Simulate cache operations
	fmt.Println("\nRecording cache operations...")
	example.RecordCacheOperation("get", "hit")
	example.RecordCacheOperation("get", "miss")
	example.RecordCacheOperation("set", "success")

	// Show statistics
	example.GetCardinalityStats()

	// Demonstrate validation
	example.DemonstrateValidation()

	// Demonstrate formatting
	example.DemonstrateFormatting()
}
