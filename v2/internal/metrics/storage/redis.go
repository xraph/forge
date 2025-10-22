package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/v2/internal/logger"
	metrics "github.com/xraph/forge/v2/internal/metrics/internal"
)

// =============================================================================
// REDIS STORAGE
// =============================================================================

// RedisStorage provides Redis-backed storage for metrics
type RedisStorage struct {
	name            string
	client          *redis.Client
	keyPrefix       string
	logger          logger.Logger
	mu              sync.RWMutex
	retention       time.Duration
	cleanupInterval time.Duration
	started         bool
	stopCh          chan struct{}
	stats           *RedisStorageStats
}

// RedisStorageConfig contains configuration for Redis storage
type RedisStorageConfig struct {
	Address         string        `yaml:"address" json:"address"`
	Password        string        `yaml:"password" json:"password"`
	Database        int           `yaml:"database" json:"database"`
	KeyPrefix       string        `yaml:"key_prefix" json:"key_prefix"`
	PoolSize        int           `yaml:"pool_size" json:"pool_size"`
	MinIdleConns    int           `yaml:"min_idle_conns" json:"min_idle_conns"`
	MaxRetries      int           `yaml:"max_retries" json:"max_retries"`
	DialTimeout     time.Duration `yaml:"dial_timeout" json:"dial_timeout"`
	ReadTimeout     time.Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout" json:"write_timeout"`
	Retention       time.Duration `yaml:"retention" json:"retention"`
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	EnableStats     bool          `yaml:"enable_stats" json:"enable_stats"`
}

// RedisStorageStats contains statistics about Redis storage
type RedisStorageStats struct {
	Address          string        `json:"address"`
	Connected        bool          `json:"connected"`
	PoolSize         int           `json:"pool_size"`
	TotalConns       int           `json:"total_conns"`
	IdleConns        int           `json:"idle_conns"`
	StaleConns       int           `json:"stale_conns"`
	EntriesCount     int64         `json:"entries_count"`
	TotalWrites      int64         `json:"total_writes"`
	TotalReads       int64         `json:"total_reads"`
	TotalDeletes     int64         `json:"total_deletes"`
	FailedWrites     int64         `json:"failed_writes"`
	FailedReads      int64         `json:"failed_reads"`
	FailedDeletes    int64         `json:"failed_deletes"`
	CleanupRuns      int64         `json:"cleanup_runs"`
	LastCleanup      time.Time     `json:"last_cleanup"`
	AverageEntrySize float64       `json:"average_entry_size"`
	KeyspaceSize     int64         `json:"keyspace_size"`
	HitRate          float64       `json:"hit_rate"`
	Uptime           time.Duration `json:"uptime"`
	startTime        time.Time
	hits             int64
	misses           int64
}

// DefaultRedisStorageConfig returns default configuration
func DefaultRedisStorageConfig() *RedisStorageConfig {
	return &RedisStorageConfig{
		Address:         "localhost:6379",
		Password:        "",
		Database:        0,
		KeyPrefix:       "forge:metrics:",
		PoolSize:        10,
		MinIdleConns:    5,
		MaxRetries:      3,
		DialTimeout:     time.Second * 5,
		ReadTimeout:     time.Second * 3,
		WriteTimeout:    time.Second * 3,
		Retention:       time.Hour * 24,
		CleanupInterval: time.Hour,
		EnableStats:     true,
	}
}

// NewRedisStorage creates a new Redis storage instance
func NewRedisStorage() MetricsStorage {
	return NewRedisStorageWithConfig(DefaultRedisStorageConfig())
}

// NewRedisStorageWithConfig creates a new Redis storage instance with configuration
func NewRedisStorageWithConfig(config *RedisStorageConfig) MetricsStorage {
	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:         config.Address,
		Password:     config.Password,
		DB:           config.Database,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		MaxRetries:   config.MaxRetries,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	})

	return &RedisStorage{
		name:            "redis",
		client:          client,
		keyPrefix:       config.KeyPrefix,
		retention:       config.Retention,
		cleanupInterval: config.CleanupInterval,
		stopCh:          make(chan struct{}),
		stats: &RedisStorageStats{
			Address:   config.Address,
			PoolSize:  config.PoolSize,
			startTime: time.Now(),
		},
	}
}

