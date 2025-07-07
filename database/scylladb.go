package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"
)

// scyllaDatabase implements NoSQLDatabase interface for ScyllaDB
type scyllaDatabase struct {
	*baseNoSQLDatabase
	session    *gocql.Session
	cluster    *gocql.ClusterConfig
	keyspace   string
	hosts      []string
	port       int
	username   string
	password   string
	timeout    time.Duration
	retryCount int
}

// scyllaCollection implements Collection interface for ScyllaDB
type scyllaCollection struct {
	*baseCollection
	session  *gocql.Session
	table    string
	keyspace string
}

// scyllaCursor implements Cursor interface for ScyllaDB
type scyllaCursor struct {
	*baseCursor
	iter    *gocql.Iter
	scanner gocql.Scanner
	closed  bool
	mu      sync.RWMutex
}

// ScyllaDocument represents a ScyllaDB document
type ScyllaDocument struct {
	ID        string                 `json:"id"`
	Data      map[string]interface{} `json:"data"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	TTL       *int                   `json:"ttl,omitempty"`
}

// ScyllaQuery represents a ScyllaDB query
type ScyllaQuery struct {
	Query       string             `json:"query"`
	Values      []interface{}      `json:"values,omitempty"`
	Consistency gocql.Consistency  `json:"consistency,omitempty"`
	PageSize    int                `json:"page_size,omitempty"`
	Timeout     time.Duration      `json:"timeout,omitempty"`
	Retry       *gocql.RetryPolicy `json:"retry,omitempty"`
}

// NewScyllaDatabase creates a new ScyllaDB database connection
func newScyllaDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &scyllaDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "scylladb",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		keyspace:   config.Database,
		username:   config.Username,
		password:   config.Password,
		timeout:    30 * time.Second,
		retryCount: 3,
	}

	// Set timeout from config
	if config.ConnectTimeout > 0 {
		db.timeout = config.ConnectTimeout
	}

	// Parse hosts
	if config.URL != "" {
		// Parse URL format: scylla://host1,host2:port/keyspace
		parts := strings.Split(config.URL, "://")
		if len(parts) == 2 {
			urlParts := strings.Split(parts[1], "/")
			hostsPart := urlParts[0]
			if len(urlParts) > 1 {
				db.keyspace = urlParts[1]
			}

			// Parse hosts and port
			if strings.Contains(hostsPart, ":") {
				hostPort := strings.Split(hostsPart, ":")
				db.hosts = strings.Split(hostPort[0], ",")
				if len(hostPort) > 1 {
					if port, err := strconv.Atoi(hostPort[1]); err == nil {
						db.port = port
					}
				}
			} else {
				db.hosts = strings.Split(hostsPart, ",")
			}
		}
	} else {
		// Use individual config fields
		if config.Host != "" {
			db.hosts = []string{config.Host}
		} else {
			db.hosts = []string{"127.0.0.1"}
		}
		if config.Port > 0 {
			db.port = config.Port
		} else {
			db.port = 9042 // Default ScyllaDB port
		}
	}

	// Set default keyspace
	if db.keyspace == "" {
		db.keyspace = "forge"
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}

	return db, nil
}

// Connect establishes ScyllaDB connection
func (db *scyllaDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Create cluster configuration
	db.cluster = gocql.NewCluster(db.hosts...)
	db.cluster.Port = db.port
	db.cluster.Timeout = db.timeout
	db.cluster.ConnectTimeout = db.timeout
	db.cluster.NumConns = 2
	db.cluster.Consistency = gocql.LocalQuorum
	db.cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		Max:        3,
		Min:        100 * time.Millisecond,
		NumRetries: db.retryCount,
	}

	// Set authentication if provided
	if db.username != "" && db.password != "" {
		db.cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: db.username,
			Password: db.password,
		}
	}

	// Connect without keyspace first to create it if needed
	tempSession, err := db.cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to connect to ScyllaDB: %w", err)
	}

	// Create keyspace if it doesn't exist
	createKeyspaceQuery := fmt.Sprintf(`
		CREATE KEYSPACE IF NOT EXISTS %s
		WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}
	`, db.keyspace)

	if err := tempSession.Query(createKeyspaceQuery).Exec(); err != nil {
		tempSession.Close()
		return fmt.Errorf("failed to create keyspace: %w", err)
	}
	tempSession.Close()

	// Now connect to the specific keyspace
	db.cluster.Keyspace = db.keyspace
	db.session, err = db.cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to connect to keyspace: %w", err)
	}

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping ScyllaDB: %w", err)
	}

	db.connected = true
	return nil
}

// Close closes ScyllaDB connection
func (db *scyllaDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.session != nil {
		db.session.Close()
	}

	db.connected = false
	return nil
}

// Ping tests ScyllaDB connection
func (db *scyllaDatabase) Ping(ctx context.Context) error {
	if db.session == nil {
		return fmt.Errorf("ScyllaDB not connected")
	}

	// Simple query to test connection
	query := "SELECT now() FROM system.local"
	iter := db.session.Query(query).Iter()
	defer iter.Close()

	var now time.Time
	if !iter.Scan(&now) {
		if err := iter.Close(); err != nil {
			return fmt.Errorf("ping failed: %w", err)
		}
		return fmt.Errorf("ping failed: no response")
	}

	return iter.Close()
}

// Collection returns a ScyllaDB collection (table)
func (db *scyllaDatabase) Collection(name string) Collection {
	return &scyllaCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		session:  db.session,
		table:    name,
		keyspace: db.keyspace,
	}
}

// CreateCollection creates a new collection (table)
func (db *scyllaDatabase) CreateCollection(ctx context.Context, name string) error {
	if db.session == nil {
		return fmt.Errorf("ScyllaDB not connected")
	}

	// Create table with default schema
	createTableQuery := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.%s (
			id text PRIMARY KEY,
			data text,
			created_at timestamp,
			updated_at timestamp,
			ttl int
		)
	`, db.keyspace, name)

	if err := db.session.Query(createTableQuery).Exec(); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// DropCollection drops a collection (table)
