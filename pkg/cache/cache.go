package cache

import (
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
)

// Cache defines the core cache interface
type Cache = cachecore.Cache

// CacheItem represents a cache entry with metadata
type CacheItem = cachecore.CacheItem

// CacheStats provides cache statistics
type CacheStats = cachecore.CacheStats

// CacheCallback defines callback functions for cache events
type CacheCallback = cachecore.CacheCallback

// CacheOptions defines options for cache operations
type CacheOptions = cachecore.CacheOptions

// EvictionPolicy defines cache eviction policies
type EvictionPolicy = cachecore.EvictionPolicy

type CacheManager = cachecore.CacheManager

const (
	EvictionPolicyLRU    = cachecore.EvictionPolicyLRU
	EvictionPolicyLFU    = cachecore.EvictionPolicyLFU
	EvictionPolicyFIFO   = cachecore.EvictionPolicyFIFO
	EvictionPolicyRandom = cachecore.EvictionPolicyRandom
	EvictionPolicyTTL    = cachecore.EvictionPolicyRandom
	EvictionPolicyNone   = cachecore.EvictionPolicyNone
)

// ConsistencyLevel defines consistency levels for distributed caches
type ConsistencyLevel = cachecore.ConsistencyLevel

const (
	ConsistencyLevelStrong   = cachecore.ConsistencyLevelStrong
	ConsistencyLevelEventual = cachecore.ConsistencyLevelEventual
	ConsistencyLevelWeakly   = cachecore.ConsistencyLevelWeakly
)

// CacheType defines the type of cache backend
type CacheType = cachecore.CacheType

const (
	CacheTypeRedis    = cachecore.CacheTypeRedis
	CacheTypeMemory   = cachecore.CacheTypeMemory
	CacheTypeHybrid   = cachecore.CacheTypeHybrid
	CacheTypeDatabase = cachecore.CacheTypeDatabase
)

// CacheBackend defines the cache backend interface
type CacheBackend = cachecore.CacheBackend

// CacheFactory creates cache instances
type CacheFactory = cachecore.CacheFactory

// DistributedCache extends Cache with distributed capabilities
type DistributedCache = cachecore.DistributedCache

// Lock represents a distributed lock
type Lock = cachecore.Lock

// CacheObserver defines cache event observer interface
type CacheObserver = cachecore.CacheObserver

// MultiTierCache defines multi-tier cache interface
type MultiTierCache = cachecore.MultiTierCache

// CacheError defines cache-specific errors
type CacheError = cachecore.CacheError

// Common cache errors
var (
	ErrCacheNotFound     = cachecore.ErrCacheNotFound
	ErrCacheTimeout      = cachecore.ErrCacheTimeout
	ErrCacheUnavailable  = cachecore.ErrCacheUnavailable
	ErrCacheInvalidKey   = cachecore.ErrCacheInvalidKey
	ErrCacheInvalidValue = cachecore.ErrCacheInvalidValue
	ErrCacheLocked       = cachecore.ErrCacheLocked
	ErrCacheCorrupted    = cachecore.ErrCacheCorrupted
)

// NewCacheError Helper functions
func NewCacheError(code, message, key, operation string, cause error) *CacheError {
	return cachecore.NewCacheError(code, message, key, operation, cause)
}

func IsNotFound(err error) bool {
	return cachecore.IsNotFound(err)
}

func IsTimeout(err error) bool {
	return cachecore.IsTimeout(err)
}

func IsUnavailable(err error) bool {
	return cachecore.IsUnavailable(err)
}

func CalculateHitRatio(hits, misses int64) float64 {
	return cachecore.CalculateHitRatio(hits, misses)
}

func ValidateKey(key string) error {
	return cachecore.ValidateKey(key)
}

func ValidateTTL(ttl time.Duration) error {
	return cachecore.ValidateTTL(ttl)
}
