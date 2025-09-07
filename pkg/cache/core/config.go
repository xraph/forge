package core

import (
	"fmt"
	"strings"
	"time"
)

// CacheBackendConfig contains configuration for a cache backend
type CacheBackendConfig struct {
	Type      CacheType `yaml:"type" json:"type"`
	Enabled   bool      `yaml:"enabled" json:"enabled"`
	Config    any       `yaml:"config" json:"config"`
	Priority  int       `yaml:"priority" json:"priority"`
	Tags      []string  `yaml:"tags" json:"tags"`
	Namespace string    `yaml:"namespace" json:"namespace"`
}

// CacheConfig contains the main cache configuration
type CacheConfig struct {
	// General settings
	Enabled      bool   `yaml:"enabled" json:"enabled" default:"true"`
	DefaultCache string `yaml:"default_cache" json:"default_cache" default:"memory"`
	Namespace    string `yaml:"namespace" json:"namespace" default:"forge"`

	// Cache backends
	Backends map[string]*CacheBackendConfig `yaml:"backends" json:"backends"`

	// Global settings
	GlobalTTL    time.Duration `yaml:"global_ttl" json:"global_ttl" default:"1h"`
	GlobalTags   []string      `yaml:"global_tags" json:"global_tags"`
	MaxKeyLength int           `yaml:"max_key_length" json:"max_key_length" default:"250"`
	MaxValueSize int64         `yaml:"max_value_size" json:"max_value_size" default:"1048576"` // 1MB
	KeyPrefix    string        `yaml:"key_prefix" json:"key_prefix"`

	// Serialization
	Serialization SerializationConfig `yaml:"serialization" json:"serialization"`

	// Invalidation
	Invalidation InvalidationConfig `yaml:"invalidation" json:"invalidation"`

	// Warming
	Warming WarmingConfig `yaml:"warming" json:"warming"`

	// Monitoring
	Monitoring MonitoringConfig `yaml:"monitoring" json:"monitoring"`

	// Advanced settings
	EnableCompression bool `yaml:"enable_compression" json:"enable_compression" default:"false"`
	EnableEncryption  bool `yaml:"enable_encryption" json:"enable_encryption" default:"false"`
	EnableMetrics     bool `yaml:"enable_metrics" json:"enable_metrics" default:"true"`
	EnableTracing     bool `yaml:"enable_tracing" json:"enable_tracing" default:"false"`

	// Circuit breaker
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuit_breaker"`

	// Rate limiting
	RateLimit RateLimitConfig `yaml:"rate_limit" json:"rate_limit"`
}

// RedisConfig contains Redis cache configuration
type RedisConfig struct {
	// Connection
	Addresses []string `yaml:"addresses" json:"addresses"`
	Password  string   `yaml:"password" json:"password"`
	Database  int      `yaml:"database" json:"database" default:"0"`
	Username  string   `yaml:"username" json:"username"`

	// Pool settings
	PoolSize        int           `yaml:"pool_size" json:"pool_size" default:"10"`
	MinIdleConns    int           `yaml:"min_idle_conns" json:"min_idle_conns" default:"2"`
	MaxIdleConns    int           `yaml:"max_idle_conns" json:"max_idle_conns" default:"5"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" json:"conn_max_lifetime" default:"1h"`
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time" json:"conn_max_idle_time" default:"30m"`

	// Timeouts
	DialTimeout  time.Duration `yaml:"dial_timeout" json:"dial_timeout" default:"5s"`
	ReadTimeout  time.Duration `yaml:"read_timeout" json:"read_timeout" default:"3s"`
	WriteTimeout time.Duration `yaml:"write_timeout" json:"write_timeout" default:"3s"`

	// Cluster settings
	EnableCluster bool     `yaml:"enable_cluster" json:"enable_cluster" default:"false"`
	ClusterNodes  []string `yaml:"cluster_nodes" json:"cluster_nodes"`

	// Sentinel settings
	EnableSentinel   bool     `yaml:"enable_sentinel" json:"enable_sentinel" default:"false"`
	SentinelAddrs    []string `yaml:"sentinel_addrs" json:"sentinel_addrs"`
	SentinelPassword string   `yaml:"sentinel_password" json:"sentinel_password"`
	MasterName       string   `yaml:"master_name" json:"master_name"`

	// Advanced
	EnablePipelining bool `yaml:"enable_pipelining" json:"enable_pipelining" default:"true"`
	PipelineSize     int  `yaml:"pipeline_size" json:"pipeline_size" default:"100"`

	// SSL/TLS
	EnableTLS  bool   `yaml:"enable_tls" json:"enable_tls" default:"false"`
	TLSConfig  string `yaml:"tls_config" json:"tls_config"`
	CertFile   string `yaml:"cert_file" json:"cert_file"`
	KeyFile    string `yaml:"key_file" json:"key_file"`
	CAFile     string `yaml:"ca_file" json:"ca_file"`
	SkipVerify bool   `yaml:"skip_verify" json:"skip_verify" default:"false"`
}

