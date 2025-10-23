package core

import (
	"context"
	"time"
)

// CacheWarmer defines the interface for cache warming
type CacheWarmer interface {

	// Start initializes and starts the cache warmer, preparing it to perform cache warming operations.
	Start(ctx context.Context) error

	// Stop stops the cache warming process gracefully, ensuring any ongoing operations are completed or terminated safely.
	Stop(ctx context.Context) error

	// WarmCache warms a cache with predefined data
	WarmCache(ctx context.Context, cacheName string, config WarmConfig) error

	// GetWarmStats returns warming statistics for a cache
	GetWarmStats(cacheName string) (WarmStats, error)

	// ScheduleWarming schedules automatic cache warming
	ScheduleWarming(cacheName string, config WarmConfig) error

	// CancelWarming cancels scheduled warming for a cache
	CancelWarming(cacheName string) error

	// GetActiveWarmings returns currently active warming operations
	GetActiveWarmings() []WarmingOperation

	// RegisterDataSource registers a data source for warming
	RegisterDataSource(source DataSource) error

	// UnregisterDataSource unregisters a data source
	UnregisterDataSource(name string) error

	// GetDataSources returns all registered data sources
	GetDataSources() []DataSource
}

// WarmConfig contains configuration for cache warming
type WarmConfig struct {
	Strategy      WarmStrategy           `json:"strategy"`
	DataSources   []string               `json:"data_sources"`
	Concurrency   int                    `json:"concurrency"`
	BatchSize     int                    `json:"batch_size"`
	Timeout       time.Duration          `json:"timeout"`
	RetryAttempts int                    `json:"retry_attempts"`
	RetryDelay    time.Duration          `json:"retry_delay"`
	Priority      int                    `json:"priority"`
	Filter        map[string]interface{} `json:"filter"`
	TTL           time.Duration          `json:"ttl"`
	Tags          []string               `json:"tags"`
	Metadata      map[string]interface{} `json:"metadata"`
	Schedule      string                 `json:"schedule,omitempty"` // Cron expression for scheduled warming
	MaxItems      int64                  `json:"max_items"`
	MaxSize       int64                  `json:"max_size"`
	FailureMode   FailureMode            `json:"failure_mode"`
}

// WarmStrategy defines warming strategies
type WarmStrategy string

const (
	WarmStrategyEager      WarmStrategy = "eager"
	WarmStrategyLazy       WarmStrategy = "lazy"
	WarmStrategyScheduled  WarmStrategy = "scheduled"
	WarmStrategyOnDemand   WarmStrategy = "on_demand"
	WarmStrategyPredictive WarmStrategy = "predictive"
)

// FailureMode defines how to handle warming failures
type FailureMode string

const (
	FailureModeIgnore FailureMode = "ignore"
	FailureModeLog    FailureMode = "log"
	FailureModeRetry  FailureMode = "retry"
	FailureModeAbort  FailureMode = "abort"
)

// WarmStats provides statistics about cache warming
type WarmStats struct {
	CacheName   string        `json:"cache_name"`
	Strategy    WarmStrategy  `json:"strategy"`
	Status      WarmStatus    `json:"status"`
	Progress    float64       `json:"progress"`
	ItemsWarmed int64         `json:"items_warmed"`
	ItemsTotal  int64         `json:"items_total"`
	ItemsFailed int64         `json:"items_failed"`
	StartTime   time.Time     `json:"start_time"`
	EndTime     *time.Time    `json:"end_time,omitempty"`
	Duration    time.Duration `json:"duration"`
	LastWarmed  time.Time     `json:"last_warmed"`
	TotalSize   int64         `json:"total_size"`
	AverageSize float64       `json:"average_size"`
	Throughput  float64       `json:"throughput"` // Items per second
	ErrorRate   float64       `json:"error_rate"`
	RetryCount  int64         `json:"retry_count"`
	LastError   string        `json:"last_error,omitempty"`
	DataSources []string      `json:"data_sources"`
}

// WarmStatus represents the warming status
type WarmStatus string

const (
	WarmStatusIdle      WarmStatus = "idle"
	WarmStatusRunning   WarmStatus = "running"
	WarmStatusCompleted WarmStatus = "completed"
	WarmStatusFailed    WarmStatus = "failed"
	WarmStatusCancelled WarmStatus = "cancelled"
	WarmStatusScheduled WarmStatus = "scheduled"
)

