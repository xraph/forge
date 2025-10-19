package filters

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	streaming "github.com/xraph/forge/pkg/streaming/core"
)

// RateLimitStrategy represents different rate limiting strategies
type RateLimitStrategy string

const (
	RateLimitTokenBucket   RateLimitStrategy = "token_bucket"
	RateLimitSlidingWindow RateLimitStrategy = "sliding_window"
	RateLimitFixedWindow   RateLimitStrategy = "fixed_window"
	RateLimitLeakyBucket   RateLimitStrategy = "leaky_bucket"
)

// RateLimitScope represents the scope of rate limiting
type RateLimitScope string

const (
	RateLimitScopeConnection RateLimitScope = "connection"
	RateLimitScopeUser       RateLimitScope = "user"
	RateLimitScopeRoom       RateLimitScope = "room"
	RateLimitScopeGlobal     RateLimitScope = "global"
)

// RateLimitConfig contains configuration for rate limiting
type RateLimitConfig struct {
	Strategy          RateLimitStrategy `yaml:"strategy" default:"token_bucket"`
	Scope             RateLimitScope    `yaml:"scope" default:"connection"`
	RequestsPerSecond int               `yaml:"requests_per_second" default:"10"`
	RequestsPerMinute int               `yaml:"requests_per_minute" default:"100"`
	RequestsPerHour   int               `yaml:"requests_per_hour" default:"1000"`
	BurstSize         int               `yaml:"burst_size" default:"20"`
	WindowSize        time.Duration     `yaml:"window_size" default:"60s"`
	BlockDuration     time.Duration     `yaml:"block_duration" default:"0s"`
	Enabled           bool              `yaml:"enabled" default:"true"`
}

// DefaultRateLimitConfig returns default rate limiting configuration
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Strategy:          RateLimitTokenBucket,
		Scope:             RateLimitScopeConnection,
		RequestsPerSecond: 10,
		RequestsPerMinute: 100,
		RequestsPerHour:   1000,
		BurstSize:         20,
		WindowSize:        60 * time.Second,
		BlockDuration:     0, // No blocking by default
		Enabled:           true,
	}
}

// RateLimitResult represents the result of a rate limit check
type RateLimitResult struct {
	Allowed       bool          `json:"allowed"`
	Remaining     int           `json:"remaining"`
	ResetTime     time.Time     `json:"reset_time"`
	RetryAfter    time.Duration `json:"retry_after"`
	CurrentCount  int           `json:"current_count"`
	WindowStart   time.Time     `json:"window_start"`
	LimitExceeded bool          `json:"limit_exceeded"`
	BlockedUntil  time.Time     `json:"blocked_until,omitempty"`
	RejectReason  string        `json:"reject_reason,omitempty"`
}

// RateLimitState represents the current state of rate limiting for an entity
type RateLimitState struct {
	Key           string    `json:"key"`
	CurrentCount  int       `json:"current_count"`
	WindowStart   time.Time `json:"window_start"`
	LastRequest   time.Time `json:"last_request"`
	BlockedUntil  time.Time `json:"blocked_until"`
	TokensLeft    float64   `json:"tokens_left"`
	LastRefill    time.Time `json:"last_refill"`
	Violations    int       `json:"violations"`
	TotalRequests int64     `json:"total_requests"`
}

// RateLimitStats represents statistics for rate limiting
type RateLimitStats struct {
	Strategy         RateLimitStrategy          `json:"strategy"`
	Scope            RateLimitScope             `json:"scope"`
	TotalRequests    int64                      `json:"total_requests"`
	AllowedRequests  int64                      `json:"allowed_requests"`
	RejectedRequests int64                      `json:"rejected_requests"`
	ActiveLimits     int                        `json:"active_limits"`
	BlockedEntities  int                        `json:"blocked_entities"`
	RejectRate       float64                    `json:"reject_rate"`
	StatesByKey      map[string]*RateLimitState `json:"states_by_key"`
	LastReset        time.Time                  `json:"last_reset"`
}