// MemoryConfig contains memory cache configuration
type MemoryConfig struct {
	// Capacity
	MaxSize   int64 `yaml:"max_size" json:"max_size" default:"100000"`        // Max items
	MaxMemory int64 `yaml:"max_memory" json:"max_memory" default:"134217728"` // 128MB

	// Eviction
	EvictionPolicy EvictionPolicy `yaml:"eviction_policy" json:"eviction_policy" default:"lru"`
	EvictionRate   float64        `yaml:"eviction_rate" json:"eviction_rate" default:"0.1"`

	// Sharding
	ShardCount  int    `yaml:"shard_count" json:"shard_count" default:"16"`
	ShardingKey string `yaml:"sharding_key" json:"sharding_key" default:"key"`

	// Cleanup
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval" default:"5m"`

	// Features
	EnableStats bool `yaml:"enable_stats" json:"enable_stats" default:"true"`
	EnableTTL   bool `yaml:"enable_ttl" json:"enable_ttl" default:"true"`
	EnableTags  bool `yaml:"enable_tags" json:"enable_tags" default:"true"`

	// Advanced
	InitialCapacity int     `yaml:"initial_capacity" json:"initial_capacity" default:"1000"`
	LoadFactor      float64 `yaml:"load_factor" json:"load_factor" default:"0.75"`
}

// HybridConfig contains hybrid cache configuration
type HybridConfig struct {
	// Tiers
	L1Cache CacheBackendConfig `yaml:"l1_cache" json:"l1_cache"`
	L2Cache CacheBackendConfig `yaml:"l2_cache" json:"l2_cache"`
	L3Cache CacheBackendConfig `yaml:"l3_cache" json:"l3_cache"`

	// Promotion/Demotion
	PromotionThreshold int           `yaml:"promotion_threshold" json:"promotion_threshold" default:"5"`
	DemotionThreshold  int           `yaml:"demotion_threshold" json:"demotion_threshold" default:"2"`
	PromotionInterval  time.Duration `yaml:"promotion_interval" json:"promotion_interval" default:"1m"`

	// Replication
	ReplicationMode  string        `yaml:"replication_mode" json:"replication_mode" default:"async"`
	ReplicationDelay time.Duration `yaml:"replication_delay" json:"replication_delay" default:"100ms"`

	// Consistency
	ConsistencyLevel   ConsistencyLevel `yaml:"consistency_level" json:"consistency_level" default:"eventual"`
	ConsistencyTimeout time.Duration    `yaml:"consistency_timeout" json:"consistency_timeout" default:"5s"`

	// Optimization
	AutoOptimize     bool          `yaml:"auto_optimize" json:"auto_optimize" default:"true"`
	OptimizeInterval time.Duration `yaml:"optimize_interval" json:"optimize_interval" default:"10m"`
}

// DatabaseConfig contains database cache configuration
type DatabaseConfig struct {
	// Connection
	DSN       string `yaml:"dsn" json:"dsn"`
	Driver    string `yaml:"driver" json:"driver" default:"postgres"`
	TableName string `yaml:"table_name" json:"table_name" default:"cache_entries"`

	// Pool settings
	MaxOpenConns    int           `yaml:"max_open_conns" json:"max_open_conns" default:"25"`
	MaxIdleConns    int           `yaml:"max_idle_conns" json:"max_idle_conns" default:"5"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" json:"conn_max_lifetime" default:"1h"`
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time" json:"conn_max_idle_time" default:"30m"`

	// Cleanup
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval" default:"10m"`
	BatchSize       int           `yaml:"batch_size" json:"batch_size" default:"1000"`

	// Features
	EnablePartitioning bool   `yaml:"enable_partitioning" json:"enable_partitioning" default:"false"`
	PartitionColumn    string `yaml:"partition_column" json:"partition_column" default:"created_at"`

	// Performance
	EnableIndexes bool `yaml:"enable_indexes" json:"enable_indexes" default:"true"`
	EnableWAL     bool `yaml:"enable_wal" json:"enable_wal" default:"true"`

	// Transactions
	TransactionIsolation string `yaml:"transaction_isolation" json:"transaction_isolation" default:"READ_COMMITTED"`
}

