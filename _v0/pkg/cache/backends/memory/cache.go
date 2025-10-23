package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	cachecore "github.com/xraph/forge/v0/pkg/cache/core"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// MemoryCache implements an in-memory cache with LRU eviction
type MemoryCache struct {
	name    string
	config  *cachecore.MemoryConfig
	logger  common.Logger
	metrics common.Metrics

	// Storage
	data         map[string]*CacheEntry
	keysByAccess *LRUList
	keysByExpiry *TTLHeap

	// Sharding
	shards     []*CacheShard
	shardCount int
	shardMask  uint64

	// Statistics
	stats *MemoryCacheStats

	// Lifecycle
	mu            sync.RWMutex
	running       bool
	cleanupTicker *time.Ticker
	stopChan      chan struct{}
	startTime     time.Time
}

// CacheEntry represents a cache entry with metadata
type CacheEntry struct {
	Key         string
	Value       interface{}
	CreatedAt   time.Time
	UpdatedAt   time.Time
	AccessedAt  time.Time
	ExpiresAt   time.Time
	TTL         time.Duration
	AccessCount int64
	Size        int64
	Tags        []string
	Metadata    map[string]interface{}

	// LRU linking
	prev, next *CacheEntry

	// TTL heap index
	heapIndex int
}

// CacheShard represents a shard of the cache for concurrent access
type CacheShard struct {
	data         map[string]*CacheEntry
	keysByAccess *LRUList
	keysByExpiry *TTLHeap
	mu           sync.RWMutex
	size         int64
	count        int64
}

// MemoryCacheStats tracks memory cache statistics
type MemoryCacheStats struct {
	hits        int64
	misses      int64
	sets        int64
	deletes     int64
	evictions   int64
	expirations int64
	errors      int64
	size        int64
	count       int64
	shardStats  []*ShardStats
	mu          sync.RWMutex
}

// NewMemoryCache creates a new in-memory cache
func NewMemoryCache(name string, config *cachecore.MemoryConfig, l common.Logger, metrics common.Metrics) (*MemoryCache, error) {
	if config == nil {
		config = &cachecore.MemoryConfig{
			MaxSize:         100000,
			MaxMemory:       128 * 1024 * 1024, // 128MB
			EvictionPolicy:  cachecore.EvictionPolicyLRU,
			ShardCount:      16,
			CleanupInterval: 5 * time.Minute,
			EnableStats:     true,
		}
	}

	cache := &MemoryCache{
		name:       name,
		config:     config,
		logger:     l,
		metrics:    metrics,
		shardCount: config.ShardCount,
		shardMask:  uint64(config.ShardCount - 1),
		stats:      &MemoryCacheStats{},
		stopChan:   make(chan struct{}),
		startTime:  time.Now(),
	}

	// Initialize shards
	cache.shards = make([]*CacheShard, config.ShardCount)
	for i := 0; i < config.ShardCount; i++ {
		cache.shards[i] = &CacheShard{
			data:         make(map[string]*CacheEntry),
			keysByAccess: NewLRUList(),
			keysByExpiry: NewTTLHeap(),
		}
	}

	// Initialize shard stats
	cache.stats.shardStats = make([]*ShardStats, config.ShardCount)
	for i := 0; i < config.ShardCount; i++ {
		cache.stats.shardStats[i] = &ShardStats{ID: i}
	}

	// Start background cleanup if TTL is enabled
	if config.EnableTTL {
		cache.startCleanup()
	}

	if l != nil {
		l.Info("memory cache created",
			logger.String("name", name),
			logger.Int64("max_size", config.MaxSize),
			logger.Int64("max_memory", config.MaxMemory),
			logger.String("eviction_policy", string(config.EvictionPolicy)),
			logger.Int("shard_count", config.ShardCount),
		)
	}

	return cache, nil
}

// Type returns the cache type
func (mc *MemoryCache) Type() cachecore.CacheType {
	return cachecore.CacheTypeMemory
}

// Name returns the cache name
func (mc *MemoryCache) Name() string {
	return mc.name
}