// RateLimiter interface for rate limiting functionality
type RateLimiter interface {
	// Check if a request is allowed
	Allow(ctx context.Context, key string) (*RateLimitResult, error)

	// Get current state for a key
	GetState(key string) (*RateLimitState, error)

	// Reset rate limiting for a key
	Reset(key string) error

	// Block a key for a specific duration
	Block(key string, duration time.Duration, reason string) error

	// Unblock a key
	Unblock(key string) error

	// Get statistics
	GetStats() *RateLimitStats

	// Cleanup expired states
	Cleanup(ctx context.Context) error

	// Update configuration
	UpdateConfig(config RateLimitConfig) error
}

// MessageRateLimiter implements rate limiting for streaming messages
type MessageRateLimiter struct {
	config        RateLimitConfig
	states        map[string]*RateLimitState
	stats         *RateLimitStats
	mu            sync.RWMutex
	logger        common.Logger
	metrics       common.Metrics
	stopCh        chan struct{}
	cleanupTicker *time.Ticker
}

// NewMessageRateLimiter creates a new message rate limiter
func NewMessageRateLimiter(config RateLimitConfig, logger common.Logger, metrics common.Metrics) RateLimiter {
	limiter := &MessageRateLimiter{
		config:  config,
		states:  make(map[string]*RateLimitState),
		logger:  logger,
		metrics: metrics,
		stopCh:  make(chan struct{}),
		stats: &RateLimitStats{
			Strategy:    config.Strategy,
			Scope:       config.Scope,
			StatesByKey: make(map[string]*RateLimitState),
			LastReset:   time.Now(),
		},
	}

	// Start cleanup routine
	limiter.startCleanup()

	return limiter
}

