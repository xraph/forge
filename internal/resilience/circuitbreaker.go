package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// CircuitBreaker provides circuit breaker functionality
type CircuitBreaker struct {
	config    CircuitBreakerConfig
	state     CircuitState
	stats     CircuitStats
	mu        sync.RWMutex
	logger    logger.Logger
	metrics   shared.Metrics
	lastError time.Time
}

// CircuitBreakerConfig contains circuit breaker configuration
type CircuitBreakerConfig struct {
	Name                string         `yaml:"name"`
	MaxRequests         int            `yaml:"max_requests" default:"10"`
	Timeout             time.Duration  `yaml:"timeout" default:"60s"`
	MaxFailures         int            `yaml:"max_failures" default:"5"`
	FailureThreshold    float64        `yaml:"failure_threshold" default:"0.5"`
	SuccessThreshold    float64        `yaml:"success_threshold" default:"0.8"`
	RecoveryTimeout     time.Duration  `yaml:"recovery_timeout" default:"30s"`
	HalfOpenMaxRequests int            `yaml:"half_open_max_requests" default:"3"`
	EnableMetrics       bool           `yaml:"enable_metrics" default:"true"`
	Logger              logger.Logger  `yaml:"-"`
	Metrics             shared.Metrics `yaml:"-"`
}

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitStateClosed CircuitState = iota
	CircuitStateOpen
	CircuitStateHalfOpen
)

// CircuitStats represents circuit breaker statistics
type CircuitStats struct {
	TotalRequests      int64     `json:"total_requests"`
	SuccessfulRequests int64     `json:"successful_requests"`
	FailedRequests     int64     `json:"failed_requests"`
	StateChanges       int64     `json:"state_changes"`
	LastStateChange    time.Time `json:"last_state_change"`
	LastFailure        time.Time `json:"last_failure"`
	LastSuccess        time.Time `json:"last_success"`
}

// CircuitBreakerError represents a circuit breaker error
type CircuitBreakerError struct {
	State   CircuitState `json:"state"`
	Message string       `json:"message"`
	Time    time.Time    `json:"time"`
}

func (e *CircuitBreakerError) Error() string {
	return fmt.Sprintf("circuit breaker %s: %s", e.State.String(), e.Message)
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	return &CircuitBreaker{
		config: config,
		state:  CircuitStateClosed,
		stats: CircuitStats{
			LastStateChange: time.Now(),
		},
		logger:  config.Logger,
		metrics: config.Metrics,
	}
}

// Execute executes a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if circuit is open
	if cb.state == CircuitStateOpen {
		if time.Since(cb.lastError) < cb.config.RecoveryTimeout {
			return nil, &CircuitBreakerError{
				State:   CircuitStateOpen,
				Message: "circuit is open",
				Time:    time.Now(),
			}
		}
		// Try to move to half-open state
		cb.setState(CircuitStateHalfOpen)
	}

	// Check if circuit is half-open and we've reached the limit
	if cb.state == CircuitStateHalfOpen && cb.stats.TotalRequests >= int64(cb.config.HalfOpenMaxRequests) {
		return nil, &CircuitBreakerError{
			State:   CircuitStateHalfOpen,
			Message: "half-open request limit reached",
			Time:    time.Now(),
		}
	}

	// Execute the function
	start := time.Now()
	result, err := fn()
	duration := time.Since(start)

	// Update statistics
	cb.stats.TotalRequests++
	if err != nil {
		cb.stats.FailedRequests++
		cb.lastError = time.Now()
		cb.stats.LastFailure = time.Now()
	} else {
		cb.stats.SuccessfulRequests++
		cb.stats.LastSuccess = time.Now()
	}

	// Update circuit state based on results
	cb.updateState()

	// Record metrics
	if cb.config.EnableMetrics && cb.metrics != nil {
		cb.recordMetrics(duration, err)
	}

	return result, err
}

// setState sets the circuit breaker state
func (cb *CircuitBreaker) setState(newState CircuitState) {
	if cb.state != newState {
		cb.state = newState
		cb.stats.StateChanges++
		cb.stats.LastStateChange = time.Now()

		cb.logger.Info("circuit breaker state changed",
			logger.String("name", cb.config.Name),
			logger.String("old_state", cb.state.String()),
			logger.String("new_state", newState.String()))
	}
}

// updateState updates the circuit breaker state based on current statistics
func (cb *CircuitBreaker) updateState() {
	switch cb.state {
	case CircuitStateClosed:
		// Check if we should open the circuit
		if cb.shouldOpen() {
			cb.setState(CircuitStateOpen)
		}
	case CircuitStateHalfOpen:
		// Check if we should close or open the circuit
		if cb.shouldClose() {
			cb.setState(CircuitStateClosed)
		} else if cb.shouldOpen() {
			cb.setState(CircuitStateOpen)
		}
	case CircuitStateOpen:
		// Check if we should move to half-open
		if cb.shouldHalfOpen() {
			cb.setState(CircuitStateHalfOpen)
		}
	}
}

// shouldOpen determines if the circuit should be opened
func (cb *CircuitBreaker) shouldOpen() bool {
	if cb.stats.TotalRequests < int64(cb.config.MaxRequests) {
		return false
	}

	failureRate := float64(cb.stats.FailedRequests) / float64(cb.stats.TotalRequests)
	return failureRate >= cb.config.FailureThreshold
}

// shouldClose determines if the circuit should be closed
func (cb *CircuitBreaker) shouldClose() bool {
	if cb.stats.TotalRequests < int64(cb.config.MaxRequests) {
		return false
	}

	successRate := float64(cb.stats.SuccessfulRequests) / float64(cb.stats.TotalRequests)
	return successRate >= cb.config.SuccessThreshold
}

// shouldHalfOpen determines if the circuit should move to half-open
func (cb *CircuitBreaker) shouldHalfOpen() bool {
	return time.Since(cb.lastError) >= cb.config.RecoveryTimeout
}

// recordMetrics records circuit breaker metrics
func (cb *CircuitBreaker) recordMetrics(duration time.Duration, err error) {
	// Record request metrics
	cb.metrics.Counter("circuit_breaker_requests_total", "name", cb.config.Name, "state", cb.state.String(), "result", cb.getResultLabel(err)).Inc()

	// Record duration metrics
	cb.metrics.Histogram("circuit_breaker_request_duration_seconds", "name", cb.config.Name, "state", cb.state.String()).Observe(duration.Seconds())

	// Record state change metrics
	if cb.stats.StateChanges > 0 {
		cb.metrics.Counter("circuit_breaker_state_changes_total", "name", cb.config.Name).Inc()
	}
}

// getResultLabel returns the result label for metrics
func (cb *CircuitBreaker) getResultLabel(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns the circuit breaker statistics
func (cb *CircuitBreaker) GetStats() CircuitStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.stats
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitStateClosed
	cb.stats = CircuitStats{
		LastStateChange: time.Now(),
	}
}

// IsOpen returns true if the circuit is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.GetState() == CircuitStateOpen
}

// IsClosed returns true if the circuit is closed
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.GetState() == CircuitStateClosed
}

// IsHalfOpen returns true if the circuit is half-open
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.GetState() == CircuitStateHalfOpen
}

// GetConfig returns the circuit breaker configuration
func (cb *CircuitBreaker) GetConfig() CircuitBreakerConfig {
	return cb.config
}

// UpdateConfig updates the circuit breaker configuration
func (cb *CircuitBreaker) UpdateConfig(config CircuitBreakerConfig) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.config = config
}

// String methods for enums

func (s CircuitState) String() string {
	switch s {
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