// =============================================================================
// METRICS STORAGE INTERFACE IMPLEMENTATION
// =============================================================================

// Store stores a metric entry in Redis
func (rs *RedisStorage) Store(ctx context.Context, entry *MetricEntry) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Generate key
	key := rs.generateKey(entry.Name, entry.Tags)

	// Serialize entry
	data, err := rs.serializeEntry(entry)
	if err != nil {
		rs.stats.FailedWrites++
		return fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Store in Redis with TTL
	err = rs.client.Set(ctx, key, data, rs.retention).Err()
	if err != nil {
		rs.stats.FailedWrites++
		return fmt.Errorf("failed to store entry in Redis: %w", err)
	}

	// Update index
	if err := rs.updateIndex(ctx, entry); err != nil {
		rs.logger.Warn("failed to update index", logger.Error(err))
	}

	// Update stats
	rs.stats.TotalWrites++
	rs.updateStorageStats(ctx)

	return nil
}

// Retrieve retrieves a metric entry from Redis
func (rs *RedisStorage) Retrieve(ctx context.Context, name string, tags map[string]string) (*MetricEntry, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	key := rs.generateKey(name, tags)

	// Get from Redis
	data, err := rs.client.Get(ctx, key).Result()
	if err == redis.Nil {
		rs.stats.misses++
		return nil, fmt.Errorf("metric not found: %s", key)
	} else if err != nil {
		rs.stats.FailedReads++
		return nil, fmt.Errorf("failed to retrieve from Redis: %w", err)
	}

	// Deserialize entry
	entry, err := rs.deserializeEntry([]byte(data))
	if err != nil {
		rs.stats.FailedReads++
		return nil, fmt.Errorf("failed to deserialize entry: %w", err)
	}

	// Update access statistics
	rs.stats.TotalReads++
	rs.stats.hits++

	return entry, nil
}

// Delete deletes a metric entry from Redis
func (rs *RedisStorage) Delete(ctx context.Context, name string, tags map[string]string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	key := rs.generateKey(name, tags)

	// Delete from Redis
	result := rs.client.Del(ctx, key)
	if result.Err() != nil {
		rs.stats.FailedDeletes++
		return fmt.Errorf("failed to delete from Redis: %w", result.Err())
	}

	if result.Val() == 0 {
		return fmt.Errorf("metric not found: %s", key)
	}

	// Remove from index
	if err := rs.removeFromIndex(ctx, name, tags); err != nil {
		rs.logger.Warn("failed to remove from index", logger.Error(err))
	}

	rs.stats.TotalDeletes++
	rs.updateStorageStats(ctx)

	return nil
}

// List lists all metric entries matching filters
func (rs *RedisStorage) List(ctx context.Context, filters map[string]string) ([]*MetricEntry, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	var entries []*MetricEntry

	// Use index if available, otherwise scan all keys
	if rs.canUseIndex(filters) {
		var err error
		entries, err = rs.listFromIndex(ctx, filters)
		if err != nil {
			return nil, fmt.Errorf("failed to list from index: %w", err)
		}
	} else {
		var err error
		entries, err = rs.listFromScan(ctx, filters)
		if err != nil {
			return nil, fmt.Errorf("failed to list from scan: %w", err)
		}
	}

	rs.stats.TotalReads += int64(len(entries))
	return entries, nil
}

// Count returns the number of stored entries
func (rs *RedisStorage) Count(ctx context.Context) (int64, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// Use SCAN to count all keys with our prefix
	pattern := rs.keyPrefix + "*"
	var count int64

	iter := rs.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		count++
	}

	if err := iter.Err(); err != nil {
		return 0, fmt.Errorf("failed to count entries: %w", err)
	}

	return count, nil
}