// Allow checks if a request is allowed under the rate limit
func (rl *MessageRateLimiter) Allow(ctx context.Context, key string) (*RateLimitResult, error) {
	if !rl.config.Enabled {
		return &RateLimitResult{
			Allowed:   true,
			Remaining: rl.config.BurstSize,
		}, nil
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Update stats
	rl.stats.TotalRequests++

	// Get or create state
	state := rl.getOrCreateStateUnsafe(key)
	now := time.Now()

	// Check if currently blocked
	if !state.BlockedUntil.IsZero() && now.Before(state.BlockedUntil) {
		rl.stats.RejectedRequests++
		result := &RateLimitResult{
			Allowed:       false,
			LimitExceeded: true,
			BlockedUntil:  state.BlockedUntil,
			RetryAfter:    state.BlockedUntil.Sub(now),
			RejectReason:  "temporarily blocked due to rate limit violations",
		}
		rl.recordRejection(key, result)
		return result, nil
	}

	// Apply rate limiting strategy
	var result *RateLimitResult
	var err error

	switch rl.config.Strategy {
	case RateLimitTokenBucket:
		result, err = rl.applyTokenBucket(state, now)
	case RateLimitSlidingWindow:
		result, err = rl.applySlidingWindow(state, now)
	case RateLimitFixedWindow:
		result, err = rl.applyFixedWindow(state, now)
	case RateLimitLeakyBucket:
		result, err = rl.applyLeakyBucket(state, now)
	default:
		return nil, common.ErrInvalidConfig("rate_limit_strategy", fmt.Errorf("unknown strategy: %s", rl.config.Strategy))
	}

	if err != nil {
		return nil, err
	}

	// Update state
	state.LastRequest = now
	state.TotalRequests++

	if result.Allowed {
		rl.stats.AllowedRequests++
		rl.recordSuccess(key, result)
	} else {
		rl.stats.RejectedRequests++
		state.Violations++
		rl.recordRejection(key, result)

		// Apply blocking if configured
		if rl.config.BlockDuration > 0 && state.Violations >= 3 {
			state.BlockedUntil = now.Add(rl.config.BlockDuration)
			result.BlockedUntil = state.BlockedUntil
			result.RejectReason = fmt.Sprintf("blocked for %v due to repeated violations", rl.config.BlockDuration)
		}
	}

	// Update stats
	rl.updateStatsUnsafe()

	return result, nil
}

// applyTokenBucket implements token bucket rate limiting
func (rl *MessageRateLimiter) applyTokenBucket(state *RateLimitState, now time.Time) (*RateLimitResult, error) {
	// Initialize if first request
	if state.LastRefill.IsZero() {
		state.TokensLeft = float64(rl.config.BurstSize)
		state.LastRefill = now
	}

	// Calculate tokens to add based on time elapsed
	elapsed := now.Sub(state.LastRefill)
	tokensToAdd := elapsed.Seconds() * float64(rl.config.RequestsPerSecond)

	// Add tokens up to burst size
	state.TokensLeft += tokensToAdd
	if state.TokensLeft > float64(rl.config.BurstSize) {
		state.TokensLeft = float64(rl.config.BurstSize)
	}
	state.LastRefill = now

	// Check if we have tokens available
	if state.TokensLeft >= 1.0 {
		state.TokensLeft -= 1.0
		return &RateLimitResult{
			Allowed:      true,
			Remaining:    int(state.TokensLeft),
			CurrentCount: int(float64(rl.config.BurstSize) - state.TokensLeft),
		}, nil
	}

	// No tokens available
	nextToken := time.Duration(1.0/float64(rl.config.RequestsPerSecond)) * time.Second
	return &RateLimitResult{
		Allowed:       false,
		Remaining:     0,
		RetryAfter:    nextToken,
		LimitExceeded: true,
		CurrentCount:  rl.config.BurstSize,
		RejectReason:  "token bucket exhausted",
	}, nil
}

// applySlidingWindow implements sliding window rate limiting
func (rl *MessageRateLimiter) applySlidingWindow(state *RateLimitState, now time.Time) (*RateLimitResult, error) {
	windowStart := now.Add(-rl.config.WindowSize)

	// Reset if window has completely passed
	if state.WindowStart.Before(windowStart) {
		state.WindowStart = now
		state.CurrentCount = 0
	}

	// Check against per-second limit
	limit := rl.config.RequestsPerSecond
	if rl.config.WindowSize >= time.Minute {
		limit = rl.config.RequestsPerMinute
	}
	if rl.config.WindowSize >= time.Hour {
		limit = rl.config.RequestsPerHour
	}

	if state.CurrentCount >= limit {
		resetTime := state.WindowStart.Add(rl.config.WindowSize)
		return &RateLimitResult{
			Allowed:       false,
			Remaining:     0,
			ResetTime:     resetTime,
			RetryAfter:    resetTime.Sub(now),
			LimitExceeded: true,
			CurrentCount:  state.CurrentCount,
			WindowStart:   state.WindowStart,
			RejectReason:  "sliding window limit exceeded",
		}, nil
	}

	// Allow request
	state.CurrentCount++
	return &RateLimitResult{
		Allowed:      true,
		Remaining:    limit - state.CurrentCount,
		CurrentCount: state.CurrentCount,
		WindowStart:  state.WindowStart,
	}, nil
}

// applyFixedWindow implements fixed window rate limiting
func (rl *MessageRateLimiter) applyFixedWindow(state *RateLimitState, now time.Time) (*RateLimitResult, error) {
	// Calculate current window
	windowStart := now.Truncate(rl.config.WindowSize)

	// Reset counter if new window
	if state.WindowStart.Before(windowStart) {
		state.WindowStart = windowStart
		state.CurrentCount = 0
	}

	limit := rl.config.RequestsPerSecond
	if rl.config.WindowSize >= time.Minute {
		limit = rl.config.RequestsPerMinute
	}
	if rl.config.WindowSize >= time.Hour {
		limit = rl.config.RequestsPerHour
	}

	if state.CurrentCount >= limit {
		resetTime := windowStart.Add(rl.config.WindowSize)
		return &RateLimitResult{
			Allowed:       false,
			Remaining:     0,
			ResetTime:     resetTime,
			RetryAfter:    resetTime.Sub(now),
			LimitExceeded: true,
			CurrentCount:  state.CurrentCount,
			WindowStart:   windowStart,
			RejectReason:  "fixed window limit exceeded",
		}, nil
	}

	// Allow request
	state.CurrentCount++
	return &RateLimitResult{
		Allowed:      true,
		Remaining:    limit - state.CurrentCount,
		CurrentCount: state.CurrentCount,
		WindowStart:  windowStart,
		ResetTime:    windowStart.Add(rl.config.WindowSize),
	}, nil
}

// applyLeakyBucket implements leaky bucket rate limiting
func (rl *MessageRateLimiter) applyLeakyBucket(state *RateLimitState, now time.Time) (*RateLimitResult, error) {
	// Initialize if first request
	if state.LastRefill.IsZero() {
		state.CurrentCount = 0
		state.LastRefill = now
	}

	// Calculate requests that have "leaked" out
	elapsed := now.Sub(state.LastRefill)
	leakedRequests := int(elapsed.Seconds() * float64(rl.config.RequestsPerSecond))

	// Remove leaked requests
	state.CurrentCount -= leakedRequests
	if state.CurrentCount < 0 {
		state.CurrentCount = 0
	}
	state.LastRefill = now

	// Check if bucket is full
	if state.CurrentCount >= rl.config.BurstSize {
		leakTime := time.Duration(1.0/float64(rl.config.RequestsPerSecond)) * time.Second
		return &RateLimitResult{
			Allowed:       false,
			Remaining:     0,
			RetryAfter:    leakTime,
			LimitExceeded: true,
			CurrentCount:  state.CurrentCount,
			RejectReason:  "leaky bucket full",
		}, nil
	}

	// Add request to bucket
	state.CurrentCount++
	return &RateLimitResult{
		Allowed:      true,
		Remaining:    rl.config.BurstSize - state.CurrentCount,
		CurrentCount: state.CurrentCount,
	}, nil
}

// GetState returns the current state for a key
func (rl *MessageRateLimiter) GetState(key string) (*RateLimitState, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	state, exists := rl.states[key]
	if !exists {
		return nil, common.ErrServiceNotFound(key)
	}

	// Return a copy to avoid race conditions
	stateCopy := *state
	return &stateCopy, nil
}

// Reset resets the rate limiting state for a key
func (rl *MessageRateLimiter) Reset(key string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if _, exists := rl.states[key]; !exists {
		return common.ErrServiceNotFound(key)
	}

	delete(rl.states, key)

	if rl.logger != nil {
		rl.logger.Info("rate limit reset", logger.String("key", key))
	}

	if rl.metrics != nil {
		rl.metrics.Counter("streaming.rate_limit.resets").Inc()
	}

	return nil
}

// Block blocks a key for a specific duration
func (rl *MessageRateLimiter) Block(key string, duration time.Duration, reason string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	state := rl.getOrCreateStateUnsafe(key)
	state.BlockedUntil = time.Now().Add(duration)

	if rl.logger != nil {
		rl.logger.Info("key blocked",
			logger.String("key", key),
			logger.Duration("duration", duration),
			logger.String("reason", reason),
		)
	}

	if rl.metrics != nil {
		rl.metrics.Counter("streaming.rate_limit.blocks").Inc()
	}

	return nil
}

// Unblock unblocks a key
func (rl *MessageRateLimiter) Unblock(key string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	state, exists := rl.states[key]
	if !exists {
		return common.ErrServiceNotFound(key)
	}

	state.BlockedUntil = time.Time{}
	state.Violations = 0

	if rl.logger != nil {
		rl.logger.Info("key unblocked", logger.String("key", key))
	}

	if rl.metrics != nil {
		rl.metrics.Counter("streaming.rate_limit.unblocks").Inc()
	}

	return nil
}

// GetStats returns rate limiting statistics
func (rl *MessageRateLimiter) GetStats() *RateLimitStats {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	// Create a copy of stats to avoid race conditions
	stats := &RateLimitStats{
		Strategy:         rl.stats.Strategy,
		Scope:            rl.stats.Scope,
		TotalRequests:    rl.stats.TotalRequests,
		AllowedRequests:  rl.stats.AllowedRequests,
		RejectedRequests: rl.stats.RejectedRequests,
		ActiveLimits:     len(rl.states),
		StatesByKey:      make(map[string]*RateLimitState),
		LastReset:        rl.stats.LastReset,
	}

	// Calculate reject rate
	if stats.TotalRequests > 0 {
		stats.RejectRate = float64(stats.RejectedRequests) / float64(stats.TotalRequests)
	}

	// Count blocked entities
	blockedCount := 0
	now := time.Now()
	for key, state := range rl.states {
		if !state.BlockedUntil.IsZero() && now.Before(state.BlockedUntil) {
			blockedCount++
		}
		// Copy state for safety
		stateCopy := *state
		stats.StatesByKey[key] = &stateCopy
	}
	stats.BlockedEntities = blockedCount

	return stats
}

// Cleanup removes expired states
func (rl *MessageRateLimiter) Cleanup(ctx context.Context) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	expiredKeys := make([]string, 0)
	cleanupThreshold := now.Add(-time.Hour) // Remove states older than 1 hour

	for key, state := range rl.states {
		// Remove if last request was long ago and not currently blocked
		if state.LastRequest.Before(cleanupThreshold) &&
			(state.BlockedUntil.IsZero() || now.After(state.BlockedUntil)) {
			expiredKeys = append(expiredKeys, key)
		}
	}

	for _, key := range expiredKeys {
		delete(rl.states, key)
	}

	if len(expiredKeys) > 0 {
		if rl.logger != nil {
			rl.logger.Debug("rate limit states cleaned up",
				logger.Int("expired_count", len(expiredKeys)),
				logger.Int("remaining_count", len(rl.states)),
			)
		}

		if rl.metrics != nil {
			rl.metrics.Counter("streaming.rate_limit.cleanup").Add(float64(len(expiredKeys)))
			rl.metrics.Gauge("streaming.rate_limit.active_states").Set(float64(len(rl.states)))
		}
	}

	return nil
}

