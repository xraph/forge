package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
)

// CacheStore is the gateway's interface for a cache backend.
// This decouples from the concrete cache extension, allowing the gateway
// to use any backend that implements this interface.
type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// CachedResponse represents a cached HTTP response.
type CachedResponse struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
	CachedAt   time.Time         `json:"cachedAt"`
	TTL        time.Duration     `json:"ttl"`
}

// ResponseCache provides HTTP response caching for the gateway.
// It supports:
//   - Configurable cache backends (in-memory, external via CacheStore interface)
//   - Per-route cache policies with TTL, vary-by headers, and method filtering
//   - Cache-Control header respect (no-cache, no-store, max-age)
//   - Automatic cache key generation based on method, path, query, and vary-by headers
//   - Cache invalidation via admin API
//   - Metrics (hits/misses)
type ResponseCache struct {
	config  CachingConfig
	logger  forge.Logger
	store   CacheStore
	stats   *CacheStats
	enabled bool
}

// CacheStats tracks cache hit/miss metrics.
type CacheStats struct {
	hits   atomic.Int64
	misses atomic.Int64
}

// Hits returns the total cache hits.
func (cs *CacheStats) Hits() int64 { return cs.hits.Load() }

// Misses returns the total cache misses.
func (cs *CacheStats) Misses() int64 { return cs.misses.Load() }

// NewResponseCache creates a new response cache.
func NewResponseCache(config CachingConfig, logger forge.Logger, store CacheStore) *ResponseCache {
	rc := &ResponseCache{
		config:  config,
		logger:  logger,
		store:   store,
		stats:   &CacheStats{},
		enabled: config.Enabled,
	}

	// If no external store is provided, use an in-memory LRU cache
	if store == nil && config.Enabled {
		rc.store = NewInMemoryCacheStore(config.MaxSize)
	}

	return rc
}

// Get attempts to retrieve a cached response for the given request.
// Returns nil if not cached, cache is disabled, or the request is not cacheable.
func (rc *ResponseCache) Get(r *http.Request, route *Route) *CachedResponse {
	if !rc.enabled || rc.store == nil {
		return nil
	}

	// Check if the method is cacheable
	if !rc.isCacheableMethod(r.Method, route) {
		return nil
	}

	// Respect Cache-Control: no-cache from client
	if cc := r.Header.Get("Cache-Control"); strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		return nil
	}

	key := rc.cacheKey(r, route)

	data, err := rc.store.Get(r.Context(), key)
	if err != nil {
		rc.stats.misses.Add(1)
		return nil
	}

	// Deserialize the cached response
	cached, err := deserializeCachedResponse(data)
	if err != nil {
		rc.logger.Debug("failed to deserialize cached response",
			forge.F("key", key),
			forge.F("error", err),
		)
		rc.stats.misses.Add(1)
		return nil
	}

	// Check TTL expiry
	if time.Since(cached.CachedAt) > cached.TTL {
		// Expired - delete and return miss
		_ = rc.store.Delete(r.Context(), key)
		rc.stats.misses.Add(1)
		return nil
	}

	rc.stats.hits.Add(1)
	return cached
}

// Set stores a response in the cache.
func (rc *ResponseCache) Set(r *http.Request, route *Route, statusCode int, headers http.Header, body []byte) {
	if !rc.enabled || rc.store == nil {
		return
	}

	// Only cache successful responses
	if statusCode < 200 || statusCode >= 400 {
		return
	}

	// Respect upstream Cache-Control: no-store
	if cc := headers.Get("Cache-Control"); strings.Contains(cc, "no-store") || strings.Contains(cc, "private") {
		return
	}

	// Check if the method is cacheable
	if !rc.isCacheableMethod(r.Method, route) {
		return
	}

	ttl := rc.effectiveTTL(route, headers)
	if ttl <= 0 {
		return
	}

	cached := &CachedResponse{
		StatusCode: statusCode,
		Headers:    flattenHeaders(headers),
		Body:       body,
		CachedAt:   time.Now(),
		TTL:        ttl,
	}

	data, err := serializeCachedResponse(cached)
	if err != nil {
		rc.logger.Debug("failed to serialize response for cache",
			forge.F("error", err),
		)
		return
	}

	key := rc.cacheKey(r, route)
	if err := rc.store.Set(r.Context(), key, data, ttl); err != nil {
		rc.logger.Debug("failed to store cached response",
			forge.F("key", key),
			forge.F("error", err),
		)
	}
}

