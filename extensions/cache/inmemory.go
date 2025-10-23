package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// cacheItem represents a single cache entry
type cacheItem struct {
	value      []byte
	expiration time.Time
	hasExpiry  bool
}

// isExpired checks if the item has expired
func (item *cacheItem) isExpired() bool {
	if !item.hasExpiry {
		return false
	}
	return time.Now().After(item.expiration)
}

// InMemoryCache implements Cache interface using an in-memory store
type InMemoryCache struct {
	config  Config
	logger  forge.Logger
	metrics forge.Metrics

	items     map[string]*cacheItem
	mu        sync.RWMutex
	connected bool

	stopCleanup chan struct{}
	cleanupDone chan struct{}
}

// NewInMemoryCache creates a new in-memory cache
func NewInMemoryCache(config Config, logger forge.Logger, metrics forge.Metrics) *InMemoryCache {
	if config.DefaultTTL == 0 {
		config.DefaultTTL = 5 * time.Minute
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	return &InMemoryCache{
		config:      config,
		logger:      logger,
		metrics:     metrics,
		items:       make(map[string]*cacheItem),
		stopCleanup: make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}
}

// Connect initializes the cache and starts the cleanup goroutine
func (c *InMemoryCache) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	c.connected = true
	c.logger.Info("in-memory cache connected",
		forge.F("max_size", c.config.MaxSize),
		forge.F("default_ttl", c.config.DefaultTTL),
	)

	// Start cleanup goroutine
	go c.cleanupLoop()

	return nil
}

// Disconnect stops the cache and cleans up resources
func (c *InMemoryCache) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil
	}
	c.connected = false
	c.mu.Unlock()

	// Stop cleanup goroutine
	close(c.stopCleanup)
	<-c.cleanupDone

	c.mu.Lock()
	c.items = make(map[string]*cacheItem)
	c.mu.Unlock()

	c.logger.Info("in-memory cache disconnected")
	return nil
}

// Ping checks if the cache is connected
func (c *InMemoryCache) Ping(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return ErrNotConnected
	}
	return nil
}

// Get retrieves a value from the cache
func (c *InMemoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, ErrNotConnected
	}

	key = c.prefixedKey(key)

	item, exists := c.items[key]
	if !exists {
		c.recordMetric("cache_miss")
		return nil, ErrNotFound
	}

	if item.isExpired() {
		c.mu.RUnlock()
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		c.mu.RLock()
		c.recordMetric("cache_miss")
		return nil, ErrNotFound
	}

	c.recordMetric("cache_hit")
	return item.value, nil
}

// Set stores a value in the cache
func (c *InMemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return ErrNotConnected
	}

	if err := c.validateKey(key); err != nil {
		return err
	}

	if err := c.validateValue(value); err != nil {
		return err
	}

	key = c.prefixedKey(key)

	// Check max size
	if c.config.MaxSize > 0 && len(c.items) >= c.config.MaxSize {
		// Evict oldest expired item, or oldest item if none expired
		c.evictOne()
	}

	// Use default TTL if not specified
	if ttl == 0 {
		ttl = c.config.DefaultTTL
	}

	item := &cacheItem{
		value:     make([]byte, len(value)),
		hasExpiry: ttl > 0,
	}
	copy(item.value, value)

	if item.hasExpiry {
		item.expiration = time.Now().Add(ttl)
	}

	c.items[key] = item
	c.recordMetric("cache_set")

	return nil
}

// Delete removes a key from the cache
func (c *InMemoryCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return ErrNotConnected
	}

	key = c.prefixedKey(key)
	delete(c.items, key)
	c.recordMetric("cache_delete")

	return nil
}

// Exists checks if a key exists in the cache
func (c *InMemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return false, ErrNotConnected
	}

	key = c.prefixedKey(key)
	item, exists := c.items[key]
	if !exists {
		return false, nil
	}

	if item.isExpired() {
		return false, nil
	}

	return true, nil
}

// Clear removes all keys from the cache
func (c *InMemoryCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return ErrNotConnected
	}

	c.items = make(map[string]*cacheItem)
	c.recordMetric("cache_clear")
	c.logger.Info("cache cleared")

	return nil
}

