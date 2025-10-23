package transport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// TransportMiddleware represents transport-level middleware
type TransportMiddleware func(next TransportHandler) TransportHandler

// TransportHandler handles transport operations
type TransportHandler func(ctx context.Context, msg *internal.Message) error

// MiddlewareChain chains multiple middleware
type MiddlewareChain struct {
	middlewares []TransportMiddleware
	logger      forge.Logger
}

// NewMiddlewareChain creates a new middleware chain
func NewMiddlewareChain(logger forge.Logger) *MiddlewareChain {
	return &MiddlewareChain{
		middlewares: make([]TransportMiddleware, 0),
		logger:      logger,
	}
}

// Use adds middleware to the chain
func (mc *MiddlewareChain) Use(middleware TransportMiddleware) {
	mc.middlewares = append(mc.middlewares, middleware)
}

// Execute executes the middleware chain
func (mc *MiddlewareChain) Execute(handler TransportHandler) TransportHandler {
	// Build chain in reverse order
	for i := len(mc.middlewares) - 1; i >= 0; i-- {
		handler = mc.middlewares[i](handler)
	}
	return handler
}

// RateLimitMiddleware creates rate limiting middleware
func RateLimitMiddleware(maxRate int, window time.Duration, logger forge.Logger) TransportMiddleware {
	type rateLimiter struct {
		tokens    int
		lastReset time.Time
		mu        sync.Mutex
	}

	limiters := make(map[string]*rateLimiter)
	var limitersMu sync.RWMutex

	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			// Get peer ID from message
			peerID := msg.From

			// Get or create rate limiter for peer
			limitersMu.Lock()
			limiter, exists := limiters[peerID]
			if !exists {
				limiter = &rateLimiter{
					tokens:    maxRate,
					lastReset: time.Now(),
				}
				limiters[peerID] = limiter
			}
			limitersMu.Unlock()

			// Check rate limit
			limiter.mu.Lock()
			now := time.Now()

			// Reset tokens if window passed
			if now.Sub(limiter.lastReset) >= window {
				limiter.tokens = maxRate
				limiter.lastReset = now
			}

			// Check if tokens available
			if limiter.tokens <= 0 {
				limiter.mu.Unlock()
				logger.Warn("rate limit exceeded",
					forge.F("peer", peerID),
					forge.F("max_rate", maxRate),
					forge.F("window", window),
				)
				return internal.ErrRateLimitExceeded.WithContext(map[string]interface{}{
					"peer":     peerID,
					"max_rate": maxRate,
				})
			}

			limiter.tokens--
			limiter.mu.Unlock()

			return next(ctx, msg)
		}
	}
}

// CompressionMiddleware creates compression middleware
func CompressionMiddleware(minSize int, logger forge.Logger) TransportMiddleware {
	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			// Check if message size exceeds threshold
			if len(msg.Data) >= minSize {
				// Compression logic would go here
				// For now, just log
				logger.Debug("message eligible for compression",
					forge.F("size", len(msg.Data)),
					forge.F("threshold", minSize),
				)
			}

			return next(ctx, msg)
		}
	}
}

// AuthenticationMiddleware creates authentication middleware
func AuthenticationMiddleware(verifyFunc func(string) bool, logger forge.Logger) TransportMiddleware {
	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			// Verify sender
			if !verifyFunc(msg.From) {
				logger.Warn("authentication failed",
					forge.F("peer", msg.From),
				)
				return internal.ErrAuthenticationFailed.WithContext(map[string]interface{}{
					"peer": msg.From,
				})
			}

			return next(ctx, msg)
		}
	}
}

// MetricsMiddleware creates metrics collection middleware
func MetricsMiddleware(collector MetricsCollector, logger forge.Logger) TransportMiddleware {
	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			start := time.Now()

			err := next(ctx, msg)

			duration := time.Since(start)

			// Record metrics
			collector.RecordMessage(msg, duration, err == nil)

			return err
		}
	}
}

// MetricsCollector collects transport metrics
type MetricsCollector interface {
	RecordMessage(msg *internal.Message, duration time.Duration, success bool)
}

// RetryMiddleware creates retry middleware
func RetryMiddleware(maxRetries int, backoff time.Duration, logger forge.Logger) TransportMiddleware {
	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			var lastErr error

			for attempt := 0; attempt <= maxRetries; attempt++ {
				err := next(ctx, msg)
				if err == nil {
					return nil
				}

				lastErr = err

				// Check if retryable
				if !isRetryable(err) {
					return err
				}

				if attempt < maxRetries {
					wait := backoff * time.Duration(1<<uint(attempt))
					logger.Debug("retrying message",
						forge.F("attempt", attempt+1),
						forge.F("max_retries", maxRetries),
						forge.F("backoff", wait),
					)

					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}

			return lastErr
		}
	}
}

