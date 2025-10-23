package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/cache/core"
	"github.com/xraph/forge/v0/pkg/cache/serialization"
)

// DatabaseCache implements core.Cache using a database backend
type DatabaseCache struct {
	name             string
	storage          CacheStorage
	serializer       serialization.Serializer
	serializationMgr serialization.SerializationManager
	namespace        string
	defaultTTL       time.Duration
	cleanupInterval  time.Duration
	enableCleanup    bool
	enableMetrics    bool
	stats            *DatabaseCacheStats
	mu               sync.RWMutex
	stopCleanup      chan struct{}
	cleanupRunning   bool
}

func (d *DatabaseCache) Initialize(ctx context.Context) error {
	return nil
}

func (d *DatabaseCache) Type() core.CacheType {
	return core.CacheTypeDatabase
}

func (d *DatabaseCache) Name() string {
	return d.name
}

func (d *DatabaseCache) Configure(config interface{}) error {
	return nil
}

func (d *DatabaseCache) Start(ctx context.Context) error {
	return nil
}

func (d *DatabaseCache) Stop(ctx context.Context) error {
	return nil
}

// DatabaseCacheConfig contains configuration for database cache
type DatabaseCacheConfig struct {
	Name            string        `yaml:"name" json:"name"`
	Driver          string        `yaml:"driver" json:"driver" default:"postgres"`
	DSN             string        `yaml:"dsn" json:"dsn"`
	TableName       string        `yaml:"table_name" json:"table_name" default:"cache_entries"`
	Namespace       string        `yaml:"namespace" json:"namespace"`
	DefaultTTL      time.Duration `yaml:"default_ttl" json:"default_ttl" default:"1h"`
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval" default:"5m"`
	EnableCleanup   bool          `yaml:"enable_cleanup" json:"enable_cleanup" default:"true"`
	EnableMetrics   bool          `yaml:"enable_metrics" json:"enable_metrics" default:"true"`
	Serializer      string        `yaml:"serializer" json:"serializer" default:"json"`
	MaxConnections  int           `yaml:"max_connections" json:"max_connections" default:"10"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" json:"conn_max_lifetime" default:"1h"`
}

// DatabaseCacheStats provides statistics for database cache
type DatabaseCacheStats struct {
	Name           string        `json:"name"`
	Type           string        `json:"type"`
	Hits           int64         `json:"hits"`
	Misses         int64         `json:"misses"`
	Sets           int64         `json:"sets"`
	Deletes        int64         `json:"deletes"`
	Errors         int64         `json:"errors"`
	Size           int64         `json:"size"`
	Memory         int64         `json:"memory"`
	Connections    int           `json:"connections"`
	HitRatio       float64       `json:"hit_ratio"`
	LastAccess     time.Time     `json:"last_access"`
	Uptime         time.Duration `json:"uptime"`
	StartTime      time.Time     `json:"start_time"`
	CleanupRuns    int64         `json:"cleanup_runs"`
	CleanupErrors  int64         `json:"cleanup_errors"`
	ExpiredCleaned int64         `json:"expired_cleaned"`
	StorageStats   *StorageStats `json:"storage_stats,omitempty"`
}

// NewDatabaseCache creates a new database cache
func NewDatabaseCache(config DatabaseCacheConfig) (*DatabaseCache, error) {
	// Open database connection
	db, err := sql.Open(config.Driver, config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections / 2)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Create storage based on driver
	var storage CacheStorage
	switch config.Driver {
	case "postgres", "postgresql":
		storage = NewPostgresStorage(db, config.TableName, config.Namespace)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", config.Driver)
	}

	// Initialize storage
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := storage.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create serialization manager
	serializationMgr := serialization.NewSerializationManager()

	// Register default serializers
	jsonSerializer := serialization.NewJSONSerializer()
	gobSerializer := serialization.NewGobSerializer()

	serializationMgr.RegisterSerializer(jsonSerializer)
	serializationMgr.RegisterSerializer(gobSerializer)

	// Get the specified serializer
	serializer, err := serializationMgr.GetSerializer(config.Serializer)
	if err != nil {
		// Fall back to default serializer
		serializer = serializationMgr.GetDefaultSerializer()
		if serializer == nil {
			return nil, fmt.Errorf("no serializer available")
		}
	}

	cache := &DatabaseCache{
		name:             config.Name,
		storage:          storage,
		serializer:       serializer,
		serializationMgr: serializationMgr,
		namespace:        config.Namespace,
		defaultTTL:       config.DefaultTTL,
		cleanupInterval:  config.CleanupInterval,
		enableCleanup:    config.EnableCleanup,
		enableMetrics:    config.EnableMetrics,
		stats: &DatabaseCacheStats{
			Name:      config.Name,
			Type:      "database",
			StartTime: time.Now(),
		},
		stopCleanup: make(chan struct{}),
	}

	// Start cleanup goroutine if enabled
	if cache.enableCleanup {
		go cache.cleanupWorker()
	}

	return cache, nil
}

