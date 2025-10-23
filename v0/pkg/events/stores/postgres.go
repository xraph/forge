package stores

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/xraph/forge/pkg/common"
	eventscore "github.com/xraph/forge/pkg/events/core"
	"github.com/xraph/forge/pkg/logger"
)

// PostgresEventStore implements EventStore interface using PostgreSQL
type PostgresEventStore struct {
	connection common.Connection
	db         *gorm.DB
	logger     common.Logger
	metrics    common.Metrics
	config     *eventscore.EventStoreConfig
	stats      *eventscore.EventStoreStats
}

// PostgresEvent represents an event in PostgreSQL
type PostgresEvent struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	AggregateID string    `gorm:"index;not null"`
	Type        string    `gorm:"index;not null"`
	Version     int       `gorm:"index;not null"`
	Data        string    `gorm:"type:jsonb;not null"` // JSON data
	Metadata    string    `gorm:"type:jsonb"`          // JSON metadata
	Source      string    `gorm:"index"`
	Timestamp   time.Time `gorm:"index;not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

// PostgresSnapshot represents a snapshot in PostgreSQL
type PostgresSnapshot struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	AggregateID string    `gorm:"uniqueIndex;not null"`
	Type        string    `gorm:"index;not null"`
	Version     int       `gorm:"not null"`
	Data        string    `gorm:"type:jsonb;not null"` // JSON data
	Metadata    string    `gorm:"type:jsonb"`          // JSON metadata
	Timestamp   time.Time `gorm:"index;not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

// TableName returns the table name for PostgresEvent
func (PostgresEvent) TableName() string {
	return "forge_events"
}

// TableName returns the table name for PostgresSnapshot
func (PostgresSnapshot) TableName() string {
	return "forge_snapshots"
}

// NewPostgresEventStore creates a new PostgreSQL event store
func NewPostgresEventStore(config *eventscore.EventStoreConfig, l common.Logger, metrics common.Metrics, dbManager common.DatabaseManager) (eventscore.EventStore, error) {
	// Get database connection
	connectionName := "default"
	if config.ConnectionName != "" {
		connectionName = config.ConnectionName
	}

	connection, err := dbManager.GetConnection(connectionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection '%s': %w", connectionName, err)
	}

	// Type assert to get GORM connection
	gormConnection := connection

	db := gormConnection.DB().(*gorm.DB)
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	store := &PostgresEventStore{
		connection: gormConnection,
		db:         db,
		logger:     l,
		metrics:    metrics,
		config:     config,
		stats: &eventscore.EventStoreStats{
			TotalEvents:     0,
			EventsByType:    make(map[string]int64),
			TotalSnapshots:  0,
			SnapshotsByType: make(map[string]int64),
			Health:          common.HealthStatusHealthy,
			ConnectionInfo: map[string]interface{}{
				"type":            "postgres",
				"connection_name": connectionName,
			},
			Metrics: &eventscore.EventStoreMetrics{
				EventsSaved:      0,
				EventsRead:       0,
				SnapshotsCreated: 0,
				SnapshotsRead:    0,
				Errors:           0,
			},
		},
	}

	// Auto-migrate tables
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database schema: %w", err)
	}

	// Initialize statistics
	if err := store.initializeStats(); err != nil {
		l.Warn("failed to initialize statistics", logger.Error(err))
	}

	return store, nil
}

// migrate creates the necessary database tables
func (pes *PostgresEventStore) migrate() error {
	if err := pes.db.AutoMigrate(&PostgresEvent{}, &PostgresSnapshot{}); err != nil {
		return fmt.Errorf("failed to auto-migrate tables: %w", err)
	}

	// Create additional indexes
	if err := pes.createIndexes(); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	return nil
}

// createIndexes creates additional database indexes for performance
func (pes *PostgresEventStore) createIndexes() error {
	// Composite index for aggregate queries
	if err := pes.db.Exec("CREATE INDEX IF NOT EXISTS idx_events_aggregate_version ON forge_events (aggregate_id, version)").Error; err != nil {
		return err
	}

	// Index for timestamp range queries
	if err := pes.db.Exec("CREATE INDEX IF NOT EXISTS idx_events_timestamp ON forge_events (timestamp DESC)").Error; err != nil {
		return err
	}

	// Index for type and timestamp queries
	if err := pes.db.Exec("CREATE INDEX IF NOT EXISTS idx_events_type_timestamp ON forge_events (type, timestamp DESC)").Error; err != nil {
		return err
	}

	return nil
}

