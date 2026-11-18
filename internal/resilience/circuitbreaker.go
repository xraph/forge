package resilience

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/metrics"
)

// CircuitBreaker provides circuit breaker functionality.
//
// The mutex covers admission and result recording only, never the protected
// call: callers run concurrently, and a slow call cannot hold up a caller whose
// context has already given up.
type CircuitBreaker struct {
	config  CircuitBreakerConfig
	state   CircuitState
	stats   CircuitStats // cumulative, for GetStats
	mu      sync.RWMutex
	logger  logger.Logger
	metrics shared.Metrics

	// window counts the results recorded since the last state change, and
	// is what the state decisions read. Cumulative stats never reset, so a
	// half-open breaker reading them judged its few probes against the whole
	// history and never closed again.
	window windowCounts

	// generation increments on every state change. A call records into the
	// window only if the state has not changed since it was admitted, so a
	// slow call from before a trip cannot sway the next state's decision.
	generation uint64

	// halfOpenInFlight counts admitted half-open probes still running.
	halfOpenInFlight int

	// openedAt is when the breaker last opened; it may try again once
	// RecoveryTimeout has passed.
	openedAt time.Time

	// handles holds the metric instruments, looked up once rather than
	// through the registry on every call.
	handles *cbMetricHandles
}

// windowCounts counts results within one state.
type windowCounts struct {
	total     int64
	failures  int64
	successes int64
}

// cbMetricHandles are a breaker's metric instruments, one per label set.
type cbMetricHandles struct {
	requests     [3][2]metrics.Counter // [state][result]
	duration     [3]metrics.Histogram  // [state]
	stateChanges metrics.Counter
}

// CircuitBreakerConfig contains circuit breaker configuration.
type CircuitBreakerConfig struct {
	Name                string         `yaml:"name"`
	MaxRequests         int            `default:"10"   yaml:"max_requests"`
	Timeout             time.Duration  `default:"60s"  yaml:"timeout"`
	MaxFailures         int            `default:"5"    yaml:"max_failures"`
	FailureThreshold    float64        `default:"0.5"  yaml:"failure_threshold"`
	SuccessThreshold    float64        `default:"0.8"  yaml:"success_threshold"`
	RecoveryTimeout     time.Duration  `default:"30s"  yaml:"recovery_timeout"`
	HalfOpenMaxRequests int            `default:"3"    yaml:"half_open_max_requests"`
	EnableMetrics       bool           `default:"true" yaml:"enable_metrics"`
	Logger              logger.Logger  `yaml:"-"`
	Metrics             shared.Metrics `yaml:"-"`
}

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitStateClosed CircuitState = iota
	CircuitStateOpen
	CircuitStateHalfOpen
)

// CircuitStats represents circuit breaker statistics.
type CircuitStats struct {
	TotalRequests      int64     `json:"total_requests"`
	SuccessfulRequests int64     `json:"successful_requests"`
	FailedRequests     int64     `json:"failed_requests"`
	StateChanges       int64     `json:"state_changes"`
	LastStateChange    time.Time `json:"last_state_change"`
	LastFailure        time.Time `json:"last_failure"`
	LastSuccess        time.Time `json:"last_success"`
}

// CircuitBreakerError represents a circuit breaker error.
type CircuitBreakerError struct {
	State   CircuitState `json:"state"`
	Message string       `json:"message"`
	Time    time.Time    `json:"time"`
}

func (e *CircuitBreakerError) Error() string {
	return fmt.Sprintf("circuit breaker %s: %s", e.State.String(), e.Message)
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	cb := &CircuitBreaker{
		config: config,
		state:  CircuitStateClosed,
		stats: CircuitStats{
			LastStateChange: time.Now(),
		},
		logger:  config.Logger,
		metrics: config.Metrics,
	}
	cb.handles = cb.buildHandles()

	return cb
}

// Execute executes a function with circuit breaker protection.
//
// A context that is already done is returned as an error without running fn
// or counting against the breaker. fn itself takes no context, so a call that
// has started runs to completion.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ticket, err := cb.admit()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	result, err := fn()
	duration := time.Since(start)

	changes, handles := cb.record(ticket, err)

	if handles != nil {
		handles.requests[ticket.state][resultIndex(err)].Inc()
		handles.duration[ticket.state].Observe(duration.Seconds())

		for range changes {
			handles.stateChanges.Inc()
		}
	}

	return result, err
}

// admission is what a call needs to record its result.
type admission struct {
	state      CircuitState
	generation uint64
}

// admit decides whether a call may run, moving an open breaker to half-open
// once its recovery timeout has passed.
func (cb *CircuitBreaker) admit() (admission, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitStateOpen {
		if time.Since(cb.openedAt) < cb.config.RecoveryTimeout {
			return admission{}, &CircuitBreakerError{
				State:   CircuitStateOpen,
				Message: "circuit is open",
				Time:    time.Now(),
			}
		}

		cb.setState(CircuitStateHalfOpen)
	}

	if cb.state == CircuitStateHalfOpen {
		if cb.window.total+int64(cb.halfOpenInFlight) >= int64(cb.halfOpenMax()) {
			return admission{}, &CircuitBreakerError{
				State:   CircuitStateHalfOpen,
				Message: "half-open request limit reached",
				Time:    time.Now(),
			}
		}

		cb.halfOpenInFlight++
	}

	return admission{state: cb.state, generation: cb.generation}, nil
}

