package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// slidingWindow implements sliding window rate limiter.
type slidingWindow struct {
	config RateLimitConfig
	store  Store

	// Local windows for in-memory mode
	windows map[string]*window
	mu      sync.RWMutex
}

type window struct {
	counts map[int64]int // timestamp -> count
	mu     sync.Mutex
}

// NewSlidingWindow creates a sliding window rate limiter.
func NewSlidingWindow(config RateLimitConfig, store Store) RateLimiter {
	sw := &slidingWindow{
		config:  config,
		store:   store,
		windows: make(map[string]*window),
	}

	// Start cleanup goroutine for in-memory mode
	if store == nil {
		go sw.cleanup()
	}

	return sw
}

// Allow checks if action is allowed.
func (sw *slidingWindow) Allow(ctx context.Context, key string, action string) (bool, error) {
	return sw.AllowN(ctx, key, action, 1)
}

// AllowN checks if N actions are allowed.
func (sw *slidingWindow) AllowN(ctx context.Context, key string, action string, n int) (bool, error) {
	limit, ok := sw.config.ActionLimits[action]
	if !ok {
		limit = RateLimit{
			Requests: sw.config.MessagesPerSecond,
			Window:   time.Second,
			Burst:    sw.config.BurstSize,
		}
	}

	// Use distributed store if available
	if sw.store != nil {
		return sw.allowWithStore(ctx, key, action, limit, n)
	}

	// Use in-memory window
	return sw.allowInMemory(key, action, limit, n)
}

// GetStatus returns rate limit status.
func (sw *slidingWindow) GetStatus(ctx context.Context, key string, action string) (*RateLimitStatus, error) {
	limit, ok := sw.config.ActionLimits[action]
	if !ok {
		limit = RateLimit{
			Requests: sw.config.MessagesPerSecond,
			Window:   time.Second,
			Burst:    sw.config.BurstSize,
		}
	}

	fullKey := sw.makeKey(key, action)
	now := time.Now()
	windowStart := now.Add(-limit.Window)

	if sw.store != nil {
		// Get count from store
		count, err := sw.store.Increment(ctx, fullKey, limit.Window)
		if err != nil {
			return nil, err
		}

		remaining := limit.Requests - int(count)
		if remaining < 0 {
			remaining = 0
		}

		return &RateLimitStatus{
			Allowed:   count <= int64(limit.Requests),
			Remaining: remaining,
			Limit:     limit.Requests,
			ResetAt:   windowStart.Add(limit.Window),
		}, nil
	}

	// In-memory status
	sw.mu.RLock()
	w, ok := sw.windows[fullKey]
	sw.mu.RUnlock()

	if !ok {
		return &RateLimitStatus{
			Allowed:   true,
			Remaining: limit.Requests,
			Limit:     limit.Requests,
			ResetAt:   now.Add(limit.Window),
		}, nil
	}

	w.mu.Lock()
	count := sw.countInWindow(w, windowStart)
	w.mu.Unlock()

	remaining := limit.Requests - count
	if remaining < 0 {
		remaining = 0
	}

	return &RateLimitStatus{
		Allowed:   count < limit.Requests,
		Remaining: remaining,
		Limit:     limit.Requests,
		ResetAt:   now.Add(limit.Window),
	}, nil
}

// Reset resets rate limit.
func (sw *slidingWindow) Reset(ctx context.Context, key string, action string) error {
	fullKey := sw.makeKey(key, action)

	if sw.store != nil {
		return sw.store.Delete(ctx, fullKey)
	}

	sw.mu.Lock()
	delete(sw.windows, fullKey)
	sw.mu.Unlock()

	return nil
}

func (sw *slidingWindow) allowInMemory(key, action string, limit RateLimit, n int) (bool, error) {
	fullKey := sw.makeKey(key, action)

	sw.mu.Lock()
	w, ok := sw.windows[fullKey]
	if !ok {
		w = &window{
			counts: make(map[int64]int),
		}
		sw.windows[fullKey] = w
	}
	sw.mu.Unlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-limit.Window)

	// Clean old entries
	sw.cleanWindow(w, windowStart)

	// Count requests in window
	count := sw.countInWindow(w, windowStart)

	// Check if within limit
	if count+n > limit.Requests {
		return false, nil
	}

	// Record this request
	timestamp := now.Unix()
	w.counts[timestamp] += n

	return true, nil
}

func (sw *slidingWindow) allowWithStore(ctx context.Context, key, action string, limit RateLimit, n int) (bool, error) {
	fullKey := sw.makeKey(key, action)

	// Increment counter for this window
	count, err := sw.store.Increment(ctx, fullKey, limit.Window)
	if err != nil {
		return false, fmt.Errorf("failed to increment counter: %w", err)
	}

	// Check if within limit
	if count > int64(limit.Requests) {
		return false, nil
	}

	return true, nil
}

func (sw *slidingWindow) countInWindow(w *window, windowStart time.Time) int {
	count := 0
	windowStartUnix := windowStart.Unix()

	for timestamp, c := range w.counts {
		if timestamp >= windowStartUnix {
			count += c
		}
	}

	return count
}

func (sw *slidingWindow) cleanWindow(w *window, windowStart time.Time) {
	windowStartUnix := windowStart.Unix()

	for timestamp := range w.counts {
		if timestamp < windowStartUnix {
			delete(w.counts, timestamp)
		}
	}
}

func (sw *slidingWindow) makeKey(key, action string) string {
	if action != "" {
		return fmt.Sprintf("ratelimit:sw:%s:%s", key, action)
	}
	return fmt.Sprintf("ratelimit:sw:%s", key)
}

func (sw *slidingWindow) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sw.mu.Lock()
		now := time.Now()
		for key, w := range sw.windows {
			w.mu.Lock()
			// Clean windows older than 1 hour
			sw.cleanWindow(w, now.Add(-time.Hour))
			if len(w.counts) == 0 {
				delete(sw.windows, key)
			}
			w.mu.Unlock()
		}
		sw.mu.Unlock()
	}
}
