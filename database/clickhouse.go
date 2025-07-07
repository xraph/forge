package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jmoiron/sqlx"
)

// clickHouseDatabase implements NoSQLDatabase interface for ClickHouse
type clickHouseDatabase struct {
	*baseNoSQLDatabase
	conn   driver.Conn
	db     *sql.DB
	sqlxDB *sqlx.DB
	config clickhouse.Options
}

// clickHouseCollection implements Collection interface for ClickHouse
type clickHouseCollection struct {
	*baseCollection
	db        *clickHouseDatabase
	tableName string
}

// clickHouseCursor implements Cursor interface for ClickHouse
type clickHouseCursor struct {
	*baseCursor
	rows   driver.Rows
	ctx    context.Context
	closed bool
	mu     sync.RWMutex
}

// NewClickHouseDatabase creates a new ClickHouse database connection
func newClickHouseDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &clickHouseDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "clickhouse",
			connected: false,
			stats:     make(map[string]interface{}),
		},
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}

	return db, nil
}

// Connect establishes ClickHouse connection
func (db *clickHouseDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Build ClickHouse connection options
	options := db.buildClickHouseOptions()
	db.config = options

	// Create native connection
	conn, err := clickhouse.Open(&options)
	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}

	db.conn = conn

	// Create SQL database connection for compatibility
	sqlDB := clickhouse.OpenDB(&options)
	db.db = sqlDB
	db.sqlxDB = sqlx.NewDb(sqlDB, "clickhouse")

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	// Create database if it doesn't exist
	if db.baseNoSQLDatabase.config.Database != "" {
		if err := db.ensureDatabase(ctx); err != nil {
			return fmt.Errorf("failed to ensure database: %w", err)
		}
	}

	db.connected = true
	return nil
}

// buildClickHouseOptions builds ClickHouse connection options
func (db *clickHouseDatabase) buildClickHouseOptions() clickhouse.Options {
	// Parse configuration
	host := db.baseNoSQLDatabase.config.Host
	if host == "" {
		host = "localhost"
	}

	port := db.baseNoSQLDatabase.config.Port
	if port == 0 {
		port = 9000 // ClickHouse native port
	}

	database := db.baseNoSQLDatabase.config.Database
	if database == "" {
		database = "default"
	}

	// Build options
	options := clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", host, port)},
		Auth: clickhouse.Auth{
			Database: database,
			Username: db.baseNoSQLDatabase.config.Username,
			Password: db.baseNoSQLDatabase.config.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
	}

	// Configure connection pool
	if db.baseNoSQLDatabase.config.MaxPoolSize > 0 {
		options.MaxOpenConns = db.baseNoSQLDatabase.config.MaxPoolSize
	} else {
		options.MaxOpenConns = 10
	}

	if db.baseNoSQLDatabase.config.MaxIdleTimeMS > 0 {
		options.MaxIdleConns = int(db.baseNoSQLDatabase.config.MaxIdleTimeMS.Seconds())
	} else {
		options.MaxIdleConns = 5
	}

	// Configure timeouts
	if db.baseNoSQLDatabase.config.ConnectTimeout > 0 {
		options.DialTimeout = db.baseNoSQLDatabase.config.ConnectTimeout
	} else {
		options.DialTimeout = 10 * time.Second
	}

	if db.baseNoSQLDatabase.config.SocketTimeout > 0 {
		options.ReadTimeout = db.baseNoSQLDatabase.config.SocketTimeout
	} else {
		options.ReadTimeout = 10 * time.Second
	}

	return options
}

// ensureDatabase creates the database if it doesn't exist
func (db *clickHouseDatabase) ensureDatabase(ctx context.Context) error {
	// Check if database exists
	query := "SELECT name FROM system.databases WHERE name = ?"
	var dbName string
	err := db.conn.QueryRow(ctx, query, db.baseNoSQLDatabase.config.Database).Scan(&dbName)

	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if dbName == "" {
		// Create database
		createQuery := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", db.baseNoSQLDatabase.config.Database)
		if err := db.conn.Exec(ctx, createQuery); err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
	}

	return nil
}

// Close closes ClickHouse connection
func (db *clickHouseDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.conn != nil {
		db.conn.Close()
	}

	if db.db != nil {
		db.db.Close()
	}

	db.connected = false
	return nil
}

// Ping tests ClickHouse connection
func (db *clickHouseDatabase) Ping(ctx context.Context) error {
	if db.conn == nil {
		return fmt.Errorf("ClickHouse not connected")
	}

	return db.conn.Ping(ctx)
}

// Collection returns a ClickHouse collection (table)
func (db *clickHouseDatabase) Collection(name string) Collection {
	return &clickHouseCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		db:        db,
		tableName: name,
	}
}

