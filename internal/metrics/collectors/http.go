package collectors

//nolint:gosec // G104: Collector Reset() methods are intentionally void
// HTTP collector Reset() methods don't return errors by design.

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	metrics "github.com/xraph/forge/internal/metrics/internal"
)

// =============================================================================
// HTTP COLLECTOR
// =============================================================================

// HTTPCollector collects HTTP request/response metrics
type HTTPCollector struct {
	name    string
	metrics map[string]interface{}
	mu      sync.RWMutex
	enabled bool

	// Metrics
	requestsTotal   metrics.Counter
	requestDuration metrics.Histogram
	requestSize     metrics.Histogram
	responseSize    metrics.Histogram
	activeRequests  metrics.Gauge

	// Request tracking
	activeRequestsCount int64
	requestsByStatus    map[int]int64
	requestsByMethod    map[string]int64
	requestsByPath      map[string]int64

	// Configuration
	config *HTTPCollectorConfig
}

// HTTPCollectorConfig contains configuration for the HTTP collector
type HTTPCollectorConfig struct {
	TrackPaths         bool     `yaml:"track_paths" json:"track_paths"`
	TrackUserAgents    bool     `yaml:"track_user_agents" json:"track_user_agents"`
	TrackStatusCodes   bool     `yaml:"track_status_codes" json:"track_status_codes"`
	TrackMethods       bool     `yaml:"track_methods" json:"track_methods"`
	TrackSizes         bool     `yaml:"track_sizes" json:"track_sizes"`
	PathWhitelist      []string `yaml:"path_whitelist" json:"path_whitelist"`
	PathBlacklist      []string `yaml:"path_blacklist" json:"path_blacklist"`
	MaxPathsTracked    int      `yaml:"max_paths_tracked" json:"max_paths_tracked"`
	GroupSimilarPaths  bool     `yaml:"group_similar_paths" json:"group_similar_paths"`
	IncludeQueryParams bool     `yaml:"include_query_params" json:"include_query_params"`
}

// HTTPRequestMetrics represents metrics for a single HTTP request
type HTTPRequestMetrics struct {
	Method        string
	Path          string
	StatusCode    int
	UserAgent     string
	ContentLength int64
	ResponseSize  int64
	Duration      time.Duration
	Timestamp     time.Time
}

// DefaultHTTPCollectorConfig returns default configuration
func DefaultHTTPCollectorConfig() *HTTPCollectorConfig {
	return &HTTPCollectorConfig{
		TrackPaths:         true,
		TrackUserAgents:    false,
		TrackStatusCodes:   true,
		TrackMethods:       true,
		TrackSizes:         true,
		PathWhitelist:      []string{},
		PathBlacklist:      []string{"/health", "/metrics"},
		MaxPathsTracked:    1000,
		GroupSimilarPaths:  true,
		IncludeQueryParams: false,
	}
}

// NewHTTPCollector creates a new HTTP collector
func NewHTTPCollector() metrics.CustomCollector {
	return NewHTTPCollectorWithConfig(DefaultHTTPCollectorConfig())
}