// Configure configures the cache
func (mc *MemoryCache) Configure(config interface{}) error {
	memConfig, ok := config.(*cachecore.MemoryConfig)
	if !ok {
		return fmt.Errorf("invalid config type for memory cache")
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.config = memConfig
	return nil
}

// Start starts the cache
func (mc *MemoryCache) Start(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.running {
		return nil
	}

	mc.running = true

	if mc.logger != nil {
		mc.logger.Info("memory cache started", logger.String("name", mc.name))
	}

	return nil
}

// Stop stops the cache
func (mc *MemoryCache) Stop(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if !mc.running {
		return nil
	}

	mc.running = false
	close(mc.stopChan)

	if mc.cleanupTicker != nil {
		mc.cleanupTicker.Stop()
	}

	if mc.logger != nil {
		mc.logger.Info("memory cache stopped", logger.String("name", mc.name))
	}

	return nil
}

// getShard returns the shard for a given key
func (mc *MemoryCache) getShard(key string) *CacheShard {
	hash := fnv1aHash(key)
	return mc.shards[hash&mc.shardMask]
}

// Get retrieves a value from the cache
func (mc *MemoryCache) Get(ctx context.Context, key string) (interface{}, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		mc.recordError()
		return nil, err
	}

	shard := mc.getShard(key)
	shard.mu.RLock()
	entry, exists := shard.data[key]
	shard.mu.RUnlock()

	if !exists {
		mc.recordMiss()
		return nil, cachecore.ErrCacheNotFound
	}

	// Check expiration
	if mc.config.EnableTTL && !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		// Entry expired, remove it
		mc.Delete(ctx, key)
		mc.recordMiss()
		mc.recordExpiration()
		return nil, cachecore.ErrCacheNotFound
	}

	// Update access information
	now := time.Now()
	atomic.AddInt64(&entry.AccessCount, 1)
	entry.AccessedAt = now

	// Move to front in LRU
	shard.mu.Lock()
	shard.keysByAccess.MoveToFront(entry)
	shard.mu.Unlock()

	mc.recordHit()

	// Return a copy of the value to prevent modification
	return mc.copyValue(entry.Value), nil
}

// Set stores a value in the cache
func (mc *MemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if err := cachecore.ValidateKey(key); err != nil {
		mc.recordError()
		return err
	}

	if err := cachecore.ValidateTTL(ttl); err != nil {
		mc.recordError()
		return err
	}

	now := time.Now()
	size := mc.calculateSize(value)

	entry := &CacheEntry{
		Key:         key,
		Value:       mc.copyValue(value),
		CreatedAt:   now,
		UpdatedAt:   now,
		AccessedAt:  now,
		TTL:         ttl,
		AccessCount: 1,
		Size:        size,
	}

	if mc.config.EnableTTL && ttl > 0 {
		entry.ExpiresAt = now.Add(ttl)
	}

	shard := mc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Check if key already exists
	if existingEntry, exists := shard.data[key]; exists {
		// Update existing entry
		oldSize := existingEntry.Size
		existingEntry.Value = entry.Value
		existingEntry.UpdatedAt = now
		existingEntry.AccessedAt = now
		existingEntry.TTL = ttl
		existingEntry.ExpiresAt = entry.ExpiresAt
		existingEntry.Size = size

		// Update size tracking
		atomic.AddInt64(&shard.size, size-oldSize)
		mc.updateTotalSize(size - oldSize)

		// Move to front in LRU
		shard.keysByAccess.MoveToFront(existingEntry)

		// Update TTL heap if needed
		if mc.config.EnableTTL && ttl > 0 {
			shard.keysByExpiry.Update(existingEntry)
		}
	} else {
		// New entry
		shard.data[key] = entry
		atomic.AddInt64(&shard.count, 1)
		atomic.AddInt64(&shard.size, size)
		mc.updateTotalSize(size)
		mc.updateTotalCount(1)

		// Add to LRU
		shard.keysByAccess.PushFront(entry)

		// Add to TTL heap if needed
		if mc.config.EnableTTL && ttl > 0 {
			shard.keysByExpiry.Push(entry)
		}

		// Check if eviction is needed
		mc.maybeEvict(shard)
	}

	mc.recordSet()
	return nil
}