// CreateCollection creates a new collection (table)
func (db *clickHouseDatabase) CreateCollection(ctx context.Context, name string) error {
	if db.conn == nil {
		return fmt.Errorf("ClickHouse not connected")
	}

	// Create a basic table structure for document storage
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.%s (
			id UUID DEFAULT generateUUIDv4(),
			data String,
			created_at DateTime64(3) DEFAULT now64(),
			updated_at DateTime64(3) DEFAULT now64()
		) ENGINE = MergeTree()
		ORDER BY (id, created_at)
		SETTINGS index_granularity = 8192
	`, db.baseNoSQLDatabase.config.Database, name)

	return db.conn.Exec(ctx, query)
}

// DropCollection drops a collection (table)
func (db *clickHouseDatabase) DropCollection(ctx context.Context, name string) error {
	if db.conn == nil {
		return fmt.Errorf("ClickHouse not connected")
	}

	query := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", db.baseNoSQLDatabase.config.Database, name)
	return db.conn.Exec(ctx, query)
}

// ListCollections lists all collections (tables)
func (db *clickHouseDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("ClickHouse not connected")
	}

	query := "SELECT name FROM system.tables WHERE database = ?"
	rows, err := db.conn.Query(ctx, query, db.baseNoSQLDatabase.config.Database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		tables = append(tables, tableName)
	}

	return tables, nil
}

// Drop drops the entire database
func (db *clickHouseDatabase) Drop(ctx context.Context) error {
	if db.conn == nil {
		return fmt.Errorf("ClickHouse not connected")
	}

	query := fmt.Sprintf("DROP DATABASE IF EXISTS %s", db.baseNoSQLDatabase.config.Database)
	return db.conn.Exec(ctx, query)
}

// ClickHouse Collection implementation

// FindOne finds a single document
func (c *clickHouseCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	if c.db.conn == nil {
		return nil, fmt.Errorf("connection not initialized")
	}

	query := fmt.Sprintf("SELECT id, data, created_at, updated_at FROM %s.%s LIMIT 1",
		c.db.baseNoSQLDatabase.config.Database, c.tableName)

	var id string
	var data string
	var createdAt, updatedAt time.Time

	err := c.db.conn.QueryRow(ctx, query).Scan(&id, &data, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no documents found")
		}
		return nil, err
	}

	result := map[string]interface{}{
		"id":         id,
		"data":       data,
		"created_at": createdAt,
		"updated_at": updatedAt,
	}

	return result, nil
}

// Find finds multiple documents
func (c *clickHouseCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if c.db.conn == nil {
		return nil, fmt.Errorf("connection not initialized")
	}

	query := fmt.Sprintf("SELECT id, data, created_at, updated_at FROM %s.%s",
		c.db.baseNoSQLDatabase.config.Database, c.tableName)

	rows, err := c.db.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	return &clickHouseCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		rows:   rows,
		ctx:    ctx,
		closed: false,
	}, nil
}

// Insert inserts a single document
func (c *clickHouseCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if c.db.conn == nil {
		return nil, fmt.Errorf("connection not initialized")
	}

	// Convert document to string (simplified)
	data := fmt.Sprintf("%v", document)

	query := fmt.Sprintf(`
		INSERT INTO %s.%s (data, created_at, updated_at)
		VALUES (?, now64(), now64())
	`, c.db.baseNoSQLDatabase.config.Database, c.tableName)

	err := c.db.conn.Exec(ctx, query, data)
	if err != nil {
		return nil, err
	}

	// Return a generated ID (simplified)
	return fmt.Sprintf("doc_%d", time.Now().UnixNano()), nil
}

// InsertMany inserts multiple documents
func (c *clickHouseCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if c.db.conn == nil {
		return nil, fmt.Errorf("connection not initialized")
	}

	// Prepare batch insert
	batch, err := c.db.conn.PrepareBatch(ctx, fmt.Sprintf(
		"INSERT INTO %s.%s (data, created_at, updated_at)",
		c.db.baseNoSQLDatabase.config.Database, c.tableName))
	if err != nil {
		return nil, err
	}

	var ids []interface{}
	for _, document := range documents {
		data := fmt.Sprintf("%v", document)
		now := time.Now()

		err := batch.Append(data, now, now)
		if err != nil {
			return nil, err
		}

		ids = append(ids, fmt.Sprintf("doc_%d", now.UnixNano()))
	}

	err = batch.Send()
	if err != nil {
		return nil, err
	}

	return ids, nil
}

// Update updates multiple documents
func (c *clickHouseCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	if c.db.conn == nil {
		return 0, fmt.Errorf("connection not initialized")
	}

	// ClickHouse doesn't support traditional UPDATE operations
	// You would typically use ALTER TABLE UPDATE or mutations
	return 0, fmt.Errorf("update operations not directly supported in ClickHouse")
}

// UpdateOne updates a single document
func (c *clickHouseCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	if c.db.conn == nil {
		return fmt.Errorf("connection not initialized")
	}

	// ClickHouse doesn't support traditional UPDATE operations
	return fmt.Errorf("update operations not directly supported in ClickHouse")
}

// Delete deletes multiple documents
func (c *clickHouseCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	if c.db.conn == nil {
		return 0, fmt.Errorf("connection not initialized")
	}

	// ClickHouse doesn't support traditional DELETE operations
	// You would typically use ALTER TABLE DELETE or mutations
	return 0, fmt.Errorf("delete operations not directly supported in ClickHouse")
}

// DeleteOne deletes a single document
func (c *clickHouseCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	if c.db.conn == nil {
		return fmt.Errorf("connection not initialized")
	}

	// ClickHouse doesn't support traditional DELETE operations
	return fmt.Errorf("delete operations not directly supported in ClickHouse")
}

// Aggregate performs aggregation
func (c *clickHouseCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if c.db.conn == nil {
		return nil, fmt.Errorf("connection not initialized")
	}

	// ClickHouse is excellent for aggregations
	// This is a simplified implementation - you'd build proper aggregation queries
	query := fmt.Sprintf("SELECT COUNT(*) as count FROM %s.%s",
		c.db.baseNoSQLDatabase.config.Database, c.tableName)

	rows, err := c.db.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	return &clickHouseCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		rows:   rows,
		ctx:    ctx,
		closed: false,
	}, nil
}

// Count counts documents
func (c *clickHouseCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	if c.db.conn == nil {
		return 0, fmt.Errorf("connection not initialized")
	}

	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", c.db.baseNoSQLDatabase.config.Database, c.tableName)

	var count int64
	err := c.db.conn.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// CreateIndex creates an index (not applicable for ClickHouse)
func (c *clickHouseCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	// ClickHouse uses different indexing mechanisms
	// You would typically define indexes in the table schema
	return fmt.Errorf("index creation not supported - define indexes in table schema")
}

// DropIndex drops an index (not applicable for ClickHouse)
func (c *clickHouseCollection) DropIndex(ctx context.Context, name string) error {
	return fmt.Errorf("index dropping not supported")
}

// ListIndexes lists all indexes (not applicable for ClickHouse)
func (c *clickHouseCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	return []IndexInfo{}, nil
}

// BulkWrite performs bulk operations
func (c *clickHouseCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if c.db.conn == nil {
		return fmt.Errorf("connection not initialized")
	}

	// Prepare batch insert
	batch, err := c.db.conn.PrepareBatch(ctx, fmt.Sprintf(
		"INSERT INTO %s.%s (data, created_at, updated_at)",
		c.db.baseNoSQLDatabase.config.Database, c.tableName))
	if err != nil {
		return err
	}

	for _, op := range operations {
		data := fmt.Sprintf("%v", op)
		now := time.Now()

		err := batch.Append(data, now, now)
		if err != nil {
			return err
		}
	}

	return batch.Send()
}

// ClickHouse Cursor implementation

// Next moves to the next document
func (c *clickHouseCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.rows == nil {
		return false
	}

	return c.rows.Next()
}

// Decode decodes the current document
func (c *clickHouseCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.rows == nil {
		return fmt.Errorf("cursor is closed")
	}

	// This is a simplified decode - in a real implementation, you'd properly handle dest
	return c.rows.Scan(dest)
}

// All decodes all documents
func (c *clickHouseCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.rows == nil {
		return fmt.Errorf("cursor is closed")
	}

	// This is a simplified implementation
	// In a real implementation, you'd properly handle scanning all rows into dest
	return nil
}

// Close closes the cursor
func (c *clickHouseCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rows != nil {
		err := c.rows.Close()
		c.closed = true
		return err
	}

	return nil
}

// Current returns the current document
func (c *clickHouseCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.rows == nil {
		return nil
	}

	// This is a simplified implementation
	// In a real implementation, you'd return the current row data
	return nil
}

// Err returns the last error
func (c *clickHouseCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.rows != nil {
		return c.rows.Err()
	}

	return c.err
}

// ClickHouse-specific utility functions

// GetConn returns the underlying ClickHouse connection
func (db *clickHouseDatabase) GetConn() driver.Conn {
	return db.conn
}

// GetSQLDB returns the SQL database connection
func (db *clickHouseDatabase) GetSQLDB() *sql.DB {
	return db.db
}

// GetSQLXDB returns the sqlx database connection
func (db *clickHouseDatabase) GetSQLXDB() *sqlx.DB {
	return db.sqlxDB
}

// ExecuteSQL executes a SQL query
func (db *clickHouseDatabase) ExecuteSQL(ctx context.Context, query string, args ...interface{}) error {
	if db.conn == nil {
		return fmt.Errorf("ClickHouse not connected")
	}

	return db.conn.Exec(ctx, query, args...)
}

// QuerySQL executes a SQL query and returns rows
func (db *clickHouseDatabase) QuerySQL(ctx context.Context, query string, args ...interface{}) (driver.Rows, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("ClickHouse not connected")
	}

	return db.conn.Query(ctx, query, args...)
}

// PrepareBatch prepares a batch for bulk inserts
func (db *clickHouseDatabase) PrepareBatch(ctx context.Context, query string) (driver.Batch, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("ClickHouse not connected")
	}

	return db.conn.PrepareBatch(ctx, query)
}

// GetServerVersion returns the ClickHouse server version
func (db *clickHouseDatabase) GetServerVersion(ctx context.Context) (string, error) {
	if db.conn == nil {
		return "", fmt.Errorf("ClickHouse not connected")
	}

	var version string
	err := db.conn.QueryRow(ctx, "SELECT version()").Scan(&version)
	return version, err
}

// GetDatabaseSize returns the database size
func (db *clickHouseDatabase) GetDatabaseSize(ctx context.Context) (int64, error) {
	if db.conn == nil {
		return 0, fmt.Errorf("ClickHouse not connected")
	}

	query := `
		SELECT sum(bytes_on_disk) as total_size
		FROM system.parts
		WHERE database = ?
	`

	var totalSize int64
	err := db.conn.QueryRow(ctx, query, db.baseNoSQLDatabase.config.Database).Scan(&totalSize)
	return totalSize, err
}

// GetTableInfo returns information about tables
func (db *clickHouseDatabase) GetTableInfo(ctx context.Context) ([]map[string]interface{}, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("ClickHouse not connected")
	}

	query := `
		SELECT 
			name,
			engine,
			total_rows,
			total_bytes
		FROM system.tables
		WHERE database = ?
	`

	rows, err := db.conn.Query(ctx, query, db.baseNoSQLDatabase.config.Database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []map[string]interface{}
	for rows.Next() {
		var name, engine string
		var totalRows, totalBytes int64

		if err := rows.Scan(&name, &engine, &totalRows, &totalBytes); err != nil {
			continue
		}

		table := map[string]interface{}{
			"name":        name,
			"engine":      engine,
			"total_rows":  totalRows,
			"total_bytes": totalBytes,
		}
		tables = append(tables, table)
	}

	return tables, nil
}

// OptimizeTable optimizes a table
func (db *clickHouseDatabase) OptimizeTable(ctx context.Context, tableName string) error {
	if db.conn == nil {
		return fmt.Errorf("ClickHouse not connected")
	}

	query := fmt.Sprintf("OPTIMIZE TABLE %s.%s", db.baseNoSQLDatabase.config.Database, tableName)
	return db.conn.Exec(ctx, query)
}

// GetSystemMetrics returns system metrics
func (db *clickHouseDatabase) GetSystemMetrics(ctx context.Context) (map[string]interface{}, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("ClickHouse not connected")
	}

	query := "SELECT metric, value FROM system.metrics"
	rows, err := db.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metrics := make(map[string]interface{})
	for rows.Next() {
		var metric string
		var value int64

		if err := rows.Scan(&metric, &value); err != nil {
			continue
		}

		metrics[metric] = value
	}

	return metrics, nil
}

// GetConnectionInfo returns ClickHouse connection information
func (db *clickHouseDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.conn == nil {
		return nil, fmt.Errorf("ClickHouse not connected")
	}

	info := make(map[string]interface{})

	// Get version
	version, err := db.GetServerVersion(ctx)
	if err != nil {
		return nil, err
	}
	info["version"] = version

	// Get database
	info["database"] = db.baseNoSQLDatabase.config.Database

	// Get user
	info["user"] = db.config.Auth.Username

	// Get addresses
	info["addresses"] = db.config.Addr

	return info, nil
}

// Stats returns ClickHouse statistics
func (db *clickHouseDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}

	stats["driver"] = db.driver
	stats["connected"] = db.connected
	stats["database"] = db.baseNoSQLDatabase.config.Database
	stats["addresses"] = db.config.Addr

	return stats
}

// init function to override the ClickHouse constructor
func init() {
	NewClickHouseDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newClickHouseDatabase(config)
	}
}
