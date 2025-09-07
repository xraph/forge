package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
)

// =============================================================================
// HTTP METRICS MIDDLEWARE
// =============================================================================

// HTTPMetricsMiddleware provides automatic HTTP metrics collection
type HTTPMetricsMiddleware struct {
	name      string
	collector metrics.MetricsCollector
	config    *HTTPMetricsConfig
	logger    logger.Logger
	started   bool

	// Metrics
	requestsTotal        metrics.Counter
	requestDuration      metrics.Histogram
	requestSize          metrics.Histogram
	responseSize         metrics.Histogram
	requestsInFlight     metrics.Gauge
	requestErrors        metrics.Counter
	requestsByStatusCode map[int]metrics.Counter
	requestsByMethod     map[string]metrics.Counter
	requestsByEndpoint   map[string]metrics.Counter
}

// HTTPMetricsConfig contains configuration for HTTP metrics middleware
type HTTPMetricsConfig struct {
	Enabled             bool      `yaml:"enabled" json:"enabled"`
	CollectRequestSize  bool      `yaml:"collect_request_size" json:"collect_request_size"`
	CollectResponseSize bool      `yaml:"collect_response_size" json:"collect_response_size"`
	CollectUserAgent    bool      `yaml:"collect_user_agent" json:"collect_user_agent"`
	CollectRemoteAddr   bool      `yaml:"collect_remote_addr" json:"collect_remote_addr"`
	GroupByEndpoint     bool      `yaml:"group_by_endpoint" json:"group_by_endpoint"`
	GroupByMethod       bool      `yaml:"group_by_method" json:"group_by_method"`
	GroupByStatusCode   bool      `yaml:"group_by_status_code" json:"group_by_status_code"`
	IgnorePaths         []string  `yaml:"ignore_paths" json:"ignore_paths"`
	BucketSizes         []float64 `yaml:"bucket_sizes" json:"bucket_sizes"`
	MaxPathLength       int       `yaml:"max_path_length" json:"max_path_length"`
	NormalizeEndpoints  bool      `yaml:"normalize_endpoints" json:"normalize_endpoints"`
}

// DefaultHTTPMetricsConfig returns default configuration
func DefaultHTTPMetricsConfig() *HTTPMetricsConfig {
	return &HTTPMetricsConfig{
		Enabled:             true,
		CollectRequestSize:  true,
		CollectResponseSize: true,
		CollectUserAgent:    false,
		CollectRemoteAddr:   false,
		GroupByEndpoint:     true,
		GroupByMethod:       true,
		GroupByStatusCode:   true,
		IgnorePaths:         []string{"/health", "/metrics"},
		BucketSizes:         []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		MaxPathLength:       100,
		NormalizeEndpoints:  true,
	}
}

// NewHTTPMetricsMiddleware creates a new HTTP metrics middleware
func NewHTTPMetricsMiddleware(collector metrics.MetricsCollector) *HTTPMetricsMiddleware {
	return NewHTTPMetricsMiddlewareWithConfig(collector, DefaultHTTPMetricsConfig())
}

// NewHTTPMetricsMiddlewareWithConfig creates a new HTTP metrics middleware with configuration
func NewHTTPMetricsMiddlewareWithConfig(collector metrics.MetricsCollector, config *HTTPMetricsConfig) *HTTPMetricsMiddleware {
	return &HTTPMetricsMiddleware{
		name:                 "http-metrics",
		collector:            collector,
		config:               config,
		requestsByStatusCode: make(map[int]metrics.Counter),
		requestsByMethod:     make(map[string]metrics.Counter),
		requestsByEndpoint:   make(map[string]metrics.Counter),
	}
}

// =============================================================================
// MIDDLEWARE IMPLEMENTATION
// =============================================================================

// Name returns the middleware name
func (m *HTTPMetricsMiddleware) Name() string {
	return m.name
}

// Dependencies returns the middleware dependencies
func (m *HTTPMetricsMiddleware) Dependencies() []string {
	return []string{}
}

// OnStart is called when the middleware starts
func (m *HTTPMetricsMiddleware) OnStart(ctx context.Context) error {
	if m.started {
		return common.ErrServiceAlreadyExists(m.name)
	}

	// Initialize metrics
	m.initializeMetrics()

	m.started = true

	if m.logger != nil {
		m.logger.Info("HTTP metrics middleware started",
			logger.String("name", m.name),
			logger.Bool("enabled", m.config.Enabled),
			logger.Bool("collect_request_size", m.config.CollectRequestSize),
			logger.Bool("collect_response_size", m.config.CollectResponseSize),
			logger.String("ignore_paths", strings.Join(m.config.IgnorePaths, ",")),
		)
	}

	return nil
}

// OnStop is called when the middleware stops
func (m *HTTPMetricsMiddleware) OnStop(ctx context.Context) error {
	if !m.started {
		return common.ErrServiceNotFound(m.name)
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("HTTP metrics middleware stopped", logger.String("name", m.name))
	}

	return nil
}

