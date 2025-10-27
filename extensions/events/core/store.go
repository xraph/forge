package core

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
)

// EventStore defines the interface for persisting and retrieving events
type EventStore interface {
	// SaveEvent persists a single event
	SaveEvent(ctx context.Context, event *Event) error

	// SaveEvents persists multiple events atomically
	SaveEvents(ctx context.Context, events []*Event) error

	// GetEvent retrieves a single event by ID
	GetEvent(ctx context.Context, eventID string) (*Event, error)

	// GetEvents retrieves events by various criteria
	GetEvents(ctx context.Context, criteria EventCriteria) (*EventCollection, error)

	// GetEventsByAggregate retrieves all events for a specific aggregate
	GetEventsByAggregate(ctx context.Context, aggregateID string, fromVersion int) ([]*Event, error)

	// GetEventsByType retrieves events of a specific type
	GetEventsByType(ctx context.Context, eventType string, limit int, offset int64) ([]*Event, error)

	// GetEventsSince retrieves events since a specific timestamp
	GetEventsSince(ctx context.Context, since time.Time, limit int, offset int64) ([]*Event, error)

	// GetEventsInRange retrieves events within a time range
	GetEventsInRange(ctx context.Context, start, end time.Time, limit int, offset int64) ([]*Event, error)

	// DeleteEvent deletes an event by ID (soft delete)
	DeleteEvent(ctx context.Context, eventID string) error

	// DeleteEventsByAggregate deletes all events for an aggregate (soft delete)
	DeleteEventsByAggregate(ctx context.Context, aggregateID string) error

	// GetLastEvent gets the last event for an aggregate
	GetLastEvent(ctx context.Context, aggregateID string) (*Event, error)

	// GetEventCount gets the total count of events
	GetEventCount(ctx context.Context) (int64, error)

	// GetEventCountByType gets the count of events by type
	GetEventCountByType(ctx context.Context, eventType string) (int64, error)

	// CreateSnapshot creates a snapshot of an aggregate's state
	CreateSnapshot(ctx context.Context, snapshot *Snapshot) error

	// GetSnapshot retrieves the latest snapshot for an aggregate
	GetSnapshot(ctx context.Context, aggregateID string) (*Snapshot, error)

	// DeleteSnapshot deletes a snapshot
	DeleteSnapshot(ctx context.Context, snapshotID string) error

	// Close closes the event store connection
	Close(ctx context.Context) error

	// HealthCheck checks if the event store is healthy
	HealthCheck(ctx context.Context) error
}