// WriteCachedResponse writes a cached response to the client.
func (rc *ResponseCache) WriteCachedResponse(w http.ResponseWriter, cached *CachedResponse) {
	for k, v := range cached.Headers {
		w.Header().Set(k, v)
	}

	w.Header().Set("X-Cache", "HIT")
	w.Header().Set("X-Cache-Age", fmt.Sprintf("%.0f", time.Since(cached.CachedAt).Seconds()))
	w.WriteHeader(cached.StatusCode)

	if len(cached.Body) > 0 {
		_, _ = w.Write(cached.Body)
	}
}

// Invalidate removes a cached entry for the given request.
func (rc *ResponseCache) Invalidate(ctx context.Context, method, path string) error {
	if rc.store == nil {
		return nil
	}

	key := fmt.Sprintf("gw:cache:%s:%s", method, path)
	return rc.store.Delete(ctx, key)
}

// Stats returns the cache statistics.
func (rc *ResponseCache) Stats() *CacheStats {
	return rc.stats
}

// cacheKey generates a unique cache key for a request.
func (rc *ResponseCache) cacheKey(r *http.Request, route *Route) string {
	h := sha256.New()
	h.Write([]byte(r.Method))
	h.Write([]byte(r.URL.Path))
	h.Write([]byte(r.URL.RawQuery))

	// Include vary-by headers in the key
	varyBy := rc.config.VaryBy
	if route != nil && route.Cache != nil && len(route.Cache.VaryBy) > 0 {
		varyBy = route.Cache.VaryBy
	}

	if len(varyBy) > 0 {
		// Sort for deterministic keys
		sorted := make([]string, len(varyBy))
		copy(sorted, varyBy)
		sort.Strings(sorted)

		for _, header := range sorted {
			h.Write([]byte(header))
			h.Write([]byte(r.Header.Get(header)))
		}
	}

	return "gw:cache:" + hex.EncodeToString(h.Sum(nil))[:32]
}

// isCacheableMethod checks if the HTTP method is cacheable.
func (rc *ResponseCache) isCacheableMethod(method string, route *Route) bool {
	methods := rc.config.Methods
	if route != nil && route.Cache != nil && len(route.Cache.Methods) > 0 {
		methods = route.Cache.Methods
	}

	for _, m := range methods {
		if strings.EqualFold(m, method) {
			return true
		}
	}

	return false
}

// effectiveTTL determines the cache TTL for a response.
func (rc *ResponseCache) effectiveTTL(route *Route, headers http.Header) time.Duration {
	// Per-route TTL takes precedence
	if route != nil && route.Cache != nil && route.Cache.TTL > 0 {
		return route.Cache.TTL
	}

	// Respect upstream Cache-Control max-age
	if cc := headers.Get("Cache-Control"); cc != "" {
		for _, directive := range strings.Split(cc, ",") {
			directive = strings.TrimSpace(directive)
			if strings.HasPrefix(directive, "max-age=") {
				ageStr := strings.TrimPrefix(directive, "max-age=")
				var seconds int
				for _, ch := range ageStr {
					if ch < '0' || ch > '9' {
						break
					}
					seconds = seconds*10 + int(ch-'0')
				}

				if seconds > 0 {
					return time.Duration(seconds) * time.Second
				}
			}
		}
	}

	// Fall back to default TTL
	return rc.config.DefaultTTL
}

// flattenHeaders converts http.Header to a simple map (takes first value).
func flattenHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}

	return result
}

// Simple serialization for cached responses using a compact binary format.
// Using a basic length-prefixed format to avoid external JSON dependency overhead.

func serializeCachedResponse(cr *CachedResponse) ([]byte, error) {
	var buf bytes.Buffer

	// Status code (4 bytes)
	buf.Write([]byte{
		byte(cr.StatusCode >> 24),
		byte(cr.StatusCode >> 16),
		byte(cr.StatusCode >> 8),
		byte(cr.StatusCode),
	})

	// Timestamp (8 bytes, unix nano)
	ts := cr.CachedAt.UnixNano()
	for i := 7; i >= 0; i-- {
		buf.WriteByte(byte(ts >> (i * 8)))
	}

	// TTL (8 bytes, nanoseconds)
	ttl := int64(cr.TTL)
	for i := 7; i >= 0; i-- {
		buf.WriteByte(byte(ttl >> (i * 8)))
	}

	// Headers count (2 bytes)
	headerCount := len(cr.Headers)
	buf.WriteByte(byte(headerCount >> 8))
	buf.WriteByte(byte(headerCount))

	// Headers
	for k, v := range cr.Headers {
		writeString(&buf, k)
		writeString(&buf, v)
	}

	// Body
	buf.Write(cr.Body)

	return buf.Bytes(), nil
}

