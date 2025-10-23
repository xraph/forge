package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	json "github.com/json-iterator/go"
	health "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// HealthMiddleware provides health check middleware functionality
type HealthMiddleware struct {
	healthService health.HealthService
	config        *HealthMiddlewareConfig
	logger        logger.Logger
	metrics       shared.Metrics
}

// HealthMiddlewareConfig contains configuration for health middleware
type HealthMiddlewareConfig struct {
	// Skip health checks for specific paths
	SkipPaths []string `yaml:"skip_paths" json:"skip_paths"`

	// Skip health checks for specific methods
	SkipMethods []string `yaml:"skip_methods" json:"skip_methods"`

	// Skip health checks for specific headers
	SkipHeaders map[string]string `yaml:"skip_headers" json:"skip_headers"`

	// Perform health check on every request
	CheckOnEveryRequest bool `yaml:"check_on_every_request" json:"check_on_every_request"`

	// Minimum interval between health checks
	CheckInterval time.Duration `yaml:"check_interval" json:"check_interval"`

	// Return 503 if unhealthy
	FailOnUnhealthy bool `yaml:"fail_on_unhealthy" json:"fail_on_unhealthy"`

	// Return 503 if degraded
	FailOnDegraded bool `yaml:"fail_on_degraded" json:"fail_on_degraded"`

	// Custom response for unhealthy status
	UnhealthyResponse string `yaml:"unhealthy_response" json:"unhealthy_response"`

	// Custom response for degraded status
	DegradedResponse string `yaml:"degraded_response" json:"degraded_response"`

	// Include health status in response headers
	IncludeHealthHeaders bool `yaml:"include_health_headers" json:"include_health_headers"`

	// Health header name
	HealthHeaderName string `yaml:"health_header_name" json:"health_header_name"`

	// Include detailed health information
	IncludeDetailedHealth bool `yaml:"include_detailed_health" json:"include_detailed_health"`

	// Timeout for health checks
	HealthCheckTimeout time.Duration `yaml:"health_check_timeout" json:"health_check_timeout"`

	// Enable request tracking
	TrackRequests bool `yaml:"track_requests" json:"track_requests"`

	// Enable performance monitoring
	MonitorPerformance bool `yaml:"monitor_performance" json:"monitor_performance"`
}

// DefaultHealthMiddlewareConfig returns default configuration
func DefaultHealthMiddlewareConfig() *HealthMiddlewareConfig {
	return &HealthMiddlewareConfig{
		SkipPaths:             []string{"/health", "/metrics", "/favicon.ico"},
		SkipMethods:           []string{"OPTIONS"},
		SkipHeaders:           make(map[string]string),
		CheckOnEveryRequest:   false,
		CheckInterval:         30 * time.Second,
		FailOnUnhealthy:       true,
		FailOnDegraded:        false,
		UnhealthyResponse:     "Service temporarily unavailable",
		DegradedResponse:      "Service operating in degraded mode",
		IncludeHealthHeaders:  true,
		HealthHeaderName:      "X-Health-Status",
		IncludeDetailedHealth: false,
		HealthCheckTimeout:    5 * time.Second,
		TrackRequests:         true,
		MonitorPerformance:    true,
	}
}

// NewHealthMiddleware creates a new health middleware
func NewHealthMiddleware(healthService health.HealthService, config *HealthMiddlewareConfig, logger logger.Logger, metrics shared.Metrics) *HealthMiddleware {
	if config == nil {
		config = DefaultHealthMiddlewareConfig()
	}

	return &HealthMiddleware{
		healthService: healthService,
		config:        config,
		logger:        logger,
		metrics:       metrics,
	}
}

