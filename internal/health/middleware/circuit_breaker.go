package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	health "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitStateClosed CircuitState = iota
	CircuitStateOpen
	CircuitStateHalfOpen
)

// String returns the string representation of the circuit state
func (cs CircuitState) String() string {
	switch cs {
	case CircuitStateClosed:
		return "closed"
	case CircuitStateOpen:
		return "open"
	case CircuitStateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerMiddleware provides circuit breaker functionality based on health status
type CircuitBreakerMiddleware struct {
	healthService health.HealthService
	config        *CircuitBreakerConfig
	logger        logger.Logger
	metrics       shared.Metrics

	// Circuit breaker state
	state          CircuitState
	failures       int
	successes      int
	lastFailure    time.Time
	lastSuccess    time.Time
	lastTransition time.Time

	// Statistics
	totalRequests      int64
	successfulRequests int64
	failedRequests     int64
	rejectedRequests   int64

	mu sync.RWMutex
}

// CircuitBreakerConfig contains configuration for circuit breaker
type CircuitBreakerConfig struct {
	// Failure threshold to open circuit
	FailureThreshold int `yaml:"failure_threshold" json:"failure_threshold"`

	// Success threshold to close circuit from half-open
	SuccessThreshold int `yaml:"success_threshold" json:"success_threshold"`

	// Timeout before transitioning from open to half-open
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// Maximum number of concurrent requests in half-open state
	MaxConcurrentRequests int `yaml:"max_concurrent_requests" json:"max_concurrent_requests"`

	// Health statuses that should trigger circuit opening
	TriggerStatuses []health.HealthStatus `yaml:"trigger_statuses" json:"trigger_statuses"`

	// Enable health-based circuit breaking
	EnableHealthBased bool `yaml:"enable_health_based" json:"enable_health_based"`

	// Health check interval for circuit decisions
	HealthCheckInterval time.Duration `yaml:"health_check_interval" json:"health_check_interval"`

	// Force open circuit if health is unhealthy
	ForceOpenOnUnhealthy bool `yaml:"force_open_on_unhealthy" json:"force_open_on_unhealthy"`

	// Force close circuit if health is healthy
	ForceCloseOnHealthy bool `yaml:"force_close_on_healthy" json:"force_close_on_healthy"`

	// Enable request-based circuit breaking
	EnableRequestBased bool `yaml:"enable_request_based" json:"enable_request_based"`

	// Time window for failure counting
	FailureWindow time.Duration `yaml:"failure_window" json:"failure_window"`

	// Minimum number of requests before considering circuit opening
	MinimumRequests int `yaml:"minimum_requests" json:"minimum_requests"`

	// Error rate threshold (0.0 to 1.0)
	ErrorRateThreshold float64 `yaml:"error_rate_threshold" json:"error_rate_threshold"`

	// Skip circuit breaker for specific paths
	SkipPaths []string `yaml:"skip_paths" json:"skip_paths"`

	// Skip circuit breaker for specific methods
	SkipMethods []string `yaml:"skip_methods" json:"skip_methods"`

	// Custom response for circuit open
	OpenResponse string `yaml:"open_response" json:"open_response"`

	// Include circuit state in headers
	IncludeStateHeaders bool `yaml:"include_state_headers" json:"include_state_headers"`

	// State header name
	StateHeaderName string `yaml:"state_header_name" json:"state_header_name"`

	// Enable detailed metrics
	EnableMetrics bool `yaml:"enable_metrics" json:"enable_metrics"`

	// Enable state change notifications
	EnableNotifications bool `yaml:"enable_notifications" json:"enable_notifications"`
}

// DefaultCircuitBreakerConfig returns default configuration
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold:      5,
		SuccessThreshold:      3,
		Timeout:               60 * time.Second,
		MaxConcurrentRequests: 10,
		TriggerStatuses: []health.HealthStatus{
			health.HealthStatusUnhealthy,
			health.HealthStatusDegraded,
		},
		EnableHealthBased:    true,
		HealthCheckInterval:  30 * time.Second,
		ForceOpenOnUnhealthy: true,
		ForceCloseOnHealthy:  false,
		EnableRequestBased:   true,
		FailureWindow:        120 * time.Second,
		MinimumRequests:      10,
		ErrorRateThreshold:   0.5,
		SkipPaths:            []string{"/health", "/metrics", "/ready"},
		SkipMethods:          []string{"OPTIONS"},
		OpenResponse:         "Service temporarily unavailable due to circuit breaker",
		IncludeStateHeaders:  true,
		StateHeaderName:      "X-Circuit-State",
		EnableMetrics:        true,
		EnableNotifications:  false,
	}
}

