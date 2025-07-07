package database

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// baseCacheDatabase provides common functionality for cache implementations
type baseCacheDatabase struct {
	config    CacheConfig
	driver    string
	connected bool
	stats     map[string]interface{}
	mu        sync.RWMutex
}

// Connect establishes database connection
func (db *baseCacheDatabase) Connect(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// This is a base implementation - specific caches will override
	db.connected = true
	return nil
}

// Close closes database connection
func (db *baseCacheDatabase) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.connected = false
	return nil
}

// Ping tests database connection
func (db *baseCacheDatabase) Ping(ctx context.Context) error {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if !db.connected {
		return fmt.Errorf("cache not connected")
	}
	return nil
}

// IsConnected returns connection status
func (db *baseCacheDatabase) IsConnected() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.connected
}

// Stats returns database statistics
func (db *baseCacheDatabase) Stats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range db.stats {
		stats[k] = v
	}
	return stats
}

// Base cache operations (to be implemented by specific caches)
func (db *baseCacheDatabase) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, fmt.Errorf("Get not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return fmt.Errorf("Set not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) Delete(ctx context.Context, key string) error {
	return fmt.Errorf("Delete not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) Exists(ctx context.Context, key string) (bool, error) {
	return false, fmt.Errorf("Exists not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	return nil, fmt.Errorf("GetMulti not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	return fmt.Errorf("SetMulti not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) DeleteMulti(ctx context.Context, keys []string) error {
	return fmt.Errorf("DeleteMulti not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return fmt.Errorf("Expire not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) TTL(ctx context.Context, key string) (time.Duration, error) {
	return 0, fmt.Errorf("TTL not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	return 0, fmt.Errorf("Increment not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	return 0, fmt.Errorf("Decrement not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) Keys(ctx context.Context, pattern string) ([]string, error) {
	return nil, fmt.Errorf("Keys not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) Clear(ctx context.Context) error {
	return fmt.Errorf("Clear not implemented for driver: %s", db.driver)
}

func (db *baseCacheDatabase) GetJSON(ctx context.Context, key string, dest interface{}) error {
	data, err := db.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func (db *baseCacheDatabase) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return db.Set(ctx, key, data, ttl)
}

// Memory cache implementation
type memoryCache struct {
	*baseCacheDatabase
	data        map[string]*cacheItem
	mu          sync.RWMutex
	maxSize     int64
	maxItems    int64
	defaultTTL  time.Duration
	cleanup     *time.Ticker
	stopCleanup chan struct{}
}

type cacheItem struct {
	value     []byte
	expiresAt time.Time
	accessed  time.Time
	size      int64
}

// NewMemoryCache creates a new memory cache
func newMemoryCache(config CacheConfig) (Cache, error) {
	cache := &memoryCache{
		baseCacheDatabase: &baseCacheDatabase{
			config:    config,
			driver:    "memory",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		data:        make(map[string]*cacheItem),
		maxSize:     config.MaxSize,
		maxItems:    config.MaxItems,
		defaultTTL:  config.DefaultTTL,
		stopCleanup: make(chan struct{}),
	}

	// Set defaults
	if cache.maxSize == 0 {
		cache.maxSize = 100 * 1024 * 1024 // 100MB default
	}
	if cache.maxItems == 0 {
		cache.maxItems = 10000 // 10k items default
	}
	if cache.defaultTTL == 0 {
		cache.defaultTTL = 1 * time.Hour // 1 hour default
	}

	// Set cleanup interval
	cleanupInterval := config.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = 5 * time.Minute // 5 minutes default
	}

	if err := cache.Connect(context.Background()); err != nil {
		return nil, err
	}

	// Start cleanup goroutine
	cache.cleanup = time.NewTicker(cleanupInterval)
	go cache.cleanupExpired()

	return cache, nil
}

// Connect establishes connection (no-op for memory cache)
func (c *memoryCache) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = true
	return nil
}

// Close closes the cache
func (c *memoryCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	close(c.stopCleanup)
	if c.cleanup != nil {
		c.cleanup.Stop()
	}
	return nil
}

// Get retrieves a value from cache
func (c *memoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, fmt.Errorf("cache not connected")
	}

	item, exists := c.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}

	if time.Now().After(item.expiresAt) {
		// Item expired, remove it
		delete(c.data, key)
		return nil, fmt.Errorf("key expired")
	}

	// Update access time
	item.accessed = time.Now()

	// Return copy of data
	result := make([]byte, len(item.value))
	copy(result, item.value)
	return result, nil
}

// Set stores a value in cache
func (c *memoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("cache not connected")
	}

	if ttl == 0 {
		ttl = c.defaultTTL
	}

	// Create new item
	item := &cacheItem{
		value:     make([]byte, len(value)),
		expiresAt: time.Now().Add(ttl),
		accessed:  time.Now(),
		size:      int64(len(value)),
	}
	copy(item.value, value)

	// Check if we need to make room
	if c.shouldEvict(item.size) {
		c.evictLRU(item.size)
	}

	c.data[key] = item
	return nil
}

// Delete removes a key from cache
func (c *memoryCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("cache not connected")
	}

	delete(c.data, key)
	return nil
}

// Exists checks if a key exists in cache
func (c *memoryCache) Exists(ctx context.Context, key string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return false, fmt.Errorf("cache not connected")
	}

	item, exists := c.data[key]
	if !exists {
		return false, nil
	}

	if time.Now().After(item.expiresAt) {
		return false, nil
	}

	return true, nil
}

// GetMulti retrieves multiple values from cache
func (c *memoryCache) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	result := make(map[string][]byte)

	for _, key := range keys {
		if value, err := c.Get(ctx, key); err == nil {
			result[key] = value
		}
	}

	return result, nil
}

