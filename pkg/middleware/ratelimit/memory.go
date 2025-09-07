package ratelimit

import (
	"context"
	"sync"
	"time"
)

// MemoryLimiter implements an in-memory rate limiter using token bucket algorithm
type MemoryLimiter struct {
	config  Config
	buckets map[string]*tokenBucket
	mu      sync.RWMutex
}

type tokenBucket struct {
	tokens     int
	lastRefill time.Time
	mu         sync.Mutex
}

// NewMemoryLimiter creates a new memory-based rate limiter
func NewMemoryLimiter(config Config) (*MemoryLimiter, error) {
	return &MemoryLimiter{
		config:  config,
		buckets: make(map[string]*tokenBucket),
	}, nil
}

// Allow checks if a request should be allowed
func (ml *MemoryLimiter) Allow(ctx context.Context, key string) (bool, *LimitInfo, error) {
	ml.mu.Lock()
	bucket, exists := ml.buckets[key]
	if !exists {
		bucket = &tokenBucket{
			tokens:     ml.config.BurstSize,
			lastRefill: time.Now(),
		}
		ml.buckets[key] = bucket
	}
	ml.mu.Unlock()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()

	// Refill tokens based on time elapsed
	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int(elapsed.Seconds() * float64(ml.config.RequestsPerSecond))

	bucket.tokens += tokensToAdd
	if bucket.tokens > ml.config.BurstSize {
		bucket.tokens = ml.config.BurstSize
	}
	bucket.lastRefill = now

	// Check if request can be allowed
	allowed := bucket.tokens > 0
	if allowed {
		bucket.tokens--
	}

	// Calculate reset time (when bucket will be full again)
	tokensNeeded := ml.config.BurstSize - bucket.tokens
	secondsToFull := float64(tokensNeeded) / float64(ml.config.RequestsPerSecond)
	resetTime := now.Add(time.Duration(secondsToFull * float64(time.Second)))

	// Calculate retry after (time until next token is available)
	retryAfter := time.Duration(0)
	if !allowed {
		retryAfter = time.Duration(1.0 / float64(ml.config.RequestsPerSecond) * float64(time.Second))
	}

	limitInfo := &LimitInfo{
		Limit:      ml.config.BurstSize,
		Remaining:  bucket.tokens,
		ResetTime:  resetTime,
		RetryAfter: retryAfter,
	}

	return allowed, limitInfo, nil
}

// Reset resets the limit for a key
func (ml *MemoryLimiter) Reset(ctx context.Context, key string) error {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	if bucket, exists := ml.buckets[key]; exists {
		bucket.mu.Lock()
		bucket.tokens = ml.config.BurstSize
		bucket.lastRefill = time.Now()
		bucket.mu.Unlock()
	}

	return nil
}

// Close closes the memory limiter
func (ml *MemoryLimiter) Close() error {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	ml.buckets = make(map[string]*tokenBucket)
	return nil
}