// NewCircuitBreakerMiddleware creates a new circuit breaker middleware
func NewCircuitBreakerMiddleware(healthService health.HealthService, config *CircuitBreakerConfig, logger logger.Logger, metrics shared.Metrics) *CircuitBreakerMiddleware {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	cbm := &CircuitBreakerMiddleware{
		healthService:  healthService,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		state:          CircuitStateClosed,
		lastTransition: time.Now(),
	}

	// Start health monitoring goroutine if enabled
	if config.EnableHealthBased && healthService != nil {
		go cbm.healthMonitor()
	}

	return cbm
}

// Handler returns the circuit breaker middleware handler function
func (cbm *CircuitBreakerMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if we should skip circuit breaker
			if cbm.shouldSkipCircuitBreaker(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Check circuit state
			state := cbm.GetState()

			// Add state headers if enabled
			if cbm.config.IncludeStateHeaders {
				w.Header().Set(cbm.config.StateHeaderName, state.String())
			}

			// Handle based on circuit state
			switch state {
			case CircuitStateOpen:
				cbm.handleOpenCircuit(w, r)
				return

			case CircuitStateHalfOpen:
				if !cbm.canProceedHalfOpen() {
					cbm.handleOpenCircuit(w, r)
					return
				}
			}

			// Proceed with request
			start := time.Now()
			wrapped := &responseWriterWrapper{
				ResponseWriter: w,
				statusCode:     200,
			}

			next.ServeHTTP(wrapped, r)

			// Record result
			duration := time.Since(start)
			success := wrapped.statusCode < 500

			cbm.recordResult(success, duration)

			// Update metrics
			if cbm.config.EnableMetrics && cbm.metrics != nil {
				cbm.updateMetrics(success, duration)
			}
		})
	}
}

// shouldSkipCircuitBreaker determines if circuit breaker should be skipped
func (cbm *CircuitBreakerMiddleware) shouldSkipCircuitBreaker(r *http.Request) bool {
	// Check skip paths
	for _, path := range cbm.config.SkipPaths {
		if strings.HasPrefix(r.URL.Path, path) {
			return true
		}
	}

	// Check skip methods
	for _, method := range cbm.config.SkipMethods {
		if r.Method == method {
			return true
		}
	}

	return false
}

// handleOpenCircuit handles requests when circuit is open
func (cbm *CircuitBreakerMiddleware) handleOpenCircuit(w http.ResponseWriter, r *http.Request) {
	cbm.mu.Lock()
	cbm.rejectedRequests++
	cbm.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)

	response := map[string]interface{}{
		"error":         cbm.config.OpenResponse,
		"circuit_state": "open",
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	if err := writeJSON(w, response); err != nil {
		if cbm.logger != nil {
			cbm.logger.Error("failed to write circuit open response",
				logger.Error(err),
			)
		}
	}

	// Record metrics
	if cbm.config.EnableMetrics && cbm.metrics != nil {
		cbm.metrics.Counter("forge.health.circuit_breaker_rejected").Inc()
	}

	// Log request rejection
	if cbm.logger != nil {
		cbm.logger.Warn("request rejected due to circuit breaker",
			logger.String("path", r.URL.Path),
			logger.String("method", r.Method),
			logger.String("circuit_state", cbm.state.String()),
			logger.String("remote_addr", r.RemoteAddr),
		)
	}
}

// canProceedHalfOpen checks if request can proceed in half-open state
func (cbm *CircuitBreakerMiddleware) canProceedHalfOpen() bool {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	// Simple implementation - in production, use more sophisticated concurrency control
	return cbm.successes < cbm.config.MaxConcurrentRequests
}

// recordResult records the result of a request
func (cbm *CircuitBreakerMiddleware) recordResult(success bool, duration time.Duration) {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cbm.totalRequests++

	if success {
		cbm.successfulRequests++
		cbm.successes++
		cbm.lastSuccess = time.Now()
		cbm.failures = 0 // Reset failure count on success

		// Check if we should close circuit from half-open
		if cbm.state == CircuitStateHalfOpen && cbm.successes >= cbm.config.SuccessThreshold {
			cbm.transitionTo(CircuitStateClosed)
		}
	} else {
		cbm.failedRequests++
		cbm.failures++
		cbm.lastFailure = time.Now()
		cbm.successes = 0 // Reset success count on failure

		// Check if we should open circuit
		if cbm.state == CircuitStateClosed && cbm.shouldOpenCircuit() {
			cbm.transitionTo(CircuitStateOpen)
		} else if cbm.state == CircuitStateHalfOpen {
			cbm.transitionTo(CircuitStateOpen)
		}
	}
}

