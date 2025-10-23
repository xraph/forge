package ratelimit

import (
	"context"
	"time"
)

// RateLimiter provides rate limiting functionality.
type RateLimiter interface {
	// Allow checks if action is allowed for key
	Allow(ctx context.Context, key string, action string) (bool, error)

	// AllowN checks if N actions are allowed
	AllowN(ctx context.Context, key string, action string, n int) (bool, error)

	// GetStatus returns current rate limit status
	GetStatus(ctx context.Context, key string, action string) (*RateLimitStatus, error)

	// Reset resets rate limit for key
	Reset(ctx context.Context, key string, action string) error
}

// RateLimitStatus represents current rate limit state.
type RateLimitStatus struct {
	Allowed   bool
	Remaining int
	Limit     int
	ResetAt   time.Time
	RetryIn   time.Duration
}

// RateLimitConfig configures rate limiting.
type RateLimitConfig struct {
	// Per-user limits
	MessagesPerSecond int
	MessagesPerMinute int
	BurstSize         int

	// Per-room limits
	RoomMessagesPerSecond int

	// Connection limits
	ConnectionsPerUser int
	ConnectionsPerIP   int

	// Action-specific limits
	ActionLimits map[string]RateLimit
}

// RateLimit defines a rate limit rule.
type RateLimit struct {
	Requests int           // Number of requests
	Window   time.Duration // Time window
	Burst    int           // Burst allowance
}

// DefaultRateLimitConfig returns default configuration.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		MessagesPerSecond:     10,
		MessagesPerMinute:     100,
		BurstSize:             20,
		RoomMessagesPerSecond: 100,
		ConnectionsPerUser:    5,
		ConnectionsPerIP:      20,
		ActionLimits: map[string]RateLimit{
			"message.send": {
				Requests: 10,
				Window:   time.Second,
				Burst:    20,
			},
			"room.join": {
				Requests: 5,
				Window:   time.Minute,
				Burst:    10,
			},
			"presence.update": {
				Requests: 60,
				Window:   time.Minute,
				Burst:    10,
			},
		},
	}
}

// Store provides persistence for rate limit data.
type Store interface {
	// Get retrieves rate limit data
	Get(ctx context.Context, key string) (*StoreData, error)

	// Set stores rate limit data
	Set(ctx context.Context, key string, data *StoreData, ttl time.Duration) error

	// Increment atomically increments counter
	Increment(ctx context.Context, key string, window time.Duration) (int64, error)

	// Delete removes rate limit data
	Delete(ctx context.Context, key string) error
}

// StoreData represents stored rate limit data.
type StoreData struct {
	Tokens      int
	LastRefill  time.Time
	Count       int64
	WindowStart time.Time
}
