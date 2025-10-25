package stores

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/core"
)

// PostgresEventStore implements EventStore using PostgreSQL
type PostgresEventStore struct {
	db      *sql.DB
	logger  forge.Logger
	metrics forge.Metrics
	config  *core.EventStoreConfig
	stats   *core.EventStoreStats
}

// PostgresEventRow represents an event row in PostgreSQL
type PostgresEventRow struct {
	ID          string
	AggregateID string
	Type        string
	Version     int
	Data        []byte
	Metadata    []byte
	Source      string
	Timestamp   time.Time
	CreatedAt   time.Time
}

// PostgresSnapshotRow represents a snapshot row in PostgreSQL
type PostgresSnapshotRow struct {
	ID          string
	AggregateID string
	Type        string
	Version     int
	Data        []byte
	Metadata    []byte
	Timestamp   time.Time
	CreatedAt   time.Time
}

// NewPostgresEventStore creates a new PostgreSQL event store
func NewPostgresEventStore(db *sql.DB, config *core.EventStoreConfig, logger forge.Logger, metrics forge.Metrics) (*PostgresEventStore, error) {
	store := &PostgresEventStore{
		db:      db,
		logger:  logger,
		metrics: metrics,
		config:  config,
		stats: &core.EventStoreStats{
			TotalEvents:     0,
			EventsByType:    make(map[string]int64),
			TotalSnapshots:  0,
			SnapshotsByType: make(map[string]int64),
			Metrics: &core.EventStoreMetrics{
				EventsSaved:      0,
				EventsRead:       0,
				SnapshotsCreated: 0,
				SnapshotsRead:    0,
				Errors:           0,
			},
		},
	}

	// Create tables
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	// Initialize stats
	if err := store.initializeStats(context.Background()); err != nil {
		if logger != nil {
			logger.Warn("failed to initialize statistics", forge.F("error", err))
		}
	}

	return store, nil
}

// migrate creates necessary tables
func (pes *PostgresEventStore) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS forge_events (
			id VARCHAR(36) PRIMARY KEY,
			aggregate_id VARCHAR(255) NOT NULL,
			type VARCHAR(255) NOT NULL,
			version INTEGER NOT NULL,
			data JSONB NOT NULL,
			metadata JSONB,
			source VARCHAR(255),
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_aggregate ON forge_events(aggregate_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_type ON forge_events(type)`,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON forge_events(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_aggregate_version ON forge_events(aggregate_id, version)`,
		`CREATE TABLE IF NOT EXISTS forge_snapshots (
			id VARCHAR(36) PRIMARY KEY,
			aggregate_id VARCHAR(255) UNIQUE NOT NULL,
			type VARCHAR(255) NOT NULL,
			version INTEGER NOT NULL,
			data JSONB NOT NULL,
			metadata JSONB,
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_aggregate ON forge_snapshots(aggregate_id)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_type ON forge_snapshots(type)`,
	}

	for _, query := range queries {
		if _, err := pes.db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute migration query: %w", err)
		}
	}

	return nil
}

// initializeStats loads current statistics
func (pes *PostgresEventStore) initializeStats(ctx context.Context) error {
	// Count total events
	err := pes.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM forge_events").Scan(&pes.stats.TotalEvents)
	if err != nil {
		return err
	}

	// Count events by type
	rows, err := pes.db.QueryContext(ctx, "SELECT type, COUNT(*) FROM forge_events GROUP BY type")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var eventType string
		var count int64
		if err := rows.Scan(&eventType, &count); err != nil {
			return err
		}
		pes.stats.EventsByType[eventType] = count
	}

	// Count total snapshots
	err = pes.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM forge_snapshots").Scan(&pes.stats.TotalSnapshots)
	if err != nil {
		return err
	}

	return nil
}

