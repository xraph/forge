package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// RedisCache implements the Cache interface using Redis
type RedisCache struct {
	name      string
	client    redis.UniversalClient
	config    *cachecore.RedisConfig
	logger    common.Logger
	metrics   common.Metrics
	stats     *RedisCacheStats
	mu        sync.RWMutex
	closed    bool
	startTime time.Time
}

// RedisCacheStats tracks Redis cache statistics
type RedisCacheStats struct {
	hits        int64
	misses      int64
	sets        int64
	deletes     int64
	errors      int64
	connections int
	mu          sync.RWMutex
}

// NewRedisCache creates a new Redis cache instance
func NewRedisCache(name string, config *cachecore.RedisConfig, logger common.Logger, metrics common.Metrics) (*RedisCache, error) {
	if config == nil {
		return nil, fmt.Errorf("redis config cannot be nil")
	}

	cache := &RedisCache{
		name:      name,
		config:    config,
		logger:    logger,
		metrics:   metrics,
		stats:     &RedisCacheStats{},
		startTime: time.Now(),
	}

	if err := cache.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return cache, nil
}

// connect establishes connection to Redis
func (r *RedisCache) connect() error {
	var client redis.UniversalClient

	if r.config.EnableCluster {
		// Redis Cluster
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        r.config.ClusterNodes,
			Password:     r.config.Password,
			PoolSize:     r.config.PoolSize,
			MinIdleConns: r.config.MinIdleConns,
			MaxIdleConns: r.config.MaxIdleConns,
			DialTimeout:  r.config.DialTimeout,
			ReadTimeout:  r.config.ReadTimeout,
			WriteTimeout: r.config.WriteTimeout,
		})
	} else if r.config.EnableSentinel {
		// Redis Sentinel
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    r.config.MasterName,
			SentinelAddrs: r.config.SentinelAddrs,
			Password:      r.config.Password,
			DB:            r.config.Database,
			PoolSize:      r.config.PoolSize,
			MinIdleConns:  r.config.MinIdleConns,
			MaxIdleConns:  r.config.MaxIdleConns,
			DialTimeout:   r.config.DialTimeout,
			ReadTimeout:   r.config.ReadTimeout,
			WriteTimeout:  r.config.WriteTimeout,
		})
	} else {
		// Single Redis instance
		addr := "localhost:6379"
		if len(r.config.Addresses) > 0 {
			addr = r.config.Addresses[0]
		}

		client = redis.NewClient(&redis.Options{
			Addr:         addr,
			Password:     r.config.Password,
			DB:           r.config.Database,
			PoolSize:     r.config.PoolSize,
			MinIdleConns: r.config.MinIdleConns,
			MaxIdleConns: r.config.MaxIdleConns,
			DialTimeout:  r.config.DialTimeout,
			ReadTimeout:  r.config.ReadTimeout,
			WriteTimeout: r.config.WriteTimeout,
		})
	}

	r.client = client

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), r.config.DialTimeout)
	defer cancel()

	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to ping Redis: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("Redis cache connected",
			logger.String("name", r.name),
			logger.String("addr", r.getAddress()),
			logger.Bool("cluster", r.config.EnableCluster),
			logger.Bool("sentinel", r.config.EnableSentinel),
		)
	}

	return nil
}

// Type returns the cache type
func (r *RedisCache) Type() cachecore.CacheType {
	return cachecore.CacheTypeRedis
}

// Name returns the cache name
func (r *RedisCache) Name() string {
	return r.name
}

// Configure configures the cache
func (r *RedisCache) Configure(config interface{}) error {
	redisConfig, ok := config.(*cachecore.RedisConfig)
	if !ok {
		return fmt.Errorf("invalid config type for Redis cache")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.config = redisConfig
	return r.connect()
}

// Start starts the cache
func (r *RedisCache) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("cache is closed")
	}

	if r.client == nil {
		if err := r.connect(); err != nil {
			return err
		}
	}

	if r.logger != nil {
		r.logger.Info("Redis cache started", logger.String("name", r.name))
	}

	return nil
}

// Stop stops the cache
func (r *RedisCache) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	if r.client != nil {
		if err := r.client.Close(); err != nil {
			if r.logger != nil {
				r.logger.Error("failed to close Redis client", logger.Error(err))
			}
		}
		r.client = nil
	}

	r.closed = true

	if r.logger != nil {
		r.logger.Info("Redis cache stopped", logger.String("name", r.name))
	}

	return nil
}

// Get retrieves a value from the cache
func (r *RedisCache) Get(ctx context.Context, key string) (interface{}, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		r.recordError()
		return nil, err
	}

	result, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			r.recordMiss()
			return nil, cachecore.ErrCacheNotFound
		}
		r.recordError()
		return nil, cachecore.NewCacheError("GET_FAILED", "failed to get value", key, "get", err)
	}

	r.recordHit()

	// Deserialize the value
	var value interface{}
	if err := json.Unmarshal([]byte(result), &value); err != nil {
		r.recordError()
		return nil, cachecore.NewCacheError("DESERIALIZE_FAILED", "failed to deserialize value", key, "get", err)
	}

	return value, nil
}

