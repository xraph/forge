package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	// "github.com/redis/go-redis/v9"
)

// redisCache implements Cache interface for Redis
type redisCache struct {
	*baseCacheDatabase
	client *redis.Client
	prefix string
}

// NewRedisCache creates a new Redis cache connection
func newRedisCache(config CacheConfig) (Cache, error) {
	cache := &redisCache{
		baseCacheDatabase: &baseCacheDatabase{
			config:    config,
			driver:    "redis",
			connected: false,
			stats:     make(map[string]interface{}),
		},
		prefix: config.Prefix,
	}

	if err := cache.Connect(context.Background()); err != nil {
		return nil, err
	}

	return cache, nil
}

// Connect establishes Redis connection
func (r *redisCache) Connect(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Build Redis options
	opts := &redis.Options{
		Addr:     r.buildAddr(),
		Password: r.config.Password,
		DB:       r.config.Database,
		Username: r.config.Username,
	}

	// Connection pool settings
	if r.config.PoolSize > 0 {
		opts.PoolSize = r.config.PoolSize
	} else {
		opts.PoolSize = 10 // default
	}

	if r.config.MinIdleConns > 0 {
		opts.MinIdleConns = r.config.MinIdleConns
	} else {
		opts.MinIdleConns = 3 // default
	}

	if r.config.MaxConnAge > 0 {
		opts.MaxConnAge = r.config.MaxConnAge
	} else {
		opts.MaxConnAge = 5 * time.Minute // default
	}

	if r.config.PoolTimeout > 0 {
		opts.PoolTimeout = r.config.PoolTimeout
	} else {
		opts.PoolTimeout = 30 * time.Second // default
	}

	if r.config.IdleTimeout > 0 {
		opts.IdleTimeout = r.config.IdleTimeout
	} else {
		opts.IdleTimeout = 5 * time.Minute // default
	}

	if r.config.IdleCheckFreq > 0 {
		opts.IdleCheckFrequency = r.config.IdleCheckFreq
	} else {
		opts.IdleCheckFrequency = 1 * time.Minute // default
	}

	// Retry settings
	if r.config.MaxRetries > 0 {
		opts.MaxRetries = r.config.MaxRetries
	} else {
		opts.MaxRetries = 3 // default
	}

	// Create Redis client
	r.client = redis.NewClient(opts)

	// Test connection
	if err := r.Ping(ctx); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	r.connected = true
	return nil
}

// buildAddr builds Redis address from host and port
func (r *redisCache) buildAddr() string {
	if r.config.URL != "" {
		return r.config.URL
	}

	host := r.config.Host
	if host == "" {
		host = "localhost"
	}

	port := r.config.Port
	if port == 0 {
		port = 6379
	}

	return fmt.Sprintf("%s:%d", host, port)
}

// Close closes Redis connection
func (r *redisCache) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.client != nil {
		err := r.client.Close()
		r.connected = false
		return err
	}

	return nil
}

// Ping tests Redis connection
func (r *redisCache) Ping(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("Redis not connected")
	}

	return r.client.Ping(ctx).Err()
}

// IsConnected returns connection status
func (r *redisCache) IsConnected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.connected
}

// Stats returns Redis statistics
func (r *redisCache) Stats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range r.stats {
		stats[k] = v
	}

	if r.client != nil {
		poolStats := r.client.PoolStats()
		stats["pool_hits"] = poolStats.Hits
		stats["pool_misses"] = poolStats.Misses
		stats["pool_timeouts"] = poolStats.Timeouts
		stats["pool_total_conns"] = poolStats.TotalConns
		stats["pool_idle_conns"] = poolStats.IdleConns
		stats["pool_stale_conns"] = poolStats.StaleConns
	}

	return stats
}

// prefixKey adds prefix to key
func (r *redisCache) prefixKey(key string) string {
	if r.prefix == "" {
		return key
	}
	return r.prefix + ":" + key
}

// unprefixKey removes prefix from key
func (r *redisCache) unprefixKey(key string) string {
	if r.prefix == "" {
		return key
	}
	prefix := r.prefix + ":"
	if strings.HasPrefix(key, prefix) {
		return key[len(prefix):]
	}
	return key
}

// Get retrieves a value from Redis
func (r *redisCache) Get(ctx context.Context, key string) ([]byte, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	result, err := r.client.Get(ctx, r.prefixKey(key)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("key not found")
		}
		return nil, err
	}

	return result, nil
}

// Set stores a value in Redis
func (r *redisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	return r.client.Set(ctx, r.prefixKey(key), value, ttl).Err()
}

// Delete removes a key from Redis
func (r *redisCache) Delete(ctx context.Context, key string) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	return r.client.Del(ctx, r.prefixKey(key)).Err()
}

