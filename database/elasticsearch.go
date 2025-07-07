package database

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// elasticsearchDatabase implements NoSQLDatabase interface for Elasticsearch
type elasticsearchDatabase struct {
	*baseNoSQLDatabase
	client *elasticsearch.Client
	config elasticsearch.Config
}

// elasticsearchCollection implements Collection interface for Elasticsearch
type elasticsearchCollection struct {
	*baseCollection
	client *elasticsearch.Client
	index  string
}

// elasticsearchCursor implements Cursor interface for Elasticsearch
type elasticsearchCursor struct {
	*baseCursor
	hits    []map[string]interface{}
	current int
	mu      sync.RWMutex
}

// NewElasticsearchDatabase creates a new Elasticsearch database connection
func newElasticsearchDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &elasticsearchDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "elasticsearch",
			connected: false,
			stats:     make(map[string]interface{}),
		},
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}

	return db, nil
}

// Connect establishes Elasticsearch connection
func (db *elasticsearchDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Build Elasticsearch configuration
	esConfig := db.buildElasticsearchConfig()
	db.config = esConfig

	// Create Elasticsearch client
	client, err := elasticsearch.NewClient(esConfig)
	if err != nil {
		return fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	db.client = client

	// Test connection
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Elasticsearch: %w", err)
	}

	db.connected = true
	return nil
}

// buildElasticsearchConfig builds Elasticsearch client configuration
func (db *elasticsearchDatabase) buildElasticsearchConfig() elasticsearch.Config {
	config := elasticsearch.Config{}

	// Configure addresses
	if db.baseNoSQLDatabase.config.URL != "" {
		config.Addresses = []string{db.baseNoSQLDatabase.config.URL}
	} else {
		host := db.baseNoSQLDatabase.config.Host
		if host == "" {
			host = "localhost"
		}

		port := db.baseNoSQLDatabase.config.Port
		if port == 0 {
			port = 9200 // Elasticsearch default port
		}

		config.Addresses = []string{fmt.Sprintf("http://%s:%d", host, port)}
	}

	// Configure authentication
	if db.config.Username != "" && db.config.Password != "" {
		config.Username = db.config.Username
		config.Password = db.config.Password
	}

	// Configure timeouts
	if db.baseNoSQLDatabase.config.ConnectTimeout > 0 {
		config.Transport = &http.Transport{
			IdleConnTimeout: db.baseNoSQLDatabase.config.ConnectTimeout,
		}
	}

	// Configure retries
	config.RetryOnStatus = []int{502, 503, 504, 429}
	config.MaxRetries = 3

	return config
}

// Close closes Elasticsearch connection
func (db *elasticsearchDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.connected = false
	return nil
}

// Ping tests Elasticsearch connection
func (db *elasticsearchDatabase) Ping(ctx context.Context) error {
	if db.client == nil {
		return fmt.Errorf("Elasticsearch not connected")
	}

	req := esapi.PingRequest{}
	res, err := req.Do(ctx, db.client)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch ping failed: %s", res.Status())
	}

	return nil
}

// Collection returns an Elasticsearch collection (index)
func (db *elasticsearchDatabase) Collection(name string) Collection {
	return &elasticsearchCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		client: db.client,
		index:  name,
	}
}

// CreateCollection creates a new collection (index)
func (db *elasticsearchDatabase) CreateCollection(ctx context.Context, name string) error {
	if db.client == nil {
		return fmt.Errorf("Elasticsearch not connected")
	}

	// Create index with basic mapping
	mapping := `{
		"mappings": {
			"properties": {
				"data": {
					"type": "text"
				},
				"created_at": {
					"type": "date"
				},
				"updated_at": {
					"type": "date"
				}
			}
		}
	}`

	req := esapi.IndicesCreateRequest{
		Index: name,
		Body:  strings.NewReader(mapping),
	}

	res, err := req.Do(ctx, db.client)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("failed to create index: %s", res.Status())
	}

	return nil
}

// DropCollection drops a collection (index)
func (db *elasticsearchDatabase) DropCollection(ctx context.Context, name string) error {
	if db.client == nil {
		return fmt.Errorf("Elasticsearch not connected")
	}

	req := esapi.IndicesDeleteRequest{
		Index: []string{name},
	}

	res, err := req.Do(ctx, db.client)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() && res.StatusCode != 404 {
		return fmt.Errorf("failed to drop index: %s", res.Status())
	}

	return nil
}