// SetMulti stores multiple values in cache
func (c *memoryCache) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	for key, value := range items {
		if err := c.Set(ctx, key, value, ttl); err != nil {
			return err
		}
	}
	return nil
}

// DeleteMulti removes multiple keys from cache
func (c *memoryCache) DeleteMulti(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := c.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// Expire sets TTL for a key
func (c *memoryCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("cache not connected")
	}

	item, exists := c.data[key]
	if !exists {
		return fmt.Errorf("key not found")
	}

	item.expiresAt = time.Now().Add(ttl)
	return nil
}

// TTL returns the TTL for a key
func (c *memoryCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return 0, fmt.Errorf("cache not connected")
	}

	item, exists := c.data[key]
	if !exists {
		return 0, fmt.Errorf("key not found")
	}

	ttl := time.Until(item.expiresAt)
	if ttl < 0 {
		return 0, nil
	}

	return ttl, nil
}

// Increment increments a numeric value
func (c *memoryCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return 0, fmt.Errorf("cache not connected")
	}

	item, exists := c.data[key]
	if !exists {
		// Create new item with delta as initial value
		newValue := fmt.Sprintf("%d", delta)
		item = &cacheItem{
			value:     []byte(newValue),
			expiresAt: time.Now().Add(c.defaultTTL),
			accessed:  time.Now(),
			size:      int64(len(newValue)),
		}
		c.data[key] = item
		return delta, nil
	}

	// Parse current value
	var currentValue int64
	if err := json.Unmarshal(item.value, &currentValue); err != nil {
		return 0, fmt.Errorf("value is not a number")
	}

	// Increment
	newValue := currentValue + delta
	newData, _ := json.Marshal(newValue)
	item.value = newData
	item.accessed = time.Now()
	item.size = int64(len(newData))

	return newValue, nil
}

// Decrement decrements a numeric value
func (c *memoryCache) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	return c.Increment(ctx, key, -delta)
}

// Keys returns all keys matching a pattern
func (c *memoryCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, fmt.Errorf("cache not connected")
	}

	var keys []string
	for key := range c.data {
		// Simple pattern matching (would need proper glob matching)
		if pattern == "*" || key == pattern {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// Clear removes all items from cache
func (c *memoryCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("cache not connected")
	}

	c.data = make(map[string]*cacheItem)
	return nil
}

// Stats returns cache statistics
func (c *memoryCache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalSize := int64(0)
	for _, item := range c.data {
		totalSize += item.size
	}

	stats := make(map[string]interface{})
	stats["driver"] = c.driver
	stats["connected"] = c.connected
	stats["items"] = len(c.data)
	stats["size"] = totalSize
	stats["max_size"] = c.maxSize
	stats["max_items"] = c.maxItems

	return stats
}

// Helper methods for memory cache

// shouldEvict checks if eviction is needed
func (c *memoryCache) shouldEvict(newItemSize int64) bool {
	if c.maxItems > 0 && int64(len(c.data)) >= c.maxItems {
		return true
	}

	if c.maxSize > 0 {
		currentSize := int64(0)
		for _, item := range c.data {
			currentSize += item.size
		}
		return currentSize+newItemSize > c.maxSize
	}

	return false
}

// evictLRU evicts least recently used items
func (c *memoryCache) evictLRU(sizeNeeded int64) {
	type keyTime struct {
		key      string
		accessed time.Time
	}

	var items []keyTime
	for key, item := range c.data {
		items = append(items, keyTime{key: key, accessed: item.accessed})
	}

	// Sort by access time (oldest first)
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].accessed.After(items[j].accessed) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	// Remove oldest items until we have enough space
	freedSize := int64(0)
	for _, item := range items {
		if freedSize >= sizeNeeded {
			break
		}
		if cacheItem, exists := c.data[item.key]; exists {
			freedSize += cacheItem.size
			delete(c.data, item.key)
		}
	}
}

// cleanupExpired removes expired items
func (c *memoryCache) cleanupExpired() {
	for {
		select {
		case <-c.cleanup.C:
			c.mu.Lock()
			now := time.Now()
			for key, item := range c.data {
				if now.After(item.expiresAt) {
					delete(c.data, key)
				}
			}
			c.mu.Unlock()
		case <-c.stopCleanup:
			return
		}
	}
}

// Placeholder implementations for specific cache types

// Memcached cache placeholder
type memcachedCache struct {
	*baseCacheDatabase
	// Memcached-specific fields would go here
}

// Constructor functions (placeholders that will be implemented in specific files)

func newMemcachedCache(config CacheConfig) (Cache, error) {
	return &memcachedCache{
		baseCacheDatabase: &baseCacheDatabase{
			config:    config,
			driver:    "memcached",
			connected: false,
			stats:     make(map[string]interface{}),
		},
	}, fmt.Errorf("Memcached cache not implemented - use memcached.go")
}

// init function to register the cache constructors
func init() {
	NewRedisCache = newRedisCache
	NewMemoryCache = newMemoryCache
	NewMemcachedCache = newMemcachedCache
	NewBadgerCache = newBadgerCache
	NewNATSKVCache = newNATSKVCache
}