// Delete removes a value from the cache
func (mc *MemoryCache) Delete(ctx context.Context, key string) error {
	if err := cachecore.ValidateKey(key); err != nil {
		mc.recordError()
		return err
	}

	shard := mc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists := shard.data[key]
	if !exists {
		return nil // Already deleted
	}

	// Remove from all data structures
	delete(shard.data, key)
	shard.keysByAccess.Remove(entry)

	if mc.config.EnableTTL {
		shard.keysByExpiry.Remove(entry)
	}

	// Update counters
	atomic.AddInt64(&shard.count, -1)
	atomic.AddInt64(&shard.size, -entry.Size)
	mc.updateTotalSize(-entry.Size)
	mc.updateTotalCount(-1)

	mc.recordDelete()
	return nil
}

// Exists checks if a key exists in the cache
func (mc *MemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		mc.recordError()
		return false, err
	}

	shard := mc.getShard(key)
	shard.mu.RLock()
	entry, exists := shard.data[key]
	shard.mu.RUnlock()

	if !exists {
		return false, nil
	}

	// Check expiration
	if mc.config.EnableTTL && !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		// Entry expired
		mc.Delete(ctx, key)
		return false, nil
	}

	return true, nil
}

// GetMulti retrieves multiple values from the cache
func (mc *MemoryCache) GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error) {
	if len(keys) == 0 {
		return make(map[string]interface{}), nil
	}

	result := make(map[string]interface{})

	for _, key := range keys {
		if err := cachecore.ValidateKey(key); err != nil {
			mc.recordError()
			continue
		}

		value, err := mc.Get(ctx, key)
		if err == nil {
			result[key] = value
		}
	}

	return result, nil
}

// SetMulti stores multiple values in the cache
func (mc *MemoryCache) SetMulti(ctx context.Context, items map[string]cachecore.CacheItem) error {
	for key, item := range items {
		if err := mc.Set(ctx, key, item.Value, item.TTL); err != nil {
			return err
		}
	}
	return nil
}

// DeleteMulti removes multiple values from the cache
func (mc *MemoryCache) DeleteMulti(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := mc.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// Increment increments a numeric value
func (mc *MemoryCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		mc.recordError()
		return 0, err
	}

	shard := mc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists := shard.data[key]
	if !exists {
		// Create new entry with delta as initial value
		return delta, mc.Set(ctx, key, delta, 0)
	}

	// Try to convert current value to int64
	var current int64
	switch v := entry.Value.(type) {
	case int:
		current = int64(v)
	case int32:
		current = int64(v)
	case int64:
		current = v
	case float32:
		current = int64(v)
	case float64:
		current = int64(v)
	default:
		mc.recordError()
		return 0, cachecore.NewCacheError("INVALID_VALUE", "value is not numeric", key, "incr", nil)
	}

	newValue := current + delta
	entry.Value = newValue
	entry.UpdatedAt = time.Now()
	entry.AccessedAt = time.Now()

	return newValue, nil
}

// Decrement decrements a numeric value
func (mc *MemoryCache) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	return mc.Increment(ctx, key, -delta)
}

// Touch updates the TTL of a key
func (mc *MemoryCache) Touch(ctx context.Context, key string, ttl time.Duration) error {
	if err := cachecore.ValidateKey(key); err != nil {
		mc.recordError()
		return err
	}

	if err := cachecore.ValidateTTL(ttl); err != nil {
		mc.recordError()
		return err
	}

	shard := mc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists := shard.data[key]
	if !exists {
		return cachecore.ErrCacheNotFound
	}

	now := time.Now()
	entry.TTL = ttl
	entry.AccessedAt = now

	if mc.config.EnableTTL && ttl > 0 {
		entry.ExpiresAt = now.Add(ttl)
		shard.keysByExpiry.Update(entry)
	} else {
		entry.ExpiresAt = time.Time{}
		if mc.config.EnableTTL {
			shard.keysByExpiry.Remove(entry)
		}
	}

	return nil
}