// ListCollections lists all collections (indices)
func (db *elasticsearchDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Elasticsearch not connected")
	}

	req := esapi.CatIndicesRequest{
		Format: "json",
	}

	res, err := req.Do(ctx, db.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("failed to list indices: %s", res.Status())
	}

	var indices []struct {
		Index string `json:"index"`
	}

	if err := json.NewDecoder(res.Body).Decode(&indices); err != nil {
		return nil, err
	}

	var indexNames []string
	for _, idx := range indices {
		// Skip system indices
		if !strings.HasPrefix(idx.Index, ".") {
			indexNames = append(indexNames, idx.Index)
		}
	}

	return indexNames, nil
}

// Drop drops the entire database (all indices)
func (db *elasticsearchDatabase) Drop(ctx context.Context) error {
	if db.client == nil {
		return fmt.Errorf("Elasticsearch not connected")
	}

	// Get all indices
	indices, err := db.ListCollections(ctx)
	if err != nil {
		return err
	}

	// Drop each index
	for _, index := range indices {
		if err := db.DropCollection(ctx, index); err != nil {
			return err
		}
	}

	return nil
}

// Elasticsearch Collection implementation

// FindOne finds a single document
func (c *elasticsearchCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build query
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		"size": 1,
	}

	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	req := esapi.SearchRequest{
		Index: []string{c.index},
		Body:  strings.NewReader(string(queryJSON)),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("search failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	hits := result["hits"].(map[string]interface{})["hits"].([]interface{})
	if len(hits) == 0 {
		return nil, fmt.Errorf("no documents found")
	}

	hit := hits[0].(map[string]interface{})
	return hit["_source"], nil
}

// Find finds multiple documents
func (c *elasticsearchCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build query
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
		"size": 10000, // Maximum results
	}

	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	req := esapi.SearchRequest{
		Index: []string{c.index},
		Body:  strings.NewReader(string(queryJSON)),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("search failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	hits := result["hits"].(map[string]interface{})["hits"].([]interface{})
	var documents []map[string]interface{}

	for _, hit := range hits {
		hitMap := hit.(map[string]interface{})
		documents = append(documents, hitMap["_source"].(map[string]interface{}))
	}

	return &elasticsearchCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{},
			index:  -1,
			err:    nil,
			closed: false,
		},
		hits:    documents,
		current: -1,
	}, nil
}

