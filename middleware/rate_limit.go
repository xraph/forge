package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	forge "github.com/xraph/forge"
)

// defaultMaxBuckets caps how many distinct keys a RateLimiter tracks. Without a
// cap the bucket map is an unbounded, attacker-driven allocation: every new key
// adds an entry and cleanup only runs periodically.
const defaultMaxBuckets = 100_000

// RateLimiter implements token bucket algorithm for rate limiting.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	rate       int           // tokens per second
	capacity   int           // max tokens
	cleanup    time.Duration // cleanup interval
	maxBuckets int
	stopOnce   sync.Once
	stopCh     chan struct{}
}

type bucket struct {
	tokens    int
	lastCheck time.Time
}

// NewRateLimiter creates a new rate limiter
// rate: maximum requests per second
// burst: maximum burst size (capacity)
//
// Call Stop when the limiter is no longer needed to release its cleanup
// goroutine.
func NewRateLimiter(rate, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:    make(map[string]*bucket),
		rate:       rate,
		capacity:   burst,
		cleanup:    5 * time.Minute,
		maxBuckets: defaultMaxBuckets,
		stopCh:     make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// SetMaxBuckets overrides how many distinct keys are tracked before new keys
// are rejected. Values below 1 are ignored.
func (rl *RateLimiter) SetMaxBuckets(maxBuckets int) {
	if maxBuckets < 1 {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.maxBuckets = maxBuckets
}

// Stop terminates the cleanup goroutine. Safe to call more than once.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stopCh) })
}

// Allow checks if a request from the given key should be allowed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Get or create bucket
	b, exists := rl.buckets[key]
	if !exists {
		// At capacity: deny rather than grow without bound. Denying is the
		// safe direction — evicting instead would let an attacker flush
		// legitimate clients' buckets and reset their limits.
		if len(rl.buckets) >= rl.maxBuckets {
			return false
		}

		rl.buckets[key] = &bucket{
			tokens:    rl.capacity - 1, // -1 for this request
			lastCheck: now,
		}

		return true
	}

	// Refill tokens based on time passed
	elapsed := now.Sub(b.lastCheck)
	tokensToAdd := int(elapsed.Seconds() * float64(rl.rate))

	b.tokens += tokensToAdd
	if b.tokens > rl.capacity {
		b.tokens = rl.capacity
	}

	b.lastCheck = now

	// Check if we have tokens available
	if b.tokens > 0 {
		b.tokens--

		return true
	}

	return false
}

// cleanupLoop periodically removes old buckets.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()

			now := time.Now()
			for key, b := range rl.buckets {
				if now.Sub(b.lastCheck) > rl.cleanup {
					delete(rl.buckets, key)
				}
			}

			rl.mu.Unlock()
		}
	}
}

// ClientIP returns the client's IP address from RemoteAddr, discarding the port.
//
// This is the default rate-limit key. Using RemoteAddr verbatim does not work:
// Go's HTTP server sets it to "IP:ephemeral-port" and the port changes per
// connection, so every connection would get a fresh bucket — no effective limit
// at all, plus unbounded map growth.
//
// This deliberately ignores X-Forwarded-For. Behind a proxy every client
// collapses into the proxy's IP, so supply a RateLimitKeyFunc that reads the
// forwarded header only for hops you actually trust — reading it
// unconditionally is a header-spoofing bypass.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr carried no port (some test servers, unix sockets).
		return r.RemoteAddr
	}

	return host
}

// RateLimitKeyFunc derives the bucket key for a request. Return "" to skip rate
// limiting for that request.
type RateLimitKeyFunc func(*http.Request) string

// RateLimit middleware enforces rate limiting per client, keyed on client IP.
func RateLimit(limiter *RateLimiter, logger forge.Logger) forge.Middleware {
	return RateLimitWithKey(limiter, ClientIP, logger)
}

// RateLimitWithKey enforces rate limiting using a caller-supplied key function.
// Use this to key on an authenticated user ID, an API key, or a trusted
// forwarded-for hop instead of the client IP.
func RateLimitWithKey(limiter *RateLimiter, keyFunc RateLimitKeyFunc, logger forge.Logger) forge.Middleware {
	if keyFunc == nil {
		keyFunc = ClientIP
	}

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			key := keyFunc(ctx.Request())
			if key == "" {
				return next(ctx)
			}

			if !limiter.Allow(key) {
				if logger != nil {
					logger.Warn("rate limit exceeded")
				}

				ctx.Response().Header().Set("X-Ratelimit-Limit", "exceeded")

				return ctx.String(http.StatusTooManyRequests, "Rate Limit Exceeded")
			}

			return next(ctx)
		}
	}
}
