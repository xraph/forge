package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// CacheManager manages multiple cache backends
type CacheManager struct {
	caches           map[string]CacheBackend
	defaultCache     string
	factory          CacheFactory
	invalidation     cachecore.InvalidationManager
	warming          cachecore.CacheWarmer
	logger           common.Logger
	metrics          common.Metrics
	config           *CacheConfig
	observers        []CacheObserver
	healthChecks     map[string]func(context.Context) error
	mu               sync.RWMutex
	started          bool
	shutdownChannel  chan struct{}
	shutdownComplete chan struct{}
}

// CacheManagerConfig contains configuration for CacheManager
type CacheManagerConfig struct {
	DefaultCache  string                        `yaml:"default_cache" json:"default_cache"`
	Caches        map[string]CacheBackendConfig `yaml:"caches" json:"caches"`
	Invalidation  InvalidationConfig            `yaml:"invalidation" json:"invalidation"`
	Warming       WarmingConfig                 `yaml:"warming" json:"warming"`
	Monitoring    MonitoringConfig              `yaml:"monitoring" json:"monitoring"`
	GlobalTTL     time.Duration                 `yaml:"global_ttl" json:"global_ttl"`
	GlobalTags    []string                      `yaml:"global_tags" json:"global_tags"`
	EnableMetrics bool                          `yaml:"enable_metrics" json:"enable_metrics"`
	EnableTracing bool                          `yaml:"enable_tracing" json:"enable_tracing"`
	MaxKeyLength  int                           `yaml:"max_key_length" json:"max_key_length"`
	MaxValueSize  int64                         `yaml:"max_value_size" json:"max_value_size"`
}

// CacheBackendConfig contains configuration for a cache backend
type CacheBackendConfig = cachecore.CacheBackendConfig

// NewCacheManager creates a new CacheManager
func NewCacheManager(config *CacheManagerConfig, logger common.Logger, metrics common.Metrics) *CacheManager {
	return &CacheManager{
		caches:           make(map[string]CacheBackend),
		factory:          NewCacheFactory(),
		logger:           logger,
		metrics:          metrics,
		config:           &CacheConfig{},
		observers:        make([]CacheObserver, 0),
		healthChecks:     make(map[string]func(context.Context) error),
		shutdownChannel:  make(chan struct{}),
		shutdownComplete: make(chan struct{}),
	}
}

// Name returns the service name
func (cm *CacheManager) Name() string {
	return "cache-manager"
}

// Dependencies returns the service dependencies
func (cm *CacheManager) Dependencies() []string {
	return []string{"metrics", "logger", "config-manager"}
}

// OnStart starts the cache manager
func (cm *CacheManager) OnStart(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.started {
		return common.ErrServiceAlreadyStarted("cache-manager", fmt.Errorf("cache manager already started"))
	}

	cm.logger.Info("starting cache manager",
		logger.String("service", cm.Name()),
		logger.Int("cache_count", len(cm.caches)),
	)

	// Initialize cache factory
	cm.initializeCacheFactory()

	// Start cache backends
	for name, backend := range cm.caches {
		if err := backend.Start(ctx); err != nil {
			cm.logger.Error("failed to start cache backend",
				logger.String("backend", name),
				logger.Error(err),
			)
			return common.ErrServiceStartFailed(name, err)
		}
	}

	// Start invalidation manager
	if cm.invalidation != nil {
		if err := cm.invalidation.Start(ctx); err != nil {
			return common.ErrServiceStartFailed("invalidation-manager", err)
		}
	}

	// Start cache warming
	if cm.warming != nil {
		if err := cm.warming.Start(ctx); err != nil {
			return common.ErrServiceStartFailed(common.CacheWarmer, err)
		}
	}

	// Start background tasks
	go cm.backgroundTasks()

	cm.started = true

	cm.logger.Info("cache manager started successfully",
		logger.String("service", cm.Name()),
		logger.String("default_cache", cm.defaultCache),
	)

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cache.manager.started").Inc()
	}

	return nil
}