// Set stores a value in the cache
func (r *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if err := cachecore.ValidateKey(key); err != nil {
		r.recordError()
		return err
	}

	if err := cachecore.ValidateTTL(ttl); err != nil {
		r.recordError()
		return err
	}

	// Serialize the value
	data, err := json.Marshal(value)
	if err != nil {
		r.recordError()
		return cachecore.NewCacheError("SERIALIZE_FAILED", "failed to serialize value", key, "set", err)
	}

	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		r.recordError()
		return cachecore.NewCacheError("SET_FAILED", "failed to set value", key, "set", err)
	}

	r.recordSet()
	return nil
}

// Delete removes a value from the cache
func (r *RedisCache) Delete(ctx context.Context, key string) error {
	if err := cachecore.ValidateKey(key); err != nil {
		r.recordError()
		return err
	}

	if err := r.client.Del(ctx, key).Err(); err != nil {
		r.recordError()
		return cachecore.NewCacheError("DELETE_FAILED", "failed to delete value", key, "delete", err)
	}

	r.recordDelete()
	return nil
}

// Exists checks if a key exists in the cache
func (r *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		r.recordError()
		return false, err
	}

	count, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		r.recordError()
		return false, cachecore.NewCacheError("EXISTS_FAILED", "failed to check existence", key, "exists", err)
	}

	return count > 0, nil
}

// GetMulti retrieves multiple values from the cache
func (r *RedisCache) GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error) {
	if len(keys) == 0 {
		return make(map[string]interface{}), nil
	}

	// Validate all keys
	for _, key := range keys {
		if err := cachecore.ValidateKey(key); err != nil {
			r.recordError()
			return nil, err
		}
	}

	results, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		r.recordError()
		return nil, cachecore.NewCacheError("MGET_FAILED", "failed to get multiple values", "", "mget", err)
	}

	values := make(map[string]interface{})
	for i, result := range results {
		if result != nil {
			var value interface{}
			if err := json.Unmarshal([]byte(result.(string)), &value); err != nil {
				r.recordError()
				continue
			}
			values[keys[i]] = value
			r.recordHit()
		} else {
			r.recordMiss()
		}
	}

	return values, nil
}

// SetMulti stores multiple values in the cache
func (r *RedisCache) SetMulti(ctx context.Context, items map[string]cachecore.CacheItem) error {
	if len(items) == 0 {
		return nil
	}

	pipe := r.client.Pipeline()

	for key, item := range items {
		if err := cachecore.ValidateKey(key); err != nil {
			r.recordError()
			return err
		}

		data, err := json.Marshal(item.Value)
		if err != nil {
			r.recordError()
			return cachecore.NewCacheError("SERIALIZE_FAILED", "failed to serialize value", key, "mset", err)
		}

		pipe.Set(ctx, key, data, item.TTL)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		r.recordError()
		return cachecore.NewCacheError("MSET_FAILED", "failed to set multiple values", "", "mset", err)
	}

	r.stats.mu.Lock()
	r.stats.sets += int64(len(items))
	r.stats.mu.Unlock()

	return nil
}

// DeleteMulti removes multiple values from the cache
func (r *RedisCache) DeleteMulti(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	// Validate all keys
	for _, key := range keys {
		if err := cachecore.ValidateKey(key); err != nil {
			r.recordError()
			return err
		}
	}

	if err := r.client.Del(ctx, keys...).Err(); err != nil {
		r.recordError()
		return cachecore.NewCacheError("MDEL_FAILED", "failed to delete multiple values", "", "mdel", err)
	}

	r.stats.mu.Lock()
	r.stats.deletes += int64(len(keys))
	r.stats.mu.Unlock()

	return nil
}

// Increment increments a numeric value
func (r *RedisCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		r.recordError()
		return 0, err
	}

	result, err := r.client.IncrBy(ctx, key, delta).Result()
	if err != nil {
		r.recordError()
		return 0, cachecore.NewCacheError("INCR_FAILED", "failed to increment value", key, "incr", err)
	}

	return result, nil
}

// Decrement decrements a numeric value
func (r *RedisCache) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	return r.Increment(ctx, key, -delta)
}

// Touch updates the TTL of a key
func (r *RedisCache) Touch(ctx context.Context, key string, ttl time.Duration) error {
	if err := cachecore.ValidateKey(key); err != nil {
		r.recordError()
		return err
	}

	if err := cachecore.ValidateTTL(ttl); err != nil {
		r.recordError()
		return err
	}

	if err := r.client.Expire(ctx, key, ttl).Err(); err != nil {
		r.recordError()
		return cachecore.NewCacheError("TOUCH_FAILED", "failed to touch key", key, "touch", err)
	}

	return nil
}

