package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
)

// ShardedMemoryCache implements a sharded in-memory cache for better concurrency
type ShardedMemoryCache struct {
	name       string
	config     *cachecore.MemoryConfig
	logger     common.Logger
	metrics    common.Metrics
	shards     []*MemoryShard
	shardCount int
	shardMask  uint64
	hasher     Hasher
	startTime  time.Time
	running    bool
	mu         sync.RWMutex
}

// MemoryShard represents a single shard of the cache
type MemoryShard struct {
	id           int
	data         map[string]*CacheEntry
	lru          *LRUList
	lfu          *LFUHeap
	ttlHeap      *TTLHeap
	evictPolicy  cachecore.EvictionPolicy
	maxSize      int64
	maxMemory    int64
	currentSize  int64
	currentCount int64

	// Statistics
	hits        int64
	misses      int64
	sets        int64
	deletes     int64
	evictions   int64
	expirations int64

	mu sync.RWMutex
}

// Hasher defines the interface for key hashing
type Hasher interface {
	Hash(key string) uint64
}

// FNVHasher implements FNV-1a hashing
type FNVHasher struct{}

// ConsistentHasher implements consistent hashing
type ConsistentHasher struct {
	vnodes map[uint64]int
	ring   []uint64
	mu     sync.RWMutex
}

// NewShardedMemoryCache creates a new sharded memory cache
func NewShardedMemoryCache(name string, config *cachecore.MemoryConfig, logger common.Logger, metrics common.Metrics) (*ShardedMemoryCache, error) {
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

	// Ensure shard count is a power of 2 for efficient masking
	shardCount := nextPowerOfTwo(config.ShardCount)
	shardMask := uint64(shardCount - 1)

	cache := &ShardedMemoryCache{
		name:       name,
		config:     config,
		logger:     logger,
		metrics:    metrics,
		shardCount: shardCount,
		shardMask:  shardMask,
		hasher:     &FNVHasher{},
		startTime:  time.Now(),
		running:    false,
	}

	// Initialize shards
	cache.shards = make([]*MemoryShard, shardCount)
	maxSizePerShard := config.MaxSize / int64(shardCount)
	maxMemoryPerShard := config.MaxMemory / int64(shardCount)

	for i := 0; i < shardCount; i++ {
		cache.shards[i] = &MemoryShard{
			id:          i,
			data:        make(map[string]*CacheEntry),
			lru:         NewLRUList(),
			lfu:         NewLFUHeap(),
			ttlHeap:     NewTTLHeap(),
			evictPolicy: config.EvictionPolicy,
			maxSize:     maxSizePerShard,
			maxMemory:   maxMemoryPerShard,
		}
	}

	return cache, nil
}

// getShard returns the shard for a given key
func (smc *ShardedMemoryCache) getShard(key string) *MemoryShard {
	hash := smc.hasher.Hash(key)
	return smc.shards[hash&smc.shardMask]
}

// Get retrieves a value from the cache
func (smc *ShardedMemoryCache) Get(ctx context.Context, key string) (interface{}, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		return nil, err
	}

	shard := smc.getShard(key)
	return smc.getFromShard(ctx, shard, key)
}

// Set stores a value in the cache
func (smc *ShardedMemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if err := cachecore.ValidateKey(key); err != nil {
		return err
	}

	if err := cachecore.ValidateTTL(ttl); err != nil {
		return err
	}

	shard := smc.getShard(key)
	return smc.setInShard(ctx, shard, key, value, ttl)
}

// Delete removes a value from the cache
func (smc *ShardedMemoryCache) Delete(ctx context.Context, key string) error {
	if err := cachecore.ValidateKey(key); err != nil {
		return err
	}

	shard := smc.getShard(key)
	return smc.deleteFromShard(ctx, shard, key)
}

// Exists checks if a key exists in the cache
func (smc *ShardedMemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		return false, err
	}

	shard := smc.getShard(key)
	return smc.existsInShard(ctx, shard, key)
}