// record counts a finished call and applies any state change it causes. It
// returns the number of state changes and the metric handles to record with,
// so the metrics are written after the lock is released.
func (cb *CircuitBreaker) record(t admission, err error) (int, *cbMetricHandles) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	cb.stats.TotalRequests++
	if err != nil {
		cb.stats.FailedRequests++
		cb.stats.LastFailure = now
	} else {
		cb.stats.SuccessfulRequests++
		cb.stats.LastSuccess = now
	}

	before := cb.stats.StateChanges

	if t.generation == cb.generation {
		if t.state == CircuitStateHalfOpen {
			cb.halfOpenInFlight--
		}

		cb.window.total++
		if err != nil {
			cb.window.failures++
		} else {
			cb.window.successes++
		}

		cb.updateState()
	}

	var handles *cbMetricHandles
	if cb.config.EnableMetrics {
		handles = cb.handles
	}

	return int(cb.stats.StateChanges - before), handles
}

// setState sets the circuit breaker state and starts a fresh window.
func (cb *CircuitBreaker) setState(newState CircuitState) {
	if cb.state == newState {
		return
	}

	old := cb.state

	cb.state = newState
	cb.generation++
	cb.window = windowCounts{}
	cb.halfOpenInFlight = 0
	cb.stats.StateChanges++
	cb.stats.LastStateChange = time.Now()

	if newState == CircuitStateOpen {
		cb.openedAt = cb.stats.LastStateChange
	}

	cb.logger.Info("circuit breaker state changed",
		logger.String("name", cb.config.Name),
		logger.String("old_state", old.String()),
		logger.String("new_state", newState.String()))
}

// updateState updates the circuit breaker state based on the current window.
func (cb *CircuitBreaker) updateState() {
	switch cb.state {
	case CircuitStateClosed:
		if cb.shouldOpen() {
			cb.setState(CircuitStateOpen)
		}
	case CircuitStateHalfOpen:
		// The probes decide as soon as the outcome is settled: open once too
		// many have failed to reach SuccessThreshold, close once enough have
		// succeeded.
		limit := int64(cb.halfOpenMax())
		need := int64(math.Ceil(cb.config.SuccessThreshold * float64(limit)))

		switch {
		case cb.window.failures > limit-need:
			cb.setState(CircuitStateOpen)
		case cb.window.successes >= need:
			cb.setState(CircuitStateClosed)
		}
	case CircuitStateOpen:
		// Results of calls admitted before the trip are not counted (see
		// generation), so nothing here.
	}
}

// shouldOpen determines if a closed circuit should be opened.
func (cb *CircuitBreaker) shouldOpen() bool {
	if cb.window.total < int64(cb.config.MaxRequests) {
		return false
	}

	failureRate := float64(cb.window.failures) / float64(cb.window.total)

	return failureRate >= cb.config.FailureThreshold
}

// halfOpenMax is how many probes a half-open breaker admits, at least one.
func (cb *CircuitBreaker) halfOpenMax() int {
	return max(cb.config.HalfOpenMaxRequests, 1)
}

// buildHandles looks up the metric instruments once.
func (cb *CircuitBreaker) buildHandles() *cbMetricHandles {
	if cb.metrics == nil || !cb.config.EnableMetrics {
		return nil
	}

	name := metrics.WithLabel("name", cb.config.Name)
	h := &cbMetricHandles{
		stateChanges: cb.metrics.Counter("circuit_breaker_state_changes_total", name),
	}

	for _, st := range []CircuitState{CircuitStateClosed, CircuitStateOpen, CircuitStateHalfOpen} {
		state := metrics.WithLabel("state", st.String())

		for i, res := range []string{"success", "error"} {
			h.requests[st][i] = cb.metrics.Counter("circuit_breaker_requests_total", name, state, metrics.WithLabel("result", res))
		}

		h.duration[st] = cb.metrics.Histogram("circuit_breaker_request_duration_seconds", name, state)
	}

	return h
}

// resultIndex maps a call's error to the result label's index in
// cbMetricHandles.requests.
func resultIndex(err error) int {
	if err != nil {
		return 1
	}

	return 0
}

// GetState returns the current circuit breaker state.
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return cb.state
}

// GetStats returns the circuit breaker statistics.
func (cb *CircuitBreaker) GetStats() CircuitStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return cb.stats
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitStateClosed
	cb.generation++
	cb.window = windowCounts{}
	cb.halfOpenInFlight = 0
	cb.stats = CircuitStats{
		LastStateChange: time.Now(),
	}
}

// IsOpen returns true if the circuit is open.
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.GetState() == CircuitStateOpen
}

// IsClosed returns true if the circuit is closed.
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.GetState() == CircuitStateClosed
}

// IsHalfOpen returns true if the circuit is half-open.
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.GetState() == CircuitStateHalfOpen
}

// GetConfig returns the circuit breaker configuration.
func (cb *CircuitBreaker) GetConfig() CircuitBreakerConfig {
	return cb.config
}

// UpdateConfig updates the circuit breaker configuration.
func (cb *CircuitBreaker) UpdateConfig(config CircuitBreakerConfig) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if config.Logger == nil {
		config.Logger = cb.logger
	}

	if config.Metrics == nil {
		config.Metrics = cb.metrics
	}

	cb.config = config
	cb.logger = config.Logger
	cb.metrics = config.Metrics
	cb.handles = cb.buildHandles()
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
