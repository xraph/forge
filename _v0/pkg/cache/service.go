package cache

import (
	"context"
	"fmt"
	"time"

	cachecore "github.com/xraph/forge/v0/pkg/cache/core"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// CacheService integrates cache management with the DI container
type CacheService struct {
	manager       cachecore.CacheManager
	config        *CacheConfig
	container     common.Container
	logger        common.Logger
	metrics       common.Metrics
	configManager common.ConfigManager
	started       bool
	startTime     time.Time
}

// NewCacheService creates a new cache service
func NewCacheService(
	container common.Container,
	logger common.Logger,
	metrics common.Metrics,
	configManager common.ConfigManager,
) *CacheService {
	return &CacheService{
		container:     container,
		logger:        logger,
		metrics:       metrics,
		configManager: configManager,
	}
}

// Name returns the service name
func (cs *CacheService) Name() string {
	return common.CacheServiceKey
}

// Dependencies returns the service dependencies
func (cs *CacheService) Dependencies() []string {
	return []string{common.LoggerKey, common.MetricsKey, common.ConfigKey}
}

// OnStart starts the cache service
func (cs *CacheService) Start(ctx context.Context) error {
	if cs.started {
		return common.ErrServiceAlreadyStarted(common.CacheServiceKey, nil)
	}

	cs.logger.Info("starting cache service", logger.String("service", cs.Name()))
	cs.startTime = time.Now()

	// Load configuration
	if err := cs.loadConfiguration(); err != nil {
		return common.ErrServiceStartFailed(common.CacheServiceKey, err)
	}

	// Validate configuration
	if err := cs.config.Validate(); err != nil {
		return common.ErrServiceStartFailed(common.CacheServiceKey, fmt.Errorf("invalid configuration: %w", err))
	}

	// Create cache manager
	cs.manager = cachecore.NewDefaultCacheManager(cs.config, cs.logger, cs.metrics)

	// Configure cache backends
	if err := cs.configureCacheBackends(); err != nil {
		return common.ErrServiceStartFailed(common.CacheServiceKey, err)
	}

	// Start cache manager
	if err := cs.manager.Start(ctx); err != nil {
		return common.ErrServiceStartFailed(common.CacheServiceKey, err)
	}

	// Register cache service with container
	if err := cs.registerWithContainer(); err != nil {
		return common.ErrServiceStartFailed(common.CacheServiceKey, err)
	}

	cs.started = true

	cs.logger.Info("cache service started successfully",
		logger.String("service", cs.Name()),
		logger.Duration("startup_time", time.Since(cs.startTime)),
		logger.Int("cache_backends", len(cs.config.Backends)),
		logger.String("default_cache", cs.config.DefaultCache),
	)

	if cs.metrics != nil {
		cs.metrics.Counter("forge.cache.service.started").Inc()
		cs.metrics.Histogram("forge.cache.service.startup_time").Observe(time.Since(cs.startTime).Seconds())
	}

	return nil
}

// OnStop stops the cache service
func (cs *CacheService) Stop(ctx context.Context) error {
	if !cs.started {
		return common.ErrServiceNotStarted(common.CacheServiceKey, nil)
	}

	cs.logger.Info("stopping cache service", logger.String("service", cs.Name()))
	stopTime := time.Now()

	// Stop cache manager
	if cs.manager != nil {
		if err := cs.manager.Stop(ctx); err != nil {
			cs.logger.Error("failed to stop cache manager", logger.Error(err))
		}
	}

	cs.started = false

	uptime := time.Since(cs.startTime)
	cs.logger.Info("cache service stopped successfully",
		logger.String("service", cs.Name()),
		logger.Duration("shutdown_time", time.Since(stopTime)),
		logger.Duration("uptime", uptime),
	)

	if cs.metrics != nil {
		cs.metrics.Counter("forge.cache.service.stopped").Inc()
		cs.metrics.Histogram("forge.cache.service.shutdown_time").Observe(time.Since(stopTime).Seconds())
		cs.metrics.Gauge("forge.cache.service.uptime").Set(uptime.Seconds())
	}

	return nil
}

// OnHealthCheck performs health check
func (cs *CacheService) OnHealthCheck(ctx context.Context) error {
	if !cs.started {
		return common.ErrHealthCheckFailed(common.CacheServiceKey, fmt.Errorf("cache service not started"))
	}

	if cs.manager == nil {
		return common.ErrHealthCheckFailed(common.CacheServiceKey, fmt.Errorf("cache manager not initialized"))
	}

	return cs.manager.OnHealthCheck(ctx)
}

// GetManager returns the cache manager
func (cs *CacheService) GetManager() cachecore.CacheManager {
	return cs.manager
}

// GetCache returns a cache by name
func (cs *CacheService) GetCache(name string) (Cache, error) {
	if cs.manager == nil {
		return nil, fmt.Errorf("cache manager not initialized")
	}

	return cs.manager.GetCache(name)
}

// GetDefaultCache returns the default cache
func (cs *CacheService) GetDefaultCache() (Cache, error) {
	if cs.manager == nil {
		return nil, fmt.Errorf("cache manager not initialized")
	}

	return cs.manager.GetDefaultCache()
}

// GetConfig returns the cache configuration
func (cs *CacheService) GetConfig() *CacheConfig {
	return cs.config
}

// IsStarted returns true if the service is started
func (cs *CacheService) IsStarted() bool {
	return cs.started
}

// GetStats returns cache statistics
func (cs *CacheService) GetStats() map[string]CacheStats {
	if cs.manager == nil {
		return make(map[string]CacheStats)
	}

	coreStats := cs.manager.GetStats()
	stats := make(map[string]CacheStats)
	for name, stat := range coreStats {
		stats[name] = CacheStats{
			TotalRequests:  stat.Hits + stat.Misses,
			HitRequests:    stat.Hits,
			MissRequests:   stat.Misses,
			SetRequests:    stat.Sets,
			DeleteRequests: stat.Deletes,
			ErrorRequests:  stat.Errors,
			TotalSize:      stat.Size,
			LastAccess:     stat.LastAccess,
			HitRate:        stat.HitRatio,
		}
	}
	return stats
}

// GetCombinedStats returns combined statistics
func (cs *CacheService) GetCombinedStats() CacheStats {
	if cs.manager == nil {
		return CacheStats{}
	}

	coreStat := cs.manager.GetCombinedStats()
	return CacheStats{
		TotalRequests:  coreStat.Hits + coreStat.Misses,
		HitRequests:    coreStat.Hits,
		MissRequests:   coreStat.Misses,
		SetRequests:    coreStat.Sets,
		DeleteRequests: coreStat.Deletes,
		ErrorRequests:  coreStat.Errors,
		TotalSize:      coreStat.Size,
		LastAccess:     coreStat.LastAccess,
		HitRate:        coreStat.HitRatio,
	}
}

// loadConfiguration loads the cache configuration
func (cs *CacheService) loadConfiguration() error {
	// Load from config manager
	config := DefaultCacheConfig

	if cs.configManager != nil {
		// Try to bind the cache configuration
		if err := cs.configManager.Bind("cache", config); err != nil {
			cs.logger.Warn("failed to bind cache configuration, using defaults",
				logger.Error(err),
			)
		}
	}

	cs.config = config

	cs.logger.Debug("cache configuration loaded",
		logger.Bool("enabled", config.Enabled),
		logger.String("default_cache", config.DefaultCache),
		logger.Int("backends", len(config.Backends)),
	)

	return nil
}

// configureCacheBackends configures all cache backends
func (cs *CacheService) configureCacheBackends() error {
	if !cs.config.Enabled {
		cs.logger.Info("cache is disabled, skipping backend configuration")
		return nil
	}

	cs.logger.Info("configuring cache backends",
		logger.Int("backend_count", len(cs.config.Backends)),
	)

	for name, backendConfig := range cs.config.Backends {
		if !backendConfig.Enabled {
			cs.logger.Debug("skipping disabled cache backend",
				logger.String("backend", name),
				logger.String("type", string(backendConfig.Type)),
			)
			continue
		}

		cs.logger.Debug("configuring cache backend",
			logger.String("backend", name),
			logger.String("type", string(backendConfig.Type)),
		)

		// Create backend using factory
		backend, err := cs.manager.GetFactory().Create(
			backendConfig.Type,
			name,
			backendConfig.Config,
			cs.logger,
			cs.metrics,
		)
		if err != nil {
			return fmt.Errorf("failed to create cache backend '%s': %w", name, err)
		}

		// Configure backend
		if err := backend.Configure(backendConfig.Config); err != nil {
			return fmt.Errorf("failed to configure cache backend '%s': %w", name, err)
		}

		// Register backend with manager
		if err := cs.manager.RegisterCache(name, backend); err != nil {
			return fmt.Errorf("failed to register cache backend '%s': %w", name, err)
		}

		cs.logger.Info("cache backend configured successfully",
			logger.String("backend", name),
			logger.String("type", string(backendConfig.Type)),
		)
	}

	// Set default cache if specified
	if cs.config.DefaultCache != "" {
		if err := cs.manager.SetDefaultCache(cs.config.DefaultCache); err != nil {
			return fmt.Errorf("failed to set default cache '%s': %w", cs.config.DefaultCache, err)
		}
	}

	return nil
}

// registerWithContainer registers the cache service with the DI container
func (cs *CacheService) registerWithContainer() error {
	if cs.container == nil {
		return nil
	}

	// Register cache manager
	if err := cs.container.Register(common.ServiceDefinition{
		Name:         "cache-manager",
		Type:         (*cachecore.CacheManager)(nil),
		Instance:     cs.manager,
		Singleton:    true,
		Dependencies: []string{common.CacheServiceKey},
	}); err != nil {
		return fmt.Errorf("failed to register cache manager: %w", err)
	}

	// Register default cache
	defaultCache, err := cs.manager.GetDefaultCache()
	if err != nil {
		return fmt.Errorf("failed to get default cache: %w", err)
	}

	if err := cs.container.Register(common.ServiceDefinition{
		Name:         "cache",
		Type:         (*Cache)(nil),
		Instance:     defaultCache,
		Singleton:    true,
		Dependencies: []string{"cache-manager"},
	}); err != nil {
		return fmt.Errorf("failed to register default cache: %w", err)
	}

	// Register named caches
	for _, cache := range cs.manager.ListCaches() {
		cacheName := cache.Name()
		cache, err := cs.manager.GetCache(cacheName)
		if err != nil {
			cs.logger.Error("failed to get cache for registration",
				logger.String("cache", cacheName),
				logger.Error(err),
			)
			continue
		}

		serviceName := fmt.Sprintf("cache-%s", cacheName)
		if err := cs.container.Register(common.ServiceDefinition{
			Name:         serviceName,
			Type:         (*Cache)(nil),
			Instance:     cache,
			Singleton:    true,
			Dependencies: []string{"cache-manager"},
		}); err != nil {
			cs.logger.Error("failed to register named cache",
				logger.String("cache", cacheName),
				logger.String("service", serviceName),
				logger.Error(err),
			)
		}
	}

	cs.logger.Debug("cache service registered with container")

	return nil
}

// Reload reloads the cache configuration
func (cs *CacheService) Reload(ctx context.Context) error {
	if !cs.started {
		return fmt.Errorf("cache service not started")
	}

	cs.logger.Info("reloading cache configuration")

	// Load new configuration
	if err := cs.loadConfiguration(); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate new configuration
	if err := cs.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Update cache manager configuration
	if err := cs.manager.UpdateConfig(cs.config); err != nil {
		return fmt.Errorf("failed to update cache manager configuration: %w", err)
	}

	cs.logger.Info("cache configuration reloaded successfully")

	if cs.metrics != nil {
		cs.metrics.Counter("forge.cache.service.reloaded").Inc()
	}

	return nil
}

// AddObserver adds a cache observer
func (cs *CacheService) AddObserver(observer CacheObserver) {
	if cs.manager != nil {
		cs.manager.AddObserver(observer)
	}
}

// RemoveObserver removes a cache observer
func (cs *CacheService) RemoveObserver(observer CacheObserver) {
	if cs.manager != nil {
		cs.manager.RemoveObserver(observer)
	}
}

// InvalidatePattern invalidates keys matching a pattern
func (cs *CacheService) InvalidatePattern(ctx context.Context, pattern string) error {
	if cs.manager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	return cs.manager.InvalidatePattern(ctx, pattern)
}

// InvalidateByTags invalidates keys with specific tags
func (cs *CacheService) InvalidateByTags(ctx context.Context, tags []string) error {
	if cs.manager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	return cs.manager.InvalidateByTags(ctx, tags)
}

// WarmCache warms a specific cache
func (cs *CacheService) WarmCache(ctx context.Context, cacheName string, config cachecore.WarmConfig) error {
	if cs.manager == nil {
		return fmt.Errorf("cache manager not initialized")
	}

	return cs.manager.WarmCache(ctx, cacheName, config)
}

// GetUptime returns the service uptime
func (cs *CacheService) GetUptime() time.Duration {
	if !cs.started {
		return 0
	}
	return time.Since(cs.startTime)
}

// GetInfo returns service information
func (cs *CacheService) GetInfo() map[string]interface{} {
	info := map[string]interface{}{
		"name":    cs.Name(),
		"started": cs.started,
		"uptime":  cs.GetUptime().String(),
	}

	if cs.config != nil {
		info["enabled"] = cs.config.Enabled
		info["default_cache"] = cs.config.DefaultCache
		info["backends"] = len(cs.config.Backends)
	}

	if cs.manager != nil {
		info["cache_stats"] = cs.manager.GetCombinedStats()
	}

	return info
}

// CacheServiceFactory creates cache services
type CacheServiceFactory struct {
	container     common.Container
	logger        common.Logger
	metrics       common.Metrics
	configManager common.ConfigManager
}

// NewCacheServiceFactory creates a new cache service factory
func NewCacheServiceFactory(
	container common.Container,
	logger common.Logger,
	metrics common.Metrics,
	configManager common.ConfigManager,
) *CacheServiceFactory {
	return &CacheServiceFactory{
		container:     container,
		logger:        logger,
		metrics:       metrics,
		configManager: configManager,
	}
}

// CreateCacheService creates a cache service
func (f *CacheServiceFactory) CreateCacheService() *CacheService {
	return NewCacheService(f.container, f.logger, f.metrics, f.configManager)
}

// RegisterCacheService registers the cache service with the container
func RegisterCacheService(container common.Container) error {
	return container.Register(common.ServiceDefinition{
		Name:         common.CacheServiceKey,
		Type:         (*CacheService)(nil),
		Constructor:  NewCacheService,
		Singleton:    true,
		Dependencies: []string{"container", "logger", "metrics", "config-manager"},
	})
}

// Default cache service constructor for DI
func NewCacheServiceForDI(
	container common.Container,
	logger common.Logger,
	metrics common.Metrics,
	configManager common.ConfigManager,
) (*CacheService, error) {
	service := NewCacheService(container, logger, metrics, configManager)
	return service, nil
}

// CacheServiceProvider provides cache services
type CacheServiceProvider struct {
	service *CacheService
}

// NewCacheServiceProvider creates a new cache service provider
func NewCacheServiceProvider(service *CacheService) *CacheServiceProvider {
	return &CacheServiceProvider{service: service}
}

// GetCacheService returns the cache service
func (p *CacheServiceProvider) GetCacheService() *CacheService {
	return p.service
}

// GetCache returns a cache by name
func (p *CacheServiceProvider) GetCache(name string) (Cache, error) {
	return p.service.GetCache(name)
}

// GetDefaultCache returns the default cache
func (p *CacheServiceProvider) GetDefaultCache() (Cache, error) {
	return p.service.GetDefaultCache()
}

// GetStats returns cache statistics
func (p *CacheServiceProvider) GetStats() map[string]CacheStats {
	return p.service.GetStats()
}

// IsHealthy checks if the cache service is healthy
func (p *CacheServiceProvider) IsHealthy(ctx context.Context) bool {
	return p.service.OnHealthCheck(ctx) == nil
}