// GetMulti retrieves multiple values from the cache
func (smc *ShardedMemoryCache) GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error) {
	if len(keys) == 0 {
		return make(map[string]interface{}), nil
	}

	// Group keys by shard for efficient access
	shardKeys := make(map[int][]string)
	for _, key := range keys {
		if err := cachecore.ValidateKey(key); err != nil {
			continue
		}
		shard := smc.getShard(key)
		shardKeys[shard.id] = append(shardKeys[shard.id], key)
	}

	result := make(map[string]interface{})
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process each shard concurrently
	for shardID, keys := range shardKeys {
		wg.Add(1)
		go func(shardID int, keys []string) {
			defer wg.Done()
			shard := smc.shards[shardID]

			for _, key := range keys {
				if value, err := smc.getFromShard(ctx, shard, key); err == nil {
					mu.Lock()
					result[key] = value
					mu.Unlock()
				}
			}
		}(shardID, keys)
	}

	wg.Wait()
	return result, nil
}

// SetMulti stores multiple values in the cache
func (smc *ShardedMemoryCache) SetMulti(ctx context.Context, items map[string]cachecore.CacheItem) error {
	// Group items by shard
	shardItems := make(map[int]map[string]cachecore.CacheItem)
	for key, item := range items {
		if err := cachecore.ValidateKey(key); err != nil {
			continue
		}
		shard := smc.getShard(key)
		if shardItems[shard.id] == nil {
			shardItems[shard.id] = make(map[string]cachecore.CacheItem)
		}
		shardItems[shard.id][key] = item
	}

	var wg sync.WaitGroup
	var errors []error
	var mu sync.Mutex

	// Process each shard concurrently
	for shardID, items := range shardItems {
		wg.Add(1)
		go func(shardID int, items map[string]cachecore.CacheItem) {
			defer wg.Done()
			shard := smc.shards[shardID]

			for key, item := range items {
				if err := smc.setInShard(ctx, shard, key, item.Value, item.TTL); err != nil {
					mu.Lock()
					errors = append(errors, err)
					mu.Unlock()
				}
			}
		}(shardID, items)
	}

	wg.Wait()

	if len(errors) > 0 {
		return errors[0] // Return first error
	}
	return nil
}

// DeleteMulti removes multiple values from the cache
func (smc *ShardedMemoryCache) DeleteMulti(ctx context.Context, keys []string) error {
	// Group keys by shard
	shardKeys := make(map[int][]string)
	for _, key := range keys {
		if err := cachecore.ValidateKey(key); err != nil {
			continue
		}
		shard := smc.getShard(key)
		shardKeys[shard.id] = append(shardKeys[shard.id], key)
	}

	var wg sync.WaitGroup

	// Process each shard concurrently
	for shardID, keys := range shardKeys {
		wg.Add(1)
		go func(shardID int, keys []string) {
			defer wg.Done()
			shard := smc.shards[shardID]

			for _, key := range keys {
				smc.deleteFromShard(ctx, shard, key)
			}
		}(shardID, keys)
	}

	wg.Wait()
	return nil
}

// Flush clears all keys in the cache
func (smc *ShardedMemoryCache) Flush(ctx context.Context) error {
	var wg sync.WaitGroup

	for _, shard := range smc.shards {
		wg.Add(1)
		go func(shard *MemoryShard) {
			defer wg.Done()
			smc.flushShard(shard)
		}(shard)
	}

	wg.Wait()
	return nil
}

// Size returns the total number of keys in the cache
func (smc *ShardedMemoryCache) Size(ctx context.Context) (int64, error) {
	var total int64
	for _, shard := range smc.shards {
		atomic.AddInt64(&total, atomic.LoadInt64(&shard.currentCount))
	}
	return total, nil
}

// Stats returns cache statistics
func (smc *ShardedMemoryCache) Stats() cachecore.CacheStats {
	var totalHits, totalMisses, totalSets, totalDeletes, totalEvictions, totalExpirations int64
	var totalSize, totalMemory int64

	shardStats := make(map[string]int64)

	for i, shard := range smc.shards {
		shard.mu.RLock()

		totalHits += shard.hits
		totalMisses += shard.misses
		totalSets += shard.sets
		totalDeletes += shard.deletes
		totalEvictions += shard.evictions
		totalExpirations += shard.expirations
		totalSize += shard.currentCount
		totalMemory += shard.currentSize

		shardStats[fmt.Sprintf("shard_%d_items", i)] = shard.currentCount
		shardStats[fmt.Sprintf("shard_%d_memory", i)] = shard.currentSize
		shardStats[fmt.Sprintf("shard_%d_hits", i)] = shard.hits
		shardStats[fmt.Sprintf("shard_%d_misses", i)] = shard.misses

		shard.mu.RUnlock()
	}

	return cachecore.CacheStats{
		Name:     smc.name,
		Type:     string(cachecore.CacheTypeMemory),
		Hits:     totalHits,
		Misses:   totalMisses,
		Sets:     totalSets,
		Deletes:  totalDeletes,
		Size:     totalSize,
		Memory:   totalMemory,
		HitRatio: cachecore.CalculateHitRatio(totalHits, totalMisses),
		Uptime:   time.Since(smc.startTime),
		Custom: map[string]interface{}{
			"evictions":   totalEvictions,
			"expirations": totalExpirations,
			"shard_count": len(smc.shards),
			"shards":      shardStats,
		},
	}
}