// shouldOpenCircuit determines if circuit should be opened
func (cbm *CircuitBreakerMiddleware) shouldOpenCircuit() bool {
	if !cbm.config.EnableRequestBased {
		return false
	}

	// Check minimum requests threshold
	if cbm.totalRequests < int64(cbm.config.MinimumRequests) {
		return false
	}

	// Check failure threshold
	if cbm.failures >= cbm.config.FailureThreshold {
		return true
	}

	// Check error rate threshold
	if cbm.totalRequests > 0 {
		errorRate := float64(cbm.failedRequests) / float64(cbm.totalRequests)
		if errorRate >= cbm.config.ErrorRateThreshold {
			return true
		}
	}

	return false
}

// transitionTo transitions circuit to a new state
func (cbm *CircuitBreakerMiddleware) transitionTo(newState CircuitState) {
	oldState := cbm.state
	cbm.state = newState
	cbm.lastTransition = time.Now()

	// Reset counters on state change
	if newState == CircuitStateClosed {
		cbm.failures = 0
		cbm.successes = 0
	} else if newState == CircuitStateOpen {
		cbm.failures = 0
		cbm.successes = 0
	} else if newState == CircuitStateHalfOpen {
		cbm.successes = 0
	}

	// Log state change
	if cbm.logger != nil {
		cbm.logger.Info("circuit breaker state changed",
			logger.String("old_state", oldState.String()),
			logger.String("new_state", newState.String()),
			logger.Int("failures", cbm.failures),
			logger.Int("successes", cbm.successes),
		)
	}

	// Record metrics
	if cbm.config.EnableMetrics && cbm.metrics != nil {
		cbm.metrics.Counter("forge.health.circuit_breaker_state_changes").Inc()
		cbm.metrics.Gauge("forge.health.circuit_breaker_state").Set(float64(newState))
	}

	// Send notification if enabled
	if cbm.config.EnableNotifications {
		cbm.sendStateChangeNotification(oldState, newState)
	}
}

// healthMonitor monitors health status and adjusts circuit state
func (cbm *CircuitBreakerMiddleware) healthMonitor() {
	ticker := time.NewTicker(cbm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cbm.checkHealthAndAdjustCircuit()
		}
	}
}

// checkHealthAndAdjustCircuit checks health status and adjusts circuit accordingly
func (cbm *CircuitBreakerMiddleware) checkHealthAndAdjustCircuit() {
	if cbm.healthService == nil {
		return
	}

	status := cbm.healthService.GetStatus()

	cbm.mu.Lock()
	currentState := cbm.state
	cbm.mu.Unlock()

	// Force open on unhealthy
	if cbm.config.ForceOpenOnUnhealthy && cbm.shouldTriggerOnStatus(status) {
		if currentState != CircuitStateOpen {
			cbm.mu.Lock()
			cbm.transitionTo(CircuitStateOpen)
			cbm.mu.Unlock()
		}
		return
	}

	// Force close on healthy
	if cbm.config.ForceCloseOnHealthy && status == health.HealthStatusHealthy {
		if currentState != CircuitStateClosed {
			cbm.mu.Lock()
			cbm.transitionTo(CircuitStateClosed)
			cbm.mu.Unlock()
		}
		return
	}

	// Check if we should transition from open to half-open
	if currentState == CircuitStateOpen {
		cbm.mu.RLock()
		timeSinceTransition := time.Since(cbm.lastTransition)
		cbm.mu.RUnlock()

		if timeSinceTransition >= cbm.config.Timeout {
			cbm.mu.Lock()
			cbm.transitionTo(CircuitStateHalfOpen)
			cbm.mu.Unlock()
		}
	}
}

// shouldTriggerOnStatus checks if status should trigger circuit opening
func (cbm *CircuitBreakerMiddleware) shouldTriggerOnStatus(status health.HealthStatus) bool {
	for _, triggerStatus := range cbm.config.TriggerStatuses {
		if status == triggerStatus {
			return true
		}
	}
	return false
}

