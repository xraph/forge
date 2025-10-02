package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// DatabaseStorage implements Storage interface using database
type DatabaseStorage struct {
	db        database.Connection
	tableName string
	logger    common.Logger
	metrics   common.Metrics
	mu        sync.RWMutex
	stats     StorageStats
	startTime time.Time

	// Configuration
	config DatabaseStorageConfig

	// Prepared statements for performance
	stmtInsertEntry         *sql.Stmt
	stmtGetEntry            *sql.Stmt
	stmtGetEntries          *sql.Stmt
	stmtDeleteEntry         *sql.Stmt
	stmtGetLastIndex        *sql.Stmt
	stmtGetFirstIndex       *sql.Stmt
	stmtStoreSnapshot       *sql.Stmt
	stmtGetSnapshot         *sql.Stmt
	stmtDeleteSnapshot      *sql.Stmt
	stmtStoreTerm           *sql.Stmt
	stmtGetTerm             *sql.Stmt
	stmtStoreVote           *sql.Stmt
	stmtGetVote             *sql.Stmt
	stmtStoreElectionRecord *sql.Stmt
	stmtGetElectionHistory  *sql.Stmt
}

// DatabaseStorageConfig contains configuration for database storage
type DatabaseStorageConfig struct {
	ConnectionName    string        `json:"connection_name"`
	TableName         string        `json:"table_name"`
	MetadataTableName string        `json:"metadata_table_name"`
	SnapshotTableName string        `json:"snapshot_table_name"`
	BatchSize         int           `json:"batch_size"`
	ConnectionTimeout time.Duration `json:"connection_timeout"`
	QueryTimeout      time.Duration `json:"query_timeout"`
	MaxRetries        int           `json:"max_retries"`
	RetryDelay        time.Duration `json:"retry_delay"`
}

// LogEntryRecord represents a log entry record in the database
type LogEntryRecord struct {
	Index     uint64    `db:"index"`
	Term      uint64    `db:"term"`
	Type      string    `db:"type"`
	Data      []byte    `db:"data"`
	Metadata  []byte    `db:"metadata"`
	Timestamp time.Time `db:"timestamp"`
	Checksum  uint32    `db:"checksum"`
}

// SnapshotRecord represents a snapshot record in the database
type SnapshotRecord struct {
	Index              uint64    `db:"index"`
	Term               uint64    `db:"term"`
	Data               []byte    `db:"data"`
	Metadata           []byte    `db:"metadata"`
	Timestamp          time.Time `db:"timestamp"`
	Checksum           uint32    `db:"checksum"`
	Configuration      []byte    `db:"configuration"`
	ConfigurationIndex uint64    `db:"configuration_index"`
}

// MetadataRecord represents metadata in the database
type MetadataRecord struct {
	Key       string    `db:"key"`
	Value     string    `db:"value"`
	UpdatedAt time.Time `db:"updated_at"`
}

// NewDatabaseStorage creates a new database-backed storage
func NewDatabaseStorage(config DatabaseStorageConfig, dbConn database.Connection, l common.Logger, metrics common.Metrics) (Storage, error) {
	if config.ConnectionName == "" {
		return nil, NewStorageError(ErrCodeInvalidConfig, "connection_name is required")
	}

	if config.TableName == "" {
		config.TableName = "raft_log"
	}
	if config.MetadataTableName == "" {
		config.MetadataTableName = "raft_metadata"
	}
	if config.SnapshotTableName == "" {
		config.SnapshotTableName = "raft_snapshot"
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 1000
	}
	if config.ConnectionTimeout <= 0 {
		config.ConnectionTimeout = 30 * time.Second
	}
	if config.QueryTimeout <= 0 {
		config.QueryTimeout = 30 * time.Second
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = time.Second
	}

	ds := &DatabaseStorage{
		db:        dbConn,
		tableName: config.TableName,
		logger:    l,
		metrics:   metrics,
		config:    config,
		startTime: time.Now(),
		stats: StorageStats{
			ReadCount:  0,
			WriteCount: 0,
			ErrorCount: 0,
		},
	}

	// Initialize database schema
	if err := ds.initSchema(); err != nil {
		return nil, err
	}

	// Prepare statements
	if err := ds.prepareStatements(); err != nil {
		return nil, err
	}

	// Load initial stats
	if err := ds.loadStats(); err != nil {
		return nil, err
	}

	if l != nil {
		l.Info("database storage initialized",
			logger.String("table_name", config.TableName),
			logger.String("connection_name", config.ConnectionName),
		)
	}

	return ds, nil
}

