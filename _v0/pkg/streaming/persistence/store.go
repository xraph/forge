package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/streaming"
)

// MessageStore defines the interface for message persistence
type MessageStore interface {
	// Message operations
	StoreMessage(ctx context.Context, message *streaming.Message) error
	GetMessage(ctx context.Context, messageID string) (*streaming.Message, error)
	DeleteMessage(ctx context.Context, messageID string) error

	// Room message operations
	GetRoomMessages(ctx context.Context, roomID string, filter *MessageFilter) ([]*streaming.Message, error)
	GetRoomMessageCount(ctx context.Context, roomID string) (int64, error)
	DeleteRoomMessages(ctx context.Context, roomID string, olderThan *time.Time) error

	// User message operations
	GetUserMessages(ctx context.Context, userID string, filter *MessageFilter) ([]*streaming.Message, error)
	GetUserMessageCount(ctx context.Context, userID string) (int64, error)

	// Message history and replay
	GetMessageHistory(ctx context.Context, roomID string, since *time.Time, limit int) ([]*streaming.Message, error)
	GetMessagesByTimeRange(ctx context.Context, roomID string, start, end time.Time) ([]*streaming.Message, error)

	// Bulk operations
	StoreMessages(ctx context.Context, messages []*streaming.Message) error
	DeleteMessages(ctx context.Context, messageIDs []string) error

	// Statistics
	GetMessageStats(ctx context.Context, roomID string) (*MessageStats, error)
	GetGlobalStats(ctx context.Context) (*GlobalMessageStats, error)

	// Cleanup and maintenance
	CleanupExpiredMessages(ctx context.Context, ttl time.Duration) (int64, error)
	Vacuum(ctx context.Context) error

	// Health and status
	HealthCheck(ctx context.Context) error
}

