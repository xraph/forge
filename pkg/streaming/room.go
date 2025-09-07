package streaming

import (
	"github.com/xraph/forge/pkg/common"
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

// RateLimitConfig contains rate limiting configuration
type RateLimitConfig = streamingcore.RateLimitConfig

// RateLimiter interface for rate limiting
type RateLimiter = streamingcore.RateLimiter

// RateLimitStats represents rate limiting statistics
type RateLimitStats = streamingcore.RateLimitStats

// TokenBucketRateLimiter implements token bucket rate limiting
type TokenBucketRateLimiter = streamingcore.TokenBucketRateLimiter

// NewTokenBucketRateLimiter creates a new token bucket rate limiter
func NewTokenBucketRateLimiter(config RateLimitConfig) RateLimiter {
	return streamingcore.NewTokenBucketRateLimiter(config)
}

// RoomType represents different types of rooms
type RoomType = streamingcore.RoomType

const (
	RoomTypePublic    = streamingcore.RoomTypePublic
	RoomTypePrivate   = streamingcore.RoomTypePrivate
	RoomTypeDirect    = streamingcore.RoomTypeDirect
	RoomTypeBroadcast = streamingcore.RoomTypeBroadcast
	RoomTypeSystem    = streamingcore.RoomTypeSystem
)

// RoomConfig contains room configuration
type RoomConfig = streamingcore.RoomConfig

// DefaultRoomConfig returns default room configuration
func DefaultRoomConfig() RoomConfig {
	return streamingcore.DefaultRoomConfig()
}

// RoomStats represents room statistics
type RoomStats = streamingcore.RoomStats

// Room represents a streaming room
type Room = streamingcore.Room

// UserJoinHandler Event handlers
type UserJoinHandler = streamingcore.UserJoinHandler
type UserLeaveHandler = streamingcore.UserLeaveHandler
type RoomMessageHandler = streamingcore.RoomMessageHandler
type RoomErrorHandler = streamingcore.RoomErrorHandler

// UserModeration represents user moderation state
type UserModeration = streamingcore.UserModeration

// DefaultRoom implements the Room interface
type DefaultRoom = streamingcore.DefaultRoom

// NewDefaultRoom creates a new default room
func NewDefaultRoom(id string, config RoomConfig, logger common.Logger, metrics common.Metrics) Room {
	return streamingcore.NewDefaultRoom(id, config, logger, metrics)
}
