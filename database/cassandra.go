package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"
)

// cassandraDatabase implements NoSQLDatabase interface for Cassandra
type cassandraDatabase struct {
	*baseNoSQLDatabase
	session  *gocql.Session
	cluster  *gocql.ClusterConfig
	keyspace string
	hosts    []string
}

// cassandraCollection implements Collection interface for Cassandra
type cassandraCollection struct {
	*baseCollection
	session  *gocql.Session
	keyspace string
	table    string
}

// cassandraCursor implements Cursor interface for Cassandra
type cassandraCursor struct {
	*baseCursor
	iter   *gocql.Iter
	ctx    context.Context
	closed bool
	mu     sync.RWMutex
}

// NewCassandraDatabase creates a new Cassandra database connection
func newCassandraDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &cassandraDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "cassandra",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		keyspace: config.Database,
		hosts:    []string{},
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}

	return db, nil
}

// Connect establishes Cassandra connection
func (db *cassandraDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Parse hosts
	db.hosts = db.parseHosts()

	// Create cluster configuration
	cluster := gocql.NewCluster(db.hosts...)

	// Configure authentication
	if db.config.Username != "" && db.config.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: db.config.Username,
			Password: db.config.Password,
		}
	}

	// Configure consistency
	cluster.Consistency = gocql.Quorum
	cluster.DefaultTimestamp = true

	// Configure connection pool
	if db.config.MaxPoolSize > 0 {
		cluster.NumConns = db.config.MaxPoolSize
	} else {
		cluster.NumConns = 2 // default
	}

	// Configure timeouts
	if db.config.ConnectTimeout > 0 {
		cluster.ConnectTimeout = db.config.ConnectTimeout
	} else {
		cluster.ConnectTimeout = 10 * time.Second
	}

	if db.config.SocketTimeout > 0 {
		cluster.Timeout = db.config.SocketTimeout
	} else {
		cluster.Timeout = 10 * time.Second
	}

	// Configure retry policy
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		Min:        100 * time.Millisecond,
		Max:        10 * time.Second,
		NumRetries: 3,
	}

	// Configure reconnection policy
	cluster.ReconnectInterval = 1 * time.Second

	// Protocol version
	cluster.ProtoVersion = 4

	db.cluster = cluster

	// Create session
	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to create Cassandra session: %w", err)
	}

	db.session = session

	// Create keyspace if it doesn't exist
	if db.keyspace != "" {
		if err := db.ensureKeyspace(ctx); err != nil {
			return fmt.Errorf("failed to ensure keyspace: %w", err)
		}
	}

	db.connected = true
	return nil
}

// parseHosts parses host configuration
func (db *cassandraDatabase) parseHosts() []string {
	if db.config.URL != "" {
		// Parse URL format: cassandra://host1:port1,host2:port2/keyspace
		url := strings.TrimPrefix(db.config.URL, "cassandra://")
		parts := strings.Split(url, "/")
		if len(parts) > 0 {
			hosts := strings.Split(parts[0], ",")
			return hosts
		}
	}

	// Use host and port
	host := db.config.Host
	if host == "" {
		host = "localhost"
	}

	port := db.config.Port
	if port == 0 {
		port = 9042 // Cassandra default port
	}

	return []string{fmt.Sprintf("%s:%d", host, port)}
}

// ensureKeyspace creates the keyspace if it doesn't exist
func (db *cassandraDatabase) ensureKeyspace(ctx context.Context) error {
	// Check if keyspace exists
	query := "SELECT keyspace_name FROM system_schema.keyspaces WHERE keyspace_name = ?"
	var keyspaceName string
	err := db.session.Query(query, db.keyspace).Scan(&keyspaceName)

	if err != nil {
		if err == gocql.ErrNotFound {
			// Create keyspace
			createQuery := fmt.Sprintf(`
				CREATE KEYSPACE IF NOT EXISTS %s
				WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}
			`, db.keyspace)

			if err := db.session.Query(createQuery).Exec(); err != nil {
				return fmt.Errorf("failed to create keyspace: %w", err)
			}
		} else {
			return err
		}
	}

	// Use keyspace
	if err := db.session.Query(fmt.Sprintf("USE %s", db.keyspace)).Exec(); err != nil {
		return fmt.Errorf("failed to use keyspace: %w", err)
	}

	return nil
}

