package resilience

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/metrics"
)

// Retry provides retry functionality with various backoff strategies.
type Retry struct {
	config  RetryConfig
	stats   RetryStats
	mu      sync.RWMutex
	logger  logger.Logger
	metrics shared.Metrics
}

// RetryConfig contains retry configuration.
type RetryConfig struct {
	Name               string          `yaml:"name"`
	MaxAttempts        int             `default:"3"                 yaml:"max_attempts"`
	InitialDelay       time.Duration   `default:"100ms"             yaml:"initial_delay"`
	MaxDelay           time.Duration   `default:"30s"               yaml:"max_delay"`
	Multiplier         float64         `default:"2.0"               yaml:"multiplier"`
	Jitter             bool            `default:"true"              yaml:"jitter"`
	BackoffStrategy    BackoffStrategy `default:"exponential"       yaml:"backoff_strategy"`
	RetryableErrors    []string        `yaml:"retryable_errors"`
	NonRetryableErrors []string        `yaml:"non_retryable_errors"`
	EnableMetrics      bool            `default:"true"              yaml:"enable_metrics"`
	Logger             logger.Logger   `yaml:"-"`
	Metrics            shared.Metrics  `yaml:"-"`
}

// BackoffStrategy represents the backoff strategy.
type BackoffStrategy int

const (
	BackoffStrategyFixed BackoffStrategy = iota
	BackoffStrategyLinear
	BackoffStrategyExponential
	BackoffStrategyFibonacci
)

// RetryStats represents retry statistics.
type RetryStats struct {
	TotalAttempts     int64         `json:"total_attempts"`
	SuccessfulRetries int64         `json:"successful_retries"`
	FailedRetries     int64         `json:"failed_retries"`
	TotalDelay        time.Duration `json:"total_delay"`
	LastAttempt       time.Time     `json:"last_attempt"`
	LastSuccess       time.Time     `json:"last_success"`
	LastFailure       time.Time     `json:"last_failure"`
}

// RetryError represents a retry error.
type RetryError struct {
	Attempts   int           `json:"attempts"`
	TotalDelay time.Duration `json:"total_delay"`
	LastError  error         `json:"last_error"`
	AllErrors  []error       `json:"all_errors"`
	Time       time.Time     `json:"time"`
}

func (e *RetryError) Error() string {
	return fmt.Sprintf("retry failed after %d attempts: %v", e.Attempts, e.LastError)
}

// NewRetry creates a new retry instance.
func NewRetry(config RetryConfig) *Retry {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	return &Retry{
		config: config,
		stats: RetryStats{
			LastAttempt: time.Now(),
		},
		logger:  config.Logger,
		metrics: config.Metrics,
	}
}

// Execute executes a function with retry logic.
func (r *Retry) Execute(ctx context.Context, fn func() (any, error)) (any, error) {
	var (
		lastError  error
		allErrors  []error
		totalDelay time.Duration
	)

	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Execute the function
		result, err := fn()
		if err == nil {
			// Success
			r.recordSuccess(attempt, totalDelay)

			return result, nil
		}

		// Check if error is retryable
		if !r.isRetryableError(err) {
			r.recordFailure(attempt, totalDelay, err)

			return nil, err
		}

		// Record attempt
		lastError = err
		allErrors = append(allErrors, err)
		r.stats.TotalAttempts++

		// If this is the last attempt, return the error
		if attempt == r.config.MaxAttempts {
			break
		}

		// Calculate delay for next attempt
		delay := r.calculateDelay(attempt)
		totalDelay += delay

		// Wait before next attempt
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	// All attempts failed
	r.recordFailure(r.config.MaxAttempts, totalDelay, lastError)

	return nil, &RetryError{
		Attempts:   r.config.MaxAttempts,
		TotalDelay: totalDelay,
		LastError:  lastError,
		AllErrors:  allErrors,
		Time:       time.Now(),
	}
}

// isRetryableError checks if an error is retryable.
func (r *Retry) isRetryableError(err error) bool {
	errorStr := err.Error()

	// Check non-retryable errors first
	if slices.Contains(r.config.NonRetryableErrors, errorStr) {
		return false
	}

	// Check retryable errors
	if len(r.config.RetryableErrors) > 0 {
		return slices.Contains(r.config.RetryableErrors, errorStr)
	}

	// Default: all errors are retryable
	return true
}