// SerializationConfig contains serialization configuration
type SerializationConfig struct {
	DefaultFormat string                      `yaml:"default_format" json:"default_format" default:"json"`
	Formats       map[string]SerializerConfig `yaml:"formats" json:"formats"`
	Compression   CompressionConfig           `yaml:"compression" json:"compression"`
	Encryption    EncryptionConfig            `yaml:"encryption" json:"encryption"`
}

// SerializerConfig contains serializer configuration
type SerializerConfig struct {
	Enabled     bool                   `yaml:"enabled" json:"enabled" default:"true"`
	Options     map[string]interface{} `yaml:"options" json:"options"`
	Compression bool                   `yaml:"compression" json:"compression" default:"false"`
	Encryption  bool                   `yaml:"encryption" json:"encryption" default:"false"`
}

// CompressionConfig contains compression configuration
type CompressionConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled" default:"false"`
	Algorithm string `yaml:"algorithm" json:"algorithm" default:"gzip"`
	Level     int    `yaml:"level" json:"level" default:"6"`
	MinSize   int    `yaml:"min_size" json:"min_size" default:"1024"`
}

// EncryptionConfig contains encryption configuration
type EncryptionConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled" default:"false"`
	Algorithm string `yaml:"algorithm" json:"algorithm" default:"aes-256-gcm"`
	Key       string `yaml:"key" json:"key"`
	KeyFile   string `yaml:"key_file" json:"key_file"`
}

// InvalidationConfig contains invalidation configuration
type InvalidationConfig struct {
	Enabled       bool                       `yaml:"enabled" json:"enabled" default:"true"`
	Strategy      string                     `yaml:"strategy" json:"strategy" default:"event"`
	Brokers       []InvalidationBrokerConfig `yaml:"brokers" json:"brokers"`
	Patterns      []InvalidationPattern      `yaml:"patterns" json:"patterns"`
	BatchSize     int                        `yaml:"batch_size" json:"batch_size" default:"100"`
	BufferSize    int                        `yaml:"buffer_size" json:"buffer_size" default:"1000"`
	FlushInterval time.Duration              `yaml:"flush_interval" json:"flush_interval" default:"1s"`
}

// InvalidationBrokerConfig contains invalidation broker configuration
type InvalidationBrokerConfig struct {
	Type     string                 `yaml:"type" json:"type"`
	Enabled  bool                   `yaml:"enabled" json:"enabled" default:"true"`
	Config   map[string]interface{} `yaml:"config" json:"config"`
	Priority int                    `yaml:"priority" json:"priority" default:"0"`
}

// InvalidationPattern contains invalidation pattern configuration
type InvalidationPattern struct {
	Name    string        `yaml:"name" json:"name"`
	Pattern string        `yaml:"pattern" json:"pattern"`
	Tags    []string      `yaml:"tags" json:"tags"`
	Enabled bool          `yaml:"enabled" json:"enabled" default:"true"`
	TTL     time.Duration `yaml:"ttl" json:"ttl"`
	Cascade bool          `yaml:"cascade" json:"cascade" default:"false"`
}

