package inference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// InferenceCache handles caching of inference results
type InferenceCache struct {
	config    InferenceCacheConfig
	cache     map[string]*CacheEntry
	lru       *LRUList
	stats     CacheStats
	started   bool
	mu        sync.RWMutex
	logger    common.Logger
	metrics   common.Metrics
	shutdownC chan struct{}
	wg        sync.WaitGroup
}

// InferenceCacheConfig contains configuration for inference caching
type InferenceCacheConfig struct {
	Size                int            `yaml:"size" default:"1000"`
	TTL                 time.Duration  `yaml:"ttl" default:"1h"`
	MaxEntrySize        int            `yaml:"max_entry_size" default:"1048576"` // 1MB
	CleanupInterval     time.Duration  `yaml:"cleanup_interval" default:"5m"`
	EnableCompression   bool           `yaml:"enable_compression" default:"true"`
	EnablePersistence   bool           `yaml:"enable_persistence" default:"false"`
	PersistenceFile     string         `yaml:"persistence_file" default:"inference_cache.json"`
	PersistenceInterval time.Duration  `yaml:"persistence_interval" default:"1m"`
	HashFunction        string         `yaml:"hash_function" default:"sha256"`
	Logger              common.Logger  `yaml:"-"`
	Metrics             common.Metrics `yaml:"-"`
}

// CacheEntry represents a cached inference result
type CacheEntry struct {
	Key         string
	Request     InferenceRequest
	Response    InferenceResponse
	CreatedAt   time.Time
	AccessedAt  time.Time
	ExpiresAt   time.Time
	AccessCount int64
	Size        int
	Compressed  bool
	Data        []byte
	Hash        string
	Tags        map[string]string
	mu          sync.RWMutex
}

// CacheStats contains statistics for the inference cache
type CacheStats struct {
	Hits            int64         `json:"hits"`
	Misses          int64         `json:"misses"`
	Entries         int           `json:"entries"`
	Size            int           `json:"size"`
	MaxSize         int           `json:"max_size"`
	HitRate         float64       `json:"hit_rate"`
	AverageLatency  time.Duration `json:"average_latency"`
	TotalLatency    time.Duration `json:"total_latency"`
	Evictions       int64         `json:"evictions"`
	Expirations     int64         `json:"expirations"`
	Compressions    int64         `json:"compressions"`
	Decompressions  int64         `json:"decompressions"`
	PersistenceOps  int64         `json:"persistence_ops"`
	LastCleanup     time.Time     `json:"last_cleanup"`
	LastPersistence time.Time     `json:"last_persistence"`
	LastUpdated     time.Time     `json:"last_updated"`
}

// LRUList implements a least-recently-used list
type LRUList struct {
	head *LRUNode
	tail *LRUNode
	size int
	mu   sync.RWMutex
}

// LRUNode represents a node in the LRU list
type LRUNode struct {
	key   string
	prev  *LRUNode
	next  *LRUNode
	entry *CacheEntry
}

// CacheEvictionStrategy defines different cache eviction strategies
type CacheEvictionStrategy interface {
	Name() string
	ShouldEvict(entry *CacheEntry, cacheSize int, maxSize int) bool
	SelectVictim(entries map[string]*CacheEntry) string
}

// NewInferenceCache creates a new inference cache
func NewInferenceCache(config InferenceCacheConfig) (*InferenceCache, error) {
	if config.Size <= 0 {
		config.Size = 1000
	}
	if config.TTL == 0 {
		config.TTL = time.Hour
	}
	if config.MaxEntrySize <= 0 {
		config.MaxEntrySize = 1048576 // 1MB
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 5 * time.Minute
	}
	if config.PersistenceInterval == 0 {
		config.PersistenceInterval = time.Minute
	}
	if config.HashFunction == "" {
		config.HashFunction = "sha256"
	}

	cache := &InferenceCache{
		config:    config,
		cache:     make(map[string]*CacheEntry),
		lru:       NewLRUList(),
		stats:     CacheStats{MaxSize: config.Size},
		logger:    config.Logger,
		metrics:   config.Metrics,
		shutdownC: make(chan struct{}),
	}

	return cache, nil
}

