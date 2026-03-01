package search

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// InMemorySearch implements Search interface with an in-memory store.
type InMemorySearch struct {
	config     Config
	logger     forge.Logger
	metrics    forge.Metrics
	connected  bool
	indexes    map[string]*memoryIndex
	mu         sync.RWMutex
	startTime  time.Time
	queryCount int64
}

// memoryIndex represents an in-memory search index.
type memoryIndex struct {
	name      string
	schema    IndexSchema
	documents map[string]Document
	createdAt time.Time
	updatedAt time.Time
	mu        sync.RWMutex
}

// NewInMemorySearch creates a new in-memory search instance.
func NewInMemorySearch(config Config, logger forge.Logger, metrics forge.Metrics) *InMemorySearch {
	return &InMemorySearch{
		config:  config,
		logger:  logger,
		metrics: metrics,
		indexes: make(map[string]*memoryIndex),
	}
}

// Connect establishes connection to the search backend.
// Connect is idempotent — calling it on an already-connected instance is a no-op.
func (s *InMemorySearch) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected {
		return nil
	}

	s.connected = true
	s.startTime = time.Now()
	s.logger.Info("connected to in-memory search")

	return nil
}

// Disconnect closes the connection to the search backend.
func (s *InMemorySearch) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return ErrNotConnected
	}

	s.connected = false
	s.indexes = make(map[string]*memoryIndex)
	s.logger.Info("disconnected from in-memory search")

	return nil
}

// Ping checks if the search backend is responsive.
func (s *InMemorySearch) Ping(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.connected {
		return ErrNotConnected
	}

	return nil
}

// CreateIndex creates a new search index.
func (s *InMemorySearch) CreateIndex(ctx context.Context, name string, schema IndexSchema) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return ErrNotConnected
	}

	if _, exists := s.indexes[name]; exists {
		return ErrIndexAlreadyExists
	}

	now := time.Now()
	s.indexes[name] = &memoryIndex{
		name:      name,
		schema:    schema,
		documents: make(map[string]Document),
		createdAt: now,
		updatedAt: now,
	}

	s.logger.Info("created search index",
		forge.F("index", name),
		forge.F("fields", len(schema.Fields)),
	)

	return nil
}

// DeleteIndex deletes a search index.
func (s *InMemorySearch) DeleteIndex(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return ErrNotConnected
	}

	if _, exists := s.indexes[name]; !exists {
		return ErrIndexNotFound
	}

	delete(s.indexes, name)

	s.logger.Info("deleted search index",
		forge.F("index", name),
	)

	return nil
}

// ListIndexes returns a list of all indexes.
func (s *InMemorySearch) ListIndexes(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.connected {
		return nil, ErrNotConnected
	}

	names := make([]string, 0, len(s.indexes))
	for name := range s.indexes {
		names = append(names, name)
	}

	return names, nil
}

