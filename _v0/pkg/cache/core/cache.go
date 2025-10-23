package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// CacheManager defines the interface for cache management operations
type CacheManager interface {
	// Service lifecycle methods
	Name() string
	Dependencies() []string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	OnHealthCheck(ctx context.Context) error
	IsStarted() bool

	// Cache management methods
	RegisterCache(name string, backend CacheBackend) error
	UnregisterCache(name string) error
	GetCache(name string) (Cache, error)
	GetDefaultCache() (Cache, error)
	ListCaches() []Cache
	SetDefaultCache(name string) error

	// Observer methods
	AddObserver(observer CacheObserver)
	RemoveObserver(observer CacheObserver)

	// Statistics methods
	GetStats() map[string]CacheStats
	GetCombinedStats() CacheStats

	// Invalidation methods
	InvalidatePattern(ctx context.Context, pattern string) error
	InvalidateByTags(ctx context.Context, tags []string) error

	// Cache warming methods
	WarmCache(ctx context.Context, cacheName string, config WarmConfig) error

	// Component getters
	GetInvalidationManager() InvalidationManager
	GetCacheWarmer() CacheWarmer
	GetFactory() CacheFactory

	// Configuration methods
	GetConfig() *CacheConfig
	UpdateConfig(config *CacheConfig) error
}

// Cache defines the core cache interface
type Cache interface {
	// Initialize the cache
	Stop(ctx context.Context) error

	// Name returns the name as a string representation.
	Name() string

	// Get retrieves the value associated with the given key from the data store within the provided context.
	Get(ctx context.Context, key string) (interface{}, error)

	// Set stores a value associated with a key in the storage with a specified TTL (time-to-live) duration.
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Delete removes the specified key from the datastore. Returns an error if the operation fails.
	Delete(ctx context.Context, key string) error

	// Exists checks if the specified key exists in the cache and returns true if found, otherwise false.
	Exists(ctx context.Context, key string) (bool, error)

	// GetMulti retrieves multiple values from the cache using the provided keys and returns a map of key-value pairs.
	// If a key is not found, its value in the result map is set to nil. Returns an error if any retrieval operation fails.
	GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error)

	// SetMulti stores multiple key-value pairs in the cache where each item may have its own TTL and metadata.
	// Returns an error if any of the operations fail.
	SetMulti(ctx context.Context, items map[string]CacheItem) error

	// DeleteMulti removes multiple keys from the cache in a single operation.
	// Returns an error if any deletion fails or if the input is invalid.
	DeleteMulti(ctx context.Context, keys []string) error

	// Increment increases the numeric value associated with the given key by the specified delta and returns the updated value.
	Increment(ctx context.Context, key string, delta int64) (int64, error)

	// Decrement decreases the integer value stored at the specified key by the given delta and returns the new value or an error.
	Decrement(ctx context.Context, key string, delta int64) (int64, error)

	// Touch updates the expiration time (TTL) of an existing key in the cache without altering its value.
	Touch(ctx context.Context, key string, ttl time.Duration) error

	// TTL retrieves the time-to-live (TTL) of the specified key in the cache. If the key is not found, it returns an error.
	TTL(ctx context.Context, key string) (time.Duration, error)

	// Keys retrieves all keys that match the given pattern from the cache, potentially across all cache tiers.
	Keys(ctx context.Context, pattern string) ([]string, error)

	// DeletePattern removes all keys matching a specified pattern from the cache. Returns an error if the operation fails.
	DeletePattern(ctx context.Context, pattern string) error

	// Flush clears all data from the cache, ensuring no cached entries remain. Returns an error if the operation fails.
	Flush(ctx context.Context) error

	// Size returns the size of a resource in bytes and an error, if any occurs, using the provided context for cancellation.
	Size(ctx context.Context) (int64, error)

	// Close releases any resources associated with the object and performs necessary cleanup. Returns an error if closing fails.
	Close() error

	// Stats retrieves the current statistics of the cache, including hits, misses, size, memory usage, and uptime.
	Stats() CacheStats

	// HealthCheck checks the health of the cache system and returns an error if any issue is detected.
	HealthCheck(ctx context.Context) error
}