// Insert inserts a single document
func (c *elasticsearchCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Add timestamps
	doc := map[string]interface{}{
		"data":       document,
		"created_at": time.Now().Format(time.RFC3339),
		"updated_at": time.Now().Format(time.RFC3339),
	}

	docJSON, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}

	req := esapi.IndexRequest{
		Index: c.index,
		Body:  strings.NewReader(string(docJSON)),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("index failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result["_id"], nil
}

// InsertMany inserts multiple documents
func (c *elasticsearchCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build bulk request
	var bulk strings.Builder
	var ids []interface{}

	for _, document := range documents {
		// Add timestamps
		doc := map[string]interface{}{
			"data":       document,
			"created_at": time.Now().Format(time.RFC3339),
			"updated_at": time.Now().Format(time.RFC3339),
		}

		// Action line
		action := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": c.index,
			},
		}

		actionJSON, err := json.Marshal(action)
		if err != nil {
			return nil, err
		}

		bulk.Write(actionJSON)
		bulk.WriteString("\n")

		// Document line
		docJSON, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}

		bulk.Write(docJSON)
		bulk.WriteString("\n")
	}

	req := esapi.BulkRequest{
		Body: strings.NewReader(bulk.String()),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("bulk index failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Extract IDs from bulk response
	items := result["items"].([]interface{})
	for _, item := range items {
		itemMap := item.(map[string]interface{})
		indexResult := itemMap["index"].(map[string]interface{})
		ids = append(ids, indexResult["_id"])
	}

	return ids, nil
}

// Update updates multiple documents
func (c *elasticsearchCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	if c.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	// Build update query
	query := map[string]interface{}{
		"script": map[string]interface{}{
			"source": "ctx._source.updated_at = params.updated_at",
			"params": map[string]interface{}{
				"updated_at": time.Now().Format(time.RFC3339),
			},
		},
		"query": map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
	}

	queryJSON, err := json.Marshal(query)
	if err != nil {
		return 0, err
	}

	req := esapi.UpdateByQueryRequest{
		Index: []string{c.index},
		Body:  strings.NewReader(string(queryJSON)),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return 0, fmt.Errorf("update failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return 0, err
	}

	return int64(result["updated"].(float64)), nil
}

// UpdateOne updates a single document
func (c *elasticsearchCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// This is a simplified implementation
	// In practice, you'd need to find the document first, then update it
	return fmt.Errorf("update one not implemented for Elasticsearch collection")
}

// Delete deletes multiple documents
func (c *elasticsearchCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	if c.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	// Build delete query
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
	}

	queryJSON, err := json.Marshal(query)
	if err != nil {
		return 0, err
	}

	req := esapi.DeleteByQueryRequest{
		Index: []string{c.index},
		Body:  strings.NewReader(string(queryJSON)),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return 0, fmt.Errorf("delete failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return 0, err
	}

	return int64(result["deleted"].(float64)), nil
}

// DeleteOne deletes a single document
func (c *elasticsearchCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// This is a simplified implementation
	// In practice, you'd need to find the document first, then delete it
	return fmt.Errorf("delete one not implemented for Elasticsearch collection")
}

// Aggregate performs aggregation
func (c *elasticsearchCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build aggregation query
	query := map[string]interface{}{
		"size": 0,
		"aggs": map[string]interface{}{
			"count": map[string]interface{}{
				"value_count": map[string]interface{}{
					"field": "_id",
				},
			},
		},
	}

	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	req := esapi.SearchRequest{
		Index: []string{c.index},
		Body:  strings.NewReader(string(queryJSON)),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("aggregation failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Extract aggregation results
	aggs := result["aggregations"].(map[string]interface{})

	return &elasticsearchCursor{
		baseCursor: &baseCursor{
			data:   []interface{}{aggs},
			index:  -1,
			err:    nil,
			closed: false,
		},
		hits:    []map[string]interface{}{aggs},
		current: -1,
	}, nil
}

// Count counts documents
func (c *elasticsearchCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	if c.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	req := esapi.CountRequest{
		Index: []string{c.index},
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return 0, fmt.Errorf("count failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return 0, err
	}

	return int64(result["count"].(float64)), nil
}

// CreateIndex creates an index (mapping)
func (c *elasticsearchCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Build mapping for the fields
	properties := make(map[string]interface{})
	if keyMap, ok := keys.(map[string]interface{}); ok {
		for key := range keyMap {
			properties[key] = map[string]interface{}{
				"type": "text",
			}
		}
	}

	mapping := map[string]interface{}{
		"properties": properties,
	}

	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return err
	}

	req := esapi.IndicesPutMappingRequest{
		Index: []string{c.index},
		Body:  strings.NewReader(string(mappingJSON)),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("mapping failed: %s", res.Status())
	}

	return nil
}

// DropIndex drops an index (not directly supported)
func (c *elasticsearchCollection) DropIndex(ctx context.Context, name string) error {
	// Elasticsearch doesn't have explicit index dropping for fields
	// You would need to reindex or use aliases
	return fmt.Errorf("index dropping not directly supported in Elasticsearch")
}

// ListIndexes lists all indexes (mappings)
func (c *elasticsearchCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	req := esapi.IndicesGetMappingRequest{
		Index: []string{c.index},
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("get mapping failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	var indexes []IndexInfo
	for _, indexData := range result {
		indexMap := indexData.(map[string]interface{})
		mappings := indexMap["mappings"].(map[string]interface{})
		properties := mappings["properties"].(map[string]interface{})

		for fieldName := range properties {
			index := IndexInfo{
				Name: fieldName,
				Keys: map[string]interface{}{
					fieldName: 1,
				},
			}
			indexes = append(indexes, index)
		}

		// Only process the first index since we're looking at a specific index
		break
	}

	return indexes, nil
}

// BulkWrite performs bulk operations
func (c *elasticsearchCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Build bulk request
	var bulk strings.Builder

	for _, operation := range operations {
		// Add timestamps
		doc := map[string]interface{}{
			"data":       operation,
			"created_at": time.Now().Format(time.RFC3339),
			"updated_at": time.Now().Format(time.RFC3339),
		}

		// Action line
		action := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": c.index,
			},
		}

		actionJSON, err := json.Marshal(action)
		if err != nil {
			return err
		}

		bulk.Write(actionJSON)
		bulk.WriteString("\n")

		// Document line
		docJSON, err := json.Marshal(doc)
		if err != nil {
			return err
		}

		bulk.Write(docJSON)
		bulk.WriteString("\n")
	}

	req := esapi.BulkRequest{
		Body: strings.NewReader(bulk.String()),
	}

	res, err := req.Do(ctx, c.client)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("bulk write failed: %s", res.Status())
	}

	return nil
}

// Elasticsearch Cursor implementation

// Next moves to the next document
func (c *elasticsearchCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return false
	}

	c.current++
	return c.current < len(c.hits)
}

// Decode decodes the current document
func (c *elasticsearchCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	if c.current < 0 || c.current >= len(c.hits) {
		return fmt.Errorf("no current document")
	}

	// Convert to JSON and back to handle the map[string]interface{} to dest conversion
	data, err := json.Marshal(c.hits[c.current])
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// All decodes all documents
func (c *elasticsearchCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	// Convert to JSON and back to handle the []map[string]interface{} to dest conversion
	data, err := json.Marshal(c.hits)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// Close closes the cursor
func (c *elasticsearchCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	return nil
}

// Current returns the current document
func (c *elasticsearchCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.current < 0 || c.current >= len(c.hits) {
		return nil
	}

	return c.hits[c.current]
}

// Err returns the last error
func (c *elasticsearchCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.err
}

// Elasticsearch-specific utility functions

// GetClient returns the underlying Elasticsearch client
func (db *elasticsearchDatabase) GetClient() *elasticsearch.Client {
	return db.client
}

// ExecuteQuery executes a custom query
func (db *elasticsearchDatabase) ExecuteQuery(ctx context.Context, index string, query map[string]interface{}) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Elasticsearch not connected")
	}

	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	req := esapi.SearchRequest{
		Index: []string{index},
		Body:  strings.NewReader(string(queryJSON)),
	}

	res, err := req.Do(ctx, db.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("query failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetClusterInfo returns cluster information
func (db *elasticsearchDatabase) GetClusterInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Elasticsearch not connected")
	}

	req := esapi.InfoRequest{}
	res, err := req.Do(ctx, db.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("cluster info failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetClusterHealth returns cluster health
func (db *elasticsearchDatabase) GetClusterHealth(ctx context.Context) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Elasticsearch not connected")
	}

	req := esapi.ClusterHealthRequest{}
	res, err := req.Do(ctx, db.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("cluster health failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetIndexStats returns index statistics
func (db *elasticsearchDatabase) GetIndexStats(ctx context.Context, index string) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Elasticsearch not connected")
	}

	req := esapi.IndicesStatsRequest{
		Index: []string{index},
	}

	res, err := req.Do(ctx, db.client)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("index stats failed: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// RefreshIndex refreshes an index
func (db *elasticsearchDatabase) RefreshIndex(ctx context.Context, index string) error {
	if db.client == nil {
		return fmt.Errorf("Elasticsearch not connected")
	}

	req := esapi.IndicesRefreshRequest{
		Index: []string{index},
	}

	res, err := req.Do(ctx, db.client)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("refresh failed: %s", res.Status())
	}

	return nil
}

// GetConnectionInfo returns Elasticsearch connection information
func (db *elasticsearchDatabase) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Elasticsearch not connected")
	}

	info := make(map[string]interface{})

	// Get cluster info
	if clusterInfo, err := db.GetClusterInfo(ctx); err == nil {
		info["cluster_info"] = clusterInfo
	}

	// Get cluster health
	if clusterHealth, err := db.GetClusterHealth(ctx); err == nil {
		info["cluster_health"] = clusterHealth
	}

	// Connection config
	info["addresses"] = db.config.Addresses
	info["username"] = db.config.Username

	return info, nil
}

// Stats returns Elasticsearch statistics
func (db *elasticsearchDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}

	stats["driver"] = db.driver
	stats["connected"] = db.connected
	stats["addresses"] = db.config.Addresses

	return stats
}

// init function to override the Elasticsearch constructor
func init() {
	NewElasticsearchDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newElasticsearchDatabase(config)
	}
}
