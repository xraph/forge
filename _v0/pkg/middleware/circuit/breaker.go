package circuit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/middleware"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// Config contains configuration for circuit breaker
type Config struct {
	// Failure threshold
	FailureThreshold int           `yaml:"failure_threshold" json:"failure_threshold" default:"5"`
	FailureRate      float64       `yaml:"failure_rate" json:"failure_rate" default:"0.5"` // 50%
	MinRequests      int           `yaml:"min_requests" json:"min_requests" default:"10"`
	Timeout          time.Duration `yaml:"timeout" json:"timeout" default:"60s"`

	// Recovery settings
	SuccessThreshold int           `yaml:"success_threshold" json:"success_threshold" default:"3"`
	HalfOpenMaxCalls int           `yaml:"half_open_max_calls" json:"half_open_max_calls" default:"3"`
	ResetTimeout     time.Duration `yaml:"reset_timeout" json:"reset_timeout" default:"60s"`

	// Monitoring
	SlidingWindow    time.Duration `yaml:"sliding_window" json:"sliding_window" default:"60s"`
	MonitoringPeriod time.Duration `yaml:"monitoring_period" json:"monitoring_period" default:"10s"`

	// Response configuration
	ErrorMessage string `yaml:"error_message" json:"error_message" default:"Service temporarily unavailable"`
	ErrorCode    string `yaml:"error_code" json:"error_code" default:"SERVICE_UNAVAILABLE"`

	// Failure detection
	FailureStatusCodes []int    `yaml:"failure_status_codes" json:"failure_status_codes"`
	SkipPaths          []string `yaml:"skip_paths" json:"skip_paths"`

	// Advanced options
	SlowCallThreshold time.Duration `yaml:"slow_call_threshold" json:"slow_call_threshold" default:"10s"`
	SlowCallRate      float64       `yaml:"slow_call_rate" json:"slow_call_rate" default:"0.8"` // 80%
}

// CircuitBreakerMiddleware implements circuit breaker middleware
type CircuitBreakerMiddleware struct {
	*middleware.BaseServiceMiddleware
	config  Config
	breaker *CircuitBreaker
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	config      Config
	state       State
	failures    int
	successes   int
	requests    int
	lastFailAt  time.Time
	lastResetAt time.Time
	stats       *Stats
	mu          sync.RWMutex
}

// Call represents a function call result
type Call struct {
	Success  bool
	Duration time.Duration
	Error    error
}

// NewCircuitBreakerMiddleware creates a new circuit breaker middleware
func NewCircuitBreakerMiddleware(config Config) *CircuitBreakerMiddleware {
	return &CircuitBreakerMiddleware{
		BaseServiceMiddleware: middleware.NewBaseServiceMiddleware("circuit-breaker", 15, []string{"config-manager"}),
		config:                config,
	}
}

// Initialize initializes the circuit breaker middleware
func (cb *CircuitBreakerMiddleware) Initialize(container common.Container) error {
	if err := cb.BaseServiceMiddleware.Initialize(container); err != nil {
		return err
	}

	// Load configuration from container if needed
	if configManager, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		var circuitConfig Config
		if err := configManager.(common.ConfigManager).Bind("middleware.circuit", &circuitConfig); err == nil {
			cb.config = circuitConfig
		}
	}

	// Set defaults
	if cb.config.FailureThreshold == 0 {
		cb.config.FailureThreshold = 5
	}
	if cb.config.FailureRate == 0 {
		cb.config.FailureRate = 0.5
	}
	if cb.config.MinRequests == 0 {
		cb.config.MinRequests = 10
	}
	if cb.config.Timeout == 0 {
		cb.config.Timeout = 60 * time.Second
	}
	if cb.config.SuccessThreshold == 0 {
		cb.config.SuccessThreshold = 3
	}
	if cb.config.HalfOpenMaxCalls == 0 {
		cb.config.HalfOpenMaxCalls = 3
	}
	if cb.config.ResetTimeout == 0 {
		cb.config.ResetTimeout = 60 * time.Second
	}
	if cb.config.SlidingWindow == 0 {
		cb.config.SlidingWindow = 60 * time.Second
	}
	if cb.config.MonitoringPeriod == 0 {
		cb.config.MonitoringPeriod = 10 * time.Second
	}
	if cb.config.ErrorMessage == "" {
		cb.config.ErrorMessage = "Service temporarily unavailable"
	}
	if cb.config.ErrorCode == "" {
		cb.config.ErrorCode = "SERVICE_UNAVAILABLE"
	}
	if cb.config.SlowCallThreshold == 0 {
		cb.config.SlowCallThreshold = 10 * time.Second
	}
	if cb.config.SlowCallRate == 0 {
		cb.config.SlowCallRate = 0.8
	}
	if len(cb.config.FailureStatusCodes) == 0 {
		cb.config.FailureStatusCodes = []int{500, 502, 503, 504}
	}

	// Initialize circuit breaker
	cb.breaker = NewCircuitBreaker(cb.config)

	return nil
}