// sendStateChangeNotification sends notification about state change
func (cbm *CircuitBreakerMiddleware) sendStateChangeNotification(oldState, newState CircuitState) {
	// This would integrate with the alerting system
	// For now, just log the change
	if cbm.logger != nil {
		cbm.logger.Info("circuit breaker state change notification",
			logger.String("old_state", oldState.String()),
			logger.String("new_state", newState.String()),
			logger.Time("timestamp", time.Now()),
		)
	}
}

// updateMetrics updates circuit breaker metrics
func (cbm *CircuitBreakerMiddleware) updateMetrics(success bool, duration time.Duration) {
	cbm.metrics.Counter("forge.health.circuit_breaker_requests").Inc()
	cbm.metrics.Histogram("forge.health.circuit_breaker_duration").Observe(duration.Seconds())

	if success {
		cbm.metrics.Counter("forge.health.circuit_breaker_successes").Inc()
	} else {
		cbm.metrics.Counter("forge.health.circuit_breaker_failures").Inc()
	}
}

// GetState returns the current circuit state
func (cbm *CircuitBreakerMiddleware) GetState() CircuitState {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()
	return cbm.state
}

// GetStats returns circuit breaker statistics
func (cbm *CircuitBreakerMiddleware) GetStats() *CircuitBreakerStats {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	return &CircuitBreakerStats{
		State:              cbm.state,
		TotalRequests:      cbm.totalRequests,
		SuccessfulRequests: cbm.successfulRequests,
		FailedRequests:     cbm.failedRequests,
		RejectedRequests:   cbm.rejectedRequests,
		CurrentFailures:    cbm.failures,
		CurrentSuccesses:   cbm.successes,
		LastFailure:        cbm.lastFailure,
		LastSuccess:        cbm.lastSuccess,
		LastTransition:     cbm.lastTransition,
	}
}

// Reset resets the circuit breaker to closed state
func (cbm *CircuitBreakerMiddleware) Reset() {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cbm.transitionTo(CircuitStateClosed)
	cbm.totalRequests = 0
	cbm.successfulRequests = 0
	cbm.failedRequests = 0
	cbm.rejectedRequests = 0
}

// ForceOpen forces the circuit breaker to open state
func (cbm *CircuitBreakerMiddleware) ForceOpen() {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cbm.transitionTo(CircuitStateOpen)
}

// ForceClose forces the circuit breaker to closed state
func (cbm *CircuitBreakerMiddleware) ForceClose() {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cbm.transitionTo(CircuitStateClosed)
}

// CircuitBreakerStats contains circuit breaker statistics
type CircuitBreakerStats struct {
	State              CircuitState `json:"state"`
	TotalRequests      int64        `json:"total_requests"`
	SuccessfulRequests int64        `json:"successful_requests"`
	FailedRequests     int64        `json:"failed_requests"`
	RejectedRequests   int64        `json:"rejected_requests"`
	CurrentFailures    int          `json:"current_failures"`
	CurrentSuccesses   int          `json:"current_successes"`
	LastFailure        time.Time    `json:"last_failure"`
	LastSuccess        time.Time    `json:"last_success"`
	LastTransition     time.Time    `json:"last_transition"`
}

// HealthBasedCircuitBreaker provides a simpler health-only circuit breaker
type HealthBasedCircuitBreaker struct {
	healthService health.HealthService
	config        *HealthBasedCircuitBreakerConfig
	logger        logger.Logger
	metrics       shared.Metrics

	state          CircuitState
	lastTransition time.Time
	mu             sync.RWMutex
}

// HealthBasedCircuitBreakerConfig contains configuration for health-based circuit breaker
type HealthBasedCircuitBreakerConfig struct {
	// Health statuses that trigger circuit opening
	TriggerStatuses []health.HealthStatus `yaml:"trigger_statuses" json:"trigger_statuses"`

	// Health check interval
	CheckInterval time.Duration `yaml:"check_interval" json:"check_interval"`

	// Close circuit when health recovers
	AutoClose bool `yaml:"auto_close" json:"auto_close"`

	// Timeout before checking health for recovery
	RecoveryTimeout time.Duration `yaml:"recovery_timeout" json:"recovery_timeout"`

	// Skip paths
	SkipPaths []string `yaml:"skip_paths" json:"skip_paths"`

	// Response message
	OpenResponse string `yaml:"open_response" json:"open_response"`
}

