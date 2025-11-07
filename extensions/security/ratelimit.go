package security

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	// Enabled determines if rate limiting is enabled
	Enabled bool

	// RequestsPerWindow is the maximum number of requests allowed per window
	RequestsPerWindow int

	// Window is the time window for rate limiting
	Window time.Duration

	// Note: Rate limit key extraction is handled internally using IP address
	// For custom key extraction, wrap the middleware with your own logic

	// SkipPaths is a list of paths to skip rate limiting
	SkipPaths []string

	// AutoApplyMiddleware automatically applies rate limiting middleware globally
	AutoApplyMiddleware bool

	// Store specifies the rate limit store backend ("memory", "redis")
	// For now, only memory is implemented
	Store string
}

// RateLimitInfo contains information about the current rate limit status
type RateLimitInfo struct {
	Limit      int           // Maximum requests allowed
	Remaining  int           // Remaining requests in current window
	Reset      time.Time     // When the rate limit resets
	RetryAfter time.Duration // How long to wait before retrying
}

// DefaultRateLimitConfig returns the default rate limit configuration
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Enabled:             true,
		RequestsPerWindow:   100,
		Window:              1 * time.Minute,
		SkipPaths:           []string{"/health", "/metrics"},
		AutoApplyMiddleware: false, // Default to false for backwards compatibility
		Store:               "memory",
	}
}

// tokenBucket represents a token bucket for rate limiting
type tokenBucket struct {
	tokens       int       // Current number of tokens
	lastRefill   time.Time // Last time tokens were refilled
	capacity     int       // Maximum number of tokens
	refillRate   int       // Tokens added per refill period
	refillPeriod time.Duration
	mu           sync.Mutex
}

// newTokenBucket creates a new token bucket
func newTokenBucket(capacity int, window time.Duration) *tokenBucket {
	return &tokenBucket{
		tokens:       capacity,
		lastRefill:   time.Now(),
		capacity:     capacity,
		refillRate:   capacity,
		refillPeriod: window,
	}
}

// take attempts to take n tokens from the bucket
// Returns true if tokens were taken, false if not enough tokens available
func (tb *tokenBucket) take(n int) (bool, *RateLimitInfo) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	if elapsed >= tb.refillPeriod {
		// Full refill if a full period has elapsed
		tb.tokens = tb.capacity
		tb.lastRefill = now
	}

	// Check if we have enough tokens
	if tb.tokens >= n {
		tb.tokens -= n
		return true, &RateLimitInfo{
			Limit:      tb.capacity,
			Remaining:  tb.tokens,
			Reset:      tb.lastRefill.Add(tb.refillPeriod),
			RetryAfter: 0,
		}
	}

	// Not enough tokens - calculate retry after
	timeUntilRefill := tb.refillPeriod - elapsed
	return false, &RateLimitInfo{
		Limit:      tb.capacity,
		Remaining:  0,
		Reset:      tb.lastRefill.Add(tb.refillPeriod),
		RetryAfter: timeUntilRefill,
	}
}

// MemoryRateLimiter implements in-memory rate limiting
type MemoryRateLimiter struct {
	config  RateLimitConfig
	buckets sync.Map // map[string]*tokenBucket
	logger  forge.Logger
	metrics forge.Metrics
}

// NewMemoryRateLimiter creates a new in-memory rate limiter
func NewMemoryRateLimiter(config RateLimitConfig, logger forge.Logger, metrics forge.Metrics) *MemoryRateLimiter {
	if config.RequestsPerWindow <= 0 {
		config.RequestsPerWindow = 100
	}
	if config.Window <= 0 {
		config.Window = 1 * time.Minute
	}

	limiter := &MemoryRateLimiter{
		config:  config,
		logger:  logger,
		metrics: metrics,
	}

	// Start cleanup goroutine
	go limiter.cleanup()

	return limiter
}

// cleanup periodically removes expired buckets
func (rl *MemoryRateLimiter) cleanup() {
	ticker := time.NewTicker(rl.config.Window * 2)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		rl.buckets.Range(func(key, value interface{}) bool {
			bucket := value.(*tokenBucket)
			bucket.mu.Lock()
			// Remove bucket if it hasn't been used in 2 windows
			if now.Sub(bucket.lastRefill) > rl.config.Window*2 {
				rl.buckets.Delete(key)
			}
			bucket.mu.Unlock()
			return true
		})
	}
}

// Allow checks if a request is allowed based on rate limit
func (rl *MemoryRateLimiter) Allow(key string) (bool, *RateLimitInfo) {
	// Get or create bucket for this key
	val, _ := rl.buckets.LoadOrStore(key, newTokenBucket(rl.config.RequestsPerWindow, rl.config.Window))
	bucket := val.(*tokenBucket)

	// Try to take a token
	allowed, info := bucket.take(1)

	if !allowed {
		// Rate limit exceeded
		if rl.metrics != nil {
			rl.metrics.Counter("security.ratelimit.exceeded").Inc()
		}
	}

	return allowed, info
}

// shouldSkipPath checks if the path should skip rate limiting
func (rl *MemoryRateLimiter) shouldSkipPath(path string) bool {
	for _, skipPath := range rl.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// RateLimitMiddleware returns a middleware function for rate limiting
func RateLimitMiddleware(limiter *MemoryRateLimiter) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if !limiter.config.Enabled {
				return next(ctx)
			}

			r := ctx.Request()
			w := ctx.Response()

			// Skip rate limiting for specified paths
			if limiter.shouldSkipPath(r.URL.Path) {
				return next(ctx)
			}

			// Extract rate limit key
			key := limiter.extractRateLimitKey(r)
			if key == "" {
				limiter.logger.Warn("rate limit key extraction failed, allowing request")
				return next(ctx)
			}

			// Check rate limit
			allowed, info := limiter.Allow(key)

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", info.Limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", info.Remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", info.Reset.Unix()))

			if !allowed {
				limiter.logger.Warn("rate limit exceeded",
					forge.F("key", key),
					forge.F("path", r.URL.Path),
				)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(info.RetryAfter.Seconds())))
				return ctx.String(http.StatusTooManyRequests, "Rate limit exceeded")
			}

			return next(ctx)
		}
	}
}

// extractRateLimitKey extracts the rate limit key from request
func (rl *MemoryRateLimiter) extractRateLimitKey(r *http.Request) string {
	// Try to get real IP from headers (for proxied requests)
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		ips := strings.Split(ip, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// WithRateLimitPerIP creates a rate limiter that limits by IP address
func WithRateLimitPerIP(requests int, window time.Duration) ConfigOption {
	return func(c *Config) {
		c.RateLimit.Enabled = true
		c.RateLimit.RequestsPerWindow = requests
		c.RateLimit.Window = window
	}
}

// WithRateLimitPerUser creates a rate limiter that limits by user ID
// Note: This requires session middleware to be active
func WithRateLimitPerUser(requests int, window time.Duration) ConfigOption {
	return func(c *Config) {
		c.RateLimit.Enabled = true
		c.RateLimit.RequestsPerWindow = requests
		c.RateLimit.Window = window
		// KeyFunc removed - use custom middleware wrapper for user-based rate limiting
	}
}