// Get retrieves a value from the cache
func (d *DatabaseCache) Get(ctx context.Context, key string) (interface{}, error) {
	d.mu.Lock()
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	entry, err := d.storage.Get(ctx, key)
	if err != nil {
		d.mu.Lock()
		d.stats.Misses++
		if err != core.ErrCacheNotFound {
			d.stats.Errors++
		}
		d.mu.Unlock()
		return nil, err
	}

	// Check if expired (defensive check)
	if entry.IsExpired() {
		d.mu.Lock()
		d.stats.Misses++
		d.mu.Unlock()

		// Async delete expired entry
		go d.storage.Delete(context.Background(), key)
		return nil, core.ErrCacheNotFound
	}

	// Deserialize value
	var value interface{}
	if err := d.serializer.Deserialize(ctx, entry.Value, &value); err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return nil, core.NewCacheError("DESERIALIZE_ERROR", "failed to deserialize value", key, "get", err)
	}

	d.mu.Lock()
	d.stats.Hits++
	d.mu.Unlock()

	return value, nil
}

// Set stores a value in the cache
func (d *DatabaseCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	d.mu.Lock()
	d.stats.Sets++
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	// Use default TTL if not specified
	if ttl == 0 {
		ttl = d.defaultTTL
	}

	// Serialize value
	data, err := d.serializer.Serialize(ctx, value)
	if err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return core.NewCacheError("SERIALIZE_ERROR", "failed to serialize value", key, "set", err)
	}

	// Create cache entry
	entry := &CacheEntry{
		Key:       key,
		Value:     data,
		Namespace: d.namespace,
		Size:      int64(len(data)),
	}

	// Set expiration if TTL is specified
	if ttl > 0 {
		expiresAt := time.Now().Add(ttl)
		entry.ExpiresAt = &expiresAt
	}

	// Store in database
	if err := d.storage.Set(ctx, entry); err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return core.NewCacheError("STORAGE_ERROR", "failed to store in database", key, "set", err)
	}

	return nil
}

// Delete removes a value from the cache
func (d *DatabaseCache) Delete(ctx context.Context, key string) error {
	d.mu.Lock()
	d.stats.Deletes++
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	err := d.storage.Delete(ctx, key)
	if err != nil && err != core.ErrCacheNotFound {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return core.NewCacheError("DELETE_ERROR", "failed to delete from database", key, "delete", err)
	}

	return err
}

// Exists checks if a key exists in the cache
func (d *DatabaseCache) Exists(ctx context.Context, key string) (bool, error) {
	d.mu.Lock()
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	exists, err := d.storage.Exists(ctx, key)
	if err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return false, core.NewCacheError("EXISTS_ERROR", "failed to check existence", key, "exists", err)
	}

	return exists, nil
}

// GetMulti retrieves multiple values from the cache
func (d *DatabaseCache) GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error) {
	d.mu.Lock()
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	entries, err := d.storage.GetMulti(ctx, keys)
	if err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return nil, core.NewCacheError("GET_MULTI_ERROR", "failed to get multiple entries", "", "get_multi", err)
	}

	result := make(map[string]interface{})
	for key, entry := range entries {
		// Check if expired
		if entry.IsExpired() {
			d.mu.Lock()
			d.stats.Misses++
			d.mu.Unlock()
			continue
		}

		// Deserialize value
		var value interface{}
		if err := d.serializer.Deserialize(ctx, entry.Value, &value); err != nil {
			d.mu.Lock()
			d.stats.Errors++
			d.mu.Unlock()
			continue
		}

		result[key] = value
		d.mu.Lock()
		d.stats.Hits++
		d.mu.Unlock()
	}

	// Count misses for keys not found
	d.mu.Lock()
	d.stats.Misses += int64(len(keys) - len(result))
	d.mu.Unlock()

	return result, nil
}

