package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// solrDatabase implements NoSQLDatabase interface for Apache Solr
type solrDatabase struct {
	*baseNoSQLDatabase
	client   *http.Client
	baseURL  string
	timeout  time.Duration
	username string
	password string
}

// solrCollection implements Collection interface for Solr
type solrCollection struct {
	*baseCollection
	client  *http.Client
	baseURL string
	core    string
}

// solrCursor implements Cursor interface for Solr
type solrCursor struct {
	*baseCursor
	documents []SolrDocument
	current   int
	mu        sync.RWMutex
}

// SolrDocument represents a Solr document
type SolrDocument struct {
	ID        string                 `json:"id"`
	Data      map[string]interface{} `json:"data"`
	Score     float64                `json:"score,omitempty"`
	Boost     float64                `json:"boost,omitempty"`
	Version   int64                  `json:"_version_,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// SolrQuery represents a Solr query
type SolrQuery struct {
	Query       string                 `json:"q"`
	FilterQuery []string               `json:"fq,omitempty"`
	Fields      []string               `json:"fl,omitempty"`
	Sort        string                 `json:"sort,omitempty"`
	Start       int                    `json:"start,omitempty"`
	Rows        int                    `json:"rows,omitempty"`
	Facets      map[string]interface{} `json:"facet,omitempty"`
	Highlight   map[string]interface{} `json:"hl,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

// SolrResponse represents a Solr response
type SolrResponse struct {
	ResponseHeader SolrResponseHeader     `json:"responseHeader"`
	Response       SolrResponseBody       `json:"response"`
	Facets         map[string]interface{} `json:"facet_counts,omitempty"`
	Highlighting   map[string]interface{} `json:"highlighting,omitempty"`
	Stats          map[string]interface{} `json:"stats,omitempty"`
}

// SolrResponseHeader represents response header
type SolrResponseHeader struct {
	Status int                    `json:"status"`
	QTime  int                    `json:"QTime"`
	Params map[string]interface{} `json:"params"`
}

// SolrResponseBody represents response body
type SolrResponseBody struct {
	NumFound int                      `json:"numFound"`
	Start    int                      `json:"start"`
	Docs     []map[string]interface{} `json:"docs"`
}

// SolrUpdateResponse represents update response
type SolrUpdateResponse struct {
	ResponseHeader SolrResponseHeader `json:"responseHeader"`
}

// NewSolrDatabase creates a new Solr database connection
func newSolrDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	db := &solrDatabase{
		baseNoSQLDatabase: &baseNoSQLDatabase{
			config:    config,
			driver:    "solr",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		timeout:  30 * time.Second,
		username: config.Username,
		password: config.Password,
	}

	// Set timeout from config
	if config.ConnectTimeout > 0 {
		db.timeout = config.ConnectTimeout
	}

	// Build base URL
	if config.URL != "" {
		db.baseURL = config.URL
	} else {
		host := config.Host
		if host == "" {
			host = "localhost"
		}
		port := config.Port
		if port == 0 {
			port = 8983 // Default Solr port
		}
		db.baseURL = fmt.Sprintf("http://%s:%d/solr", host, port)
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

// Connect establishes Solr connection
func (db *solrDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Test connection by getting system info
	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Solr: %w", err)
	}

	db.connected = true
	return nil
}

// Close closes Solr connection
func (db *solrDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.connected = false
	return nil
}

// Ping tests Solr connection
func (db *solrDatabase) Ping(ctx context.Context) error {
	if db.client == nil {
		return fmt.Errorf("Solr client not initialized")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", db.baseURL+"/admin/info/system", nil)
	if err != nil {
		return err
	}

	// Add authentication if provided
	if db.username != "" && db.password != "" {
		req.SetBasicAuth(db.username, db.password)
	}

	resp, err := db.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Solr ping failed: %s", resp.Status)
	}

	return nil
}

// Collection returns a Solr collection (core)
func (db *solrDatabase) Collection(name string) Collection {
	return &solrCollection{
		baseCollection: &baseCollection{
			name: name,
			db:   db,
		},
		client:  db.client,
		baseURL: db.baseURL,
		core:    name,
	}
}

// CreateCollection creates a new collection (core)
func (db *solrDatabase) CreateCollection(ctx context.Context, name string) error {
	if db.client == nil {
		return fmt.Errorf("Solr not connected")
	}

	// Create core using CoreAdmin API
	reqURL := fmt.Sprintf("%s/admin/cores?action=CREATE&name=%s&configSet=_default", db.baseURL, name)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	// Add authentication if provided
	if db.username != "" && db.password != "" {
		req.SetBasicAuth(db.username, db.password)
	}

	resp, err := db.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create core: %s - %s", resp.Status, string(body))
	}

	return nil
}

// DropCollection drops a collection (core)
func (db *solrDatabase) DropCollection(ctx context.Context, name string) error {
	if db.client == nil {
		return fmt.Errorf("Solr not connected")
	}

	// Delete core using CoreAdmin API
	reqURL := fmt.Sprintf("%s/admin/cores?action=UNLOAD&core=%s&deleteIndex=true&deleteDataDir=true&deleteInstanceDir=true", db.baseURL, name)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	// Add authentication if provided
	if db.username != "" && db.password != "" {
		req.SetBasicAuth(db.username, db.password)
	}

	resp, err := db.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to drop core: %s - %s", resp.Status, string(body))
	}

	return nil
}