// OnHealthCheck is called to check middleware health
func (m *HTTPMetricsMiddleware) OnHealthCheck(ctx context.Context) error {
	if !m.started {
		return common.ErrHealthCheckFailed(m.name, fmt.Errorf("middleware not started"))
	}

	return nil
}

// Handler returns the HTTP handler function
func (m *HTTPMetricsMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !m.config.Enabled || m.shouldIgnorePath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Record metrics
			m.recordRequest(w, r, next)
		})
	}
}

// MiddlewareDefinition returns the middleware definition for the router
func (m *HTTPMetricsMiddleware) MiddlewareDefinition() common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:         m.name,
		Priority:     100, // Run early in the chain
		Handler:      m.Handler(),
		Dependencies: m.Dependencies(),
		Config:       m.config,
	}
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// initializeMetrics initializes the metrics
func (m *HTTPMetricsMiddleware) initializeMetrics() {
	// Core metrics
	m.requestsTotal = m.collector.Counter("http_requests_total", "method", "endpoint", "status_code")
	m.requestDuration = m.collector.Histogram("http_request_duration_seconds", "method", "endpoint")
	m.requestsInFlight = m.collector.Gauge("http_requests_in_flight")
	m.requestErrors = m.collector.Counter("http_request_errors_total", "method", "endpoint", "error_type")

	// Optional metrics
	if m.config.CollectRequestSize {
		m.requestSize = m.collector.Histogram("http_request_size_bytes", "method", "endpoint")
	}

	if m.config.CollectResponseSize {
		m.responseSize = m.collector.Histogram("http_response_size_bytes", "method", "endpoint", "status_code")
	}
}

// recordRequest records metrics for an HTTP request
func (m *HTTPMetricsMiddleware) recordRequest(w http.ResponseWriter, r *http.Request, next http.Handler) {
	start := time.Now()

	// Increment in-flight requests
	m.requestsInFlight.Inc()
	defer m.requestsInFlight.Dec()

	// Create response wrapper to capture metrics
	wrapper := &httpResponseWrapper{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		bytesWritten:   0,
	}

	// Handle panics
	defer func() {
		if recovered := recover(); recovered != nil {
			m.recordError(r, "panic", fmt.Errorf("panic: %v", recovered))
			panic(recovered) // Re-panic to let other middleware handle it
		}
	}()

	// Process request
	next.ServeHTTP(wrapper, r)

	// Record metrics
	duration := time.Since(start)
	method := r.Method
	endpoint := m.normalizeEndpoint(r.URL.Path)
	statusCode := wrapper.statusCode

	// Record basic metrics
	m.requestsTotal.Add(1)
	m.requestDuration.Observe(duration.Seconds())

	// Record request size if enabled
	if m.config.CollectRequestSize && r.ContentLength > 0 {
		m.requestSize.Observe(float64(r.ContentLength))
	}

	// Record response size if enabled
	if m.config.CollectResponseSize && wrapper.bytesWritten > 0 {
		m.responseSize.Observe(float64(wrapper.bytesWritten))
	}

	// Record error if status code indicates error
	if statusCode >= 400 {
		errorType := m.getErrorType(statusCode)
		m.requestErrors.Add(1)
		m.recordError(r, errorType, fmt.Errorf("HTTP %d", statusCode))
	}

	// Record grouped metrics
	if m.config.GroupByStatusCode {
		m.recordStatusCodeMetric(statusCode)
	}

	if m.config.GroupByMethod {
		m.recordMethodMetric(method)
	}

	if m.config.GroupByEndpoint {
		m.recordEndpointMetric(endpoint)
	}

	// Log request if logger is available
	if m.logger != nil {
		fields := []logger.Field{
			logger.String("method", method),
			logger.String("endpoint", endpoint),
			logger.Int("status_code", statusCode),
			logger.Duration("duration", duration),
			logger.Int64("request_size", r.ContentLength),
			logger.Int("response_size", wrapper.bytesWritten),
		}

		if m.config.CollectUserAgent {
			fields = append(fields, logger.String("user_agent", r.UserAgent()))
		}

		if m.config.CollectRemoteAddr {
			fields = append(fields, logger.String("remote_addr", r.RemoteAddr))
		}

		m.logger.Info("HTTP request processed", fields...)
	}
}

// recordError records an error metric
func (m *HTTPMetricsMiddleware) recordError(r *http.Request, errorType string, err error) {
	method := r.Method
	endpoint := m.normalizeEndpoint(r.URL.Path)

	// Record error metric with tags
	m.requestErrors.Add(1)

	if m.logger != nil {
		m.logger.Error("HTTP request error",
			logger.String("method", method),
			logger.String("endpoint", endpoint),
			logger.String("error_type", errorType),
			logger.Error(err),
		)
	}
}