// CacheItem represents a cache entry with metadata
type CacheItem struct {
	Key       string
	Value     interface{}
	TTL       time.Duration
	Tags      []string
	Metadata  map[string]interface{}
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CacheStats provides cache statistics
type CacheStats struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Hits        int64                  `json:"hits"`
	Misses      int64                  `json:"misses"`
	Sets        int64                  `json:"sets"`
	Deletes     int64                  `json:"deletes"`
	Errors      int64                  `json:"errors"`
	Size        int64                  `json:"size"`
	Memory      int64                  `json:"memory"`
	Connections int                    `json:"connections"`
	HitRatio    float64                `json:"hit_ratio"`
	LastAccess  time.Time              `json:"last_access"`
	Uptime      time.Duration          `json:"uptime"`
	Shards      map[string]int64       `json:"shards,omitempty"`
	Custom      map[string]interface{} `json:"custom,omitempty"`

	TotalRequests  int64         `json:"total_requests"`
	HitRequests    int64         `json:"hit_requests"`
	MissRequests   int64         `json:"miss_requests"`
	SetRequests    int64         `json:"set_requests"`
	DeleteRequests int64         `json:"delete_requests"`
	ClearRequests  int64         `json:"clear_requests"`
	ErrorRequests  int64         `json:"error_requests"`
	TotalSize      int64         `json:"total_size"`
	LastMiss       time.Time     `json:"last_miss"`
	HitRate        float64       `json:"hit_rate"`
	AverageLatency time.Duration `json:"average_latency"`
}

// CacheCallback defines callback functions for cache events
type CacheCallback func(ctx context.Context, key string, value interface{})

// CacheOptions defines options for cache operations
type CacheOptions struct {
	TTL               time.Duration
	Tags              []string
	Metadata          map[string]interface{}
	OnHit             CacheCallback
	OnMiss            CacheCallback
	OnSet             CacheCallback
	OnDelete          CacheCallback
	OnError           CacheCallback
	SerializerType    string
	CompressionType   string
	SkipSerialization bool
}

// EvictionPolicy defines cache eviction policies
type EvictionPolicy string

const (
	EvictionPolicyLRU    EvictionPolicy = "lru"
	EvictionPolicyLFU    EvictionPolicy = "lfu"
	EvictionPolicyFIFO   EvictionPolicy = "fifo"
	EvictionPolicyRandom EvictionPolicy = "random"
	EvictionPolicyTTL    EvictionPolicy = "ttl"
	EvictionPolicyNone   EvictionPolicy = "none"
)

// ConsistencyLevel defines consistency levels for distributed caches
type ConsistencyLevel string

const (
	ConsistencyLevelStrong   ConsistencyLevel = "strong"
	ConsistencyLevelEventual ConsistencyLevel = "eventual"
	ConsistencyLevelWeakly   ConsistencyLevel = "weak"
)

// CacheType defines the type of cache backend
type CacheType string

const (
	CacheTypeRedis    CacheType = "redis"
	CacheTypeMemory   CacheType = "memory"
	CacheTypeHybrid   CacheType = "hybrid"
	CacheTypeDatabase CacheType = "database"
)