// UpdateConfig updates the rate limiting configuration
func (rl *MessageRateLimiter) UpdateConfig(config RateLimitConfig) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.config = config
	rl.stats.Strategy = config.Strategy
	rl.stats.Scope = config.Scope

	if rl.logger != nil {
		rl.logger.Info("rate limit config updated",
			logger.String("strategy", string(config.Strategy)),
			logger.String("scope", string(config.Scope)),
			logger.Int("requests_per_second", config.RequestsPerSecond),
			logger.Int("burst_size", config.BurstSize),
		)
	}

	return nil
}

// Helper methods

// getOrCreateStateUnsafe gets or creates a state entry (unsafe, requires lock)
func (rl *MessageRateLimiter) getOrCreateStateUnsafe(key string) *RateLimitState {
	state, exists := rl.states[key]
	if !exists {
		state = &RateLimitState{
			Key:         key,
			WindowStart: time.Now(),
			LastRefill:  time.Now(),
			TokensLeft:  float64(rl.config.BurstSize),
		}
		rl.states[key] = state
	}
	return state
}

// updateStatsUnsafe updates statistics (unsafe, requires lock)
func (rl *MessageRateLimiter) updateStatsUnsafe() {
	rl.stats.ActiveLimits = len(rl.states)
	if rl.stats.TotalRequests > 0 {
		rl.stats.RejectRate = float64(rl.stats.RejectedRequests) / float64(rl.stats.TotalRequests)
	}
}