// Start starts the inference cache
func (c *InferenceCache) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("inference cache already started")
	}

	// Load persistent cache if enabled
	if c.config.EnablePersistence {
		if err := c.loadPersistentCache(); err != nil {
			if c.logger != nil {
				c.logger.Warn("failed to load persistent cache", logger.Error(err))
			}
		}
	}

	// Start cleanup routine
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runCleanupRoutine(ctx)
	}()

	// Start persistence routine if enabled
	if c.config.EnablePersistence {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.runPersistenceRoutine(ctx)
		}()
	}

	// Start stats collection
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runStatsCollection(ctx)
	}()

	c.started = true

	if c.logger != nil {
		c.logger.Info("inference cache started",
			logger.Int("size", c.config.Size),
			logger.Duration("ttl", c.config.TTL),
			logger.Bool("compression", c.config.EnableCompression),
			logger.Bool("persistence", c.config.EnablePersistence),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.ai.inference_cache_started").Inc()
	}

	return nil
}

// Stop stops the inference cache
func (c *InferenceCache) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return fmt.Errorf("inference cache not started")
	}

	// Signal shutdown
	close(c.shutdownC)

	// Wait for routines to finish
	c.wg.Wait()

	// Save persistent cache if enabled
	if c.config.EnablePersistence {
		if err := c.savePersistentCache(); err != nil {
			if c.logger != nil {
				c.logger.Error("failed to save persistent cache", logger.Error(err))
			}
		}
	}

	c.started = false

	if c.logger != nil {
		c.logger.Info("inference cache stopped")
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.ai.inference_cache_stopped").Inc()
	}

	return nil
}

// Get retrieves a cached inference result
func (c *InferenceCache) Get(request InferenceRequest) (InferenceResponse, bool) {
	if !c.started {
		return InferenceResponse{}, false
	}

	key := c.generateKey(request)

	c.mu.RLock()
	entry, exists := c.cache[key]
	c.mu.RUnlock()

	if !exists {
		c.updateStats(func(stats *CacheStats) {
			stats.Misses++
		})

		if c.metrics != nil {
			c.metrics.Counter("forge.ai.inference_cache_misses").Inc()
		}

		return InferenceResponse{}, false
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Check if entry is expired
	if time.Now().After(entry.ExpiresAt) {
		c.mu.Lock()
		delete(c.cache, key)
		c.lru.Remove(key)
		c.mu.Unlock()

		c.updateStats(func(stats *CacheStats) {
			stats.Misses++
			stats.Expirations++
		})

		if c.metrics != nil {
			c.metrics.Counter("forge.ai.inference_cache_misses").Inc()
			c.metrics.Counter("forge.ai.inference_cache_expirations").Inc()
		}

		return InferenceResponse{}, false
	}

	// Update access information
	entry.AccessedAt = time.Now()
	entry.AccessCount++

	// Move to front of LRU
	c.lru.MoveToFront(key)

	// Decompress if needed
	response := entry.Response
	if entry.Compressed {
		// Decompress response data
		if err := c.decompressResponse(&response, entry.Data); err != nil {
			if c.logger != nil {
				c.logger.Error("failed to decompress cache entry", logger.Error(err))
			}
			return InferenceResponse{}, false
		}

		c.updateStats(func(stats *CacheStats) {
			stats.Decompressions++
		})
	}

	// Mark as from cache
	response.FromCache = true

	c.updateStats(func(stats *CacheStats) {
		stats.Hits++
	})

	if c.metrics != nil {
		c.metrics.Counter("forge.ai.inference_cache_hits").Inc()
	}

	if c.logger != nil {
		c.logger.Debug("cache hit",
			logger.String("key", key),
			logger.String("model_id", request.ModelID),
			logger.Int64("access_count", entry.AccessCount),
		)
	}

	return response, true
}

// Set stores an inference result in the cache
func (c *InferenceCache) Set(request InferenceRequest, response InferenceResponse) {
	if !c.started {
		return
	}

	key := c.generateKey(request)

	// Calculate entry size
	entrySize := c.calculateEntrySize(request, response)
	if entrySize > c.config.MaxEntrySize {
		if c.logger != nil {
			c.logger.Warn("entry too large for cache",
				logger.String("key", key),
				logger.Int("size", entrySize),
				logger.Int("max_size", c.config.MaxEntrySize),
			)
		}
		return
	}

	now := time.Now()
	entry := &CacheEntry{
		Key:         key,
		Request:     request,
		Response:    response,
		CreatedAt:   now,
		AccessedAt:  now,
		ExpiresAt:   now.Add(c.config.TTL),
		AccessCount: 1,
		Size:        entrySize,
		Hash:        c.generateHash(request),
		Tags:        make(map[string]string),
	}

	// Compress if enabled
	if c.config.EnableCompression {
		if err := c.compressEntry(entry); err != nil {
			if c.logger != nil {
				c.logger.Error("failed to compress cache entry", logger.Error(err))
			}
		} else {
			c.updateStats(func(stats *CacheStats) {
				stats.Compressions++
			})
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict entries
	if len(c.cache) >= c.config.Size {
		c.evictEntries(1)
	}

	// Store entry
	c.cache[key] = entry
	c.lru.AddToFront(key, entry)

	c.updateStats(func(stats *CacheStats) {
		stats.Entries = len(c.cache)
		stats.Size += entrySize
	})

	if c.metrics != nil {
		c.metrics.Counter("forge.ai.inference_cache_sets").Inc()
		c.metrics.Gauge("forge.ai.inference_cache_entries").Set(float64(len(c.cache)))
	}

	if c.logger != nil {
		c.logger.Debug("cache set",
			logger.String("key", key),
			logger.String("model_id", request.ModelID),
			logger.Int("size", entrySize),
			logger.Bool("compressed", entry.Compressed),
		)
	}
}

// Delete removes an entry from the cache
func (c *InferenceCache) Delete(request InferenceRequest) {
	if !c.started {
		return
	}

	key := c.generateKey(request)

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.cache[key]; exists {
		delete(c.cache, key)
		c.lru.Remove(key)

		c.updateStats(func(stats *CacheStats) {
			stats.Entries = len(c.cache)
			stats.Size -= entry.Size
		})

		if c.metrics != nil {
			c.metrics.Counter("forge.ai.inference_cache_deletes").Inc()
			c.metrics.Gauge("forge.ai.inference_cache_entries").Set(float64(len(c.cache)))
		}
	}
}

// Clear clears all entries from the cache
func (c *InferenceCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := len(c.cache)
	c.cache = make(map[string]*CacheEntry)
	c.lru = NewLRUList()

	c.updateStats(func(stats *CacheStats) {
		stats.Entries = 0
		stats.Size = 0
	})

	if c.metrics != nil {
		c.metrics.Counter("forge.ai.inference_cache_clears").Inc()
		c.metrics.Gauge("forge.ai.inference_cache_entries").Set(0)
	}

	if c.logger != nil {
		c.logger.Info("cache cleared",
			logger.Int("entries_removed", count),
		)
	}
}

// GetStats returns cache statistics
func (c *InferenceCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	stats.Entries = len(c.cache)
	stats.LastUpdated = time.Now()

	// Calculate hit rate
	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total)
	}

	return stats
}

// generateKey generates a cache key for a request
func (c *InferenceCache) generateKey(request InferenceRequest) string {
	// Create a deterministic key based on request content
	data := struct {
		ModelID string
		Input   interface{}
		Options InferenceOptions
	}{
		ModelID: request.ModelID,
		Input:   request.Input,
		Options: request.Options,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		// Fallback to simple key
		return fmt.Sprintf("%s-%s-%d", request.ModelID, request.ID, time.Now().UnixNano())
	}

	// Generate hash
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:])
}

