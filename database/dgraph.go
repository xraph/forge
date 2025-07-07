package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/dgo/v210"
	"github.com/dgraph-io/dgo/v210/protos/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// dgraphDatabase implements NoSQLDatabase interface for Dgraph
type dgraphDatabase struct {
	*baseNoSQLDatabase
	client *dgo.Dgraph
	conns  []*grpc.ClientConn
	alpha  []string
}

// dgraphCollection implements Collection interface for Dgraph
type dgraphCollection struct {
	*baseCollection
	db        *dgraphDatabase
	predicate string
}

// dgraphCursor implements Cursor interface for Dgraph
type dgraphCursor struct {
	*baseCursor
	results []interface{}
	current int
	mu      sync.RWMutex
}

// NewDgraphDatabase creates a new Dgraph database connection
func newDgraphDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &dgraphDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "dgraph",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		alpha: []string{},
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}

	return db, nil
}

// Connect establishes Dgraph connection
func (db *dgraphDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Parse alpha addresses
	db.alpha = db.parseAlphaAddresses()

	// Create gRPC connections
	var conns []*grpc.ClientConn
	for _, addr := range db.alpha {
		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			// Close any existing connections
			for _, c := range conns {
				c.Close()
			}
			return fmt.Errorf("failed to connect to Dgraph alpha %s: %w", addr, err)
		}
		conns = append(conns, conn)
	}

	db.conns = conns

	// Create Dgraph client
	db.client = dgo.NewDgraphClient(api.NewDgraphClient(conns[0]))

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Dgraph: %w", err)
	}

	// Set up schema if needed
	if err := db.setupSchema(ctx); err != nil {
		return fmt.Errorf("failed to setup schema: %w", err)
	}

	db.connected = true
	return nil
}

// parseAlphaAddresses parses Dgraph alpha addresses
func (db *dgraphDatabase) parseAlphaAddresses() []string {
	if db.config.URL != "" {
		// Parse URL format: dgraph://alpha1:port1,alpha2:port2
		url := strings.TrimPrefix(db.config.URL, "dgraph://")
		return strings.Split(url, ",")
	}

	// Use host and port
	host := db.config.Host
	if host == "" {
		host = "localhost"
	}

	port := db.config.Port
	if port == 0 {
		port = 9080 // Dgraph alpha default port
	}

	return []string{fmt.Sprintf("%s:%d", host, port)}
}

// setupSchema sets up basic schema for document storage
func (db *dgraphDatabase) setupSchema(ctx context.Context) error {
	// Define basic schema for document storage
	schema := `
		type Document {
			id: string
			data: string
			created_at: datetime
			updated_at: datetime
		}
		
		id: string @index(hash) .
		data: string .
		created_at: datetime @index(hour) .
		updated_at: datetime @index(hour) .
	`

	op := &api.Operation{Schema: schema}
	return db.client.Alter(ctx, op)
}

// Close closes Dgraph connection
func (db *dgraphDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Close all gRPC connections
	for _, conn := range db.conns {
		if err := conn.Close(); err != nil {
			return err
		}
	}

	db.connected = false
	return nil
}

// Ping tests Dgraph connection
func (db *dgraphDatabase) Ping(ctx context.Context) error {
	if db.client == nil {
		return fmt.Errorf("Dgraph not connected")
	}

	// Simple health check query
	query := `
		{
			health() {
				instance
				address
				status
				version
			}
		}
	`

	_, err := db.client.NewReadOnlyTxn().Query(ctx, query)
	return err
}

// Collection returns a Dgraph collection (predicate namespace)
func (db *dgraphDatabase) Collection(name string) Collection {
	return &dgraphCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		db:        db,
		predicate: name,
	}
}