// recordSuccess records a successful request
func (rl *MessageRateLimiter) recordSuccess(key string, result *RateLimitResult) {
	if rl.metrics != nil {
		rl.metrics.Counter("streaming.rate_limit.allowed", "key", key).Inc()
		rl.metrics.Histogram("streaming.rate_limit.remaining_tokens").Observe(float64(result.Remaining))
	}
}

// recordRejection records a rejected request
func (rl *MessageRateLimiter) recordRejection(key string, result *RateLimitResult) {
	if rl.metrics != nil {
		rl.metrics.Counter("streaming.rate_limit.rejected", "key", key).Inc()
		if result.RetryAfter.Milliseconds() > 0 {
			rl.metrics.Histogram("streaming.rate_limit.retry_after").Observe(result.RetryAfter.Seconds())
		}
	}

	if rl.logger != nil {
		rl.logger.Debug("rate limit exceeded",
			logger.String("key", key),
			logger.String("reason", result.RejectReason),
			logger.Duration("retry_after", result.RetryAfter),
		)
	}
}

// startCleanup starts the cleanup routine
func (rl *MessageRateLimiter) startCleanup() {
	rl.cleanupTicker = time.NewTicker(5 * time.Minute) // Cleanup every 5 minutes

	go func() {
		defer rl.cleanupTicker.Stop()

		for {
			select {
			case <-rl.cleanupTicker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := rl.Cleanup(ctx); err != nil {
					if rl.logger != nil {
						rl.logger.Error("rate limit cleanup failed", logger.Error(err))
					}
				}
				cancel()

			case <-rl.stopCh:
				return
			}
		}
	}()
}

