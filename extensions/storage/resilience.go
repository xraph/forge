package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// ResilienceConfig configures resilience features
type ResilienceConfig struct {
	// Retry configuration
	MaxRetries        int           `yaml:"max_retries" json:"max_retries" default:"3"`
	InitialBackoff    time.Duration `yaml:"initial_backoff" json:"initial_backoff" default:"100ms"`
	MaxBackoff        time.Duration `yaml:"max_backoff" json:"max_backoff" default:"10s"`
	BackoffMultiplier float64       `yaml:"backoff_multiplier" json:"backoff_multiplier" default:"2.0"`

	// Circuit breaker configuration
	CircuitBreakerEnabled     bool          `yaml:"circuit_breaker_enabled" json:"circuit_breaker_enabled" default:"true"`
	CircuitBreakerThreshold   uint32        `yaml:"circuit_breaker_threshold" json:"circuit_breaker_threshold" default:"5"`
	CircuitBreakerTimeout     time.Duration `yaml:"circuit_breaker_timeout" json:"circuit_breaker_timeout" default:"60s"`
	CircuitBreakerHalfOpenMax uint32        `yaml:"circuit_breaker_half_open_max" json:"circuit_breaker_half_open_max" default:"3"`

	// Rate limiting
	RateLimitEnabled bool `yaml:"rate_limit_enabled" json:"rate_limit_enabled" default:"true"`
	RateLimitPerSec  int  `yaml:"rate_limit_per_sec" json:"rate_limit_per_sec" default:"100"`
	RateLimitBurst   int  `yaml:"rate_limit_burst" json:"rate_limit_burst" default:"200"`

	// Timeout configuration
	OperationTimeout time.Duration `yaml:"operation_timeout" json:"operation_timeout" default:"30s"`
}

// DefaultResilienceConfig returns default resilience configuration
func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{
		MaxRetries:                3,
		InitialBackoff:            100 * time.Millisecond,
		MaxBackoff:                10 * time.Second,
		BackoffMultiplier:         2.0,
		CircuitBreakerEnabled:     true,
		CircuitBreakerThreshold:   5,
		CircuitBreakerTimeout:     60 * time.Second,
		CircuitBreakerHalfOpenMax: 3,
		RateLimitEnabled:          true,
		RateLimitPerSec:           100,
		RateLimitBurst:            200,
		OperationTimeout:          30 * time.Second,
	}
}

// CircuitState represents circuit breaker state
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements circuit breaker pattern
type CircuitBreaker struct {
	config           ResilienceConfig
	state            CircuitState
	failures         uint32
	lastFailTime     time.Time
	halfOpenAttempts uint32
	mu               sync.RWMutex
	logger           forge.Logger
	metrics          forge.Metrics
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config ResilienceConfig, logger forge.Logger, metrics forge.Metrics) *CircuitBreaker {
	return &CircuitBreaker{
		config:  config,
		state:   CircuitClosed,
		logger:  logger,
		metrics: metrics,
	}
}

// Execute executes a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, name string, fn func() error) error {
	if !cb.config.CircuitBreakerEnabled {
		return fn()
	}

	// Check if circuit is open
	if !cb.CanAttempt() {
		cb.metrics.Counter("storage_circuit_breaker_open", "operation", name).Inc()
		return ErrCircuitBreakerOpen
	}

	// Execute function
	err := fn()

	// Record result
	if err != nil {
		cb.RecordFailure()
		cb.metrics.Counter("storage_circuit_breaker_failures", "operation", name).Inc()
	} else {
		cb.RecordSuccess()
		cb.metrics.Counter("storage_circuit_breaker_successes", "operation", name).Inc()
	}

	cb.metrics.Gauge("storage_circuit_breaker_state", "operation", name).Set(float64(cb.GetState()))

	return err
}

// CanAttempt checks if a request can be attempted
func (cb *CircuitBreaker) CanAttempt() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if timeout has passed
		if time.Since(cb.lastFailTime) > cb.config.CircuitBreakerTimeout {
			return true // Transition to half-open will happen in RecordFailure/Success
		}
		return false
	case CircuitHalfOpen:
		return cb.halfOpenAttempts < cb.config.CircuitBreakerHalfOpenMax
	default:
		return false
	}
}

// RecordSuccess records a successful execution
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitHalfOpen:
		cb.state = CircuitClosed
		cb.failures = 0
		cb.halfOpenAttempts = 0
		cb.logger.Info("circuit breaker closed")
	case CircuitClosed:
		cb.failures = 0
	}
}