// EventCriteria defines criteria for querying events
type EventCriteria struct {
	EventTypes   []string               `json:"event_types,omitempty"`
	AggregateIDs []string               `json:"aggregate_ids,omitempty"`
	Sources      []string               `json:"sources,omitempty"`
	StartTime    *time.Time             `json:"start_time,omitempty"`
	EndTime      *time.Time             `json:"end_time,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	Limit        int                    `json:"limit,omitempty"`
	Offset       int64                  `json:"offset,omitempty"`
	SortBy       string                 `json:"sort_by,omitempty"`    // timestamp, type, aggregate_id
	SortOrder    string                 `json:"sort_order,omitempty"` // asc, desc
}

// NewEventCriteria creates a new event criteria
func NewEventCriteria() *EventCriteria {
	return &EventCriteria{
		Limit:     100,
		Offset:    0,
		SortBy:    "timestamp",
		SortOrder: "asc",
	}
}

// WithEventTypes adds event types to the criteria
func (ec *EventCriteria) WithEventTypes(eventTypes ...string) *EventCriteria {
	ec.EventTypes = append(ec.EventTypes, eventTypes...)
	return ec
}

// WithAggregateIDs adds aggregate IDs to the criteria
func (ec *EventCriteria) WithAggregateIDs(aggregateIDs ...string) *EventCriteria {
	ec.AggregateIDs = append(ec.AggregateIDs, aggregateIDs...)
	return ec
}

// WithSources adds sources to the criteria
func (ec *EventCriteria) WithSources(sources ...string) *EventCriteria {
	ec.Sources = append(ec.Sources, sources...)
	return ec
}

// WithTimeRange sets the time range for the criteria
func (ec *EventCriteria) WithTimeRange(start, end time.Time) *EventCriteria {
	ec.StartTime = &start
	ec.EndTime = &end
	return ec
}

// WithStartTime sets the start time for the criteria
func (ec *EventCriteria) WithStartTime(start time.Time) *EventCriteria {
	ec.StartTime = &start
	return ec
}

// WithEndTime sets the end time for the criteria
func (ec *EventCriteria) WithEndTime(end time.Time) *EventCriteria {
	ec.EndTime = &end
	return ec
}

// WithMetadata adds metadata filter to the criteria
func (ec *EventCriteria) WithMetadata(key string, value interface{}) *EventCriteria {
	if ec.Metadata == nil {
		ec.Metadata = make(map[string]interface{})
	}
	ec.Metadata[key] = value
	return ec
}

// WithLimit sets the limit for the criteria
func (ec *EventCriteria) WithLimit(limit int) *EventCriteria {
	ec.Limit = limit
	return ec
}

// WithOffset sets the offset for the criteria
func (ec *EventCriteria) WithOffset(offset int64) *EventCriteria {
	ec.Offset = offset
	return ec
}

// WithSort sets the sort parameters for the criteria
func (ec *EventCriteria) WithSort(sortBy, sortOrder string) *EventCriteria {
	ec.SortBy = sortBy
	ec.SortOrder = sortOrder
	return ec
}

// Validate validates the event criteria
func (ec *EventCriteria) Validate() error {
	if ec.Limit < 0 {
		return fmt.Errorf("limit cannot be negative")
	}
	if ec.Offset < 0 {
		return fmt.Errorf("offset cannot be negative")
	}
	if ec.StartTime != nil && ec.EndTime != nil && ec.StartTime.After(*ec.EndTime) {
		return fmt.Errorf("start time cannot be after end time")
	}

	validSortBy := map[string]bool{
		"timestamp":    true,
		"type":         true,
		"aggregate_id": true,
		"version":      true,
	}
	if ec.SortBy != "" && !validSortBy[ec.SortBy] {
		return fmt.Errorf("invalid sort_by field: %s", ec.SortBy)
	}

	validSortOrder := map[string]bool{
		"asc":  true,
		"desc": true,
	}
	if ec.SortOrder != "" && !validSortOrder[ec.SortOrder] {
		return fmt.Errorf("invalid sort_order: %s", ec.SortOrder)
	}

	return nil
}

// Snapshot represents a point-in-time snapshot of aggregate state
type Snapshot struct {
	ID          string                 `json:"id"`
	AggregateID string                 `json:"aggregate_id"`
	Type        string                 `json:"type"`
	Data        interface{}            `json:"data"`
	Version     int                    `json:"version"`
	Timestamp   time.Time              `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewSnapshot creates a new snapshot
func NewSnapshot(aggregateID, snapshotType string, data interface{}, version int) *Snapshot {
	return &Snapshot{
		ID:          fmt.Sprintf("%s-%d-%d", aggregateID, version, time.Now().UnixNano()),
		AggregateID: aggregateID,
		Type:        snapshotType,
		Data:        data,
		Version:     version,
		Timestamp:   time.Now().UTC(),
		Metadata:    make(map[string]interface{}),
	}
}

// WithMetadata adds metadata to the snapshot
func (s *Snapshot) WithMetadata(key string, value interface{}) *Snapshot {
	if s.Metadata == nil {
		s.Metadata = make(map[string]interface{})
	}
	s.Metadata[key] = value
	return s
}

// Validate validates the snapshot
func (s *Snapshot) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("snapshot ID is required")
	}
	if s.AggregateID == "" {
		return fmt.Errorf("aggregate ID is required")
	}
	if s.Type == "" {
		return fmt.Errorf("snapshot type is required")
	}
	if s.Data == nil {
		return fmt.Errorf("snapshot data is required")
	}
	if s.Version <= 0 {
		return fmt.Errorf("snapshot version must be positive")
	}
	if s.Timestamp.IsZero() {
		return fmt.Errorf("snapshot timestamp is required")
	}
	return nil
}