func (db *scyllaDatabase) DropCollection(ctx context.Context, name string) error {
	if db.session == nil {
		return fmt.Errorf("ScyllaDB not connected")
	}

	dropTableQuery := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", db.keyspace, name)
	if err := db.session.Query(dropTableQuery).Exec(); err != nil {
		return fmt.Errorf("failed to drop table: %w", err)
	}

	return nil
}

// ListCollections lists all collections (tables)
func (db *scyllaDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.session == nil {
		return nil, fmt.Errorf("ScyllaDB not connected")
	}

	query := "SELECT table_name FROM system_schema.tables WHERE keyspace_name = ?"
	iter := db.session.Query(query, db.keyspace).Iter()
	defer iter.Close()

	var tables []string
	var tableName string
	for iter.Scan(&tableName) {
		tables = append(tables, tableName)
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	return tables, nil
}

// Drop drops the entire database (keyspace)
func (db *scyllaDatabase) Drop(ctx context.Context) error {
	if db.session == nil {
		return fmt.Errorf("ScyllaDB not connected")
	}

	dropKeyspaceQuery := fmt.Sprintf("DROP KEYSPACE IF EXISTS %s", db.keyspace)
	if err := db.session.Query(dropKeyspaceQuery).Exec(); err != nil {
		return fmt.Errorf("failed to drop keyspace: %w", err)
	}

	return nil
}

// ScyllaDB Collection implementation

// FindOne finds a single document
func (c *scyllaCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// Build query from filter
	query, values := c.buildSelectQuery(filter, 1)

	iter := c.session.Query(query, values...).Iter()
	defer iter.Close()

	var doc ScyllaDocument
	if c.scanDocument(iter, &doc) {
		return doc, nil
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return nil, fmt.Errorf("no documents found")
}

// Find finds multiple documents
func (c *scyllaCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// Build query from filter
	query, values := c.buildSelectQuery(filter, 0)

	iter := c.session.Query(query, values...).Iter()

	return &scyllaCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		iter:   iter,
		closed: false,
	}, nil
}

// Insert inserts a single document
func (c *scyllaCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// Convert document to ScyllaDocument
	doc, err := c.convertToScyllaDocument(document)
	if err != nil {
		return nil, err
	}

	// Generate ID if not provided
	if doc.ID == "" {
		doc.ID = c.generateID()
	}

	// Set timestamps
	now := time.Now()
	doc.CreatedAt = now
	doc.UpdatedAt = now

	// Serialize data
	dataJSON, err := json.Marshal(doc.Data)
	if err != nil {
		return nil, err
	}

	// Insert document
	insertQuery := fmt.Sprintf(`
		INSERT INTO %s.%s (id, data, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`, c.keyspace, c.table)

	if err := c.session.Query(insertQuery, doc.ID, string(dataJSON), doc.CreatedAt, doc.UpdatedAt).Exec(); err != nil {
		return nil, fmt.Errorf("failed to insert document: %w", err)
	}

	return doc.ID, nil
}

// InsertMany inserts multiple documents
func (c *scyllaCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	var ids []interface{}
	batch := c.session.NewBatch(gocql.LoggedBatch)

	for _, document := range documents {
		// Convert document to ScyllaDocument
		doc, err := c.convertToScyllaDocument(document)
		if err != nil {
			return nil, err
		}

		// Generate ID if not provided
		if doc.ID == "" {
			doc.ID = c.generateID()
		}

		// Set timestamps
		now := time.Now()
		doc.CreatedAt = now
		doc.UpdatedAt = now

		// Serialize data
		dataJSON, err := json.Marshal(doc.Data)
		if err != nil {
			return nil, err
		}

		// Add to batch
		insertQuery := fmt.Sprintf(`
			INSERT INTO %s.%s (id, data, created_at, updated_at)
			VALUES (?, ?, ?, ?)
		`, c.keyspace, c.table)

		batch.Query(insertQuery, doc.ID, string(dataJSON), doc.CreatedAt, doc.UpdatedAt)
		ids = append(ids, doc.ID)
	}

	// Execute batch
	if err := c.session.ExecuteBatch(batch); err != nil {
		return nil, fmt.Errorf("failed to insert documents: %w", err)
	}

	return ids, nil
}

// Update updates multiple documents
func (c *scyllaCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	if c.session == nil {
		return 0, fmt.Errorf("session not initialized")
	}

	// ScyllaDB doesn't support UPDATE without WHERE clause
	// We need to find documents first, then update them
	cursor, err := c.Find(ctx, filter)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var count int64
	batch := c.session.NewBatch(gocql.LoggedBatch)

	for cursor.Next(ctx) {
		var doc ScyllaDocument
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		// Apply update
		updatedDoc := c.applyUpdate(doc, update)
		updatedDoc.UpdatedAt = time.Now()

		// Serialize data
		dataJSON, err := json.Marshal(updatedDoc.Data)
		if err != nil {
			continue
		}

		// Add to batch
		updateQuery := fmt.Sprintf(`
			UPDATE %s.%s
			SET data = ?, updated_at = ?
			WHERE id = ?
		`, c.keyspace, c.table)

		batch.Query(updateQuery, string(dataJSON), updatedDoc.UpdatedAt, doc.ID)
		count++
	}

	// Execute batch
	if err := c.session.ExecuteBatch(batch); err != nil {
		return 0, fmt.Errorf("failed to update documents: %w", err)
	}

	return count, nil
}

// UpdateOne updates a single document
func (c *scyllaCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	// Find the document first
	doc, err := c.FindOne(ctx, filter)
	if err != nil {
		return err
	}

	scyllaDoc, ok := doc.(ScyllaDocument)
	if !ok {
		return fmt.Errorf("invalid document type")
	}

	// Apply update
	updatedDoc := c.applyUpdate(scyllaDoc, update)
	updatedDoc.UpdatedAt = time.Now()

	// Serialize data
	dataJSON, err := json.Marshal(updatedDoc.Data)
	if err != nil {
		return err
	}

	// Update document
	updateQuery := fmt.Sprintf(`
		UPDATE %s.%s
		SET data = ?, updated_at = ?
		WHERE id = ?
	`, c.keyspace, c.table)

	if err := c.session.Query(updateQuery, string(dataJSON), updatedDoc.UpdatedAt, scyllaDoc.ID).Exec(); err != nil {
		return fmt.Errorf("failed to update document: %w", err)
	}

	return nil
}

// Delete deletes multiple documents
func (c *scyllaCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	if c.session == nil {
		return 0, fmt.Errorf("session not initialized")
	}

	// Find documents first
	cursor, err := c.Find(ctx, filter)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var count int64
	batch := c.session.NewBatch(gocql.LoggedBatch)

	for cursor.Next(ctx) {
		var doc ScyllaDocument
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		// Add to batch
		deleteQuery := fmt.Sprintf(`
			DELETE FROM %s.%s
			WHERE id = ?
		`, c.keyspace, c.table)

		batch.Query(deleteQuery, doc.ID)
		count++
	}

	// Execute batch
	if err := c.session.ExecuteBatch(batch); err != nil {
		return 0, fmt.Errorf("failed to delete documents: %w", err)
	}

	return count, nil
}

// DeleteOne deletes a single document
func (c *scyllaCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	// Find the document first
	doc, err := c.FindOne(ctx, filter)
	if err != nil {
		return err
	}

	scyllaDoc, ok := doc.(ScyllaDocument)
	if !ok {
		return fmt.Errorf("invalid document type")
	}

	// Delete document
	deleteQuery := fmt.Sprintf(`
		DELETE FROM %s.%s
		WHERE id = ?
	`, c.keyspace, c.table)

	if err := c.session.Query(deleteQuery, scyllaDoc.ID).Exec(); err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}

	return nil
}