// initializeStats loads initial statistics from database
func (pes *PostgresEventStore) initializeStats() error {
	// Count total events
	var totalEvents int64
	if err := pes.db.Model(&PostgresEvent{}).Count(&totalEvents).Error; err != nil {
		return err
	}
	pes.stats.TotalEvents = totalEvents

	// Count events by type
	var eventTypes []struct {
		Type  string
		Count int64
	}
	if err := pes.db.Model(&PostgresEvent{}).Select("type, COUNT(*) as count").Group("type").Find(&eventTypes).Error; err != nil {
		return err
	}

	for _, et := range eventTypes {
		pes.stats.EventsByType[et.Type] = et.Count
	}

	// Count total snapshots
	var totalSnapshots int64
	if err := pes.db.Model(&PostgresSnapshot{}).Count(&totalSnapshots).Error; err != nil {
		return err
	}
	pes.stats.TotalSnapshots = totalSnapshots

	// Count snapshots by type
	var snapshotTypes []struct {
		Type  string
		Count int64
	}
	if err := pes.db.Model(&PostgresSnapshot{}).Select("type, COUNT(*) as count").Group("type").Find(&snapshotTypes).Error; err != nil {
		return err
	}

	for _, st := range snapshotTypes {
		pes.stats.SnapshotsByType[st.Type] = st.Count
	}

	// Get oldest and newest event timestamps
	var oldest, newest PostgresEvent
	if err := pes.db.Order("timestamp ASC").First(&oldest).Error; err == nil {
		pes.stats.OldestEvent = &oldest.Timestamp
	}
	if err := pes.db.Order("timestamp DESC").First(&newest).Error; err == nil {
		pes.stats.NewestEvent = &newest.Timestamp
	}

	return nil
}

