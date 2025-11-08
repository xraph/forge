package sdk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	// CircuitStateClosed allows requests through.
	CircuitStateClosed CircuitState = "closed"
	// CircuitStateOpen blocks requests.
	CircuitStateOpen CircuitState = "open"
	// CircuitStateHalfOpen allows limited requests to test recovery.
	CircuitStateHalfOpen CircuitState = "half_open"
)

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	name         string
	maxFailures  int
	resetTimeout time.Duration
	halfOpenMax  int
	logger       forge.Logger
	metrics      forge.Metrics

	mu              sync.RWMutex
	state           CircuitState
	failures        int
	lastFailureTime time.Time
	halfOpenSuccess int
}

// CircuitBreakerConfig configures a circuit breaker.
type CircuitBreakerConfig struct {
	Name         string
	MaxFailures  int           // Failures before opening (default: 5)
	ResetTimeout time.Duration // Time before trying half-open (default: 60s)
	HalfOpenMax  int           // Successful calls in half-open to close (default: 2)
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(config CircuitBreakerConfig, logger forge.Logger, metrics forge.Metrics) *CircuitBreaker {
	if config.MaxFailures == 0 {
		config.MaxFailures = 5
	}

	if config.ResetTimeout == 0 {
		config.ResetTimeout = 60 * time.Second
	}

	if config.HalfOpenMax == 0 {
		config.HalfOpenMax = 2
	}

	return &CircuitBreaker{
		name:         config.Name,
		maxFailures:  config.MaxFailures,
		resetTimeout: config.ResetTimeout,
		halfOpenMax:  config.HalfOpenMax,
		logger:       logger,
		metrics:      metrics,
		state:        CircuitStateClosed,
	}
}

// Execute executes a function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	if !cb.canExecute() {
		if cb.metrics != nil {
			cb.metrics.Counter("forge.ai.sdk.circuit_breaker.rejected", "circuit", cb.name).Inc()
		}

		return fmt.Errorf("circuit breaker %s is open", cb.name)
	}

	err := fn(ctx)
	cb.recordResult(err)

	return err
}

// canExecute checks if the circuit allows execution.
func (cb *CircuitBreaker) canExecute() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitStateClosed:
		return true
	case CircuitStateOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = CircuitStateHalfOpen

			cb.halfOpenSuccess = 0
			if cb.logger != nil {
				cb.logger.Info("Circuit breaker transitioning to half-open", F("circuit", cb.name))
			}

			return true
		}

		return false
	case CircuitStateHalfOpen:
		return true
	default:
		return false
	}
}

// recordResult records the result of an execution.
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.lastFailureTime = time.Now()

		if cb.state == CircuitStateHalfOpen {
			// Any failure in half-open goes back to open
			cb.state = CircuitStateOpen
			if cb.logger != nil {
				cb.logger.Warn("Circuit breaker reopening after half-open failure", F("circuit", cb.name))
			}
		} else if cb.failures >= cb.maxFailures {
			cb.state = CircuitStateOpen
			if cb.logger != nil {
				cb.logger.Warn("Circuit breaker opening",
					F("circuit", cb.name),
					F("failures", cb.failures),
				)
			}
		}

		if cb.metrics != nil {
			cb.metrics.Counter("forge.ai.sdk.circuit_breaker.failures", "circuit", cb.name).Inc()
		}
	} else {
		// Success
		switch cb.state {
		case CircuitStateHalfOpen:
			cb.halfOpenSuccess++
			if cb.halfOpenSuccess >= cb.halfOpenMax {
				cb.state = CircuitStateClosed

				cb.failures = 0
				if cb.logger != nil {
					cb.logger.Info("Circuit breaker closed", F("circuit", cb.name))
				}
			}
		case CircuitStateClosed:
			cb.failures = 0 // Reset failures on success
		}

		if cb.metrics != nil {
			cb.metrics.Counter("forge.ai.sdk.circuit_breaker.successes", "circuit", cb.name).Inc()
		}
	}
}

// GetState returns the current circuit state.
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return cb.state
}

// Reset manually resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitStateClosed
	cb.failures = 0
	cb.halfOpenSuccess = 0
}

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	Multiplier      float64
	Jitter          bool
	RetryableErrors []error
}

// DefaultRetryConfig returns a sensible default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
		Jitter:       true,
	}
}

// Retry executes a function with exponential backoff retry logic.
func Retry(ctx context.Context, config RetryConfig, logger forge.Logger, fn func(context.Context) error) error {
	var lastErr error

	delay := config.InitialDelay

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if len(config.RetryableErrors) > 0 {
			retryable := false

			for _, retryableErr := range config.RetryableErrors {
				if errors.Is(err, retryableErr) {
					retryable = true

					break
				}
			}

			if !retryable {
				return err
			}
		}

		// Don't retry if we've exhausted attempts
		if attempt >= config.MaxAttempts {
			break
		}

		// Calculate next delay
		if config.Jitter {
			// Add jitter (±25%)
			jitter := float64(delay) * 0.25
			jitterSign := float64(time.Now().UnixNano()%2*2 - 1) // -1 or 1
			delay = time.Duration(float64(delay) + (jitter * jitterSign))
		}

		if logger != nil {
			logger.Warn("Retrying after error",
				F("attempt", attempt),
				F("max_attempts", config.MaxAttempts),
				F("delay", delay),
				F("error", err.Error()),
			)
		}

		// Wait before retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		// Exponential backoff for next attempt
		delay = time.Duration(float64(delay) * config.Multiplier)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	return fmt.Errorf("max retries (%d) exceeded: %w", config.MaxAttempts, lastErr)
}