// initSchema initializes the database schema
func (ds *DatabaseStorage) initSchema() error {
	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	// Create log entries table
	createLogTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			idx BIGINT PRIMARY KEY,
			term BIGINT NOT NULL,
			type VARCHAR(50) NOT NULL,
			data BYTEA NOT NULL,
			metadata BYTEA,
			timestamp TIMESTAMP NOT NULL,
			checksum INTEGER NOT NULL,
			INDEX idx_term (term),
			INDEX idx_timestamp (timestamp)
		)
	`, ds.config.TableName)

	if _, err := db.Exec(createLogTable); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to create log table: %v", err))
	}

	// Create metadata table
	createMetadataTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			key VARCHAR(255) PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)
	`, ds.config.MetadataTableName)

	if _, err := db.Exec(createMetadataTable); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to create metadata table: %v", err))
	}

	// Create snapshot table
	createSnapshotTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			idx BIGINT PRIMARY KEY,
			term BIGINT NOT NULL,
			data BYTEA NOT NULL,
			metadata BYTEA,
			timestamp TIMESTAMP NOT NULL,
			checksum INTEGER NOT NULL,
			configuration BYTEA,
			configuration_index BIGINT NOT NULL
		)
	`, ds.config.SnapshotTableName)

	if _, err := db.Exec(createSnapshotTable); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to create snapshot table: %v", err))
	}

	return nil
}

// prepareStatements prepares SQL statements for better performance
func (ds *DatabaseStorage) prepareStatements() error {
	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	var err error

	// Insert entry
	ds.stmtInsertEntry, err = db.Prepare(fmt.Sprintf(`
		INSERT INTO %s (idx, term, type, data, metadata, timestamp, checksum) 
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, ds.config.TableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare insert statement: %v", err))
	}

	// Get entry
	ds.stmtGetEntry, err = db.Prepare(fmt.Sprintf(`
		SELECT idx, term, type, data, metadata, timestamp, checksum 
		FROM %s WHERE idx = $1
	`, ds.config.TableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare get entry statement: %v", err))
	}

	// Get entries range
	ds.stmtGetEntries, err = db.Prepare(fmt.Sprintf(`
		SELECT idx, term, type, data, metadata, timestamp, checksum 
		FROM %s WHERE idx >= $1 AND idx <= $2 ORDER BY idx
	`, ds.config.TableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare get entries statement: %v", err))
	}

	// Delete entry
	ds.stmtDeleteEntry, err = db.Prepare(fmt.Sprintf(`
		DELETE FROM %s WHERE idx = $1
	`, ds.config.TableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare delete entry statement: %v", err))
	}

	// Get last index
	ds.stmtGetLastIndex, err = db.Prepare(fmt.Sprintf(`
		SELECT COALESCE(MAX(idx), 0) FROM %s
	`, ds.config.TableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare get last index statement: %v", err))
	}

	// Get first index
	ds.stmtGetFirstIndex, err = db.Prepare(fmt.Sprintf(`
		SELECT COALESCE(MIN(idx), 0) FROM %s
	`, ds.config.TableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare get first index statement: %v", err))
	}

	// Store snapshot
	ds.stmtStoreSnapshot, err = db.Prepare(fmt.Sprintf(`
		INSERT INTO %s (idx, term, data, metadata, timestamp, checksum, configuration, configuration_index)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (idx) DO UPDATE SET
			term = EXCLUDED.term,
			data = EXCLUDED.data,
			metadata = EXCLUDED.metadata,
			timestamp = EXCLUDED.timestamp,
			checksum = EXCLUDED.checksum,
			configuration = EXCLUDED.configuration,
			configuration_index = EXCLUDED.configuration_index
	`, ds.config.SnapshotTableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare store snapshot statement: %v", err))
	}

	// Get snapshot
	ds.stmtGetSnapshot, err = db.Prepare(fmt.Sprintf(`
		SELECT idx, term, data, metadata, timestamp, checksum, configuration, configuration_index
		FROM %s ORDER BY idx DESC LIMIT 1
	`, ds.config.SnapshotTableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare get snapshot statement: %v", err))
	}

	// Delete snapshot
	ds.stmtDeleteSnapshot, err = db.Prepare(fmt.Sprintf(`
		DELETE FROM %s
	`, ds.config.SnapshotTableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare delete snapshot statement: %v", err))
	}

	// Store term
	ds.stmtStoreTerm, err = db.Prepare(fmt.Sprintf(`
		INSERT INTO %s (key, value, updated_at) VALUES ('current_term', $1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
	`, ds.config.MetadataTableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare store term statement: %v", err))
	}

	// Get term
	ds.stmtGetTerm, err = db.Prepare(fmt.Sprintf(`
		SELECT value FROM %s WHERE key = 'current_term'
	`, ds.config.MetadataTableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare get term statement: %v", err))
	}

	// Store vote
	ds.stmtStoreVote, err = db.Prepare(fmt.Sprintf(`
		INSERT INTO %s (key, value, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
	`, ds.config.MetadataTableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare store vote statement: %v", err))
	}

	// Get vote
	ds.stmtGetVote, err = db.Prepare(fmt.Sprintf(`
		SELECT value FROM %s WHERE key = $1
	`, ds.config.MetadataTableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare get vote statement: %v", err))
	}

	// Prepare election-related statements
	if err := ds.preparElectionStatements(); err != nil {
		return err
	}

	return nil
}

// loadStats loads initial statistics from the database
func (ds *DatabaseStorage) loadStats() error {
	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	// Get entry count
	var entryCount int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", ds.config.TableName)
	if err := db.QueryRow(query).Scan(&entryCount); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get entry count: %v", err))
	}

	ds.stats.EntryCount = entryCount

	// Get first and last index
	if entryCount > 0 {
		if err := ds.stmtGetFirstIndex.QueryRow().Scan(&ds.stats.FirstIndex); err != nil {
			return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get first index: %v", err))
		}

		if err := ds.stmtGetLastIndex.QueryRow().Scan(&ds.stats.LastIndex); err != nil {
			return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get last index: %v", err))
		}
	}

	return nil
}

// StoreEntry stores a log entry
func (ds *DatabaseStorage) StoreEntry(ctx context.Context, entry LogEntry) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	start := time.Now()
	defer func() {
		ds.stats.WriteLatency = time.Since(start)
		ds.stats.WriteCount++
	}()

	// Validate entry
	if err := ValidateLogEntry(entry); err != nil {
		ds.stats.ErrorCount++
		return err
	}

	// Calculate checksum if not provided
	if entry.Checksum == 0 {
		entry.Checksum = CalculateChecksum(entry.Data)
	}

	// Serialize metadata
	metadataBytes, err := json.Marshal(entry.Metadata)
	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to serialize metadata: %v", err))
	}

	// Execute with retries
	err = ds.executeWithRetry(ctx, func() error {
		_, err := ds.stmtInsertEntry.ExecContext(ctx,
			entry.Index,
			entry.Term,
			string(entry.Type),
			entry.Data,
			metadataBytes,
			entry.Timestamp,
			entry.Checksum,
		)
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to store entry: %v", err))
	}

	// Update stats
	ds.stats.EntryCount++
	if ds.stats.FirstIndex == 0 || entry.Index < ds.stats.FirstIndex {
		ds.stats.FirstIndex = entry.Index
	}
	if entry.Index > ds.stats.LastIndex {
		ds.stats.LastIndex = entry.Index
	}

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.entries_written").Inc()
		ds.metrics.Histogram("forge.consensus.storage.write_latency").Observe(ds.stats.WriteLatency.Seconds())
	}

	return nil
}

// GetEntry retrieves a log entry by index
func (ds *DatabaseStorage) GetEntry(ctx context.Context, index uint64) (*LogEntry, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	start := time.Now()
	defer func() {
		ds.stats.ReadLatency = time.Since(start)
		ds.stats.ReadCount++
	}()

	var record LogEntryRecord
	var metadataBytes []byte

	err := ds.executeWithRetry(ctx, func() error {
		return ds.stmtGetEntry.QueryRowContext(ctx, index).Scan(
			&record.Index,
			&record.Term,
			&record.Type,
			&record.Data,
			&metadataBytes,
			&record.Timestamp,
			&record.Checksum,
		)
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, NewStorageError(ErrCodeEntryNotFound, fmt.Sprintf("entry with index %d not found", index))
		}
		ds.stats.ErrorCount++
		return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get entry: %v", err))
	}

	// Deserialize metadata
	var metadata map[string]interface{}
	if len(metadataBytes) > 0 {
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			ds.stats.ErrorCount++
			return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to deserialize metadata: %v", err))
		}
	}

	entry := &LogEntry{
		Index:     record.Index,
		Term:      record.Term,
		Type:      EntryType(record.Type),
		Data:      record.Data,
		Metadata:  metadata,
		Timestamp: record.Timestamp,
		Checksum:  record.Checksum,
	}

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.entries_read").Inc()
		ds.metrics.Histogram("forge.consensus.storage.read_latency").Observe(ds.stats.ReadLatency.Seconds())
	}

	return entry, nil
}

// GetEntries retrieves log entries in a range
func (ds *DatabaseStorage) GetEntries(ctx context.Context, start, end uint64) ([]LogEntry, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if start > end {
		return nil, NewStorageError(ErrCodeInvalidIndex, "start index must be <= end index")
	}

	var entries []LogEntry
	err := ds.executeWithRetry(ctx, func() error {
		rows, err := ds.stmtGetEntries.QueryContext(ctx, start, end)
		if err != nil {
			return err
		}
		defer rows.Close()

		entries = make([]LogEntry, 0, end-start+1)
		for rows.Next() {
			var record LogEntryRecord
			var metadataBytes []byte

			err := rows.Scan(
				&record.Index,
				&record.Term,
				&record.Type,
				&record.Data,
				&metadataBytes,
				&record.Timestamp,
				&record.Checksum,
			)
			if err != nil {
				return err
			}

			// Deserialize metadata
			var metadata map[string]interface{}
			if len(metadataBytes) > 0 {
				if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
					return err
				}
			}

			entry := LogEntry{
				Index:     record.Index,
				Term:      record.Term,
				Type:      EntryType(record.Type),
				Data:      record.Data,
				Metadata:  metadata,
				Timestamp: record.Timestamp,
				Checksum:  record.Checksum,
			}

			entries = append(entries, entry)
		}

		return rows.Err()
	})

	if err != nil {
		ds.stats.ErrorCount++
		return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get entries: %v", err))
	}

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.entries_read").Add(float64(len(entries)))
	}

	return entries, nil
}

// GetLastEntry retrieves the last log entry
func (ds *DatabaseStorage) GetLastEntry(ctx context.Context) (*LogEntry, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if ds.stats.LastIndex == 0 {
		return nil, NewStorageError(ErrCodeEntryNotFound, "no entries found")
	}

	return ds.GetEntry(ctx, ds.stats.LastIndex)
}

// GetFirstEntry retrieves the first log entry
func (ds *DatabaseStorage) GetFirstEntry(ctx context.Context) (*LogEntry, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if ds.stats.FirstIndex == 0 {
		return nil, NewStorageError(ErrCodeEntryNotFound, "no entries found")
	}

	return ds.GetEntry(ctx, ds.stats.FirstIndex)
}

// DeleteEntry deletes a log entry
func (ds *DatabaseStorage) DeleteEntry(ctx context.Context, index uint64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	err := ds.executeWithRetry(ctx, func() error {
		_, err := ds.stmtDeleteEntry.ExecContext(ctx, index)
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to delete entry: %v", err))
	}

	ds.stats.EntryCount--

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.entries_deleted").Inc()
	}

	return nil
}

// DeleteEntriesFrom deletes entries from a given index
func (ds *DatabaseStorage) DeleteEntriesFrom(ctx context.Context, index uint64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE idx >= $1", ds.config.TableName)

	err := ds.executeWithRetry(ctx, func() error {
		_, err := db.ExecContext(ctx, query, index)
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to delete entries from index %d: %v", index, err))
	}

	// Update stats
	ds.stats.LastIndex = index - 1
	if ds.stats.FirstIndex > 0 {
		ds.stats.EntryCount = int64(ds.stats.LastIndex - ds.stats.FirstIndex + 1)
	}

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.entries_deleted").Inc()
	}

	return nil
}

// DeleteEntriesTo deletes entries up to a given index
func (ds *DatabaseStorage) DeleteEntriesTo(ctx context.Context, index uint64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE idx <= $1", ds.config.TableName)

	err := ds.executeWithRetry(ctx, func() error {
		_, err := db.ExecContext(ctx, query, index)
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to delete entries to index %d: %v", index, err))
	}

	// Update stats
	ds.stats.FirstIndex = index + 1
	if ds.stats.LastIndex > 0 {
		ds.stats.EntryCount = int64(ds.stats.LastIndex - ds.stats.FirstIndex + 1)
	}

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.entries_deleted").Inc()
	}

	return nil
}

// GetLastIndex returns the index of the last log entry
func (ds *DatabaseStorage) GetLastIndex(ctx context.Context) (uint64, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var lastIndex uint64
	err := ds.executeWithRetry(ctx, func() error {
		return ds.stmtGetLastIndex.QueryRowContext(ctx).Scan(&lastIndex)
	})

	if err != nil {
		ds.stats.ErrorCount++
		return 0, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get last index: %v", err))
	}

	return lastIndex, nil
}

// GetFirstIndex returns the index of the first log entry
func (ds *DatabaseStorage) GetFirstIndex(ctx context.Context) (uint64, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var firstIndex uint64
	err := ds.executeWithRetry(ctx, func() error {
		return ds.stmtGetFirstIndex.QueryRowContext(ctx).Scan(&firstIndex)
	})

	if err != nil {
		ds.stats.ErrorCount++
		return 0, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get first index: %v", err))
	}

	return firstIndex, nil
}

// StoreSnapshot stores a snapshot
func (ds *DatabaseStorage) StoreSnapshot(ctx context.Context, snapshot Snapshot) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Validate snapshot
	if err := ValidateSnapshot(snapshot); err != nil {
		return err
	}

	// Serialize metadata
	metadataBytes, err := json.Marshal(snapshot.Metadata)
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to serialize metadata: %v", err))
	}

	err = ds.executeWithRetry(ctx, func() error {
		_, err := ds.stmtStoreSnapshot.ExecContext(ctx,
			snapshot.Index,
			snapshot.Term,
			snapshot.Data,
			metadataBytes,
			snapshot.Timestamp,
			snapshot.Checksum,
			snapshot.Configuration,
			snapshot.ConfigurationIndex,
		)
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to store snapshot: %v", err))
	}

	ds.stats.SnapshotSize = int64(len(snapshot.Data))

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.snapshots_stored").Inc()
	}

	return nil
}

// GetSnapshot retrieves a snapshot
func (ds *DatabaseStorage) GetSnapshot(ctx context.Context) (*Snapshot, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var record SnapshotRecord
	var metadataBytes []byte

	err := ds.executeWithRetry(ctx, func() error {
		return ds.stmtGetSnapshot.QueryRowContext(ctx).Scan(
			&record.Index,
			&record.Term,
			&record.Data,
			&metadataBytes,
			&record.Timestamp,
			&record.Checksum,
			&record.Configuration,
			&record.ConfigurationIndex,
		)
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, NewStorageError(ErrCodeSnapshotNotFound, "snapshot not found")
		}
		ds.stats.ErrorCount++
		return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get snapshot: %v", err))
	}

	// Deserialize metadata
	var metadata map[string]interface{}
	if len(metadataBytes) > 0 {
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			ds.stats.ErrorCount++
			return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to deserialize metadata: %v", err))
		}
	}

	snapshot := &Snapshot{
		Index:              record.Index,
		Term:               record.Term,
		Data:               record.Data,
		Metadata:           metadata,
		Timestamp:          record.Timestamp,
		Checksum:           record.Checksum,
		Configuration:      record.Configuration,
		ConfigurationIndex: record.ConfigurationIndex,
	}

	return snapshot, nil
}

// DeleteSnapshot deletes a snapshot
func (ds *DatabaseStorage) DeleteSnapshot(ctx context.Context) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	err := ds.executeWithRetry(ctx, func() error {
		_, err := ds.stmtDeleteSnapshot.ExecContext(ctx)
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to delete snapshot: %v", err))
	}

	ds.stats.SnapshotSize = 0

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.snapshots_deleted").Inc()
	}

	return nil
}