// SaveEvent saves a single event
func (pes *PostgresEventStore) SaveEvent(ctx context.Context, event *eventscore.Event) error {
	if err := event.Validate(); err != nil {
		pes.recordError()
		return fmt.Errorf("invalid event: %w", err)
	}

	start := time.Now()

	pgEvent, err := pes.convertEventToPostgres(event)
	if err != nil {
		pes.recordError()
		return fmt.Errorf("failed to convert event: %w", err)
	}

	if err := pes.db.WithContext(ctx).Create(pgEvent).Error; err != nil {
		pes.recordError()
		return fmt.Errorf("failed to save event: %w", err)
	}

	// Update statistics
	pes.stats.TotalEvents++
	pes.stats.EventsByType[event.Type]++
	pes.stats.Metrics.EventsSaved++

	// Update oldest/newest timestamps
	if pes.stats.OldestEvent == nil || event.Timestamp.Before(*pes.stats.OldestEvent) {
		pes.stats.OldestEvent = &event.Timestamp
	}
	if pes.stats.NewestEvent == nil || event.Timestamp.After(*pes.stats.NewestEvent) {
		pes.stats.NewestEvent = &event.Timestamp
	}

	// Record metrics
	duration := time.Since(start)
	pes.stats.Metrics.AverageWriteTime = (pes.stats.Metrics.AverageWriteTime + duration) / 2

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_saved", "store", "postgres").Inc()
		pes.metrics.Histogram("forge.events.store.save_duration", "store", "postgres").Observe(duration.Seconds())
	}

	if pes.logger != nil {
		pes.logger.Debug("event saved to postgres store",
			logger.String("event_id", event.ID),
			logger.String("event_type", event.Type),
			logger.String("aggregate_id", event.AggregateID),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// SaveEvents saves multiple events atomically
func (pes *PostgresEventStore) SaveEvents(ctx context.Context, events []*eventscore.Event) error {
	if len(events) == 0 {
		return nil
	}

	start := time.Now()

	// Convert all events
	pgEvents := make([]*PostgresEvent, len(events))
	for i, event := range events {
		if err := event.Validate(); err != nil {
			pes.recordError()
			return fmt.Errorf("invalid event %s: %w", event.ID, err)
		}

		pgEvent, err := pes.convertEventToPostgres(event)
		if err != nil {
			pes.recordError()
			return fmt.Errorf("failed to convert event %s: %w", event.ID, err)
		}
		pgEvents[i] = pgEvent
	}

	// Save in transaction
	err := pes.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, pgEvent := range pgEvents {
			if err := tx.Create(pgEvent).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		pes.recordError()
		return fmt.Errorf("failed to save events in transaction: %w", err)
	}

	// Update statistics
	pes.stats.TotalEvents += int64(len(events))
	pes.stats.Metrics.EventsSaved += int64(len(events))

	for _, event := range events {
		pes.stats.EventsByType[event.Type]++

		// Update oldest/newest timestamps
		if pes.stats.OldestEvent == nil || event.Timestamp.Before(*pes.stats.OldestEvent) {
			pes.stats.OldestEvent = &event.Timestamp
		}
		if pes.stats.NewestEvent == nil || event.Timestamp.After(*pes.stats.NewestEvent) {
			pes.stats.NewestEvent = &event.Timestamp
		}
	}

	// Record metrics
	duration := time.Since(start)
	pes.stats.Metrics.AverageWriteTime = (pes.stats.Metrics.AverageWriteTime + duration) / 2

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_saved", "store", "postgres").Add(float64(len(events)))
		pes.metrics.Histogram("forge.events.store.batch_save_duration", "store", "postgres").Observe(duration.Seconds())
	}

	if pes.logger != nil {
		pes.logger.Debug("events saved to postgres store",
			logger.Int("count", len(events)),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// GetEvent retrieves a single event by ID
func (pes *PostgresEventStore) GetEvent(ctx context.Context, eventID string) (*eventscore.Event, error) {
	start := time.Now()

	var pgEvent PostgresEvent
	if err := pes.db.WithContext(ctx).Where("id = ?", eventID).First(&pgEvent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("event with ID %s not found", eventID)
		}
		pes.recordError()
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	event, err := pes.convertPostgresToEvent(&pgEvent)
	if err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to convert event: %w", err)
	}

	// Record metrics
	duration := time.Since(start)
	pes.stats.Metrics.EventsRead++
	pes.stats.Metrics.AverageReadTime = (pes.stats.Metrics.AverageReadTime + duration) / 2

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_read", "store", "postgres").Inc()
		pes.metrics.Histogram("forge.events.store.read_duration", "store", "postgres").Observe(duration.Seconds())
	}

	return event, nil
}

// GetEvents retrieves events by various criteria
func (pes *PostgresEventStore) GetEvents(ctx context.Context, criteria eventscore.EventCriteria) (*eventscore.EventCollection, error) {
	if err := criteria.Validate(); err != nil {
		pes.recordError()
		return nil, fmt.Errorf("invalid criteria: %w", err)
	}

	start := time.Now()

	query := pes.db.WithContext(ctx).Model(&PostgresEvent{})

	// Apply filters
	query = pes.applyFilters(query, criteria)

	// Count total records
	var total int64
	if err := query.Count(&total).Error; err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to count events: %w", err)
	}

	// Apply sorting
	query = pes.applySorting(query, criteria.SortBy, criteria.SortOrder)

	// Apply pagination
	query = query.Offset(int(criteria.Offset)).Limit(criteria.Limit)

	var pgEvents []PostgresEvent
	if err := query.Find(&pgEvents).Error; err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	// Convert to events
	events := make([]eventscore.Event, len(pgEvents))
	for i, pgEvent := range pgEvents {
		event, err := pes.convertPostgresToEvent(&pgEvent)
		if err != nil {
			pes.recordError()
			return nil, fmt.Errorf("failed to convert event: %w", err)
		}
		events[i] = *event
	}

	collection := eventscore.NewEventCollection(events, int(total), criteria.Offset, criteria.Limit)

	// Record metrics
	duration := time.Since(start)
	pes.stats.Metrics.EventsRead += int64(len(events))
	pes.stats.Metrics.AverageReadTime = (pes.stats.Metrics.AverageReadTime + duration) / 2

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_read", "store", "postgres").Add(float64(len(events)))
		pes.metrics.Histogram("forge.events.store.query_duration", "store", "postgres").Observe(duration.Seconds())
	}

	return collection, nil
}