// SetMulti stores multiple values in the cache
func (d *DatabaseCache) SetMulti(ctx context.Context, items map[string]core.CacheItem) error {
	d.mu.Lock()
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	entries := make([]*CacheEntry, 0, len(items))

	for key, item := range items {
		// Serialize value
		data, err := d.serializer.Serialize(ctx, item.Value)
		if err != nil {
			d.mu.Lock()
			d.stats.Errors++
			d.mu.Unlock()
			return core.NewCacheError("SERIALIZE_ERROR", "failed to serialize value", key, "set_multi", err)
		}

		// Use item TTL or default
		ttl := item.TTL
		if ttl == 0 {
			ttl = d.defaultTTL
		}

		entry := &CacheEntry{
			Key:       key,
			Value:     data,
			Namespace: d.namespace,
			Tags:      item.Tags,
			Metadata:  item.Metadata,
			Size:      int64(len(data)),
		}

		// Set expiration if TTL is specified
		if ttl > 0 {
			expiresAt := time.Now().Add(ttl)
			entry.ExpiresAt = &expiresAt
		}

		entries = append(entries, entry)
	}

	// Store all entries
	if err := d.storage.SetMulti(ctx, entries); err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return core.NewCacheError("SET_MULTI_ERROR", "failed to store multiple entries", "", "set_multi", err)
	}

	d.mu.Lock()
	d.stats.Sets += int64(len(items))
	d.mu.Unlock()

	return nil
}

// DeleteMulti removes multiple values from the cache
func (d *DatabaseCache) DeleteMulti(ctx context.Context, keys []string) error {
	d.mu.Lock()
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	err := d.storage.DeleteMulti(ctx, keys)
	if err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return core.NewCacheError("DELETE_MULTI_ERROR", "failed to delete multiple entries", "", "delete_multi", err)
	}

	d.mu.Lock()
	d.stats.Deletes += int64(len(keys))
	d.mu.Unlock()

	return nil
}

// Increment increments a numeric value
func (d *DatabaseCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	// Database-backed increment requires a transaction for consistency
	// For simplicity, we'll implement it as get-modify-set
	// In a production system, you'd want to use database-specific atomic operations

	value, err := d.Get(ctx, key)
	if err != nil && err != core.ErrCacheNotFound {
		return 0, err
	}

	var currentValue int64
	if value != nil {
		if v, ok := value.(int64); ok {
			currentValue = v
		} else if v, ok := value.(float64); ok {
			currentValue = int64(v)
		} else {
			return 0, core.NewCacheError("TYPE_ERROR", "value is not numeric", key, "increment", nil)
		}
	}

	newValue := currentValue + delta
	err = d.Set(ctx, key, newValue, d.defaultTTL)
	if err != nil {
		return 0, err
	}

	return newValue, nil
}

// Decrement decrements a numeric value
func (d *DatabaseCache) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	return d.Increment(ctx, key, -delta)
}

// Touch updates the TTL of an entry
func (d *DatabaseCache) Touch(ctx context.Context, key string, ttl time.Duration) error {
	d.mu.Lock()
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	err := d.storage.Touch(ctx, key, ttl)
	if err != nil && err != core.ErrCacheNotFound {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return core.NewCacheError("TOUCH_ERROR", "failed to touch entry", key, "touch", err)
	}

	return err
}

// TTL returns the time to live for an entry
func (d *DatabaseCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	entry, err := d.storage.Get(ctx, key)
	if err != nil {
		return 0, err
	}

	return entry.TTL(), nil
}

// Keys returns all keys matching a pattern
func (d *DatabaseCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	d.mu.Lock()
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	keys, err := d.storage.Keys(ctx, pattern)
	if err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return nil, core.NewCacheError("KEYS_ERROR", "failed to get keys", "", "keys", err)
	}

	return keys, nil
}

// DeletePattern removes entries matching a pattern
func (d *DatabaseCache) DeletePattern(ctx context.Context, pattern string) error {
	d.mu.Lock()
	d.stats.LastAccess = time.Now()
	d.mu.Unlock()

	err := d.storage.DeletePattern(ctx, pattern)
	if err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return core.NewCacheError("DELETE_PATTERN_ERROR", "failed to delete pattern", pattern, "delete_pattern", err)
	}

	return nil
}