// Handler returns the middleware handler function
func (hm *HealthMiddleware) Handler() func(http.Handler) http.Handler {
	var lastHealthCheck time.Time
	var lastHealthStatus health.HealthStatus

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Check if we should skip health checks for this request
			if hm.shouldSkipHealthCheck(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Get current health status
			var currentStatus health.HealthStatus
			var shouldPerformCheck bool

			if hm.config.CheckOnEveryRequest {
				shouldPerformCheck = true
			} else {
				// Check if enough time has passed since last health check
				if time.Since(lastHealthCheck) > hm.config.CheckInterval {
					shouldPerformCheck = true
				} else {
					currentStatus = lastHealthStatus
				}
			}

			if shouldPerformCheck {
				// Create context with timeout for health check
				_, cancel := context.WithTimeout(r.Context(), hm.config.HealthCheckTimeout)
				defer cancel()

				// Perform health check
				if hm.healthService != nil {
					currentStatus = hm.healthService.GetStatus()

					// Update cache
					lastHealthCheck = time.Now()
					lastHealthStatus = currentStatus

					// Record metrics
					if hm.metrics != nil {
						hm.metrics.Counter("forge.health.middleware_checks").Inc()
						hm.metrics.Gauge("forge.health.middleware_status").Set(float64(currentStatus.Severity()))
					}
				} else {
					currentStatus = health.HealthStatusUnknown
				}
			}

			// Add health headers if enabled
			if hm.config.IncludeHealthHeaders {
				w.Header().Set(hm.config.HealthHeaderName, string(currentStatus))
			}

			// Check if we should fail the request based on health status
			if hm.shouldFailRequest(currentStatus) {
				hm.handleUnhealthyRequest(w, r, currentStatus)
				return
			}

			// Track request if enabled
			if hm.config.TrackRequests {
				hm.trackRequest(r, currentStatus)
			}

			// Wrap response writer for monitoring
			var wrappedWriter http.ResponseWriter = w
			if hm.config.MonitorPerformance {
				wrappedWriter = &responseWriterWrapper{
					ResponseWriter: w,
					statusCode:     200,
				}
			}

			// Continue with request
			next.ServeHTTP(wrappedWriter, r)

			// Record performance metrics
			if hm.config.MonitorPerformance {
				duration := time.Since(start)
				if hm.metrics != nil {
					hm.metrics.Counter("forge.health.middleware_requests").Inc()
					hm.metrics.Histogram("forge.health.middleware_duration").Observe(duration.Seconds())

					if wrapper, ok := wrappedWriter.(*responseWriterWrapper); ok {
						hm.metrics.Histogram("forge.health.middleware_response_status").Observe(float64(wrapper.statusCode))
					}
				}
			}
		})
	}
}

// shouldSkipHealthCheck determines if health check should be skipped
func (hm *HealthMiddleware) shouldSkipHealthCheck(r *http.Request) bool {
	// Check skip paths
	for _, path := range hm.config.SkipPaths {
		if strings.HasPrefix(r.URL.Path, path) {
			return true
		}
	}

	// Check skip methods
	for _, method := range hm.config.SkipMethods {
		if r.Method == method {
			return true
		}
	}

	// Check skip headers
	for header, value := range hm.config.SkipHeaders {
		if r.Header.Get(header) == value {
			return true
		}
	}

	return false
}

// shouldFailRequest determines if request should fail based on health status
func (hm *HealthMiddleware) shouldFailRequest(status health.HealthStatus) bool {
	switch status {
	case health.HealthStatusUnhealthy:
		return hm.config.FailOnUnhealthy
	case health.HealthStatusDegraded:
		return hm.config.FailOnDegraded
	default:
		return false
	}
}

// handleUnhealthyRequest handles requests when service is unhealthy
func (hm *HealthMiddleware) handleUnhealthyRequest(w http.ResponseWriter, r *http.Request, status health.HealthStatus) {
	var message string

	switch status {
	case health.HealthStatusUnhealthy:
		message = hm.config.UnhealthyResponse
	case health.HealthStatusDegraded:
		message = hm.config.DegradedResponse
	default:
		message = "Service unavailable"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)

	response := map[string]interface{}{
		"error":     message,
		"status":    string(status),
		"timestamp": time.Now().Format(time.RFC3339),
	}

	if hm.config.IncludeDetailedHealth && hm.healthService != nil {
		if report := hm.healthService.GetLastReport(); report != nil {
			response["health_report"] = report
		}
	}

	// Write JSON response
	if err := writeJSON(w, response); err != nil {
		if hm.logger != nil {
			hm.logger.Error("failed to write unhealthy response",
				logger.Error(err),
			)
		}
	}

	// Record metrics
	if hm.metrics != nil {
		hm.metrics.Counter("forge.health.middleware_rejected_requests").Inc()
	}

	// Log request rejection
	if hm.logger != nil {
		hm.logger.Warn("request rejected due to health status",
			logger.String("path", r.URL.Path),
			logger.String("method", r.Method),
			logger.String("status", string(status)),
			logger.String("remote_addr", r.RemoteAddr),
		)
	}
}

