package collectors

// HTTP collector Reset() methods don't return errors by design.

import (
	"bufio"
	"fmt"
	"maps"
	"net"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/go-utils/metrics"
)

// =============================================================================
// HTTP COLLECTOR
// =============================================================================

// HTTPCollector collects HTTP request/response metrics.
type HTTPCollector struct {
	name    string
	metrics map[string]any
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

// HTTPCollectorConfig contains configuration for the HTTP collector.
type HTTPCollectorConfig struct {
	TrackPaths         bool     `json:"track_paths"          yaml:"track_paths"`
	TrackUserAgents    bool     `json:"track_user_agents"    yaml:"track_user_agents"`
	TrackStatusCodes   bool     `json:"track_status_codes"   yaml:"track_status_codes"`
	TrackMethods       bool     `json:"track_methods"        yaml:"track_methods"`
	TrackSizes         bool     `json:"track_sizes"          yaml:"track_sizes"`
	PathWhitelist      []string `json:"path_whitelist"       yaml:"path_whitelist"`
	PathBlacklist      []string `json:"path_blacklist"       yaml:"path_blacklist"`
	MaxPathsTracked    int      `json:"max_paths_tracked"    yaml:"max_paths_tracked"`
	GroupSimilarPaths  bool     `json:"group_similar_paths"  yaml:"group_similar_paths"`
	IncludeQueryParams bool     `json:"include_query_params" yaml:"include_query_params"`
}

// HTTPRequestMetrics represents metrics for a single HTTP request.
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

// DefaultHTTPCollectorConfig returns default configuration.
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

// NewHTTPCollector creates a new HTTP collector.
func NewHTTPCollector() metrics.CustomCollector {
	return NewHTTPCollectorWithConfig(DefaultHTTPCollectorConfig())
}

// NewHTTPCollectorWithConfig creates a new HTTP collector with configuration.
func NewHTTPCollectorWithConfig(config *HTTPCollectorConfig) metrics.CustomCollector {
	collector := &HTTPCollector{
		name:             "http",
		metrics:          make(map[string]any),
		enabled:          true,
		requestsByStatus: make(map[int]int64),
		requestsByMethod: make(map[string]int64),
		requestsByPath:   make(map[string]int64),
		config:           config,
	}

	// Initialize metrics (these would be created by the metrics collector)
	collector.requestsTotal = metrics.NewCounter("http_requests_total")
	collector.requestDuration = metrics.NewHistogram("http_request_duration_seconds")
	collector.requestSize = metrics.NewHistogram("http_request_size_bytes")
	collector.responseSize = metrics.NewHistogram("http_response_size_bytes")
	collector.activeRequests = metrics.NewGauge("http_active_requests")

	return collector
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name.
func (hc *HTTPCollector) Name() string {
	return hc.name
}

// Collect collects HTTP metrics.
func (hc *HTTPCollector) Collect() map[string]any {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Rebuild metrics map fresh each time to avoid accumulating stale entries
	result := make(map[string]any)

	if !hc.enabled {
		return result
	}

	// Basic counters
	result["http.requests.total"] = hc.requestsTotal.Value()
	result["http.requests.active"] = hc.activeRequests.Value()

	// Request duration metrics
	result["http.request.duration"] = map[string]any{
		"count":   hc.requestDuration.Count(),
		"sum":     hc.requestDuration.Sum(),
		"mean":    hc.requestDuration.Mean(),
		"buckets": hc.requestDuration.Buckets(),
	}

	// Request size metrics
	if hc.config.TrackSizes {
		result["http.request.size"] = map[string]any{
			"count":   hc.requestSize.Count(),
			"sum":     hc.requestSize.Sum(),
			"mean":    hc.requestSize.Mean(),
			"buckets": hc.requestSize.Buckets(),
		}

		result["http.response.size"] = map[string]any{
			"count":   hc.responseSize.Count(),
			"sum":     hc.responseSize.Sum(),
			"mean":    hc.responseSize.Mean(),
			"buckets": hc.responseSize.Buckets(),
		}
	}

	// Status code metrics
	if hc.config.TrackStatusCodes {
		for status, count := range hc.requestsByStatus {
			result[fmt.Sprintf("http.requests.status.%d", status)] = count
		}
	}

	// Method metrics
	if hc.config.TrackMethods {
		for method, count := range hc.requestsByMethod {
			result["http.requests.method."+method] = count
		}
	}

	// Path metrics
	if hc.config.TrackPaths {
		for path, count := range hc.requestsByPath {
			result["http.requests.path."+hc.sanitizePath(path)] = count
		}
	}

	// Calculate derived metrics into result
	hc.calculateDerivedMetricsInto(result)

	return result
}

// Reset resets the collector.
func (hc *HTTPCollector) Reset() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.metrics = make(map[string]any)
	hc.requestsByStatus = make(map[int]int64)
	hc.requestsByMethod = make(map[string]int64)
	hc.requestsByPath = make(map[string]int64)
	hc.activeRequestsCount = 0

	// Reset underlying metrics
	_ = hc.requestsTotal.Reset()
	_ = hc.requestDuration.Reset()
	_ = hc.requestSize.Reset()
	_ = hc.responseSize.Reset()
	_ = hc.activeRequests.Reset()

	return nil
}

// =============================================================================
// HTTP METRICS RECORDING
// =============================================================================

// RecordRequest records metrics for an HTTP request.
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

		// Check if this is a new path that would exceed the limit
		if _, exists := hc.requestsByPath[path]; !exists && len(hc.requestsByPath) >= hc.config.MaxPathsTracked {
			hc.pruneOldPaths()
		}

		hc.requestsByPath[path]++
	}
}