// StoreTerm stores the current term
func (ds *DatabaseStorage) StoreTerm(ctx context.Context, term uint64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	err := ds.executeWithRetry(ctx, func() error {
		_, err := ds.stmtStoreTerm.ExecContext(ctx, fmt.Sprintf("%d", term), time.Now())
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to store term: %v", err))
	}

	return nil
}

// GetTerm retrieves the current term
func (ds *DatabaseStorage) GetTerm(ctx context.Context) (uint64, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var termStr string
	err := ds.executeWithRetry(ctx, func() error {
		return ds.stmtGetTerm.QueryRowContext(ctx).Scan(&termStr)
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // Default term is 0
		}
		ds.stats.ErrorCount++
		return 0, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get term: %v", err))
	}

	var term uint64
	if _, err := fmt.Sscanf(termStr, "%d", &term); err != nil {
		ds.stats.ErrorCount++
		return 0, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to parse term: %v", err))
	}

	return term, nil
}

// StoreVote stores the vote for a term
func (ds *DatabaseStorage) StoreVote(ctx context.Context, term uint64, candidateID string) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	voteKey := fmt.Sprintf("vote_term_%d", term)

	err := ds.executeWithRetry(ctx, func() error {
		_, err := ds.stmtStoreVote.ExecContext(ctx, voteKey, candidateID, time.Now())
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to store vote: %v", err))
	}

	return nil
}

// GetVote retrieves the vote for a term
func (ds *DatabaseStorage) GetVote(ctx context.Context, term uint64) (string, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	voteKey := fmt.Sprintf("vote_term_%d", term)
	var candidateID string

	err := ds.executeWithRetry(ctx, func() error {
		return ds.stmtGetVote.QueryRowContext(ctx, voteKey).Scan(&candidateID)
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // No vote cast
		}
		ds.stats.ErrorCount++
		return "", NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get vote: %v", err))
	}

	return candidateID, nil
}

// Sync ensures all data is persisted
func (ds *DatabaseStorage) Sync(ctx context.Context) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	start := time.Now()
	defer func() {
		ds.stats.SyncLatency = time.Since(start)
		ds.stats.SyncCount++
	}()

	// For database storage, sync is typically handled by the database engine
	// We can force a transaction commit if needed

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.syncs").Inc()
		ds.metrics.Histogram("forge.consensus.storage.sync_latency").Observe(ds.stats.SyncLatency.Seconds())
	}

	return nil
}