// ListCollections lists all collections (cores)
func (db *solrDatabase) ListCollections(ctx context.Context) ([]string, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Solr not connected")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", db.baseURL+"/admin/cores?action=STATUS", nil)
	if err != nil {
		return nil, err
	}

	// Add authentication if provided
	if db.username != "" && db.password != "" {
		req.SetBasicAuth(db.username, db.password)
	}

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list cores: %s", resp.Status)
	}

	var response struct {
		Status map[string]interface{} `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	var cores []string
	for coreName := range response.Status {
		cores = append(cores, coreName)
	}

	return cores, nil
}

// Drop drops the entire database (not applicable for Solr)
func (db *solrDatabase) Drop(ctx context.Context) error {
	return fmt.Errorf("Solr does not support dropping entire database")
}

// Solr Collection implementation

// FindOne finds a single document
func (c *solrCollection) FindOne(ctx context.Context, filter interface{}) (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build query
	query := c.buildQuery(filter)
	query.Rows = 1

	// Execute query
	docs, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, fmt.Errorf("no documents found")
	}

	return docs[0], nil
}

// Find finds multiple documents
func (c *solrCollection) Find(ctx context.Context, filter interface{}) (Cursor, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build query
	query := c.buildQuery(filter)
	if query.Rows == 0 {
		query.Rows = 1000 // Default limit
	}

	// Execute query
	docs, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	return &solrCursor{
		baseCursor: &baseCursor{
			data:   make([]interface{}, len(docs)),
			index:  -1,
			err:    nil,
			closed: false,
		},
		documents: docs,
		current:   -1,
	}, nil
}

// Insert inserts a single document
func (c *solrCollection) Insert(ctx context.Context, document interface{}) (interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Convert document to SolrDocument
	doc, err := c.convertToSolrDocument(document)
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

	// Create update request
	updateDoc := c.prepareSolrDocument(doc)
	updateRequest := []map[string]interface{}{updateDoc}

	// Execute update
	if err := c.executeUpdate(ctx, updateRequest); err != nil {
		return nil, err
	}

	return doc.ID, nil
}

// InsertMany inserts multiple documents
func (c *solrCollection) InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	var updateRequest []map[string]interface{}
	var ids []interface{}

	for _, document := range documents {
		// Convert document to SolrDocument
		doc, err := c.convertToSolrDocument(document)
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

		// Prepare document for Solr
		updateDoc := c.prepareSolrDocument(doc)
		updateRequest = append(updateRequest, updateDoc)
		ids = append(ids, doc.ID)
	}

	// Execute update
	if err := c.executeUpdate(ctx, updateRequest); err != nil {
		return nil, err
	}

	return ids, nil
}

// Update updates multiple documents
func (c *solrCollection) Update(ctx context.Context, filter interface{}, update interface{}) (int64, error) {
	if c.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	// Find documents to update
	cursor, err := c.Find(ctx, filter)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var updateRequest []map[string]interface{}
	var count int64

	for cursor.Next(ctx) {
		var doc SolrDocument
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		// Apply update
		updatedDoc := c.applyUpdate(doc, update)
		updatedDoc.UpdatedAt = time.Now()

		// Prepare document for Solr
		updateDoc := c.prepareSolrDocument(updatedDoc)
		updateRequest = append(updateRequest, updateDoc)
		count++
	}

	if len(updateRequest) > 0 {
		if err := c.executeUpdate(ctx, updateRequest); err != nil {
			return 0, err
		}
	}

	return count, nil
}

// UpdateOne updates a single document
func (c *solrCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Find the document
	doc, err := c.FindOne(ctx, filter)
	if err != nil {
		return err
	}

	solrDoc, ok := doc.(SolrDocument)
	if !ok {
		return fmt.Errorf("invalid document type")
	}

	// Apply update
	updatedDoc := c.applyUpdate(solrDoc, update)
	updatedDoc.UpdatedAt = time.Now()

	// Prepare document for Solr
	updateDoc := c.prepareSolrDocument(updatedDoc)
	updateRequest := []map[string]interface{}{updateDoc}

	// Execute update
	return c.executeUpdate(ctx, updateRequest)
}

// Delete deletes multiple documents
func (c *solrCollection) Delete(ctx context.Context, filter interface{}) (int64, error) {
	if c.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	// Build delete query
	deleteQuery := c.buildDeleteQuery(filter)

	// Execute delete
	return c.executeDelete(ctx, deleteQuery)
}

// DeleteOne deletes a single document
func (c *solrCollection) DeleteOne(ctx context.Context, filter interface{}) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Find the document first
	doc, err := c.FindOne(ctx, filter)
	if err != nil {
		return err
	}

	solrDoc, ok := doc.(SolrDocument)
	if !ok {
		return fmt.Errorf("invalid document type")
	}

	// Delete by ID
	deleteQuery := map[string]interface{}{
		"delete": map[string]interface{}{
			"id": solrDoc.ID,
		},
	}

	_, err = c.executeDelete(ctx, deleteQuery)
	return err
}

// Aggregate performs aggregation using Solr facets
func (c *solrCollection) Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	// Build aggregation query using facets
	query := c.buildAggregationQuery(pipeline)

	// Execute query
	docs, err := c.executeQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	return &solrCursor{
		baseCursor: &baseCursor{
			data:   make([]interface{}, len(docs)),
			index:  -1,
			err:    nil,
			closed: false,
		},
		documents: docs,
		current:   -1,
	}, nil
}

// Count counts documents
func (c *solrCollection) Count(ctx context.Context, filter interface{}) (int64, error) {
	if c.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	// Build query
	query := c.buildQuery(filter)
	query.Rows = 0 // We only want the count

	// Execute query
	if c.client == nil {
		return 0, fmt.Errorf("client not initialized")
	}

	// Build request URL
	reqURL := fmt.Sprintf("%s/%s/select", c.baseURL, c.core)
	params := url.Values{}
	params.Add("q", query.Query)
	params.Add("rows", strconv.Itoa(query.Rows))
	params.Add("wt", "json")

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL+"?"+params.Encode(), nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("query failed: %s", resp.Status)
	}

	var response SolrResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, err
	}

	return int64(response.Response.NumFound), nil
}

// CreateIndex creates an index (field type)
func (c *solrCollection) CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Solr schema management - add field
	if keyMap, ok := keys.(map[string]interface{}); ok {
		for fieldName, fieldConfig := range keyMap {
			fieldType := "text_general"
			if config, ok := fieldConfig.(map[string]interface{}); ok {
				if ft, ok := config["type"].(string); ok {
					fieldType = ft
				}
			}

			// Add field to schema
			schema := map[string]interface{}{
				"add-field": map[string]interface{}{
					"name":    fieldName,
					"type":    fieldType,
					"stored":  true,
					"indexed": true,
				},
			}

			schemaJSON, err := json.Marshal(schema)
			if err != nil {
				return err
			}

			reqURL := fmt.Sprintf("%s/%s/schema", c.baseURL, c.core)
			req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(schemaJSON))
			if err != nil {
				return err
			}

			req.Header.Set("Content-Type", "application/json")

			resp, err := c.client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("failed to add field: %s - %s", resp.Status, string(body))
			}
		}
	}

	return nil
}

// DropIndex drops an index (field)
func (c *solrCollection) DropIndex(ctx context.Context, name string) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	// Remove field from schema
	schema := map[string]interface{}{
		"delete-field": map[string]interface{}{
			"name": name,
		},
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return err
	}

	reqURL := fmt.Sprintf("%s/%s/schema", c.baseURL, c.core)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(schemaJSON))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to remove field: %s - %s", resp.Status, string(body))
	}

	return nil
}

// ListIndexes lists all indexes (fields)
func (c *solrCollection) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	if c.client == nil {
		return nil, fmt.Errorf("client not initialized")
	}

	reqURL := fmt.Sprintf("%s/%s/schema/fields", c.baseURL, c.core)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get fields: %s", resp.Status)
	}

	var response struct {
		Fields []map[string]interface{} `json:"fields"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	var indexes []IndexInfo
	for _, field := range response.Fields {
		if name, ok := field["name"].(string); ok {
			index := IndexInfo{
				Name: name,
				Keys: map[string]interface{}{
					name: 1,
				},
				Unique: false,
			}
			indexes = append(indexes, index)
		}
	}

	return indexes, nil
}