// EventStoreConfig defines configuration for event stores
type EventStoreConfig struct {
	Type             string        `yaml:"type" json:"type"`
	ConnectionName   string        `yaml:"connection_name" json:"connection_name"`
	ConnectionString string        `yaml:"connection_string" json:"connection_string"`
	Database         string        `yaml:"database" json:"database"`
	EventsTable      string        `yaml:"events_table" json:"events_table"`
	SnapshotsTable   string        `yaml:"snapshots_table" json:"snapshots_table"`
	MaxConnections   int           `yaml:"max_connections" json:"max_connections"`
	ConnTimeout      time.Duration `yaml:"connection_timeout" json:"connection_timeout"`
	ReadTimeout      time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout     time.Duration `yaml:"write_timeout" json:"write_timeout"`
	EnableMetrics    bool          `yaml:"enable_metrics" json:"enable_metrics"`
	EnableTracing    bool          `yaml:"enable_tracing" json:"enable_tracing"`
}

// DefaultEventStoreConfig returns default configuration
func DefaultEventStoreConfig() *EventStoreConfig {
	return &EventStoreConfig{
		Type:           "memory",
		Database:       "events",
		EventsTable:    "events",
		SnapshotsTable: "snapshots",
		MaxConnections: 10,
		ConnTimeout:    time.Second * 30,
		ReadTimeout:    time.Second * 10,
		WriteTimeout:   time.Second * 10,
		EnableMetrics:  true,
		EnableTracing:  false,
	}
}

// Validate validates the event store configuration
func (cfg *EventStoreConfig) Validate() error {
	if cfg.Type == "" {
		return fmt.Errorf("event store type is required")
	}
	if cfg.Database == "" {
		return fmt.Errorf("database name is required")
	}
	if cfg.EventsTable == "" {
		return fmt.Errorf("events table name is required")
	}
	if cfg.SnapshotsTable == "" {
		return fmt.Errorf("snapshots table name is required")
	}
	if cfg.MaxConnections <= 0 {
		return fmt.Errorf("max connections must be positive")
	}
	if cfg.ConnTimeout <= 0 {
		return fmt.Errorf("connection timeout must be positive")
	}
	if cfg.ReadTimeout <= 0 {
		return fmt.Errorf("read timeout must be positive")
	}
	if cfg.WriteTimeout <= 0 {
		return fmt.Errorf("write timeout must be positive")
	}
	return nil
}

// EventStream represents a stream of events
type EventStream interface {
	// Subscribe subscribes to the event stream
	Subscribe(ctx context.Context, handler EventStreamHandler) error

	// Unsubscribe unsubscribes from the event stream
	Unsubscribe(ctx context.Context) error

	// Position returns the current stream position
	Position() int64

	// SeekTo seeks to a specific position in the stream
	SeekTo(position int64) error

	// Close closes the event stream
	Close() error
}

// EventStreamHandler handles events from an event stream
type EventStreamHandler interface {
	// HandleEvent handles an event from the stream
	HandleEvent(ctx context.Context, event *Event) error

	// HandleError handles errors from the stream
	HandleError(ctx context.Context, err error)

	// HandleEnd handles the end of the stream
	HandleEnd(ctx context.Context)
}

// EventStreamConfig defines configuration for event streams
type EventStreamConfig struct {
	StartPosition int64         `yaml:"start_position" json:"start_position"`
	BatchSize     int           `yaml:"batch_size" json:"batch_size"`
	BufferSize    int           `yaml:"buffer_size" json:"buffer_size"`
	PollInterval  time.Duration `yaml:"poll_interval" json:"poll_interval"`
	MaxRetries    int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay    time.Duration `yaml:"retry_delay" json:"retry_delay"`
}

// DefaultEventStreamConfig returns default stream configuration
func DefaultEventStreamConfig() *EventStreamConfig {
	return &EventStreamConfig{
		StartPosition: 0,
		BatchSize:     100,
		BufferSize:    1000,
		PollInterval:  time.Second,
		MaxRetries:    3,
		RetryDelay:    time.Second * 5,
	}
}