// isRetryable checks if an error is retryable
func isRetryable(err error) bool {
	if internal.Is(err, internal.ErrTimeout) {
		return true
	}
	if internal.Is(err, internal.ErrConnectionFailed) {
		return true
	}
	return false
}

// TimeoutMiddleware creates timeout middleware
func TimeoutMiddleware(timeout time.Duration, logger forge.Logger) TransportMiddleware {
	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			errChan := make(chan error, 1)

			go func() {
				errChan <- next(timeoutCtx, msg)
			}()

			select {
			case err := <-errChan:
				return err
			case <-timeoutCtx.Done():
				logger.Warn("message handling timeout",
					forge.F("timeout", timeout),
					forge.F("peer", msg.From),
				)
				return internal.ErrTimeout
			}
		}
	}
}

// LoggingMiddleware creates logging middleware
func LoggingMiddleware(logger forge.Logger) TransportMiddleware {
	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			logger.Debug("handling message",
				forge.F("type", msg.Type),
				forge.F("from", msg.From),
				forge.F("to", msg.To),
				forge.F("size", len(msg.Data)),
			)

			err := next(ctx, msg)

			if err != nil {
				logger.Warn("message handling failed",
					forge.F("type", msg.Type),
					forge.F("from", msg.From),
					forge.F("error", err),
				)
			} else {
				logger.Debug("message handled successfully",
					forge.F("type", msg.Type),
					forge.F("from", msg.From),
				)
			}

			return err
		}
	}
}

// CircuitBreakerMiddleware creates circuit breaker middleware
func CircuitBreakerMiddleware(threshold int, resetTimeout time.Duration, logger forge.Logger) TransportMiddleware {
	type circuitState struct {
		failures int
		state    string // "closed", "open", "half-open"
		lastFail time.Time
		mu       sync.Mutex
	}

	circuits := make(map[string]*circuitState)
	var circuitsMu sync.RWMutex

	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			peerID := msg.From

			// Get or create circuit
			circuitsMu.Lock()
			circuit, exists := circuits[peerID]
			if !exists {
				circuit = &circuitState{
					state: "closed",
				}
				circuits[peerID] = circuit
			}
			circuitsMu.Unlock()

			circuit.mu.Lock()

			// Check if circuit is open
			if circuit.state == "open" {
				// Check if reset timeout passed
				if time.Since(circuit.lastFail) >= resetTimeout {
					circuit.state = "half-open"
					circuit.failures = 0
					logger.Info("circuit breaker half-open",
						forge.F("peer", peerID),
					)
				} else {
					circuit.mu.Unlock()
					logger.Warn("circuit breaker open",
						forge.F("peer", peerID),
					)
					return fmt.Errorf("circuit breaker open for peer: %s", peerID)
				}
			}

			circuit.mu.Unlock()

			// Execute handler
			err := next(ctx, msg)

			circuit.mu.Lock()
			defer circuit.mu.Unlock()

			if err != nil {
				circuit.failures++
				circuit.lastFail = time.Now()

				if circuit.failures >= threshold {
					circuit.state = "open"
					logger.Warn("circuit breaker opened",
						forge.F("peer", peerID),
						forge.F("failures", circuit.failures),
					)
				}
			} else {
				// Success - close circuit
				if circuit.state == "half-open" {
					circuit.state = "closed"
					circuit.failures = 0
					logger.Info("circuit breaker closed",
						forge.F("peer", peerID),
					)
				}
			}

			return err
		}
	}
}

// DeduplicationMiddleware creates message deduplication middleware
func DeduplicationMiddleware(window time.Duration, logger forge.Logger) TransportMiddleware {
	type msgKey struct {
		from string
		id   string
	}

	seen := make(map[msgKey]time.Time)
	var seenMu sync.RWMutex

	// Cleanup goroutine
	go func() {
		ticker := time.NewTicker(window)
		defer ticker.Stop()

		for range ticker.C {
			seenMu.Lock()
			now := time.Now()
			for key, timestamp := range seen {
				if now.Sub(timestamp) > window {
					delete(seen, key)
				}
			}
			seenMu.Unlock()
		}
	}()

	return func(next TransportHandler) TransportHandler {
		return func(ctx context.Context, msg *internal.Message) error {
			key := msgKey{
				from: msg.From,
				id:   fmt.Sprintf("%d", msg.Index),
			}

			seenMu.Lock()
			_, duplicate := seen[key]
			if !duplicate {
				seen[key] = time.Now()
			}
			seenMu.Unlock()

			if duplicate {
				logger.Debug("duplicate message detected",
					forge.F("from", msg.From),
					forge.F("id", key.id),
				)
				return nil // Silently drop duplicates
			}

			return next(ctx, msg)
		}
	}
}
