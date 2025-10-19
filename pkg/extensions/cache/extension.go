package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/cache"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/extensions"
	"github.com/xraph/forge/pkg/logger"
)

// CacheExtension provides caching capabilities as an extension
type CacheExtension struct {
	manager      *cache.Manager
	config       CacheConfig
	logger       common.Logger
	metrics      common.Metrics
	status       extensions.ExtensionStatus
	startedAt    time.Time
	capabilities []string
}

// CacheConfig contains configuration for the cache extension
type CacheConfig struct {
	EnableRedis       bool           `yaml:"enable_redis" default:"false"`
	EnableMemory      bool           `yaml:"enable_memory" default:"true"`
	EnableDistributed bool           `yaml:"enable_distributed" default:"false"`
	EnableReplication bool           `yaml:"enable_replication" default:"false"`
	EnableSharding    bool           `yaml:"enable_sharding" default:"false"`
	EnableWarming     bool           `yaml:"enable_warming" default:"false"`
	EnableMonitoring  bool           `yaml:"enable_monitoring" default:"true"`
	DefaultTTL        time.Duration  `yaml:"default_ttl" default:"1h"`
	MaxSize           int64          `yaml:"max_size" default:"1000000"`
	RedisURL          string         `yaml:"redis_url" default:"redis://localhost:6379"`
	RedisPassword     string         `yaml:"redis_password"`
	RedisDB           int            `yaml:"redis_db" default:"0"`
	MemorySize        int64          `yaml:"memory_size" default:"100000"`
	ShardCount        int            `yaml:"shard_count" default:"16"`
	ReplicaCount      int            `yaml:"replica_count" default:"2"`
	Logger            common.Logger  `yaml:"-"`
	Metrics           common.Metrics `yaml:"-"`
}

// NewCacheExtension creates a new cache extension
func NewCacheExtension(config CacheConfig) *CacheExtension {
	capabilities := []string{"memory"}
	if config.EnableRedis {
		capabilities = append(capabilities, "redis")
	}
	if config.EnableDistributed {
		capabilities = append(capabilities, "distributed")
	}
	if config.EnableReplication {
		capabilities = append(capabilities, "replication")
	}
	if config.EnableSharding {
		capabilities = append(capabilities, "sharding")
	}
	if config.EnableWarming {
		capabilities = append(capabilities, "warming")
	}
	if config.EnableMonitoring {
		capabilities = append(capabilities, "monitoring")
	}

	return &CacheExtension{
		config:       config,
		logger:       config.Logger,
		metrics:      config.Metrics,
		status:       extensions.ExtensionStatusUnknown,
		capabilities: capabilities,
	}
}

// Extension interface implementation

func (ce *CacheExtension) Name() string {
	return "cache"
}

func (ce *CacheExtension) Version() string {
	return "1.0.0"
}

func (ce *CacheExtension) Description() string {
	return "Caching capabilities including Redis, in-memory, distributed, and replicated caching"
}

func (ce *CacheExtension) Dependencies() []string {
	return []string{} // No dependencies for now
}

func (ce *CacheExtension) Initialize(ctx context.Context, config extensions.ExtensionConfig) error {
	ce.status = extensions.ExtensionStatusLoading

	// Initialize cache manager
	cacheConfig := cache.ManagerConfig{
		EnableRedis:       ce.config.EnableRedis,
		EnableMemory:      ce.config.EnableMemory,
		EnableDistributed: ce.config.EnableDistributed,
		EnableReplication: ce.config.EnableReplication,
		EnableSharding:    ce.config.EnableSharding,
		EnableWarming:     ce.config.EnableWarming,
		EnableMonitoring:  ce.config.EnableMonitoring,
		DefaultTTL:        ce.config.DefaultTTL,
		MaxSize:           ce.config.MaxSize,
		RedisURL:          ce.config.RedisURL,
		RedisPassword:     ce.config.RedisPassword,
		RedisDB:           ce.config.RedisDB,
		MemorySize:        ce.config.MemorySize,
		ShardCount:        ce.config.ShardCount,
		ReplicaCount:      ce.config.ReplicaCount,
		Logger:            ce.logger,
		Metrics:           ce.metrics,
	}

	var err error
	ce.manager, err = cache.NewManager(cacheConfig)
	if err != nil {
		ce.status = extensions.ExtensionStatusError
		return fmt.Errorf("failed to initialize cache manager: %w", err)
	}

	ce.status = extensions.ExtensionStatusLoaded

	if ce.logger != nil {
		ce.logger.Info("Cache extension initialized",
			logger.Strings("capabilities", ce.capabilities),
		)
	}

	return nil
}

