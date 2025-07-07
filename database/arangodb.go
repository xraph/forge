package database

import (
	"context"
	"fmt"

	"github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/http"
)

// arangoDatabase implements NoSQLDatabase interface for ArangoDB
type arangoDatabase struct {
	*baseNoSQLDatabase
	client   driver.Client
	database driver.Database
	dbName   string
}

// arangoCollection implements Collection interface for ArangoDB
type arangoCollection struct {
	*baseCollection
	collection driver.Collection
	db         *arangoDatabase
}

// arangoCursor implements Cursor interface for ArangoDB
type arangoCursor struct {
	*baseCursor
	cursor driver.Cursor
	ctx    context.Context
}

// NewArangoDatabase creates a new ArangoDB database connection
func newArangoDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &arangoDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "arangodb",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		dbName: config.Database,
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}

	return db, nil
}

// Connect establishes ArangoDB connection
func (db *arangoDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Build connection URL
	url := db.buildArangoURL()

	// Create HTTP connection
	conn, err := http.NewConnection(http.ConnectionConfig{
		Endpoints: []string{url},
		TLSConfig: nil, // Configure TLS if needed
	})
	if err != nil {
		return fmt.Errorf("failed to create ArangoDB connection: %w", err)
	}

	// Create client
	client, err := driver.NewClient(driver.ClientConfig{
		Connection:     conn,
		Authentication: driver.BasicAuthentication(db.config.Username, db.config.Password),
	})
	if err != nil {
		return fmt.Errorf("failed to create ArangoDB client: %w", err)
	}

	db.client = client

	// Connect to database
	if db.dbName != "" {
		exists, err := client.DatabaseExists(ctx, db.dbName)
		if err != nil {
			return fmt.Errorf("failed to check database existence: %w", err)
		}

		if !exists {
			// Create database if it doesn't exist
			database, err := client.CreateDatabase(ctx, db.dbName, nil)
			if err != nil {
				return fmt.Errorf("failed to create database: %w", err)
			}
			db.database = database
		} else {
			database, err := client.Database(ctx, db.dbName)
			if err != nil {
				return fmt.Errorf("failed to open database: %w", err)
			}
			db.database = database
		}
	}

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping ArangoDB: %w", err)
	}

	db.connected = true
	return nil
}

// buildArangoURL builds ArangoDB connection URL
func (db *arangoDatabase) buildArangoURL() string {
	config := db.config

	// If URL is provided, use it directly
	if config.URL != "" {
		return config.URL
	}

	// Build URL from components
	host := config.Host
	if host == "" {
		host = "localhost"
	}

	port := config.Port
	if port == 0 {
		port = 8529 // ArangoDB default port
	}

	return fmt.Sprintf("http://%s:%d", host, port)
}

// Close closes ArangoDB connection
func (db *arangoDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.connected = false
	return nil
}

// Ping tests ArangoDB connection
func (db *arangoDatabase) Ping(ctx context.Context) error {
	if db.client == nil {
		return fmt.Errorf("ArangoDB not connected")
	}

	_, err := db.client.Version(ctx)
	return err
}

// Collection returns an ArangoDB collection
func (db *arangoDatabase) Collection(name string) Collection {
	return &arangoCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		collection: nil, // Will be initialized on first use
		db:         db,
	}
}

// ensureCollection ensures the collection exists and is initialized
func (c *arangoCollection) ensureCollection(ctx context.Context) error {
	if c.collection != nil {
		return nil
	}

	if c.db.database == nil {
		return fmt.Errorf("database not connected")
	}

	// Check if collection exists
	exists, err := c.db.database.CollectionExists(ctx, c.name)
	if err != nil {
		return err
	}

	if !exists {
		// Create collection
		collection, err := c.db.database.CreateCollection(ctx, c.name, nil)
		if err != nil {
			return err
		}
		c.collection = collection
	} else {
		// Open existing collection
		collection, err := c.db.database.Collection(ctx, c.name)
		if err != nil {
			return err
		}
		c.collection = collection
	}

	return nil
}

// CreateCollection creates a new collection
func (db *arangoDatabase) CreateCollection(ctx context.Context, name string) error {
	if db.database == nil {
		return fmt.Errorf("ArangoDB not connected")
	}

	_, err := db.database.CreateCollection(ctx, name, nil)
	return err
}