// Stop stops the rate limiter
func (rl *MessageRateLimiter) Stop() {
	close(rl.stopCh)
	if rl.cleanupTicker != nil {
		rl.cleanupTicker.Stop()
	}
}

// RateLimitFilter implements message filtering based on rate limits
type RateLimitFilter struct {
	*BaseFilter
	limiter     RateLimiter
	keyResolver func(*streaming.Message, *FilterContext) string
}

// NewRateLimitFilter creates a new rate limiting filter
func NewRateLimitFilter(config RateLimitConfig, logger common.Logger, metrics common.Metrics) MessageFilter {
	filter := &RateLimitFilter{
		BaseFilter: NewBaseFilter("rate_limit", 5), // High priority
		limiter:    NewMessageRateLimiter(config, logger, metrics),
	}

	// Default key resolver based on scope
	filter.keyResolver = func(message *streaming.Message, ctx *FilterContext) string {
		switch config.Scope {
		case RateLimitScopeConnection:
			return ctx.ConnectionID
		case RateLimitScopeUser:
			return ctx.UserID
		case RateLimitScopeRoom:
			return ctx.RoomID
		case RateLimitScopeGlobal:
			return "global"
		default:
			return ctx.ConnectionID
		}
	}

	return filter
}

// Apply applies the rate limiting filter
func (f *RateLimitFilter) Apply(ctx context.Context, message *streaming.Message, filterContext *FilterContext) *FilterResult {
	if !f.IsEnabled() {
		return nil
	}

	// Resolve the key for rate limiting
	key := f.keyResolver(message, filterContext)

	// Check rate limit
	result, err := f.limiter.Allow(ctx, key)
	if err != nil {
		return &FilterResult{
			Action:  FilterActionBlock,
			Allowed: false,
			Reason:  fmt.Sprintf("rate limit check failed: %v", err),
		}
	}

	if !result.Allowed {
		return &FilterResult{
			Action:  FilterActionBlock,
			Allowed: false,
			Reason:  result.RejectReason,
			Metadata: map[string]interface{}{
				"rate_limit_exceeded": true,
				"retry_after":         result.RetryAfter.String(),
				"current_count":       result.CurrentCount,
				"limit_scope":         string(f.limiter.GetStats().Scope),
			},
		}
	}

	// Rate limit passed, add metadata
	return &FilterResult{
		Action:  FilterActionAllow,
		Allowed: true,
		Metadata: map[string]interface{}{
			"rate_limit_remaining": result.Remaining,
			"rate_limit_key":       key,
		},
	}
}

// SetKeyResolver sets a custom key resolver function
func (f *RateLimitFilter) SetKeyResolver(resolver func(*streaming.Message, *FilterContext) string) {
	f.keyResolver = resolver
}