// CacheBackend defines the cache backend interface
type CacheBackend interface {
	Cache
	Type() CacheType
	Name() string
	Configure(config interface{}) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type CacheConstructor = func(name string, config interface{}, l common.Logger, metrics common.Metrics) (CacheBackend, error)

// CacheFactory creates cache instances
type CacheFactory interface {
	Create(cacheType CacheType, name string, config interface{}, l common.Logger, metrics common.Metrics) (CacheBackend, error)
	List() []CacheType
	Register(cacheType CacheType, factory CacheConstructor)
}

// DistributedCache extends Cache with distributed capabilities
type DistributedCache interface {
	Cache

	// Distributed operations
	Lock(ctx context.Context, key string, ttl time.Duration) (Lock, error)
	Unlock(ctx context.Context, key string) error

	// Cluster operations
	GetNode(key string) (string, error)
	Rebalance(ctx context.Context) error

	// Replication
	Replicate(ctx context.Context, key string, value interface{}) error

	// Consistency
	ConsistencyLevel() ConsistencyLevel
	WaitForConsistency(ctx context.Context, key string, timeout time.Duration) error
}

// Lock represents a distributed lock
type Lock interface {
	Key() string
	TTL() time.Duration
	Owner() string
	Extend(ctx context.Context, ttl time.Duration) error
	Release(ctx context.Context) error
}

// CacheObserver defines cache event observer interface
type CacheObserver interface {
	OnCacheHit(ctx context.Context, key string, value interface{})
	OnCacheMiss(ctx context.Context, key string)
	OnCacheSet(ctx context.Context, key string, value interface{})
	OnCacheDelete(ctx context.Context, key string)
	OnCacheError(ctx context.Context, key string, err error)
}

// MultiTierCache defines multi-tier cache interface
type MultiTierCache interface {
	Cache

	// Tier operations
	GetTier(level int) (Cache, error)
	SetTier(level int, cache Cache) error
	PromoteToTier(ctx context.Context, key string, targetTier int) error
	DemoteFromTier(ctx context.Context, key string, sourceTier int) error

	// Tier management
	TierCount() int
	TierStats(level int) CacheStats
	OptimizeTiers(ctx context.Context) error
}

// CacheError defines cache-specific errors
type CacheError struct {
	Code      string
	Message   string
	Key       string
	Operation string
	Cause     error
}

func (e *CacheError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s (key: %s, op: %s): %v", e.Code, e.Message, e.Key, e.Operation, e.Cause)
	}
	return fmt.Sprintf("[%s] %s (key: %s, op: %s)", e.Code, e.Message, e.Key, e.Operation)
}

// Common cache errors
var (
	ErrCacheNotFound     = &CacheError{Code: "NOT_FOUND", Message: "key not found"}
	ErrCacheTimeout      = &CacheError{Code: "TIMEOUT", Message: "operation timed out"}
	ErrCacheUnavailable  = &CacheError{Code: "UNAVAILABLE", Message: "cache service unavailable"}
	ErrCacheInvalidKey   = &CacheError{Code: "INVALID_KEY", Message: "invalid cache key"}
	ErrCacheInvalidValue = &CacheError{Code: "INVALID_VALUE", Message: "invalid cache value"}
	ErrCacheLocked       = &CacheError{Code: "LOCKED", Message: "cache key is locked"}
	ErrCacheCorrupted    = &CacheError{Code: "CORRUPTED", Message: "cache data corrupted"}
)

// Helper functions
func NewCacheError(code, message, key, operation string, cause error) *CacheError {
	return &CacheError{
		Code:      code,
		Message:   message,
		Key:       key,
		Operation: operation,
		Cause:     cause,
	}
}

func IsNotFound(err error) bool {
	if cacheErr, ok := err.(*CacheError); ok {
		return cacheErr.Code == "NOT_FOUND"
	}
	return false
}

func IsTimeout(err error) bool {
	if cacheErr, ok := err.(*CacheError); ok {
		return cacheErr.Code == "TIMEOUT"
	}
	return false
}

func IsUnavailable(err error) bool {
	if cacheErr, ok := err.(*CacheError); ok {
		return cacheErr.Code == "UNAVAILABLE"
	}
	return false
}