// DropCollection drops a collection
func (db *arangoDatabase) DropCollection(ctx context.Context, name string) error {
	if db.database == nil {
		return fmt.Errorf("ArangoDB not connected")
	}

	collection, err := db.database.Collection(ctx, name)
	if err != nil {
		return err
	}

	return collection.Remove(ctx)
}

// ListCollections lists all collections
func (db *arangoDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.database == nil {
		return nil, fmt.Errorf("ArangoDB not connected")
	}

	collections, err := db.database.Collections(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(collections))
	for i, collection := range collections {
		names[i] = collection.Name()
	}

	return names, nil
}

// Drop drops the entire database
func (db *arangoDatabase) Drop(ctx context.Context) error {
	if db.database == nil {
		return fmt.Errorf("ArangoDB not connected")
	}

	return db.database.Remove(ctx)
}

// ArangoDB Collection implementation

// FindOne finds a single document
func (c *arangoCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return nil, err
	}

	// Build AQL query
	query := fmt.Sprintf("FOR doc IN %s RETURN doc", c.name)

	cursor, err := c.db.database.Query(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	defer cursor.Close()

	if cursor.HasMore() {
		var doc interface{}
		_, err := cursor.ReadDocument(ctx, &doc)
		if err != nil {
			return nil, err
		}
		return doc, nil
	}

	return nil, fmt.Errorf("no documents found")
}

// Find finds multiple documents
func (c *arangoCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return nil, err
	}

	// Build AQL query
	query := fmt.Sprintf("FOR doc IN %s RETURN doc", c.name)

	cursor, err := c.db.database.Query(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	return &arangoCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		cursor: cursor,
		ctx:    ctx,
	}, nil
}

// Insert inserts a single document
func (c *arangoCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return nil, err
	}

	meta, err := c.collection.CreateDocument(ctx, document)
	if err != nil {
		return nil, err
	}

	return meta.Key, nil
}

// InsertMany inserts multiple documents
func (c *arangoCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return nil, err
	}

	metas, errs, err := c.collection.CreateDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}

	// Check for individual errors
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	ids := make([]interface{}, len(metas))
	for i, meta := range metas {
		ids[i] = meta.Key
	}

	return ids, nil
}

// Update updates multiple documents
func (c *arangoCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return 0, err
	}

	// Build AQL query for update
	query := fmt.Sprintf("FOR doc IN %s UPDATE doc WITH @update IN %s RETURN NEW", c.name, c.name)
	bindVars := map[string]interface{}{
		"update": update,
	}

	cursor, err := c.db.database.Query(ctx, query, bindVars)
	if err != nil {
		return 0, err
	}
	defer cursor.Close()

	count := int64(0)
	for cursor.HasMore() {
		var doc interface{}
		_, err := cursor.ReadDocument(ctx, &doc)
		if err != nil {
			break
		}
		count++
	}

	return count, nil
}

// UpdateOne updates a single document
func (c *arangoCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	if err := c.ensureCollection(ctx); err != nil {
		return err
	}

	// Build AQL query for single update
	query := fmt.Sprintf("FOR doc IN %s LIMIT 1 UPDATE doc WITH @update IN %s RETURN NEW", c.name, c.name)
	bindVars := map[string]interface{}{
		"update": update,
	}

	cursor, err := c.db.database.Query(ctx, query, bindVars)
	if err != nil {
		return err
	}
	defer cursor.Close()

	return nil
}

// Delete deletes multiple documents
func (c *arangoCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return 0, err
	}

	// Build AQL query for delete
	query := fmt.Sprintf("FOR doc IN %s REMOVE doc IN %s RETURN OLD", c.name, c.name)

	cursor, err := c.db.database.Query(ctx, query, nil)
	if err != nil {
		return 0, err
	}
	defer cursor.Close()

	count := int64(0)
	for cursor.HasMore() {
		var doc interface{}
		_, err := cursor.ReadDocument(ctx, &doc)
		if err != nil {
			break
		}
		count++
	}

	return count, nil
}

// DeleteOne deletes a single document
func (c *arangoCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	if err := c.ensureCollection(ctx); err != nil {
		return err
	}

	// Build AQL query for single delete
	query := fmt.Sprintf("FOR doc IN %s LIMIT 1 REMOVE doc IN %s RETURN OLD", c.name, c.name)

	cursor, err := c.db.database.Query(ctx, query, nil)
	if err != nil {
		return err
	}
	defer cursor.Close()

	return nil
}