// Exists checks if a key exists in Redis
func (r *redisCache) Exists(ctx context.Context, key string) (bool, error) {
	if !r.connected {
		return false, fmt.Errorf("Redis not connected")
	}

	count, err := r.client.Exists(ctx, r.prefixKey(key)).Result()
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// GetMulti retrieves multiple values from Redis
func (r *redisCache) GetMulti(ctx context.Context, keys []string) (map[string][]byte, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	if len(keys) == 0 {
		return make(map[string][]byte), nil
	}

	// Prefix keys
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.prefixKey(key)
	}

	// Get values
	values, err := r.client.MGet(ctx, prefixedKeys...).Result()
	if err != nil {
		return nil, err
	}

	// Build result map
	result := make(map[string][]byte)
	for i, value := range values {
		if value != nil {
			if str, ok := value.(string); ok {
				result[keys[i]] = []byte(str)
			}
		}
	}

	return result, nil
}

// SetMulti stores multiple values in Redis
func (r *redisCache) SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	if len(items) == 0 {
		return nil
	}

	// Use pipeline for better performance
	pipe := r.client.Pipeline()

	for key, value := range items {
		pipe.Set(ctx, r.prefixKey(key), value, ttl)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// DeleteMulti removes multiple keys from Redis
func (r *redisCache) DeleteMulti(ctx context.Context, keys []string) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	if len(keys) == 0 {
		return nil
	}

	// Prefix keys
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.prefixKey(key)
	}

	return r.client.Del(ctx, prefixedKeys...).Err()
}

// Expire sets TTL for a key
func (r *redisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	return r.client.Expire(ctx, r.prefixKey(key), ttl).Err()
}

// TTL returns the TTL for a key
func (r *redisCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	if !r.connected {
		return 0, fmt.Errorf("Redis not connected")
	}

	ttl, err := r.client.TTL(ctx, r.prefixKey(key)).Result()
	if err != nil {
		return 0, err
	}

	return ttl, nil
}

// Increment increments a numeric value
func (r *redisCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	if !r.connected {
		return 0, fmt.Errorf("Redis not connected")
	}

	return r.client.IncrBy(ctx, r.prefixKey(key), delta).Result()
}

// Decrement decrements a numeric value
func (r *redisCache) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	if !r.connected {
		return 0, fmt.Errorf("Redis not connected")
	}

	return r.client.DecrBy(ctx, r.prefixKey(key), delta).Result()
}

// Keys returns all keys matching a pattern
func (r *redisCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	// Add prefix to pattern
	prefixedPattern := pattern
	if r.prefix != "" {
		prefixedPattern = r.prefix + ":" + pattern
	}

	keys, err := r.client.Keys(ctx, prefixedPattern).Result()
	if err != nil {
		return nil, err
	}

	// Remove prefix from keys
	result := make([]string, len(keys))
	for i, key := range keys {
		result[i] = r.unprefixKey(key)
	}

	return result, nil
}

// Clear removes all keys from Redis (use with caution)
func (r *redisCache) Clear(ctx context.Context) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	// If we have a prefix, only clear keys with that prefix
	if r.prefix != "" {
		keys, err := r.client.Keys(ctx, r.prefix+":*").Result()
		if err != nil {
			return err
		}

		if len(keys) > 0 {
			return r.client.Del(ctx, keys...).Err()
		}

		return nil
	}

	// Clear all keys (dangerous!)
	return r.client.FlushDB(ctx).Err()
}

// GetJSON retrieves and unmarshals JSON from Redis
func (r *redisCache) GetJSON(ctx context.Context, key string, dest interface{}) error {
	data, err := r.Get(ctx, key)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// SetJSON marshals and stores JSON in Redis
func (r *redisCache) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return r.Set(ctx, key, data, ttl)
}

// Redis-specific operations

// HGet gets a field from a hash
func (r *redisCache) HGet(ctx context.Context, key, field string) ([]byte, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	result, err := r.client.HGet(ctx, r.prefixKey(key), field).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("field not found")
		}
		return nil, err
	}

	return result, nil
}

// HSet sets a field in a hash
func (r *redisCache) HSet(ctx context.Context, key, field string, value []byte) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	return r.client.HSet(ctx, r.prefixKey(key), field, value).Err()
}

// HGetAll gets all fields from a hash
func (r *redisCache) HGetAll(ctx context.Context, key string) (map[string][]byte, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	result, err := r.client.HGetAll(ctx, r.prefixKey(key)).Result()
	if err != nil {
		return nil, err
	}

	// Convert to []byte values
	hashMap := make(map[string][]byte)
	for field, value := range result {
		hashMap[field] = []byte(value)
	}

	return hashMap, nil
}

// HDel deletes fields from a hash
func (r *redisCache) HDel(ctx context.Context, key string, fields ...string) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	return r.client.HDel(ctx, r.prefixKey(key), fields...).Err()
}

// LPush pushes values to the left of a list
func (r *redisCache) LPush(ctx context.Context, key string, values ...[]byte) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	// Convert []byte values to interface{}
	interfaceValues := make([]interface{}, len(values))
	for i, value := range values {
		interfaceValues[i] = value
	}

	return r.client.LPush(ctx, r.prefixKey(key), interfaceValues...).Err()
}