// trackRequest tracks request information
func (hm *HealthMiddleware) trackRequest(r *http.Request, status health.HealthStatus) {
	if hm.logger != nil {
		hm.logger.Debug("request tracked",
			logger.String("path", r.URL.Path),
			logger.String("method", r.Method),
			logger.String("health_status", string(status)),
			logger.String("user_agent", r.UserAgent()),
			logger.String("remote_addr", r.RemoteAddr),
		)
	}
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}

// ReadinessMiddleware provides readiness check middleware
type ReadinessMiddleware struct {
	healthService health.HealthService
	config        *ReadinessMiddlewareConfig
	logger        logger.Logger
	metrics       shared.Metrics
}

// ReadinessMiddlewareConfig contains configuration for readiness middleware
type ReadinessMiddlewareConfig struct {
	// Only allow requests when service is ready
	StrictReadiness bool `yaml:"strict_readiness" json:"strict_readiness"`

	// Grace period after startup before enforcing readiness
	GracePeriod time.Duration `yaml:"grace_period" json:"grace_period"`

	// Custom response for not ready status
	NotReadyResponse string `yaml:"not_ready_response" json:"not_ready_response"`

	// Paths to check readiness for
	CheckPaths []string `yaml:"check_paths" json:"check_paths"`

	// Skip readiness check for specific paths
	SkipPaths []string `yaml:"skip_paths" json:"skip_paths"`
}

// DefaultReadinessMiddlewareConfig returns default configuration
func DefaultReadinessMiddlewareConfig() *ReadinessMiddlewareConfig {
	return &ReadinessMiddlewareConfig{
		StrictReadiness:  true,
		GracePeriod:      30 * time.Second,
		NotReadyResponse: "Service not ready",
		CheckPaths:       []string{"/"},
		SkipPaths:        []string{"/health", "/metrics", "/ready"},
	}
}

// NewReadinessMiddleware creates a new readiness middleware
func NewReadinessMiddleware(healthService health.HealthService, config *ReadinessMiddlewareConfig, logger logger.Logger, metrics shared.Metrics) *ReadinessMiddleware {
	if config == nil {
		config = DefaultReadinessMiddlewareConfig()
	}

	return &ReadinessMiddleware{
		healthService: healthService,
		config:        config,
		logger:        logger,
		metrics:       metrics,
	}
}