// EventStoreMetrics defines metrics for event stores
type EventStoreMetrics struct {
	EventsSaved       int64         `json:"events_saved"`
	EventsRead        int64         `json:"events_read"`
	SnapshotsCreated  int64         `json:"snapshots_created"`
	SnapshotsRead     int64         `json:"snapshots_read"`
	Errors            int64         `json:"errors"`
	AverageWriteTime  time.Duration `json:"average_write_time"`
	AverageReadTime   time.Duration `json:"average_read_time"`
	ConnectionsActive int           `json:"connections_active"`
}

// EventStoreStats represents statistics for an event store
type EventStoreStats struct {
	TotalEvents     int64                  `json:"total_events"`
	EventsByType    map[string]int64       `json:"events_by_type"`
	TotalSnapshots  int64                  `json:"total_snapshots"`
	SnapshotsByType map[string]int64       `json:"snapshots_by_type"`
	OldestEvent     *time.Time             `json:"oldest_event,omitempty"`
	NewestEvent     *time.Time             `json:"newest_event,omitempty"`
	Metrics         *EventStoreMetrics     `json:"metrics"`
	Health          forge.HealthStatus     `json:"health"`
	ConnectionInfo  map[string]interface{} `json:"connection_info"`
}

// EventProjection represents a projection of events
type EventProjection interface {
	// Name returns the projection name
	Name() string

	// When defines when this projection should be applied
	When() map[string]EventHandler

	// Apply applies events to the projection
	Apply(ctx context.Context, event *Event) error

	// Reset resets the projection state
	Reset(ctx context.Context) error

	// GetState returns the current projection state
	GetState(ctx context.Context) (interface{}, error)

	// SaveState saves the projection state
	SaveState(ctx context.Context, state interface{}) error
}

// ProjectionManager manages event projections
type ProjectionManager interface {
	// RegisterProjection registers a projection
	RegisterProjection(projection EventProjection) error

	// UnregisterProjection unregisters a projection
	UnregisterProjection(name string) error

	// GetProjection gets a projection by name
	GetProjection(name string) (EventProjection, error)

	// GetProjections gets all registered projections
	GetProjections() []EventProjection

	// RebuildProjection rebuilds a projection from scratch
	RebuildProjection(ctx context.Context, name string) error

	// RebuildAllProjections rebuilds all projections
	RebuildAllProjections(ctx context.Context) error

	// Start starts the projection manager
	Start(ctx context.Context) error

	// Stop stops the projection manager
	Stop(ctx context.Context) error
}

// EventStoreTransaction represents a transaction in the event store
type EventStoreTransaction interface {
	// SaveEvent saves an event within the transaction
	SaveEvent(ctx context.Context, event *Event) error

	// SaveEvents saves multiple events within the transaction
	SaveEvents(ctx context.Context, events []*Event) error

	// CreateSnapshot creates a snapshot within the transaction
	CreateSnapshot(ctx context.Context, snapshot *Snapshot) error

	// Commit commits the transaction
	Commit(ctx context.Context) error

	// Rollback rolls back the transaction
	Rollback(ctx context.Context) error
}

// TransactionalEventStore extends EventStore with transaction support
type TransactionalEventStore interface {
	EventStore

	// BeginTransaction begins a new transaction
	BeginTransaction(ctx context.Context) (EventStoreTransaction, error)

	// ExecuteInTransaction executes a function within a transaction
	ExecuteInTransaction(ctx context.Context, fn func(tx EventStoreTransaction) error) error
}

// EventStoreMigration represents a migration for the event store
type EventStoreMigration interface {
	// Version returns the migration version
	Version() int

	// Description returns the migration description
	Description() string

	// Up applies the migration
	Up(ctx context.Context, store EventStore) error

	// Down reverts the migration
	Down(ctx context.Context, store EventStore) error
}

// EventStoreMigrator manages event store migrations
type EventStoreMigrator interface {
	// AddMigration adds a migration
	AddMigration(migration EventStoreMigration) error

	// Migrate runs pending migrations
	Migrate(ctx context.Context) error

	// Rollback rolls back the last migration
	Rollback(ctx context.Context) error

	// GetVersion gets the current migration version
	GetVersion(ctx context.Context) (int, error)

	// GetPendingMigrations gets pending migrations
	GetPendingMigrations(ctx context.Context) ([]EventStoreMigration, error)
}
