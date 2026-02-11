package gateway

import (
	"math"
	"math/rand"
	"strings"
	"time"
)

// RetryExecutor manages retry logic for proxy requests.
type RetryExecutor struct {
	globalConfig RetryConfig
}

// NewRetryExecutor creates a new retry executor.
func NewRetryExecutor(config RetryConfig) *RetryExecutor {
	return &RetryExecutor{
		globalConfig: config,
	}
}

// ShouldRetry determines if a request should be retried.
func (re *RetryExecutor) ShouldRetry(method string, statusCode int, attempt int, routeConfig *RetryConfig) bool {
	config := re.effectiveConfig(routeConfig)

	if !config.Enabled {
		return false
	}

	if attempt >= config.MaxAttempts {
		return false
	}

	// Check if method is retryable
	if !re.isRetryableMethod(method, config) {
		return false
	}

	// Check if status code is retryable
	return re.isRetryableStatus(statusCode, config)
}

// Delay returns the delay before the next retry attempt.
func (re *RetryExecutor) Delay(attempt int, routeConfig *RetryConfig) time.Duration {
	config := re.effectiveConfig(routeConfig)

	var delay time.Duration

	switch config.Backoff {
	case BackoffLinear:
		delay = config.InitialDelay * time.Duration(attempt+1)
	case BackoffFixed:
		delay = config.InitialDelay
	default: // Exponential
		delay = config.InitialDelay * time.Duration(math.Pow(config.Multiplier, float64(attempt)))
	}

	// Cap at max delay
	if delay > config.MaxDelay {
		delay = config.MaxDelay
	}

	// Apply jitter (+/- 25%)
	if config.Jitter && delay > 0 {
		jitterRange := float64(delay) * 0.25
		jitter := (rand.Float64() * 2 * jitterRange) - jitterRange
		delay = time.Duration(float64(delay) + jitter)

		if delay < 0 {
			delay = 0
		}
	}

	return delay
}

func (re *RetryExecutor) effectiveConfig(routeConfig *RetryConfig) RetryConfig {
	if routeConfig != nil {
		return *routeConfig
	}

	return re.globalConfig
}

func (re *RetryExecutor) isRetryableMethod(method string, config RetryConfig) bool {
	method = strings.ToUpper(method)

	for _, m := range config.RetryableMethods {
		if strings.ToUpper(m) == method {
			return true
		}
	}

	return false
}

func (re *RetryExecutor) isRetryableStatus(statusCode int, config RetryConfig) bool {
	for _, code := range config.RetryableStatus {
		if code == statusCode {
			return true
		}
	}

	return false
}
