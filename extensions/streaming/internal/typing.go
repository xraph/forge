package internal

import (
	"context"
	"time"
)

// TypingTracker manages typing indicator tracking per room/channel.
type TypingTracker interface {
	// Start and stop typing
	StartTyping(ctx context.Context, userID, roomID string) error
	StopTyping(ctx context.Context, userID, roomID string) error

	// Get typing users
	GetTypingUsers(ctx context.Context, roomID string) ([]string, error)
	IsTyping(ctx context.Context, userID, roomID string) (bool, error)

	// Broadcasting
	BroadcastTyping(ctx context.Context, roomID, userID string, isTyping bool) error

	// Cleanup expired typing indicators
	CleanupExpired(ctx context.Context) error

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// TypingStore provides backend storage for typing indicators.
type TypingStore interface {
	// Set and get typing status
	SetTyping(ctx context.Context, userID, roomID string, expiresAt time.Time) error
	RemoveTyping(ctx context.Context, userID, roomID string) error
	GetTypingUsers(ctx context.Context, roomID string) ([]string, error)
	IsTyping(ctx context.Context, userID, roomID string) (bool, error)

	// Cleanup
	CleanupExpired(ctx context.Context) error

	// Lifecycle
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
}

// TypingOptions contains configuration for typing indicators.
type TypingOptions struct {
	// Timeout after which typing indicator expires
	TypingTimeout time.Duration

	// Interval for cleanup of expired indicators
	CleanupInterval time.Duration

	// Broadcast typing events
	BroadcastEvents bool

	// Max typing users to track per room (prevents abuse)
	MaxTypingUsers int
}

// DefaultTypingOptions returns default typing indicator options.
func DefaultTypingOptions() TypingOptions {
	return TypingOptions{
		TypingTimeout:   3 * time.Second,
		CleanupInterval: 1 * time.Second,
		BroadcastEvents: true,
		MaxTypingUsers:  10,
	}
}

// TypingEvent represents a typing indicator event.
type TypingEvent struct {
	Type      string    `json:"type"` // "started", "stopped"
	UserID    string    `json:"user_id"`
	RoomID    string    `json:"room_id"`
	Timestamp time.Time `json:"timestamp"`
}

// Typing event types
const (
	TypingEventStarted = "started"
	TypingEventStopped = "stopped"
)