// Close closes the storage
func (ds *DatabaseStorage) Close(ctx context.Context) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Close prepared statements
	statements := []*sql.Stmt{
		ds.stmtInsertEntry,
		ds.stmtGetEntry,
		ds.stmtGetEntries,
		ds.stmtDeleteEntry,
		ds.stmtGetLastIndex,
		ds.stmtGetFirstIndex,
		ds.stmtStoreSnapshot,
		ds.stmtGetSnapshot,
		ds.stmtDeleteSnapshot,
		ds.stmtStoreTerm,
		ds.stmtGetTerm,
		ds.stmtStoreVote,
		ds.stmtGetVote,
		ds.stmtStoreElectionRecord,
		ds.stmtGetElectionHistory,
	}

	for _, stmt := range statements {
		if stmt != nil {
			stmt.Close()
		}
	}

	return nil
}

// GetStats returns storage statistics
func (ds *DatabaseStorage) GetStats() StorageStats {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	stats := ds.stats
	stats.Uptime = time.Since(ds.startTime)

	return stats
}

// Compact compacts the storage
func (ds *DatabaseStorage) Compact(ctx context.Context, index uint64) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	start := time.Now()
	defer func() {
		ds.stats.CompactionCount++
		ds.stats.LastCompaction = time.Now()
	}()

	// Delete entries up to the given index
	if err := ds.DeleteEntriesTo(ctx, index); err != nil {
		return err
	}

	// Vacuum/optimize the database (implementation depends on database type)
	db, ok := ds.db.DB().(*sql.DB)
	if ok {
		// For PostgreSQL
		_, err := db.ExecContext(ctx, "VACUUM "+ds.config.TableName)
		if err != nil {
			ds.logger.Warn("failed to vacuum table", logger.Error(err))
		}
	}

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.compactions").Inc()
		ds.metrics.Histogram("forge.consensus.storage.compaction_duration").Observe(time.Since(start).Seconds())
	}

	return nil
}