func deserializeCachedResponse(data []byte) (*CachedResponse, error) {
	if len(data) < 22 { // min: 4 (status) + 8 (ts) + 8 (ttl) + 2 (header count)
		return nil, fmt.Errorf("invalid cached response: too short")
	}

	cr := &CachedResponse{}
	pos := 0

	// Status code
	cr.StatusCode = int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	pos = 4

	// Timestamp
	var ts int64
	for i := 0; i < 8; i++ {
		ts = ts<<8 | int64(data[pos+i])
	}
	cr.CachedAt = time.Unix(0, ts)
	pos += 8

	// TTL
	var ttl int64
	for i := 0; i < 8; i++ {
		ttl = ttl<<8 | int64(data[pos+i])
	}
	cr.TTL = time.Duration(ttl)
	pos += 8

	// Header count
	headerCount := int(data[pos])<<8 | int(data[pos+1])
	pos += 2

	cr.Headers = make(map[string]string, headerCount)
	for i := 0; i < headerCount; i++ {
		k, newPos, err := readString(data, pos)
		if err != nil {
			return nil, fmt.Errorf("invalid cached response: reading header key: %w", err)
		}
		pos = newPos

		v, newPos, err := readString(data, pos)
		if err != nil {
			return nil, fmt.Errorf("invalid cached response: reading header value: %w", err)
		}
		pos = newPos

		cr.Headers[k] = v
	}

	// Body = remaining bytes
	if pos < len(data) {
		cr.Body = data[pos:]
	}

	return cr, nil
}

func writeString(buf *bytes.Buffer, s string) {
	l := len(s)
	buf.WriteByte(byte(l >> 8))
	buf.WriteByte(byte(l))
	buf.WriteString(s)
}

func readString(data []byte, pos int) (string, int, error) {
	if pos+2 > len(data) {
		return "", 0, fmt.Errorf("short read at position %d", pos)
	}

	l := int(data[pos])<<8 | int(data[pos+1])
	pos += 2

	if pos+l > len(data) {
		return "", 0, fmt.Errorf("short read for string of length %d at position %d", l, pos)
	}

	return string(data[pos : pos+l]), pos + l, nil
}

// --- In-Memory Cache Store ---

// InMemoryCacheStore provides a simple in-memory LRU cache for the gateway.
// It is used as the default cache backend when no external store is configured.
type InMemoryCacheStore struct {
	mu       sync.RWMutex
	items    map[string]*cacheItem
	maxSize  int
	evictOrder []string // simple FIFO eviction
}

type cacheItem struct {
	data      []byte
	expiresAt time.Time
}

// NewInMemoryCacheStore creates a new in-memory cache store.
func NewInMemoryCacheStore(maxSize int) *InMemoryCacheStore {
	if maxSize <= 0 {
		maxSize = 1000
	}

	return &InMemoryCacheStore{
		items:      make(map[string]*cacheItem, maxSize),
		maxSize:    maxSize,
		evictOrder: make([]string, 0, maxSize),
	}
}

func (s *InMemoryCacheStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, ok := s.items[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}

	if time.Now().After(item.expiresAt) {
		return nil, fmt.Errorf("expired")
	}

	return item.data, nil
}

func (s *InMemoryCacheStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict if at capacity
	if _, exists := s.items[key]; !exists && len(s.items) >= s.maxSize {
		s.evictOldest()
	}

	s.items[key] = &cacheItem{
		data:      value,
		expiresAt: time.Now().Add(ttl),
	}

	// Track eviction order
	s.evictOrder = append(s.evictOrder, key)

	return nil
}

func (s *InMemoryCacheStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.items, key)

	return nil
}

func (s *InMemoryCacheStore) evictOldest() {
	for len(s.evictOrder) > 0 && len(s.items) >= s.maxSize {
		oldest := s.evictOrder[0]
		s.evictOrder = s.evictOrder[1:]
		delete(s.items, oldest)
	}
}

// CleanExpired removes all expired entries.
func (s *InMemoryCacheStore) CleanExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, item := range s.items {
		if now.After(item.expiresAt) {
			delete(s.items, key)
		}
	}
}