// OnStop stops the cache manager
func (cm *CacheManager) OnStop(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return common.ErrServiceNotStarted(common.CacheKey, fmt.Errorf("cache manager not started"))
	}

	cm.logger.Info("stopping cache manager", logger.String("service", cm.Name()))

	// Signal shutdown
	close(cm.shutdownChannel)

	// Stop cache warming
	if cm.warming != nil {
		if err := cm.warming.Stop(ctx); err != nil {
			cm.logger.Error("failed to stop cache warmer", logger.Error(err))
		}
	}

	// Stop invalidation manager
	if cm.invalidation != nil {
		if err := cm.invalidation.Stop(ctx); err != nil {
			cm.logger.Error("failed to stop invalidation manager", logger.Error(err))
		}
	}

	// Stop cache backends
	for name, backend := range cm.caches {
		if err := backend.Stop(ctx); err != nil {
			cm.logger.Error("failed to stop cache backend",
				logger.String("backend", name),
				logger.Error(err),
			)
		}
	}

	// Wait for background tasks to complete
	select {
	case <-cm.shutdownComplete:
		cm.logger.Debug("background tasks completed")
	case <-ctx.Done():
		cm.logger.Warn("shutdown timeout, forcing stop")
	}

	cm.started = false

	cm.logger.Info("cache manager stopped successfully")

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cache.manager.stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs health check
func (cm *CacheManager) OnHealthCheck(ctx context.Context) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if !cm.started {
		return common.ErrHealthCheckFailed("cache-manager", fmt.Errorf("cache manager not started"))
	}

	var errors []error

	// Check all cache backends
	for name, backend := range cm.caches {
		if err := backend.HealthCheck(ctx); err != nil {
			errors = append(errors, fmt.Errorf("cache backend %s: %w", name, err))
		}
	}

	// Check invalidation manager
	if cm.invalidation != nil {
		if err := cm.invalidation.HealthCheck(ctx); err != nil {
			errors = append(errors, fmt.Errorf("invalidation manager: %w", err))
		}
	}

	// todo implement // Check cache warming
	// if cm.warming != nil {
	// 	if err := cm.warming.HealthCheck(ctx); err != nil {
	// 		errors = append(errors, fmt.Errorf("cache warmer: %w", err))
	// 	}
	// }

	if len(errors) > 0 {
		return common.ErrHealthCheckFailed("cache-manager", fmt.Errorf("%d components failed health check", len(errors)))
	}

	return nil
}

// RegisterCache registers a new cache backend
func (cm *CacheManager) RegisterCache(name string, backend CacheBackend) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.caches[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	cm.caches[name] = backend

	// Register health check
	cm.healthChecks[name] = func(ctx context.Context) error {
		return backend.HealthCheck(ctx)
	}

	// Set as default if it's the first cache
	if cm.defaultCache == "" {
		cm.defaultCache = name
	}

	cm.logger.Info("cache backend registered",
		logger.String("name", name),
		logger.String("type", string(backend.Type())),
	)

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cache.backends.registered").Inc()
		cm.metrics.Gauge("forge.cache.backends.count").Set(float64(len(cm.caches)))
	}

	return nil
}

// UnregisterCache unregisters a cache backend
func (cm *CacheManager) UnregisterCache(name string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	backend, exists := cm.caches[name]
	if !exists {
		return common.ErrServiceNotFound(name)
	}

	// Stop the backend
	if err := backend.Stop(context.Background()); err != nil {
		cm.logger.Error("failed to stop cache backend during unregistration",
			logger.String("name", name),
			logger.Error(err),
		)
	}

	delete(cm.caches, name)
	delete(cm.healthChecks, name)

	// Update default cache if necessary
	if cm.defaultCache == name {
		cm.defaultCache = ""
		for cacheName := range cm.caches {
			cm.defaultCache = cacheName
			break
		}
	}

	cm.logger.Info("cache backend unregistered", logger.String("name", name))

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cache.backends.unregistered").Inc()
		cm.metrics.Gauge("forge.cache.backends.count").Set(float64(len(cm.caches)))
	}

	return nil
}

// GetCache returns a cache by name
func (cm *CacheManager) GetCache(name string) (Cache, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if name == "" {
		name = cm.defaultCache
	}

	backend, exists := cm.caches[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}

	return backend, nil
}