// generateHash generates a hash for a request
func (c *InferenceCache) generateHash(request InferenceRequest) string {
	return c.generateKey(request)
}

// calculateEntrySize calculates the size of a cache entry
func (c *InferenceCache) calculateEntrySize(request InferenceRequest, response InferenceResponse) int {
	// Simplified size calculation
	requestSize := len(fmt.Sprintf("%+v", request))
	responseSize := len(fmt.Sprintf("%+v", response))
	return requestSize + responseSize
}

// compressEntry compresses a cache entry
func (c *InferenceCache) compressEntry(entry *CacheEntry) error {
	// Simplified compression - in practice, would use gzip or similar
	data, err := json.Marshal(entry.Response)
	if err != nil {
		return err
	}

	entry.Data = data
	entry.Compressed = true
	return nil
}

// decompressResponse decompresses a cached response
func (c *InferenceCache) decompressResponse(response *InferenceResponse, data []byte) error {
	// Simplified decompression
	return json.Unmarshal(data, response)
}

// evictEntries evicts entries from the cache
func (c *InferenceCache) evictEntries(count int) {
	evicted := 0
	for evicted < count && c.lru.Size() > 0 {
		// Remove least recently used entry
		key := c.lru.RemoveTail()
		if key != "" {
			if entry, exists := c.cache[key]; exists {
				delete(c.cache, key)
				c.updateStats(func(stats *CacheStats) {
					stats.Size -= entry.Size
					stats.Evictions++
				})
				evicted++
			}
		}
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.ai.inference_cache_evictions").Add(float64(evicted))
	}
}