// executeWithRetry executes a function with retry logic
func (ds *DatabaseStorage) executeWithRetry(ctx context.Context, fn func() error) error {
	var lastErr error

	for i := 0; i < ds.config.MaxRetries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Check if error is retryable
		if !ds.isRetryableError(lastErr) {
			return lastErr
		}

		// Wait before retry
		if i < ds.config.MaxRetries-1 {
			time.Sleep(ds.config.RetryDelay * time.Duration(i+1))
		}
	}

	return lastErr
}

func (ds *DatabaseStorage) StoreState(ctx context.Context, state *PersistentState) error {
	// TODO implement me
	panic("implement me")
}

func (ds *DatabaseStorage) LoadState(ctx context.Context) (*PersistentState, error) {
	// TODO implement me
	panic("implement me")
}

func (ds *DatabaseStorage) HealthCheck(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

// isRetryableError checks if an error is retryable
func (ds *DatabaseStorage) isRetryableError(err error) bool {
	// This is a simplified check - in practice, you'd check for specific database errors
	// like connection timeouts, deadlocks, etc.
	return err != nil && err != sql.ErrNoRows
}

// DatabaseStorageFactory creates database storage instances
type DatabaseStorageFactory struct {
	dbManager database.Manager
}

// NewDatabaseStorageFactory creates a new database storage factory
func NewDatabaseStorageFactory(dbManager database.Manager) *DatabaseStorageFactory {
	return &DatabaseStorageFactory{
		dbManager: dbManager,
	}
}

// Create creates a new database storage instance
func (f *DatabaseStorageFactory) Create(config StorageConfig, logger common.Logger, metrics common.Metrics) (Storage, error) {
	// Get database connection
	conn, err := f.dbManager.GetConnection("consensus")
	if err != nil {
		return nil, NewStorageError(ErrCodeInvalidConfig, fmt.Sprintf("failed to get database connection: %v", err))
	}

	dbConfig := DatabaseStorageConfig{
		ConnectionName:    "consensus",
		TableName:         "raft_log",
		MetadataTableName: "raft_metadata",
		SnapshotTableName: "raft_snapshot",
		BatchSize:         config.BatchSize,
		ConnectionTimeout: 30 * time.Second,
		QueryTimeout:      30 * time.Second,
		MaxRetries:        3,
		RetryDelay:        time.Second,
	}

	return NewDatabaseStorage(dbConfig, conn, logger, metrics)
}

// Name returns the factory name
func (f *DatabaseStorageFactory) Name() string {
	return "database"
}

// Version returns the factory version
func (f *DatabaseStorageFactory) Version() string {
	return "1.0.0"
}

// ValidateConfig validates the configuration
func (f *DatabaseStorageFactory) ValidateConfig(config StorageConfig) error {
	if config.BatchSize < 0 {
		return NewStorageError(ErrCodeInvalidConfig, "batch_size must be >= 0")
	}

	return nil
}

// GetVotedFor retrieves the vote for a term (alias for GetVote for consistency)
func (ds *DatabaseStorage) GetVotedFor(ctx context.Context, term uint64) (string, error) {
	return ds.GetVote(ctx, term)
}

// StoreVotedFor stores the vote for a term (alias for StoreVote for consistency)
func (ds *DatabaseStorage) StoreVotedFor(ctx context.Context, term uint64, candidateID string) error {
	return ds.StoreVote(ctx, term, candidateID)
}

// StoreElectionRecord stores an election record
func (ds *DatabaseStorage) StoreElectionRecord(ctx context.Context, record ElectionRecord) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	// Create elections table if it doesn't exist
	createElectionsTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s_elections (
			id SERIAL PRIMARY KEY,
			term BIGINT NOT NULL,
			winner VARCHAR(255),
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP NOT NULL,
			duration_ms BIGINT NOT NULL,
			vote_count INTEGER NOT NULL,
			participants TEXT NOT NULL,
			reason VARCHAR(255) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_term (term),
			INDEX idx_start_time (start_time)
		)
	`, ds.config.TableName)

	if _, err := db.ExecContext(ctx, createElectionsTable); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to create elections table: %v", err))
	}

	// Serialize participants
	participantsJSON, err := json.Marshal(record.Participants)
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to serialize participants: %v", err))
	}

	// Insert election record
	insertQuery := fmt.Sprintf(`
		INSERT INTO %s_elections (term, winner, start_time, end_time, duration_ms, vote_count, participants, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, ds.config.TableName)

	err = ds.executeWithRetry(ctx, func() error {
		_, err := db.ExecContext(ctx, insertQuery,
			record.Term,
			record.Winner,
			record.StartTime,
			record.EndTime,
			record.Duration.Milliseconds(),
			record.VoteCount,
			string(participantsJSON),
			record.Reason,
		)
		return err
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to store election record: %v", err))
	}

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.election_records_stored").Inc()
	}

	return nil
}