// GetDefaultCache returns the default cache
func (cm *CacheManager) GetDefaultCache() (Cache, error) {
	return cm.GetCache("")
}

// ListCaches returns all registered caches
func (cm *CacheManager) ListCaches() []cachecore.Cache {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	names := make([]cachecore.Cache, 0, len(cm.caches))
	for _, c := range cm.caches {
		names = append(names, c)
	}

	return names
}

// SetDefaultCache sets the default cache
func (cm *CacheManager) SetDefaultCache(name string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.caches[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	cm.defaultCache = name
	cm.logger.Info("default cache updated", logger.String("name", name))

	return nil
}

// AddObserver adds a cache observer
func (cm *CacheManager) AddObserver(observer CacheObserver) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.observers = append(cm.observers, observer)
}

// RemoveObserver removes a cache observer
func (cm *CacheManager) RemoveObserver(observer CacheObserver) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for i, obs := range cm.observers {
		if obs == observer {
			cm.observers = append(cm.observers[:i], cm.observers[i+1:]...)
			break
		}
	}
}

// GetStats returns cache statistics
func (cm *CacheManager) GetStats() map[string]CacheStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	stats := make(map[string]CacheStats)
	for name, backend := range cm.caches {
		stats[name] = backend.Stats()
	}

	return stats
}

// GetCombinedStats returns combined statistics for all caches
func (cm *CacheManager) GetCombinedStats() CacheStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	combined := CacheStats{
		Name:   "combined",
		Type:   "manager",
		Custom: make(map[string]interface{}),
	}

	for name, backend := range cm.caches {
		stats := backend.Stats()
		combined.Hits += stats.Hits
		combined.Misses += stats.Misses
		combined.Sets += stats.Sets
		combined.Deletes += stats.Deletes
		combined.Errors += stats.Errors
		combined.Size += stats.Size
		combined.Memory += stats.Memory
		combined.Connections += stats.Connections

		// Store individual cache stats
		combined.Custom[name] = stats
	}

	combined.HitRatio = CalculateHitRatio(combined.Hits, combined.Misses)
	combined.LastAccess = time.Now()

	return combined
}

// InvalidatePattern invalidates keys matching a pattern across all caches
func (cm *CacheManager) InvalidatePattern(ctx context.Context, pattern string) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var errors []error

	for name, backend := range cm.caches {
		if err := backend.DeletePattern(ctx, pattern); err != nil {
			errors = append(errors, fmt.Errorf("cache %s: %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("invalidation failed for %d caches: %v", len(errors), errors)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.cache.invalidations.pattern").Inc()
	}

	return nil
}

// InvalidateByTags invalidates keys with specific tags across all caches
func (cm *CacheManager) InvalidateByTags(ctx context.Context, tags []string) error {
	if cm.invalidation == nil {
		return fmt.Errorf("invalidation manager not configured")
	}

	return cm.invalidation.InvalidateByTags(ctx, cm.defaultCache, tags)
}

// WarmCache warms the cache with predefined data
func (cm *CacheManager) WarmCache(ctx context.Context, cacheName string, config cachecore.WarmConfig) error {
	if cm.warming == nil {
		return fmt.Errorf("cache warmer not configured")
	}

	return cm.warming.WarmCache(ctx, cacheName, config)
}

// GetInvalidationManager returns the invalidation manager
func (cm *CacheManager) GetInvalidationManager() cachecore.InvalidationManager {
	return cm.invalidation
}

// GetCacheWarmer returns the cache warmer
func (cm *CacheManager) GetCacheWarmer() cachecore.CacheWarmer {
	return cm.warming
}

// GetFactory returns the cache factory
func (cm *CacheManager) GetFactory() CacheFactory {
	return cm.factory
}

// initializeCacheFactory initializes the cache factory with default backends
func (cm *CacheManager) initializeCacheFactory() {
	// Register default cache backends
	cm.factory.Register(CacheTypeMemory, func(name string, config interface{}, l common.Logger, metrics common.Metrics) (CacheBackend, error) {
		return NewMemoryCache(name, config, l, metrics)
	})

	cm.factory.Register(CacheTypeRedis, func(name string, config interface{}, l common.Logger, metrics common.Metrics) (CacheBackend, error) {
		return NewRedisCache(name, config, l, metrics)
	})

	cm.factory.Register(CacheTypeHybrid, func(name string, config interface{}, l common.Logger, metrics common.Metrics) (CacheBackend, error) {
		return NewHybridCache(name, config, l, metrics)
	})

	cm.factory.Register(CacheTypeDatabase, func(name string, config interface{}, l common.Logger, metrics common.Metrics) (CacheBackend, error) {
		return NewDatabaseCache(name, config, l, metrics)
	})
}

// backgroundTasks runs background maintenance tasks
func (cm *CacheManager) backgroundTasks() {
	defer close(cm.shutdownComplete)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cm.shutdownChannel:
			cm.logger.Debug("shutting down background tasks")
			return
		case <-ticker.C:
			cm.performMaintenance()
		}
	}
}