// GetRateLimiter returns the underlying rate limiter
func (f *RateLimitFilter) GetRateLimiter() RateLimiter {
	return f.limiter
}

// Multi-tier rate limiting

// MultiTierRateLimiter implements multiple rate limiting tiers
type MultiTierRateLimiter struct {
	tiers   map[string]RateLimiter
	config  map[string]RateLimitConfig
	logger  common.Logger
	metrics common.Metrics
	mu      sync.RWMutex
}

// NewMultiTierRateLimiter creates a new multi-tier rate limiter
func NewMultiTierRateLimiter(configs map[string]RateLimitConfig, logger common.Logger, metrics common.Metrics) *MultiTierRateLimiter {
	limiter := &MultiTierRateLimiter{
		tiers:   make(map[string]RateLimiter),
		config:  configs,
		logger:  logger,
		metrics: metrics,
	}

	// Create limiters for each tier
	for name, config := range configs {
		limiter.tiers[name] = NewMessageRateLimiter(config, logger, metrics)
	}

	return limiter
}

// Allow checks all tiers and returns the most restrictive result
func (mrl *MultiTierRateLimiter) Allow(ctx context.Context, key string) (*RateLimitResult, error) {
	mrl.mu.RLock()
	defer mrl.mu.RUnlock()

	var mostRestrictive *RateLimitResult

	for tierName, limiter := range mrl.tiers {
		result, err := limiter.Allow(ctx, fmt.Sprintf("%s:%s", tierName, key))
		if err != nil {
			return nil, fmt.Errorf("tier %s failed: %w", tierName, err)
		}

		// Track the most restrictive result
		if !result.Allowed || (mostRestrictive != nil && result.Remaining < mostRestrictive.Remaining) {
			mostRestrictive = result
		}

		// If any tier blocks, return immediately
		if !result.Allowed {
			result.RejectReason = fmt.Sprintf("blocked by tier %s: %s", tierName, result.RejectReason)
			return result, nil
		}
	}

	// All tiers passed
	if mostRestrictive == nil {
		return &RateLimitResult{Allowed: true}, nil
	}

	return mostRestrictive, nil
}

// GetTierStats returns statistics for all tiers
func (mrl *MultiTierRateLimiter) GetTierStats() map[string]*RateLimitStats {
	mrl.mu.RLock()
	defer mrl.mu.RUnlock()

	stats := make(map[string]*RateLimitStats)
	for name, limiter := range mrl.tiers {
		stats[name] = limiter.GetStats()
	}

	return stats
}

// Strategy validation helpers

// ValidateStrategy validates a rate limiting strategy
func ValidateStrategy(strategy RateLimitStrategy) error {
	switch strategy {
	case RateLimitTokenBucket, RateLimitSlidingWindow, RateLimitFixedWindow, RateLimitLeakyBucket:
		return nil
	default:
		return common.ErrValidationError("strategy", fmt.Errorf("invalid rate limit strategy: %s", strategy))
	}
}

// ValidateScope validates a rate limiting scope
func ValidateScope(scope RateLimitScope) error {
	switch scope {
	case RateLimitScopeConnection, RateLimitScopeUser, RateLimitScopeRoom, RateLimitScopeGlobal:
		return nil
	default:
		return common.ErrValidationError("scope", fmt.Errorf("invalid rate limit scope: %s", scope))
	}
}

// ValidateConfig validates a rate limit configuration
func ValidateConfig(config RateLimitConfig) error {
	if err := ValidateStrategy(config.Strategy); err != nil {
		return err
	}

	if err := ValidateScope(config.Scope); err != nil {
		return err
	}

	if config.RequestsPerSecond <= 0 {
		return common.ErrValidationError("requests_per_second", fmt.Errorf("must be positive"))
	}

	if config.BurstSize <= 0 {
		return common.ErrValidationError("burst_size", fmt.Errorf("must be positive"))
	}

	if config.WindowSize <= 0 {
		return common.ErrValidationError("window_size", fmt.Errorf("must be positive"))
	}

	return nil
}