// GetShardStats returns statistics for individual shards
func (smc *ShardedMemoryCache) GetShardStats() []ShardStats {
	stats := make([]ShardStats, len(smc.shards))

	for i, shard := range smc.shards {
		shard.mu.RLock()
		stats[i] = ShardStats{
			ID:          i,
			Items:       shard.currentCount,
			Memory:      shard.currentSize,
			Hits:        shard.hits,
			Misses:      shard.misses,
			Sets:        shard.sets,
			Deletes:     shard.deletes,
			Evictions:   shard.evictions,
			Expirations: shard.expirations,
			LoadFactor:  float64(shard.currentCount) / float64(shard.maxSize),
		}
		shard.mu.RUnlock()
	}

	return stats
}

// ShardStats contains statistics for a single shard
type ShardStats struct {
	ID          int     `json:"id"`
	Size        int64   `json:"size"`
	Count       int64   `json:"count"`
	Items       int64   `json:"items"`
	Memory      int64   `json:"memory"`
	Hits        int64   `json:"hits"`
	Misses      int64   `json:"misses"`
	Sets        int64   `json:"sets"`
	Deletes     int64   `json:"deletes"`
	Evictions   int64   `json:"evictions"`
	Expirations int64   `json:"expirations"`
	LoadFactor  float64 `json:"load_factor"`
}

// Shard-level operations

func (smc *ShardedMemoryCache) getFromShard(ctx context.Context, shard *MemoryShard, key string) (interface{}, error) {
	shard.mu.RLock()
	entry, exists := shard.data[key]
	shard.mu.RUnlock()

	if !exists {
		atomic.AddInt64(&shard.misses, 1)
		return nil, cachecore.ErrCacheNotFound
	}

	// Check TTL
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		smc.deleteFromShard(ctx, shard, key)
		atomic.AddInt64(&shard.misses, 1)
		atomic.AddInt64(&shard.expirations, 1)
		return nil, cachecore.ErrCacheNotFound
	}

	// Update access information
	entry.AccessedAt = time.Now()
	atomic.AddInt64(&entry.AccessCount, 1)

	// Update eviction policy structures
	shard.mu.Lock()
	switch shard.evictPolicy {
	case cachecore.EvictionPolicyLRU:
		shard.lru.MoveToFront(entry)
	case cachecore.EvictionPolicyLFU:
		shard.lfu.Get(key)
	}
	shard.mu.Unlock()

	atomic.AddInt64(&shard.hits, 1)
	return entry.Value, nil
}

func (smc *ShardedMemoryCache) setInShard(ctx context.Context, shard *MemoryShard, key string, value interface{}, ttl time.Duration) error {
	now := time.Now()
	size := smc.calculateSize(value)

	entry := &CacheEntry{
		Key:         key,
		Value:       value,
		CreatedAt:   now,
		UpdatedAt:   now,
		AccessedAt:  now,
		TTL:         ttl,
		Size:        size,
		AccessCount: 1,
	}

	if ttl > 0 {
		entry.ExpiresAt = now.Add(ttl)
	}

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Check if key already exists
	if existingEntry, exists := shard.data[key]; exists {
		// Update existing entry
		oldSize := existingEntry.Size
		existingEntry.Value = value
		existingEntry.UpdatedAt = now
		existingEntry.AccessedAt = now
		existingEntry.TTL = ttl
		existingEntry.ExpiresAt = entry.ExpiresAt
		existingEntry.Size = size

		atomic.AddInt64(&shard.currentSize, size-oldSize)

		// Update eviction policy structures
		switch shard.evictPolicy {
		case cachecore.EvictionPolicyLRU:
			shard.lru.MoveToFront(existingEntry)
		case cachecore.EvictionPolicyLFU:
			shard.lfu.Put(key, value)
		}

		if ttl > 0 {
			shard.ttlHeap.Update(existingEntry)
		}
	} else {
		// New entry
		shard.data[key] = entry
		atomic.AddInt64(&shard.currentCount, 1)
		atomic.AddInt64(&shard.currentSize, size)

		// Add to eviction policy structures
		switch shard.evictPolicy {
		case cachecore.EvictionPolicyLRU:
			shard.lru.PushFront(entry)
		case cachecore.EvictionPolicyLFU:
			shard.lfu.Put(key, value)
		}

		if ttl > 0 {
			shard.ttlHeap.Push(entry)
		}

		// Check if eviction is needed
		smc.maybeEvictFromShard(shard)
	}

	atomic.AddInt64(&shard.sets, 1)
	return nil
}