// SaveEvent implements EventStore
func (pes *PostgresEventStore) SaveEvent(ctx context.Context, event *core.Event) error {
	start := time.Now()

	data, err := json.Marshal(event.Data)
	if err != nil {
		pes.stats.Metrics.Errors++
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		pes.stats.Metrics.Errors++
		return fmt.Errorf("failed to marshal event metadata: %w", err)
	}

	query := `INSERT INTO forge_events (id, aggregate_id, type, version, data, metadata, source, timestamp)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err = pes.db.ExecContext(ctx, query, event.ID, event.AggregateID, event.Type, event.Version,
		data, metadata, event.Source, event.Timestamp)
	if err != nil {
		pes.stats.Metrics.Errors++
		if pes.metrics != nil {
			pes.metrics.Counter("forge.events.store.save_errors").Inc()
		}
		return fmt.Errorf("failed to save event: %w", err)
	}

	// Update stats
	pes.stats.Metrics.EventsSaved++
	pes.stats.TotalEvents++
	pes.stats.EventsByType[event.Type]++

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_saved").Inc()
		pes.metrics.Histogram("forge.events.store.save_duration").Observe(time.Since(start).Seconds())
	}

	if pes.logger != nil {
		pes.logger.Debug("event saved to PostgreSQL", forge.F("event_id", event.ID), forge.F("aggregate_id", event.AggregateID), forge.F("type", event.Type))
	}

	return nil
}

// SaveEvents implements EventStore
func (pes *PostgresEventStore) SaveEvents(ctx context.Context, events []*core.Event) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := pes.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO forge_events (id, aggregate_id, type, version, data, metadata, source, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, event := range events {
		data, err := json.Marshal(event.Data)
		if err != nil {
			pes.stats.Metrics.Errors++
			return fmt.Errorf("failed to marshal event data: %w", err)
		}

		metadata, err := json.Marshal(event.Metadata)
		if err != nil {
			pes.stats.Metrics.Errors++
			return fmt.Errorf("failed to marshal event metadata: %w", err)
		}

		_, err = stmt.ExecContext(ctx, event.ID, event.AggregateID, event.Type, event.Version,
			data, metadata, event.Source, event.Timestamp)
		if err != nil {
			pes.stats.Metrics.Errors++
			return fmt.Errorf("failed to save event: %w", err)
		}

		pes.stats.Metrics.EventsSaved++
		pes.stats.EventsByType[event.Type]++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	pes.stats.TotalEvents += int64(len(events))

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.events_saved").Add(float64(len(events)))
	}

	return nil
}

// GetEvent implements EventStore
func (pes *PostgresEventStore) GetEvent(ctx context.Context, eventID string) (*core.Event, error) {
	query := `SELECT id, aggregate_id, type, version, data, metadata, source, timestamp
			  FROM forge_events WHERE id = $1`

	var row PostgresEventRow
	err := pes.db.QueryRowContext(ctx, query, eventID).Scan(
		&row.ID, &row.AggregateID, &row.Type, &row.Version,
		&row.Data, &row.Metadata, &row.Source, &row.Timestamp)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("event not found: %s", eventID)
	}
	if err != nil {
		pes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	event, err := pes.rowToEvent(&row)
	if err != nil {
		return nil, err
	}

	pes.stats.Metrics.EventsRead++
	return event, nil
}

// GetEventsByAggregate implements EventStore
func (pes *PostgresEventStore) GetEventsByAggregate(ctx context.Context, aggregateID string, fromVersion int) ([]*core.Event, error) {
	query := `SELECT id, aggregate_id, type, version, data, metadata, source, timestamp
			  FROM forge_events WHERE aggregate_id = $1 AND version >= $2 ORDER BY version ASC`

	rows, err := pes.db.QueryContext(ctx, query, aggregateID, fromVersion)
	if err != nil {
		pes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	events := make([]*core.Event, 0)
	for rows.Next() {
		var row PostgresEventRow
		err := rows.Scan(&row.ID, &row.AggregateID, &row.Type, &row.Version,
			&row.Data, &row.Metadata, &row.Source, &row.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		event, err := pes.rowToEvent(&row)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	pes.stats.Metrics.EventsRead += int64(len(events))
	return events, nil
}

// GetEventsByType implements EventStore
func (pes *PostgresEventStore) GetEventsByType(ctx context.Context, eventType string, fromTime, toTime time.Time) ([]*core.Event, error) {
	query := `SELECT id, aggregate_id, type, version, data, metadata, source, timestamp
			  FROM forge_events WHERE type = $1 AND timestamp BETWEEN $2 AND $3 ORDER BY timestamp ASC`

	rows, err := pes.db.QueryContext(ctx, query, eventType, fromTime, toTime)
	if err != nil {
		pes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	events := make([]*core.Event, 0)
	for rows.Next() {
		var row PostgresEventRow
		err := rows.Scan(&row.ID, &row.AggregateID, &row.Type, &row.Version,
			&row.Data, &row.Metadata, &row.Source, &row.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		event, err := pes.rowToEvent(&row)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	pes.stats.Metrics.EventsRead += int64(len(events))
	return events, nil
}

// QueryEvents implements EventStore
func (pes *PostgresEventStore) QueryEvents(ctx context.Context, criteria *core.EventCriteria) ([]*core.Event, error) {
	query := "SELECT id, aggregate_id, type, version, data, metadata, source, timestamp FROM forge_events WHERE 1=1"
	args := make([]interface{}, 0)
	argPos := 1

	if len(criteria.AggregateIDs) > 0 {
		query += fmt.Sprintf(" AND aggregate_id = ANY($%d)", argPos)
		args = append(args, pq.Array(criteria.AggregateIDs))
		argPos++
	}
	if len(criteria.EventTypes) > 0 {
		query += fmt.Sprintf(" AND type = ANY($%d)", argPos)
		args = append(args, pq.Array(criteria.EventTypes))
		argPos++
	}
	if !criteria.StartTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argPos)
		args = append(args, criteria.StartTime)
		argPos++
	}
	if !criteria.EndTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= $%d", argPos)
		args = append(args, criteria.EndTime)
		argPos++
	}

	query += " ORDER BY timestamp ASC"

	if criteria.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, criteria.Limit)
	}

	rows, err := pes.db.QueryContext(ctx, query, args...)
	if err != nil {
		pes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	events := make([]*core.Event, 0)
	for rows.Next() {
		var row PostgresEventRow
		err := rows.Scan(&row.ID, &row.AggregateID, &row.Type, &row.Version,
			&row.Data, &row.Metadata, &row.Source, &row.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		event, err := pes.rowToEvent(&row)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	pes.stats.Metrics.EventsRead += int64(len(events))
	return events, nil
}

// CreateSnapshot implements EventStore
func (pes *PostgresEventStore) CreateSnapshot(ctx context.Context, snapshot *core.Snapshot) error {
	data, err := json.Marshal(snapshot.Data)
	if err != nil {
		pes.stats.Metrics.Errors++
		return fmt.Errorf("failed to marshal snapshot data: %w", err)
	}

	metadata, err := json.Marshal(snapshot.Metadata)
	if err != nil {
		pes.stats.Metrics.Errors++
		return fmt.Errorf("failed to marshal snapshot metadata: %w", err)
	}

	query := `INSERT INTO forge_snapshots (id, aggregate_id, type, version, data, metadata, timestamp)
			  VALUES ($1, $2, $3, $4, $5, $6, $7)
			  ON CONFLICT (aggregate_id) DO UPDATE SET
			  type = $3, version = $4, data = $5, metadata = $6, timestamp = $7`

	_, err = pes.db.ExecContext(ctx, query, snapshot.ID, snapshot.AggregateID, snapshot.Type,
		snapshot.Version, data, metadata, snapshot.Timestamp)
	if err != nil {
		pes.stats.Metrics.Errors++
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	pes.stats.Metrics.SnapshotsCreated++
	pes.stats.TotalSnapshots++
	pes.stats.SnapshotsByType[snapshot.Type]++

	if pes.metrics != nil {
		pes.metrics.Counter("forge.events.store.snapshots_created").Inc()
	}

	return nil
}

// GetSnapshot implements EventStore
func (pes *PostgresEventStore) GetSnapshot(ctx context.Context, aggregateID string) (*core.Snapshot, error) {
	query := `SELECT id, aggregate_id, type, version, data, metadata, timestamp
			  FROM forge_snapshots WHERE aggregate_id = $1`

	var row PostgresSnapshotRow
	err := pes.db.QueryRowContext(ctx, query, aggregateID).Scan(
		&row.ID, &row.AggregateID, &row.Type, &row.Version,
		&row.Data, &row.Metadata, &row.Timestamp)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("snapshot not found: %s", aggregateID)
	}
	if err != nil {
		pes.stats.Metrics.Errors++
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	snapshot, err := pes.rowToSnapshot(&row)
	if err != nil {
		return nil, err
	}

	pes.stats.Metrics.SnapshotsRead++
	return snapshot, nil
}

// GetStats implements EventStore
func (pes *PostgresEventStore) GetStats() *core.EventStoreStats {
	return pes.stats
}

// Close implements EventStore
func (pes *PostgresEventStore) Close(ctx context.Context) error {
	// DB connection is managed externally
	return nil
}

// rowToEvent converts a PostgresEventRow to Event
func (pes *PostgresEventStore) rowToEvent(row *PostgresEventRow) (*core.Event, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(row.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	var metadata map[string]interface{}
	if len(row.Metadata) > 0 {
		if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event metadata: %w", err)
		}
	}

	return &core.Event{
		ID:          row.ID,
		AggregateID: row.AggregateID,
		Type:        row.Type,
		Version:     row.Version,
		Data:        data,
		Metadata:    metadata,
		Source:      row.Source,
		Timestamp:   row.Timestamp,
	}, nil
}

// rowToSnapshot converts a PostgresSnapshotRow to Snapshot
func (pes *PostgresEventStore) rowToSnapshot(row *PostgresSnapshotRow) (*core.Snapshot, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(row.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot data: %w", err)
	}

	var metadata map[string]interface{}
	if len(row.Metadata) > 0 {
		if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal snapshot metadata: %w", err)
		}
	}

	return &core.Snapshot{
		ID:          row.ID,
		AggregateID: row.AggregateID,
		Type:        row.Type,
		Version:     row.Version,
		Data:        data,
		Metadata:    metadata,
		Timestamp:   row.Timestamp,
	}, nil
}