// GetEventsByAggregate retrieves all events for a specific aggregate
func (pes *PostgresEventStore) GetEventsByAggregate(ctx context.Context, aggregateID string, fromVersion int) ([]*eventscore.Event, error) {
	start := time.Now()

	var pgEvents []PostgresEvent
	query := pes.db.WithContext(ctx).Where("aggregate_id = ?", aggregateID)

	if fromVersion > 0 {
		query = query.Where("version >= ?", fromVersion)
	}

	if err := query.Order("version ASC").Find(&pgEvents).Error; err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to get events for aggregate: %w", err)
	}

	events := make([]*eventscore.Event, len(pgEvents))
	for i, pgEvent := range pgEvents {
		event, err := pes.convertPostgresToEvent(&pgEvent)
		if err != nil {
			pes.recordError()
			return nil, fmt.Errorf("failed to convert event: %w", err)
		}
		events[i] = event
	}

	// Record metrics
	duration := time.Since(start)
	pes.stats.Metrics.EventsRead += int64(len(events))

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_read", "store", "postgres").Add(float64(len(events)))
		pes.metrics.Histogram("forge.events.store.aggregate_query_duration", "store", "postgres").Observe(duration.Seconds())
	}

	return events, nil
}

// GetEventsByType retrieves events of a specific type
func (pes *PostgresEventStore) GetEventsByType(ctx context.Context, eventType string, limit int, offset int64) ([]*eventscore.Event, error) {
	start := time.Now()
	fmt.Println("start time", start)

	var pgEvents []PostgresEvent
	if err := pes.db.WithContext(ctx).Where("type = ?", eventType).Order("timestamp DESC").Offset(int(offset)).Limit(limit).Find(&pgEvents).Error; err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to get events by type: %w", err)
	}

	events := make([]*eventscore.Event, len(pgEvents))
	for i, pgEvent := range pgEvents {
		event, err := pes.convertPostgresToEvent(&pgEvent)
		if err != nil {
			pes.recordError()
			return nil, fmt.Errorf("failed to convert event: %w", err)
		}
		events[i] = event
	}

	pes.stats.Metrics.EventsRead += int64(len(events))

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_read", "store", "postgres").Add(float64(len(events)))
	}

	return events, nil
}

// GetEventsSince retrieves events since a specific timestamp
func (pes *PostgresEventStore) GetEventsSince(ctx context.Context, since time.Time, limit int, offset int64) ([]*eventscore.Event, error) {
	var pgEvents []PostgresEvent
	if err := pes.db.WithContext(ctx).Where("timestamp >= ?", since).Order("timestamp ASC").Offset(int(offset)).Limit(limit).Find(&pgEvents).Error; err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to get events since timestamp: %w", err)
	}

	events := make([]*eventscore.Event, len(pgEvents))
	for i, pgEvent := range pgEvents {
		event, err := pes.convertPostgresToEvent(&pgEvent)
		if err != nil {
			pes.recordError()
			return nil, fmt.Errorf("failed to convert event: %w", err)
		}
		events[i] = event
	}

	pes.stats.Metrics.EventsRead += int64(len(events))

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_read", "store", "postgres").Add(float64(len(events)))
	}

	return events, nil
}

// GetEventsInRange retrieves events within a time range
func (pes *PostgresEventStore) GetEventsInRange(ctx context.Context, start, end time.Time, limit int, offset int64) ([]*eventscore.Event, error) {
	var pgEvents []PostgresEvent
	if err := pes.db.WithContext(ctx).Where("timestamp >= ? AND timestamp <= ?", start, end).Order("timestamp ASC").Offset(int(offset)).Limit(limit).Find(&pgEvents).Error; err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to get events in range: %w", err)
	}

	events := make([]*eventscore.Event, len(pgEvents))
	for i, pgEvent := range pgEvents {
		event, err := pes.convertPostgresToEvent(&pgEvent)
		if err != nil {
			pes.recordError()
			return nil, fmt.Errorf("failed to convert event: %w", err)
		}
		events[i] = event
	}

	pes.stats.Metrics.EventsRead += int64(len(events))

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_read", "store", "postgres").Add(float64(len(events)))
	}

	return events, nil
}