// RecordFailure records a failed execution
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failures++
		if cb.failures >= cb.config.CircuitBreakerThreshold {
			cb.state = CircuitOpen
			cb.logger.Warn("circuit breaker opened",
				forge.F("failures", cb.failures),
				forge.F("threshold", cb.config.CircuitBreakerThreshold),
			)
		}
	case CircuitHalfOpen:
		cb.state = CircuitOpen
		cb.halfOpenAttempts = 0
		cb.logger.Warn("circuit breaker reopened from half-open")
	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailTime) > cb.config.CircuitBreakerTimeout {
			cb.state = CircuitHalfOpen
			cb.halfOpenAttempts = 0
			cb.logger.Info("circuit breaker half-open")
		}
	}
}

// GetState returns current circuit state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failures = 0
	cb.halfOpenAttempts = 0
	cb.logger.Info("circuit breaker reset")
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	config   ResilienceConfig
	tokens   int
	lastFill time.Time
	mu       sync.Mutex
	metrics  forge.Metrics
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config ResilienceConfig, metrics forge.Metrics) *RateLimiter {
	return &RateLimiter{
		config:   config,
		tokens:   config.RateLimitBurst,
		lastFill: time.Now(),
		metrics:  metrics,
	}
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow() bool {
	if !rl.config.RateLimitEnabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(rl.lastFill)
	tokensToAdd := int(elapsed.Seconds() * float64(rl.config.RateLimitPerSec))

	if tokensToAdd > 0 {
		rl.tokens += tokensToAdd
		if rl.tokens > rl.config.RateLimitBurst {
			rl.tokens = rl.config.RateLimitBurst
		}
		rl.lastFill = now
	}

	// Check if we have tokens
	if rl.tokens > 0 {
		rl.tokens--
		rl.metrics.Counter("storage_rate_limit_allowed").Inc()
		return true
	}

	rl.metrics.Counter("storage_rate_limit_rejected").Inc()
	return false
}

// ResilientStorage wraps a storage backend with resilience features
type ResilientStorage struct {
	backend        Storage
	config         ResilienceConfig
	circuitBreaker *CircuitBreaker
	rateLimiter    *RateLimiter
	logger         forge.Logger
	metrics        forge.Metrics
}

// NewResilientStorage creates a resilient storage wrapper
func NewResilientStorage(backend Storage, config ResilienceConfig, logger forge.Logger, metrics forge.Metrics) *ResilientStorage {
	return &ResilientStorage{
		backend:        backend,
		config:         config,
		circuitBreaker: NewCircuitBreaker(config, logger, metrics),
		rateLimiter:    NewRateLimiter(config, metrics),
		logger:         logger,
		metrics:        metrics,
	}
}

// Upload uploads with resilience
func (rs *ResilientStorage) Upload(ctx context.Context, key string, data io.Reader, opts ...UploadOption) error {
	return rs.executeWithResilience(ctx, "upload", func() error {
		return rs.backend.Upload(ctx, key, data, opts...)
	})
}

// Download downloads with resilience
func (rs *ResilientStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	var result io.ReadCloser
	err := rs.executeWithResilience(ctx, "download", func() error {
		var err error
		result, err = rs.backend.Download(ctx, key)
		return err
	})
	return result, err
}

// Delete deletes with resilience
func (rs *ResilientStorage) Delete(ctx context.Context, key string) error {
	return rs.executeWithResilience(ctx, "delete", func() error {
		return rs.backend.Delete(ctx, key)
	})
}

// List lists with resilience
func (rs *ResilientStorage) List(ctx context.Context, prefix string, opts ...ListOption) ([]Object, error) {
	var result []Object
	err := rs.executeWithResilience(ctx, "list", func() error {
		var err error
		result, err = rs.backend.List(ctx, prefix, opts...)
		return err
	})
	return result, err
}

// Metadata gets metadata with resilience
func (rs *ResilientStorage) Metadata(ctx context.Context, key string) (*ObjectMetadata, error) {
	var result *ObjectMetadata
	err := rs.executeWithResilience(ctx, "metadata", func() error {
		var err error
		result, err = rs.backend.Metadata(ctx, key)
		return err
	})
	return result, err
}

// Exists checks existence with resilience
func (rs *ResilientStorage) Exists(ctx context.Context, key string) (bool, error) {
	var result bool
	err := rs.executeWithResilience(ctx, "exists", func() error {
		var err error
		result, err = rs.backend.Exists(ctx, key)
		return err
	})
	return result, err
}

// Copy copies with resilience
func (rs *ResilientStorage) Copy(ctx context.Context, srcKey, dstKey string) error {
	return rs.executeWithResilience(ctx, "copy", func() error {
		return rs.backend.Copy(ctx, srcKey, dstKey)
	})
}

// Move moves with resilience
func (rs *ResilientStorage) Move(ctx context.Context, srcKey, dstKey string) error {
	return rs.executeWithResilience(ctx, "move", func() error {
		return rs.backend.Move(ctx, srcKey, dstKey)
	})
}

// PresignUpload presigns upload URL with resilience
func (rs *ResilientStorage) PresignUpload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	var result string
	err := rs.executeWithResilience(ctx, "presign_upload", func() error {
		var err error
		result, err = rs.backend.PresignUpload(ctx, key, expiry)
		return err
	})
	return result, err
}