// BulkWrite performs bulk operations
func (c *solrCollection) BulkWrite(ctx context.Context, operations []interface{}) error {
	if c.client == nil {
		return fmt.Errorf("client not initialized")
	}

	var updateRequest []map[string]interface{}

	for _, operation := range operations {
		// Convert operation to document
		doc, err := c.convertToSolrDocument(operation)
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

		// Prepare document for Solr
		updateDoc := c.prepareSolrDocument(doc)
		updateRequest = append(updateRequest, updateDoc)
	}

	// Execute update
	return c.executeUpdate(ctx, updateRequest)
}

// Helper methods

// buildQuery builds Solr query from filter
func (c *solrCollection) buildQuery(filter interface{}) SolrQuery {
	query := SolrQuery{
		Query: "*:*",
		Rows:  10,
	}

	if filter != nil {
		if filterMap, ok := filter.(map[string]interface{}); ok {
			var queryParts []string

			for key, value := range filterMap {
				switch key {
				case "q", "query":
					if queryStr, ok := value.(string); ok {
						query.Query = queryStr
					}
				case "fq", "filter":
					if filterQuery, ok := value.([]string); ok {
						query.FilterQuery = filterQuery
					}
				case "fl", "fields":
					if fields, ok := value.([]string); ok {
						query.Fields = fields
					}
				case "sort":
					if sortStr, ok := value.(string); ok {
						query.Sort = sortStr
					}
				case "start":
					if start, ok := value.(int); ok {
						query.Start = start
					}
				case "rows", "limit":
					if rows, ok := value.(int); ok {
						query.Rows = rows
					}
				default:
					// Convert other filters to query parts
					if valueStr, ok := value.(string); ok {
						queryParts = append(queryParts, fmt.Sprintf("%s:%s", key, valueStr))
					}
				}
			}

			if len(queryParts) > 0 {
				if query.Query == "*:*" {
					query.Query = strings.Join(queryParts, " AND ")
				} else {
					query.Query += " AND " + strings.Join(queryParts, " AND ")
				}
			}
		}
	}

	return query
}

