package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// tokenBucket implements token bucket rate limiter.
type tokenBucket struct {
	config RateLimitConfig
	store  Store

	// Local buckets for in-memory mode
	buckets map[string]*bucket
	mu      sync.RWMutex
}

type bucket struct {
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

// NewTokenBucket creates a token bucket rate limiter.
func NewTokenBucket(config RateLimitConfig, store Store) RateLimiter {
	tb := &tokenBucket{
		config:  config,
		store:   store,
		buckets: make(map[string]*bucket),
	}

	// Start cleanup goroutine for in-memory mode
	if store == nil {
		go tb.cleanup()
	}

	return tb
}

// Allow checks if action is allowed.
func (tb *tokenBucket) Allow(ctx context.Context, key string, action string) (bool, error) {
	return tb.AllowN(ctx, key, action, 1)
}

// AllowN checks if N actions are allowed.
func (tb *tokenBucket) AllowN(ctx context.Context, key string, action string, n int) (bool, error) {
	limit, ok := tb.config.ActionLimits[action]
	if !ok {
		// Default limit
		limit = RateLimit{
			Requests: tb.config.MessagesPerSecond,
			Window:   time.Second,
			Burst:    tb.config.BurstSize,
		}
	}

	// Use distributed store if available
	if tb.store != nil {
		return tb.allowWithStore(ctx, key, limit, n)
	}

	// Use in-memory bucket
	return tb.allowInMemory(key, limit, n)
}

// GetStatus returns rate limit status.
func (tb *tokenBucket) GetStatus(ctx context.Context, key string, action string) (*RateLimitStatus, error) {
	limit, ok := tb.config.ActionLimits[action]
	if !ok {
		limit = RateLimit{
			Requests: tb.config.MessagesPerSecond,
			Window:   time.Second,
			Burst:    tb.config.BurstSize,
		}
	}

	if tb.store != nil {
		data, err := tb.store.Get(ctx, tb.makeKey(key, action))
		if err != nil {
			return nil, err
		}

		if data == nil {
			return &RateLimitStatus{
				Allowed:   true,
				Remaining: limit.Burst,
				Limit:     limit.Burst,
				ResetAt:   time.Now().Add(limit.Window),
			}, nil
		}

		return &RateLimitStatus{
			Allowed:   data.Tokens > 0,
			Remaining: data.Tokens,
			Limit:     limit.Burst,
			ResetAt:   data.LastRefill.Add(limit.Window),
		}, nil
	}

	// In-memory status
	tb.mu.RLock()
	b, ok := tb.buckets[tb.makeKey(key, action)]
	tb.mu.RUnlock()

	if !ok {
		return &RateLimitStatus{
			Allowed:   true,
			Remaining: limit.Burst,
			Limit:     limit.Burst,
			ResetAt:   time.Now().Add(limit.Window),
		}, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return &RateLimitStatus{
		Allowed:   b.tokens > 0,
		Remaining: int(b.tokens),
		Limit:     limit.Burst,
		ResetAt:   b.lastRefill.Add(limit.Window),
	}, nil
}

// Reset resets rate limit.
func (tb *tokenBucket) Reset(ctx context.Context, key string, action string) error {
	fullKey := tb.makeKey(key, action)

	if tb.store != nil {
		return tb.store.Delete(ctx, fullKey)
	}

	tb.mu.Lock()
	delete(tb.buckets, fullKey)
	tb.mu.Unlock()

	return nil
}

func (tb *tokenBucket) allowInMemory(key string, limit RateLimit, n int) (bool, error) {
	fullKey := tb.makeKey(key, "")

	tb.mu.Lock()
	b, ok := tb.buckets[fullKey]
	if !ok {
		b = &bucket{
			tokens:     float64(limit.Burst),
			lastRefill: time.Now(),
		}
		tb.buckets[fullKey] = b
	}
	tb.mu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(b.lastRefill)
	refillRate := float64(limit.Requests) / float64(limit.Window.Seconds())
	tokensToAdd := elapsed.Seconds() * refillRate

	b.tokens = min64(b.tokens+tokensToAdd, float64(limit.Burst))
	b.lastRefill = now

	// Check if enough tokens
	if b.tokens < float64(n) {
		return false, nil
	}

	// Consume tokens
	b.tokens -= float64(n)
	return true, nil
}

func (tb *tokenBucket) allowWithStore(ctx context.Context, key string, limit RateLimit, n int) (bool, error) {
	fullKey := tb.makeKey(key, "")

	data, err := tb.store.Get(ctx, fullKey)
	if err != nil {
		return false, fmt.Errorf("failed to get rate limit data: %w", err)
	}

	now := time.Now()
	if data == nil {
		data = &StoreData{
			Tokens:     limit.Burst,
			LastRefill: now,
		}
	}

	// Refill tokens
	elapsed := now.Sub(data.LastRefill)
	refillRate := float64(limit.Requests) / float64(limit.Window.Seconds())
	tokensToAdd := int(elapsed.Seconds() * refillRate)

	data.Tokens = min(data.Tokens+tokensToAdd, limit.Burst)
	data.LastRefill = now

	// Check if enough tokens
	if data.Tokens < n {
		// Save state even on rejection
		_ = tb.store.Set(ctx, fullKey, data, limit.Window*2)
		return false, nil
	}

	// Consume tokens
	data.Tokens -= n

	// Save updated state
	if err := tb.store.Set(ctx, fullKey, data, limit.Window*2); err != nil {
		return false, fmt.Errorf("failed to save rate limit data: %w", err)
	}

	return true, nil
}

func (tb *tokenBucket) makeKey(key, action string) string {
	if action != "" {
		return fmt.Sprintf("ratelimit:%s:%s", key, action)
	}
	return fmt.Sprintf("ratelimit:%s", key)
}

func (tb *tokenBucket) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		tb.mu.Lock()
		now := time.Now()
		for key, b := range tb.buckets {
			b.mu.Lock()
			if now.Sub(b.lastRefill) > time.Hour {
				delete(tb.buckets, key)
			}
			b.mu.Unlock()
		}
		tb.mu.Unlock()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
