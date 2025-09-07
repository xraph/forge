package cache

import (
	cachecore "github.com/xraph/forge/pkg/cache/core"
)

// CacheConfig contains the main cache configuration
type CacheConfig = cachecore.CacheConfig

// RedisConfig contains Redis cache configuration
type RedisConfig = cachecore.RedisConfig

// MemoryConfig contains memory cache configuration
type MemoryConfig = cachecore.MemoryConfig

// HybridConfig contains hybrid cache configuration
type HybridConfig = cachecore.HybridConfig

// DatabaseConfig contains database cache configuration
type DatabaseConfig = cachecore.DatabaseConfig

// SerializationConfig contains serialization configuration
type SerializationConfig = cachecore.SerializationConfig

// SerializerConfig contains serializer configuration
type SerializerConfig = cachecore.SerializerConfig

// CompressionConfig contains compression configuration
type CompressionConfig = cachecore.CompressionConfig

// EncryptionConfig contains encryption configuration
type EncryptionConfig = cachecore.EncryptionConfig

// InvalidationConfig contains invalidation configuration
type InvalidationConfig = cachecore.InvalidationConfig

// InvalidationBrokerConfig contains invalidation broker configuration
type InvalidationBrokerConfig = cachecore.InvalidationBrokerConfig

// InvalidationPattern contains invalidation pattern configuration
type InvalidationPattern = cachecore.InvalidationPattern

// WarmingConfig contains cache warming configuration
type WarmingConfig = cachecore.WarmingConfig

type WarmConfig = cachecore.WarmConfig

// WarmingDataSource contains warming data source configuration
type WarmingDataSource = cachecore.WarmingDataSource

// MonitoringConfig contains monitoring configuration
type MonitoringConfig = cachecore.MonitoringConfig

// AlertThresholds contains alert threshold configuration
type AlertThresholds = cachecore.AlertThresholds

// CircuitBreakerConfig contains circuit breaker configuration
type CircuitBreakerConfig = cachecore.CircuitBreakerConfig

// RateLimitConfig contains rate limiting configuration
type RateLimitConfig = cachecore.RateLimitConfig

// DefaultCacheConfig Default configurations
var DefaultCacheConfig = cachecore.DefaultCacheConfig

// GetRedisConfig extracts Redis configuration from backend config
func GetRedisConfig(config map[string]interface{}) *RedisConfig {
	return cachecore.GetRedisConfig(config)
}

// GetMemoryConfig extracts memory configuration from backend config
func GetMemoryConfig(config map[string]interface{}) *MemoryConfig {
	return cachecore.GetMemoryConfig(config)
}

// NormalizeKey normalizes a cache key
func NormalizeKey(key string, prefix string) string {
	return cachecore.NormalizeKey(key, prefix)
}

// SplitKey splits a cache key into prefix and key
func SplitKey(key string) (string, string) {
	return cachecore.SplitKey(key)
}

// ValidateKeyLength validates key length
func ValidateKeyLength(key string, maxLength int) error {
	return cachecore.ValidateKeyLength(key, maxLength)
}

// ValidateValueSize validates value size
func ValidateValueSize(value interface{}, maxSize int64) error {
	return cachecore.ValidateValueSize(value, maxSize)
}

// CacheServiceConfig holds cache service configuration
type CacheServiceConfig struct {
	Enabled       bool
	AutoRegister  bool
	ServiceConfig *CacheConfig
}