// WarmingOperation represents an active warming operation
type WarmingOperation struct {
	ID        string             `json:"id"`
	CacheName string             `json:"cache_name"`
	Config    WarmConfig         `json:"config"`
	Stats     WarmStats          `json:"stats"`
	Context   context.Context    `json:"-"`
	Cancel    context.CancelFunc `json:"-"`
	StartedAt time.Time          `json:"started_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// DataSource defines the interface for warming data sources
type DataSource interface {
	// Name returns the data source name
	Name() string

	// GetData retrieves data for warming
	GetData(ctx context.Context, filter map[string]interface{}) ([]WarmItem, error)

	// GetDataCount returns the total count of items available
	GetDataCount(ctx context.Context, filter map[string]interface{}) (int64, error)

	// GetDataBatch retrieves a batch of data for warming
	GetDataBatch(ctx context.Context, filter map[string]interface{}, offset, limit int64) ([]WarmItem, error)

	// SupportsFilter checks if the data source supports a filter
	SupportsFilter(filter map[string]interface{}) bool

	// Configure configures the data source
	Configure(config map[string]interface{}) error

	// HealthCheck performs a health check on the data source
	HealthCheck(ctx context.Context) error
}

// WarmItem represents an item to be warmed in the cache
type WarmItem struct {
	Key      string                 `json:"key"`
	Value    interface{}            `json:"value"`
	TTL      time.Duration          `json:"ttl"`
	Tags     []string               `json:"tags"`
	Metadata map[string]interface{} `json:"metadata"`
	Priority int                    `json:"priority"`
	Size     int64                  `json:"size"`
}

// InvalidationEvent represents a cache invalidation event
type InvalidationEvent struct {
	Type      InvalidationType       `json:"type"`
	CacheName string                 `json:"cache_name"`
	Key       string                 `json:"key,omitempty"`
	Pattern   string                 `json:"pattern,omitempty"`
	Tag       string                 `json:"tag,omitempty"`
	Tags      []string               `json:"tags,omitempty"`
	Source    string                 `json:"source"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata"`
	TTL       time.Duration          `json:"ttl,omitempty"`
}

// InvalidationType represents the type of invalidation
type InvalidationType string

const (
	InvalidationTypeKey     InvalidationType = "key"
	InvalidationTypePattern InvalidationType = "pattern"
	InvalidationTypeTag     InvalidationType = "tag"
	InvalidationTypeTags    InvalidationType = "tags"
	InvalidationTypeAll     InvalidationType = "all"
)

// InvalidationStats contains invalidation statistics
type InvalidationStats struct {
	EventsPublished int64                  `json:"events_published"`
	EventsReceived  int64                  `json:"events_received"`
	EventsProcessed int64                  `json:"events_processed"`
	EventsFailed    int64                  `json:"events_failed"`
	EventsBuffered  int64                  `json:"events_buffered"`
	AverageLatency  time.Duration          `json:"average_latency"`
	LastEvent       time.Time              `json:"last_event"`
	Publishers      map[string]interface{} `json:"publishers"`
	Subscribers     map[string]interface{} `json:"subscribers"`
	PatternsActive  int                    `json:"patterns_active"`
}

// InvalidationManager for the InvalidationManager
type InvalidationManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	HealthCheck(ctx context.Context) error
	RegisterPublisher(name string, publisher InvalidationPublisher) error
	RegisterSubscriber(name string, subscriber InvalidationSubscriber) error
	RegisterCallback(callback InvalidationCallback)
	AddPattern(pattern InvalidationPattern)
	InvalidateKey(ctx context.Context, cacheName, key string) error
	InvalidatePattern(ctx context.Context, cacheName, pattern string) error
	InvalidateTag(ctx context.Context, cacheName, tag string) error
	InvalidateByTags(ctx context.Context, cacheName string, tags []string) error
	InvalidateAll(ctx context.Context, cacheName string) error
	GetStats() InvalidationStats
}

// InvalidationPublisher publishes invalidation events
type InvalidationPublisher interface {
	Publish(ctx context.Context, event InvalidationEvent) error
	PublishBatch(ctx context.Context, events []InvalidationEvent) error
	Close() error
}

// InvalidationSubscriber subscribes to invalidation events
type InvalidationSubscriber interface {
	Subscribe(ctx context.Context, callback InvalidationCallback) error
	Unsubscribe(ctx context.Context) error
	Close() error
}

// InvalidationCallback represents a function that handles invalidation events
type InvalidationCallback func(event InvalidationEvent) error