// WarmingConfig contains cache warming configuration
type WarmingConfig struct {
	Enabled       bool                `yaml:"enabled" json:"enabled" default:"false"`
	Strategy      string              `yaml:"strategy" json:"strategy" default:"eager"`
	DataSources   []WarmingDataSource `yaml:"data_sources" json:"data_sources"`
	Schedule      string              `yaml:"schedule" json:"schedule"`
	Concurrency   int                 `yaml:"concurrency" json:"concurrency" default:"10"`
	BatchSize     int                 `yaml:"batch_size" json:"batch_size" default:"100"`
	Timeout       time.Duration       `yaml:"timeout" json:"timeout" default:"30s"`
	RetryAttempts int                 `yaml:"retry_attempts" json:"retry_attempts" default:"3"`
	RetryDelay    time.Duration       `yaml:"retry_delay" json:"retry_delay" default:"1s"`
	EnableMetrics bool                `yaml:"enable_metrics" json:"enable_metrics" default:"true"`
	TargetCaches  []string            `yaml:"target_caches" json:"target_caches"`
}

// WarmingDataSource contains warming data source configuration
type WarmingDataSource struct {
	Name      string                 `yaml:"name" json:"name"`
	Type      string                 `yaml:"type" json:"type"`
	Enabled   bool                   `yaml:"enabled" json:"enabled" default:"true"`
	Config    map[string]interface{} `yaml:"config" json:"config"`
	Priority  int                    `yaml:"priority" json:"priority" default:"0"`
	Tags      []string               `yaml:"tags" json:"tags"`
	TTL       time.Duration          `yaml:"ttl" json:"ttl"`
	Namespace string                 `yaml:"namespace" json:"namespace"`
}

// MonitoringConfig contains monitoring configuration
type MonitoringConfig struct {
	Enabled         bool            `yaml:"enabled" json:"enabled" default:"true"`
	MetricsInterval time.Duration   `yaml:"metrics_interval" json:"metrics_interval" default:"30s"`
	HealthInterval  time.Duration   `yaml:"health_interval" json:"health_interval" default:"10s"`
	AlertThresholds AlertThresholds `yaml:"alert_thresholds" json:"alert_thresholds"`
	Endpoints       []string        `yaml:"endpoints" json:"endpoints"`
	EnableAlerts    bool            `yaml:"enable_alerts" json:"enable_alerts" default:"true"`
}

// AlertThresholds contains alert threshold configuration
type AlertThresholds struct {
	HitRatio        float64       `yaml:"hit_ratio" json:"hit_ratio" default:"0.8"`
	ErrorRate       float64       `yaml:"error_rate" json:"error_rate" default:"0.05"`
	ResponseTime    time.Duration `yaml:"response_time" json:"response_time" default:"100ms"`
	MemoryUsage     float64       `yaml:"memory_usage" json:"memory_usage" default:"0.9"`
	ConnectionCount int           `yaml:"connection_count" json:"connection_count" default:"100"`
}

// CircuitBreakerConfig contains circuit breaker configuration
type CircuitBreakerConfig struct {
	Enabled          bool          `yaml:"enabled" json:"enabled" default:"false"`
	FailureThreshold int           `yaml:"failure_threshold" json:"failure_threshold" default:"5"`
	SuccessThreshold int           `yaml:"success_threshold" json:"success_threshold" default:"3"`
	Timeout          time.Duration `yaml:"timeout" json:"timeout" default:"30s"`
	MaxRequests      int           `yaml:"max_requests" json:"max_requests" default:"10"`
	OpenTimeout      time.Duration `yaml:"open_timeout" json:"open_timeout" default:"60s"`
	HalfOpenTimeout  time.Duration `yaml:"half_open_timeout" json:"half_open_timeout" default:"30s"`
}

// RateLimitConfig contains rate limiting configuration
type RateLimitConfig struct {
	Enabled   bool          `yaml:"enabled" json:"enabled" default:"false"`
	Rate      int           `yaml:"rate" json:"rate" default:"1000"`
	Burst     int           `yaml:"burst" json:"burst" default:"100"`
	Window    time.Duration `yaml:"window" json:"window" default:"1m"`
	KeyFormat string        `yaml:"key_format" json:"key_format" default:"ip"`
	Backend   string        `yaml:"backend" json:"backend" default:"memory"`
}