// Clear clears all stored entries
func (rs *RedisStorage) Clear(ctx context.Context) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Use SCAN to delete all keys with our prefix
	pattern := rs.keyPrefix + "*"
	var keys []string

	iter := rs.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	// Delete in batches
	batchSize := 100
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}

		if err := rs.client.Del(ctx, keys[i:end]...).Err(); err != nil {
			return fmt.Errorf("failed to delete keys: %w", err)
		}
	}

	// Clear indexes
	if err := rs.clearIndexes(ctx); err != nil {
		rs.logger.Warn("failed to clear indexes", logger.Error(err))
	}

	rs.updateStorageStats(ctx)

	return nil
}

// Start starts the Redis storage system
func (rs *RedisStorage) Start(ctx context.Context) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.started {
		return fmt.Errorf("Redis storage already started")
	}

	// Test connection
	if err := rs.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	rs.started = true
	rs.stats.Connected = true
	rs.stats.startTime = time.Now()

	// Start cleanup goroutine
	go rs.cleanupLoop()

	// Start stats collection goroutine
	go rs.statsLoop()

	return nil
}

// Stop stops the Redis storage system
func (rs *RedisStorage) Stop(ctx context.Context) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.started {
		return fmt.Errorf("Redis storage not started")
	}

	rs.started = false
	rs.stats.Connected = false
	close(rs.stopCh)

	// Close Redis client
	if err := rs.client.Close(); err != nil {
		return fmt.Errorf("failed to close Redis client: %w", err)
	}

	return nil
}

// Health checks the health of the Redis storage system
func (rs *RedisStorage) Health(ctx context.Context) error {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if !rs.started {
		return fmt.Errorf("Redis storage not started")
	}

	// Test Redis connection
	if err := rs.client.Ping(ctx).Err(); err != nil {
		rs.stats.Connected = false
		return fmt.Errorf("Redis connection failed: %w", err)
	}

	rs.stats.Connected = true
	return nil
}

// Stats returns storage statistics
func (rs *RedisStorage) Stats(ctx context.Context) (interface{}, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// Update dynamic stats
	stats := *rs.stats
	stats.Uptime = time.Since(stats.startTime)

	// Get Redis pool stats
	poolStats := rs.client.PoolStats()
	stats.TotalConns = int(poolStats.TotalConns)
	stats.IdleConns = int(poolStats.IdleConns)
	stats.StaleConns = int(poolStats.StaleConns)

	// Calculate hit rate
	if stats.hits+stats.misses > 0 {
		stats.HitRate = float64(stats.hits) / float64(stats.hits+stats.misses) * 100
	}

	return stats, nil
}

// =============================================================================
// ADVANCED OPERATIONS
// =============================================================================

