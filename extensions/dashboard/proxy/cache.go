package proxy

import (
	"sync"
	"time"
)

// CacheEntry holds a cached HTML fragment with expiry tracking.
type CacheEntry struct {
	Data      []byte
	FetchedAt time.Time
	ExpiresAt time.Time
}

// IsExpired returns true if the cache entry has expired.
func (e *CacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// Age returns how old this cache entry is.
func (e *CacheEntry) Age() time.Duration {
	return time.Since(e.FetchedAt)
}

// FragmentCache is an LRU+TTL cache for HTML fragments from remote contributors.
type FragmentCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	maxSize int
	ttl     time.Duration
	order   []string // LRU order — most recent at end
}

// NewFragmentCache creates a new fragment cache.
func NewFragmentCache(maxSize int, ttl time.Duration) *FragmentCache {
	return &FragmentCache{
		entries: make(map[string]*CacheEntry, maxSize),
		maxSize: maxSize,
		ttl:     ttl,
		order:   make([]string, 0, maxSize),
	}
}

// Get retrieves a cached fragment. Returns nil if not found or expired.
func (c *FragmentCache) Get(key string) *CacheEntry {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	if entry.IsExpired() {
		// Don't remove here — let it be served as stale if needed
		return nil
	}

	// Move to end of LRU order (most recently used)
	c.mu.Lock()
	c.touchLocked(key)
	c.mu.Unlock()

	return entry
}

// GetStale retrieves a cached fragment even if expired.
// This is used by the recovery system to serve stale content when remotes are down.
func (c *FragmentCache) GetStale(key string) *CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[key]
}

// Set stores a fragment in the cache.
func (c *FragmentCache) Set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// If at capacity and key doesn't already exist, evict oldest
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxSize {
		c.evictOldestLocked()
	}

	c.entries[key] = &CacheEntry{
		Data:      data,
		FetchedAt: now,
		ExpiresAt: now.Add(c.ttl),
	}

	c.touchLocked(key)
}

// Delete removes a cached entry.
func (c *FragmentCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
	c.removeLRULocked(key)
}

// Clear removes all cached entries.
func (c *FragmentCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry, c.maxSize)
	c.order = c.order[:0]
}

// Size returns the number of cached entries.
func (c *FragmentCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// touchLocked moves a key to the end of the LRU order (caller must hold lock).
func (c *FragmentCache) touchLocked(key string) {
	c.removeLRULocked(key)
	c.order = append(c.order, key)
}

// removeLRULocked removes a key from the LRU order list (caller must hold lock).
func (c *FragmentCache) removeLRULocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// evictOldestLocked removes the least recently used entry (caller must hold lock).
func (c *FragmentCache) evictOldestLocked() {
	if len(c.order) == 0 {
		return
	}

	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}

// CacheKey generates a cache key for a contributor's page or widget.
func CacheKey(contributor, resourceType, resourceID string) string {
	return contributor + ":" + resourceType + ":" + resourceID
}