// StartRequest records the start of an HTTP request.
func (hc *HTTPCollector) StartRequest() {
	if !hc.enabled {
		return
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.activeRequestsCount++
	hc.activeRequests.Set(float64(hc.activeRequestsCount))
}

// EndRequest records the end of an HTTP request.
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

// Middleware returns HTTP middleware that automatically records metrics.
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

// httpResponseWrapper wraps http.ResponseWriter to capture metrics.
type httpResponseWrapper struct {
	http.ResponseWriter

	statusCode   int
	bytesWritten int
	wroteHeader  bool
}

func (w *httpResponseWrapper) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *httpResponseWrapper) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(w.statusCode)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n

	return n, err
}

// Hijack lets WebSocket upgrades take over the TCP connection through this wrapper.
// Without this, gorilla/websocket's type assertion to http.Hijacker fails and the
// upgrade returns "websocket: hijack: feature not supported".
func (w *httpResponseWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Flush forwards to the underlying writer so SSE and streaming responses
// continue to flush after passing through the metrics wrapper.
func (w *httpResponseWrapper) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Push forwards HTTP/2 server push to the underlying writer.
func (w *httpResponseWrapper) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// =============================================================================
// PATH HANDLING
// =============================================================================

// shouldTrackPath determines if a path should be tracked.
func (hc *HTTPCollector) shouldTrackPath(path string) bool {
	// Check blacklist
	if slices.Contains(hc.config.PathBlacklist, path) {
		return false
	}

	// Check whitelist (if configured)
	if len(hc.config.PathWhitelist) > 0 {
		return slices.Contains(hc.config.PathWhitelist, path)
	}

	return true
}

// normalizePath normalizes a path for tracking.
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

// groupSimilarPath groups similar paths together.
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

// isNumericID checks if a segment is a numeric ID.
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

// isUUID checks if a segment is a UUID.
func (hc *HTTPCollector) isUUID(segment string) bool {
	if len(segment) != 36 {
		return false
	}

	// Simple UUID pattern check
	return len(strings.Split(segment, "-")) == 5
}

// sanitizePath sanitizes a path for use as a metric name.
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

// pruneOldPaths removes old paths to keep within limits.
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

// calculateDerivedMetricsInto calculates derived metrics into the provided map.
func (hc *HTTPCollector) calculateDerivedMetricsInto(result map[string]any) {
	// Calculate error rate
	var totalRequests, errorRequests int64
	for status, count := range hc.requestsByStatus {
		totalRequests += count
		if status >= 400 {
			errorRequests += count
		}
	}

	if totalRequests > 0 {
		result["http.requests.error_rate"] = float64(errorRequests) / float64(totalRequests) * 100
	}

	// Calculate average response time
	if hc.requestDuration.Count() > 0 {
		result["http.requests.avg_duration"] = hc.requestDuration.Mean()
	}

	// Calculate throughput metrics
	if hc.config.TrackSizes {
		if hc.requestSize.Count() > 0 {
			result["http.request.avg_size"] = hc.requestSize.Mean()
		}

		if hc.responseSize.Count() > 0 {
			result["http.response.avg_size"] = hc.responseSize.Mean()
		}
	}
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// Enable enables the collector.
func (hc *HTTPCollector) Enable() {
	hc.enabled = true
}

// Disable disables the collector.
func (hc *HTTPCollector) Disable() {
	hc.enabled = false
}

// IsEnabled returns whether the collector is enabled.
func (hc *HTTPCollector) IsEnabled() bool {
	return hc.enabled
}

// GetActiveRequests returns the number of active requests.
func (hc *HTTPCollector) GetActiveRequests() int64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return hc.activeRequestsCount
}

// GetRequestsByStatus returns requests grouped by status code.
func (hc *HTTPCollector) GetRequestsByStatus() map[int]int64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[int]int64)
	maps.Copy(result, hc.requestsByStatus)

	return result
}

// GetRequestsByMethod returns requests grouped by method.
func (hc *HTTPCollector) GetRequestsByMethod() map[string]int64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]int64)
	maps.Copy(result, hc.requestsByMethod)

	return result
}

// GetRequestsByPath returns requests grouped by path.
func (hc *HTTPCollector) GetRequestsByPath() map[string]int64 {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	result := make(map[string]int64)
	maps.Copy(result, hc.requestsByPath)

	return result
}

// GetTopPaths returns the top N paths by request count.
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

// CreateHTTPMetricsMiddleware creates HTTP metrics middleware.
func CreateHTTPMetricsMiddleware(collector *HTTPCollector) func(http.Handler) http.Handler {
	return collector.Middleware()
}

// RecordHTTPRequestMetrics records HTTP request metrics.
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

// CreateHTTPCollectorWithMetrics creates an HTTP collector with pre-configured metrics.
func CreateHTTPCollectorWithMetrics(metricsCollector metrics.Metrics, config *HTTPCollectorConfig) *HTTPCollector {
	collector := &HTTPCollector{
		name:             "http",
		metrics:          make(map[string]any),
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