// Handler returns the readiness middleware handler function
func (rm *ReadinessMiddleware) Handler() func(http.Handler) http.Handler {
	startTime := time.Now()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if we should skip readiness check
			if rm.shouldSkipReadinessCheck(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Check if we're in grace period
			if time.Since(startTime) < rm.config.GracePeriod {
				next.ServeHTTP(w, r)
				return
			}

			// Check if service is ready
			if rm.config.StrictReadiness && rm.healthService != nil {
				status := rm.healthService.GetStatus()
				if status != health.HealthStatusHealthy {
					rm.handleNotReadyRequest(w, r, status)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// shouldSkipReadinessCheck determines if readiness check should be skipped
func (rm *ReadinessMiddleware) shouldSkipReadinessCheck(r *http.Request) bool {
	// Check skip paths
	for _, path := range rm.config.SkipPaths {
		if strings.HasPrefix(r.URL.Path, path) {
			return true
		}
	}

	// Check if path is in check paths
	if len(rm.config.CheckPaths) > 0 {
		found := false
		for _, path := range rm.config.CheckPaths {
			if strings.HasPrefix(r.URL.Path, path) {
				found = true
				break
			}
		}
		return !found
	}

	return false
}

// handleNotReadyRequest handles requests when service is not ready
func (rm *ReadinessMiddleware) handleNotReadyRequest(w http.ResponseWriter, r *http.Request, status health.HealthStatus) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)

	response := map[string]interface{}{
		"error":     rm.config.NotReadyResponse,
		"status":    string(status),
		"ready":     false,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	if err := writeJSON(w, response); err != nil {
		if rm.logger != nil {
			rm.logger.Error("failed to write not ready response",
				logger.Error(err),
			)
		}
	}

	// Record metrics
	if rm.metrics != nil {
		rm.metrics.Counter("forge.health.readiness_rejected_requests").Inc()
	}

	// Log request rejection
	if rm.logger != nil {
		rm.logger.Warn("request rejected due to readiness check",
			logger.String("path", r.URL.Path),
			logger.String("method", r.Method),
			logger.String("status", string(status)),
			logger.String("remote_addr", r.RemoteAddr),
		)
	}
}

// LivenessMiddleware provides liveness check middleware
type LivenessMiddleware struct {
	config  *LivenessMiddlewareConfig
	logger  logger.Logger
	metrics shared.Metrics
}

// LivenessMiddlewareConfig contains configuration for liveness middleware
type LivenessMiddlewareConfig struct {
	// Add liveness headers to responses
	AddLivenessHeaders bool `yaml:"add_liveness_headers" json:"add_liveness_headers"`

	// Liveness header name
	LivenessHeaderName string `yaml:"liveness_header_name" json:"liveness_header_name"`

	// Include uptime in headers
	IncludeUptime bool `yaml:"include_uptime" json:"include_uptime"`

	// Uptime header name
	UptimeHeaderName string `yaml:"uptime_header_name" json:"uptime_header_name"`
}

// DefaultLivenessMiddlewareConfig returns default configuration
func DefaultLivenessMiddlewareConfig() *LivenessMiddlewareConfig {
	return &LivenessMiddlewareConfig{
		AddLivenessHeaders: true,
		LivenessHeaderName: "X-Liveness-Status",
		IncludeUptime:      true,
		UptimeHeaderName:   "X-Uptime",
	}
}

// NewLivenessMiddleware creates a new liveness middleware
func NewLivenessMiddleware(config *LivenessMiddlewareConfig, logger logger.Logger, metrics shared.Metrics) *LivenessMiddleware {
	if config == nil {
		config = DefaultLivenessMiddlewareConfig()
	}

	return &LivenessMiddleware{
		config:  config,
		logger:  logger,
		metrics: metrics,
	}
}

// Handler returns the liveness middleware handler function
func (lm *LivenessMiddleware) Handler() func(http.Handler) http.Handler {
	startTime := time.Now()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add liveness headers
			if lm.config.AddLivenessHeaders {
				w.Header().Set(lm.config.LivenessHeaderName, "alive")
			}

			// Add uptime header
			if lm.config.IncludeUptime {
				uptime := time.Since(startTime)
				w.Header().Set(lm.config.UptimeHeaderName, uptime.String())
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeJSON writes JSON response
func writeJSON(w http.ResponseWriter, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(data)
}

// CreateHealthMiddleware creates a health middleware with default configuration
func CreateHealthMiddleware(healthService health.HealthService, logger logger.Logger, metrics shared.Metrics) *HealthMiddleware {
	return NewHealthMiddleware(healthService, DefaultHealthMiddlewareConfig(), logger, metrics)
}

// CreateReadinessMiddleware creates a readiness middleware with default configuration
func CreateReadinessMiddleware(healthService health.HealthService, logger logger.Logger, metrics shared.Metrics) *ReadinessMiddleware {
	return NewReadinessMiddleware(healthService, DefaultReadinessMiddlewareConfig(), logger, metrics)
}

// CreateLivenessMiddleware creates a liveness middleware with default configuration
func CreateLivenessMiddleware(logger logger.Logger, metrics shared.Metrics) *LivenessMiddleware {
	return NewLivenessMiddleware(DefaultLivenessMiddlewareConfig(), logger, metrics)
}
