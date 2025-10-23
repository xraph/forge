package security

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	config   RateLimitConfig
	limiters map[string]*Limiter
	mu       sync.RWMutex
}

// RateLimitConfig contains rate limiting configuration
type RateLimitConfig struct {
	DefaultLimit    int           `yaml:"default_limit" default:"100"`
	DefaultWindow   time.Duration `yaml:"default_window" default:"1m"`
	MaxLimiters     int           `yaml:"max_limiters" default:"10000"`
	CleanupInterval time.Duration `yaml:"cleanup_interval" default:"5m"`
	EnableRedis     bool          `yaml:"enable_redis" default:"false"`
	RedisURL        string        `yaml:"redis_url" default:"redis://localhost:6379"`
	RedisPassword   string        `yaml:"redis_password"`
	RedisDB         int           `yaml:"redis_db" default:"0"`
	EnableMetrics   bool          `yaml:"enable_metrics" default:"true"`
}

// Limiter represents a rate limiter for a specific key
type Limiter struct {
	Key       string        `json:"key"`
	Limit     int           `json:"limit"`
	Window    time.Duration `json:"window"`
	Count     int           `json:"count"`
	ResetTime time.Time     `json:"reset_time"`
	LastSeen  time.Time     `json:"last_seen"`
	mu        sync.RWMutex
}

// RateLimitRequest represents a rate limit request
type RateLimitRequest struct {
	Key     string            `json:"key"`
	Limit   int               `json:"limit,omitempty"`
	Window  time.Duration     `json:"window,omitempty"`
	Context map[string]string `json:"context"`
}

// RateLimitResult represents the result of rate limiting
type RateLimitResult struct {
	Allowed    bool          `json:"allowed"`
	Limit      int           `json:"limit"`
	Remaining  int           `json:"remaining"`
	ResetTime  time.Time     `json:"reset_time"`
	RetryAfter time.Duration `json:"retry_after,omitempty"`
	Reason     string        `json:"reason"`
}

// RateLimitStats represents rate limiting statistics
type RateLimitStats struct {
	TotalRequests   int64                    `json:"total_requests"`
	AllowedRequests int64                    `json:"allowed_requests"`
	BlockedRequests int64                    `json:"blocked_requests"`
	ActiveLimiters  int                      `json:"active_limiters"`
	LimiterStats    map[string]*LimiterStats `json:"limiter_stats"`
}

// LimiterStats represents statistics for a specific limiter
type LimiterStats struct {
	Key             string        `json:"key"`
	Limit           int           `json:"limit"`
	CurrentCount    int           `json:"current_count"`
	Window          time.Duration `json:"window"`
	ResetTime       time.Time     `json:"reset_time"`
	LastSeen        time.Time     `json:"last_seen"`
	TotalRequests   int64         `json:"total_requests"`
	BlockedRequests int64         `json:"blocked_requests"`
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		config:   config,
		limiters: make(map[string]*Limiter),
	}

	// Start cleanup goroutine
	if config.CleanupInterval > 0 {
		go rl.startCleanup()
	}

	return rl
}

// CheckRateLimit checks if a request is within rate limits
func (rl *RateLimiter) CheckRateLimit(ctx context.Context, req RateLimitRequest) (*RateLimitResult, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Get or create limiter
	limiter, err := rl.getOrCreateLimiter(req.Key, req.Limit, req.Window)
	if err != nil {
		return nil, fmt.Errorf("failed to get limiter: %w", err)
	}

	// Check if we're within the limit
	now := time.Now()

	// Reset counter if window has expired
	if now.After(limiter.ResetTime) {
		limiter.Count = 0
		limiter.ResetTime = now.Add(limiter.Window)
	}

	// Check if limit is exceeded
	if limiter.Count >= limiter.Limit {
		// Calculate retry after
		retryAfter := limiter.ResetTime.Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}

		return &RateLimitResult{
			Allowed:    false,
			Limit:      limiter.Limit,
			Remaining:  0,
			ResetTime:  limiter.ResetTime,
			RetryAfter: retryAfter,
			Reason:     "rate limit exceeded",
		}, nil
	}

	// Increment counter
	limiter.Count++
	limiter.LastSeen = now

	// Calculate remaining requests
	remaining := limiter.Limit - limiter.Count
	if remaining < 0 {
		remaining = 0
	}

	return &RateLimitResult{
		Allowed:   true,
		Limit:     limiter.Limit,
		Remaining: remaining,
		ResetTime: limiter.ResetTime,
		Reason:    "within rate limit",
	}, nil
}