// Aggregate performs aggregation (limited support)
func (c *scyllaCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// ScyllaDB has limited aggregation support
	// This is a simplified implementation
	return c.Find(ctx, map[string]interface{}{})
}

// Count counts documents
func (c *scyllaCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	if c.session == nil {
		return 0, fmt.Errorf("session not initialized")
	}

	// Build count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", c.keyspace, c.table)

	// Apply filter if provided
	if filter != nil {
		whereClause, values := c.buildWhereClause(filter)
		if whereClause != "" {
			countQuery += " WHERE " + whereClause
			iter := c.session.Query(countQuery, values...).Iter()
			defer iter.Close()

			var count int64
			if iter.Scan(&count) {
				return count, nil
			}
			return 0, iter.Close()
		}
	}

	iter := c.session.Query(countQuery).Iter()
	defer iter.Close()

	var count int64
	if iter.Scan(&count) {
		return count, nil
	}

	return 0, iter.Close()
}

// CreateIndex creates an index
func (c *scyllaCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	indexName := "idx_" + c.table
	if opts != nil && opts.Name != nil {
		indexName = *opts.Name
	}

	// Build index query
	if keyMap, ok := keys.(map[string]interface{}); ok {
		for key := range keyMap {
			createIndexQuery := fmt.Sprintf(`
				CREATE INDEX IF NOT EXISTS %s
				ON %s.%s (%s)
			`, indexName+"_"+key, c.keyspace, c.table, key)

			if err := c.session.Query(createIndexQuery).Exec(); err != nil {
				return fmt.Errorf("failed to create index: %w", err)
			}
		}
	}

	return nil
}

