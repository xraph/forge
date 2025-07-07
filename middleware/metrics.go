package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/router"
)

// MetricsMiddleware provides request metrics collection
type MetricsMiddleware struct {
	*BaseMiddleware
	config  MetricsConfig
	metrics observability.Metrics
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	// Metric names
	RequestsCounterName string `json:"requests_counter_name"`
	RequestDurationName string `json:"request_duration_name"`
	RequestSizeName     string `json:"request_size_name"`
	ResponseSizeName    string `json:"response_size_name"`
	ActiveRequestsName  string `json:"active_requests_name"`

	// Labels
	IncludeMethod     bool     `json:"include_method"`
	IncludePath       bool     `json:"include_path"`
	IncludeStatus     bool     `json:"include_status"`
	IncludeUserAgent  bool     `json:"include_user_agent"`
	IncludeRemoteAddr bool     `json:"include_remote_addr"`
	CustomLabels      []string `json:"custom_labels"`

	// Path normalization
	NormalizePaths       bool              `json:"normalize_paths"`
	PathNormalizers      map[string]string `json:"path_normalizers"`
	PathRegexNormalizers map[string]string `json:"path_regex_normalizers"`

	// Skip patterns
	SkipPaths       []string `json:"skip_paths"`
	SkipMethods     []string `json:"skip_methods"`
	SkipStatusCodes []int    `json:"skip_status_codes"`

	// Histogram buckets
	DurationBuckets []float64 `json:"duration_buckets"`
	SizeBuckets     []float64 `json:"size_buckets"`

	// Sampling
	SampleRate float64 `json:"sample_rate"`

	// Custom metric extractors
	CustomMetrics []CustomMetricExtractor `json:"-"`

	// Performance
	UseGoroutine bool `json:"use_goroutine"`
	BufferSize   int  `json:"buffer_size"`
}

// CustomMetricExtractor extracts custom metrics from requests
type CustomMetricExtractor interface {
	ExtractMetrics(r *http.Request, responseInfo *router.ResponseInfo) map[string]interface{}
	MetricNames() []string
}

// NewMetricsMiddleware creates a new metrics middleware
func NewMetricsMiddleware(metrics observability.Metrics, config ...MetricsConfig) Middleware {
	cfg := DefaultMetricsConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	return &MetricsMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"metrics",
			PriorityMetrics,
			"Request metrics collection middleware",
		),
		config:  cfg,
		metrics: metrics,
	}
}

// DefaultMetricsConfig returns default metrics configuration
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		RequestsCounterName:  "http_requests_total",
		RequestDurationName:  "http_request_duration_seconds",
		RequestSizeName:      "http_request_size_bytes",
		ResponseSizeName:     "http_response_size_bytes",
		ActiveRequestsName:   "http_requests_active",
		IncludeMethod:        true,
		IncludePath:          true,
		IncludeStatus:        true,
		IncludeUserAgent:     false,
		IncludeRemoteAddr:    false,
		CustomLabels:         []string{},
		NormalizePaths:       true,
		PathNormalizers:      make(map[string]string),
		PathRegexNormalizers: make(map[string]string),
		SkipPaths:            []string{"/metrics", "/health"},
		SkipMethods:          []string{},
		SkipStatusCodes:      []int{},
		DurationBuckets:      []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		SizeBuckets:          []float64{1024, 4096, 16384, 65536, 262144, 1048576, 4194304},
		SampleRate:           1.0,
		CustomMetrics:        []CustomMetricExtractor{},
		UseGoroutine:         false,
		BufferSize:           1000,
	}
}

// ProductionMetricsConfig returns production-optimized metrics configuration
func ProductionMetricsConfig() MetricsConfig {
	config := DefaultMetricsConfig()
	config.NormalizePaths = true
	config.IncludeUserAgent = false
	config.IncludeRemoteAddr = false
	config.SampleRate = 1.0
	config.UseGoroutine = true
	return config
}

// DevelopmentMetricsConfig returns development-optimized metrics configuration
func DevelopmentMetricsConfig() MetricsConfig {
	config := DefaultMetricsConfig()
	config.IncludeUserAgent = true
	config.IncludeRemoteAddr = true
	config.SampleRate = 1.0
	config.UseGoroutine = false
	return config
}