// GetElectionHistory retrieves election history with a limit
func (ds *DatabaseStorage) GetElectionHistory(ctx context.Context, limit int) ([]ElectionRecord, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return nil, NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	// Check if elections table exists
	checkTableQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM information_schema.tables 
		WHERE table_name = '%s_elections'
	`, ds.config.TableName)

	var tableCount int
	if err := db.QueryRowContext(ctx, checkTableQuery).Scan(&tableCount); err != nil {
		return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to check elections table: %v", err))
	}

	if tableCount == 0 {
		return []ElectionRecord{}, nil
	}

	// Query election records
	queryBuilder := fmt.Sprintf(`
		SELECT term, winner, start_time, end_time, duration_ms, vote_count, participants, reason
		FROM %s_elections
		ORDER BY term DESC, start_time DESC
	`, ds.config.TableName)

	if limit > 0 {
		queryBuilder += fmt.Sprintf(" LIMIT %d", limit)
	}

	var records []ElectionRecord

	err := ds.executeWithRetry(ctx, func() error {
		rows, err := db.QueryContext(ctx, queryBuilder)
		if err != nil {
			return err
		}
		defer rows.Close()

		records = make([]ElectionRecord, 0)
		for rows.Next() {
			var record ElectionRecord
			var durationMs int64
			var participantsJSON string

			err := rows.Scan(
				&record.Term,
				&record.Winner,
				&record.StartTime,
				&record.EndTime,
				&durationMs,
				&record.VoteCount,
				&participantsJSON,
				&record.Reason,
			)
			if err != nil {
				return err
			}

			// Convert duration from milliseconds
			record.Duration = time.Duration(durationMs) * time.Millisecond

			// Deserialize participants
			if err := json.Unmarshal([]byte(participantsJSON), &record.Participants); err != nil {
				if ds.logger != nil {
					ds.logger.Warn("failed to deserialize participants",
						logger.Uint64("term", record.Term),
						logger.Error(err))
				}
				record.Participants = []string{} // Set empty slice on error
			}

			records = append(records, record)
		}

		return rows.Err()
	})

	if err != nil {
		ds.stats.ErrorCount++
		return nil, NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to get election history: %v", err))
	}

	return records, nil
}

// CleanupOldElectionRecords removes old election records to prevent unlimited growth
func (ds *DatabaseStorage) CleanupOldElectionRecords(ctx context.Context, maxRecords int) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	// Count current records
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s_elections", ds.config.TableName)
	var currentCount int
	if err := db.QueryRowContext(ctx, countQuery).Scan(&currentCount); err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to count election records: %v", err))
	}

	if currentCount <= maxRecords {
		return nil // Nothing to clean up
	}

	// Delete oldest records
	deleteQuery := fmt.Sprintf(`
		DELETE FROM %s_elections 
		WHERE id IN (
			SELECT id FROM %s_elections 
			ORDER BY term ASC, start_time ASC 
			LIMIT %d
		)
	`, ds.config.TableName, ds.config.TableName, currentCount-maxRecords)

	err := ds.executeWithRetry(ctx, func() error {
		result, err := db.ExecContext(ctx, deleteQuery)
		if err != nil {
			return err
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}

		if ds.logger != nil {
			ds.logger.Info("cleaned up old election records",
				logger.Int64("records_deleted", rowsAffected),
				logger.Int("max_records", maxRecords))
		}

		return nil
	})

	if err != nil {
		ds.stats.ErrorCount++
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to cleanup election records: %v", err))
	}

	if ds.metrics != nil {
		ds.metrics.Counter("forge.consensus.storage.election_records_cleaned").Inc()
	}

	return nil
}

// UpdatePreparedStatements adds the new prepared statements to the existing prepareStatements method
func (ds *DatabaseStorage) preparElectionStatements() error {
	db, ok := ds.db.DB().(*sql.DB)
	if !ok {
		return NewStorageError(ErrCodeInvalidConfig, "database connection is not *sql.DB")
	}

	var err error

	// Store election record
	ds.stmtStoreElectionRecord, err = db.Prepare(fmt.Sprintf(`
		INSERT INTO %s_elections (term, winner, start_time, end_time, duration_ms, vote_count, participants, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, ds.config.TableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare store election record statement: %v", err))
	}

	// Get election history
	ds.stmtGetElectionHistory, err = db.Prepare(fmt.Sprintf(`
		SELECT term, winner, start_time, end_time, duration_ms, vote_count, participants, reason
		FROM %s_elections
		ORDER BY term DESC, start_time DESC
		LIMIT $1
	`, ds.config.TableName))
	if err != nil {
		return NewStorageError(ErrCodeIOError, fmt.Sprintf("failed to prepare get election history statement: %v", err))
	}

	return nil
}