// Handler returns the circuit breaker handler
func (cb *CircuitBreakerMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Update call count
			cb.UpdateStats(1, 0, 0, nil)

			// Check if path should be skipped
			if cb.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				cb.UpdateStats(0, 0, time.Since(start), nil)
				return
			}

			// Check if circuit breaker allows the call
			if !cb.breaker.AllowRequest() {
				cb.UpdateStats(0, 1, time.Since(start), fmt.Errorf("circuit breaker open"))
				cb.writeCircuitOpenResponse(w)
				return
			}

			// Wrap response writer to capture status code
			wrapper := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Execute the request
			next.ServeHTTP(wrapper, r)

			// Record the call result
			duration := time.Since(start)
			success := cb.isSuccess(wrapper.statusCode, duration)

			call := Call{
				Success:  success,
				Duration: duration,
			}

			if !success {
				call.Error = fmt.Errorf("request failed with status %d", wrapper.statusCode)
			}

			cb.breaker.RecordCall(call)

			// Update middleware statistics
			if success {
				cb.UpdateStats(0, 0, duration, nil)
			} else {
				cb.UpdateStats(0, 1, duration, call.Error)
			}
		})
	}
}

// GetStats returns enhanced circuit breaker statistics
func (cb *CircuitBreakerMiddleware) GetStats() middleware.MiddlewareStats {
	baseStats := cb.BaseServiceMiddleware.GetStats()

	// Add circuit breaker specific stats
	cbStats := cb.breaker.GetStats()
	baseStats.Status = fmt.Sprintf("circuit_%s", cbStats.State.String())

	return baseStats
}

// GetCircuitStats returns detailed circuit breaker statistics
func (cb *CircuitBreakerMiddleware) GetCircuitStats() *Stats {
	return cb.breaker.GetStats()
}

// shouldSkipPath checks if the current path should skip circuit breaking
func (cb *CircuitBreakerMiddleware) shouldSkipPath(path string) bool {
	for _, skipPath := range cb.config.SkipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// isSuccess determines if a call was successful
func (cb *CircuitBreakerMiddleware) isSuccess(statusCode int, duration time.Duration) bool {
	// Check for failure status codes
	for _, failureCode := range cb.config.FailureStatusCodes {
		if statusCode == failureCode {
			return false
		}
	}

	// Check for slow calls
	if duration > cb.config.SlowCallThreshold {
		return false
	}

	// Consider 2xx and 3xx as success
	return statusCode >= 200 && statusCode < 400
}

// writeCircuitOpenResponse writes a circuit breaker open response
func (cb *CircuitBreakerMiddleware) writeCircuitOpenResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    cb.config.ErrorCode,
			"message": cb.config.ErrorMessage,
			"details": map[string]interface{}{
				"circuit_state": cb.breaker.GetState().String(),
				"retry_after":   int(cb.config.ResetTimeout.Seconds()),
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config Config) *CircuitBreaker {
	return &CircuitBreaker{
		config:      config,
		state:       StateClosed,
		lastResetAt: time.Now(),
		stats: &Stats{
			State:          StateClosed,
			StateChangedAt: time.Now(),
		},
	}
}

// AllowRequest checks if a request should be allowed
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if we should transition to half-open
		if now.Sub(cb.lastFailAt) >= cb.config.ResetTimeout {
			cb.setState(StateHalfOpen)
			cb.successes = 0
			cb.requests = 0
			return true
		}
		cb.stats.RejectedCalls++
		return false

	case StateHalfOpen:
		// Allow limited requests in half-open state
		if cb.requests >= cb.config.HalfOpenMaxCalls {
			cb.stats.RejectedCalls++
			return false
		}
		return true

	default:
		return false
	}
}