// getOrCreateLimiter gets an existing limiter or creates a new one
func (rl *RateLimiter) getOrCreateLimiter(key string, limit int, window time.Duration) (*Limiter, error) {
	// Check if limiter already exists
	if limiter, exists := rl.limiters[key]; exists {
		return limiter, nil
	}

	// Check if we've reached the maximum number of limiters
	if len(rl.limiters) >= rl.config.MaxLimiters {
		return nil, errors.New("maximum number of limiters reached")
	}

	// Use defaults if not specified
	if limit <= 0 {
		limit = rl.config.DefaultLimit
	}
	if window <= 0 {
		window = rl.config.DefaultWindow
	}

	// Create new limiter
	limiter := &Limiter{
		Key:       key,
		Limit:     limit,
		Window:    window,
		Count:     0,
		ResetTime: time.Now().Add(window),
		LastSeen:  time.Now(),
	}

	rl.limiters[key] = limiter
	return limiter, nil
}

// startCleanup starts the cleanup goroutine
func (rl *RateLimiter) startCleanup() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rl.cleanup()
	}
}

// cleanup removes expired limiters
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	expiredKeys := make([]string, 0)

	for key, limiter := range rl.limiters {
		// Remove limiters that haven't been seen for a long time
		if now.Sub(limiter.LastSeen) > rl.config.CleanupInterval*2 {
			expiredKeys = append(expiredKeys, key)
		}
	}

	for _, key := range expiredKeys {
		delete(rl.limiters, key)
	}
}

// GetLimiter returns a limiter by key
func (rl *RateLimiter) GetLimiter(key string) (*Limiter, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	limiter, exists := rl.limiters[key]
	if !exists {
		return nil, errors.New("limiter not found")
	}

	return limiter, nil
}

// ListLimiters returns all active limiters
func (rl *RateLimiter) ListLimiters() []*Limiter {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	limiters := make([]*Limiter, 0, len(rl.limiters))
	for _, limiter := range rl.limiters {
		limiters = append(limiters, limiter)
	}

	return limiters
}

// GetStats returns rate limiting statistics
func (rl *RateLimiter) GetStats() *RateLimitStats {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	stats := &RateLimitStats{
		ActiveLimiters: len(rl.limiters),
		LimiterStats:   make(map[string]*LimiterStats),
	}

	for key, limiter := range rl.limiters {
		limiterStats := &LimiterStats{
			Key:          limiter.Key,
			Limit:        limiter.Limit,
			CurrentCount: limiter.Count,
			Window:       limiter.Window,
			ResetTime:    limiter.ResetTime,
			LastSeen:     limiter.LastSeen,
		}

		stats.LimiterStats[key] = limiterStats
	}

	return stats
}

// ResetLimiter resets a limiter
func (rl *RateLimiter) ResetLimiter(key string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[key]
	if !exists {
		return errors.New("limiter not found")
	}

	limiter.Count = 0
	limiter.ResetTime = time.Now().Add(limiter.Window)
	limiter.LastSeen = time.Now()

	return nil
}

// RemoveLimiter removes a limiter
func (rl *RateLimiter) RemoveLimiter(key string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.limiters, key)
	return nil
}

// UpdateLimiter updates a limiter's configuration
func (rl *RateLimiter) UpdateLimiter(key string, limit int, window time.Duration) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[key]
	if !exists {
		return errors.New("limiter not found")
	}

	limiter.Limit = limit
	limiter.Window = window
	limiter.ResetTime = time.Now().Add(window)

	return nil
}

// IsAllowed checks if a request is allowed without incrementing the counter
func (rl *RateLimiter) IsAllowed(ctx context.Context, req RateLimitRequest) (bool, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	limiter, exists := rl.limiters[req.Key]
	if !exists {
		// No limiter exists, so it's allowed
		return true, nil
	}

	// Check if we're within the limit
	now := time.Now()

	// Reset counter if window has expired
	if now.After(limiter.ResetTime) {
		return true, nil
	}

	// Check if limit is exceeded
	return limiter.Count < limiter.Limit, nil
}

// GetRemaining returns the remaining requests for a key
func (rl *RateLimiter) GetRemaining(key string) (int, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	limiter, exists := rl.limiters[key]
	if !exists {
		return 0, errors.New("limiter not found")
	}

	// Check if window has expired
	now := time.Now()
	if now.After(limiter.ResetTime) {
		return limiter.Limit, nil
	}

	remaining := limiter.Limit - limiter.Count
	if remaining < 0 {
		remaining = 0
	}

	return remaining, nil
}

// GetResetTime returns the reset time for a key
func (rl *RateLimiter) GetResetTime(key string) (time.Time, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	limiter, exists := rl.limiters[key]
	if !exists {
		return time.Time{}, errors.New("limiter not found")
	}

	return limiter.ResetTime, nil
}

// ClearAllLimiters clears all limiters
func (rl *RateLimiter) ClearAllLimiters() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.limiters = make(map[string]*Limiter)
}

// GetConfig returns the current configuration
func (rl *RateLimiter) GetConfig() RateLimitConfig {
	return rl.config
}

// UpdateConfig updates the configuration
func (rl *RateLimiter) UpdateConfig(config RateLimitConfig) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.config = config
}