// DeleteEvent soft deletes an event by ID
func (pes *PostgresEventStore) DeleteEvent(ctx context.Context, eventID string) error {
	result := pes.db.WithContext(ctx).Model(&PostgresEvent{}).Where("id = ?", eventID).Update("metadata", gorm.Expr("COALESCE(metadata, '{}') || ?", `{"_deleted": true, "_deleted_at": "`+time.Now().Format(time.RFC3339)+`"}`))

	if result.Error != nil {
		pes.recordError()
		return fmt.Errorf("failed to delete event: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("event with ID %s not found", eventID)
	}

	if pes.logger != nil {
		pes.logger.Debug("event marked as deleted",
			logger.String("event_id", eventID),
		)
	}

	return nil
}

// DeleteEventsByAggregate soft deletes all events for an aggregate
func (pes *PostgresEventStore) DeleteEventsByAggregate(ctx context.Context, aggregateID string) error {
	deleteTime := time.Now().Format(time.RFC3339)
	result := pes.db.WithContext(ctx).Model(&PostgresEvent{}).Where("aggregate_id = ?", aggregateID).Update("metadata", gorm.Expr("COALESCE(metadata, '{}') || ?", `{"_deleted": true, "_deleted_at": "`+deleteTime+`"}`))

	if result.Error != nil {
		pes.recordError()
		return fmt.Errorf("failed to delete events for aggregate: %w", result.Error)
	}

	if pes.logger != nil {
		pes.logger.Debug("events marked as deleted for aggregate",
			logger.String("aggregate_id", aggregateID),
			logger.Int64("count", result.RowsAffected),
		)
	}

	return nil
}

// GetLastEvent gets the last event for an aggregate
func (pes *PostgresEventStore) GetLastEvent(ctx context.Context, aggregateID string) (*eventscore.Event, error) {
	var pgEvent PostgresEvent
	if err := pes.db.WithContext(ctx).Where("aggregate_id = ?", aggregateID).Order("version DESC").First(&pgEvent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("no events found for aggregate %s", aggregateID)
		}
		pes.recordError()
		return nil, fmt.Errorf("failed to get last event: %w", err)
	}

	event, err := pes.convertPostgresToEvent(&pgEvent)
	if err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to convert event: %w", err)
	}

	pes.stats.Metrics.EventsRead++

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_read", "store", "postgres").Inc()
	}

	return event, nil
}

// GetEventCount gets the total count of events
func (pes *PostgresEventStore) GetEventCount(ctx context.Context) (int64, error) {
	var count int64
	if err := pes.db.WithContext(ctx).Model(&PostgresEvent{}).Count(&count).Error; err != nil {
		pes.recordError()
		return 0, fmt.Errorf("failed to count events: %w", err)
	}
	return count, nil
}

// GetEventCountByType gets the count of events by type
func (pes *PostgresEventStore) GetEventCountByType(ctx context.Context, eventType string) (int64, error) {
	var count int64
	if err := pes.db.WithContext(ctx).Model(&PostgresEvent{}).Where("type = ?", eventType).Count(&count).Error; err != nil {
		pes.recordError()
		return 0, fmt.Errorf("failed to count events by type: %w", err)
	}
	return count, nil
}

// CreateSnapshot creates a snapshot of an aggregate's state
func (pes *PostgresEventStore) CreateSnapshot(ctx context.Context, snapshot *eventscore.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		pes.recordError()
		return fmt.Errorf("invalid snapshot: %w", err)
	}

	start := time.Now()

	pgSnapshot, err := pes.convertSnapshotToPostgres(snapshot)
	if err != nil {
		pes.recordError()
		return fmt.Errorf("failed to convert snapshot: %w", err)
	}

	// Use upsert to handle conflicts
	if err := pes.db.WithContext(ctx).Clauses(
		gorm.Expr("ON CONFLICT (aggregate_id) DO UPDATE SET type = EXCLUDED.type, version = EXCLUDED.version, data = EXCLUDED.data, metadata = EXCLUDED.metadata, timestamp = EXCLUDED.timestamp, updated_at = EXCLUDED.updated_at"),
	).Create(pgSnapshot).Error; err != nil {
		pes.recordError()
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	// Update statistics
	pes.stats.TotalSnapshots++
	pes.stats.SnapshotsByType[snapshot.Type]++
	pes.stats.Metrics.SnapshotsCreated++

	// Record metrics
	duration := time.Since(start)

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.snapshots_created", "store", "postgres").Inc()
		pes.metrics.Histogram("forge.events.store.snapshot_save_duration", "store", "postgres").Observe(duration.Seconds())
	}

	if pes.logger != nil {
		pes.logger.Debug("snapshot created",
			logger.String("snapshot_id", snapshot.ID),
			logger.String("aggregate_id", snapshot.AggregateID),
			logger.String("type", snapshot.Type),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// GetSnapshot retrieves the latest snapshot for an aggregate
func (pes *PostgresEventStore) GetSnapshot(ctx context.Context, aggregateID string) (*eventscore.Snapshot, error) {
	var pgSnapshot PostgresSnapshot
	if err := pes.db.WithContext(ctx).Where("aggregate_id = ?", aggregateID).First(&pgSnapshot).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("no snapshot found for aggregate %s", aggregateID)
		}
		pes.recordError()
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	snapshot, err := pes.convertPostgresToSnapshot(&pgSnapshot)
	if err != nil {
		pes.recordError()
		return nil, fmt.Errorf("failed to convert snapshot: %w", err)
	}

	pes.stats.Metrics.SnapshotsRead++

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.snapshots_read", "store", "postgres").Inc()
	}

	return snapshot, nil
}