// runCleanupRoutine runs the cache cleanup routine
func (c *InferenceCache) runCleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(c.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.shutdownC:
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// runPersistenceRoutine runs the cache persistence routine
func (c *InferenceCache) runPersistenceRoutine(ctx context.Context) {
	ticker := time.NewTicker(c.config.PersistenceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.shutdownC:
			return
		case <-ticker.C:
			if err := c.savePersistentCache(); err != nil {
				if c.logger != nil {
					c.logger.Error("failed to save persistent cache", logger.Error(err))
				}
			}
		}
	}
}

// runStatsCollection runs the statistics collection routine
func (c *InferenceCache) runStatsCollection(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.shutdownC:
			return
		case <-ticker.C:
			c.collectStats()
		}
	}
}

// cleanup removes expired entries from the cache
func (c *InferenceCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	expired := make([]string, 0)

	for key, entry := range c.cache {
		if now.After(entry.ExpiresAt) {
			expired = append(expired, key)
		}
	}

	for _, key := range expired {
		if entry, exists := c.cache[key]; exists {
			delete(c.cache, key)
			c.lru.Remove(key)
			c.updateStats(func(stats *CacheStats) {
				stats.Size -= entry.Size
				stats.Expirations++
			})
		}
	}

	c.updateStats(func(stats *CacheStats) {
		stats.Entries = len(c.cache)
		stats.LastCleanup = now
	})

	if len(expired) > 0 {
		if c.logger != nil {
			c.logger.Debug("cache cleanup completed",
				logger.Int("expired_entries", len(expired)),
			)
		}

		if c.metrics != nil {
			c.metrics.Counter("forge.ai.inference_cache_expired").Add(float64(len(expired)))
		}
	}
}

// loadPersistentCache loads the cache from persistent storage
func (c *InferenceCache) loadPersistentCache() error {
	// Simplified persistence - in practice, would use proper serialization
	return nil
}

// savePersistentCache saves the cache to persistent storage
func (c *InferenceCache) savePersistentCache() error {
	// Simplified persistence - in practice, would use proper serialization
	c.updateStats(func(stats *CacheStats) {
		stats.PersistenceOps++
		stats.LastPersistence = time.Now()
	})
	return nil
}

// updateStats updates cache statistics
func (c *InferenceCache) updateStats(fn func(*CacheStats)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.stats)
}

// collectStats collects and updates statistics
func (c *InferenceCache) collectStats() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.Entries = len(c.cache)
	c.stats.LastUpdated = time.Now()
}

// LRU List implementation

// NewLRUList creates a new LRU list
func NewLRUList() *LRUList {
	return &LRUList{}
}

// AddToFront adds an entry to the front of the LRU list
func (l *LRUList) AddToFront(key string, entry *CacheEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	node := &LRUNode{
		key:   key,
		entry: entry,
	}

	if l.head == nil {
		l.head = node
		l.tail = node
	} else {
		node.next = l.head
		l.head.prev = node
		l.head = node
	}

	l.size++
}

// MoveToFront moves an entry to the front of the LRU list
func (l *LRUList) MoveToFront(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Find the node
	node := l.findNode(key)
	if node == nil || node == l.head {
		return
	}

	// Remove from current position
	l.removeNode(node)

	// Add to front
	node.next = l.head
	node.prev = nil
	l.head.prev = node
	l.head = node
}

// Remove removes an entry from the LRU list
func (l *LRUList) Remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	node := l.findNode(key)
	if node != nil {
		l.removeNode(node)
		l.size--
	}
}

// RemoveTail removes and returns the key of the tail entry
func (l *LRUList) RemoveTail() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.tail == nil {
		return ""
	}

	key := l.tail.key
	l.removeNode(l.tail)
	l.size--
	return key
}

// Size returns the size of the LRU list
func (l *LRUList) Size() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.size
}

// findNode finds a node by key
func (l *LRUList) findNode(key string) *LRUNode {
	current := l.head
	for current != nil {
		if current.key == key {
			return current
		}
		current = current.next
	}
	return nil
}

// removeNode removes a node from the list
func (l *LRUList) removeNode(node *LRUNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		l.head = node.next
	}

	if node.next != nil {
		node.next.prev = node.prev
	} else {
		l.tail = node.prev
	}
}