// TTL returns the time-to-live of a key
func (r *RedisCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		r.recordError()
		return 0, err
	}

	ttl, err := r.client.TTL(ctx, key).Result()
	if err != nil {
		r.recordError()
		return 0, cachecore.NewCacheError("TTL_FAILED", "failed to get TTL", key, "ttl", err)
	}

	return ttl, nil
}

// Keys returns keys matching a pattern
func (r *RedisCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		r.recordError()
		return nil, cachecore.NewCacheError("KEYS_FAILED", "failed to get keys", "", "keys", err)
	}

	return keys, nil
}

// DeletePattern deletes keys matching a pattern
func (r *RedisCache) DeletePattern(ctx context.Context, pattern string) error {
	keys, err := r.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return nil
	}

	return r.DeleteMulti(ctx, keys)
}

// Flush clears all keys in the cache
func (r *RedisCache) Flush(ctx context.Context) error {
	if err := r.client.FlushDB(ctx).Err(); err != nil {
		r.recordError()
		return cachecore.NewCacheError("FLUSH_FAILED", "failed to flush cache", "", "flush", err)
	}

	return nil
}

// Size returns the number of keys in the cache
func (r *RedisCache) Size(ctx context.Context) (int64, error) {
	size, err := r.client.DBSize(ctx).Result()
	if err != nil {
		r.recordError()
		return 0, cachecore.NewCacheError("SIZE_FAILED", "failed to get cache size", "", "size", err)
	}

	return size, nil
}

// Close closes the cache connection
func (r *RedisCache) Close() error {
	return r.Stop(context.Background())
}

// Stats returns cache statistics
func (r *RedisCache) Stats() cachecore.CacheStats {
	r.stats.mu.RLock()
	defer r.stats.mu.RUnlock()

	poolStats := r.client.PoolStats()

	return cachecore.CacheStats{
		Name:        r.name,
		Type:        string(cachecore.CacheTypeRedis),
		Hits:        r.stats.hits,
		Misses:      r.stats.misses,
		Sets:        r.stats.sets,
		Deletes:     r.stats.deletes,
		Errors:      r.stats.errors,
		Connections: int(poolStats.TotalConns),
		HitRatio:    cachecore.CalculateHitRatio(r.stats.hits, r.stats.misses),
		Uptime:      time.Since(r.startTime),
		Custom: map[string]interface{}{
			"idle_conns":  poolStats.IdleConns,
			"stale_conns": poolStats.StaleConns,
		},
	}
}

// HealthCheck performs a health check
func (r *RedisCache) HealthCheck(ctx context.Context) error {
	if r.closed {
		return fmt.Errorf("Redis cache is closed")
	}

	if r.client == nil {
		return fmt.Errorf("Redis client not initialized")
	}

	start := time.Now()
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis ping failed: %w", err)
	}

	latency := time.Since(start)
	if latency > 100*time.Millisecond {
		return fmt.Errorf("Redis latency too high: %v", latency)
	}

	return nil
}

// Pipeline operations for Redis-specific optimizations
func (r *RedisCache) Pipeline() redis.Pipeliner {
	return r.client.Pipeline()
}

// TxPipeline returns a transaction pipeline
func (r *RedisCache) TxPipeline() redis.Pipeliner {
	return r.client.TxPipeline()
}

// GetClient returns the underlying Redis client
func (r *RedisCache) GetClient() redis.UniversalClient {
	return r.client
}

// Helper methods for statistics
func (r *RedisCache) recordHit() {
	r.stats.mu.Lock()
	r.stats.hits++
	r.stats.mu.Unlock()

	if r.metrics != nil {
		r.metrics.Counter("cache.hits", "cache", r.name).Inc()
	}
}

func (r *RedisCache) recordMiss() {
	r.stats.mu.Lock()
	r.stats.misses++
	r.stats.mu.Unlock()

	if r.metrics != nil {
		r.metrics.Counter("cache.misses", "cache", r.name).Inc()
	}
}

func (r *RedisCache) recordSet() {
	r.stats.mu.Lock()
	r.stats.sets++
	r.stats.mu.Unlock()

	if r.metrics != nil {
		r.metrics.Counter("cache.sets", "cache", r.name).Inc()
	}
}

func (r *RedisCache) recordDelete() {
	r.stats.mu.Lock()
	r.stats.deletes++
	r.stats.mu.Unlock()

	if r.metrics != nil {
		r.metrics.Counter("cache.deletes", "cache", r.name).Inc()
	}
}

func (r *RedisCache) recordError() {
	r.stats.mu.Lock()
	r.stats.errors++
	r.stats.mu.Unlock()

	if r.metrics != nil {
		r.metrics.Counter("cache.errors", "cache", r.name).Inc()
	}
}

func (r *RedisCache) getAddress() string {
	if len(r.config.Addresses) > 0 {
		return strings.Join(r.config.Addresses, ",")
	}
	return "localhost:6379"
}