// DropIndex drops an index
func (c *scyllaCollection) DropIndex(ctx context.Context, name string) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	dropIndexQuery := fmt.Sprintf("DROP INDEX IF EXISTS %s.%s", c.keyspace, name)
	if err := c.session.Query(dropIndexQuery).Exec(); err != nil {
		return fmt.Errorf("failed to drop index: %w", err)
	}

	return nil
}

// ListIndexes lists all indexes
func (c *scyllaCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	query := `
		SELECT index_name, options
		FROM system_schema.indexes
		WHERE keyspace_name = ? AND table_name = ?
	`

	iter := c.session.Query(query, c.keyspace, c.table).Iter()
	defer iter.Close()

	var indexes []IndexInfo
	var indexName string
	var options map[string]string

	for iter.Scan(&indexName, &options) {
		index := IndexInfo{
			Name:   indexName,
			Keys:   make(map[string]interface{}),
			Unique: false, // ScyllaDB doesn't support unique indexes
		}

		// Parse options to extract column info
		if target, ok := options["target"]; ok {
			index.Keys[target] = 1
		}

		indexes = append(indexes, index)
	}

	return indexes, iter.Close()
}

// BulkWrite performs bulk operations
func (c *scyllaCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	batch := c.session.NewBatch(gocql.LoggedBatch)

	for _, operation := range operations {
		// Convert operation to document and insert
		doc, err := c.convertToScyllaDocument(operation)
		if err != nil {
			return err
		}

		// Generate ID if not provided
		if doc.ID == "" {
			doc.ID = c.generateID()
		}

		// Set timestamps
		now := time.Now()
		doc.CreatedAt = now
		doc.UpdatedAt = now

		// Serialize data
		dataJSON, err := json.Marshal(doc.Data)
		if err != nil {
			return err
		}

		// Add to batch
		insertQuery := fmt.Sprintf(`
			INSERT INTO %s.%s (id, data, created_at, updated_at)
			VALUES (?, ?, ?, ?)
		`, c.keyspace, c.table)

		batch.Query(insertQuery, doc.ID, string(dataJSON), doc.CreatedAt, doc.UpdatedAt)
	}

	// Execute batch
	if err := c.session.ExecuteBatch(batch); err != nil {
		return fmt.Errorf("failed to execute bulk write: %w", err)
	}

	return nil
}