// RPush pushes values to the right of a list
func (r *redisCache) RPush(ctx context.Context, key string, values ...[]byte) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	// Convert []byte values to interface{}
	interfaceValues := make([]interface{}, len(values))
	for i, value := range values {
		interfaceValues[i] = value
	}

	return r.client.RPush(ctx, r.prefixKey(key), interfaceValues...).Err()
}

// LPop pops a value from the left of a list
func (r *redisCache) LPop(ctx context.Context, key string) ([]byte, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	result, err := r.client.LPop(ctx, r.prefixKey(key)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("list is empty")
		}
		return nil, err
	}

	return result, nil
}

// RPop pops a value from the right of a list
func (r *redisCache) RPop(ctx context.Context, key string) ([]byte, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	result, err := r.client.RPop(ctx, r.prefixKey(key)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("list is empty")
		}
		return nil, err
	}

	return result, nil
}

// LLen gets the length of a list
func (r *redisCache) LLen(ctx context.Context, key string) (int64, error) {
	if !r.connected {
		return 0, fmt.Errorf("Redis not connected")
	}

	return r.client.LLen(ctx, r.prefixKey(key)).Result()
}

// LRange gets a range of elements from a list
func (r *redisCache) LRange(ctx context.Context, key string, start, stop int64) ([][]byte, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	results, err := r.client.LRange(ctx, r.prefixKey(key), start, stop).Result()
	if err != nil {
		return nil, err
	}

	// Convert to [][]byte
	byteResults := make([][]byte, len(results))
	for i, result := range results {
		byteResults[i] = []byte(result)
	}

	return byteResults, nil
}

// SAdd adds members to a set
func (r *redisCache) SAdd(ctx context.Context, key string, members ...[]byte) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	// Convert []byte members to interface{}
	interfaceMembers := make([]interface{}, len(members))
	for i, member := range members {
		interfaceMembers[i] = member
	}

	return r.client.SAdd(ctx, r.prefixKey(key), interfaceMembers...).Err()
}

// SMembers gets all members of a set
func (r *redisCache) SMembers(ctx context.Context, key string) ([][]byte, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	results, err := r.client.SMembers(ctx, r.prefixKey(key)).Result()
	if err != nil {
		return nil, err
	}

	// Convert to [][]byte
	byteResults := make([][]byte, len(results))
	for i, result := range results {
		byteResults[i] = []byte(result)
	}

	return byteResults, nil
}

// SIsMember checks if a member exists in a set
func (r *redisCache) SIsMember(ctx context.Context, key string, member []byte) (bool, error) {
	if !r.connected {
		return false, fmt.Errorf("Redis not connected")
	}

	return r.client.SIsMember(ctx, r.prefixKey(key), member).Result()
}

// SRem removes members from a set
func (r *redisCache) SRem(ctx context.Context, key string, members ...[]byte) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	// Convert []byte members to interface{}
	interfaceMembers := make([]interface{}, len(members))
	for i, member := range members {
		interfaceMembers[i] = member
	}

	return r.client.SRem(ctx, r.prefixKey(key), interfaceMembers...).Err()
}

// SCard gets the cardinality of a set
func (r *redisCache) SCard(ctx context.Context, key string) (int64, error) {
	if !r.connected {
		return 0, fmt.Errorf("Redis not connected")
	}

	return r.client.SCard(ctx, r.prefixKey(key)).Result()
}

// Transaction support

// TxPipeline creates a transaction pipeline
func (r *redisCache) TxPipeline() redis.Pipeliner {
	if r.client == nil {
		return nil
	}
	return r.client.TxPipeline()
}

// Watch watches keys for changes
func (r *redisCache) Watch(ctx context.Context, fn func(*redis.Tx) error, keys ...string) error {
	if !r.connected {
		return fmt.Errorf("Redis not connected")
	}

	// Prefix keys
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.prefixKey(key)
	}

	return r.client.Watch(ctx, fn, prefixedKeys...)
}

// GetConnectionInfo returns Redis connection information
func (r *redisCache) GetConnectionInfo(ctx context.Context) (map[string]interface{}, error) {
	if !r.connected {
		return nil, fmt.Errorf("Redis not connected")
	}

	info := make(map[string]interface{})

	// Get server info
	infoResult, err := r.client.Info(ctx).Result()
	if err != nil {
		return nil, err
	}

	// Parse info string
	lines := strings.Split(infoResult, "\r\n")
	for _, line := range lines {
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])

				// Try to parse as number
				if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
					info[key] = intValue
				} else if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
					info[key] = floatValue
				} else {
					info[key] = value
				}
			}
		}
	}

	return info, nil
}

// init function to override the Redis constructor
func init() {
	NewRedisCache = func(config CacheConfig) (Cache, error) {
		return newRedisCache(config)
	}
}