func (smc *ShardedMemoryCache) deleteFromShard(ctx context.Context, shard *MemoryShard, key string) error {
	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, exists := shard.data[key]
	if !exists {
		return nil
	}

	// Remove from all structures
	delete(shard.data, key)
	atomic.AddInt64(&shard.currentCount, -1)
	atomic.AddInt64(&shard.currentSize, -entry.Size)

	switch shard.evictPolicy {
	case cachecore.EvictionPolicyLRU:
		shard.lru.Remove(entry)
	case cachecore.EvictionPolicyLFU:
		shard.lfu.Remove(key)
	}

	if !entry.ExpiresAt.IsZero() {
		shard.ttlHeap.Remove(entry)
	}

	atomic.AddInt64(&shard.deletes, 1)
	return nil
}

func (smc *ShardedMemoryCache) existsInShard(ctx context.Context, shard *MemoryShard, key string) (bool, error) {
	shard.mu.RLock()
	entry, exists := shard.data[key]
	shard.mu.RUnlock()

	if !exists {
		return false, nil
	}

	// Check TTL
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		smc.deleteFromShard(ctx, shard, key)
		return false, nil
	}

	return true, nil
}

func (smc *ShardedMemoryCache) flushShard(shard *MemoryShard) {
	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.data = make(map[string]*CacheEntry)
	shard.lru = NewLRUList()
	shard.lfu = NewLFUHeap()
	shard.ttlHeap = NewTTLHeap()
	atomic.StoreInt64(&shard.currentCount, 0)
	atomic.StoreInt64(&shard.currentSize, 0)
}

func (smc *ShardedMemoryCache) maybeEvictFromShard(shard *MemoryShard) {
	if shard.currentCount <= shard.maxSize && shard.currentSize <= shard.maxMemory {
		return
	}

	// Evict entries until under limits
	evictCount := int(float64(shard.currentCount-shard.maxSize)*smc.config.EvictionRate) + 1
	if evictCount <= 0 {
		evictCount = 1
	}

	for i := 0; i < evictCount && (shard.currentCount > shard.maxSize || shard.currentSize > shard.maxMemory); i++ {
		var keyToEvict string

		switch shard.evictPolicy {
		case cachecore.EvictionPolicyLRU:
			if entry := shard.lru.Back(); entry != nil {
				keyToEvict = entry.Key
			}
		case cachecore.EvictionPolicyLFU:
			if node := shard.lfu.RemoveLFU(); node != nil {
				keyToEvict = node.key
			}
		case cachecore.EvictionPolicyTTL:
			if entry := shard.ttlHeap.Peek(); entry != nil {
				keyToEvict = entry.Key
			}
		}

		if keyToEvict != "" {
			if entry, exists := shard.data[keyToEvict]; exists {
				delete(shard.data, keyToEvict)
				atomic.AddInt64(&shard.currentCount, -1)
				atomic.AddInt64(&shard.currentSize, -entry.Size)
				atomic.AddInt64(&shard.evictions, 1)

				// Remove from other structures
				switch shard.evictPolicy {
				case cachecore.EvictionPolicyLRU:
					shard.lru.Remove(entry)
				case cachecore.EvictionPolicyLFU:
					// Already removed above
				}

				if !entry.ExpiresAt.IsZero() {
					shard.ttlHeap.Remove(entry)
				}
			}
		} else {
			break // No more entries to evict
		}
	}
}

// Helper methods

func (smc *ShardedMemoryCache) calculateSize(value interface{}) int64 {
	// Simplified size calculation
	// In production, you might want more accurate sizing
	return 64 // Base overhead + rough estimate
}

// Hash implements FNV-1a hashing
func (f *FNVHasher) Hash(key string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return h.Sum64()
}