func CalculateHitRatio(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

func ValidateKey(key string) error {
	if key == "" {
		return ErrCacheInvalidKey
	}
	if len(key) > 250 {
		return NewCacheError("INVALID_KEY", "key too long", key, "validate", nil)
	}
	return nil
}

func ValidateTTL(ttl time.Duration) error {
	if ttl < 0 {
		return NewCacheError("INVALID_TTL", "TTL cannot be negative", "", "validate", nil)
	}
	if ttl > 365*24*time.Hour {
		return NewCacheError("INVALID_TTL", "TTL too long", "", "validate", nil)
	}
	return nil
}

// DefaultCacheManager is a concrete implementation of CacheManager
type DefaultCacheManager struct {
	config              *CacheConfig
	logger              common.Logger
	metrics             common.Metrics
	caches              map[string]CacheBackend
	defaultCache        string
	started             bool
	mu                  sync.RWMutex
	observers           []CacheObserver
	invalidationManager InvalidationManager
	cacheWarmer         CacheWarmer
	factory             CacheFactory
}

// NewDefaultCacheManager creates a new default cache manager
func NewDefaultCacheManager(config *CacheConfig, logger common.Logger, metrics common.Metrics) *DefaultCacheManager {
	return &DefaultCacheManager{
		config:    config,
		logger:    logger,
		metrics:   metrics,
		caches:    make(map[string]CacheBackend),
		observers: make([]CacheObserver, 0),
	}
}

// Name returns the service name
func (m *DefaultCacheManager) Name() string {
	return "cache-manager"
}

// Dependencies returns the service dependencies
func (m *DefaultCacheManager) Dependencies() []string {
	return []string{}
}

// OnStart starts the cache manager
func (m *DefaultCacheManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	// Initialize components
	m.invalidationManager = NewDefaultInvalidationManager(m.logger, m.metrics)
	m.cacheWarmer = NewDefaultCacheWarmer(m.logger, m.metrics)
	m.factory = NewDefaultCacheFactory()

	// Start invalidation manager
	if err := m.invalidationManager.Start(ctx); err != nil {
		return err
	}

	// Start cache warmer
	if err := m.cacheWarmer.Start(ctx); err != nil {
		return err
	}

	m.started = true
	return nil
}

// OnStop stops the cache manager
func (m *DefaultCacheManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	// Stop all caches
	for name, cache := range m.caches {
		if err := cache.Stop(ctx); err != nil {
			m.logger.Error("failed to stop cache", logger.String("cache", name), logger.String("error", err.Error()))
		}
	}

	// Stop invalidation manager
	if m.invalidationManager != nil {
		if err := m.invalidationManager.Stop(ctx); err != nil {
			m.logger.Error("failed to stop invalidation manager", logger.String("error", err.Error()))
		}
	}

	// Stop cache warmer
	if m.cacheWarmer != nil {
		if err := m.cacheWarmer.Stop(ctx); err != nil {
			m.logger.Error("failed to stop cache warmer", logger.String("error", err.Error()))
		}
	}

	m.started = false
	return nil
}

// OnHealthCheck performs a health check
func (m *DefaultCacheManager) OnHealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return fmt.Errorf("cache manager not started")
	}

	// Check if we have at least one cache
	if len(m.caches) == 0 {
		return fmt.Errorf("no caches available")
	}

	return nil
}

// IsStarted returns whether the manager is started
func (m *DefaultCacheManager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// RegisterCache registers a cache backend
func (m *DefaultCacheManager) RegisterCache(name string, backend CacheBackend) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot register cache after manager is started")
	}

	m.caches[name] = backend
	return nil
}

// UnregisterCache unregisters a cache backend
func (m *DefaultCacheManager) UnregisterCache(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot unregister cache after manager is started")
	}

	delete(m.caches, name)
	return nil
}

// GetCache returns a cache by name
func (m *DefaultCacheManager) GetCache(name string) (Cache, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cache, exists := m.caches[name]
	if !exists {
		return nil, fmt.Errorf("cache %s not found", name)
	}

	return cache, nil
}

// GetDefaultCache returns the default cache
func (m *DefaultCacheManager) GetDefaultCache() (Cache, error) {
	if m.defaultCache == "" {
		return nil, fmt.Errorf("no default cache set")
	}

	return m.GetCache(m.defaultCache)
}

// ListCaches returns all registered caches
func (m *DefaultCacheManager) ListCaches() []Cache {
	m.mu.RLock()
	defer m.mu.RUnlock()

	caches := make([]Cache, 0, len(m.caches))
	for _, cache := range m.caches {
		caches = append(caches, cache)
	}

	return caches
}