// CreateCollection creates a new collection (predicate namespace)
func (db *dgraphDatabase) CreateCollection(ctx context.Context, name string) error {
	if db.client == nil {
		return fmt.Errorf("Dgraph not connected")
	}

	// In Dgraph, collections are implicitly created by adding predicates
	// We'll add a schema for the collection
	schema := fmt.Sprintf(`
		%s.id: string @index(hash) .
		%s.data: string .
		%s.created_at: datetime @index(hour) .
		%s.updated_at: datetime @index(hour) .
	`, name, name, name, name)

	op := &api.Operation{Schema: schema}
	return db.client.Alter(ctx, op)
}

// DropCollection drops a collection (removes all data with predicate)
func (db *dgraphDatabase) DropCollection(ctx context.Context, name string) error {
	if db.client == nil {
		return fmt.Errorf("Dgraph not connected")
	}

	// Drop all predicates for this collection
	predicates := []string{
		fmt.Sprintf("%s.id", name),
		fmt.Sprintf("%s.data", name),
		fmt.Sprintf("%s.created_at", name),
		fmt.Sprintf("%s.updated_at", name),
	}

	for _, pred := range predicates {
		op := &api.Operation{DropAttr: pred}
		if err := db.client.Alter(ctx, op); err != nil {
			return err
		}
	}

	return nil
}

// ListCollections lists all collections (predicate namespaces)
func (db *dgraphDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Dgraph not connected")
	}

	// Query schema to get predicates
	query := `
		schema {
			predicate
			type
		}
	`

	resp, err := db.client.NewReadOnlyTxn().Query(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Schema []struct {
			Predicate string `json:"predicate"`
			Type      string `json:"type"`
		} `json:"schema"`
	}

	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, err
	}

	// Extract collection names (predicates ending with .id)
	collections := make(map[string]bool)
	for _, pred := range result.Schema {
		if strings.HasSuffix(pred.Predicate, ".id") {
			collection := strings.TrimSuffix(pred.Predicate, ".id")
			collections[collection] = true
		}
	}

	var collectionList []string
	for collection := range collections {
		collectionList = append(collectionList, collection)
	}

	return collectionList, nil
}

// Drop drops the entire database
func (db *dgraphDatabase) Drop(ctx context.Context) error {
	if db.client == nil {
		return fmt.Errorf("Dgraph not connected")
	}

	// Drop all data
	op := &api.Operation{DropAll: true}
	return db.client.Alter(ctx, op)
}

// Dgraph Collection implementation

// FindOne finds a single document
func (c *dgraphCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	if c.db.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build DQL query
	query := fmt.Sprintf(`
		{
			result(func: has(%s.id)) @filter(has(%s.data)) {
				uid
				%s.id
				%s.data
				%s.created_at
				%s.updated_at
			}
		}
	`, c.predicate, c.predicate, c.predicate, c.predicate, c.predicate, c.predicate)

	resp, err := c.db.client.NewReadOnlyTxn().Query(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Result []map[string]interface{} `json:"result"`
	}

	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, err
	}

	if len(result.Result) == 0 {
		return nil, fmt.Errorf("no documents found")
	}

	return result.Result[0], nil
}

// Find finds multiple documents
func (c *dgraphCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if c.db.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build DQL query
	query := fmt.Sprintf(`
		{
			result(func: has(%s.id)) @filter(has(%s.data)) {
				uid
				%s.id
				%s.data
				%s.created_at
				%s.updated_at
			}
		}
	`, c.predicate, c.predicate, c.predicate, c.predicate, c.predicate, c.predicate)

	resp, err := c.db.client.NewReadOnlyTxn().Query(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Result []interface{} `json:"result"`
	}

	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, err
	}

	return &dgraphCursor{
		baseCursor: &baseCursor{
			data:   result.Result,
			index:  -1,
			err:    nil,
			closed: false,
		},
		results: result.Result,
		current: -1,
	}, nil
}