func (ce *CacheExtension) Start(ctx context.Context) error {
	if ce.manager == nil {
		return fmt.Errorf("cache extension not initialized")
	}

	ce.status = extensions.ExtensionStatusStarting

	if err := ce.manager.Start(ctx); err != nil {
		ce.status = extensions.ExtensionStatusError
		return fmt.Errorf("failed to start cache manager: %w", err)
	}

	ce.status = extensions.ExtensionStatusRunning
	ce.startedAt = time.Now()

	if ce.logger != nil {
		ce.logger.Info("Cache extension started")
	}

	return nil
}

func (ce *CacheExtension) Stop(ctx context.Context) error {
	if ce.manager == nil {
		return nil
	}

	ce.status = extensions.ExtensionStatusStopping

	if err := ce.manager.Stop(ctx); err != nil {
		ce.logger.Warn("failed to stop cache manager", logger.Error(err))
	}

	ce.status = extensions.ExtensionStatusStopped

	if ce.logger != nil {
		ce.logger.Info("Cache extension stopped")
	}

	return nil
}

func (ce *CacheExtension) HealthCheck(ctx context.Context) error {
	if ce.manager == nil {
		return fmt.Errorf("cache extension not initialized")
	}

	if ce.status != extensions.ExtensionStatusRunning {
		return fmt.Errorf("cache extension not running")
	}

	// Perform health check on cache manager
	if err := ce.manager.HealthCheck(ctx); err != nil {
		return fmt.Errorf("cache manager health check failed: %w", err)
	}

	return nil
}

func (ce *CacheExtension) GetCapabilities() []string {
	return ce.capabilities
}

func (ce *CacheExtension) IsCapabilitySupported(capability string) bool {
	for _, cap := range ce.capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

func (ce *CacheExtension) GetConfig() interface{} {
	return ce.config
}

func (ce *CacheExtension) UpdateConfig(config interface{}) error {
	if cacheConfig, ok := config.(CacheConfig); ok {
		ce.config = cacheConfig
		return nil
	}
	return fmt.Errorf("invalid config type for cache extension")
}

// Cache-specific methods

// GetManager returns the cache manager instance
func (ce *CacheExtension) GetManager() *cache.Manager {
	return ce.manager
}

// GetStats returns cache extension statistics
func (ce *CacheExtension) GetStats() map[string]interface{} {
	if ce.manager == nil {
		return map[string]interface{}{
			"status": ce.status,
		}
	}

	stats := map[string]interface{}{
		"status":       ce.status,
		"started_at":   ce.startedAt,
		"capabilities": ce.capabilities,
	}

	// Add cache manager stats if available
	if cacheStats := ce.manager.GetStats(); cacheStats != nil {
		stats["cache_manager"] = cacheStats
	}

	return stats
}

// IsRedisEnabled returns whether Redis caching is enabled
func (ce *CacheExtension) IsRedisEnabled() bool {
	return ce.config.EnableRedis
}

// IsMemoryEnabled returns whether in-memory caching is enabled
func (ce *CacheExtension) IsMemoryEnabled() bool {
	return ce.config.EnableMemory
}

// IsDistributedEnabled returns whether distributed caching is enabled
func (ce *CacheExtension) IsDistributedEnabled() bool {
	return ce.config.EnableDistributed
}

// IsReplicationEnabled returns whether cache replication is enabled
func (ce *CacheExtension) IsReplicationEnabled() bool {
	return ce.config.EnableReplication
}

// IsShardingEnabled returns whether cache sharding is enabled
func (ce *CacheExtension) IsShardingEnabled() bool {
	return ce.config.EnableSharding
}

// IsWarmingEnabled returns whether cache warming is enabled
func (ce *CacheExtension) IsWarmingEnabled() bool {
	return ce.config.EnableWarming
}

// IsMonitoringEnabled returns whether cache monitoring is enabled
func (ce *CacheExtension) IsMonitoringEnabled() bool {
	return ce.config.EnableMonitoring
}

// GetDefaultTTL returns the default TTL for cached items
func (ce *CacheExtension) GetDefaultTTL() time.Duration {
	return ce.config.DefaultTTL
}

// GetMaxSize returns the maximum cache size
func (ce *CacheExtension) GetMaxSize() int64 {
	return ce.config.MaxSize
}

// GetRedisURL returns the Redis connection URL
func (ce *CacheExtension) GetRedisURL() string {
	return ce.config.RedisURL
}

// GetShardCount returns the number of cache shards
func (ce *CacheExtension) GetShardCount() int {
	return ce.config.ShardCount
}

// GetReplicaCount returns the number of cache replicas
func (ce *CacheExtension) GetReplicaCount() int {
	return ce.config.ReplicaCount
}