// DefaultHealthBasedCircuitBreakerConfig returns default configuration
func DefaultHealthBasedCircuitBreakerConfig() *HealthBasedCircuitBreakerConfig {
	return &HealthBasedCircuitBreakerConfig{
		TriggerStatuses: []health.HealthStatus{
			health.HealthStatusUnhealthy,
		},
		CheckInterval:   30 * time.Second,
		AutoClose:       true,
		RecoveryTimeout: 60 * time.Second,
		SkipPaths:       []string{"/health", "/metrics"},
		OpenResponse:    "Service unavailable due to health issues",
	}
}

// NewHealthBasedCircuitBreaker creates a new health-based circuit breaker
func NewHealthBasedCircuitBreaker(healthService health.HealthService, config *HealthBasedCircuitBreakerConfig, logger logger.Logger, metrics shared.Metrics) *HealthBasedCircuitBreaker {
	if config == nil {
		config = DefaultHealthBasedCircuitBreakerConfig()
	}

	hcb := &HealthBasedCircuitBreaker{
		healthService:  healthService,
		config:         config,
		logger:         logger,
		metrics:        metrics,
		state:          CircuitStateClosed,
		lastTransition: time.Now(),
	}

	// Start health monitoring
	go hcb.healthMonitor()

	return hcb
}

// Handler returns the health-based circuit breaker handler
func (hcb *HealthBasedCircuitBreaker) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if should skip
			for _, path := range hcb.config.SkipPaths {
				if strings.HasPrefix(r.URL.Path, path) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Check circuit state
			hcb.mu.RLock()
			state := hcb.state
			hcb.mu.RUnlock()

			if state == CircuitStateOpen {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)

				response := map[string]interface{}{
					"error":         hcb.config.OpenResponse,
					"circuit_state": "open",
					"timestamp":     time.Now().Format(time.RFC3339),
				}

				// nolint:gosec // G104: writeJSON errors are handled by HTTP framework
				writeJSON(w, response)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// healthMonitor monitors health and adjusts circuit state
func (hcb *HealthBasedCircuitBreaker) healthMonitor() {
	ticker := time.NewTicker(hcb.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hcb.checkHealthAndAdjust()
		}
	}
}

// checkHealthAndAdjust checks health and adjusts circuit
func (hcb *HealthBasedCircuitBreaker) checkHealthAndAdjust() {
	if hcb.healthService == nil {
		return
	}

	status := hcb.healthService.GetStatus()

	hcb.mu.Lock()
	defer hcb.mu.Unlock()

	// Check if should open circuit
	shouldOpen := false
	for _, triggerStatus := range hcb.config.TriggerStatuses {
		if status == triggerStatus {
			shouldOpen = true
			break
		}
	}

	if shouldOpen && hcb.state == CircuitStateClosed {
		hcb.state = CircuitStateOpen
		hcb.lastTransition = time.Now()

		if hcb.logger != nil {
			hcb.logger.Warn("circuit breaker opened due to health status",
				logger.String("health_status", string(status)),
			)
		}
	} else if hcb.config.AutoClose && !shouldOpen && hcb.state == CircuitStateOpen {
		// Check if enough time has passed for recovery
		if time.Since(hcb.lastTransition) >= hcb.config.RecoveryTimeout {
			hcb.state = CircuitStateClosed
			hcb.lastTransition = time.Now()

			if hcb.logger != nil {
				hcb.logger.Info("circuit breaker closed due to health recovery",
					logger.String("health_status", string(status)),
				)
			}
		}
	}
}

// GetState returns the current circuit state
func (hcb *HealthBasedCircuitBreaker) GetState() CircuitState {
	hcb.mu.RLock()
	defer hcb.mu.RUnlock()
	return hcb.state
}

// CreateCircuitBreakerMiddleware creates a circuit breaker middleware with default configuration
func CreateCircuitBreakerMiddleware(healthService health.HealthService, logger logger.Logger, metrics shared.Metrics) *CircuitBreakerMiddleware {
	return NewCircuitBreakerMiddleware(healthService, DefaultCircuitBreakerConfig(), logger, metrics)
}

// CreateHealthBasedCircuitBreaker creates a health-based circuit breaker with default configuration
func CreateHealthBasedCircuitBreaker(healthService health.HealthService, logger logger.Logger, metrics shared.Metrics) *HealthBasedCircuitBreaker {
	return NewHealthBasedCircuitBreaker(healthService, DefaultHealthBasedCircuitBreakerConfig(), logger, metrics)
}