// MessageFilter represents filtering criteria for message queries
type MessageFilter struct {
	// Message types to include
	Types []streaming.MessageType `json:"types,omitempty"`

	// User filters
	FromUsers []string `json:"from_users,omitempty"`
	ToUsers   []string `json:"to_users,omitempty"`

	// Time range
	Since *time.Time `json:"since,omitempty"`
	Until *time.Time `json:"until,omitempty"`

	// Pagination
	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`

	// Sorting
	SortBy    MessageSortField `json:"sort_by,omitempty"`
	SortOrder SortOrder        `json:"sort_order,omitempty"`

	// Content filters
	ContentFilter *ContentFilter `json:"content_filter,omitempty"`

	// Metadata filters
	MetadataFilters map[string]interface{} `json:"metadata_filters,omitempty"`
}

// MessageSortField represents fields that can be used for sorting
type MessageSortField string

const (
	SortByTimestamp MessageSortField = "timestamp"
	SortByType      MessageSortField = "type"
	SortByFrom      MessageSortField = "from"
	SortByTo        MessageSortField = "to"
)

// SortOrder represents sort order
type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

// ContentFilter represents content-based filtering
type ContentFilter struct {
	// Text search
	SearchText    string `json:"search_text,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`

	// Content patterns
	IncludePatterns []string `json:"include_patterns,omitempty"`
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`

	// Content length
	MinLength *int `json:"min_length,omitempty"`
	MaxLength *int `json:"max_length,omitempty"`
}

// MessageStats represents message statistics for a room
type MessageStats struct {
	RoomID           string                          `json:"room_id"`
	TotalMessages    int64                           `json:"total_messages"`
	MessagesByType   map[streaming.MessageType]int64 `json:"messages_by_type"`
	MessagesByUser   map[string]int64                `json:"messages_by_user"`
	MessagesByHour   []int64                         `json:"messages_by_hour"`
	AverageSize      float64                         `json:"average_size"`
	FirstMessage     *time.Time                      `json:"first_message,omitempty"`
	LastMessage      *time.Time                      `json:"last_message,omitempty"`
	PeakHour         *time.Time                      `json:"peak_hour,omitempty"`
	PeakMessageCount int64                           `json:"peak_message_count"`
}

// GlobalMessageStats represents global message statistics
type GlobalMessageStats struct {
	TotalMessages  int64                           `json:"total_messages"`
	TotalRooms     int64                           `json:"total_rooms"`
	TotalUsers     int64                           `json:"total_users"`
	MessagesByType map[streaming.MessageType]int64 `json:"messages_by_type"`
	MessagesPerDay []int64                         `json:"messages_per_day"`
	ActiveRooms    int64                           `json:"active_rooms"`
	AverageSize    float64                         `json:"average_size"`
	StorageUsed    int64                           `json:"storage_used"`
	OldestMessage  *time.Time                      `json:"oldest_message,omitempty"`
	NewestMessage  *time.Time                      `json:"newest_message,omitempty"`
}

// StoredMessage represents a persisted message with additional metadata
type StoredMessage struct {
	*streaming.Message
	StoredAt   time.Time  `json:"stored_at"`
	Size       int        `json:"size"`
	Compressed bool       `json:"compressed"`
	Checksum   string     `json:"checksum,omitempty"`
	Tags       []string   `json:"tags,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// MessageStoreConfig represents configuration for message stores
type MessageStoreConfig struct {
	// Storage settings
	MaxMessageSize   int64 `yaml:"max_message_size" default:"65536"`
	CompressionLevel int   `yaml:"compression_level" default:"6"`
	EnableChecksum   bool  `yaml:"enable_checksum" default:"true"`

	// Retention settings
	DefaultTTL       time.Duration `yaml:"default_ttl" default:"720h"`
	MaxRetentionTime time.Duration `yaml:"max_retention_time" default:"8760h"`
	CleanupInterval  time.Duration `yaml:"cleanup_interval" default:"1h"`

	// Performance settings
	BatchSize         int           `yaml:"batch_size" default:"1000"`
	MaxConnections    int           `yaml:"max_connections" default:"10"`
	ConnectionTimeout time.Duration `yaml:"connection_timeout" default:"30s"`
	QueryTimeout      time.Duration `yaml:"query_timeout" default:"10s"`

	// Indexing settings
	EnableFullTextSearch bool `yaml:"enable_full_text_search" default:"true"`
	IndexBatchSize       int  `yaml:"index_batch_size" default:"10000"`

	// Caching settings
	EnableCache bool          `yaml:"enable_cache" default:"true"`
	CacheSize   int           `yaml:"cache_size" default:"1000"`
	CacheTTL    time.Duration `yaml:"cache_ttl" default:"5m"`
}

// DefaultMessageStoreConfig returns default configuration
func DefaultMessageStoreConfig() MessageStoreConfig {
	return MessageStoreConfig{
		MaxMessageSize:       65536, // 64KB
		CompressionLevel:     6,
		EnableChecksum:       true,
		DefaultTTL:           30 * 24 * time.Hour,  // 30 days
		MaxRetentionTime:     365 * 24 * time.Hour, // 1 year
		CleanupInterval:      1 * time.Hour,
		BatchSize:            1000,
		MaxConnections:       10,
		ConnectionTimeout:    30 * time.Second,
		QueryTimeout:         10 * time.Second,
		EnableFullTextSearch: true,
		IndexBatchSize:       10000,
		EnableCache:          true,
		CacheSize:            1000,
		CacheTTL:             5 * time.Minute,
	}
}

// MessageStoreFactory creates message stores based on configuration
type MessageStoreFactory interface {
	CreateStore(config MessageStoreConfig) (MessageStore, error)
	GetSupportedTypes() []string
}

// StoreType represents different store implementations
type StoreType string

const (
	StoreTypeMemory   StoreType = "memory"
	StoreTypeDatabase StoreType = "database"
	StoreTypeRedis    StoreType = "redis"
	StoreTypeFile     StoreType = "file"
	StoreTypeS3       StoreType = "s3"
	StoreTypeHybrid   StoreType = "hybrid"
)

// MessageStoreError represents store-specific errors
type MessageStoreError struct {
	Operation string
	StoreType string
	Cause     error
	Context   map[string]interface{}
}

func (e *MessageStoreError) Error() string {
	return common.NewForgeError("MESSAGE_STORE_ERROR",
		fmt.Sprintf("store operation '%s' failed in %s store", e.Operation, e.StoreType),
		e.Cause).Error()
}

// NewMessageStoreError creates a new message store error
func NewMessageStoreError(operation, storeType string, cause error) *MessageStoreError {
	return &MessageStoreError{
		Operation: operation,
		StoreType: storeType,
		Cause:     cause,
		Context:   make(map[string]interface{}),
	}
}

// WithContext adds context to the error
func (e *MessageStoreError) WithContext(key string, value interface{}) *MessageStoreError {
	e.Context[key] = value
	return e
}

// MessageArchiver handles long-term archival of messages
type MessageArchiver interface {
	// Archive messages to long-term storage
	ArchiveMessages(ctx context.Context, messages []*streaming.Message, archiveLocation string) error

	// Retrieve archived messages
	RetrieveArchivedMessages(ctx context.Context, archiveLocation string, filter *MessageFilter) ([]*streaming.Message, error)

	// List available archives
	ListArchives(ctx context.Context) ([]ArchiveInfo, error)

	// Delete archived data
	DeleteArchive(ctx context.Context, archiveLocation string) error
}

// ArchiveInfo represents information about an archive
type ArchiveInfo struct {
	Location     string    `json:"location"`
	CreatedAt    time.Time `json:"created_at"`
	MessageCount int64     `json:"message_count"`
	SizeBytes    int64     `json:"size_bytes"`
	Compressed   bool      `json:"compressed"`
	Checksum     string    `json:"checksum,omitempty"`
}

// MessageIndexer handles indexing for fast searches
type MessageIndexer interface {
	// Index a message
	IndexMessage(ctx context.Context, message *streaming.Message) error

	// Remove message from index
	RemoveFromIndex(ctx context.Context, messageID string) error

	// Search indexed messages
	SearchMessages(ctx context.Context, query SearchQuery) ([]*streaming.Message, error)

	// Update index
	UpdateIndex(ctx context.Context) error

	// Get index statistics
	GetIndexStats(ctx context.Context) (*IndexStats, error)
}

// SearchQuery represents a search query
type SearchQuery struct {
	Text         string                  `json:"text"`
	RoomID       string                  `json:"room_id,omitempty"`
	UserID       string                  `json:"user_id,omitempty"`
	MessageTypes []streaming.MessageType `json:"message_types,omitempty"`
	Since        *time.Time              `json:"since,omitempty"`
	Until        *time.Time              `json:"until,omitempty"`
	Limit        int                     `json:"limit,omitempty"`
	Offset       int                     `json:"offset,omitempty"`
	Highlight    bool                    `json:"highlight,omitempty"`
	FuzzySearch  bool                    `json:"fuzzy_search,omitempty"`
}

// IndexStats represents indexer statistics
type IndexStats struct {
	TotalDocuments     int64         `json:"total_documents"`
	IndexSize          int64         `json:"index_size"`
	LastUpdated        time.Time     `json:"last_updated"`
	SearchQueriesCount int64         `json:"search_queries_count"`
	AverageQueryTime   time.Duration `json:"average_query_time"`
}

// MessageReplication handles message replication across stores
type MessageReplication interface {
	// Replicate message to backup stores
	ReplicateMessage(ctx context.Context, message *streaming.Message) error

	// Sync with replica stores
	SyncReplicas(ctx context.Context) error

	// Check replica health
	CheckReplicaHealth(ctx context.Context) (map[string]bool, error)

	// Failover to replica
	FailoverToReplica(ctx context.Context, replicaID string) error
}
