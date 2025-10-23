package middleware

import (
	"net/http"
	"sync"
	"time"

	forge "github.com/xraph/forge"
)

// RateLimiter implements token bucket algorithm for rate limiting
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     int           // tokens per second
	capacity int           // max tokens
	cleanup  time.Duration // cleanup interval
}

type bucket struct {
	tokens    int
	lastCheck time.Time
}

// NewRateLimiter creates a new rate limiter
// rate: maximum requests per second
// burst: maximum burst size (capacity)
func NewRateLimiter(rate, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:  make(map[string]*bucket),
		rate:     rate,
		capacity: burst,
		cleanup:  5 * time.Minute,
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request from the given key should be allowed
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Get or create bucket
	b, exists := rl.buckets[key]
	if !exists {
		b = &bucket{
			tokens:    rl.capacity - 1, // -1 for this request
			lastCheck: now,
		}
		rl.buckets[key] = b
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

// cleanupLoop periodically removes old buckets
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for range ticker.C {
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

// RateLimit middleware enforces rate limiting per client
func RateLimit(limiter *RateLimiter, logger forge.Logger) forge.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Use remote address as key (could also use user ID, API key, etc.)
			key := r.RemoteAddr

			if !limiter.Allow(key) {
				if logger != nil {
					logger.Warn("rate limit exceeded")
				}

				w.Header().Set("X-RateLimit-Limit", "exceeded")
				http.Error(w, "Rate Limit Exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