// buildAggregationQuery builds aggregation query using facets
func (c *solrCollection) buildAggregationQuery(pipeline interface{}) SolrQuery {
	query := SolrQuery{
		Query:  "*:*",
		Rows:   0,
		Facets: make(map[string]interface{}),
	}

	// Parse pipeline for aggregation operations
	if pipelineSlice, ok := pipeline.([]interface{}); ok {
		for _, stage := range pipelineSlice {
			if stageMap, ok := stage.(map[string]interface{}); ok {
				for op, params := range stageMap {
					switch op {
					case "$group":
						if paramMap, ok := params.(map[string]interface{}); ok {
							if id, ok := paramMap["_id"].(string); ok {
								query.Facets["facet"] = "true"
								query.Facets["facet.field"] = id
							}
						}
					case "$match":
						if paramMap, ok := params.(map[string]interface{}); ok {
							var queryParts []string
							for key, value := range paramMap {
								if valueStr, ok := value.(string); ok {
									queryParts = append(queryParts, fmt.Sprintf("%s:%s", key, valueStr))
								}
							}
							if len(queryParts) > 0 {
								query.Query = strings.Join(queryParts, " AND ")
							}
						}
					}
				}
			}
		}
	}

	return query
}

// buildDeleteQuery builds delete query
func (c *solrCollection) buildDeleteQuery(filter interface{}) map[string]interface{} {
	deleteQuery := map[string]interface{}{
		"delete": map[string]interface{}{
			"query": "*:*",
		},
	}

	if filter != nil {
		if filterMap, ok := filter.(map[string]interface{}); ok {
			var queryParts []string

			for key, value := range filterMap {
				if valueStr, ok := value.(string); ok {
					queryParts = append(queryParts, fmt.Sprintf("%s:%s", key, valueStr))
				}
			}

			if len(queryParts) > 0 {
				deleteQuery["delete"].(map[string]interface{})["query"] = strings.Join(queryParts, " AND ")
			}
		}
	}

	return deleteQuery
}

