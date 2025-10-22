package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	metrics "github.com/xraph/forge/v2/internal/metrics/internal"
	"github.com/xraph/forge/v2/internal/shared"
)

// =============================================================================
// MEMORY STORAGE
// =============================================================================

// MemoryStorage provides in-memory storage for metrics
type MemoryStorage struct {
	name            string
	data            map[string]*MetricEntry
	mu              sync.RWMutex
	maxEntries      int
	retention       time.Duration
	cleanupInterval time.Duration
	started         bool
	stopCh          chan struct{}
	stats           *MemoryStorageStats
}

// MetricEntry represents a stored metric entry
type MetricEntry struct {
	Name        string                 `json:"name"`
	Type        shared.MetricType      `json:"type"`
	Value       interface{}            `json:"value"`
	Tags        map[string]string      `json:"tags"`
	Timestamp   time.Time              `json:"timestamp"`
	LastUpdated time.Time              `json:"last_updated"`
	AccessCount int64                  `json:"access_count"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// MemoryStorageConfig contains configuration for memory storage
type MemoryStorageConfig struct {
	MaxEntries      int           `yaml:"max_entries" json:"max_entries"`
	Retention       time.Duration `yaml:"retention" json:"retention"`
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	EnableStats     bool          `yaml:"enable_stats" json:"enable_stats"`
}

// MemoryStorageStats contains statistics about memory storage
type MemoryStorageStats struct {
	EntriesCount     int64         `json:"entries_count"`
	MaxEntries       int           `json:"max_entries"`
	StorageSize      int64         `json:"storage_size"`
	TotalWrites      int64         `json:"total_writes"`
	TotalReads       int64         `json:"total_reads"`
	TotalDeletes     int64         `json:"total_deletes"`
	CleanupRuns      int64         `json:"cleanup_runs"`
	LastCleanup      time.Time     `json:"last_cleanup"`
	AverageEntrySize float64       `json:"average_entry_size"`
	OldestEntry      time.Time     `json:"oldest_entry"`
	NewestEntry      time.Time     `json:"newest_entry"`
	MemoryUsage      int64         `json:"memory_usage"`
	HitRate          float64       `json:"hit_rate"`
	Uptime           time.Duration `json:"uptime"`
	startTime        time.Time
	hits             int64
	misses           int64
}

// DefaultMemoryStorageConfig returns default configuration
func DefaultMemoryStorageConfig() *MemoryStorageConfig {
	return &MemoryStorageConfig{
		MaxEntries:      100000,
		Retention:       time.Hour * 24,
		CleanupInterval: time.Hour,
		EnableStats:     true,
	}
}

// NewMemoryStorage creates a new memory storage instance
func NewMemoryStorage() MetricsStorage {
	return NewMemoryStorageWithConfig(DefaultMemoryStorageConfig())
}

// NewMemoryStorageWithConfig creates a new memory storage instance with configuration
func NewMemoryStorageWithConfig(config *MemoryStorageConfig) MetricsStorage {
	return &MemoryStorage{
		name:            "memory",
		data:            make(map[string]*MetricEntry),
		maxEntries:      config.MaxEntries,
		retention:       config.Retention,
		cleanupInterval: config.CleanupInterval,
		stopCh:          make(chan struct{}),
		stats: &MemoryStorageStats{
			MaxEntries: config.MaxEntries,
			startTime:  time.Now(),
		},
	}
}

// =============================================================================
// METRICS STORAGE INTERFACE IMPLEMENTATION
// =============================================================================

// Store stores a metric entry
func (ms *MemoryStorage) Store(ctx context.Context, entry *MetricEntry) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Check if we're at capacity
	if len(ms.data) >= ms.maxEntries {
		if err := ms.evictOldest(); err != nil {
			return fmt.Errorf("failed to evict oldest entry: %w", err)
		}
	}

	// Generate key
	key := ms.generateKey(entry.Name, entry.Tags)

	// Update existing entry or create new one
	if existing, exists := ms.data[key]; exists {
		existing.Value = entry.Value
		existing.LastUpdated = time.Now()
		existing.AccessCount++
	} else {
		entry.Timestamp = time.Now()
		entry.LastUpdated = time.Now()
		entry.AccessCount = 1
		ms.data[key] = entry
	}

	// Update stats
	ms.stats.TotalWrites++
	ms.stats.EntriesCount = int64(len(ms.data))
	ms.updateStorageStats()

	return nil
}

// Retrieve retrieves a metric entry
func (ms *MemoryStorage) Retrieve(ctx context.Context, name string, tags map[string]string) (*MetricEntry, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	key := ms.generateKey(name, tags)

	if entry, exists := ms.data[key]; exists {
		// Update access statistics
		entry.AccessCount++
		ms.stats.TotalReads++
		ms.stats.hits++

		// Return a copy to prevent external modification
		return ms.copyEntry(entry), nil
	}

	ms.stats.misses++
	return nil, fmt.Errorf("metric not found: %s", key)
}

// Delete deletes a metric entry
func (ms *MemoryStorage) Delete(ctx context.Context, name string, tags map[string]string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := ms.generateKey(name, tags)

	if _, exists := ms.data[key]; exists {
		delete(ms.data, key)
		ms.stats.TotalDeletes++
		ms.stats.EntriesCount = int64(len(ms.data))
		ms.updateStorageStats()
		return nil
	}

	return fmt.Errorf("metric not found: %s", key)
}

// List lists all metric entries
func (ms *MemoryStorage) List(ctx context.Context, filters map[string]string) ([]*MetricEntry, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var entries []*MetricEntry

	for _, entry := range ms.data {
		if ms.matchesFilters(entry, filters) {
			entries = append(entries, ms.copyEntry(entry))
		}
	}

	ms.stats.TotalReads += int64(len(entries))
	return entries, nil
}

// Count returns the number of stored entries
func (ms *MemoryStorage) Count(ctx context.Context) (int64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	return int64(len(ms.data)), nil
}

// Clear clears all stored entries
func (ms *MemoryStorage) Clear(ctx context.Context) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.data = make(map[string]*MetricEntry)
	ms.stats.EntriesCount = 0
	ms.updateStorageStats()

	return nil
}

// Start starts the storage system
func (ms *MemoryStorage) Start(ctx context.Context) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.started {
		return fmt.Errorf("memory storage already started")
	}

	ms.started = true
	ms.stats.startTime = time.Now()

	// Start cleanup goroutine
	go ms.cleanupLoop()

	return nil
}

// Stop stops the storage system
func (ms *MemoryStorage) Stop(ctx context.Context) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if !ms.started {
		return fmt.Errorf("memory storage not started")
	}

	ms.started = false
	close(ms.stopCh)

	return nil
}

// Health checks the health of the storage system
func (ms *MemoryStorage) Health(ctx context.Context) error {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if !ms.started {
		return fmt.Errorf("memory storage not started")
	}

	// Check if storage is within limits
	if len(ms.data) > ms.maxEntries {
		return fmt.Errorf("storage exceeded maximum entries: %d > %d", len(ms.data), ms.maxEntries)
	}

	return nil
}

// Stats returns storage statistics
func (ms *MemoryStorage) Stats(ctx context.Context) (interface{}, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	// Update dynamic stats
	stats := *ms.stats
	stats.Uptime = time.Since(stats.startTime)

	if stats.hits+stats.misses > 0 {
		stats.HitRate = float64(stats.hits) / float64(stats.hits+stats.misses) * 100
	}

	return stats, nil
}

// =============================================================================
// ADVANCED OPERATIONS
// =============================================================================

// Query performs a query on stored metrics
func (ms *MemoryStorage) Query(ctx context.Context, query MetricsQuery) (*QueryResult, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var entries []*MetricEntry

	for _, entry := range ms.data {
		if ms.matchesQuery(entry, query) {
			entries = append(entries, ms.copyEntry(entry))
		}
	}

	// Apply sorting
	ms.sortEntries(entries, query.SortBy, query.SortOrder)

	// Apply pagination
	start := query.Offset
	end := query.Offset + query.Limit

	if start >= len(entries) {
		entries = []*MetricEntry{}
	} else {
		if end > len(entries) {
			end = len(entries)
		}
		entries = entries[start:end]
	}

	result := &QueryResult{
		Entries:    entries,
		TotalCount: len(entries),
		Query:      query,
		Timestamp:  time.Now(),
	}

	return result, nil
}

// Aggregate performs aggregation on stored metrics
func (ms *MemoryStorage) Aggregate(ctx context.Context, aggregation MetricsAggregation) (*AggregationResult, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var values []float64
	var entries []*MetricEntry

	for _, entry := range ms.data {
		if ms.matchesFilters(entry, aggregation.Filters) {
			entries = append(entries, entry)

			// Extract numeric value for aggregation
			if val, ok := ms.extractNumericValue(entry.Value); ok {
				values = append(values, val)
			}
		}
	}

	result := &AggregationResult{
		Function:  aggregation.Function,
		Filters:   aggregation.Filters,
		Count:     len(entries),
		Timestamp: time.Now(),
	}

	// Perform aggregation
	switch aggregation.Function {
	case "sum":
		result.Value = ms.sum(values)
	case "avg", "mean":
		result.Value = ms.avg(values)
	case "min":
		result.Value = ms.min(values)
	case "max":
		result.Value = ms.max(values)
	case "count":
		result.Value = float64(len(entries))
	default:
		return nil, fmt.Errorf("unsupported aggregation function: %s", aggregation.Function)
	}

	return result, nil
}

// Backup creates a backup of all stored metrics
func (ms *MemoryStorage) Backup(ctx context.Context) ([]byte, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	backup := &MemoryStorageBackup{
		Timestamp: time.Now(),
		Entries:   make([]*MetricEntry, 0, len(ms.data)),
		Stats:     *ms.stats,
	}

	for _, entry := range ms.data {
		backup.Entries = append(backup.Entries, ms.copyEntry(entry))
	}

	return ms.serializeBackup(backup)
}

// Restore restores metrics from a backup
func (ms *MemoryStorage) Restore(ctx context.Context, data []byte) error {
	backup, err := ms.deserializeBackup(data)
	if err != nil {
		return fmt.Errorf("failed to deserialize backup: %w", err)
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Clear existing data
	ms.data = make(map[string]*MetricEntry)

	// Restore entries
	for _, entry := range backup.Entries {
		key := ms.generateKey(entry.Name, entry.Tags)
		ms.data[key] = entry
	}

	// Update stats
	ms.stats.EntriesCount = int64(len(ms.data))
	ms.updateStorageStats()

	return nil
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// generateKey generates a unique key for a metric entry
func (ms *MemoryStorage) generateKey(name string, tags map[string]string) string {
	if len(tags) == 0 {
		return name
	}

	return name + "{" + metrics.TagsToString(tags) + "}"
}

// copyEntry creates a copy of a metric entry
func (ms *MemoryStorage) copyEntry(entry *MetricEntry) *MetricEntry {
	// Create a deep copy of tags
	tags := make(map[string]string)
	for k, v := range entry.Tags {
		tags[k] = v
	}

	// Create a copy of metadata
	metadata := make(map[string]interface{})
	for k, v := range entry.Metadata {
		metadata[k] = v
	}

	return &MetricEntry{
		Name:        entry.Name,
		Type:        entry.Type,
		Value:       entry.Value,
		Tags:        tags,
		Timestamp:   entry.Timestamp,
		LastUpdated: entry.LastUpdated,
		AccessCount: entry.AccessCount,
		Metadata:    metadata,
	}
}

// matchesFilters checks if an entry matches the given filters
func (ms *MemoryStorage) matchesFilters(entry *MetricEntry, filters map[string]string) bool {
	for key, value := range filters {
		switch key {
		case "name":
			if entry.Name != value {
				return false
			}
		case "type":
			if string(entry.Type) != value {
				return false
			}
		default:
			// Check tags
			if tagValue, exists := entry.Tags[key]; !exists || tagValue != value {
				return false
			}
		}
	}

	return true
}

// matchesQuery checks if an entry matches a query
func (ms *MemoryStorage) matchesQuery(entry *MetricEntry, query MetricsQuery) bool {
	// Check basic filters
	if !ms.matchesFilters(entry, query.Filters) {
		return false
	}

	// Check time range
	if !query.StartTime.IsZero() && entry.Timestamp.Before(query.StartTime) {
		return false
	}

	if !query.EndTime.IsZero() && entry.Timestamp.After(query.EndTime) {
		return false
	}

	return true
}

// sortEntries sorts entries based on the given criteria
func (ms *MemoryStorage) sortEntries(entries []*MetricEntry, sortBy, sortOrder string) {
	if sortBy == "" {
		sortBy = "timestamp"
	}

	ascending := sortOrder != "desc"

	sort.Slice(entries, func(i, j int) bool {
		var less bool

		switch sortBy {
		case "name":
			less = entries[i].Name < entries[j].Name
		case "timestamp":
			less = entries[i].Timestamp.Before(entries[j].Timestamp)
		case "last_updated":
			less = entries[i].LastUpdated.Before(entries[j].LastUpdated)
		case "access_count":
			less = entries[i].AccessCount < entries[j].AccessCount
		default:
			less = entries[i].Timestamp.Before(entries[j].Timestamp)
		}

		if ascending {
			return less
		}
		return !less
	})
}

// evictOldest evicts the oldest entry to make space
func (ms *MemoryStorage) evictOldest() error {
	if len(ms.data) == 0 {
		return nil
	}

	var oldestKey string
	var oldestTime time.Time

	for key, entry := range ms.data {
		if oldestTime.IsZero() || entry.Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Timestamp
		}
	}

	if oldestKey != "" {
		delete(ms.data, oldestKey)
		ms.stats.TotalDeletes++
	}

	return nil
}

// cleanupLoop runs periodic cleanup
func (ms *MemoryStorage) cleanupLoop() {
	ticker := time.NewTicker(ms.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ms.cleanup()
		case <-ms.stopCh:
			return
		}
	}
}

// cleanup removes expired entries
func (ms *MemoryStorage) cleanup() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.retention <= 0 {
		return
	}

	cutoff := time.Now().Add(-ms.retention)
	var deletedCount int

	for key, entry := range ms.data {
		if entry.Timestamp.Before(cutoff) {
			delete(ms.data, key)
			deletedCount++
		}
	}

	ms.stats.CleanupRuns++
	ms.stats.LastCleanup = time.Now()
	ms.stats.EntriesCount = int64(len(ms.data))
	ms.stats.TotalDeletes += int64(deletedCount)

	ms.updateStorageStats()
}

// updateStorageStats updates storage statistics
func (ms *MemoryStorage) updateStorageStats() {
	if len(ms.data) == 0 {
		ms.stats.OldestEntry = time.Time{}
		ms.stats.NewestEntry = time.Time{}
		ms.stats.AverageEntrySize = 0
		return
	}

	var oldest, newest time.Time
	var totalSize int64

	for _, entry := range ms.data {
		if oldest.IsZero() || entry.Timestamp.Before(oldest) {
			oldest = entry.Timestamp
		}
		if newest.IsZero() || entry.Timestamp.After(newest) {
			newest = entry.Timestamp
		}

		// Estimate entry size
		totalSize += ms.estimateEntrySize(entry)
	}

	ms.stats.OldestEntry = oldest
	ms.stats.NewestEntry = newest
	ms.stats.MemoryUsage = totalSize
	ms.stats.AverageEntrySize = float64(totalSize) / float64(len(ms.data))
}

// estimateEntrySize estimates the size of a metric entry
func (ms *MemoryStorage) estimateEntrySize(entry *MetricEntry) int64 {
	size := int64(len(entry.Name))
	size += int64(len(entry.Type))

	// Estimate value size
	switch v := entry.Value.(type) {
	case string:
		size += int64(len(v))
	case int64, uint64, float64:
		size += 8
	case bool:
		size += 1
	default:
		size += 8 // Default estimate
	}

	// Add tag sizes
	for k, v := range entry.Tags {
		size += int64(len(k) + len(v))
	}

	// Add metadata size estimate
	size += int64(len(entry.Metadata) * 16)

	return size
}

// extractNumericValue extracts numeric value from interface{}
func (ms *MemoryStorage) extractNumericValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

// Aggregation functions
func (ms *MemoryStorage) sum(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum
}

func (ms *MemoryStorage) avg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return ms.sum(values) / float64(len(values))
}

func (ms *MemoryStorage) min(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func (ms *MemoryStorage) max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

// serializeBackup serializes a backup to bytes
func (ms *MemoryStorage) serializeBackup(backup *MemoryStorageBackup) ([]byte, error) {
	// This is a placeholder - in a real implementation, use JSON/protobuf/etc.
	return []byte("backup"), nil
}

// deserializeBackup deserializes bytes to a backup
func (ms *MemoryStorage) deserializeBackup(data []byte) (*MemoryStorageBackup, error) {
	// This is a placeholder - in a real implementation, use JSON/protobuf/etc.
	return &MemoryStorageBackup{}, nil
}

// =============================================================================
// SUPPORTING TYPES
// =============================================================================

// MetricsStorage defines the interface for metrics storage
type MetricsStorage interface {
	Store(ctx context.Context, entry *MetricEntry) error
	Retrieve(ctx context.Context, name string, tags map[string]string) (*MetricEntry, error)
	Delete(ctx context.Context, name string, tags map[string]string) error
	List(ctx context.Context, filters map[string]string) ([]*MetricEntry, error)
	Count(ctx context.Context) (int64, error)
	Clear(ctx context.Context) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health(ctx context.Context) error
	Stats(ctx context.Context) (interface{}, error)
}

// MetricsQuery represents a query for metrics
type MetricsQuery struct {
	Filters   map[string]string `json:"filters"`
	StartTime time.Time         `json:"start_time"`
	EndTime   time.Time         `json:"end_time"`
	SortBy    string            `json:"sort_by"`
	SortOrder string            `json:"sort_order"`
	Limit     int               `json:"limit"`
	Offset    int               `json:"offset"`
}

// QueryResult represents the result of a query
type QueryResult struct {
	Entries    []*MetricEntry `json:"entries"`
	TotalCount int            `json:"total_count"`
	Query      MetricsQuery   `json:"query"`
	Timestamp  time.Time      `json:"timestamp"`
}

// MetricsAggregation represents an aggregation operation
type MetricsAggregation struct {
	Function string            `json:"function"`
	Filters  map[string]string `json:"filters"`
	GroupBy  []string          `json:"group_by"`
}

// AggregationResult represents the result of an aggregation
type AggregationResult struct {
	Function  string            `json:"function"`
	Filters   map[string]string `json:"filters"`
	Value     float64           `json:"value"`
	Count     int               `json:"count"`
	Timestamp time.Time         `json:"timestamp"`
}

// MemoryStorageBackup represents a backup of memory storage
type MemoryStorageBackup struct {
	Timestamp time.Time          `json:"timestamp"`
	Entries   []*MetricEntry     `json:"entries"`
	Stats     MemoryStorageStats `json:"stats"`
}