// Default configurations
var DefaultCacheConfig = &CacheConfig{
	Enabled:      true,
	DefaultCache: "memory",
	Namespace:    "forge",
	GlobalTTL:    time.Hour,
	MaxKeyLength: 250,
	MaxValueSize: 1024 * 1024, // 1MB
	Backends: map[string]*CacheBackendConfig{
		"memory": {
			Type:    CacheTypeMemory,
			Enabled: true,
			Config: map[string]interface{}{
				"max_size":        100000,
				"max_memory":      128 * 1024 * 1024, // 128MB
				"eviction_policy": "lru",
				"shard_count":     16,
			},
		},
	},
	Serialization: SerializationConfig{
		DefaultFormat: "json",
		Formats: map[string]SerializerConfig{
			"json": {Enabled: true},
			"gob":  {Enabled: true},
		},
	},
	Invalidation: InvalidationConfig{
		Enabled:       true,
		Strategy:      "event",
		BatchSize:     100,
		BufferSize:    1000,
		FlushInterval: time.Second,
	},
	Warming: WarmingConfig{
		Enabled:       false,
		Strategy:      "eager",
		Concurrency:   10,
		BatchSize:     100,
		Timeout:       30 * time.Second,
		RetryAttempts: 3,
		RetryDelay:    time.Second,
	},
	Monitoring: MonitoringConfig{
		Enabled:         true,
		MetricsInterval: 30 * time.Second,
		HealthInterval:  10 * time.Second,
		AlertThresholds: AlertThresholds{
			HitRatio:        0.8,
			ErrorRate:       0.05,
			ResponseTime:    100 * time.Millisecond,
			MemoryUsage:     0.9,
			ConnectionCount: 100,
		},
		EnableAlerts: true,
	},
	EnableMetrics: true,
	EnableTracing: false,
	CircuitBreaker: CircuitBreakerConfig{
		Enabled:          false,
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          30 * time.Second,
		MaxRequests:      10,
		OpenTimeout:      60 * time.Second,
		HalfOpenTimeout:  30 * time.Second,
	},
	RateLimit: RateLimitConfig{
		Enabled:   false,
		Rate:      1000,
		Burst:     100,
		Window:    time.Minute,
		KeyFormat: "ip",
		Backend:   "memory",
	},
}

// Validate validates the cache configuration
func (c *CacheConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.DefaultCache == "" {
		return fmt.Errorf("default_cache must be specified")
	}

	if c.Backends == nil || len(c.Backends) == 0 {
		return fmt.Errorf("at least one cache backend must be configured")
	}

	// Validate default cache exists
	if _, exists := c.Backends[c.DefaultCache]; !exists {
		return fmt.Errorf("default_cache '%s' is not configured", c.DefaultCache)
	}

	// Validate individual backends
	for name, backend := range c.Backends {
		if err := backend.Validate(); err != nil {
			return fmt.Errorf("backend '%s': %w", name, err)
		}
	}

	// Validate serialization
	if err := c.Serialization.Validate(); err != nil {
		return fmt.Errorf("serialization: %w", err)
	}

	// Validate invalidation
	if err := c.Invalidation.Validate(); err != nil {
		return fmt.Errorf("invalidation: %w", err)
	}

	// Validate warming
	if err := c.Warming.Validate(); err != nil {
		return fmt.Errorf("warming: %w", err)
	}

	// Validate monitoring
	if err := c.Monitoring.Validate(); err != nil {
		return fmt.Errorf("monitoring: %w", err)
	}

	return nil
}

// Validate validates the cache backend configuration
func (c *CacheBackendConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Type == "" {
		return fmt.Errorf("cache type must be specified")
	}

	validTypes := []CacheType{CacheTypeMemory, CacheTypeRedis, CacheTypeHybrid, CacheTypeDatabase}
	valid := false
	for _, validType := range validTypes {
		if c.Type == validType {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid cache type: %s", c.Type)
	}

	return nil
}

// Validate validates the serialization configuration
func (s *SerializationConfig) Validate() error {
	if s.DefaultFormat == "" {
		return fmt.Errorf("default_format must be specified")
	}

	if s.Formats == nil || len(s.Formats) == 0 {
		return fmt.Errorf("at least one serialization format must be configured")
	}

	// Validate default format exists
	if _, exists := s.Formats[s.DefaultFormat]; !exists {
		return fmt.Errorf("default_format '%s' is not configured", s.DefaultFormat)
	}

	return nil
}

// Validate validates the invalidation configuration
func (i *InvalidationConfig) Validate() error {
	if !i.Enabled {
		return nil
	}

	validStrategies := []string{"event", "time", "manual"}
	valid := false
	for _, strategy := range validStrategies {
		if i.Strategy == strategy {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid invalidation strategy: %s", i.Strategy)
	}

	if i.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be greater than 0")
	}

	if i.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be greater than 0")
	}

	return nil
}