// Flush clears all entries in the cache
func (d *DatabaseCache) Flush(ctx context.Context) error {
	return d.DeletePattern(ctx, "*")
}

// Size returns the number of entries in the cache
func (d *DatabaseCache) Size(ctx context.Context) (int64, error) {
	size, err := d.storage.Size(ctx)
	if err != nil {
		d.mu.Lock()
		d.stats.Errors++
		d.mu.Unlock()
		return 0, core.NewCacheError("SIZE_ERROR", "failed to get size", "", "size", err)
	}

	d.mu.Lock()
	d.stats.Size = size
	d.mu.Unlock()

	return size, nil
}

// Close closes the cache and cleans up resources
func (d *DatabaseCache) Close() error {
	// Stop cleanup worker
	if d.cleanupRunning {
		close(d.stopCleanup)
	}

	// Close storage
	return d.storage.Close()
}

// Stats returns cache statistics
func (d *DatabaseCache) Stats() core.CacheStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Calculate hit ratio
	total := d.stats.Hits + d.stats.Misses
	if total > 0 {
		d.stats.HitRatio = float64(d.stats.Hits) / float64(total)
	}

	// Calculate uptime
	d.stats.Uptime = time.Since(d.stats.StartTime)

	// Get storage stats if available
	if d.enableMetrics {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		storageStats, err := d.storage.GetStats(ctx)
		if err == nil {
			d.stats.StorageStats = storageStats
		}
	}

	return core.CacheStats{
		Name:        d.stats.Name,
		Type:        d.stats.Type,
		Hits:        d.stats.Hits,
		Misses:      d.stats.Misses,
		Sets:        d.stats.Sets,
		Deletes:     d.stats.Deletes,
		Errors:      d.stats.Errors,
		Size:        d.stats.Size,
		Memory:      d.stats.Memory,
		Connections: d.stats.Connections,
		HitRatio:    d.stats.HitRatio,
		LastAccess:  d.stats.LastAccess,
		Uptime:      d.stats.Uptime,
		Custom: map[string]interface{}{
			"cleanup_runs":    d.stats.CleanupRuns,
			"cleanup_errors":  d.stats.CleanupErrors,
			"expired_cleaned": d.stats.ExpiredCleaned,
			"storage_stats":   d.stats.StorageStats,
		},
	}
}

// HealthCheck performs a health check on the cache
func (d *DatabaseCache) HealthCheck(ctx context.Context) error {
	// Test basic operations
	testKey := fmt.Sprintf("__health_check_%d", time.Now().UnixNano())
	testValue := "health_check_value"

	// Test set
	if err := d.Set(ctx, testKey, testValue, time.Minute); err != nil {
		return core.NewCacheError("HEALTH_CHECK_FAILED", "set operation failed", testKey, "health_check", err)
	}

	// Test get
	value, err := d.Get(ctx, testKey)
	if err != nil {
		return core.NewCacheError("HEALTH_CHECK_FAILED", "get operation failed", testKey, "health_check", err)
	}

	if value != testValue {
		return core.NewCacheError("HEALTH_CHECK_FAILED", "value mismatch", testKey, "health_check", nil)
	}

	// Test delete
	if err := d.Delete(ctx, testKey); err != nil {
		return core.NewCacheError("HEALTH_CHECK_FAILED", "delete operation failed", testKey, "health_check", err)
	}

	return nil
}

// cleanupWorker runs periodic cleanup of expired entries
func (d *DatabaseCache) cleanupWorker() {
	d.cleanupRunning = true
	defer func() { d.cleanupRunning = false }()

	ticker := time.NewTicker(d.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.runCleanup()
		case <-d.stopCleanup:
			return
		}
	}
}

// runCleanup performs cleanup of expired entries
func (d *DatabaseCache) runCleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	d.mu.Lock()
	d.stats.CleanupRuns++
	d.mu.Unlock()

	cleaned, err := d.storage.Clean(ctx)
	if err != nil {
		d.mu.Lock()
		d.stats.CleanupErrors++
		d.stats.Errors++
		d.mu.Unlock()
		return
	}

	d.mu.Lock()
	d.stats.ExpiredCleaned += cleaned
	d.mu.Unlock()
}