// TTL returns the time-to-live of a key
func (mc *MemoryCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		mc.recordError()
		return 0, err
	}

	shard := mc.getShard(key)
	shard.mu.RLock()
	entry, exists := shard.data[key]
	shard.mu.RUnlock()

	if !exists {
		return 0, cachecore.ErrCacheNotFound
	}

	if !mc.config.EnableTTL || entry.ExpiresAt.IsZero() {
		return -1, nil // No expiration
	}

	remaining := time.Until(entry.ExpiresAt)
	if remaining <= 0 {
		return 0, nil // Expired
	}

	return remaining, nil
}

// Keys returns keys matching a pattern
func (mc *MemoryCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		mc.recordError()
		return nil, cachecore.NewCacheError("INVALID_PATTERN", "invalid regex pattern", "", "keys", err)
	}

	var keys []string

	for _, shard := range mc.shards {
		shard.mu.RLock()
		for key := range shard.data {
			if regex.MatchString(key) {
				keys = append(keys, key)
			}
		}
		shard.mu.RUnlock()
	}

	return keys, nil
}

// DeletePattern deletes keys matching a pattern
func (mc *MemoryCache) DeletePattern(ctx context.Context, pattern string) error {
	keys, err := mc.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	return mc.DeleteMulti(ctx, keys)
}

// Flush clears all keys in the cache
func (mc *MemoryCache) Flush(ctx context.Context) error {
	for _, shard := range mc.shards {
		shard.mu.Lock()
		shard.data = make(map[string]*CacheEntry)
		shard.keysByAccess = NewLRUList()
		shard.keysByExpiry = NewTTLHeap()
		atomic.StoreInt64(&shard.size, 0)
		atomic.StoreInt64(&shard.count, 0)
		shard.mu.Unlock()
	}

	mc.stats.mu.Lock()
	mc.stats.size = 0
	mc.stats.count = 0
	mc.stats.mu.Unlock()

	return nil
}

// Size returns the number of keys in the cache
func (mc *MemoryCache) Size(ctx context.Context) (int64, error) {
	mc.stats.mu.RLock()
	size := mc.stats.count
	mc.stats.mu.RUnlock()
	return size, nil
}

// Close closes the cache
func (mc *MemoryCache) Close() error {
	return mc.Stop(context.Background())
}

// Stats returns cache statistics
func (mc *MemoryCache) Stats() cachecore.CacheStats {
	mc.stats.mu.RLock()
	defer mc.stats.mu.RUnlock()

	// Update shard stats
	for i, shard := range mc.shards {
		mc.stats.shardStats[i].Size = atomic.LoadInt64(&shard.size)
		mc.stats.shardStats[i].Count = atomic.LoadInt64(&shard.count)
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return cachecore.CacheStats{
		Name:     mc.name,
		Type:     string(cachecore.CacheTypeMemory),
		Hits:     mc.stats.hits,
		Misses:   mc.stats.misses,
		Sets:     mc.stats.sets,
		Deletes:  mc.stats.deletes,
		Errors:   mc.stats.errors,
		Size:     mc.stats.count,
		Memory:   mc.stats.size,
		HitRatio: cachecore.CalculateHitRatio(mc.stats.hits, mc.stats.misses),
		Uptime:   time.Since(mc.startTime),
		Custom: map[string]interface{}{
			"evictions":   mc.stats.evictions,
			"expirations": mc.stats.expirations,
			"shard_count": len(mc.shards),
			"shards":      mc.stats.shardStats,
			"heap_memory": memStats.HeapInuse,
		},
	}
}

// HealthCheck performs a health check
func (mc *MemoryCache) HealthCheck(ctx context.Context) error {
	if !mc.running {
		return fmt.Errorf("memory cache is not running")
	}

	// Check memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	if memStats.HeapInuse > uint64(mc.config.MaxMemory)*2 {
		return fmt.Errorf("memory usage too high: %d bytes", memStats.HeapInuse)
	}

	// Check cache size
	mc.stats.mu.RLock()
	count := mc.stats.count
	size := mc.stats.size
	mc.stats.mu.RUnlock()

	if count > mc.config.MaxSize {
		return fmt.Errorf("cache size exceeded: %d/%d items", count, mc.config.MaxSize)
	}

	if size > mc.config.MaxMemory {
		return fmt.Errorf("cache memory exceeded: %d/%d bytes", size, mc.config.MaxMemory)
	}

	return nil
}

// Helper methods