// executeQuery executes Solr query
func (c *solrCollection) executeQuery(ctx context.Context, query SolrQuery) ([]SolrDocument, error) {
	reqURL := fmt.Sprintf("%s/%s/select", c.baseURL, c.core)
	params := url.Values{}
	params.Add("q", query.Query)
	params.Add("rows", strconv.Itoa(query.Rows))
	params.Add("start", strconv.Itoa(query.Start))
	params.Add("wt", "json")

	if query.Sort != "" {
		params.Add("sort", query.Sort)
	}

	if len(query.Fields) > 0 {
		params.Add("fl", strings.Join(query.Fields, ","))
	}

	for _, fq := range query.FilterQuery {
		params.Add("fq", fq)
	}

	// Add facet parameters
	for key, value := range query.Facets {
		params.Add(key, fmt.Sprintf("%v", value))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query failed: %s - %s", resp.Status, string(body))
	}

	var response SolrResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	// Convert response to SolrDocuments
	var documents []SolrDocument
	for _, doc := range response.Response.Docs {
		solrDoc := SolrDocument{
			Data: doc,
		}

		if id, ok := doc["id"].(string); ok {
			solrDoc.ID = id
		}

		if score, ok := doc["score"].(float64); ok {
			solrDoc.Score = score
		}

		if version, ok := doc["_version_"].(float64); ok {
			solrDoc.Version = int64(version)
		}

		documents = append(documents, solrDoc)
	}

	return documents, nil
}

// executeUpdate executes Solr update
func (c *solrCollection) executeUpdate(ctx context.Context, updateRequest []map[string]interface{}) error {
	reqURL := fmt.Sprintf("%s/%s/update/json?commit=true", c.baseURL, c.core)

	reqJSON, err := json.Marshal(updateRequest)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(reqJSON))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update failed: %s - %s", resp.Status, string(body))
	}

	return nil
}

// executeDelete executes Solr delete
func (c *solrCollection) executeDelete(ctx context.Context, deleteQuery map[string]interface{}) (int64, error) {
	reqURL := fmt.Sprintf("%s/%s/update/json?commit=true", c.baseURL, c.core)

	reqJSON, err := json.Marshal(deleteQuery)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(reqJSON))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("delete failed: %s - %s", resp.Status, string(body))
	}

	// Solr doesn't return the number of deleted documents directly
	// We return 1 to indicate success
	return 1, nil
}