// RecordCall records the result of a call
func (cb *CircuitBreaker) RecordCall(call Call) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cb.requests++
	cb.stats.TotalRequests++
	cb.stats.TotalLatency += call.Duration

	// Update average latency
	if cb.stats.TotalRequests > 0 {
		cb.stats.AverageLatency = cb.stats.TotalLatency / time.Duration(cb.stats.TotalRequests)
	}

	if call.Success {
		cb.recordSuccess()
		cb.stats.SuccessfulCalls++
		cb.stats.LastSuccessTime = now
	} else {
		cb.recordFailure()
		cb.stats.FailedCalls++
		cb.stats.LastFailureTime = now
	}

	// Check for slow calls
	if call.Duration > cb.config.SlowCallThreshold {
		cb.stats.SlowCalls++
	}

	// Update rates
	if cb.stats.TotalRequests > 0 {
		cb.stats.FailureRate = float64(cb.stats.FailedCalls) / float64(cb.stats.TotalRequests)
		cb.stats.SlowCallRate = float64(cb.stats.SlowCalls) / float64(cb.stats.TotalRequests)
	}

	cb.checkStateTransition()
}

// recordSuccess records a successful call
func (cb *CircuitBreaker) recordSuccess() {
	cb.successes++
	cb.failures = 0
}

// recordFailure records a failed call
func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.successes = 0
	cb.lastFailAt = time.Now()
}

// checkStateTransition checks if the circuit breaker should change state
func (cb *CircuitBreaker) checkStateTransition() {
	switch cb.state {
	case StateClosed:
		// Transition to open if failure threshold is exceeded
		if cb.shouldOpenCircuit() {
			cb.setState(StateOpen)
			cb.stats.CircuitOpenCount++
		}

	case StateHalfOpen:
		// Transition to closed if success threshold is reached
		if cb.successes >= cb.config.SuccessThreshold {
			cb.setState(StateClosed)
			cb.reset()
		} else if cb.failures > 0 {
			// Transition back to open on any failure
			cb.setState(StateOpen)
			cb.stats.CircuitOpenCount++
		}
	}
}

// shouldOpenCircuit determines if the circuit should be opened
func (cb *CircuitBreaker) shouldOpenCircuit() bool {
	// Need minimum number of requests
	if cb.requests < cb.config.MinRequests {
		return false
	}

	// Check failure count threshold
	if cb.failures >= cb.config.FailureThreshold {
		return true
	}

	// Check failure rate threshold
	failureRate := float64(cb.failures) / float64(cb.requests)
	if failureRate >= cb.config.FailureRate {
		return true
	}

	// Check slow call rate threshold
	slowCallRate := float64(cb.stats.SlowCalls) / float64(cb.requests)
	if slowCallRate >= cb.config.SlowCallRate {
		return true
	}

	return false
}

// setState sets the circuit breaker state
func (cb *CircuitBreaker) setState(state State) {
	if cb.state != state {
		cb.state = state
		cb.stats.State = state
		cb.stats.StateChangedAt = time.Now()
	}
}

// reset resets the circuit breaker counters
func (cb *CircuitBreaker) reset() {
	cb.failures = 0
	cb.successes = 0
	cb.requests = 0
	cb.lastResetAt = time.Now()
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() *Stats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Make a copy to avoid race conditions
	statsCopy := *cb.stats
	return &statsCopy
}

// IsCallAllowed checks if a call is allowed (same as AllowRequest but doesn't modify state)
func (cb *CircuitBreaker) IsCallAllowed() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		return now.Sub(cb.lastFailAt) >= cb.config.ResetTimeout
	case StateHalfOpen:
		return cb.requests < cb.config.HalfOpenMaxCalls
	default:
		return false
	}
}

// ForceOpen forces the circuit breaker to open state
func (cb *CircuitBreaker) ForceOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateOpen)
	cb.lastFailAt = time.Now()
}

// ForceClose forces the circuit breaker to closed state
func (cb *CircuitBreaker) ForceClose() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateClosed)
	cb.reset()
}

// ForceClear clears all statistics
func (cb *CircuitBreaker) ForceClear() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateClosed)
	cb.reset()
	cb.stats = &Stats{
		State:          StateClosed,
		StateChangedAt: time.Now(),
	}
}