// NewConsistentHasher creates a new consistent hasher
func NewConsistentHasher(shardCount int, virtualNodes int) *ConsistentHasher {
	ch := &ConsistentHasher{
		vnodes: make(map[uint64]int),
		ring:   make([]uint64, 0),
	}

	hasher := fnv.New64a()

	// Create virtual nodes for each shard
	for i := 0; i < shardCount; i++ {
		for j := 0; j < virtualNodes; j++ {
			hasher.Reset()
			hasher.Write([]byte(fmt.Sprintf("shard:%d:vnode:%d", i, j)))
			hash := hasher.Sum64()
			ch.vnodes[hash] = i
			ch.ring = append(ch.ring, hash)
		}
	}

	// Sort the ring
	for i := 0; i < len(ch.ring)-1; i++ {
		for j := i + 1; j < len(ch.ring); j++ {
			if ch.ring[i] > ch.ring[j] {
				ch.ring[i], ch.ring[j] = ch.ring[j], ch.ring[i]
			}
		}
	}

	return ch
}

// Hash implements consistent hashing
func (ch *ConsistentHasher) Hash(key string) uint64 {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	hasher := fnv.New64a()
	hasher.Write([]byte(key))
	hash := hasher.Sum64()

	// Find the first virtual node >= hash
	idx := ch.search(hash)
	return uint64(ch.vnodes[ch.ring[idx]])
}

func (ch *ConsistentHasher) search(hash uint64) int {
	// Binary search for the first virtual node >= hash
	left, right := 0, len(ch.ring)
	for left < right {
		mid := (left + right) / 2
		if ch.ring[mid] < hash {
			left = mid + 1
		} else {
			right = mid
		}
	}

	if left == len(ch.ring) {
		return 0 // Wrap around to the first virtual node
	}
	return left
}

// nextPowerOfTwo returns the next power of 2 >= n
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}

	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n++

	return n
}

// TTLHeap implementation for TTL-based eviction
type TTLHeap struct {
	entries []*CacheEntry
	mu      sync.RWMutex
}

func NewTTLHeap() *TTLHeap {
	return &TTLHeap{
		entries: make([]*CacheEntry, 0),
	}
}

func (h *TTLHeap) Push(entry *CacheEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = append(h.entries, entry)
	h.heapifyUp(len(h.entries) - 1)
}

func (h *TTLHeap) Pop() *CacheEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.entries) == 0 {
		return nil
	}

	root := h.entries[0]
	last := h.entries[len(h.entries)-1]
	h.entries = h.entries[:len(h.entries)-1]

	if len(h.entries) > 0 {
		h.entries[0] = last
		h.heapifyDown(0)
	}

	return root
}

func (h *TTLHeap) Peek() *CacheEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.entries) == 0 {
		return nil
	}
	return h.entries[0]
}

func (h *TTLHeap) Remove(entry *CacheEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Find entry index
	idx := -1
	for i, e := range h.entries {
		if e == entry {
			idx = i
			break
		}
	}

	if idx == -1 {
		return
	}

	// Replace with last element
	last := h.entries[len(h.entries)-1]
	h.entries = h.entries[:len(h.entries)-1]

	if idx < len(h.entries) {
		h.entries[idx] = last
		h.heapifyDown(idx)
		h.heapifyUp(idx)
	}
}

func (h *TTLHeap) Update(entry *CacheEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Find entry and re-heapify
	for i, e := range h.entries {
		if e == entry {
			h.heapifyDown(i)
			h.heapifyUp(i)
			break
		}
	}
}

func (h *TTLHeap) heapifyUp(idx int) {
	for idx > 0 {
		parent := (idx - 1) / 2
		if h.entries[idx].ExpiresAt.Before(h.entries[parent].ExpiresAt) {
			h.entries[idx], h.entries[parent] = h.entries[parent], h.entries[idx]
			idx = parent
		} else {
			break
		}
	}
}

func (h *TTLHeap) heapifyDown(idx int) {
	for {
		left := 2*idx + 1
		right := 2*idx + 2
		smallest := idx

		if left < len(h.entries) && h.entries[left].ExpiresAt.Before(h.entries[smallest].ExpiresAt) {
			smallest = left
		}

		if right < len(h.entries) && h.entries[right].ExpiresAt.Before(h.entries[smallest].ExpiresAt) {
			smallest = right
		}

		if smallest != idx {
			h.entries[idx], h.entries[smallest] = h.entries[smallest], h.entries[idx]
			idx = smallest
		} else {
			break
		}
	}
}
