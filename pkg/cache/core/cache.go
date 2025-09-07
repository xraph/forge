package core

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// CacheManager defines the interface for cache management operations
type CacheManager interface {
	// Service lifecycle methods
	Name() string
	Dependencies() []string
	OnStart(ctx context.Context) error
	OnStop(ctx context.Context) error
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
