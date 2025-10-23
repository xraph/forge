package resilience

import (
	"math"
	"math/rand"
	"time"
)

// BackoffStrategy defines a backoff strategy for retries
type BackoffStrategy interface {
	// NextDelay returns the next delay duration for the given attempt
	// Returns 0 if max attempts reached
	NextDelay(attempt int) time.Duration
}

// ExponentialBackoff implements exponential backoff strategy
type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	MaxAttempts  int
	Jitter       bool
}

// NextDelay returns the next delay with exponential backoff
func (eb *ExponentialBackoff) NextDelay(attempt int) time.Duration {
	if eb.MaxAttempts > 0 && attempt > eb.MaxAttempts {
		return 0
	}

	// Calculate exponential delay
	delay := float64(eb.InitialDelay) * math.Pow(eb.Multiplier, float64(attempt-1))

	// Apply max delay cap
	if time.Duration(delay) > eb.MaxDelay {
		delay = float64(eb.MaxDelay)
	}

	// Add jitter if enabled (±25%)
	if eb.Jitter {
		jitter := delay * 0.25
		delay += (rand.Float64()*2 - 1) * jitter
	}

	return time.Duration(delay)
}

// LinearBackoff implements linear backoff strategy
type LinearBackoff struct {
	InitialDelay time.Duration
	Increment    time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

// NextDelay returns the next delay with linear backoff
func (lb *LinearBackoff) NextDelay(attempt int) time.Duration {
	if lb.MaxAttempts > 0 && attempt > lb.MaxAttempts {
		return 0
	}

	delay := lb.InitialDelay + (lb.Increment * time.Duration(attempt-1))

	if delay > lb.MaxDelay {
		delay = lb.MaxDelay
	}

	return delay
}

// ConstantBackoff implements constant backoff strategy
type ConstantBackoff struct {
	Delay       time.Duration
	MaxAttempts int
}

// NextDelay returns constant delay
func (cb *ConstantBackoff) NextDelay(attempt int) time.Duration {
	if cb.MaxAttempts > 0 && attempt > cb.MaxAttempts {
		return 0
	}
	return cb.Delay
}

// FibonacciBackoff implements Fibonacci backoff strategy
type FibonacciBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

// NextDelay returns the next delay using Fibonacci sequence
func (fb *FibonacciBackoff) NextDelay(attempt int) time.Duration {
	if fb.MaxAttempts > 0 && attempt > fb.MaxAttempts {
		return 0
	}

	fib := fibonacci(attempt)
	delay := fb.InitialDelay * time.Duration(fib)

	if delay > fb.MaxDelay {
		delay = fb.MaxDelay
	}

	return delay
}

// fibonacci calculates the nth Fibonacci number
func fibonacci(n int) int {
	if n <= 1 {
		return 1
	}

	a, b := 1, 1
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}

	return b
}

// DecorrelatedJitterBackoff implements AWS's decorrelated jitter backoff
type DecorrelatedJitterBackoff struct {
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	MaxAttempts   int
	previousDelay time.Duration
}

// NextDelay returns the next delay with decorrelated jitter
func (djb *DecorrelatedJitterBackoff) NextDelay(attempt int) time.Duration {
	if djb.MaxAttempts > 0 && attempt > djb.MaxAttempts {
		return 0
	}

	if attempt == 1 {
		djb.previousDelay = djb.BaseDelay
		return djb.BaseDelay
	}

	// Random between base and 3x previous delay
	maxNext := djb.previousDelay * 3
	if maxNext > djb.MaxDelay {
		maxNext = djb.MaxDelay
	}

	delay := djb.BaseDelay + time.Duration(rand.Int63n(int64(maxNext-djb.BaseDelay)))
	djb.previousDelay = delay

	return delay
}

// AdaptiveBackoff adapts delay based on success rate
type AdaptiveBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
	SuccessRate  float64
	Multiplier   float64
}

// NextDelay returns adaptive delay based on success rate
func (ab *AdaptiveBackoff) NextDelay(attempt int) time.Duration {
	if ab.MaxAttempts > 0 && attempt > ab.MaxAttempts {
		return 0
	}

	// If success rate is high, use shorter delays
	// If success rate is low, use longer delays
	factor := 1.0
	if ab.SuccessRate > 0.8 {
		factor = 0.5 // 50% shorter
	} else if ab.SuccessRate < 0.3 {
		factor = 2.0 // 2x longer
	}

	baseDelay := float64(ab.InitialDelay) * math.Pow(ab.Multiplier, float64(attempt-1)) * factor

	if time.Duration(baseDelay) > ab.MaxDelay {
		baseDelay = float64(ab.MaxDelay)
	}

	return time.Duration(baseDelay)
}

// UpdateSuccessRate updates the success rate for adaptive backoff
func (ab *AdaptiveBackoff) UpdateSuccessRate(rate float64) {
	ab.SuccessRate = rate
}

// BackoffFactory creates backoff strategies
type BackoffFactory struct{}

// NewExponentialBackoff creates exponential backoff with defaults
func (BackoffFactory) NewExponentialBackoff() BackoffStrategy {
	return &ExponentialBackoff{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  5,
		Jitter:       true,
	}
}

// NewLinearBackoff creates linear backoff with defaults
func (BackoffFactory) NewLinearBackoff() BackoffStrategy {
	return &LinearBackoff{
		InitialDelay: 100 * time.Millisecond,
		Increment:    100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		MaxAttempts:  10,
	}
}

// NewConstantBackoff creates constant backoff with defaults
func (BackoffFactory) NewConstantBackoff() BackoffStrategy {
	return &ConstantBackoff{
		Delay:       500 * time.Millisecond,
		MaxAttempts: 3,
	}
}

// NewFibonacciBackoff creates Fibonacci backoff with defaults
func (BackoffFactory) NewFibonacciBackoff() BackoffStrategy {
	return &FibonacciBackoff{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		MaxAttempts:  8,
	}
}

// NewDecorrelatedJitterBackoff creates decorrelated jitter backoff
func (BackoffFactory) NewDecorrelatedJitterBackoff() BackoffStrategy {
	return &DecorrelatedJitterBackoff{
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    20 * time.Second,
		MaxAttempts: 5,
	}
}

// NewAdaptiveBackoff creates adaptive backoff
func (BackoffFactory) NewAdaptiveBackoff() BackoffStrategy {
	return &AdaptiveBackoff{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		MaxAttempts:  5,
		SuccessRate:  0.5,
		Multiplier:   2.0,
	}
}

// BackoffPresets provides preset backoff strategies
var BackoffPresets = struct {
	// Fast for low-latency networks
	Fast BackoffStrategy
	// Normal for typical networks
	Normal BackoffStrategy
	// Slow for high-latency networks
	Slow BackoffStrategy
	// Aggressive for time-sensitive operations
	Aggressive BackoffStrategy
	// Conservative for rate-limited operations
	Conservative BackoffStrategy
}{
	Fast: &ExponentialBackoff{
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   1.5,
		MaxAttempts:  5,
		Jitter:       true,
	},
	Normal: &ExponentialBackoff{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  5,
		Jitter:       true,
	},
	Slow: &ExponentialBackoff{
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  5,
		Jitter:       true,
	},
	Aggressive: &LinearBackoff{
		InitialDelay: 10 * time.Millisecond,
		Increment:    10 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		MaxAttempts:  10,
	},
	Conservative: &FibonacciBackoff{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		MaxAttempts:  8,
	},
}