// NewHTTPCollectorWithConfig creates a new HTTP collector with configuration
func NewHTTPCollectorWithConfig(config *HTTPCollectorConfig) metrics.CustomCollector {
	collector := &HTTPCollector{
		name:             "http",
		metrics:          make(map[string]interface{}),
		enabled:          true,
		requestsByStatus: make(map[int]int64),
		requestsByMethod: make(map[string]int64),
		requestsByPath:   make(map[string]int64),
		config:           config,
	}

	// Initialize metrics (these would be created by the metrics collector)
	collector.requestsTotal = metrics.NewCounter()
	collector.requestDuration = metrics.NewHistogram()
	collector.requestSize = metrics.NewHistogram()
	collector.responseSize = metrics.NewHistogram()
	collector.activeRequests = metrics.NewGauge()

	return collector
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name
func (hc *HTTPCollector) Name() string {
	return hc.name
}

// Collect collects HTTP metrics
func (hc *HTTPCollector) Collect() map[string]interface{} {
	if !hc.enabled {
		return hc.metrics
	}

	hc.mu.RLock()
	defer hc.mu.RUnlock()

	// Basic counters
	hc.metrics["http.requests.total"] = hc.requestsTotal.Get()
	hc.metrics["http.requests.active"] = hc.activeRequests.Get()

	// Request duration metrics
	hc.metrics["http.request.duration"] = map[string]interface{}{
		"count":   hc.requestDuration.GetCount(),
		"sum":     hc.requestDuration.GetSum(),
		"mean":    hc.requestDuration.GetMean(),
		"buckets": hc.requestDuration.GetBuckets(),
	}

	// Request size metrics
	if hc.config.TrackSizes {
		hc.metrics["http.request.size"] = map[string]interface{}{
			"count":   hc.requestSize.GetCount(),
			"sum":     hc.requestSize.GetSum(),
			"mean":    hc.requestSize.GetMean(),
			"buckets": hc.requestSize.GetBuckets(),
		}

		hc.metrics["http.response.size"] = map[string]interface{}{
			"count":   hc.responseSize.GetCount(),
			"sum":     hc.responseSize.GetSum(),
			"mean":    hc.responseSize.GetMean(),
			"buckets": hc.responseSize.GetBuckets(),
		}
	}

	// Status code metrics
	if hc.config.TrackStatusCodes {
		for status, count := range hc.requestsByStatus {
			hc.metrics[fmt.Sprintf("http.requests.status.%d", status)] = count
		}
	}

	// Method metrics
	if hc.config.TrackMethods {
		for method, count := range hc.requestsByMethod {
			hc.metrics[fmt.Sprintf("http.requests.method.%s", method)] = count
		}
	}

	// Path metrics
	if hc.config.TrackPaths {
		for path, count := range hc.requestsByPath {
			hc.metrics[fmt.Sprintf("http.requests.path.%s", hc.sanitizePath(path))] = count
		}
	}

	// Calculate derived metrics
	hc.calculateDerivedMetrics()

	return hc.metrics
}

// Reset resets the collector
func (hc *HTTPCollector) Reset() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.metrics = make(map[string]interface{})
	hc.requestsByStatus = make(map[int]int64)
	hc.requestsByMethod = make(map[string]int64)
	hc.requestsByPath = make(map[string]int64)
	hc.activeRequestsCount = 0

	// Reset underlying metrics
	hc.requestsTotal.Reset()
	hc.requestDuration.Reset()
	hc.requestSize.Reset()
	hc.responseSize.Reset()
	hc.activeRequests.Reset()

	return nil
}

// =============================================================================
// HTTP METRICS RECORDING
// =============================================================================

// RecordRequest records metrics for an HTTP request
func (hc *HTTPCollector) RecordRequest(reqMetrics HTTPRequestMetrics) {
	if !hc.enabled {
		return
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Record basic metrics
	hc.requestsTotal.Inc()
	hc.requestDuration.Observe(reqMetrics.Duration.Seconds())

	// Record sizes if enabled
	if hc.config.TrackSizes {
		if reqMetrics.ContentLength > 0 {
			hc.requestSize.Observe(float64(reqMetrics.ContentLength))
		}
		if reqMetrics.ResponseSize > 0 {
			hc.responseSize.Observe(float64(reqMetrics.ResponseSize))
		}
	}

	// Record status codes if enabled
	if hc.config.TrackStatusCodes {
		hc.requestsByStatus[reqMetrics.StatusCode]++
	}

	// Record methods if enabled
	if hc.config.TrackMethods {
		hc.requestsByMethod[reqMetrics.Method]++
	}

	// Record paths if enabled
	if hc.config.TrackPaths && hc.shouldTrackPath(reqMetrics.Path) {
		path := hc.normalizePath(reqMetrics.Path)
		hc.requestsByPath[path]++

		// Limit number of paths tracked
		if len(hc.requestsByPath) > hc.config.MaxPathsTracked {
			hc.pruneOldPaths()
		}
	}
}

// StartRequest records the start of an HTTP request
func (hc *HTTPCollector) StartRequest() {
	if !hc.enabled {
		return
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.activeRequestsCount++
	hc.activeRequests.Set(float64(hc.activeRequestsCount))
}

// EndRequest records the end of an HTTP request
func (hc *HTTPCollector) EndRequest() {
	if !hc.enabled {
		return
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.activeRequestsCount--
	if hc.activeRequestsCount < 0 {
		hc.activeRequestsCount = 0
	}
	hc.activeRequests.Set(float64(hc.activeRequestsCount))
}

// =============================================================================
// MIDDLEWARE INTEGRATION
// =============================================================================

// Middleware returns HTTP middleware that automatically records metrics
func (hc *HTTPCollector) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Track active requests
			hc.StartRequest()
			defer hc.EndRequest()

			// Create response wrapper to capture metrics
			wrapper := &httpResponseWrapper{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				bytesWritten:   0,
			}

			// Process request
			next.ServeHTTP(wrapper, r)

			// Record metrics
			duration := time.Since(start)

			reqMetrics := HTTPRequestMetrics{
				Method:        r.Method,
				Path:          r.URL.Path,
				StatusCode:    wrapper.statusCode,
				UserAgent:     r.UserAgent(),
				ContentLength: r.ContentLength,
				ResponseSize:  int64(wrapper.bytesWritten),
				Duration:      duration,
				Timestamp:     start,
			}

			hc.RecordRequest(reqMetrics)
		})
	}
}