// Close closes Cassandra connection
func (db *cassandraDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.session != nil {
		db.session.Close()
		db.connected = false
	}

	return nil
}

// Ping tests Cassandra connection
func (db *cassandraDatabase) Ping(ctx context.Context) error {
	if db.session == nil {
		return fmt.Errorf("cassandra not connected")
	}

	// Simple query to test connection
	query := "SELECT release_version FROM system.local"
	var version string
	err := db.session.Query(query).Scan(&version)
	return err
}

// Collection returns a Cassandra collection (table)
func (db *cassandraDatabase) Collection(name string) Collection {
	return &cassandraCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		session:  db.session,
		keyspace: db.keyspace,
		table:    name,
	}
}

// CreateCollection creates a new collection (table)
func (db *cassandraDatabase) CreateCollection(ctx context.Context, name string) error {
	if db.session == nil {
		return fmt.Errorf("cassandra not connected")
	}

	// Create a basic table structure
	// In a real implementation, you'd want to specify the schema
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.%s (
			id UUID PRIMARY KEY,
			data TEXT,
			created_at TIMESTAMP
		)
	`, db.keyspace, name)

	return db.session.Query(query).Exec()
}

// DropCollection drops a collection (table)
func (db *cassandraDatabase) DropCollection(ctx context.Context, name string) error {
	if db.session == nil {
		return fmt.Errorf("cassandra not connected")
	}

	query := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", db.keyspace, name)
	return db.session.Query(query).Exec()
}

// ListCollections lists all collections (tables)
func (db *cassandraDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.session == nil {
		return nil, fmt.Errorf("cassandra not connected")
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
		return nil, err
	}

	return tables, nil
}

// Drop drops the entire database (keyspace)
func (db *cassandraDatabase) Drop(ctx context.Context) error {
	if db.session == nil {
		return fmt.Errorf("cassandra not connected")
	}

	query := fmt.Sprintf("DROP KEYSPACE IF EXISTS %s", db.keyspace)
	return db.session.Query(query).Exec()
}

// Cassandra Collection implementation

// FindOne finds a single document
func (c *cassandraCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// Build CQL query based on filter
	query := fmt.Sprintf("SELECT * FROM %s.%s LIMIT 1", c.keyspace, c.table)

	// Execute query
	var id gocql.UUID
	var data string
	var createdAt time.Time

	err := c.session.Query(query).Scan(&id, &data, &createdAt)
	if err != nil {
		if err == gocql.ErrNotFound {
			return nil, fmt.Errorf("no documents found")
		}
		return nil, err
	}

	// Return as map
	result := map[string]interface{}{
		"id":         id.String(),
		"data":       data,
		"created_at": createdAt,
	}

	return result, nil
}

// Find finds multiple documents
func (c *cassandraCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// Build CQL query based on filter
	query := fmt.Sprintf("SELECT * FROM %s.%s", c.keyspace, c.table)

	// Execute query
	iter := c.session.Query(query).Iter()

	return &cassandraCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		iter:   iter,
		ctx:    ctx,
		closed: false,
	}, nil
}

// Insert inserts a single document
func (c *cassandraCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// Generate UUID for the document
	id := gocql.TimeUUID()

	// Convert document to string (simplified)
	data := fmt.Sprintf("%v", document)

	// Insert query
	query := fmt.Sprintf(`
		INSERT INTO %s.%s (id, data, created_at)
		VALUES (?, ?, ?)
	`, c.keyspace, c.table)

	err := c.session.Query(query, id, data, time.Now()).Exec()
	if err != nil {
		return nil, err
	}

	return id.String(), nil
}

// InsertMany inserts multiple documents
func (c *cassandraCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// Use batch for better performance
	batch := c.session.NewBatch(gocql.LoggedBatch)

	var ids []interface{}
	for _, document := range documents {
		id := gocql.TimeUUID()
		data := fmt.Sprintf("%v", document)

		query := fmt.Sprintf(`
			INSERT INTO %s.%s (id, data, created_at)
			VALUES (?, ?, ?)
		`, c.keyspace, c.table)

		batch.Query(query, id, data, time.Now())
		ids = append(ids, id.String())
	}

	err := c.session.ExecuteBatch(batch)
	if err != nil {
		return nil, err
	}

	return ids, nil
}

// Update updates multiple documents
func (c *cassandraCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	if c.session == nil {
		return 0, fmt.Errorf("session not initialized")
	}

	// Cassandra doesn't support updating without knowing the partition key
	// This is a simplified implementation
	return 0, fmt.Errorf("update not supported without partition key")
}

// UpdateOne updates a single document
func (c *cassandraCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	// Cassandra doesn't support updating without knowing the partition key
	// This is a simplified implementation
	return fmt.Errorf("update not supported without partition key")
}

// Delete deletes multiple documents
func (c *cassandraCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	if c.session == nil {
		return 0, fmt.Errorf("session not initialized")
	}

	// Cassandra doesn't support deleting without knowing the partition key
	// This is a simplified implementation
	return 0, fmt.Errorf("delete not supported without partition key")
}

// DeleteOne deletes a single document
func (c *cassandraCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	// Cassandra doesn't support deleting without knowing the partition key
	// This is a simplified implementation
	return fmt.Errorf("delete not supported without partition key")
}

// Aggregate performs aggregation
func (c *cassandraCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if c.session == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// Cassandra has limited aggregation support
	return nil, fmt.Errorf("aggregation not fully supported")
}

// Count counts documents
func (c *cassandraCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	if c.session == nil {
		return 0, fmt.Errorf("session not initialized")
	}

	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", c.keyspace, c.table)

	var count int64
	err := c.session.Query(query).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// CreateIndex creates an index
func (c *cassandraCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	// Convert keys to column names
	var columns []string
	if keyMap, ok := keys.(map[string]interface{}); ok {
		for key := range keyMap {
			columns = append(columns, key)
		}
	}

	if len(columns) == 0 {
		return fmt.Errorf("no columns specified for index")
	}

	// Create index
	indexName := fmt.Sprintf("%s_%s_idx", c.table, strings.Join(columns, "_"))
	if opts != nil && opts.Name != nil {
		indexName = *opts.Name
	}

	query := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s.%s (%s)",
		indexName, c.keyspace, c.table, strings.Join(columns, ", "))

	return c.session.Query(query).Exec()
}

// DropIndex drops an index
func (c *cassandraCollection) DropIndex(ctx context.Context, name string) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	query := fmt.Sprintf("DROP INDEX IF EXISTS %s.%s", c.keyspace, name)
	return c.session.Query(query).Exec()
}

// ListIndexes lists all indexes
func (c *cassandraCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
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
			Name: indexName,
			Keys: make(map[string]interface{}),
		}

		// Extract index information from options
		if target, ok := options["target"]; ok {
			index.Keys[target] = 1
		}

		indexes = append(indexes, index)
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return indexes, nil
}

// BulkWrite performs bulk operations
func (c *cassandraCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if c.session == nil {
		return fmt.Errorf("session not initialized")
	}

	// Use batch for bulk operations
	batch := c.session.NewBatch(gocql.LoggedBatch)

	for _, op := range operations {
		// Process each operation based on its type
		// This is a simplified implementation
		if doc, ok := op.(map[string]interface{}); ok {
			id := gocql.TimeUUID()
			data := fmt.Sprintf("%v", doc)

			query := fmt.Sprintf(`
				INSERT INTO %s.%s (id, data, created_at)
				VALUES (?, ?, ?)
			`, c.keyspace, c.table)

			batch.Query(query, id, data, time.Now())
		}
	}

	return c.session.ExecuteBatch(batch)
}

// Cassandra Cursor implementation

// Next moves to the next document
func (c *cassandraCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.iter == nil {
		return false
	}

	// Check if there are more results
	var id gocql.UUID
	var data string
	var createdAt time.Time

	if c.iter.Scan(&id, &data, &createdAt) {
		// Store the current row data
		rowData := map[string]interface{}{
			"id":         id.String(),
			"data":       data,
			"created_at": createdAt,
		}
		c.data = append(c.data, rowData)
		c.index++
		return true
	}

	return false
}

// Decode decodes the current document
func (c *cassandraCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	if c.index < 0 || c.index >= len(c.data) {
		return fmt.Errorf("no current document")
	}

	// This is a simplified decode - in a real implementation, you'd properly handle dest
	return nil
}

// All decodes all documents
func (c *cassandraCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	// Read all remaining documents
	for c.Next(ctx) {
		// Documents are stored in c.data
	}

	// This is a simplified implementation
	return nil
}

// Close closes the cursor
func (c *cassandraCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.iter != nil {
		err := c.iter.Close()
		c.closed = true
		return err
	}

	return nil
}

// Current returns the current document
func (c *cassandraCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.index < 0 || c.index >= len(c.data) {
		return nil
	}

	return c.data[c.index]
}

// Err returns the last error
func (c *cassandraCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.err
}

// Cassandra-specific utility functions

// GetSession returns the underlying Cassandra session
func (db *cassandraDatabase) GetSession() *gocql.Session {
	return db.session
}

// GetClusterConfig returns the cluster configuration
func (db *cassandraDatabase) GetClusterConfig() *gocql.ClusterConfig {
	return db.cluster
}

// ExecuteCQL executes a CQL query
func (db *cassandraDatabase) ExecuteCQL(ctx context.Context, query string, values ...interface{}) error {
	if db.session == nil {
		return fmt.Errorf("cassandra not connected")
	}

	return db.session.Query(query, values...).Exec()
}

// QueryCQL executes a CQL query and returns an iterator
func (db *cassandraDatabase) QueryCQL(ctx context.Context, query string, values ...interface{}) *gocql.Iter {
	if db.session == nil {
		return nil
	}

	return db.session.Query(query, values...).Iter()
}

// GetClusterMetadata returns cluster metadata (fixed implementation)
func (db *cassandraDatabase) GetClusterMetadata() (map[string]interface{}, error) {
	if db.session == nil {
		return nil, fmt.Errorf("cassandra not connected")
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

	return metadata, nil
}

// GetKeyspaceMetadata returns keyspace metadata (fixed implementation)
func (db *cassandraDatabase) GetKeyspaceMetadata() (*gocql.KeyspaceMetadata, error) {
	if db.session == nil {
		return nil, fmt.Errorf("cassandra not connected")
	}

	// Use gocql's built-in KeyspaceMetadata method
	metadata, err := db.session.KeyspaceMetadata(db.keyspace)
	if err != nil {
		return nil, fmt.Errorf("failed to get keyspace metadata for %s: %w", db.keyspace, err)
	}

	return metadata, nil
}

// GetConnectionInfo returns Cassandra connection information (updated implementation)
func (db *cassandraDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.session == nil {
		return nil, fmt.Errorf("cassandra not connected")
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

	// Add current connection information
	info["keyspace"] = db.keyspace
	info["hosts"] = db.hosts

	// Add cluster configuration information
	if db.cluster != nil {
		info["protocol_version"] = db.cluster.ProtoVersion
		info["consistency"] = db.cluster.Consistency.String()
		info["num_conns"] = db.cluster.NumConns
		info["connect_timeout"] = db.cluster.ConnectTimeout
		info["timeout"] = db.cluster.Timeout
	}

	return info, nil
}

// CreateKeyspace creates a new keyspace
func (db *cassandraDatabase) CreateKeyspace(ctx context.Context, keyspace string, replication map[string]interface{}) error {
	if db.session == nil {
		return fmt.Errorf("cassandra not connected")
	}

	// Build replication string
	var replParts []string
	for key, value := range replication {
		replParts = append(replParts, fmt.Sprintf("'%s': %v", key, value))
	}

	query := fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {%s}",
		keyspace, strings.Join(replParts, ", "))

	return db.session.Query(query).Exec()
}

// DropKeyspace drops a keyspace
func (db *cassandraDatabase) DropKeyspace(ctx context.Context, keyspace string) error {
	if db.session == nil {
		return fmt.Errorf("cassandra not connected")
	}

	query := fmt.Sprintf("DROP KEYSPACE IF EXISTS %s", keyspace)
	return db.session.Query(query).Exec()
}

// UseKeyspace switches to a different keyspace
func (db *cassandraDatabase) UseKeyspace(ctx context.Context, keyspace string) error {
	if db.session == nil {
		return fmt.Errorf("cassandra not connected")
	}

	query := fmt.Sprintf("USE %s", keyspace)
	err := db.session.Query(query).Exec()
	if err != nil {
		return err
	}

	db.keyspace = keyspace
	return nil
}

// Stats returns Cassandra statistics
func (db *cassandraDatabase) Stats() map[string]interface{} {
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

	return stats
}

// init function to override the Cassandra constructor
func init() {
	NewCassandraDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newCassandraDatabase(config)
	}
}