// Helper methods

// buildSelectQuery builds SELECT query from filter
func (c *scyllaCollection) buildSelectQuery(filter interface{}, limit int) (string, []interface{}) {
	query := fmt.Sprintf("SELECT id, data, created_at, updated_at FROM %s.%s", c.keyspace, c.table)
	var values []interface{}

	if filter != nil {
		whereClause, whereValues := c.buildWhereClause(filter)
		if whereClause != "" {
			query += " WHERE " + whereClause
			values = append(values, whereValues...)
		}
	}

	if limit > 0 {
		query += " LIMIT " + strconv.Itoa(limit)
	}

	return query, values
}

// buildWhereClause builds WHERE clause from filter
func (c *scyllaCollection) buildWhereClause(filter interface{}) (string, []interface{}) {
	var clauses []string
	var values []interface{}

	if filterMap, ok := filter.(map[string]interface{}); ok {
		for key, value := range filterMap {
			switch key {
			case "id":
				clauses = append(clauses, "id = ?")
				values = append(values, value)
			case "_id":
				clauses = append(clauses, "id = ?")
				values = append(values, value)
			default:
				// For other fields, we'd need to create secondary indexes
				// This is a simplified implementation
				continue
			}
		}
	}

	return strings.Join(clauses, " AND "), values
}

// convertToScyllaDocument converts interface{} to ScyllaDocument
func (c *scyllaCollection) convertToScyllaDocument(document interface{}) (ScyllaDocument, error) {
	var doc ScyllaDocument

	switch d := document.(type) {
	case ScyllaDocument:
		doc = d
	case map[string]interface{}:
		doc.Data = d
		if id, ok := d["id"].(string); ok {
			doc.ID = id
		}
		if id, ok := d["_id"].(string); ok {
			doc.ID = id
		}
		if ttl, ok := d["ttl"].(int); ok {
			doc.TTL = &ttl
		}
	default:
		// Convert to JSON and back
		jsonData, err := json.Marshal(document)
		if err != nil {
			return doc, err
		}

		var data map[string]interface{}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return doc, err
		}

		doc.Data = data
	}

	return doc, nil
}

// applyUpdate applies update operations to document
func (c *scyllaCollection) applyUpdate(doc ScyllaDocument, update interface{}) ScyllaDocument {
	if updateMap, ok := update.(map[string]interface{}); ok {
		for op, value := range updateMap {
			switch op {
			case "$set":
				if setValue, ok := value.(map[string]interface{}); ok {
					for key, val := range setValue {
						doc.Data[key] = val
					}
				}
			case "$unset":
				if unsetValue, ok := value.(map[string]interface{}); ok {
					for key := range unsetValue {
						delete(doc.Data, key)
					}
				}
			case "$inc":
				if incValue, ok := value.(map[string]interface{}); ok {
					for key, val := range incValue {
						if currentVal, exists := doc.Data[key]; exists {
							if currentFloat, ok := currentVal.(float64); ok {
								if incFloat, ok := val.(float64); ok {
									doc.Data[key] = currentFloat + incFloat
								}
							}
						}
					}
				}
			}
		}
	}

	return doc
}

// generateID generates a unique ID
func (c *scyllaCollection) generateID() string {
	return fmt.Sprintf("%d_%d", time.Now().UnixNano(), gocql.TimeUUID().Time().UnixNano())
}