// httpResponseWrapper wraps http.ResponseWriter to capture metrics
type httpResponseWrapper struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (w *httpResponseWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *httpResponseWrapper) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n
	return n, err
}

// =============================================================================
// PATH HANDLING
// =============================================================================

// shouldTrackPath determines if a path should be tracked
func (hc *HTTPCollector) shouldTrackPath(path string) bool {
	// Check blacklist
	for _, blacklistedPath := range hc.config.PathBlacklist {
		if path == blacklistedPath {
			return false
		}
	}

	// Check whitelist (if configured)
	if len(hc.config.PathWhitelist) > 0 {
		for _, whitelistedPath := range hc.config.PathWhitelist {
			if path == whitelistedPath {
				return true
			}
		}
		return false
	}

	return true
}

// normalizePath normalizes a path for tracking
func (hc *HTTPCollector) normalizePath(path string) string {
	if !hc.config.IncludeQueryParams {
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}
	}

	if hc.config.GroupSimilarPaths {
		path = hc.groupSimilarPath(path)
	}

	return path
}

// groupSimilarPath groups similar paths together
func (hc *HTTPCollector) groupSimilarPath(path string) string {
	// Replace numeric IDs with placeholders
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if hc.isNumericID(segment) {
			segments[i] = "{id}"
		} else if hc.isUUID(segment) {
			segments[i] = "{uuid}"
		}
	}

	return strings.Join(segments, "/")
}

// isNumericID checks if a segment is a numeric ID
func (hc *HTTPCollector) isNumericID(segment string) bool {
	if len(segment) == 0 {
		return false
	}

	for _, r := range segment {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// isUUID checks if a segment is a UUID
func (hc *HTTPCollector) isUUID(segment string) bool {
	if len(segment) != 36 {
		return false
	}

	// Simple UUID pattern check
	return len(strings.Split(segment, "-")) == 5
}

// sanitizePath sanitizes a path for use as a metric name
func (hc *HTTPCollector) sanitizePath(path string) string {
	// Replace invalid characters with underscores
	result := strings.Builder{}
	for _, char := range path {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-' {
			result.WriteRune(char)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}

// pruneOldPaths removes old paths to keep within limits
func (hc *HTTPCollector) pruneOldPaths() {
	if len(hc.requestsByPath) <= hc.config.MaxPathsTracked {
		return
	}

	// Sort paths by request count and keep only the top ones
	type pathCount struct {
		path  string
		count int64
	}

	var paths []pathCount
	for path, count := range hc.requestsByPath {
		paths = append(paths, pathCount{path: path, count: count})
	}

	// Sort by count (descending)
	sort.Slice(paths, func(i, j int) bool {
		return paths[i].count > paths[j].count
	})

	// Keep only the top paths
	newRequestsByPath := make(map[string]int64)
	for i := 0; i < hc.config.MaxPathsTracked && i < len(paths); i++ {
		newRequestsByPath[paths[i].path] = paths[i].count
	}

	hc.requestsByPath = newRequestsByPath
}

// =============================================================================
// DERIVED METRICS
// =============================================================================

// calculateDerivedMetrics calculates derived metrics
func (hc *HTTPCollector) calculateDerivedMetrics() {
	// Calculate error rate
	var totalRequests, errorRequests int64
	for status, count := range hc.requestsByStatus {
		totalRequests += count
		if status >= 400 {
			errorRequests += count
		}
	}

	if totalRequests > 0 {
		hc.metrics["http.requests.error_rate"] = float64(errorRequests) / float64(totalRequests) * 100
	}

	// Calculate average response time
	if hc.requestDuration.GetCount() > 0 {
		hc.metrics["http.requests.avg_duration"] = hc.requestDuration.GetMean()
	}

	// Calculate requests per second (approximate)
	hc.metrics["http.requests.rps"] = float64(totalRequests) / time.Since(time.Now()).Seconds()

	// Calculate throughput metrics
	if hc.config.TrackSizes {
		if hc.requestSize.GetCount() > 0 {
			hc.metrics["http.request.avg_size"] = hc.requestSize.GetMean()
		}
		if hc.responseSize.GetCount() > 0 {
			hc.metrics["http.response.avg_size"] = hc.responseSize.GetMean()
		}
	}
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// Enable enables the collector
func (hc *HTTPCollector) Enable() {
	hc.enabled = true
}

// Disable disables the collector
func (hc *HTTPCollector) Disable() {
	hc.enabled = false
}

// IsEnabled returns whether the collector is enabled
func (hc *HTTPCollector) IsEnabled() bool {
	return hc.enabled
}

// GetActiveRequests returns the number of active requests
func (hc *HTTPCollector) GetActiveRequests() int64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.activeRequestsCount
}

// GetRequestsByStatus returns requests grouped by status code
func (hc *HTTPCollector) GetRequestsByStatus() map[int]int64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[int]int64)
	for status, count := range hc.requestsByStatus {
		result[status] = count
	}
	return result
}

// GetRequestsByMethod returns requests grouped by method
func (hc *HTTPCollector) GetRequestsByMethod() map[string]int64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]int64)
	for method, count := range hc.requestsByMethod {
		result[method] = count
	}
	return result
}