// SetDefaultCache sets the default cache
func (m *DefaultCacheManager) SetDefaultCache(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.caches[name]; !exists {
		return fmt.Errorf("cache %s not found", name)
	}

	m.defaultCache = name
	return nil
}

// AddObserver adds a cache observer
func (m *DefaultCacheManager) AddObserver(observer CacheObserver) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.observers = append(m.observers, observer)
}

// RemoveObserver removes a cache observer
func (m *DefaultCacheManager) RemoveObserver(observer CacheObserver) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, obs := range m.observers {
		if obs == observer {
			m.observers = append(m.observers[:i], m.observers[i+1:]...)
			break
		}
	}
}

// GetStats returns statistics for all caches
func (m *DefaultCacheManager) GetStats() map[string]CacheStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]CacheStats)
	for name, cache := range m.caches {
		stats[name] = CacheStats{
			Name:   cache.Name(),
			Type:   string(cache.Type()),
			Hits:   0, // Placeholder - would need to track this
			Misses: 0, // Placeholder - would need to track this
		}
	}

	return stats
}

// GetCombinedStats returns combined statistics
func (m *DefaultCacheManager) GetCombinedStats() CacheStats {
	stats := m.GetStats()

	var combined CacheStats
	for _, stat := range stats {
		combined.Hits += stat.Hits
		combined.Misses += stat.Misses
		combined.Sets += stat.Sets
		combined.Deletes += stat.Deletes
		combined.Errors += stat.Errors
		combined.Size += stat.Size
		combined.Memory += stat.Memory
		combined.Connections += stat.Connections
	}

	// Calculate hit ratio
	totalRequests := combined.Hits + combined.Misses
	if totalRequests > 0 {
		combined.HitRatio = float64(combined.Hits) / float64(totalRequests)
	}

	return combined
}

// InvalidatePattern invalidates caches by pattern
func (m *DefaultCacheManager) InvalidatePattern(ctx context.Context, pattern string) error {
	if m.invalidationManager == nil {
		return fmt.Errorf("invalidation manager not initialized")
	}

	// Invalidate all caches with the pattern
	for name := range m.caches {
		if err := m.invalidationManager.InvalidatePattern(ctx, name, pattern); err != nil {
			return err
		}
	}
	return nil
}

// InvalidateByTags invalidates caches by tags
func (m *DefaultCacheManager) InvalidateByTags(ctx context.Context, tags []string) error {
	if m.invalidationManager == nil {
		return fmt.Errorf("invalidation manager not initialized")
	}

	// Invalidate all caches with the tags
	for name := range m.caches {
		if err := m.invalidationManager.InvalidateByTags(ctx, name, tags); err != nil {
			return err
		}
	}
	return nil
}

// WarmCache warms a cache
func (m *DefaultCacheManager) WarmCache(ctx context.Context, cacheName string, config WarmConfig) error {
	if m.cacheWarmer == nil {
		return fmt.Errorf("cache warmer not initialized")
	}

	return m.cacheWarmer.WarmCache(ctx, cacheName, config)
}

// GetInvalidationManager returns the invalidation manager
func (m *DefaultCacheManager) GetInvalidationManager() InvalidationManager {
	return m.invalidationManager
}

// GetCacheWarmer returns the cache warmer
func (m *DefaultCacheManager) GetCacheWarmer() CacheWarmer {
	return m.cacheWarmer
}

// GetFactory returns the cache factory
func (m *DefaultCacheManager) GetFactory() CacheFactory {
	return m.factory
}

// GetConfig returns the cache configuration
func (m *DefaultCacheManager) GetConfig() *CacheConfig {
	return m.config
}

// UpdateConfig updates the cache configuration
func (m *DefaultCacheManager) UpdateConfig(config *CacheConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot update config after manager is started")
	}

	m.config = config
	return nil
}

// Placeholder implementations for missing components