// GetIndexInfo returns information about an index.
func (s *InMemorySearch) GetIndexInfo(ctx context.Context, name string) (*IndexInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.connected {
		return nil, ErrNotConnected
	}

	idx, exists := s.indexes[name]
	if !exists {
		return nil, ErrIndexNotFound
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return &IndexInfo{
		Name:          idx.name,
		DocumentCount: int64(len(idx.documents)),
		IndexSize:     int64(len(idx.documents) * 1024), // Approximate
		CreatedAt:     idx.createdAt,
		UpdatedAt:     idx.updatedAt,
		Schema:        idx.schema,
	}, nil
}

// Index adds or updates a document in the index.
func (s *InMemorySearch) Index(ctx context.Context, indexName string, doc Document) error {
	s.mu.RLock()
	idx, exists := s.indexes[indexName]
	s.mu.RUnlock()

	if !s.connected {
		return ErrNotConnected
	}

	if !exists {
		return ErrIndexNotFound
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.documents[doc.ID] = doc
	idx.updatedAt = time.Now()

	s.logger.Debug("indexed document",
		forge.F("index", indexName),
		forge.F("doc_id", doc.ID),
	)

	return nil
}

// BulkIndex adds or updates multiple documents.
func (s *InMemorySearch) BulkIndex(ctx context.Context, indexName string, docs []Document) error {
	s.mu.RLock()
	idx, exists := s.indexes[indexName]
	s.mu.RUnlock()

	if !s.connected {
		return ErrNotConnected
	}

	if !exists {
		return ErrIndexNotFound
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	for _, doc := range docs {
		idx.documents[doc.ID] = doc
	}

	idx.updatedAt = time.Now()

	s.logger.Info("bulk indexed documents",
		forge.F("index", indexName),
		forge.F("count", len(docs)),
	)

	return nil
}

// Get retrieves a document by ID.
func (s *InMemorySearch) Get(ctx context.Context, indexName string, id string) (*Document, error) {
	s.mu.RLock()
	idx, exists := s.indexes[indexName]
	s.mu.RUnlock()

	if !s.connected {
		return nil, ErrNotConnected
	}

	if !exists {
		return nil, ErrIndexNotFound
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	doc, exists := idx.documents[id]
	if !exists {
		return nil, ErrDocumentNotFound
	}

	return &doc, nil
}

// Delete removes a document from the index.
func (s *InMemorySearch) Delete(ctx context.Context, indexName string, id string) error {
	s.mu.RLock()
	idx, exists := s.indexes[indexName]
	s.mu.RUnlock()

	if !s.connected {
		return ErrNotConnected
	}

	if !exists {
		return ErrIndexNotFound
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if _, exists := idx.documents[id]; !exists {
		return ErrDocumentNotFound
	}

	delete(idx.documents, id)
	idx.updatedAt = time.Now()

	s.logger.Debug("deleted document",
		forge.F("index", indexName),
		forge.F("doc_id", id),
	)

	return nil
}

// Update updates a document in the index.
func (s *InMemorySearch) Update(ctx context.Context, indexName string, id string, doc Document) error {
	s.mu.RLock()
	idx, exists := s.indexes[indexName]
	s.mu.RUnlock()

	if !s.connected {
		return ErrNotConnected
	}

	if !exists {
		return ErrIndexNotFound
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if _, exists := idx.documents[id]; !exists {
		return ErrDocumentNotFound
	}

	doc.ID = id // Ensure ID matches
	idx.documents[id] = doc
	idx.updatedAt = time.Now()

	s.logger.Debug("updated document",
		forge.F("index", indexName),
		forge.F("doc_id", id),
	)

	return nil
}

// Search performs a search query.
func (s *InMemorySearch) Search(ctx context.Context, query SearchQuery) (*SearchResults, error) {
	startTime := time.Now()

	s.mu.RLock()
	idx, exists := s.indexes[query.Index]
	s.mu.RUnlock()

	if !s.connected {
		return nil, ErrNotConnected
	}

	if !exists {
		return nil, ErrIndexNotFound
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Simple full-text search implementation
	var hits []Hit

	queryLower := strings.ToLower(query.Query)

	for _, doc := range idx.documents {
		if s.matchesQuery(doc, queryLower, query) {
			score := s.calculateScore(doc, queryLower, query)
			if score >= query.MinScore {
				hit := Hit{
					ID:       doc.ID,
					Score:    score,
					Document: doc.Fields,
				}
				hits = append(hits, hit)
			}
		}
	}

	// Apply filters
	if len(query.Filters) > 0 {
		hits = s.applyFilters(hits, query.Filters)
	}

	// Sort results
	if len(query.Sort) > 0 {
		s.sortHits(hits, query.Sort)
	} else {
		// Default sort by score
		s.sortHitsByScore(hits)
	}

	// Apply pagination
	total := int64(len(hits))

	offset := min(query.Offset, len(hits))

	limit := query.Limit
	if limit == 0 {
		limit = s.config.DefaultLimit
	}

	if limit > s.config.MaxLimit {
		limit = s.config.MaxLimit
	}

	end := min(offset+limit, len(hits))

	if offset < len(hits) {
		hits = hits[offset:end]
	} else {
		hits = []Hit{}
	}

	// Increment query count
	s.mu.Lock()
	s.queryCount++
	s.mu.Unlock()

	return &SearchResults{
		Hits:           hits,
		Total:          total,
		Offset:         offset,
		Limit:          limit,
		ProcessingTime: time.Since(startTime),
		Query:          query.Query,
		Exhaustive:     true,
	}, nil
}

// matchesQuery checks if a document matches the search query.
func (s *InMemorySearch) matchesQuery(doc Document, queryLower string, query SearchQuery) bool {
	if queryLower == "" {
		return true
	}

	// Search in all fields
	for _, value := range doc.Fields {
		str := fmt.Sprintf("%v", value)
		if strings.Contains(strings.ToLower(str), queryLower) {
			return true
		}
	}

	return false
}

// calculateScore calculates relevance score for a document.
func (s *InMemorySearch) calculateScore(doc Document, queryLower string, query SearchQuery) float64 {
	score := 0.0

	for field, value := range doc.Fields {
		str := strings.ToLower(fmt.Sprintf("%v", value))
		if strings.Contains(str, queryLower) {
			// Base score
			fieldScore := 1.0

			// Apply field boost if specified
			if boost, ok := query.BoostFields[field]; ok {
				fieldScore *= boost
			}

			// Exact match bonus
			if str == queryLower {
				fieldScore *= 2.0
			}

			score += fieldScore
		}
	}

	return score
}

// applyFilters filters hits based on filter criteria.
func (s *InMemorySearch) applyFilters(hits []Hit, filters []Filter) []Hit {
	filtered := make([]Hit, 0, len(hits))

	for _, hit := range hits {
		matches := true

		for _, filter := range filters {
			if !s.matchesFilter(hit.Document, filter) {
				matches = false

				break
			}
		}

		if matches {
			filtered = append(filtered, hit)
		}
	}

	return filtered
}

// matchesFilter checks if a document matches a filter.
func (s *InMemorySearch) matchesFilter(doc map[string]any, filter Filter) bool {
	value, exists := doc[filter.Field]
	if !exists {
		return filter.Operator == "NOT EXISTS"
	}

	switch filter.Operator {
	case "=", "==":
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", filter.Value)
	case "!=":
		return fmt.Sprintf("%v", value) != fmt.Sprintf("%v", filter.Value)
	case "EXISTS":
		return true
	case "NOT EXISTS":
		return false
	default:
		return false
	}
}

// sortHits sorts hits by specified fields.
func (s *InMemorySearch) sortHits(hits []Hit, sortFields []SortField) {
	// Simple implementation - sort by first field only
	if len(sortFields) == 0 {
		return
	}

	// This is a simplified implementation
	// For production, use a proper sorting algorithm
}

// sortHitsByScore sorts hits by score in descending order.
func (s *InMemorySearch) sortHitsByScore(hits []Hit) {
	// Bubble sort for simplicity
	n := len(hits)
	for i := range n - 1 {
		for j := range n - i - 1 {
			if hits[j].Score < hits[j+1].Score {
				hits[j], hits[j+1] = hits[j+1], hits[j]
			}
		}
	}
}

// Suggest returns search suggestions.
func (s *InMemorySearch) Suggest(ctx context.Context, query SuggestQuery) (*SuggestResults, error) {
	startTime := time.Now()

	s.mu.RLock()
	idx, exists := s.indexes[query.Index]
	s.mu.RUnlock()

	if !s.connected {
		return nil, ErrNotConnected
	}

	if !exists {
		return nil, ErrIndexNotFound
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	suggestions := make([]Suggestion, 0)
	queryLower := strings.ToLower(query.Query)
	seen := make(map[string]bool)

	limit := query.Limit
	if limit == 0 {
		limit = 10
	}

	for _, doc := range idx.documents {
		if len(suggestions) >= limit {
			break
		}

		if value, ok := doc.Fields[query.Field]; ok {
			str := fmt.Sprintf("%v", value)
			strLower := strings.ToLower(str)

			if strings.Contains(strLower, queryLower) && !seen[str] {
				suggestions = append(suggestions, Suggestion{
					Text:  str,
					Score: 1.0,
				})
				seen[str] = true
			}
		}
	}

	return &SuggestResults{
		Suggestions:    suggestions,
		ProcessingTime: time.Since(startTime),
	}, nil
}

// Autocomplete returns autocomplete results.
func (s *InMemorySearch) Autocomplete(ctx context.Context, query AutocompleteQuery) (*AutocompleteResults, error) {
	startTime := time.Now()

	s.mu.RLock()
	idx, exists := s.indexes[query.Index]
	s.mu.RUnlock()

	if !s.connected {
		return nil, ErrNotConnected
	}

	if !exists {
		return nil, ErrIndexNotFound
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	completions := make([]Completion, 0)
	queryLower := strings.ToLower(query.Query)
	seen := make(map[string]bool)

	limit := query.Limit
	if limit == 0 {
		limit = 10
	}

	for _, doc := range idx.documents {
		if len(completions) >= limit {
			break
		}

		if value, ok := doc.Fields[query.Field]; ok {
			str := fmt.Sprintf("%v", value)
			strLower := strings.ToLower(str)

			if strings.HasPrefix(strLower, queryLower) && !seen[str] {
				completions = append(completions, Completion{
					Text:  str,
					Score: 1.0,
				})
				seen[str] = true
			}
		}
	}

	return &AutocompleteResults{
		Completions:    completions,
		ProcessingTime: time.Since(startTime),
	}, nil
}

// Stats returns search engine statistics.
func (s *InMemorySearch) Stats(ctx context.Context) (*SearchStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.connected {
		return nil, ErrNotConnected
	}

	totalDocs := int64(0)

	for _, idx := range s.indexes {
		idx.mu.RLock()
		totalDocs += int64(len(idx.documents))
		idx.mu.RUnlock()
	}

	uptime := time.Duration(0)
	if !s.startTime.IsZero() {
		uptime = time.Since(s.startTime)
	}

	return &SearchStats{
		IndexCount:    int64(len(s.indexes)),
		DocumentCount: totalDocs,
		TotalSize:     totalDocs * 1024, // Approximate
		Queries:       s.queryCount,
		AvgLatency:    5 * time.Millisecond,
		Uptime:        uptime,
		Version:       "inmemory-1.0.0",
	}, nil
}