// performMaintenance performs periodic maintenance tasks
func (cm *CacheManager) performMaintenance() {
	ctx := context.Background()

	// Collect and report metrics
	if cm.metrics != nil {
		cm.collectMetrics()
	}

	// Check cache health
	cm.checkCacheHealth(ctx)

	// Cleanup expired entries (for caches that support it)
	cm.cleanupExpiredEntries(ctx)
}

// collectMetrics collects metrics from all caches
func (cm *CacheManager) collectMetrics() {
	stats := cm.GetCombinedStats()

	cm.metrics.Counter("forge.cache.hits").Add(float64(stats.Hits))
	cm.metrics.Counter("forge.cache.misses").Add(float64(stats.Misses))
	cm.metrics.Counter("forge.cache.sets").Add(float64(stats.Sets))
	cm.metrics.Counter("forge.cache.deletes").Add(float64(stats.Deletes))
	cm.metrics.Counter("forge.cache.errors").Add(float64(stats.Errors))
	cm.metrics.Gauge("forge.cache.size").Set(float64(stats.Size))
	cm.metrics.Gauge("forge.cache.memory").Set(float64(stats.Memory))
	cm.metrics.Gauge("forge.cache.hit_ratio").Set(stats.HitRatio)
	cm.metrics.Gauge("forge.cache.backends.count").Set(float64(len(cm.caches)))
}

// checkCacheHealth checks the health of all caches
func (cm *CacheManager) checkCacheHealth(ctx context.Context) {
	for name, healthCheck := range cm.healthChecks {
		if err := healthCheck(ctx); err != nil {
			cm.logger.Error("cache health check failed",
				logger.String("cache", name),
				logger.Error(err),
			)

			if cm.metrics != nil {
				cm.metrics.Counter("forge.cache.health_check.failed", "cache", name).Inc()
			}
		}
	}
}

// cleanupExpiredEntries performs cleanup of expired entries
func (cm *CacheManager) cleanupExpiredEntries(ctx context.Context) {
	// This would be implemented based on specific cache backend capabilities
	// For now, we'll just log the cleanup attempt
	cm.logger.Debug("performing cache cleanup")
}

// notifyObservers notifies all observers of a cache event
func (cm *CacheManager) notifyObservers(eventType string, ctx context.Context, key string, value interface{}) {
	for _, observer := range cm.observers {
		switch eventType {
		case "hit":
			observer.OnCacheHit(ctx, key, value)
		case "miss":
			observer.OnCacheMiss(ctx, key)
		case "set":
			observer.OnCacheSet(ctx, key, value)
		case "delete":
			observer.OnCacheDelete(ctx, key)
		}
	}
}

// IsStarted returns true if the cache manager is started
func (cm *CacheManager) IsStarted() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.started
}

// GetConfig returns the cache manager configuration
func (cm *CacheManager) GetConfig() *CacheConfig {
	return cm.config
}

// UpdateConfig updates the cache manager configuration
func (cm *CacheManager) UpdateConfig(config *CacheConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.config = config
	cm.logger.Info("cache manager configuration updated")

	return nil
}