// maybeEvict checks if eviction is needed and performs it
func (mc *MemoryCache) maybeEvict(shard *CacheShard) {
	count := atomic.LoadInt64(&shard.count)
	size := atomic.LoadInt64(&shard.size)

	maxPerShard := mc.config.MaxSize / int64(mc.shardCount)
	maxMemoryPerShard := mc.config.MaxMemory / int64(mc.shardCount)

	shouldEvict := count > maxPerShard || size > maxMemoryPerShard

	if shouldEvict {
		mc.evictFromShard(shard)
	}
}

// evictFromShard evicts entries from a shard based on the eviction policy
func (mc *MemoryCache) evictFromShard(shard *CacheShard) {
	evictCount := int(float64(atomic.LoadInt64(&shard.count)) * mc.config.EvictionRate)
	if evictCount == 0 {
		evictCount = 1
	}

	for i := 0; i < evictCount; i++ {
		var entryToEvict *CacheEntry

		switch mc.config.EvictionPolicy {
		case cachecore.EvictionPolicyLRU:
			entryToEvict = shard.keysByAccess.Back()
		case cachecore.EvictionPolicyLFU:
			entryToEvict = mc.findLFUEntry(shard)
		case cachecore.EvictionPolicyFIFO:
			entryToEvict = mc.findOldestEntry(shard)
		case cachecore.EvictionPolicyRandom:
			entryToEvict = mc.findRandomEntry(shard)
		case cachecore.EvictionPolicyTTL:
			entryToEvict = mc.findEarliestExpiryEntry(shard)
		}

		if entryToEvict != nil {
			mc.evictEntry(shard, entryToEvict)
		} else {
			break
		}
	}
}

// evictEntry removes an entry from the cache
func (mc *MemoryCache) evictEntry(shard *CacheShard, entry *CacheEntry) {
	delete(shard.data, entry.Key)
	shard.keysByAccess.Remove(entry)

	if mc.config.EnableTTL {
		shard.keysByExpiry.Remove(entry)
	}

	atomic.AddInt64(&shard.count, -1)
	atomic.AddInt64(&shard.size, -entry.Size)
	mc.updateTotalSize(-entry.Size)
	mc.updateTotalCount(-1)
	mc.recordEviction()
}

// Statistics recording methods
func (mc *MemoryCache) recordHit() {
	atomic.AddInt64(&mc.stats.hits, 1)
	if mc.metrics != nil {
		mc.metrics.Counter("cache.hits", "cache", mc.name).Inc()
	}
}

func (mc *MemoryCache) recordMiss() {
	atomic.AddInt64(&mc.stats.misses, 1)
	if mc.metrics != nil {
		mc.metrics.Counter("cache.misses", "cache", mc.name).Inc()
	}
}

func (mc *MemoryCache) recordSet() {
	atomic.AddInt64(&mc.stats.sets, 1)
	if mc.metrics != nil {
		mc.metrics.Counter("cache.sets", "cache", mc.name).Inc()
	}
}

func (mc *MemoryCache) recordDelete() {
	atomic.AddInt64(&mc.stats.deletes, 1)
	if mc.metrics != nil {
		mc.metrics.Counter("cache.deletes", "cache", mc.name).Inc()
	}
}

func (mc *MemoryCache) recordEviction() {
	atomic.AddInt64(&mc.stats.evictions, 1)
	if mc.metrics != nil {
		mc.metrics.Counter("cache.evictions", "cache", mc.name).Inc()
	}
}

func (mc *MemoryCache) recordExpiration() {
	atomic.AddInt64(&mc.stats.expirations, 1)
	if mc.metrics != nil {
		mc.metrics.Counter("cache.expirations", "cache", mc.name).Inc()
	}
}

func (mc *MemoryCache) recordError() {
	atomic.AddInt64(&mc.stats.errors, 1)
	if mc.metrics != nil {
		mc.metrics.Counter("cache.errors", "cache", mc.name).Inc()
	}
}

func (mc *MemoryCache) updateTotalSize(delta int64) {
	mc.stats.mu.Lock()
	mc.stats.size += delta
	mc.stats.mu.Unlock()
}