// scanDocument scans a document from iterator
func (c *scyllaCollection) scanDocument(iter *gocql.Iter, doc *ScyllaDocument) bool {
	var dataJSON string
	if iter.Scan(&doc.ID, &dataJSON, &doc.CreatedAt, &doc.UpdatedAt) {
		// Parse JSON data
		if err := json.Unmarshal([]byte(dataJSON), &doc.Data); err != nil {
			doc.Data = make(map[string]interface{})
		}
		return true
	}
	return false
}

// ScyllaDB Cursor implementation

// Next moves to the next document
func (c *scyllaCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return false
	}

	var doc ScyllaDocument
	var dataJSON string
	if c.iter.Scan(&doc.ID, &dataJSON, &doc.CreatedAt, &doc.UpdatedAt) {
		// Parse JSON data
		if err := json.Unmarshal([]byte(dataJSON), &doc.Data); err != nil {
			doc.Data = make(map[string]interface{})
		}
		c.data = append(c.data, doc)
		c.index++
		return true
	}

	return false
}

// Decode decodes the current document
func (c *scyllaCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	if c.index < 0 || c.index >= len(c.data) {
		return fmt.Errorf("no current document")
	}

	// Convert to JSON and back to handle type conversion
	data, err := json.Marshal(c.data[c.index])
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// All decodes all documents
func (c *scyllaCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	// Read all remaining documents
	for c.Next(ctx) {
		// Documents are added to c.data in Next()
	}

	// Convert to JSON and back to handle type conversion
	data, err := json.Marshal(c.data)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// Close closes the cursor
func (c *scyllaCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.iter != nil {
		c.err = c.iter.Close()
	}
	c.closed = true
	return c.err
}

// Current returns the current document
func (c *scyllaCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.index < 0 || c.index >= len(c.data) {
		return nil
	}

	return c.data[c.index]
}

// Err returns the last error
func (c *scyllaCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.err
}

// ScyllaDB-specific utility functions

// GetSession returns the underlying ScyllaDB session
func (db *scyllaDatabase) GetSession() *gocql.Session {
	return db.session
}

// GetKeyspace returns the keyspace name
func (db *scyllaDatabase) GetKeyspace() string {
	return db.keyspace
}

// ExecuteQuery executes a custom CQL query
func (db *scyllaDatabase) ExecuteQuery(ctx context.Context, query string, values ...interface{}) error {
	if db.session == nil {
		return fmt.Errorf("ScyllaDB not connected")
	}

	return db.session.Query(query, values...).Exec()
}

// ExecuteQueryWithResults executes a CQL query and returns results
func (db *scyllaDatabase) ExecuteQueryWithResults(ctx context.Context, query string, values ...interface{}) (*gocql.Iter, error) {
	if db.session == nil {
		return nil, fmt.Errorf("ScyllaDB not connected")
	}

	return db.session.Query(query, values...).Iter(), nil
}

// GetClusterMetadata returns cluster metadata (fixed implementation)
func (db *scyllaDatabase) GetClusterMetadata() (map[string]interface{}, error) {
	if db.session == nil {
		return nil, fmt.Errorf("ScyllaDB not connected")
	}

	metadata := make(map[string]interface{})

	// Get basic cluster information from system tables
	// Query system.local for cluster information
	query := "SELECT cluster_name, release_version, data_center, rack FROM system.local"
	var clusterName, releaseVersion, dataCenter, rack string

	err := db.session.Query(query).Scan(&clusterName, &releaseVersion, &dataCenter, &rack)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster metadata: %w", err)
	}

	metadata["cluster_name"] = clusterName
	metadata["release_version"] = releaseVersion
	metadata["data_center"] = dataCenter
	metadata["rack"] = rack

	// Get peer information
	peers := []map[string]interface{}{}
	iter := db.session.Query("SELECT peer, data_center, rack, release_version FROM system.peers").Iter()

	var peer, peerDataCenter, peerRack, peerVersion string
	for iter.Scan(&peer, &peerDataCenter, &peerRack, &peerVersion) {
		peerInfo := map[string]interface{}{
			"peer":            peer,
			"data_center":     peerDataCenter,
			"rack":            peerRack,
			"release_version": peerVersion,
		}
		peers = append(peers, peerInfo)
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to get peer information: %w", err)
	}

	metadata["peers"] = peers
	metadata["total_hosts"] = len(peers) + 1 // +1 for local node

	// Add ScyllaDB-specific information if available
	scyllaQuery := "SELECT value FROM system.scylla_local WHERE key = 'cluster_name'"
	var scyllaClusterName string
	if err := db.session.Query(scyllaQuery).Scan(&scyllaClusterName); err == nil {
		metadata["scylla_cluster_name"] = scyllaClusterName
	}

	return metadata, nil
}

// GetKeyspaceMetadata returns keyspace metadata (fixed implementation)
func (db *scyllaDatabase) GetKeyspaceMetadata() (*gocql.KeyspaceMetadata, error) {
	if db.session == nil {
		return nil, fmt.Errorf("ScyllaDB not connected")
	}

	// Use gocql's built-in KeyspaceMetadata method
	metadata, err := db.session.KeyspaceMetadata(db.keyspace)
	if err != nil {
		return nil, fmt.Errorf("failed to get keyspace metadata for %s: %w", db.keyspace, err)
	}

	return metadata, nil
}

// GetConnectionInfo returns ScyllaDB connection information (updated implementation)
func (db *scyllaDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.session == nil {
		return nil, fmt.Errorf("ScyllaDB not connected")
	}

	info := make(map[string]interface{})

	// Get cluster metadata using our fixed method
	clusterMeta, err := db.GetClusterMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster metadata: %w", err)
	}

	// Add cluster information
	info["cluster_name"] = clusterMeta["cluster_name"]
	info["release_version"] = clusterMeta["release_version"]
	info["data_center"] = clusterMeta["data_center"]
	info["rack"] = clusterMeta["rack"]
	info["peers"] = clusterMeta["peers"]
	info["total_hosts"] = clusterMeta["total_hosts"]

	// Add ScyllaDB-specific info if available
	if scyllaClusterName, ok := clusterMeta["scylla_cluster_name"]; ok {
		info["scylla_cluster_name"] = scyllaClusterName
	}

	// Add current connection information
	info["keyspace"] = db.keyspace
	info["hosts"] = db.hosts
	info["port"] = db.port

	// Add cluster configuration information
	if db.cluster != nil {
		info["protocol_version"] = db.cluster.ProtoVersion
		info["consistency"] = db.cluster.Consistency.String()
		info["num_conns"] = db.cluster.NumConns
		info["connect_timeout"] = db.cluster.ConnectTimeout
		info["timeout"] = db.cluster.Timeout
	}

	// Add ScyllaDB-specific configuration
	info["retry_count"] = db.retryCount
	info["driver_type"] = "scylladb"

	return info, nil
}