// DeleteSnapshot deletes a snapshot
func (pes *PostgresEventStore) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	result := pes.db.WithContext(ctx).Where("id = ?", snapshotID).Delete(&PostgresSnapshot{})

	if result.Error != nil {
		pes.recordError()
		return fmt.Errorf("failed to delete snapshot: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("snapshot with ID %s not found", snapshotID)
	}

	pes.stats.TotalSnapshots--

	if pes.logger != nil {
		pes.logger.Debug("snapshot deleted",
			logger.String("snapshot_id", snapshotID),
		)
	}

	return nil
}

// Close closes the event store connection
func (pes *PostgresEventStore) Close(ctx context.Context) error {
	// The database connection is managed by the database package
	// so we don't need to close it here
	if pes.logger != nil {
		pes.logger.Info("postgres event store closed")
	}
	return nil
}

// HealthCheck checks if the event store is healthy
func (pes *PostgresEventStore) HealthCheck(ctx context.Context) error {
	if err := pes.connection.OnHealthCheck(ctx); err != nil {
		pes.stats.Health = common.HealthStatusUnhealthy
		return fmt.Errorf("database connection unhealthy: %w", err)
	}

	// Test a simple query
	var count int64
	if err := pes.db.WithContext(ctx).Model(&PostgresEvent{}).Limit(1).Count(&count).Error; err != nil {
		pes.stats.Health = common.HealthStatusUnhealthy
		pes.recordError()
		return fmt.Errorf("failed to query events table: %w", err)
	}

	pes.stats.Health = common.HealthStatusHealthy
	return nil
}

// GetStats returns event store statistics
func (pes *PostgresEventStore) GetStats() *eventscore.EventStoreStats {
	// Update real-time statistics
	if err := pes.initializeStats(); err != nil && pes.logger != nil {
		pes.logger.Warn("failed to update statistics", logger.Error(err))
	}

	// Create a copy to avoid external modifications
	statsCopy := *pes.stats
	statsCopy.EventsByType = make(map[string]int64)
	statsCopy.SnapshotsByType = make(map[string]int64)
	statsCopy.ConnectionInfo = make(map[string]interface{})

	for k, v := range pes.stats.EventsByType {
		statsCopy.EventsByType[k] = v
	}
	for k, v := range pes.stats.SnapshotsByType {
		statsCopy.SnapshotsByType[k] = v
	}
	for k, v := range pes.stats.ConnectionInfo {
		statsCopy.ConnectionInfo[k] = v
	}

	// Copy metrics
	if pes.stats.Metrics != nil {
		metricsCopy := *pes.stats.Metrics
		statsCopy.Metrics = &metricsCopy
	}

	return &statsCopy
}

// Helper methods

// convertEventToPostgres converts an Event to PostgresEvent
func (pes *PostgresEventStore) convertEventToPostgres(event *eventscore.Event) (*PostgresEvent, error) {
	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event metadata: %w", err)
	}

	return &PostgresEvent{
		ID:          event.ID,
		AggregateID: event.AggregateID,
		Type:        event.Type,
		Version:     event.Version,
		Data:        string(dataJSON),
		Metadata:    string(metadataJSON),
		Source:      event.Source,
		Timestamp:   event.Timestamp,
	}, nil
}