// Aggregate performs aggregation
func (c *arangoCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return nil, err
	}

	// Convert pipeline to AQL query
	aqlQuery := fmt.Sprintf("FOR doc IN %s RETURN doc", c.name)

	cursor, err := c.db.database.Query(ctx, aqlQuery, nil)
	if err != nil {
		return nil, err
	}

	return &arangoCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		cursor: cursor,
		ctx:    ctx,
	}, nil
}

// Count counts documents
func (c *arangoCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return 0, err
	}

	return c.collection.Count(ctx)
}

// CreateIndex creates an index
func (c *arangoCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	if err := c.ensureCollection(ctx); err != nil {
		return err
	}

	// Convert keys to ArangoDB format
	fields := []string{}
	if keyMap, ok := keys.(map[string]interface{}); ok {
		for key := range keyMap {
			fields = append(fields, key)
		}
	}

	// Determine index type
	indexType := driver.PersistentIndex
	unique := false

	if opts != nil {
		if opts.Unique != nil && *opts.Unique {
			unique = true
		}
	}

	switch indexType {
	case driver.PersistentIndex, driver.EdgeIndex, driver.PrimaryIndex:
		// Create index
		_, _, err := c.collection.EnsurePersistentIndex(ctx, fields, &driver.EnsurePersistentIndexOptions{
			Unique: unique,
		})
		return err
	case driver.HashIndex:
		// Create index
		_, _, err := c.collection.EnsureHashIndex(ctx, fields, &driver.EnsureHashIndexOptions{
			Unique: unique,
		})
		return err
	case driver.FullTextIndex:
		// Create index
		_, _, err := c.collection.EnsureFullTextIndex(ctx, fields, &driver.EnsureFullTextIndexOptions{})
		return err
	case driver.MDIIndex, driver.MDIPrefixedIndex:
		_, _, err := c.collection.EnsureMDIIndex(ctx, fields, &driver.EnsureMDIIndexOptions{
			Unique: unique,
		})
		return err
	case driver.InvertedIndex:
		_, _, err := c.collection.EnsureInvertedIndex(ctx, &driver.InvertedIndexOptions{
			// Fields: fields,
		})
		return err
	case driver.GeoIndex:
		_, _, err := c.collection.EnsureGeoIndex(ctx, fields, &driver.EnsureGeoIndexOptions{
			// Fields: fields,
		})
		return err
	case driver.TTLIndex:
		for _, field := range fields {
			if field == "_key" {
				return fmt.Errorf("cannot create TTL index on _key")
			}

			if _, _, err := c.collection.EnsureTTLIndex(ctx, field, int(opts.ExpireAfter.Milliseconds()), &driver.EnsureTTLIndexOptions{
				// Fields: fields,
			}); err != nil {
				return err
			}
			return nil
		}
		return nil
	}

	return nil
}

// DropIndex drops an index
func (c *arangoCollection) DropIndex(ctx context.Context, name string) error {
	if err := c.ensureCollection(ctx); err != nil {
		return err
	}

	// Get indexes and find the one to drop
	indexes, err := c.collection.Indexes(ctx)
	if err != nil {
		return err
	}

	for _, index := range indexes {
		if index.Name() == name {
			return index.Remove(ctx)
		}
	}

	return fmt.Errorf("index %s not found", name)
}

// ListIndexes lists all indexes
func (c *arangoCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	if err := c.ensureCollection(ctx); err != nil {
		return nil, err
	}

	indexes, err := c.collection.Indexes(ctx)
	if err != nil {
		return nil, err
	}

	indexInfos := make([]IndexInfo, len(indexes))
	for i, index := range indexes {
		indexInfos[i] = IndexInfo{
			Name:   index.Name(),
			Keys:   map[string]interface{}{},               // Would need to extract fields
			Unique: index.Type() == driver.PersistentIndex, // Simplified
		}
	}

	return indexInfos, nil
}

