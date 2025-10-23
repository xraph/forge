package internal

import (
	"context"
	"time"
)

// MessageStore provides backend storage for message history and persistence.
type MessageStore interface {
	// Save and retrieve messages
	Save(ctx context.Context, message *Message) error
	SaveBatch(ctx context.Context, messages []*Message) error
	Get(ctx context.Context, messageID string) (*Message, error)
	Delete(ctx context.Context, messageID string) error

	// History retrieval
	GetHistory(ctx context.Context, roomID string, query HistoryQuery) ([]*Message, error)
	GetThreadHistory(ctx context.Context, roomID, threadID string, query HistoryQuery) ([]*Message, error)
	GetUserMessages(ctx context.Context, userID string, query HistoryQuery) ([]*Message, error)

	// Search
	Search(ctx context.Context, roomID, searchTerm string, query HistoryQuery) ([]*Message, error)

	// Statistics
	GetMessageCount(ctx context.Context, roomID string) (int64, error)
	GetMessageCountByUser(ctx context.Context, roomID, userID string) (int64, error)

	// Cleanup
	DeleteOld(ctx context.Context, olderThan time.Duration) error
	DeleteByRoom(ctx context.Context, roomID string) error
	DeleteByUser(ctx context.Context, userID string) error

	// Lifecycle
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
}

// MessageStoreOptions contains configuration for message persistence.
type MessageStoreOptions struct {
	// Enable message persistence
	Enabled bool

	// Retention period for messages
	RetentionPeriod time.Duration

	// Cleanup interval
	CleanupInterval time.Duration

	// Maximum messages per room
	MaxMessagesPerRoom int64

	// Index messages for search
	EnableSearch bool

	// Compress old messages
	CompressOld bool

	// Compress messages older than this duration
	CompressAfter time.Duration
}

// DefaultMessageStoreOptions returns default message store options.
func DefaultMessageStoreOptions() MessageStoreOptions {
	return MessageStoreOptions{
		Enabled:            true,
		RetentionPeriod:    30 * 24 * time.Hour, // 30 days
		CleanupInterval:    1 * time.Hour,
		MaxMessagesPerRoom: 100000,
		EnableSearch:       true,
		CompressOld:        false,
		CompressAfter:      7 * 24 * time.Hour, // 7 days
	}
}

// MessageFilter provides advanced filtering for message retrieval.
type MessageFilter struct {
	// Time range
	StartTime time.Time
	EndTime   time.Time

	// User filter
	UserIDs []string

	// Message types
	Types []string

	// Thread filter
	ThreadID string

	// Search query
	SearchTerm string

	// Metadata filters
	Metadata map[string]any
}

// Pagination handles result pagination.
type Pagination struct {
	Limit  int
	Offset int
	Cursor string // For cursor-based pagination
}

// MessageStats provides statistics about messages.
type MessageStats struct {
	TotalMessages  int64            `json:"total_messages"`
	MessagesByType map[string]int64 `json:"messages_by_type"`
	MessagesByUser map[string]int64 `json:"messages_by_user"`
	MessagesPerDay map[string]int64 `json:"messages_per_day"`
	AveragePerDay  float64          `json:"average_per_day"`
	OldestMessage  time.Time        `json:"oldest_message"`
	NewestMessage  time.Time        `json:"newest_message"`
	StorageSize    int64            `json:"storage_size"` // Bytes
	CompressedSize int64            `json:"compressed_size,omitempty"`
	Extra          map[string]any   `json:"extra,omitempty"`
}