// Insert inserts a single document
func (c *dgraphCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if c.db.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Convert document to JSON
	docJSON, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}

	// Generate UID
	uid := fmt.Sprintf("_:%s_%d", c.predicate, time.Now().UnixNano())

	// Create mutation
	mutation := map[string]interface{}{
		"uid":                                     uid,
		fmt.Sprintf("%s.id", c.predicate):         uid,
		fmt.Sprintf("%s.data", c.predicate):       string(docJSON),
		fmt.Sprintf("%s.created_at", c.predicate): time.Now().Format(time.RFC3339),
		fmt.Sprintf("%s.updated_at", c.predicate): time.Now().Format(time.RFC3339),
	}

	mutationJSON, err := json.Marshal(mutation)
	if err != nil {
		return nil, err
	}

	mu := &api.Mutation{
		SetJson:   mutationJSON,
		CommitNow: true,
	}

	assigned, err := c.db.client.NewTxn().Mutate(ctx, mu)
	if err != nil {
		return nil, err
	}

	// Return the assigned UID
	for _, assignedUID := range assigned.Uids {
		return assignedUID, nil
	}

	return uid, nil
}

// InsertMany inserts multiple documents
func (c *dgraphCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if c.db.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	var mutations []map[string]interface{}
	var tempUIDs []string

	for i, document := range documents {
		// Convert document to JSON
		docJSON, err := json.Marshal(document)
		if err != nil {
			return nil, err
		}

		// Generate UID
		uid := fmt.Sprintf("_:%s_%d_%d", c.predicate, time.Now().UnixNano(), i)
		tempUIDs = append(tempUIDs, uid)

		// Create mutation
		mutation := map[string]interface{}{
			"uid":                                     uid,
			fmt.Sprintf("%s.id", c.predicate):         uid,
			fmt.Sprintf("%s.data", c.predicate):       string(docJSON),
			fmt.Sprintf("%s.created_at", c.predicate): time.Now().Format(time.RFC3339),
			fmt.Sprintf("%s.updated_at", c.predicate): time.Now().Format(time.RFC3339),
		}

		mutations = append(mutations, mutation)
	}

	mutationJSON, err := json.Marshal(mutations)
	if err != nil {
		return nil, err
	}

	mu := &api.Mutation{
		SetJson:   mutationJSON,
		CommitNow: true,
	}

	assigned, err := c.db.client.NewTxn().Mutate(ctx, mu)
	if err != nil {
		return nil, err
	}

	// Return the assigned UIDs
	var ids []interface{}
	for _, uid := range assigned.Uids {
		ids = append(ids, uid)
	}

	return ids, nil
}

// Update updates multiple documents
func (c *dgraphCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	if c.db.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	// Dgraph updates are complex - this is a simplified implementation
	// In practice, you'd need to query first, then update
	return 0, fmt.Errorf("update not implemented for Dgraph collection")
}

// UpdateOne updates a single document
func (c *dgraphCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	if c.db.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Dgraph updates are complex - this is a simplified implementation
	return fmt.Errorf("update one not implemented for Dgraph collection")
}

// Delete deletes multiple documents
func (c *dgraphCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	if c.db.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	// Dgraph deletes are complex - this is a simplified implementation
	return 0, fmt.Errorf("delete not implemented for Dgraph collection")
}

// DeleteOne deletes a single document
func (c *dgraphCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	if c.db.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Dgraph deletes are complex - this is a simplified implementation
	return fmt.Errorf("delete one not implemented for Dgraph collection")
}

// Aggregate performs aggregation
func (c *dgraphCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if c.db.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Dgraph has powerful aggregation capabilities
	// This is a simplified implementation
	query := fmt.Sprintf(`
		{
			result(func: has(%s.id)) @filter(has(%s.data)) {
				count(uid)
			}
		}
	`, c.predicate, c.predicate)

	resp, err := c.db.client.NewReadOnlyTxn().Query(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Result []interface{} `json:"result"`
	}

	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, err
	}

	return &dgraphCursor{
		baseCursor: &baseCursor{
			data:   result.Result,
			index:  -1,
			err:    nil,
			closed: false,
		},
		results: result.Result,
		current: -1,
	}, nil
}