func (mc *MemoryCache) updateTotalCount(delta int64) {
	mc.stats.mu.Lock()
	mc.stats.count += delta
	mc.stats.mu.Unlock()
}

// Utility methods

// calculateSize estimates the memory size of a value
func (mc *MemoryCache) calculateSize(value interface{}) int64 {
	// Simple estimation - in practice you might want more accurate sizing
	data, err := json.Marshal(value)
	if err != nil {
		return 64 // Default estimate
	}
	return int64(len(data)) + 64 // Add overhead for metadata
}

// copyValue creates a copy of the value to prevent external modification
func (mc *MemoryCache) copyValue(value interface{}) interface{} {
	// For simple types, return as-is
	switch v := value.(type) {
	case string, int, int32, int64, float32, float64, bool:
		return v
	default:
		// For complex types, use JSON marshal/unmarshal for deep copy
		data, err := json.Marshal(value)
		if err != nil {
			return value // Return original if can't copy
		}

		var copy interface{}
		if err := json.Unmarshal(data, &copy); err != nil {
			return value // Return original if can't copy
		}

		return copy
	}
}

// startCleanup starts the background cleanup routine
func (mc *MemoryCache) startCleanup() {
	mc.cleanupTicker = time.NewTicker(mc.config.CleanupInterval)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				if mc.logger != nil {
					mc.logger.Error("cache cleanup panic recovered",
						logger.String("cache", mc.name),
						logger.Any("panic", r),
					)
				}
			}
		}()

		for {
			select {
			case <-mc.stopChan:
				return
			case <-mc.cleanupTicker.C:
				mc.cleanupExpired()
			}
		}
	}()
}

// cleanupExpired removes expired entries
func (mc *MemoryCache) cleanupExpired() {
	if !mc.config.EnableTTL {
		return
	}

	now := time.Now()
	expiredCount := 0

	for _, shard := range mc.shards {
		shard.mu.Lock()

		// Remove expired entries from TTL heap
		for {
			entry := shard.keysByExpiry.Peek()
			if entry == nil || entry.ExpiresAt.After(now) {
				break
			}

			// Remove expired entry
			shard.keysByExpiry.Pop()
			delete(shard.data, entry.Key)
			shard.keysByAccess.Remove(entry)

			atomic.AddInt64(&shard.count, -1)
			atomic.AddInt64(&shard.size, -entry.Size)
			expiredCount++
		}

		shard.mu.Unlock()
	}

	if expiredCount > 0 {
		mc.updateTotalCount(int64(-expiredCount))

		for i := 0; i < expiredCount; i++ {
			mc.recordExpiration()
		}

		if mc.logger != nil {
			mc.logger.Debug("expired entries cleaned up",
				logger.String("cache", mc.name),
				logger.Int("count", expiredCount),
			)
		}
	}
}

// Helper functions for different eviction policies

func (mc *MemoryCache) findLFUEntry(shard *CacheShard) *CacheEntry {
	var lfu *CacheEntry
	minCount := int64(^uint64(0) >> 1) // max int64

	for _, entry := range shard.data {
		if atomic.LoadInt64(&entry.AccessCount) < minCount {
			minCount = atomic.LoadInt64(&entry.AccessCount)
			lfu = entry
		}
	}

	return lfu
}

func (mc *MemoryCache) findOldestEntry(shard *CacheShard) *CacheEntry {
	var oldest *CacheEntry
	oldestTime := time.Now()

	for _, entry := range shard.data {
		if entry.CreatedAt.Before(oldestTime) {
			oldestTime = entry.CreatedAt
			oldest = entry
		}
	}

	return oldest
}

func (mc *MemoryCache) findRandomEntry(shard *CacheShard) *CacheEntry {
	if len(shard.data) == 0 {
		return nil
	}

	// Get a random entry (simple approach)
	for _, entry := range shard.data {
		return entry // Return first entry found
	}

	return nil
}

func (mc *MemoryCache) findEarliestExpiryEntry(shard *CacheShard) *CacheEntry {
	return shard.keysByExpiry.Peek()
}

// fnv1aHash computes FNV-1a hash for string
func fnv1aHash(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)

	hash := uint64(offset64)
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= prime64
	}
	return hash
}