// PresignDownload presigns download URL with resilience
func (rs *ResilientStorage) PresignDownload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	var result string
	err := rs.executeWithResilience(ctx, "presign_download", func() error {
		var err error
		result, err = rs.backend.PresignDownload(ctx, key, expiry)
		return err
	})
	return result, err
}

// executeWithResilience executes a function with retry, circuit breaker, and rate limiting
func (rs *ResilientStorage) executeWithResilience(ctx context.Context, operation string, fn func() error) error {
	// Check rate limit
	if !rs.rateLimiter.Allow() {
		return ErrRateLimitExceeded
	}

	// Apply timeout (only if configured and not zero)
	if rs.config.OperationTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rs.config.OperationTimeout)
		defer cancel()
	}

	// Execute with circuit breaker and retry
	return rs.retryWithBackoff(ctx, operation, func() error {
		return rs.circuitBreaker.Execute(ctx, operation, fn)
	})
}

// retryWithBackoff implements exponential backoff retry
func (rs *ResilientStorage) retryWithBackoff(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	backoff := rs.config.InitialBackoff
	
	// Ensure backoff has a minimum value to prevent tight retry loops
	if backoff == 0 {
		backoff = 10 * time.Millisecond
	}

	for attempt := 0; attempt <= rs.config.MaxRetries; attempt++ {
		// Check context
		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		// Execute function
		err := fn()
		if err == nil {
			if attempt > 0 {
				rs.logger.Info("operation succeeded after retry",
					forge.F("operation", operation),
					forge.F("attempts", attempt+1),
				)
			}
			return nil
		}

		lastErr = err

		// Don't retry non-retryable errors
		if !isRetryableError(err) {
			rs.metrics.Counter("storage_non_retryable_errors", "operation", operation).Inc()
			return err
		}

		// Last attempt?
		if attempt == rs.config.MaxRetries {
			rs.metrics.Counter("storage_max_retries_exceeded", "operation", operation).Inc()
			break
		}

		// Log retry attempt
		rs.logger.Warn("operation failed, retrying",
			forge.F("operation", operation),
			forge.F("attempt", attempt+1),
			forge.F("max_retries", rs.config.MaxRetries),
			forge.F("backoff", backoff),
			forge.F("error", err.Error()),
		)

		rs.metrics.Counter("storage_retries", "operation", operation).Inc()

		// Wait with backoff
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled during backoff: %w", ctx.Err())
		}

		// Increase backoff
		backoff = time.Duration(float64(backoff) * rs.config.BackoffMultiplier)
		if backoff > rs.config.MaxBackoff {
			backoff = rs.config.MaxBackoff
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", rs.config.MaxRetries+1, lastErr)
}

// isRetryableError determines if an error is retryable
func isRetryableError(err error) bool {
	// Don't retry these errors
	if errors.Is(err, ErrObjectNotFound) ||
		errors.Is(err, ErrInvalidKey) ||
		errors.Is(err, ErrPresignNotSupported) ||
		errors.Is(err, ErrMultipartNotSupported) ||
		errors.Is(err, ErrCircuitBreakerOpen) ||
		errors.Is(err, ErrRateLimitExceeded) ||
		errors.Is(err, context.Canceled) {
		return false
	}

	// Retry timeouts and other transient errors
	return true
}

// ResetCircuitBreaker resets the circuit breaker
func (rs *ResilientStorage) ResetCircuitBreaker() {
	rs.circuitBreaker.Reset()
}

// GetCircuitBreakerState returns circuit breaker state
func (rs *ResilientStorage) GetCircuitBreakerState() CircuitState {
	return rs.circuitBreaker.GetState()
}