// convertToSolrDocument converts interface{} to SolrDocument
func (c *solrCollection) convertToSolrDocument(document interface{}) (SolrDocument, error) {
	var doc SolrDocument

	switch d := document.(type) {
	case SolrDocument:
		doc = d
	case map[string]interface{}:
		doc.Data = d
		if id, ok := d["id"].(string); ok {
			doc.ID = id
		}
		if id, ok := d["_id"].(string); ok {
			doc.ID = id
		}
		if score, ok := d["score"].(float64); ok {
			doc.Score = score
		}
		if boost, ok := d["boost"].(float64); ok {
			doc.Boost = boost
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

// prepareSolrDocument prepares document for Solr indexing
func (c *solrCollection) prepareSolrDocument(doc SolrDocument) map[string]interface{} {
	result := make(map[string]interface{})

	// Add ID
	result["id"] = doc.ID

	// Add data fields
	for key, value := range doc.Data {
		result[key] = value
	}

	// Add timestamps
	result["created_at"] = doc.CreatedAt.Format(time.RFC3339)
	result["updated_at"] = doc.UpdatedAt.Format(time.RFC3339)

	// Add boost if provided
	if doc.Boost > 0 {
		result["boost"] = doc.Boost
	}

	return result
}

// applyUpdate applies update operations to document
func (c *solrCollection) applyUpdate(doc SolrDocument, update interface{}) SolrDocument {
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
func (c *solrCollection) generateID() string {
	return fmt.Sprintf("doc_%d", time.Now().UnixNano())
}

// Solr Cursor implementation

// Next moves to the next document
func (c *solrCursor) Next(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return false
	}

	c.current++
	return c.current < len(c.documents)
}

// Decode decodes the current document
func (c *solrCursor) Decode(dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	if c.current < 0 || c.current >= len(c.documents) {
		return fmt.Errorf("no current document")
	}

	// Convert to JSON and back to handle type conversion
	data, err := json.Marshal(c.documents[c.current])
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// All decodes all documents
func (c *solrCursor) All(ctx context.Context, dest interface{}) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return fmt.Errorf("cursor is closed")
	}

	// Convert to JSON and back to handle type conversion
	data, err := json.Marshal(c.documents)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// Close closes the cursor
func (c *solrCursor) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	return nil
}

// Current returns the current document
func (c *solrCursor) Current() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed || c.current < 0 || c.current >= len(c.documents) {
		return nil
	}

	return c.documents[c.current]
}

// Err returns the last error
func (c *solrCursor) Err() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.err
}

// Solr-specific utility functions

// GetClient returns the underlying HTTP client
func (db *solrDatabase) GetClient() *http.Client {
	return db.client
}

// GetBaseURL returns the Solr base URL
func (db *solrDatabase) GetBaseURL() string {
	return db.baseURL
}

// ExecuteQuery executes a custom Solr query
func (db *solrDatabase) ExecuteQuery(ctx context.Context, core string, query map[string]interface{}) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Solr not connected")
	}

	reqURL := fmt.Sprintf("%s/%s/select", db.baseURL, core)
	params := url.Values{}

	for key, value := range query {
		params.Add(key, fmt.Sprintf("%v", value))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query failed: %s", resp.Status)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetCoreStatus returns core status information
func (db *solrDatabase) GetCoreStatus(ctx context.Context, core string) (map[string]interface{}, error) {
	if db.client == nil {
		return nil, fmt.Errorf("Solr not connected")
	}

	reqURL := fmt.Sprintf("%s/admin/cores?action=STATUS&core=%s", db.baseURL, core)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := db.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("core status failed: %s", resp.Status)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// Stats returns database statistics
func (db *solrDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}

	stats["driver"] = db.driver
	stats["connected"] = db.connected
	stats["base_url"] = db.baseURL
	stats["timeout"] = db.timeout

	return stats
}

// init function to register the Solr constructor
func init() {
	NewSolrDatabase = func(config NoSQLConfig) (NoSQLDatabase, error) {
		return newSolrDatabase(config)
	}
}
