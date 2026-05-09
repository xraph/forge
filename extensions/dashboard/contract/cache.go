package contract

import (
	"container/list"
	"sync"
	"time"
)

// GraphCacheKey is the (route, permissionsHash, shellVersion) tuple keyed by the cache.
type GraphCacheKey struct {
	Route           string
	PermissionsHash string
	ShellVersion    string
}

// GraphCache is a small LRU+TTL cache. Bust on contributor manifest reload.
type GraphCache struct {
	mu    sync.Mutex
	cap   int
	ttl   time.Duration
	items map[GraphCacheKey]*list.Element
	order *list.List // front = MRU
}

type graphEntry struct {
	key   GraphCacheKey
	value *GraphNode
	at    time.Time
}

// NewGraphCache creates a cache with the given max size and TTL per entry.
// TTL of 0 disables expiry.
func NewGraphCache(maxEntries int, ttl time.Duration) *GraphCache {
	if maxEntries < 1 {
		maxEntries = 64
	}
	return &GraphCache{
		cap:   maxEntries,
		ttl:   ttl,
		items: map[GraphCacheKey]*list.Element{},
		order: list.New(),
	}
}

func (c *GraphCache) Get(k GraphCacheKey) (*GraphNode, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		return nil, false
	}
	e := el.Value.(*graphEntry)
	if c.ttl > 0 && time.Since(e.at) > c.ttl {
		c.order.Remove(el)
		delete(c.items, k)
		return nil, false
	}
	c.order.MoveToFront(el)
	return e.value, true
}

func (c *GraphCache) Put(k GraphCacheKey, v *GraphNode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		e := el.Value.(*graphEntry)
		e.value = v
		e.at = time.Now()
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&graphEntry{key: k, value: v, at: time.Now()})
	c.items[k] = el
	if c.order.Len() > c.cap {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*graphEntry).key)
		}
	}
}

// BustAll clears the cache. Call after a contributor manifest reload or shell deploy.
func (c *GraphCache) BustAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[GraphCacheKey]*list.Element{}
	c.order = list.New()
}