// DatabaseCacheBuilder helps build database cache instances
type DatabaseCacheBuilder struct {
	config DatabaseCacheConfig
}

// NewDatabaseCacheBuilder creates a new database cache builder
func NewDatabaseCacheBuilder() *DatabaseCacheBuilder {
	return &DatabaseCacheBuilder{
		config: DatabaseCacheConfig{
			Driver:          "postgres",
			TableName:       "cache_entries",
			DefaultTTL:      time.Hour,
			CleanupInterval: 5 * time.Minute,
			EnableCleanup:   true,
			EnableMetrics:   true,
			Serializer:      "json",
			MaxConnections:  10,
			ConnMaxLifetime: time.Hour,
		},
	}
}

// WithName sets the cache name
func (b *DatabaseCacheBuilder) WithName(name string) *DatabaseCacheBuilder {
	b.config.Name = name
	return b
}

// WithDriver sets the database driver
func (b *DatabaseCacheBuilder) WithDriver(driver string) *DatabaseCacheBuilder {
	b.config.Driver = driver
	return b
}

// WithDSN sets the database connection string
func (b *DatabaseCacheBuilder) WithDSN(dsn string) *DatabaseCacheBuilder {
	b.config.DSN = dsn
	return b
}

// WithTableName sets the table name
func (b *DatabaseCacheBuilder) WithTableName(tableName string) *DatabaseCacheBuilder {
	b.config.TableName = tableName
	return b
}

// WithNamespace sets the cache namespace
func (b *DatabaseCacheBuilder) WithNamespace(namespace string) *DatabaseCacheBuilder {
	b.config.Namespace = namespace
	return b
}

// WithDefaultTTL sets the default TTL
func (b *DatabaseCacheBuilder) WithDefaultTTL(ttl time.Duration) *DatabaseCacheBuilder {
	b.config.DefaultTTL = ttl
	return b
}

// WithCleanupInterval sets the cleanup interval
func (b *DatabaseCacheBuilder) WithCleanupInterval(interval time.Duration) *DatabaseCacheBuilder {
	b.config.CleanupInterval = interval
	return b
}

// WithSerializer sets the serializer
func (b *DatabaseCacheBuilder) WithSerializer(serializer string) *DatabaseCacheBuilder {
	b.config.Serializer = serializer
	return b
}

// Build creates the database cache
func (b *DatabaseCacheBuilder) Build() (*DatabaseCache, error) {
	return NewDatabaseCache(b.config)
}

// NewDatabaseCacheFactory Factory function for integration with cache manager
func NewDatabaseCacheFactory() func(config interface{}) (core.CacheBackend, error) {
	return func(config interface{}) (core.CacheBackend, error) {
		var dbConfig DatabaseCacheConfig

		// Convert config to DatabaseCacheConfig
		if configMap, ok := config.(map[string]interface{}); ok {
			// Extract configuration from map
			if name, ok := configMap["name"].(string); ok {
				dbConfig.Name = name
			}
			if driver, ok := configMap["driver"].(string); ok {
				dbConfig.Driver = driver
			}
			if dsn, ok := configMap["dsn"].(string); ok {
				dbConfig.DSN = dsn
			}
			if tableName, ok := configMap["table_name"].(string); ok {
				dbConfig.TableName = tableName
			}
			if namespace, ok := configMap["namespace"].(string); ok {
				dbConfig.Namespace = namespace
			}
			if defaultTTL, ok := configMap["default_ttl"].(time.Duration); ok {
				dbConfig.DefaultTTL = defaultTTL
			}
			if cleanupInterval, ok := configMap["cleanup_interval"].(time.Duration); ok {
				dbConfig.CleanupInterval = cleanupInterval
			}
			if enableCleanup, ok := configMap["enable_cleanup"].(bool); ok {
				dbConfig.EnableCleanup = enableCleanup
			}
			if enableMetrics, ok := configMap["enable_metrics"].(bool); ok {
				dbConfig.EnableMetrics = enableMetrics
			}
			if serializer, ok := configMap["serializer"].(string); ok {
				dbConfig.Serializer = serializer
			}
		}

		return NewDatabaseCache(dbConfig)
	}
}
