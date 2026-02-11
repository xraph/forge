package gateway

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements token-bucket rate limiting.
type RateLimiter struct {
	config RateLimitConfig

	mu      sync.RWMutex
	buckets map[string]*tokenBucket
	global  *tokenBucket
}

type tokenBucket struct {
	tokens    float64
	maxTokens float64
	rate      float64 // tokens per second
	lastTime  time.Time
	mu        sync.Mutex
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:    float64(burst),
		maxTokens: float64(burst),
		rate:      rate,
		lastTime:  time.Now(),
	}
}

func (tb *tokenBucket) allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Refill tokens
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}

	if tb.tokens < 1 {
		return false
	}

	tb.tokens--

	return true
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		config:  config,
		buckets: make(map[string]*tokenBucket),
	}

	if !config.PerClient {
		rl.global = newTokenBucket(config.RequestsPerSec, config.Burst)
	}

	return rl
}

// Allow checks if a request is allowed.
func (rl *RateLimiter) Allow(r *http.Request) bool {
	if !rl.config.Enabled {
		return true
	}

	if rl.config.PerClient {
		key := rl.extractKey(r)

		return rl.allowForKey(key)
	}

	return rl.global.allow()
}

// AllowWithConfig checks rate limit with a per-route override.
func (rl *RateLimiter) AllowWithConfig(r *http.Request, config *RateLimitConfig) bool {
	if config == nil {
		return rl.Allow(r)
	}

	if !config.Enabled {
		return true
	}

	key := "route:" + r.URL.Path
	if config.PerClient {
		key += ":" + rl.extractKey(r)
	}

	rl.mu.RLock()
	bucket, ok := rl.buckets[key]
	rl.mu.RUnlock()

	if !ok {
		rl.mu.Lock()
		bucket, ok = rl.buckets[key]
		if !ok {
			bucket = newTokenBucket(config.RequestsPerSec, config.Burst)
			rl.buckets[key] = bucket
		}
		rl.mu.Unlock()
	}

	return bucket.allow()
}

func (rl *RateLimiter) allowForKey(key string) bool {
	rl.mu.RLock()
	bucket, ok := rl.buckets[key]
	rl.mu.RUnlock()

	if !ok {
		rl.mu.Lock()
		bucket, ok = rl.buckets[key]
		if !ok {
			bucket = newTokenBucket(rl.config.RequestsPerSec, rl.config.Burst)
			rl.buckets[key] = bucket
		}
		rl.mu.Unlock()
	}

	return bucket.allow()
}

func (rl *RateLimiter) extractKey(r *http.Request) string {
	// Use custom header if configured
	if rl.config.KeyHeader != "" {
		if key := r.Header.Get(rl.config.KeyHeader); key != "" {
			return key
		}
	}

	// Default to remote address
	return r.RemoteAddr
}

// Cleanup removes stale buckets older than the given duration.
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	for key, bucket := range rl.buckets {
		bucket.mu.Lock()
		if now.Sub(bucket.lastTime) > maxAge {
			delete(rl.buckets, key)
		}
		bucket.mu.Unlock()
	}
}