// Count counts documents
func (c *dgraphCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	if c.db.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	query := fmt.Sprintf(`
		{
			result(func: has(%s.id)) @filter(has(%s.data)) {
				count(uid)
			}
		}
	`, c.predicate, c.predicate)

	resp, err := c.db.client.NewReadOnlyTxn().Query(ctx, query)
	if err != nil {
		return 0, err
	}

	var result struct {
		Result []struct {
			Count int64 `json:"count"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return 0, err
	}

	if len(result.Result) > 0 {
		return result.Result[0].Count, nil
	}

	return 0, nil
}

// CreateIndex creates an index
func (c *dgraphCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	if c.db.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Convert keys to predicate names
	var predicates []string
	if keyMap, ok := keys.(map[string]interface{}); ok {
		for key := range keyMap {
			predicates = append(predicates, fmt.Sprintf("%s.%s", c.predicate, key))
		}
	}

	// Create index schema
	var schema strings.Builder
	for _, pred := range predicates {
		schema.WriteString(fmt.Sprintf("%s: string @index(hash) .\n", pred))
	}

	op := &api.Operation{Schema: schema.String()}
	return c.db.client.Alter(ctx, op)
}

// DropIndex drops an index
func (c *dgraphCollection) DropIndex(ctx context.Context, name string) error {
	if c.db.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Dgraph doesn't have explicit index dropping
	// You would need to alter the schema to remove the index
	return fmt.Errorf("index dropping not directly supported in Dgraph")
}

// ListIndexes lists all indexes
func (c *dgraphCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	if c.db.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Query schema to get indexed predicates
	query := `
		schema {
			predicate
			type
			index
		}
	`

	resp, err := c.db.client.NewReadOnlyTxn().Query(ctx, query)
	if err != nil {
		return nil, err
	}

	var result struct {
		Schema []struct {
			Predicate string   `json:"predicate"`
			Type      string   `json:"type"`
			Index     []string `json:"index"`
		} `json:"schema"`
	}

	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, err
	}

	var indexes []IndexInfo
	for _, pred := range result.Schema {
		if len(pred.Index) > 0 && strings.HasPrefix(pred.Predicate, c.predicate+".") {
			index := IndexInfo{
				Name: pred.Predicate,
				Keys: make(map[string]interface{}),
			}

			// Extract field name
			field := strings.TrimPrefix(pred.Predicate, c.predicate+".")
			index.Keys[field] = 1

			indexes = append(indexes, index)
		}
	}

	return indexes, nil
}

// BulkWrite performs bulk operations
func (c *dgraphCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if c.db.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Convert operations to mutations
	var mutations []map[string]interface{}

	for i, op := range operations {
		// Convert operation to JSON
		opJSON, err := json.Marshal(op)
		if err != nil {
			return err
		}

		// Generate UID
		uid := fmt.Sprintf("_:%s_%d_%d", c.predicate, time.Now().UnixNano(), i)

		// Create mutation
		mutation := map[string]interface{}{
			"uid":                                     uid,
			fmt.Sprintf("%s.id", c.predicate):         uid,
			fmt.Sprintf("%s.data", c.predicate):       string(opJSON),
			fmt.Sprintf("%s.created_at", c.predicate): time.Now().Format(time.RFC3339),
			fmt.Sprintf("%s.updated_at", c.predicate): time.Now().Format(time.RFC3339),
		}

		mutations = append(mutations, mutation)
	}

	mutationJSON, err := json.Marshal(mutations)
	if err != nil {
		return err
	}

	mu := &api.Mutation{
		SetJson:   mutationJSON,
		CommitNow: true,
	}

	_, err = c.db.client.NewTxn().Mutate(ctx, mu)
	return err
}

// Dgraph Cursor implementation

// Next moves to the next document
func (c *dgraphCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return false
	}

	c.current++
	return c.current < len(c.results)
}

// Decode decodes the current document
func (c *dgraphCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	if c.current < 0 || c.current >= len(c.results) {
		return fmt.Errorf("no current document")
	}

	// Convert to JSON and back to handle the interface{} to dest conversion
	data, err := json.Marshal(c.results[c.current])
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// All decodes all documents
func (c *dgraphCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	// Convert to JSON and back to handle the interface{} to dest conversion
	data, err := json.Marshal(c.results)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// Close closes the cursor
func (c *dgraphCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	return nil
}

// Current returns the current document
func (c *dgraphCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.current < 0 || c.current >= len(c.results) {
		return nil
	}

	return c.results[c.current]
}

// Err returns the last error
func (c *dgraphCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.err
}

// Dgraph-specific utility functions

// GetClient returns the underlying Dgraph client
func (db *dgraphDatabase) GetClient() *dgo.Dgraph {
	return db.client
}

// ExecuteDQL executes a DQL query
func (db *dgraphDatabase) ExecuteDQL(ctx context.Context, query string) (*api.Response, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Dgraph not connected")
	}

	return db.client.NewReadOnlyTxn().Query(ctx, query)
}

// ExecuteMutation executes a mutation
func (db *dgraphDatabase) ExecuteMutation(ctx context.Context, mutation *api.Mutation) (*api.Response, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Dgraph not connected")
	}

	return db.client.NewTxn().Mutate(ctx, mutation)
}

// AlterSchema alters the schema
func (db *dgraphDatabase) AlterSchema(ctx context.Context, schema string) error {
	if db.client == nil {
		return fmt.Errorf("Dgraph not connected")
	}

	op := &api.Operation{Schema: schema}
	return db.client.Alter(ctx, op)
}

// GetSchema returns the current schema
func (db *dgraphDatabase) GetSchema(ctx context.Context) (string, error) {
	if db.client == nil {
		return "", fmt.Errorf("Dgraph not connected")
	}

	query := `
		schema {
			predicate
			type
			index
		}
	`

	resp, err := db.client.NewReadOnlyTxn().Query(ctx, query)
	if err != nil {
		return "", err
	}

	return string(resp.Json), nil
}

// GetClusterState returns cluster state
func (db *dgraphDatabase) GetClusterState(ctx context.Context) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Dgraph not connected")
	}

	query := `
		{
			state() {
				counter
				groups {
					id
					members {
						id
						groupId
						addr
						leader
					}
				}
			}
		}
	`

	resp, err := db.client.NewReadOnlyTxn().Query(ctx, query)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Json, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// Transaction support

// NewTransaction creates a new transaction
func (db *dgraphDatabase) NewTransaction() *dgo.Txn {
	if db.client == nil {
		return nil
	}

	return db.client.NewTxn()
}

// NewReadOnlyTransaction creates a new read-only transaction
func (db *dgraphDatabase) NewReadOnlyTransaction() *dgo.Txn {
	if db.client == nil {
		return nil
	}

	return db.client.NewReadOnlyTxn()
}

// GetConnectionInfo returns Dgraph connection information
func (db *dgraphDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Dgraph not connected")
	}

	info := make(map[string]interface{})

	// Get cluster state
	if state, err := db.GetClusterState(ctx); err == nil {
		info["cluster_state"] = state
	}

	// Get schema info
	if schema, err := db.GetSchema(ctx); err == nil {
		info["schema"] = schema
	}

	// Connection info
	info["alpha_addresses"] = db.alpha
	info["connections"] = len(db.conns)

	return info, nil
}

// Stats returns Dgraph statistics
func (db *dgraphDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}

	stats["driver"] = db.driver
	stats["connected"] = db.connected
	stats["alpha_addresses"] = db.alpha
	stats["connections"] = len(db.conns)

	return stats
}

// init function to override the Dgraph constructor
func init() {
	NewDgraphDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newDgraphDatabase(config)
	}
}
