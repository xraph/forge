package internal

import (
	"context"
	"time"
)

// Channel represents a pub/sub topic for message broadcasting.
type Channel interface {
	// Identity
	GetID() string
	GetName() string
	GetCreated() time.Time
	GetMessageCount() int64

	// Subscription management
	Subscribe(ctx context.Context, sub Subscription) error
	Unsubscribe(ctx context.Context, connID string) error
	GetSubscribers(ctx context.Context) ([]Subscription, error)
	GetSubscriberCount(ctx context.Context) (int, error)
	IsSubscribed(ctx context.Context, connID string) (bool, error)

	// Publishing
	Publish(ctx context.Context, message *Message) error

	// Lifecycle
	Delete(ctx context.Context) error
}

// ChannelStore provides backend storage for channels.
type ChannelStore interface {
	// CRUD operations
	Create(ctx context.Context, channel Channel) error
	Get(ctx context.Context, channelID string) (Channel, error)
	Delete(ctx context.Context, channelID string) error
	List(ctx context.Context) ([]Channel, error)
	Exists(ctx context.Context, channelID string) (bool, error)

	// Subscription operations
	AddSubscription(ctx context.Context, channelID string, sub Subscription) error
	RemoveSubscription(ctx context.Context, channelID, connID string) error
	GetSubscriptions(ctx context.Context, channelID string) ([]Subscription, error)
	GetSubscriberCount(ctx context.Context, channelID string) (int, error)
	IsSubscribed(ctx context.Context, channelID, connID string) (bool, error)

	// Publishing (for distributed backends)
	Publish(ctx context.Context, channelID string, message *Message) error

	// User's channels
	GetUserChannels(ctx context.Context, userID string) ([]Channel, error)

	// Lifecycle
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
}

// Subscription represents a connection's subscription to a channel.
type Subscription interface {
	GetConnID() string
	GetUserID() string
	GetSubscribedAt() time.Time
	GetFilters() map[string]any

	SetFilters(filters map[string]any)
	MatchesFilter(message *Message) bool
}

// ChannelOptions contains configuration for creating a channel.
type ChannelOptions struct {
	ID          string         `json:"id,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Private     bool           `json:"private"`
	Persistent  bool           `json:"persistent"` // Persist messages for late subscribers
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// SubscriptionOptions contains configuration for subscribing to a channel.
type SubscriptionOptions struct {
	ConnID  string         `json:"conn_id"`
	UserID  string         `json:"user_id"`
	Filters map[string]any `json:"filters,omitempty"` // Message filters (e.g., event type, user ID)
}

// ChannelPattern represents pattern-matching for channel subscriptions.
type ChannelPattern interface {
	// Match checks if a channel name matches the pattern
	Match(channelName string) bool

	// GetPattern returns the pattern string
	GetPattern() string
}

// Common channel patterns
const (
	// PatternExact matches exact channel name
	PatternExact = "exact"

	// PatternWildcard allows * wildcards (e.g., "user.*.events")
	PatternWildcard = "wildcard"

	// PatternPrefix matches channel name prefix (e.g., "system.")
	PatternPrefix = "prefix"

	// PatternSuffix matches channel name suffix (e.g., ".events")
	PatternSuffix = "suffix"
)
