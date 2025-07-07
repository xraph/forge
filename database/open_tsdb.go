package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// openTSDBDatabase implements NoSQLDatabase interface for OpenTSDB
type openTSDBDatabase struct {
	*baseNoSQLDatabase
	client   *http.Client
	endpoint string
	timeout  time.Duration
}

// openTSDBCollection implements Collection interface for OpenTSDB
type openTSDBCollection struct {
	*baseCollection
	client    *http.Client
	endpoint  string
	metric    string
	tagPrefix string
}

// openTSDBCursor implements Cursor interface for OpenTSDB
type openTSDBCursor struct {
	*baseCursor
	dataPoints []OpenTSDBDataPoint
	current    int
	mu         sync.RWMutex
}

// OpenTSDBDataPoint represents a time series data point
type OpenTSDBDataPoint struct {
	Metric    string                 `json:"metric"`
	Value     float64                `json:"value"`
	Timestamp int64                  `json:"timestamp"`
	Tags      map[string]string      `json:"tags"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// OpenTSDBQuery represents a query structure
type OpenTSDBQuery struct {
	Start             interface{}        `json:"start"`
	End               interface{}        `json:"end,omitempty"`
	Queries           []OpenTSDBSubQuery `json:"queries"`
	NoAnnotations     bool               `json:"noAnnotations,omitempty"`
	GlobalAnnotations bool               `json:"globalAnnotations,omitempty"`
	MsResolution      bool               `json:"msResolution,omitempty"`
	ShowTSUIDs        bool               `json:"showTSUIDs,omitempty"`
	ShowSummary       bool               `json:"showSummary,omitempty"`
	ShowStats         bool               `json:"showStats,omitempty"`
	ShowQuery         bool               `json:"showQuery,omitempty"`
	Delete            bool               `json:"delete,omitempty"`
}

// OpenTSDBSubQuery represents a sub-query
type OpenTSDBSubQuery struct {
	Aggregator  string                 `json:"aggregator"`
	Metric      string                 `json:"metric"`
	Rate        bool                   `json:"rate,omitempty"`
	RateOptions map[string]interface{} `json:"rateOptions,omitempty"`
	Downsample  string                 `json:"downsample,omitempty"`
	Tags        map[string]string      `json:"tags,omitempty"`
	Filters     []OpenTSDBFilter       `json:"filters,omitempty"`
}

// OpenTSDBFilter represents a filter
type OpenTSDBFilter struct {
	Type    string `json:"type"`
	Tagk    string `json:"tagk"`
	Filter  string `json:"filter"`
	GroupBy bool   `json:"groupBy,omitempty"`
}

// OpenTSDBResponse represents query response
type OpenTSDBResponse struct {
	Metric         string                 `json:"metric"`
	Tags           map[string]string      `json:"tags"`
	AggregatedTags []string               `json:"aggregatedTags"`
	Dps            map[string]float64     `json:"dps"`
	Query          OpenTSDBSubQuery       `json:"query,omitempty"`
	Stats          map[string]interface{} `json:"stats,omitempty"`
}

// NewOpenTSDBDatabase creates a new OpenTSDB database connection
func newOpenTSDBDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &openTSDBDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "opentsdb",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		timeout: 30 * time.Second,
	}

	// Set timeout from config
	if config.ConnectTimeout > 0 {
		db.timeout = config.ConnectTimeout
	}

	// Build endpoint URL
	if config.URL != "" {
		db.endpoint = config.URL
	} else {
		host := config.Host
		if host == "" {
			host = "localhost"
		}
		port := config.Port
		if port == 0 {
			port = 4242 // OpenTSDB default port
		}
		db.endpoint = fmt.Sprintf("http://%s:%d", host, port)
	}

	// Create HTTP client
	db.client = &http.Client{
		Timeout: db.timeout,
	}

	if err := db.Connect(context.Background()); err != nil {
		return nil, err
	}

	return db, nil
}

// Connect establishes OpenTSDB connection
func (db *openTSDBDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Test connection by getting version
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping OpenTSDB: %w", err)
	}

	db.connected = true
	return nil
}

// Close closes OpenTSDB connection
func (db *openTSDBDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.connected = false
	return nil
}

// Ping tests OpenTSDB connection
func (db *openTSDBDatabase) Ping(ctx context.Context) error {
	if db.client == nil {
		return fmt.Errorf("OpenTSDB client not initialized")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", db.endpoint+"/api/version", nil)
	if err != nil {
		return err
	}

	resp, err := db.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenTSDB ping failed: %s", resp.Status)
	}

	return nil
}

// Collection returns an OpenTSDB collection (metric namespace)
func (db *openTSDBDatabase) Collection(name string) Collection {
	return &openTSDBCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		client:    db.client,
		endpoint:  db.endpoint,
		metric:    name,
		tagPrefix: "tag_",
	}
}

// CreateCollection creates a new collection (metric namespace)
func (db *openTSDBDatabase) CreateCollection(ctx context.Context, name string) error {
	// OpenTSDB doesn't require explicit metric creation
	// Metrics are created automatically when first data point is inserted
	return nil
}

// DropCollection drops a collection (metric namespace)
func (db *openTSDBDatabase) DropCollection(ctx context.Context, name string) error {
	// OpenTSDB doesn't support dropping metrics directly
	// This would require deleting all data points for the metric
	return fmt.Errorf("OpenTSDB does not support dropping metrics")
}

// ListCollections lists all collections (metrics)
func (db *openTSDBDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.client == nil {
		return nil, fmt.Errorf("OpenTSDB not connected")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", db.endpoint+"/api/suggest", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("type", "metrics")
	q.Add("max", "10000")
	req.URL.RawQuery = q.Encode()

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list metrics: %s", resp.Status)
	}

	var metrics []string
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, err
	}

	return metrics, nil
}

// Drop drops the entire database (not supported)
func (db *openTSDBDatabase) Drop(ctx context.Context) error {
	return fmt.Errorf("OpenTSDB does not support dropping entire database")
}

// OpenTSDB Collection implementation

// FindOne finds a single data point (latest)
func (c *openTSDBCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	cursor, err := c.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	if cursor.Next(ctx) {
		return cursor.Current(), nil
	}

	return nil, fmt.Errorf("no data points found")
}

// Find finds multiple data points
func (c *openTSDBCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build query from filter
	query := c.buildQuery(filter)

	// Execute query
	dataPoints, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	return &openTSDBCursor{
		baseCursor: &baseCursor{
			data:   make([]interface{}, len(dataPoints)),
			index:  -1,
			err:    nil,
			closed: false,
		},
		dataPoints: dataPoints,
		current:    -1,
	}, nil
}

// Insert inserts a single data point
func (c *openTSDBCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Convert document to data point
	dataPoint, err := c.convertToDataPoint(document)
	if err != nil {
		return nil, err
	}

	// Insert data point
	if err := c.insertDataPoint(ctx, dataPoint); err != nil {
		return nil, err
	}

	return dataPoint.Timestamp, nil
}

// InsertMany inserts multiple data points
func (c *openTSDBCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	var dataPoints []OpenTSDBDataPoint
	var ids []interface{}

	for _, doc := range documents {
		dataPoint, err := c.convertToDataPoint(doc)
		if err != nil {
			return nil, err
		}
		dataPoints = append(dataPoints, dataPoint)
		ids = append(ids, dataPoint.Timestamp)
	}

	// Insert all data points
	if err := c.insertDataPoints(ctx, dataPoints); err != nil {
		return nil, err
	}

	return ids, nil
}

// Update updates data points (not supported in OpenTSDB)
func (c *openTSDBCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	return 0, fmt.Errorf("OpenTSDB does not support updating data points")
}

// UpdateOne updates a single data point (not supported in OpenTSDB)
func (c *openTSDBCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	return fmt.Errorf("OpenTSDB does not support updating data points")
}

// Delete deletes data points (limited support)
func (c *openTSDBCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	// OpenTSDB has limited delete support
	// Would need to use delete query with specific time range
	return 0, fmt.Errorf("OpenTSDB delete not fully implemented")
}

// DeleteOne deletes a single data point (not supported)
func (c *openTSDBCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	return fmt.Errorf("OpenTSDB does not support deleting single data points")
}

// Aggregate performs aggregation operations
func (c *openTSDBCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build aggregation query from pipeline
	query := c.buildAggregationQuery(pipeline)

	// Execute query
	dataPoints, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	return &openTSDBCursor{
		baseCursor: &baseCursor{
			data:   make([]interface{}, len(dataPoints)),
			index:  -1,
			err:    nil,
			closed: false,
		},
		dataPoints: dataPoints,
		current:    -1,
	}, nil
}

// Count counts data points
func (c *openTSDBCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	cursor, err := c.Find(ctx, filter)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var count int64
	for cursor.Next(ctx) {
		count++
	}

	return count, nil
}

// CreateIndex creates an index (not applicable for OpenTSDB)
func (c *openTSDBCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	return fmt.Errorf("OpenTSDB does not support explicit index creation")
}

// DropIndex drops an index (not applicable for OpenTSDB)
func (c *openTSDBCollection) DropIndex(ctx context.Context, name string) error {
	return fmt.Errorf("OpenTSDB does not support explicit index dropping")
}

// ListIndexes lists all indexes (not applicable for OpenTSDB)
func (c *openTSDBCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	return []IndexInfo{}, nil
}

// BulkWrite performs bulk operations
func (c *openTSDBCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	var dataPoints []OpenTSDBDataPoint
	for _, op := range operations {
		dataPoint, err := c.convertToDataPoint(op)
		if err != nil {
			return err
		}
		dataPoints = append(dataPoints, dataPoint)
	}

	return c.insertDataPoints(ctx, dataPoints)
}

// Helper methods

// buildQuery builds OpenTSDB query from filter
func (c *openTSDBCollection) buildQuery(filter interface{}) OpenTSDBQuery {
	query := OpenTSDBQuery{
		Start:   "1h-ago",
		Queries: []OpenTSDBSubQuery{},
	}

	subQuery := OpenTSDBSubQuery{
		Aggregator: "avg",
		Metric:     c.metric,
		Tags:       make(map[string]string),
	}

	// Parse filter if provided
	if filterMap, ok := filter.(map[string]interface{}); ok {
		for key, value := range filterMap {
			switch key {
			case "start":
				query.Start = value
			case "end":
				query.End = value
			case "aggregator":
				if aggStr, ok := value.(string); ok {
					subQuery.Aggregator = aggStr
				}
			case "downsample":
				if dsStr, ok := value.(string); ok {
					subQuery.Downsample = dsStr
				}
			case "tags":
				if tagMap, ok := value.(map[string]interface{}); ok {
					for k, v := range tagMap {
						if vStr, ok := v.(string); ok {
							subQuery.Tags[k] = vStr
						}
					}
				}
			}
		}
	}

	query.Queries = append(query.Queries, subQuery)
	return query
}

// buildAggregationQuery builds aggregation query
func (c *openTSDBCollection) buildAggregationQuery(pipeline interface{}) OpenTSDBQuery {
	query := OpenTSDBQuery{
		Start:   "1h-ago",
		Queries: []OpenTSDBSubQuery{},
	}

	subQuery := OpenTSDBSubQuery{
		Aggregator: "avg",
		Metric:     c.metric,
		Tags:       make(map[string]string),
	}

	// Parse pipeline if provided
	if pipelineSlice, ok := pipeline.([]interface{}); ok {
		for _, stage := range pipelineSlice {
			if stageMap, ok := stage.(map[string]interface{}); ok {
				for op, params := range stageMap {
					switch op {
					case "$match":
						// Handle match conditions
						if paramMap, ok := params.(map[string]interface{}); ok {
							for key, value := range paramMap {
								if key == "tags" {
									if tagMap, ok := value.(map[string]interface{}); ok {
										for k, v := range tagMap {
											if vStr, ok := v.(string); ok {
												subQuery.Tags[k] = vStr
											}
										}
									}
								}
							}
						}
					case "$group":
						// Handle grouping
						if paramMap, ok := params.(map[string]interface{}); ok {
							if aggField, ok := paramMap["aggregator"]; ok {
								if aggStr, ok := aggField.(string); ok {
									subQuery.Aggregator = aggStr
								}
							}
						}
					}
				}
			}
		}
	}

	query.Queries = append(query.Queries, subQuery)
	return query
}

// executeQuery executes OpenTSDB query
func (c *openTSDBCollection) executeQuery(ctx context.Context, query OpenTSDBQuery) ([]OpenTSDBDataPoint, error) {
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/api/query", bytes.NewBuffer(queryJSON))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query failed: %s - %s", resp.Status, string(body))
	}

	var responses []OpenTSDBResponse
	if err := json.NewDecoder(resp.Body).Decode(&responses); err != nil {
		return nil, err
	}

	// Convert responses to data points
	var dataPoints []OpenTSDBDataPoint
	for _, response := range responses {
		for tsStr, value := range response.Dps {
			timestamp, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				continue
			}

			dataPoint := OpenTSDBDataPoint{
				Metric:    response.Metric,
				Value:     value,
				Timestamp: timestamp,
				Tags:      response.Tags,
			}
			dataPoints = append(dataPoints, dataPoint)
		}
	}

	return dataPoints, nil
}

// convertToDataPoint converts document to OpenTSDB data point
func (c *openTSDBCollection) convertToDataPoint(document interface{}) (OpenTSDBDataPoint, error) {
	var dataPoint OpenTSDBDataPoint

	switch doc := document.(type) {
	case OpenTSDBDataPoint:
		dataPoint = doc
	case map[string]interface{}:
		// Convert map to data point
		dataPoint.Metric = c.metric
		dataPoint.Tags = make(map[string]string)

		for key, value := range doc {
			switch key {
			case "metric":
				if metricStr, ok := value.(string); ok {
					dataPoint.Metric = metricStr
				}
			case "value":
				if valueFloat, ok := value.(float64); ok {
					dataPoint.Value = valueFloat
				} else if valueInt, ok := value.(int64); ok {
					dataPoint.Value = float64(valueInt)
				}
			case "timestamp":
				if timestampInt, ok := value.(int64); ok {
					dataPoint.Timestamp = timestampInt
				} else if timestampFloat, ok := value.(float64); ok {
					dataPoint.Timestamp = int64(timestampFloat)
				}
			case "tags":
				if tagMap, ok := value.(map[string]interface{}); ok {
					for k, v := range tagMap {
						if vStr, ok := v.(string); ok {
							dataPoint.Tags[k] = vStr
						}
					}
				}
			default:
				// Add other fields as tags
				if valueStr, ok := value.(string); ok {
					dataPoint.Tags[key] = valueStr
				}
			}
		}

		// Set default timestamp if not provided
		if dataPoint.Timestamp == 0 {
			dataPoint.Timestamp = time.Now().Unix()
		}
	default:
		return dataPoint, fmt.Errorf("unsupported document type: %T", document)
	}

	// Set default metric if not provided
	if dataPoint.Metric == "" {
		dataPoint.Metric = c.metric
	}

	return dataPoint, nil
}

// insertDataPoint inserts a single data point
func (c *openTSDBCollection) insertDataPoint(ctx context.Context, dataPoint OpenTSDBDataPoint) error {
	return c.insertDataPoints(ctx, []OpenTSDBDataPoint{dataPoint})
}

// insertDataPoints inserts multiple data points
func (c *openTSDBCollection) insertDataPoints(ctx context.Context, dataPoints []OpenTSDBDataPoint) error {
	if len(dataPoints) == 0 {
		return nil
	}

	dataJSON, err := json.Marshal(dataPoints)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/api/put", bytes.NewBuffer(dataJSON))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("insert failed: %s - %s", resp.Status, string(body))
	}

	return nil
}

// OpenTSDB Cursor implementation

// Next moves to the next data point
func (c *openTSDBCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return false
	}

	c.current++
	return c.current < len(c.dataPoints)
}

// Decode decodes the current data point
func (c *openTSDBCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	if c.current < 0 || c.current >= len(c.dataPoints) {
		return fmt.Errorf("no current data point")
	}

	// Convert to JSON and back to handle type conversion
	data, err := json.Marshal(c.dataPoints[c.current])
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// All decodes all data points
func (c *openTSDBCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	// Convert to JSON and back to handle type conversion
	data, err := json.Marshal(c.dataPoints)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// Close closes the cursor
func (c *openTSDBCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	return nil
}

// Current returns the current data point
func (c *openTSDBCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.current < 0 || c.current >= len(c.dataPoints) {
		return nil
	}

	return c.dataPoints[c.current]
}

// Err returns the last error
func (c *openTSDBCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.err
}

// OpenTSDB-specific utility functions

// GetClient returns the underlying HTTP client
func (db *openTSDBDatabase) GetClient() *http.Client {
	return db.client
}

// GetEndpoint returns the OpenTSDB endpoint
func (db *openTSDBDatabase) GetEndpoint() string {
	return db.endpoint
}

// GetMetrics returns all available metrics
func (db *openTSDBDatabase) GetMetrics(ctx context.Context) ([]string, error) {
	return db.ListCollections(ctx)
}

// GetTags returns all available tags
func (db *openTSDBDatabase) GetTags(ctx context.Context) ([]string, error) {
	if db.client == nil {
		return nil, fmt.Errorf("OpenTSDB not connected")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", db.endpoint+"/api/suggest", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("type", "tagk")
	q.Add("max", "10000")
	req.URL.RawQuery = q.Encode()

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get tags: %s", resp.Status)
	}

	var tags []string
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}

	return tags, nil
}

// GetTagValues returns all values for a specific tag
func (db *openTSDBDatabase) GetTagValues(ctx context.Context, tag string) ([]string, error) {
	if db.client == nil {
		return nil, fmt.Errorf("OpenTSDB not connected")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", db.endpoint+"/api/suggest", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("type", "tagv")
	q.Add("max", "10000")
	if tag != "" {
		q.Add("q", tag)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get tag values: %s", resp.Status)
	}

	var values []string
	if err := json.NewDecoder(resp.Body).Decode(&values); err != nil {
		return nil, err
	}

	return values, nil
}

// GetStats returns OpenTSDB statistics
func (db *openTSDBDatabase) GetStats(ctx context.Context) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("OpenTSDB not connected")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", db.endpoint+"/api/stats", nil)
	if err != nil {
		return nil, err
	}

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get stats: %s", resp.Status)
	}

	var stats []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, err
	}

	// Convert stats array to map
	result := make(map[string]interface{})
	for _, stat := range stats {
		if metric, ok := stat["metric"].(string); ok {
			result[metric] = stat
		}
	}

	return result, nil
}

// Stats returns database statistics
func (db *openTSDBDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}

	stats["driver"] = db.driver
	stats["connected"] = db.connected
	stats["endpoint"] = db.endpoint
	stats["timeout"] = db.timeout

	return stats
}

// init function to register the OpenTSDB constructor
func init() {
	NewOpenTSDBDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newOpenTSDBDatabase(config)
	}
}