// Handle implements the Middleware interface
func (mm *MetricsMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip metrics for certain patterns
		if mm.shouldSkipMetrics(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Sample if configured
		if mm.config.SampleRate < 1.0 && !mm.shouldSample() {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		// Increment active requests
		mm.incrementActiveRequests(r)
		defer mm.decrementActiveRequests(r)

		// Record request size
		if r.ContentLength > 0 {
			mm.recordRequestSize(r, float64(r.ContentLength))
		}

		// Wrap response writer to capture response data
		wrapped := &metricsResponseWriter{
			ResponseWriter: w,
			statusCode:     200,
		}

		// Execute handler
		next.ServeHTTP(wrapped, r)

		// Calculate duration
		duration := time.Since(start)

		// Create response info
		responseInfo := &router.ResponseInfo{
			StatusCode: wrapped.statusCode,
			Size:       wrapped.size,
			Duration:   duration,
			Headers:    wrapped.Header(),
		}

		// Record metrics
		if mm.config.UseGoroutine {
			go mm.recordMetrics(r, responseInfo)
		} else {
			mm.recordMetrics(r, responseInfo)
		}
	})
}

// shouldSkipMetrics checks if metrics should be skipped
func (mm *MetricsMiddleware) shouldSkipMetrics(r *http.Request) bool {
	// Check skip paths
	for _, skipPath := range mm.config.SkipPaths {
		if strings.HasPrefix(r.URL.Path, skipPath) {
			return true
		}
	}

	// Check skip methods
	for _, skipMethod := range mm.config.SkipMethods {
		if r.Method == skipMethod {
			return true
		}
	}

	return false
}

// shouldSample determines if this request should be sampled
func (mm *MetricsMiddleware) shouldSample() bool {
	// Simple sampling based on sample rate
	// In production, you might use more sophisticated sampling
	return true // Simplified for this implementation
}

// recordMetrics records all metrics for the request
func (mm *MetricsMiddleware) recordMetrics(r *http.Request, responseInfo *router.ResponseInfo) {
	// Skip if status code should be skipped
	for _, skipStatus := range mm.config.SkipStatusCodes {
		if responseInfo.StatusCode == skipStatus {
			return
		}
	}

	// Prepare labels
	labels := mm.buildLabels(r, responseInfo)

	// Record request count
	mm.metrics.IncrementCounter(mm.config.RequestsCounterName, 1, labels...)

	// Record request duration
	mm.metrics.ObserveHistogram(mm.config.RequestDurationName, responseInfo.Duration.Seconds(), labels...)

	// Record response size
	if responseInfo.Size > 0 {
		mm.metrics.ObserveHistogram(mm.config.ResponseSizeName, float64(responseInfo.Size), labels...)
	}

	// Record custom metrics
	for _, extractor := range mm.config.CustomMetrics {
		customMetrics := extractor.ExtractMetrics(r, responseInfo)
		for metricName, value := range customMetrics {
			switch v := value.(type) {
			case float64:
				mm.metrics.SetGauge(metricName, v, labels...)
			case int64:
				mm.metrics.IncrementCounter(metricName, float64(v), labels...)
			}
		}
	}
}

// buildLabels builds metric labels from request and response
func (mm *MetricsMiddleware) buildLabels(r *http.Request, responseInfo *router.ResponseInfo) []observability.Label {
	var labels []observability.Label

	// Method label
	if mm.config.IncludeMethod {
		labels = append(labels, observability.Label{
			Name:  "method",
			Value: r.Method,
		})
	}

	// Path label
	if mm.config.IncludePath {
		path := mm.normalizePath(r.URL.Path)
		labels = append(labels, observability.Label{
			Name:  "path",
			Value: path,
		})
	}

	// Status label
	if mm.config.IncludeStatus {
		labels = append(labels, observability.Label{
			Name:  "status",
			Value: strconv.Itoa(responseInfo.StatusCode),
		})
	}

	// User agent label
	if mm.config.IncludeUserAgent {
		userAgent := r.UserAgent()
		if len(userAgent) > 50 {
			userAgent = userAgent[:50] // Truncate to avoid label explosion
		}
		labels = append(labels, observability.Label{
			Name:  "user_agent",
			Value: userAgent,
		})
	}

	// Remote address label
	if mm.config.IncludeRemoteAddr {
		labels = append(labels, observability.Label{
			Name:  "remote_addr",
			Value: mm.normalizeIP(r.RemoteAddr),
		})
	}

	// Custom labels
	for _, labelName := range mm.config.CustomLabels {
		if value := r.Header.Get(labelName); value != "" {
			labels = append(labels, observability.Label{
				Name:  strings.ToLower(labelName),
				Value: value,
			})
		}
	}

	return labels
}