// GetScyllaInfo returns ScyllaDB-specific information
func (db *scyllaDatabase) GetScyllaInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.session == nil {
		return nil, fmt.Errorf("ScyllaDB not connected")
	}

	info := make(map[string]interface{})

	// Get ScyllaDB version and features
	versionQuery := "SELECT value FROM system.scylla_local WHERE key = 'scylla_version'"
	var scyllaVersion string
	if err := db.session.Query(versionQuery).Scan(&scyllaVersion); err == nil {
		info["scylla_version"] = scyllaVersion
	}

	// Get CPU count
	cpuQuery := "SELECT value FROM system.scylla_local WHERE key = 'cpu_count'"
	var cpuCount string
	if err := db.session.Query(cpuQuery).Scan(&cpuCount); err == nil {
		info["cpu_count"] = cpuCount
	}

	// Get memory information
	memQuery := "SELECT value FROM system.scylla_local WHERE key = 'memory'"
	var memory string
	if err := db.session.Query(memQuery).Scan(&memory); err == nil {
		info["memory"] = memory
	}

	// Get shards information
	shardsQuery := "SELECT value FROM system.scylla_local WHERE key = 'shard_count'"
	var shardCount string
	if err := db.session.Query(shardsQuery).Scan(&shardCount); err == nil {
		info["shard_count"] = shardCount
	}

	return info, nil
}

// Stats returns database statistics
func (db *scyllaDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}

	stats["driver"] = db.driver
	stats["connected"] = db.connected
	stats["keyspace"] = db.keyspace
	stats["hosts"] = db.hosts
	stats["port"] = db.port
	stats["timeout"] = db.timeout

	return stats
}

// init function to register the ScyllaDB constructor
func init() {
	NewScyllaDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newScyllaDatabase(config)
	}
}