// convertPostgresToEvent converts a PostgresEvent to Event
func (pes *PostgresEventStore) convertPostgresToEvent(pgEvent *PostgresEvent) (*eventscore.Event, error) {
	var data interface{}
	if err := json.Unmarshal([]byte(pgEvent.Data), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	var metadata map[string]interface{}
	if pgEvent.Metadata != "" {
		if err := json.Unmarshal([]byte(pgEvent.Metadata), &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event metadata: %w", err)
		}
	}

	return &eventscore.Event{
		ID:          pgEvent.ID,
		AggregateID: pgEvent.AggregateID,
		Type:        pgEvent.Type,
		Version:     pgEvent.Version,
		Data:        data,
		Metadata:    metadata,
		Source:      pgEvent.Source,
		Timestamp:   pgEvent.Timestamp,
	}, nil
}

// convertSnapshotToPostgres converts a Snapshot to PostgresSnapshot
func (pes *PostgresEventStore) convertSnapshotToPostgres(snapshot *eventscore.Snapshot) (*PostgresSnapshot, error) {
	dataJSON, err := json.Marshal(snapshot.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot data: %w", err)
	}

	metadataJSON, err := json.Marshal(snapshot.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot metadata: %w", err)
	}

	return &PostgresSnapshot{
		ID:          snapshot.ID,
		AggregateID: snapshot.AggregateID,
		Type:        snapshot.Type,
		Version:     snapshot.Version,
		Data:        string(dataJSON),
		Metadata:    string(metadataJSON),
		Timestamp:   snapshot.Timestamp,
	}, nil
}

// convertPostgresToSnapshot converts a PostgresSnapshot to Snapshot
func (pes *PostgresEventStore) convertPostgresToSnapshot(pgSnapshot *PostgresSnapshot) (*eventscore.Snapshot, error) {
	var data interface{}
	if err := json.Unmarshal([]byte(pgSnapshot.Data), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot data: %w", err)
	}

	var metadata map[string]interface{}
	if pgSnapshot.Metadata != "" {
		if err := json.Unmarshal([]byte(pgSnapshot.Metadata), &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal snapshot metadata: %w", err)
		}
	}

	return &eventscore.Snapshot{
		ID:          pgSnapshot.ID,
		AggregateID: pgSnapshot.AggregateID,
		Type:        pgSnapshot.Type,
		Version:     pgSnapshot.Version,
		Data:        data,
		Metadata:    metadata,
		Timestamp:   pgSnapshot.Timestamp,
	}, nil
}

// applyFilters applies query filters
func (pes *PostgresEventStore) applyFilters(query *gorm.DB, criteria eventscore.EventCriteria) *gorm.DB {
	// Filter by event types
	if len(criteria.EventTypes) > 0 {
		query = query.Where("type IN ?", criteria.EventTypes)
	}

	// Filter by aggregate IDs
	if len(criteria.AggregateIDs) > 0 {
		query = query.Where("aggregate_id IN ?", criteria.AggregateIDs)
	}

	// Filter by time range
	if criteria.StartTime != nil {
		query = query.Where("timestamp >= ?", *criteria.StartTime)
	}
	if criteria.EndTime != nil {
		query = query.Where("timestamp <= ?", *criteria.EndTime)
	}

	// Filter by sources
	if len(criteria.Sources) > 0 {
		query = query.Where("source IN ?", criteria.Sources)
	}

	// Filter by metadata (using JSONB operators)
	for key, value := range criteria.Metadata {
		query = query.Where("metadata->? = ?", key, fmt.Sprintf(`"%v"`, value))
	}

	// Exclude deleted events
	query = query.Where("NOT (metadata->>'_deleted' = 'true')")

	return query
}

// applySorting applies query sorting
func (pes *PostgresEventStore) applySorting(query *gorm.DB, sortBy, sortOrder string) *gorm.DB {
	if sortBy == "" {
		sortBy = "timestamp"
	}
	if sortOrder == "" {
		sortOrder = "asc"
	}

	orderClause := fmt.Sprintf("%s %s", sortBy, strings.ToUpper(sortOrder))
	return query.Order(orderClause)
}

// recordError increments the error count
func (pes *PostgresEventStore) recordError() {
	pes.stats.Metrics.Errors++
	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.errors", "store", "postgres").Inc()
	}
}