// normalizePath normalizes URL paths for metrics
func (mm *MetricsMiddleware) normalizePath(path string) string {
	if !mm.config.NormalizePaths {
		return path
	}

	// Check exact matches first
	if normalized, exists := mm.config.PathNormalizers[path]; exists {
		return normalized
	}

	// Check regex normalizers
	// This is simplified - in practice you'd compile regexes once
	for pattern, replacement := range mm.config.PathRegexNormalizers {
		// Simple pattern matching - in production use compiled regexes
		if strings.Contains(path, pattern) {
			return replacement
		}
	}

	// Default path normalization
	return mm.defaultPathNormalization(path)
}

// defaultPathNormalization applies default path normalization rules
func (mm *MetricsMiddleware) defaultPathNormalization(path string) string {
	// Remove query parameters
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}

	// Replace numeric IDs with placeholder
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if mm.isNumericID(part) {
			parts[i] = "{id}"
		} else if mm.isUUID(part) {
			parts[i] = "{uuid}"
		}
	}

	return strings.Join(parts, "/")
}

// isNumericID checks if a string is a numeric ID
func (mm *MetricsMiddleware) isNumericID(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isUUID checks if a string looks like a UUID
func (mm *MetricsMiddleware) isUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// normalizeIP normalizes IP addresses for metrics
func (mm *MetricsMiddleware) normalizeIP(addr string) string {
	// Remove port if present
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		addr = addr[:idx]
	}

	// For privacy, you might want to mask IP addresses
	// This is a simplified implementation
	return addr
}

// incrementActiveRequests increments active request counter
func (mm *MetricsMiddleware) incrementActiveRequests(r *http.Request) {
	labels := []observability.Label{
		{Name: "method", Value: r.Method},
	}
	mm.metrics.IncrementGauge(mm.config.ActiveRequestsName, 1, labels...)
}

// decrementActiveRequests decrements active request counter
func (mm *MetricsMiddleware) decrementActiveRequests(r *http.Request) {
	labels := []observability.Label{
		{Name: "method", Value: r.Method},
	}
	mm.metrics.DecrementGauge(mm.config.ActiveRequestsName, 1, labels...)
}

// recordRequestSize records request body size
func (mm *MetricsMiddleware) recordRequestSize(r *http.Request, size float64) {
	labels := []observability.Label{
		{Name: "method", Value: r.Method},
	}
	mm.metrics.ObserveHistogram(mm.config.RequestSizeName, size, labels...)
}

// Configure implements the Middleware interface
func (mm *MetricsMiddleware) Configure(config map[string]interface{}) error {
	if requestsCounterName, ok := config["requests_counter_name"].(string); ok {
		mm.config.RequestsCounterName = requestsCounterName
	}
	if requestDurationName, ok := config["request_duration_name"].(string); ok {
		mm.config.RequestDurationName = requestDurationName
	}
	if includeMethod, ok := config["include_method"].(bool); ok {
		mm.config.IncludeMethod = includeMethod
	}
	if includePath, ok := config["include_path"].(bool); ok {
		mm.config.IncludePath = includePath
	}
	if includeStatus, ok := config["include_status"].(bool); ok {
		mm.config.IncludeStatus = includeStatus
	}
	if normalizePaths, ok := config["normalize_paths"].(bool); ok {
		mm.config.NormalizePaths = normalizePaths
	}
	if skipPaths, ok := config["skip_paths"].([]string); ok {
		mm.config.SkipPaths = skipPaths
	}
	if skipMethods, ok := config["skip_methods"].([]string); ok {
		mm.config.SkipMethods = skipMethods
	}
	if sampleRate, ok := config["sample_rate"].(float64); ok {
		mm.config.SampleRate = sampleRate
	}
	if useGoroutine, ok := config["use_goroutine"].(bool); ok {
		mm.config.UseGoroutine = useGoroutine
	}

	return nil
}

// Health implements the Middleware interface
func (mm *MetricsMiddleware) Health(ctx context.Context) error {
	if mm.metrics == nil {
		return fmt.Errorf("metrics provider not configured")
	}

	// Test metric creation
	testLabels := []observability.Label{
		{Name: "test", Value: "health_check"},
	}
	mm.metrics.IncrementCounter("health_check_test", 1, testLabels...)

	return nil
}

// metricsResponseWriter wraps http.ResponseWriter to capture metrics
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int64
}

func (mrw *metricsResponseWriter) WriteHeader(statusCode int) {
	mrw.statusCode = statusCode
	mrw.ResponseWriter.WriteHeader(statusCode)
}

func (mrw *metricsResponseWriter) Write(data []byte) (int, error) {
	size, err := mrw.ResponseWriter.Write(data)
	mrw.size += int64(size)
	return size, err
}

// Custom metric extractors