// Validate validates the warming configuration
func (w *WarmingConfig) Validate() error {
	if !w.Enabled {
		return nil
	}

	validStrategies := []string{"eager", "lazy", "scheduled"}
	valid := false
	for _, strategy := range validStrategies {
		if w.Strategy == strategy {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid warming strategy: %s", w.Strategy)
	}

	if w.Strategy == "scheduled" && w.Schedule == "" {
		return fmt.Errorf("schedule must be specified for scheduled warming strategy")
	}

	if w.Concurrency <= 0 {
		return fmt.Errorf("concurrency must be greater than 0")
	}

	if w.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be greater than 0")
	}

	return nil
}

// Validate validates the monitoring configuration
func (m *MonitoringConfig) Validate() error {
	if !m.Enabled {
		return nil
	}

	if m.MetricsInterval <= 0 {
		return fmt.Errorf("metrics_interval must be greater than 0")
	}

	if m.HealthInterval <= 0 {
		return fmt.Errorf("health_interval must be greater than 0")
	}

	return nil
}

// GetRedisConfig extracts Redis configuration from backend config
func GetRedisConfig(config map[string]interface{}) *RedisConfig {
	redisConfig := &RedisConfig{
		Database:         0,
		PoolSize:         10,
		MinIdleConns:     2,
		MaxIdleConns:     5,
		ConnMaxLifetime:  time.Hour,
		ConnMaxIdleTime:  30 * time.Minute,
		DialTimeout:      5 * time.Second,
		ReadTimeout:      3 * time.Second,
		WriteTimeout:     3 * time.Second,
		EnablePipelining: true,
		PipelineSize:     100,
	}

	// Extract values from config map
	if addresses, ok := config["addresses"].([]interface{}); ok {
		for _, addr := range addresses {
			if str, ok := addr.(string); ok {
				redisConfig.Addresses = append(redisConfig.Addresses, str)
			}
		}
	}

	if password, ok := config["password"].(string); ok {
		redisConfig.Password = password
	}

	if database, ok := config["database"].(int); ok {
		redisConfig.Database = database
	}

	return redisConfig
}

// GetMemoryConfig extracts memory configuration from backend config
func GetMemoryConfig(config map[string]interface{}) *MemoryConfig {
	memoryConfig := &MemoryConfig{
		MaxSize:         100000,
		MaxMemory:       128 * 1024 * 1024, // 128MB
		EvictionPolicy:  EvictionPolicyLRU,
		EvictionRate:    0.1,
		ShardCount:      16,
		ShardingKey:     "key",
		CleanupInterval: 5 * time.Minute,
		EnableStats:     true,
		EnableTTL:       true,
		EnableTags:      true,
		InitialCapacity: 1000,
		LoadFactor:      0.75,
	}

	// Extract values from config map
	if maxSize, ok := config["max_size"].(int); ok {
		memoryConfig.MaxSize = int64(maxSize)
	}

	if maxMemory, ok := config["max_memory"].(int); ok {
		memoryConfig.MaxMemory = int64(maxMemory)
	}

	if evictionPolicy, ok := config["eviction_policy"].(string); ok {
		memoryConfig.EvictionPolicy = EvictionPolicy(evictionPolicy)
	}

	if shardCount, ok := config["shard_count"].(int); ok {
		memoryConfig.ShardCount = shardCount
	}

	return memoryConfig
}

// NormalizeKey normalizes a cache key
func NormalizeKey(key string, prefix string) string {
	if prefix == "" {
		return key
	}
	return fmt.Sprintf("%s:%s", prefix, key)
}

// SplitKey splits a cache key into prefix and key
func SplitKey(key string) (string, string) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", key
}

// ValidateKeyLength validates key length
func ValidateKeyLength(key string, maxLength int) error {
	if len(key) > maxLength {
		return fmt.Errorf("key length %d exceeds maximum %d", len(key), maxLength)
	}
	return nil
}

// ValidateValueSize validates value size
func ValidateValueSize(value interface{}, maxSize int64) error {
	// This is a simplified validation
	// In practice, you'd need to serialize the value to get its size
	return nil
}