// RateLimiter implements token bucket rate limiting.
type RateLimiter struct {
	name       string
	rate       int // tokens per second
	burst      int // max burst size
	tokens     float64
	lastUpdate time.Time
	mu         sync.Mutex
	logger     forge.Logger
	metrics    forge.Metrics
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(name string, rate, burst int, logger forge.Logger, metrics forge.Metrics) *RateLimiter {
	return &RateLimiter{
		name:       name,
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst),
		lastUpdate: time.Now(),
		logger:     logger,
		metrics:    metrics,
	}
}

// Allow checks if a request is allowed.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastUpdate).Seconds()

	// Refill tokens based on elapsed time
	rl.tokens = min(float64(rl.burst), rl.tokens+elapsed*float64(rl.rate))
	rl.lastUpdate = now

	if rl.tokens >= 1 {
		rl.tokens--
		if rl.metrics != nil {
			rl.metrics.Counter("forge.ai.sdk.rate_limiter.allowed", "limiter", rl.name).Inc()
		}

		return true
	}

	if rl.metrics != nil {
		rl.metrics.Counter("forge.ai.sdk.rate_limiter.rejected", "limiter", rl.name).Inc()
	}

	return false
}

// Wait waits until a token is available.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		if rl.Allow() {
			return nil
		}

		// Calculate wait time
		rl.mu.Lock()
		tokensNeeded := 1.0 - rl.tokens
		waitTime := time.Duration(tokensNeeded/float64(rl.rate)*1000) * time.Millisecond
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}
}

// FallbackChain executes functions in order until one succeeds.
type FallbackChain struct {
	name    string
	logger  forge.Logger
	metrics forge.Metrics
}

// NewFallbackChain creates a new fallback chain.
func NewFallbackChain(name string, logger forge.Logger, metrics forge.Metrics) *FallbackChain {
	return &FallbackChain{
		name:    name,
		logger:  logger,
		metrics: metrics,
	}
}

// Execute tries each function until one succeeds.
func (fc *FallbackChain) Execute(ctx context.Context, fns ...func(context.Context) error) error {
	if len(fns) == 0 {
		return errors.New("no functions provided to fallback chain")
	}

	var lastErr error

	for i, fn := range fns {
		err := fn(ctx)
		if err == nil {
			if fc.metrics != nil {
				fc.metrics.Counter("forge.ai.sdk.fallback.success",
					"chain", fc.name,
					"attempt", strconv.Itoa(i+1),
				).Inc()
			}

			return nil
		}

		lastErr = err
		if fc.logger != nil {
			fc.logger.Warn("Fallback attempt failed",
				F("chain", fc.name),
				F("attempt", i+1),
				F("total", len(fns)),
				F("error", err.Error()),
			)
		}
	}

	if fc.metrics != nil {
		fc.metrics.Counter("forge.ai.sdk.fallback.exhausted", "chain", fc.name).Inc()
	}

	return fmt.Errorf("all fallback attempts failed, last error: %w", lastErr)
}

// Bulkhead implements the bulkhead pattern for resource isolation.
type Bulkhead struct {
	name          string
	maxConcurrent int
	semaphore     chan struct{}
	logger        forge.Logger
	metrics       forge.Metrics
}

// NewBulkhead creates a new bulkhead.
func NewBulkhead(name string, maxConcurrent int, logger forge.Logger, metrics forge.Metrics) *Bulkhead {
	return &Bulkhead{
		name:          name,
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
		logger:        logger,
		metrics:       metrics,
	}
}

// Execute executes a function within the bulkhead's concurrency limit.
func (b *Bulkhead) Execute(ctx context.Context, fn func(context.Context) error) error {
	select {
	case b.semaphore <- struct{}{}:
		defer func() { <-b.semaphore }()

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.bulkhead.accepted", "bulkhead", b.name).Inc()
			b.metrics.Gauge("forge.ai.sdk.bulkhead.active", "bulkhead", b.name).Set(float64(len(b.semaphore)))
		}

		return fn(ctx)
	case <-ctx.Done():
		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.bulkhead.timeout", "bulkhead", b.name).Inc()
		}

		return ctx.Err()
	}
}

// GetActiveCount returns the current number of active executions.
func (b *Bulkhead) GetActiveCount() int {
	return len(b.semaphore)
}

// Timeout wraps a function with a timeout.
func Timeout(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	errChan := make(chan error, 1)

	go func() {
		errChan <- fn(ctx)
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}

	return b
}
