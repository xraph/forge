package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// adaptiveRateLimiter implements adaptive rate limiting based on system load.
type adaptiveRateLimiter struct {
	config     RateLimitConfig
	baseLimit  RateLimit
	underlying RateLimiter

	// Adaptive parameters
	loadFactor     float64
	successRate    float64
	lastAdjustment time.Time
	mu             sync.RWMutex

	// Metrics
	totalRequests int64
	successCount  int64
	failureCount  int64
}

// AdaptiveConfig configures adaptive rate limiting.
type AdaptiveConfig struct {
	BaseConfig         RateLimitConfig
	MinLoadFactor      float64       // Minimum multiplier (e.g., 0.5 = 50% of base)
	MaxLoadFactor      float64       // Maximum multiplier (e.g., 2.0 = 200% of base)
	AdjustmentInterval time.Duration // How often to adjust
	TargetSuccessRate  float64       // Target success rate (e.g., 0.95)
}

// NewAdaptiveRateLimiter creates an adaptive rate limiter.
func NewAdaptiveRateLimiter(config AdaptiveConfig, underlying RateLimiter) RateLimiter {
	baseLimit := RateLimit{
		Requests: config.BaseConfig.MessagesPerSecond,
		Window:   time.Second,
		Burst:    config.BaseConfig.BurstSize,
	}

	arl := &adaptiveRateLimiter{
		config:         config.BaseConfig,
		baseLimit:      baseLimit,
		underlying:     underlying,
		loadFactor:     1.0,
		lastAdjustment: time.Now(),
	}

	// Start adjustment goroutine
	go arl.adjustLoop(config)

	return arl
}

// Allow checks if action is allowed.
func (arl *adaptiveRateLimiter) Allow(ctx context.Context, key string, action string) (bool, error) {
	return arl.AllowN(ctx, key, action, 1)
}

// AllowN checks if N actions are allowed.
func (arl *adaptiveRateLimiter) AllowN(ctx context.Context, key string, action string, n int) (bool, error) {
	// Track request
	arl.mu.Lock()
	arl.totalRequests++
	arl.mu.Unlock()

	// Use underlying limiter with adjusted limit
	allowed, err := arl.underlying.AllowN(ctx, key, action, n)

	// Track result
	arl.mu.Lock()
	if allowed {
		arl.successCount++
	} else {
		arl.failureCount++
	}
	arl.mu.Unlock()

	return allowed, err
}

// GetStatus returns rate limit status with adaptive adjustment.
func (arl *adaptiveRateLimiter) GetStatus(ctx context.Context, key string, action string) (*RateLimitStatus, error) {
	status, err := arl.underlying.GetStatus(ctx, key, action)
	if err != nil {
		return nil, err
	}

	// Adjust limits based on load factor
	arl.mu.RLock()
	loadFactor := arl.loadFactor
	arl.mu.RUnlock()

	status.Limit = int(float64(status.Limit) * loadFactor)
	status.Remaining = int(float64(status.Remaining) * loadFactor)

	return status, nil
}

// Reset resets rate limit.
func (arl *adaptiveRateLimiter) Reset(ctx context.Context, key string, action string) error {
	return arl.underlying.Reset(ctx, key, action)
}

func (arl *adaptiveRateLimiter) adjustLoop(config AdaptiveConfig) {
	ticker := time.NewTicker(config.AdjustmentInterval)
	defer ticker.Stop()

	for range ticker.C {
		arl.adjust(config)
	}
}

func (arl *adaptiveRateLimiter) adjust(config AdaptiveConfig) {
	arl.mu.Lock()
	defer arl.mu.Unlock()

	// Calculate success rate
	if arl.totalRequests == 0 {
		return
	}

	arl.successRate = float64(arl.successCount) / float64(arl.totalRequests)

	// Adjust load factor based on success rate
	if arl.successRate < config.TargetSuccessRate {
		// Too many failures, reduce load factor
		arl.loadFactor *= 0.9
		if arl.loadFactor < config.MinLoadFactor {
			arl.loadFactor = config.MinLoadFactor
		}
	} else if arl.successRate > config.TargetSuccessRate+0.05 {
		// High success rate, can increase load factor
		arl.loadFactor *= 1.1
		if arl.loadFactor > config.MaxLoadFactor {
			arl.loadFactor = config.MaxLoadFactor
		}
	}

	// Reset counters
	arl.totalRequests = 0
	arl.successCount = 0
	arl.failureCount = 0
	arl.lastAdjustment = time.Now()
}

// GetLoadFactor returns current load factor.
func (arl *adaptiveRateLimiter) GetLoadFactor() float64 {
	arl.mu.RLock()
	defer arl.mu.RUnlock()
	return arl.loadFactor
}

// GetSuccessRate returns current success rate.
func (arl *adaptiveRateLimiter) GetSuccessRate() float64 {
	arl.mu.RLock()
	defer arl.mu.RUnlock()
	return arl.successRate
}

// DefaultAdaptiveConfig returns default adaptive configuration.
func DefaultAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		BaseConfig:         DefaultRateLimitConfig(),
		MinLoadFactor:      0.5,
		MaxLoadFactor:      2.0,
		AdjustmentInterval: 10 * time.Second,
		TargetSuccessRate:  0.95,
	}
}

// localStore implements Store for in-memory rate limiting.
type localStore struct {
	data map[string]*StoreData
	mu   sync.RWMutex
}

// NewLocalStore creates an in-memory store.
func NewLocalStore() Store {
	return &localStore{
		data: make(map[string]*StoreData),
	}
}

func (ls *localStore) Get(ctx context.Context, key string) (*StoreData, error) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	data, ok := ls.data[key]
	if !ok {
		return nil, nil
	}

	return data, nil
}

func (ls *localStore) Set(ctx context.Context, key string, data *StoreData, ttl time.Duration) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	ls.data[key] = data
	return nil
}

func (ls *localStore) Increment(ctx context.Context, key string, window time.Duration) (int64, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	data, ok := ls.data[key]
	if !ok {
		data = &StoreData{
			Count:       0,
			WindowStart: time.Now(),
		}
		ls.data[key] = data
	}

	// Check if window expired
	if time.Since(data.WindowStart) > window {
		data.Count = 0
		data.WindowStart = time.Now()
	}

	data.Count++
	return data.Count, nil
}

func (ls *localStore) Delete(ctx context.Context, key string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	delete(ls.data, key)
	return nil
}

// Format formats rate limit status.
func (rls *RateLimitStatus) Format() string {
	if rls.Allowed {
		return fmt.Sprintf("Allowed (%d/%d remaining, resets at %s)",
			rls.Remaining, rls.Limit, rls.ResetAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("Rate limited (retry in %s, resets at %s)",
		rls.RetryIn, rls.ResetAt.Format(time.RFC3339))
}