// GetRequestsByPath returns requests grouped by path
func (hc *HTTPCollector) GetRequestsByPath() map[string]int64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]int64)
	for path, count := range hc.requestsByPath {
		result[path] = count
	}
	return result
}

// GetTopPaths returns the top N paths by request count
func (hc *HTTPCollector) GetTopPaths(n int) []string {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	type pathCount struct {
		path  string
		count int64
	}

	var paths []pathCount
	for path, count := range hc.requestsByPath {
		paths = append(paths, pathCount{path: path, count: count})
	}

	// Sort by count (descending)
	sort.Slice(paths, func(i, j int) bool {
		return paths[i].count > paths[j].count
	})

	var result []string
	for i := 0; i < n && i < len(paths); i++ {
		result = append(result, paths[i].path)
	}

	return result
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// CreateHTTPMetricsMiddleware creates HTTP metrics middleware
func CreateHTTPMetricsMiddleware(collector *HTTPCollector) func(http.Handler) http.Handler {
	return collector.Middleware()
}

// RecordHTTPRequestMetrics records HTTP request metrics
func RecordHTTPRequestMetrics(collector *HTTPCollector, method, path string, statusCode int, duration time.Duration) {
	metrics := HTTPRequestMetrics{
		Method:     method,
		Path:       path,
		StatusCode: statusCode,
		Duration:   duration,
		Timestamp:  time.Now(),
	}

	collector.RecordRequest(metrics)
}

// CreateHTTPCollectorWithMetrics creates an HTTP collector with pre-configured metrics
func CreateHTTPCollectorWithMetrics(metricsCollector metrics.Metrics, config *HTTPCollectorConfig) *HTTPCollector {
	collector := &HTTPCollector{
		name:             "http",
		metrics:          make(map[string]interface{}),
		enabled:          true,
		requestsByStatus: make(map[int]int64),
		requestsByMethod: make(map[string]int64),
		requestsByPath:   make(map[string]int64),
		config:           config,
	}

	// Create metrics using the metrics collector
	collector.requestsTotal = metricsCollector.Counter("http_requests_total")
	collector.requestDuration = metricsCollector.Histogram("http_request_duration_seconds")
	collector.requestSize = metricsCollector.Histogram("http_request_size_bytes")
	collector.responseSize = metricsCollector.Histogram("http_response_size_bytes")
	collector.activeRequests = metricsCollector.Gauge("http_active_requests")

	return collector
}
