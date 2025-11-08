package validation

import (
	"context"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// RateLimitValidatorConfig configures rate limit validation.
type RateLimitValidatorConfig struct {
	// Per-user limits
	MessagesPerSecond int
	MessagesPerMinute int
	BurstSize         int

	// Per-room limits
	RoomMessagesPerSecond int

	// Dynamic throttling
	EnableDynamicThrottling bool
	ThrottleThreshold       float64 // % of limit before throttling
}

// RateLimitValidator validates message rate limits.
type RateLimitValidator struct {
	config RateLimitValidatorConfig

	// Per-user tracking
	userLimits map[string]*rateLimitBucket
	userMu     sync.RWMutex

	// Per-room tracking
	roomLimits map[string]*rateLimitBucket
	roomMu     sync.RWMutex
}

type rateLimitBucket struct {
	tokens       int
	lastRefill   time.Time
	mu           sync.Mutex
	messageCount int
	windowStart  time.Time
}

// NewRateLimitValidator creates a rate limit validator.
func NewRateLimitValidator(config RateLimitValidatorConfig) *RateLimitValidator {
	return &RateLimitValidator{
		config:     config,
		userLimits: make(map[string]*rateLimitBucket),
		roomLimits: make(map[string]*rateLimitBucket),
	}
}

// Validate checks if message passes rate limit.
func (rlv *RateLimitValidator) Validate(ctx context.Context, msg *streaming.Message, sender streaming.EnhancedConnection) error {
	// Check per-user limit
	if rlv.config.MessagesPerSecond > 0 || rlv.config.MessagesPerMinute > 0 {
		if err := rlv.checkUserLimit(msg.UserID); err != nil {
			return err
		}
	}

	// Check per-room limit
	if rlv.config.RoomMessagesPerSecond > 0 && msg.RoomID != "" {
		if err := rlv.checkRoomLimit(msg.RoomID); err != nil {
			return err
		}
	}

	return nil
}

// ValidateContent is not used for rate limit validation.
func (rlv *RateLimitValidator) ValidateContent(content any) error {
	return nil
}

// ValidateMetadata is not used for rate limit validation.
func (rlv *RateLimitValidator) ValidateMetadata(metadata map[string]any) error {
	return nil
}

func (rlv *RateLimitValidator) checkUserLimit(userID string) error {
	rlv.userMu.Lock()

	bucket, ok := rlv.userLimits[userID]
	if !ok {
		bucket = &rateLimitBucket{
			tokens:      rlv.config.BurstSize,
			lastRefill:  time.Now(),
			windowStart: time.Now(),
		}
		rlv.userLimits[userID] = bucket
	}

	rlv.userMu.Unlock()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill)

	if rlv.config.MessagesPerSecond > 0 {
		tokensToAdd := int(elapsed.Seconds() * float64(rlv.config.MessagesPerSecond))
		bucket.tokens = min(bucket.tokens+tokensToAdd, rlv.config.BurstSize)
		bucket.lastRefill = now
	}

	// Check minute window
	if rlv.config.MessagesPerMinute > 0 {
		if now.Sub(bucket.windowStart) > time.Minute {
			bucket.messageCount = 0
			bucket.windowStart = now
		}

		if bucket.messageCount >= rlv.config.MessagesPerMinute {
			return NewValidationError("", "rate limit exceeded (per minute)", "RATE_LIMIT_MINUTE")
		}
	}

	// Check if tokens available
	if bucket.tokens <= 0 {
		return NewValidationError("", "rate limit exceeded (per second)", "RATE_LIMIT_SECOND")
	}

	// Consume token
	bucket.tokens--
	bucket.messageCount++

	return nil
}

func (rlv *RateLimitValidator) checkRoomLimit(roomID string) error {
	rlv.roomMu.Lock()

	bucket, ok := rlv.roomLimits[roomID]
	if !ok {
		bucket = &rateLimitBucket{
			tokens:     rlv.config.RoomMessagesPerSecond * 10, // 10 second burst
			lastRefill: time.Now(),
		}
		rlv.roomLimits[roomID] = bucket
	}

	rlv.roomMu.Unlock()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int(elapsed.Seconds() * float64(rlv.config.RoomMessagesPerSecond))
	bucket.tokens = min(bucket.tokens+tokensToAdd, rlv.config.RoomMessagesPerSecond*10)
	bucket.lastRefill = now

	// Check if tokens available
	if bucket.tokens <= 0 {
		return NewValidationError("", "room rate limit exceeded: "+roomID, "RATE_LIMIT_ROOM")
	}

	// Consume token
	bucket.tokens--

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