// DatabaseMetricsExtractor extracts database-related metrics
type DatabaseMetricsExtractor struct{}

func (dme *DatabaseMetricsExtractor) ExtractMetrics(r *http.Request, responseInfo *router.ResponseInfo) map[string]interface{} {
	metrics := make(map[string]interface{})

	// Extract database query count from context if available
	if queryCount, ok := r.Context().Value("db_query_count").(int64); ok {
		metrics["db_queries_total"] = queryCount
	}

	// Extract database duration from context if available
	if dbDuration, ok := r.Context().Value("db_duration").(time.Duration); ok {
		metrics["db_duration_seconds"] = dbDuration.Seconds()
	}

	return metrics
}

func (dme *DatabaseMetricsExtractor) MetricNames() []string {
	return []string{"db_queries_total", "db_duration_seconds"}
}

// CacheMetricsExtractor extracts cache-related metrics
type CacheMetricsExtractor struct{}

func (cme *CacheMetricsExtractor) ExtractMetrics(r *http.Request, responseInfo *router.ResponseInfo) map[string]interface{} {
	metrics := make(map[string]interface{})

	// Extract cache hits from context if available
	if cacheHits, ok := r.Context().Value("cache_hits").(int64); ok {
		metrics["cache_hits_total"] = cacheHits
	}

	// Extract cache misses from context if available
	if cacheMisses, ok := r.Context().Value("cache_misses").(int64); ok {
		metrics["cache_misses_total"] = cacheMisses
	}

	return metrics
}

func (cme *CacheMetricsExtractor) MetricNames() []string {
	return []string{"cache_hits_total", "cache_misses_total"}
}

// Utility functions

// MetricsMiddlewareFunc creates a simple metrics middleware function
func MetricsMiddlewareFunc(metrics observability.Metrics) func(http.Handler) http.Handler {
	middleware := NewMetricsMiddleware(metrics)
	return middleware.Handle
}

// WithMetrics creates metrics middleware with default configuration
func WithMetrics(metrics observability.Metrics) func(http.Handler) http.Handler {
	return MetricsMiddlewareFunc(metrics)
}

// WithMetricsConfig creates metrics middleware with custom configuration
func WithMetricsConfig(metrics observability.Metrics, config MetricsConfig) func(http.Handler) http.Handler {
	middleware := NewMetricsMiddleware(metrics, config)
	return middleware.Handle
}

// WithBasicMetrics creates metrics middleware with basic configuration
func WithBasicMetrics(metrics observability.Metrics) func(http.Handler) http.Handler {
	config := MetricsConfig{
		RequestsCounterName: "http_requests_total",
		RequestDurationName: "http_request_duration_seconds",
		IncludeMethod:       true,
		IncludePath:         false, // Disable path to avoid high cardinality
		IncludeStatus:       true,
		NormalizePaths:      false,
		SkipPaths:           []string{"/metrics", "/health"},
		SampleRate:          1.0,
		UseGoroutine:        false,
	}
	return WithMetricsConfig(metrics, config)
}

// WithDetailedMetrics creates metrics middleware with detailed configuration
func WithDetailedMetrics(metrics observability.Metrics) func(http.Handler) http.Handler {
	config := DefaultMetricsConfig()
	config.IncludeUserAgent = true
	config.CustomMetrics = []CustomMetricExtractor{
		&DatabaseMetricsExtractor{},
		&CacheMetricsExtractor{},
	}
	return WithMetricsConfig(metrics, config)
}

// Metrics utilities

// GetMetricsFromContext extracts metrics from request context
func GetMetricsFromContext(ctx context.Context, key string) interface{} {
	return ctx.Value(key)
}

// SetMetricsInContext sets metrics in request context
func SetMetricsInContext(ctx context.Context, key string, value interface{}) context.Context {
	return context.WithValue(ctx, key, value)
}

// IncrementDBQueryCount increments database query count in context
func IncrementDBQueryCount(ctx context.Context) context.Context {
	count, _ := ctx.Value("db_query_count").(int64)
	return context.WithValue(ctx, "db_query_count", count+1)
}

// SetDBDuration sets database operation duration in context
func SetDBDuration(ctx context.Context, duration time.Duration) context.Context {
	return context.WithValue(ctx, "db_duration", duration)
}

// IncrementCacheHit increments cache hit count in context
func IncrementCacheHit(ctx context.Context) context.Context {
	hits, _ := ctx.Value("cache_hits").(int64)
	return context.WithValue(ctx, "cache_hits", hits+1)
}

// IncrementCacheMiss increments cache miss count in context
func IncrementCacheMiss(ctx context.Context) context.Context {
	misses, _ := ctx.Value("cache_misses").(int64)
	return context.WithValue(ctx, "cache_misses", misses+1)
}