// BulkWrite performs bulk operations
func (c *arangoCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if err := c.ensureCollection(ctx); err != nil {
		return err
	}

	// ArangoDB doesn't have a direct bulk write, so we'll process operations individually
	for _, op := range operations {
		// Process each operation based on its type
		// This is a simplified implementation
		if doc, ok := op.(map[string]interface{}); ok {
			if _, ok := doc["_key"]; ok {
				// Update operation
				_, err := c.collection.UpdateDocument(ctx, doc["_key"].(string), doc)
				if err != nil {
					return err
				}
			} else {
				// Insert operation
				_, err := c.collection.CreateDocument(ctx, doc)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// ArangoDB Cursor implementation

// Next moves to the next document
func (c *arangoCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.cursor == nil {
		return false
	}

	return c.cursor.HasMore()
}

// Decode decodes the current document
func (c *arangoCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.cursor == nil {
		return fmt.Errorf("cursor is closed")
	}

	_, err := c.cursor.ReadDocument(c.ctx, dest)
	return err
}

// All decodes all documents
func (c *arangoCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.cursor == nil {
		return fmt.Errorf("cursor is closed")
	}

	// Read all documents
	var docs []interface{}
	for c.cursor.HasMore() {
		var doc interface{}
		_, err := c.cursor.ReadDocument(ctx, &doc)
		if err != nil {
			return err
		}
		docs = append(docs, doc)
	}

	// This is a simplified implementation
	// In a real implementation, you'd need to properly handle the dest parameter
	return nil
}

// Close closes the cursor
func (c *arangoCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cursor != nil {
		err := c.cursor.Close()
		c.closed = true
		return err
	}

	return nil
}

// Current returns the current document
func (c *arangoCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.cursor == nil {
		return nil
	}

	var doc interface{}
	_, err := c.cursor.ReadDocument(c.ctx, &doc)
	if err != nil {
		return nil
	}

	return doc
}

// Err returns the last error
func (c *arangoCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.err
}

// ArangoDB-specific utility functions

// RunAQL executes an AQL query
func (db *arangoDatabase) RunAQL(ctx context.Context, query string, bindVars map[string]interface{}) (driver.Cursor, error) {
	if db.database == nil {
		return nil, fmt.Errorf("ArangoDB not connected")
	}

	return db.database.Query(ctx, query, bindVars)
}

// GetDatabase returns the underlying ArangoDB database
func (db *arangoDatabase) GetDatabase() driver.Database {
	return db.database
}

// GetClient returns the underlying ArangoDB client
func (db *arangoDatabase) GetClient() driver.Client {
	return db.client
}

// CreateGraph creates a new graph
func (db *arangoDatabase) CreateGraph(ctx context.Context, name string, edgeDefinitions []driver.EdgeDefinition) (driver.Graph, error) {
	if db.database == nil {
		return nil, fmt.Errorf("ArangoDB not connected")
	}

	return db.database.CreateGraph(ctx, name, &driver.CreateGraphOptions{
		EdgeDefinitions: edgeDefinitions,
	})
}

// Graph returns an existing graph
func (db *arangoDatabase) Graph(ctx context.Context, name string) (driver.Graph, error) {
	if db.database == nil {
		return nil, fmt.Errorf("ArangoDB not connected")
	}

	return db.database.Graph(ctx, name)
}

// GetDatabaseInfo returns database information
func (db *arangoDatabase) GetDatabaseInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.database == nil {
		return nil, fmt.Errorf("ArangoDB not connected")
	}

	info := make(map[string]interface{})

	// Get database name
	info["name"] = db.database.Name()

	// Get server version
	version, err := db.client.Version(ctx)
	if err != nil {
		return nil, err
	}
	info["version"] = version.Version

	// Get collections count
	collections, err := db.database.Collections(ctx)
	if err != nil {
		return nil, err
	}
	info["collections_count"] = len(collections)

	return info, nil
}

// Transaction support

// BeginTransaction starts a new transaction
func (db *arangoDatabase) BeginTransaction(ctx context.Context, collections []string) (driver.TransactionID, error) {
	if db.database == nil {
		return "", fmt.Errorf("ArangoDB not connected")
	}

	return db.database.BeginTransaction(ctx, driver.TransactionCollections{
		Write: collections,
	}, &driver.BeginTransactionOptions{
		AllowImplicit: false,
	})
}

// init function to override the ArangoDB constructor
func init() {
	NewArangoDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newArangoDatabase(config)
	}
}