// recordStatusCodeMetric records a status code specific metric
func (m *HTTPMetricsMiddleware) recordStatusCodeMetric(statusCode int) {
	counter, exists := m.requestsByStatusCode[statusCode]
	if !exists {
		counter = m.collector.Counter("http_requests_by_status_code", "status_code", strconv.Itoa(statusCode))
		m.requestsByStatusCode[statusCode] = counter
	}
	counter.Inc()
}

// recordMethodMetric records a method specific metric
func (m *HTTPMetricsMiddleware) recordMethodMetric(method string) {
	counter, exists := m.requestsByMethod[method]
	if !exists {
		counter = m.collector.Counter("http_requests_by_method", "method", method)
		m.requestsByMethod[method] = counter
	}
	counter.Inc()
}

// recordEndpointMetric records an endpoint specific metric
func (m *HTTPMetricsMiddleware) recordEndpointMetric(endpoint string) {
	counter, exists := m.requestsByEndpoint[endpoint]
	if !exists {
		counter = m.collector.Counter("http_requests_by_endpoint", "endpoint", endpoint)
		m.requestsByEndpoint[endpoint] = counter
	}
	counter.Inc()
}

// normalizeEndpoint normalizes the endpoint path for metrics
func (m *HTTPMetricsMiddleware) normalizeEndpoint(path string) string {
	if !m.config.NormalizeEndpoints {
		return path
	}

	// Truncate if too long
	if len(path) > m.config.MaxPathLength {
		path = path[:m.config.MaxPathLength]
	}

	// Replace path parameters with placeholders
	path = m.replacePathParameters(path)

	return path
}

// replacePathParameters replaces path parameters with placeholders
func (m *HTTPMetricsMiddleware) replacePathParameters(path string) string {
	// Simple implementation - in a real implementation, this would use
	// the router's path pattern matching to identify parameters
	parts := strings.Split(path, "/")

	for i, part := range parts {
		// Replace numeric IDs with placeholder
		if m.isNumeric(part) {
			parts[i] = "{id}"
		}
		// Replace UUID-like strings with placeholder
		if m.isUUID(part) {
			parts[i] = "{uuid}"
		}
	}

	return strings.Join(parts, "/")
}

// isNumeric checks if a string is numeric
func (m *HTTPMetricsMiddleware) isNumeric(s string) bool {
	if s == "" {
		return false
	}

	for _, char := range s {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}

// isUUID checks if a string looks like a UUID
func (m *HTTPMetricsMiddleware) isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	// Simple UUID pattern check
	return strings.Count(s, "-") == 4
}

// shouldIgnorePath checks if a path should be ignored
func (m *HTTPMetricsMiddleware) shouldIgnorePath(path string) bool {
	for _, ignorePath := range m.config.IgnorePaths {
		if path == ignorePath || strings.HasPrefix(path, ignorePath) {
			return true
		}
	}
	return false
}

// getErrorType returns the error type based on status code
func (m *HTTPMetricsMiddleware) getErrorType(statusCode int) string {
	switch {
	case statusCode >= 400 && statusCode < 500:
		return "client_error"
	case statusCode >= 500:
		return "server_error"
	default:
		return "unknown_error"
	}
}

// SetLogger sets the logger
func (m *HTTPMetricsMiddleware) SetLogger(logger logger.Logger) {
	m.logger = logger
}

// =============================================================================
// RESPONSE WRAPPER
// =============================================================================

// httpResponseWrapper wraps http.ResponseWriter to capture metrics
type httpResponseWrapper struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

// WriteHeader captures the status code
func (w *httpResponseWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Write captures the number of bytes written
func (w *httpResponseWrapper) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n
	return n, err
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// CreateHTTPMetricsMiddleware creates HTTP metrics middleware for the router
func CreateHTTPMetricsMiddleware(collector metrics.MetricsCollector) common.MiddlewareDefinition {
	middleware := NewHTTPMetricsMiddleware(collector)
	return middleware.MiddlewareDefinition()
}

// CreateHTTPMetricsMiddlewareWithConfig creates HTTP metrics middleware with custom config
func CreateHTTPMetricsMiddlewareWithConfig(collector metrics.MetricsCollector, config *HTTPMetricsConfig) common.MiddlewareDefinition {
	middleware := NewHTTPMetricsMiddlewareWithConfig(collector, config)
	return middleware.MiddlewareDefinition()
}

// RegisterHTTPMetricsMiddleware registers HTTP metrics middleware with the router
func RegisterHTTPMetricsMiddleware(router common.Router, collector metrics.MetricsCollector) error {
	middleware := CreateHTTPMetricsMiddleware(collector)
	return router.AddMiddleware(middleware)
}

// RegisterHTTPMetricsMiddlewareWithConfig registers HTTP metrics middleware with custom config
func RegisterHTTPMetricsMiddlewareWithConfig(router common.Router, collector metrics.MetricsCollector, config *HTTPMetricsConfig) error {
	middleware := CreateHTTPMetricsMiddlewareWithConfig(collector, config)
	return router.AddMiddleware(middleware)
}