// Metrics testing utilities

// TestMetricsMiddleware provides utilities for testing metrics
type TestMetricsMiddleware struct {
	*MetricsMiddleware
	testMetrics *TestMetrics
}

// NewTestMetricsMiddleware creates a test metrics middleware
func NewTestMetricsMiddleware() *TestMetricsMiddleware {
	testMetrics := &TestMetrics{
		counters:   make(map[string]float64),
		gauges:     make(map[string]float64),
		histograms: make(map[string][]float64),
	}

	config := DefaultMetricsConfig()
	config.UseGoroutine = false // Disable goroutines in tests

	middleware := NewMetricsMiddleware(testMetrics, config)

	return &TestMetricsMiddleware{
		MetricsMiddleware: middleware.(*MetricsMiddleware),
		testMetrics:       testMetrics,
	}
}

// GetCounter gets counter value from test metrics
func (tmm *TestMetricsMiddleware) GetCounter(name string) float64 {
	return tmm.testMetrics.GetCounter(name)
}

// GetGauge gets gauge value from test metrics
func (tmm *TestMetricsMiddleware) GetGauge(name string) float64 {
	return tmm.testMetrics.GetGauge(name)
}

// GetHistogramCount gets histogram observation count
func (tmm *TestMetricsMiddleware) GetHistogramCount(name string) int {
	return len(tmm.testMetrics.GetHistogram(name))
}

// TestMetrics provides a test implementation of observability.Metrics
type TestMetrics struct {
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string][]float64
}

func (tm *TestMetrics) HTTPMiddleware() func(http.Handler) http.Handler {
	// TODO implement me
	panic("implement me")
}

func (tm *TestMetrics) StartServer(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *TestMetrics) StopServer(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *TestMetrics) Register(collector observability.Collector) error {
	// TODO implement me
	panic("implement me")
}

func (tm *TestMetrics) Unregister(collector observability.Collector) error {
	// TODO implement me
	panic("implement me")
}

func (tm *TestMetrics) Export(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (tm *TestMetrics) Gather() ([]*observability.MetricFamily, error) {
	// TODO implement me
	panic("implement me")
}

func (tm *TestMetrics) IncrementCounter(name string, value float64, labels ...observability.Label) {
	key := tm.makeKey(name, labels)
	tm.counters[key] += value
}

func (tm *TestMetrics) SetGauge(name string, value float64, labels ...observability.Label) {
	key := tm.makeKey(name, labels)
	tm.gauges[key] = value
}

func (tm *TestMetrics) IncrementGauge(name string, value float64, labels ...observability.Label) {
	key := tm.makeKey(name, labels)
	tm.gauges[key] += value
}

func (tm *TestMetrics) DecrementGauge(name string, value float64, labels ...observability.Label) {
	key := tm.makeKey(name, labels)
	tm.gauges[key] -= value
}

func (tm *TestMetrics) ObserveHistogram(name string, value float64, labels ...observability.Label) {
	key := tm.makeKey(name, labels)
	tm.histograms[key] = append(tm.histograms[key], value)
}

func (tm *TestMetrics) ObserveSummary(name string, value float64, labels ...observability.Label) {
	// Not implemented for test
}

func (tm *TestMetrics) Counter(name string, labels ...observability.Label) observability.Counter {
	// Not implemented for test
	return nil
}

func (tm *TestMetrics) Gauge(name string, labels ...observability.Label) observability.Gauge {
	// Not implemented for test
	return nil
}

func (tm *TestMetrics) Histogram(name string, buckets []float64, labels ...observability.Label) observability.Histogram {
	// Not implemented for test
	return nil
}

func (tm *TestMetrics) Summary(name string, objectives map[float64]float64, labels ...observability.Label) observability.Summary {
	// Not implemented for test
	return nil
}

func (tm *TestMetrics) makeKey(name string, labels []observability.Label) string {
	key := name
	for _, label := range labels {
		key += fmt.Sprintf(":%s=%s", label.Name, label.Value)
	}
	return key
}

func (tm *TestMetrics) GetCounter(name string) float64 {
	return tm.counters[name]
}

func (tm *TestMetrics) GetGauge(name string) float64 {
	return tm.gauges[name]
}

func (tm *TestMetrics) GetHistogram(name string) []float64 {
	return tm.histograms[name]
}

func (tm *TestMetrics) Reset() {
	tm.counters = make(map[string]float64)
	tm.gauges = make(map[string]float64)
	tm.histograms = make(map[string][]float64)
}