// calculateDelay calculates the delay for the next attempt.
func (r *Retry) calculateDelay(attempt int) time.Duration {
	var delay time.Duration

	switch r.config.BackoffStrategy {
	case BackoffStrategyFixed:
		delay = r.config.InitialDelay
	case BackoffStrategyLinear:
		delay = r.config.InitialDelay * time.Duration(attempt)
	case BackoffStrategyExponential:
		delay = time.Duration(float64(r.config.InitialDelay) * math.Pow(r.config.Multiplier, float64(attempt-1)))
	case BackoffStrategyFibonacci:
		delay = time.Duration(float64(r.config.InitialDelay) * fibonacci(attempt))
	}

	// Apply jitter if enabled
	if r.config.Jitter {
		delay = r.addJitter(delay)
	}

	// Cap at max delay
	if delay > r.config.MaxDelay {
		delay = r.config.MaxDelay
	}

	return delay
}

// addJitter adds random jitter to the delay.
func (r *Retry) addJitter(delay time.Duration) time.Duration {
	// Simple jitter: ±25% of the delay
	jitter := time.Duration(float64(delay) * 0.25)

	return delay + time.Duration(float64(jitter)*(2*math.Mod(float64(time.Now().UnixNano()), 1.0)-1.0))
}

// fibonacci calculates the nth Fibonacci number.
func fibonacci(n int) float64 {
	if n <= 1 {
		return 1
	}

	a, b := 1.0, 1.0
	for i := 2; i < n; i++ {
		a, b = b, a+b
	}

	return b
}

// recordSuccess records a successful retry.
func (r *Retry) recordSuccess(attempts int, totalDelay time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stats.SuccessfulRetries++
	r.stats.TotalDelay += totalDelay
	r.stats.LastSuccess = time.Now()

	if r.config.EnableMetrics && r.metrics != nil {
		r.recordMetrics(attempts, totalDelay, true)
	}

	r.logger.Info("retry succeeded",
		logger.String("name", r.config.Name),
		logger.Int("attempts", attempts),
		logger.String("total_delay", totalDelay.String()))
}

// recordFailure records a failed retry.
func (r *Retry) recordFailure(attempts int, totalDelay time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stats.FailedRetries++
	r.stats.TotalDelay += totalDelay
	r.stats.LastFailure = time.Now()

	if r.config.EnableMetrics && r.metrics != nil {
		r.recordMetrics(attempts, totalDelay, false)
	}

	r.logger.Error("retry failed",
		logger.String("name", r.config.Name),
		logger.Int("attempts", attempts),
		logger.String("total_delay", totalDelay.String()),
		logger.String("error", err.Error()))
}

// recordMetrics records retry metrics.
func (r *Retry) recordMetrics(attempts int, totalDelay time.Duration, success bool) {
	result := "success"
	if !success {
		result = "failure"
	}

	// Record attempt metrics
	if r.metrics != nil {
		counter := r.metrics.Counter("retry_attempts_total",
			metrics.WithLabel("name", r.config.Name),
			metrics.WithLabel("result", result),
		)
		counter.Inc()

		// Record delay metrics
		histogram := r.metrics.Histogram("retry_delay_seconds",
			metrics.WithLabel("name", r.config.Name),
			metrics.WithLabel("result", result),
		)
		histogram.Observe(totalDelay.Seconds())

		// Record attempt count metrics
		attemptHistogram := r.metrics.Histogram("retry_attempt_count",
			metrics.WithLabel("name", r.config.Name),
			metrics.WithLabel("result", result),
		)
		attemptHistogram.Observe(float64(attempts))
	}
}

// GetStats returns the retry statistics.
func (r *Retry) GetStats() RetryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.stats
}

// Reset resets the retry statistics.
func (r *Retry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stats = RetryStats{
		LastAttempt: time.Now(),
	}
}

// GetConfig returns the retry configuration.
func (r *Retry) GetConfig() RetryConfig {
	return r.config
}

// UpdateConfig updates the retry configuration.
func (r *Retry) UpdateConfig(config RetryConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.config = config
}

// String methods for enums

func (s BackoffStrategy) String() string {
	switch s {
	case BackoffStrategyFixed:
		return "fixed"
	case BackoffStrategyLinear:
		return "linear"
	case BackoffStrategyExponential:
		return "exponential"
	case BackoffStrategyFibonacci:
		return "fibonacci"
	default:
		return "unknown"
	}
}