// Keys returns all keys matching the pattern
func (c *InMemoryCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, ErrNotConnected
	}

	pattern = c.prefixedKey(pattern)
	var keys []string

	for key, item := range c.items {
		if !item.isExpired() && c.matchPattern(pattern, key) {
			// Remove prefix before returning
			if c.config.Prefix != "" && strings.HasPrefix(key, c.config.Prefix) {
				key = strings.TrimPrefix(key, c.config.Prefix)
			}
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// TTL returns the remaining time-to-live for a key
func (c *InMemoryCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return 0, ErrNotConnected
	}

	key = c.prefixedKey(key)
	item, exists := c.items[key]
	if !exists {
		return 0, ErrNotFound
	}

	if !item.hasExpiry {
		return 0, nil // No expiration
	}

	if item.isExpired() {
		return 0, ErrNotFound
	}

	return time.Until(item.expiration), nil
}

// Expire sets a new TTL for a key
func (c *InMemoryCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return ErrNotConnected
	}

	key = c.prefixedKey(key)
	item, exists := c.items[key]
	if !exists {
		return ErrNotFound
	}

	if ttl <= 0 {
		item.hasExpiry = false
	} else {
		item.hasExpiry = true
		item.expiration = time.Now().Add(ttl)
	}

	return nil
}

// Helper methods

func (c *InMemoryCache) prefixedKey(key string) string {
	if c.config.Prefix == "" {
		return key
	}
	return c.config.Prefix + key
}

func (c *InMemoryCache) validateKey(key string) error {
	if key == "" {
		return errors.New("cache: key cannot be empty")
	}
	if c.config.MaxKeySize > 0 && len(key) > c.config.MaxKeySize {
		return ErrKeyTooLarge
	}
	return nil
}

func (c *InMemoryCache) validateValue(value []byte) error {
	if c.config.MaxValueSize > 0 && len(value) > c.config.MaxValueSize {
		return ErrValueTooLarge
	}
	return nil
}

func (c *InMemoryCache) matchPattern(pattern, key string) bool {
	// Simple glob matching (* wildcard only)
	if pattern == "*" || pattern == key {
		return true
	}

	if !strings.Contains(pattern, "*") {
		return pattern == key
	}

	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		// Prefix match: "prefix*"
		if parts[1] == "" {
			return strings.HasPrefix(key, parts[0])
		}
		// Suffix match: "*suffix"
		if parts[0] == "" {
			return strings.HasSuffix(key, parts[1])
		}
		// Contains match: "*infix*" or "prefix*suffix"
		return strings.HasPrefix(key, parts[0]) && strings.HasSuffix(key, parts[1])
	}

	return false
}

func (c *InMemoryCache) evictOne() {
	// Simple LRU-ish: remove first expired item found, or first item
	for key, item := range c.items {
		if item.isExpired() {
			delete(c.items, key)
			c.recordMetric("cache_evict")
			return
		}
	}

	// No expired items, just remove first one
	for key := range c.items {
		delete(c.items, key)
		c.recordMetric("cache_evict")
		return
	}
}

func (c *InMemoryCache) cleanupLoop() {
	ticker := time.NewTicker(c.config.CleanupInterval)
	defer ticker.Stop()
	defer close(c.cleanupDone)

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopCleanup:
			return
		}
	}
}

func (c *InMemoryCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	before := len(c.items)
	for key, item := range c.items {
		if item.isExpired() {
			delete(c.items, key)
		}
	}
	after := len(c.items)

	if before != after {
		c.logger.Debug("cache cleanup completed",
			forge.F("removed", before-after),
			forge.F("remaining", after),
		)
	}
}

func (c *InMemoryCache) recordMetric(name string) {
	if c.metrics != nil {
		c.metrics.Counter(name).Inc()
	}
}

// Typed operations

func (c *InMemoryCache) GetString(ctx context.Context, key string) (string, error) {
	data, err := c.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *InMemoryCache) SetString(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.Set(ctx, key, []byte(value), ttl)
}

func (c *InMemoryCache) GetJSON(ctx context.Context, key string, target interface{}) error {
	data, err := c.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func (c *InMemoryCache) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache: failed to marshal JSON: %w", err)
	}
	return c.Set(ctx, key, data, ttl)
}