// DefaultInvalidationManager is a placeholder implementation
type DefaultInvalidationManager struct {
	logger  common.Logger
	metrics common.Metrics
}

func NewDefaultInvalidationManager(logger common.Logger, metrics common.Metrics) *DefaultInvalidationManager {
	return &DefaultInvalidationManager{logger: logger, metrics: metrics}
}

func (m *DefaultInvalidationManager) Start(ctx context.Context) error       { return nil }
func (m *DefaultInvalidationManager) Stop(ctx context.Context) error        { return nil }
func (m *DefaultInvalidationManager) HealthCheck(ctx context.Context) error { return nil }
func (m *DefaultInvalidationManager) RegisterPublisher(name string, publisher InvalidationPublisher) error {
	return nil
}
func (m *DefaultInvalidationManager) RegisterSubscriber(name string, subscriber InvalidationSubscriber) error {
	return nil
}
func (m *DefaultInvalidationManager) RegisterCallback(callback InvalidationCallback) {}
func (m *DefaultInvalidationManager) AddPattern(pattern InvalidationPattern)         {}
func (m *DefaultInvalidationManager) InvalidateKey(ctx context.Context, cacheName, key string) error {
	return nil
}
func (m *DefaultInvalidationManager) InvalidatePattern(ctx context.Context, cacheName, pattern string) error {
	return nil
}
func (m *DefaultInvalidationManager) InvalidateTag(ctx context.Context, cacheName, tag string) error {
	return nil
}
func (m *DefaultInvalidationManager) InvalidateByTags(ctx context.Context, cacheName string, tags []string) error {
	return nil
}
func (m *DefaultInvalidationManager) InvalidateAll(ctx context.Context, cacheName string) error {
	return nil
}
func (m *DefaultInvalidationManager) GetStats() InvalidationStats { return InvalidationStats{} }

// DefaultCacheWarmer is a placeholder implementation
type DefaultCacheWarmer struct {
	logger  common.Logger
	metrics common.Metrics
}

func NewDefaultCacheWarmer(logger common.Logger, metrics common.Metrics) *DefaultCacheWarmer {
	return &DefaultCacheWarmer{logger: logger, metrics: metrics}
}

func (w *DefaultCacheWarmer) Start(ctx context.Context) error { return nil }
func (w *DefaultCacheWarmer) Stop(ctx context.Context) error  { return nil }
func (w *DefaultCacheWarmer) WarmCache(ctx context.Context, cacheName string, config WarmConfig) error {
	return nil
}
func (w *DefaultCacheWarmer) GetWarmStats(cacheName string) (WarmStats, error) {
	return WarmStats{}, nil
}
func (w *DefaultCacheWarmer) ScheduleWarming(cacheName string, config WarmConfig) error { return nil }
func (w *DefaultCacheWarmer) CancelWarming(cacheName string) error                      { return nil }
func (w *DefaultCacheWarmer) GetStats() WarmStats                                       { return WarmStats{} }
func (w *DefaultCacheWarmer) GetActiveWarmings() []WarmingOperation                     { return []WarmingOperation{} }
func (w *DefaultCacheWarmer) GetDataSources() []DataSource                              { return []DataSource{} }
func (w *DefaultCacheWarmer) RegisterDataSource(source DataSource) error                { return nil }
func (w *DefaultCacheWarmer) UnregisterDataSource(name string) error                    { return nil }

// Placeholder types are defined in warmer.go

// DefaultCacheFactory is a placeholder implementation
type DefaultCacheFactory struct{}

func NewDefaultCacheFactory() *DefaultCacheFactory {
	return &DefaultCacheFactory{}
}

func (f *DefaultCacheFactory) Create(cacheType CacheType, name string, config interface{}, l common.Logger, metrics common.Metrics) (CacheBackend, error) {
	return nil, fmt.Errorf("cache factory not implemented")
}

func (f *DefaultCacheFactory) List() []CacheType {
	return []CacheType{}
}

func (f *DefaultCacheFactory) Register(cacheType CacheType, factory CacheConstructor) {
	// Placeholder implementation
}