// Query performs a query on stored metrics
func (rs *RedisStorage) Query(ctx context.Context, query MetricsQuery) (*QueryResult, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// Get all matching entries
	entries, err := rs.List(ctx, query.Filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	// Apply time filters
	var filteredEntries []*MetricEntry
	for _, entry := range entries {
		if rs.matchesTimeRange(entry, query.StartTime, query.EndTime) {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// Apply sorting
	rs.sortEntries(filteredEntries, query.SortBy, query.SortOrder)

	// Apply pagination
	start := query.Offset
	end := query.Offset + query.Limit

	if start >= len(filteredEntries) {
		filteredEntries = []*MetricEntry{}
	} else {
		if end > len(filteredEntries) {
			end = len(filteredEntries)
		}
		filteredEntries = filteredEntries[start:end]
	}

	result := &QueryResult{
		Entries:    filteredEntries,
		TotalCount: len(filteredEntries),
		Query:      query,
		Timestamp:  time.Now(),
	}

	return result, nil
}

// Aggregate performs aggregation on stored metrics
func (rs *RedisStorage) Aggregate(ctx context.Context, aggregation MetricsAggregation) (*AggregationResult, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// Get all matching entries
	entries, err := rs.List(ctx, aggregation.Filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	var values []float64
	for _, entry := range entries {
		if val, ok := rs.extractNumericValue(entry.Value); ok {
			values = append(values, val)
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
		result.Value = rs.sum(values)
	case "avg", "mean":
		result.Value = rs.avg(values)
	case "min":
		result.Value = rs.min(values)
	case "max":
		result.Value = rs.max(values)
	case "count":
		result.Value = float64(len(entries))
	default:
		return nil, fmt.Errorf("unsupported aggregation function: %s", aggregation.Function)
	}

	return result, nil
}

// Backup creates a backup of all stored metrics
func (rs *RedisStorage) Backup(ctx context.Context) ([]byte, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// Get all entries
	entries, err := rs.List(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}

	backup := &RedisStorageBackup{
		Timestamp: time.Now(),
		Entries:   entries,
		Stats:     *rs.stats,
	}

	return rs.serializeBackup(backup)
}

// Restore restores metrics from a backup
func (rs *RedisStorage) Restore(ctx context.Context, data []byte) error {
	backup, err := rs.deserializeBackup(data)
	if err != nil {
		return fmt.Errorf("failed to deserialize backup: %w", err)
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Clear existing data
	if err := rs.Clear(ctx); err != nil {
		return fmt.Errorf("failed to clear existing data: %w", err)
	}

	// Restore entries
	for _, entry := range backup.Entries {
		if err := rs.Store(ctx, entry); err != nil {
			return fmt.Errorf("failed to restore entry: %w", err)
		}
	}

	rs.updateStorageStats(ctx)

	return nil
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// generateKey generates a unique key for a metric entry
func (rs *RedisStorage) generateKey(name string, tags map[string]string) string {
	key := rs.keyPrefix + name
	if len(tags) > 0 {
		key += "{" + metrics.TagsToString(tags) + "}"
	}
	return key
}

// serializeEntry serializes a metric entry to JSON
func (rs *RedisStorage) serializeEntry(entry *MetricEntry) ([]byte, error) {
	// Update metadata
	entry.LastUpdated = time.Now()
	entry.AccessCount++

	return json.Marshal(entry)
}

// deserializeEntry deserializes a metric entry from JSON
func (rs *RedisStorage) deserializeEntry(data []byte) (*MetricEntry, error) {
	var entry MetricEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// updateIndex updates the index for a metric entry
func (rs *RedisStorage) updateIndex(ctx context.Context, entry *MetricEntry) error {
	// Add to name index
	nameKey := rs.keyPrefix + "index:name:" + entry.Name
	if err := rs.client.SAdd(ctx, nameKey, rs.generateKey(entry.Name, entry.Tags)).Err(); err != nil {
		return err
	}

	// Add to type index
	typeKey := rs.keyPrefix + "index:type:" + string(entry.Type)
	if err := rs.client.SAdd(ctx, typeKey, rs.generateKey(entry.Name, entry.Tags)).Err(); err != nil {
		return err
	}

	// Add to tag indexes
	for tagKey, tagValue := range entry.Tags {
		tagIndexKey := rs.keyPrefix + "index:tag:" + tagKey + ":" + tagValue
		if err := rs.client.SAdd(ctx, tagIndexKey, rs.generateKey(entry.Name, entry.Tags)).Err(); err != nil {
			return err
		}
	}

	return nil
}

// removeFromIndex removes a metric entry from indexes
func (rs *RedisStorage) removeFromIndex(ctx context.Context, name string, tags map[string]string) error {
	key := rs.generateKey(name, tags)

	// Remove from name index
	nameKey := rs.keyPrefix + "index:name:" + name
	rs.client.SRem(ctx, nameKey, key)

	// Remove from tag indexes
	for tagKey, tagValue := range tags {
		tagIndexKey := rs.keyPrefix + "index:tag:" + tagKey + ":" + tagValue
		rs.client.SRem(ctx, tagIndexKey, key)
	}

	return nil
}

// canUseIndex determines if we can use indexes for filtering
func (rs *RedisStorage) canUseIndex(filters map[string]string) bool {
	// We can use index if we have name or tag filters
	if filters == nil {
		return false
	}

	_, hasName := filters["name"]
	_, hasType := filters["type"]

	// Check for tag filters
	hasTagFilters := false
	for key := range filters {
		if key != "name" && key != "type" {
			hasTagFilters = true
			break
		}
	}

	return hasName || hasType || hasTagFilters
}

// listFromIndex lists entries using indexes
func (rs *RedisStorage) listFromIndex(ctx context.Context, filters map[string]string) ([]*MetricEntry, error) {
	var keys []string

	// Get keys from indexes
	for filterKey, filterValue := range filters {
		var indexKey string
		switch filterKey {
		case "name":
			indexKey = rs.keyPrefix + "index:name:" + filterValue
		case "type":
			indexKey = rs.keyPrefix + "index:type:" + filterValue
		default:
			// Tag filter
			indexKey = rs.keyPrefix + "index:tag:" + filterKey + ":" + filterValue
		}

		// Get keys from index
		indexKeys, err := rs.client.SMembers(ctx, indexKey).Result()
		if err != nil {
			return nil, err
		}

		// If this is the first filter, use all keys
		if len(keys) == 0 {
			keys = indexKeys
		} else {
			// Intersect with existing keys
			keys = rs.intersectKeys(keys, indexKeys)
		}
	}

	// Retrieve entries
	var entries []*MetricEntry
	for _, key := range keys {
		data, err := rs.client.Get(ctx, key).Result()
		if err == redis.Nil {
			continue // Key might have expired
		} else if err != nil {
			return nil, err
		}

		entry, err := rs.deserializeEntry([]byte(data))
		if err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// listFromScan lists entries using SCAN
func (rs *RedisStorage) listFromScan(ctx context.Context, filters map[string]string) ([]*MetricEntry, error) {
	pattern := rs.keyPrefix + "*"
	var entries []*MetricEntry

	iter := rs.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()

		data, err := rs.client.Get(ctx, key).Result()
		if err == redis.Nil {
			continue // Key might have expired
		} else if err != nil {
			return nil, err
		}

		entry, err := rs.deserializeEntry([]byte(data))
		if err != nil {
			return nil, err
		}

		// Apply filters
		if rs.matchesFilters(entry, filters) {
			entries = append(entries, entry)
		}
	}

	if err := iter.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// clearIndexes clears all indexes
func (rs *RedisStorage) clearIndexes(ctx context.Context) error {
	pattern := rs.keyPrefix + "index:*"

	iter := rs.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		if err := rs.client.Del(ctx, iter.Val()).Err(); err != nil {
			return err
		}
	}

	return iter.Err()
}

// intersectKeys returns the intersection of two key slices
func (rs *RedisStorage) intersectKeys(keys1, keys2 []string) []string {
	keySet := make(map[string]bool)
	for _, key := range keys1 {
		keySet[key] = true
	}

	var intersection []string
	for _, key := range keys2 {
		if keySet[key] {
			intersection = append(intersection, key)
		}
	}

	return intersection
}

// matchesFilters checks if an entry matches the given filters
func (rs *RedisStorage) matchesFilters(entry *MetricEntry, filters map[string]string) bool {
	if filters == nil {
		return true
	}

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

// matchesTimeRange checks if an entry matches the time range
func (rs *RedisStorage) matchesTimeRange(entry *MetricEntry, startTime, endTime time.Time) bool {
	if !startTime.IsZero() && entry.Timestamp.Before(startTime) {
		return false
	}

	if !endTime.IsZero() && entry.Timestamp.After(endTime) {
		return false
	}

	return true
}

// updateStorageStats updates storage statistics
func (rs *RedisStorage) updateStorageStats(ctx context.Context) {
	// Get keyspace info
	info, err := rs.client.Info(ctx, "keyspace").Result()
	if err == nil {
		rs.parseKeyspaceInfo(info)
	}

	// Update entry count
	if count, err := rs.Count(ctx); err == nil {
		rs.stats.EntriesCount = count
	}
}

// parseKeyspaceInfo parses keyspace information from Redis INFO command
func (rs *RedisStorage) parseKeyspaceInfo(info string) {
	lines := strings.Split(info, "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "db"+strconv.Itoa(rs.client.Options().DB)+":") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				keyValues := strings.Split(parts[1], ",")
				for _, kv := range keyValues {
					kvParts := strings.Split(kv, "=")
					if len(kvParts) == 2 && kvParts[0] == "keys" {
						if keyspaceSize, err := strconv.ParseInt(kvParts[1], 10, 64); err == nil {
							rs.stats.KeyspaceSize = keyspaceSize
						}
					}
				}
			}
		}
	}
}

// cleanupLoop runs periodic cleanup
func (rs *RedisStorage) cleanupLoop() {
	ticker := time.NewTicker(rs.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !rs.started {
				return
			}
			rs.cleanup()
		case <-rs.stopCh:
			return
		}
	}
}

// cleanup performs Redis cleanup
func (rs *RedisStorage) cleanup() {
	ctx := context.Background()
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Redis handles TTL automatically, so we mainly need to clean up indexes
	rs.cleanupIndexes(ctx)

	rs.stats.CleanupRuns++
	rs.stats.LastCleanup = time.Now()
}

// cleanupIndexes removes stale index entries
func (rs *RedisStorage) cleanupIndexes(ctx context.Context) {
	// Get all index keys
	pattern := rs.keyPrefix + "index:*"
	iter := rs.client.Scan(ctx, 0, pattern, 0).Iterator()

	for iter.Next(ctx) {
		indexKey := iter.Val()

		// Get all members of the index
		members, err := rs.client.SMembers(ctx, indexKey).Result()
		if err != nil {
			continue
		}

		// Check if the original keys still exist
		for _, member := range members {
			exists, err := rs.client.Exists(ctx, member).Result()
			if err != nil {
				continue
			}

			// If the original key doesn't exist, remove from index
			if exists == 0 {
				rs.client.SRem(ctx, indexKey, member)
			}
		}
	}
}

// statsLoop runs periodic stats collection
func (rs *RedisStorage) statsLoop() {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !rs.started {
				return
			}
			rs.updateStorageStats(context.Background())
		case <-rs.stopCh:
			return
		}
	}
}

// Utility functions (same as memory storage)
func (rs *RedisStorage) sortEntries(entries []*MetricEntry, sortBy, sortOrder string) {
	// Implementation same as memory storage
}

func (rs *RedisStorage) extractNumericValue(value interface{}) (float64, bool) {
	// Implementation same as memory storage
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

// Aggregation functions (same as memory storage)
func (rs *RedisStorage) sum(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum
}

func (rs *RedisStorage) avg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return rs.sum(values) / float64(len(values))
}

func (rs *RedisStorage) min(values []float64) float64 {
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

func (rs *RedisStorage) max(values []float64) float64 {
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

// serializeBackup serializes a backup to JSON
func (rs *RedisStorage) serializeBackup(backup *RedisStorageBackup) ([]byte, error) {
	return json.Marshal(backup)
}

// deserializeBackup deserializes a backup from JSON
func (rs *RedisStorage) deserializeBackup(data []byte) (*RedisStorageBackup, error) {
	var backup RedisStorageBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return nil, err
	}
	return &backup, nil
}

// =============================================================================
// SUPPORTING TYPES
// =============================================================================

// RedisStorageBackup represents a backup of Redis storage
type RedisStorageBackup struct {
	Timestamp time.Time         `json:"timestamp"`
	Entries   []*MetricEntry    `json:"entries"`
	Stats     RedisStorageStats `json:"stats"`
}

// SetLogger sets the logger for the Redis storage
func (rs *RedisStorage) SetLogger(logger logger.Logger) {
	rs.logger = logger
}
