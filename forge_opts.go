package forge

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/cache"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/config"
	"github.com/xraph/forge/pkg/config/sources"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/di"
	"github.com/xraph/forge/pkg/events"
	"github.com/xraph/forge/pkg/health"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
	"github.com/xraph/forge/pkg/middleware"
	"github.com/xraph/forge/pkg/plugins"
	"github.com/xraph/forge/pkg/streaming"
)

// =============================================================================
// CONFIGURATION STORAGE STRUCTURE
// =============================================================================

// ApplicationConfig stores all configuration options before component initialization
type ApplicationConfig struct {
	// Basic information
	Description string
	StopTimeout time.Duration

	// Component configurations (store config, not instances)
	LoggerConfig    *logger.LoggingConfig
	MetricsConfig   *metrics.CollectorConfig
	ConfigConfig    *config.ManagerConfig
	HealthConfig    *health.HealthCheckerConfig
	ContainerConfig *di.ContainerConfig

	// Cache configurations (NEW)
	CacheConfig  *cache.CacheConfig
	CacheService *cache.CacheService
	CacheManager *cache.CacheManager

	// Pre-built instances (for cases where user provides ready instances)
	Logger        common.Logger
	Metrics       metrics.MetricsCollector
	Config        common.ConfigManager
	HealthChecker common.HealthChecker
	Container     common.Container
	Lifecycle     common.LifecycleManager
	ErrorHandler  common.ErrorHandler

	// OpenAPI configurations
	OpenAPIConfig  *common.OpenAPIConfig
	AsyncAPIConfig *common.AsyncAPIConfig

	// Service configurations
	StreamingConfig  *streaming.StreamingConfig
	DatabaseConfig   *database.DatabaseConfig
	EventBusConfig   *events.EventServiceConfig
	MiddlewareConfig *middleware.ManagerConfig

	// Component collections (to be added after initialization)
	Services      []common.Service
	Controllers   []common.Controller
	Plugins       []common.Plugin
	PluginSources []PluginSource

	// Service definitions for DI container
	ServiceDefinitions []common.ServiceDefinition

	RouterAdapter RouterAdapter

	// Config sources (can include deferred sources)
	ConfigSources []interface{} // Mix of config.ConfigSource and ConfigSourceCreator

	// Feature flags
	EnableProduction       bool
	EnableMetricsEndpoints bool
	EnableHealthEndpoints  bool
	EnableCacheEndpoints   bool
}

// NewApplicationConfig creates a new configuration with defaults
func NewApplicationConfig() *ApplicationConfig {
	return &ApplicationConfig{
		StopTimeout:        30 * time.Second,
		Services:           make([]common.Service, 0),
		Controllers:        make([]common.Controller, 0),
		Plugins:            make([]common.Plugin, 0),
		ServiceDefinitions: make([]common.ServiceDefinition, 0),
		ConfigSources:      make([]interface{}, 0), // Mix of config.ConfigSource and ConfigSourceCreator
	}
}

// =============================================================================
// FUNCTIONAL OPTIONS FOR APPLICATION CREATION
// =============================================================================

// ApplicationOption defines a functional option for configuring the application
type ApplicationOption func(*ApplicationConfig) error

// =============================================================================
// BASIC APPLICATION OPTIONS
// =============================================================================

// WithDescription sets the application description
func WithDescription(description string) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Description = description
		return nil
	}
}

// WithStopTimeout sets the graceful shutdown timeout
func WithStopTimeout(timeout time.Duration) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.StopTimeout = timeout
		return nil
	}
}

// =============================================================================
// CORE COMPONENT OPTIONS - INSTANCES
// =============================================================================

// WithLogger sets a pre-built logger instance
func WithLogger(logger common.Logger) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Logger = logger
		return nil
	}
}

// WithMetrics sets a pre-built metrics collector instance
func WithMetrics(metrics metrics.MetricsCollector) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Metrics = metrics
		return nil
	}
}

// WithConfig sets a pre-built configuration manager instance
func WithConfig(configManager common.ConfigManager) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Config = configManager
		return nil
	}
}

// WithHealthChecker sets a pre-built health checker instance
func WithHealthChecker(healthChecker common.HealthChecker) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.HealthChecker = healthChecker
		return nil
	}
}

// WithContainer sets a pre-built DI container instance
func WithContainer(container common.Container) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Container = container
		return nil
	}
}

// WithLifecycleManager sets a pre-built lifecycle manager instance
func WithLifecycleManager(lifecycle common.LifecycleManager) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Lifecycle = lifecycle
		return nil
	}
}

// WithErrorHandler sets a pre-built error handler instance
func WithErrorHandler(errorHandler common.ErrorHandler) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.ErrorHandler = errorHandler
		return nil
	}
}

// =============================================================================
// CORE COMPONENT OPTIONS - CONFIGURATIONS
// =============================================================================

// WithLoggerConfig stores logger configuration for later initialization
func WithLoggerConfig(loggerConfig logger.LoggingConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.LoggerConfig = &loggerConfig
		return nil
	}
}

// WithMetricsConfig stores metrics configuration for later initialization
func WithMetricsConfig(metricsConfig metrics.CollectorConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.MetricsConfig = &metricsConfig
		return nil
	}
}

// WithConfigOptions stores configuration manager config for later initialization
func WithConfigOptions(managerConfig config.ManagerConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.ConfigConfig = &managerConfig
		return nil
	}
}

// WithHealthConfig stores health checker configuration for later initialization
func WithHealthConfig(healthConfig health.HealthCheckerConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.HealthConfig = &healthConfig
		return nil
	}
}

// WithContainerConfig stores DI container configuration for later initialization
func WithContainerConfig(containerConfig di.ContainerConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.ContainerConfig = &containerConfig
		return nil
	}
}

// =============================================================================
// OPENAPI CONFIGURATION
// =============================================================================

// WithOpenAPI enables OpenAPI documentation
func WithOpenAPI(config common.OpenAPIConfig) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		appConfig.OpenAPIConfig = &config
		return nil
	}
}

// WithAsyncAPI enables AsyncAPI documentation for streaming endpoints
func WithAsyncAPI(config common.AsyncAPIConfig) ApplicationOption {
	return func(appConfig *ApplicationConfig) error {
		appConfig.AsyncAPIConfig = &config
		return nil
	}
}

// =============================================================================
// SERVICE REGISTRATION HELPERS
// =============================================================================

// WithService adds a service to be registered during initialization
func WithService(service common.Service) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Services = append(config.Services, service)
		return nil
	}
}

// WithServices adds multiple services to be registered during initialization
func WithServices(services ...common.Service) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Services = append(config.Services, services...)
		return nil
	}
}

// WithController adds a controller to be registered during initialization
func WithController(controller common.Controller) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Controllers = append(config.Controllers, controller)
		return nil
	}
}

// WithControllers adds multiple controllers to be registered during initialization
func WithControllers(controllers ...common.Controller) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Controllers = append(config.Controllers, controllers...)
		return nil
	}
}

// WithPlugin adds a plugin to be registered during initialization
func WithPlugin(plugin common.Plugin) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Plugins = append(config.Plugins, plugin)
		return nil
	}
}

// WithPlugins adds multiple plugins to be registered during initialization
func WithPlugins(plugins ...common.Plugin) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.Plugins = append(config.Plugins, plugins...)
		return nil
	}
}

// =============================================================================
// SERVICE DEFINITION REGISTRATION
// =============================================================================

// WithServiceDefinition stores a service definition for later registration
func WithServiceDefinition(definition common.ServiceDefinition) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.ServiceDefinitions = append(config.ServiceDefinitions, definition)
		return nil
	}
}

// WithServiceDefinitions stores multiple service definitions for later registration
func WithServiceDefinitions(definitions ...common.ServiceDefinition) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.ServiceDefinitions = append(config.ServiceDefinitions, definitions...)
		return nil
	}
}

// =============================================================================
// CONFIGURATION SOURCE HELPERS
// =============================================================================

// WithConfigSources stores configuration sources for later loading
func WithConfigSources(sources ...config.ConfigSource) ApplicationOption {
	return func(config *ApplicationConfig) error {
		for _, source := range sources {
			config.ConfigSources = append(config.ConfigSources, source)
		}
		return nil
	}
}

// WithFileConfig stores file configuration source for later loading
func WithFileConfig(filepath string) ApplicationOption {
	return func(config *ApplicationConfig) error {
		// Store the configuration, don't create the source yet
		// We'll create it later when we have access to logger
		fileSourceConfig := &FileConfigSource{
			Filepath: filepath,
			Options: sources.FileSourceOptions{
				Priority:     100,
				WatchEnabled: true,
			},
		}
		config.ConfigSources = append(config.ConfigSources, fileSourceConfig)
		return nil
	}
}

// WithEnvConfig stores environment configuration source for later loading
func WithEnvConfig(prefix string) ApplicationOption {
	return func(config *ApplicationConfig) error {
		// Store the configuration, don't create the source yet
		envSourceConfig := &EnvConfigSource{
			Prefix: prefix,
			Options: sources.EnvSourceOptions{
				Prefix:         prefix,
				Priority:       200,
				WatchEnabled:   true,
				TypeConversion: true,
			},
		}
		config.ConfigSources = append(config.ConfigSources, envSourceConfig)
		return nil
	}
}

// =============================================================================
// FEATURE CONFIGURATION
// =============================================================================

// WithDatabaseSupport stores database configuration for later initialization
func WithDatabaseSupport(dbConfig *database.DatabaseConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.DatabaseConfig = dbConfig
		return nil
	}
}

// WithEventBus stores event bus configuration for later initialization
func WithEventBus(eventConfig *events.EventServiceConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.EventBusConfig = eventConfig
		return nil
	}
}

// WithStreaming stores streaming configuration for later initialization
func WithStreaming(streamingConfig *streaming.StreamingConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.StreamingConfig = streamingConfig
		return nil
	}
}

// WithMiddleware stores middleware configuration for later initialization
func WithMiddleware(middlewareConfig *middleware.ManagerConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.MiddlewareConfig = middlewareConfig
		return nil
	}
}

// =============================================================================
// CONVENIENCE OPTIONS
// =============================================================================

// WithProductionMode enables production-optimized settings
func WithProductionMode() ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.EnableProduction = true
		if config.StopTimeout == 30*time.Second { // Only set if still default
			config.StopTimeout = 60 * time.Second
		}
		return nil
	}
}

// WithMetricsEndpoints enables metrics HTTP endpoints
func WithMetricsEndpoints() ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.EnableMetricsEndpoints = true
		return nil
	}
}

// WithHealthEndpoints enables health HTTP endpoints
func WithHealthEndpoints() ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.EnableHealthEndpoints = true
		return nil
	}
}

// =============================================================================
// HELPER TYPES FOR DEFERRED CONFIG SOURCES
// =============================================================================

// FileConfigSource stores file source configuration for later creation
type FileConfigSource struct {
	Filepath string
	Options  sources.FileSourceOptions
}

// CreateSource creates the actual config source with proper logger
func (f *FileConfigSource) CreateSource(logger common.Logger) (config.ConfigSource, error) {
	f.Options.Logger = logger
	return sources.NewFileSource(f.Filepath, f.Options)
}

// EnvConfigSource stores environment source configuration for later creation
type EnvConfigSource struct {
	Prefix  string
	Options sources.EnvSourceOptions
}

// CreateSource creates the actual config source with proper logger
func (e *EnvConfigSource) CreateSource(logger common.Logger) (config.ConfigSource, error) {
	e.Options.Logger = logger
	return sources.NewEnvSource(e.Prefix, e.Options)
}

// ConfigSourceCreator interface for deferred config source creation
type ConfigSourceCreator interface {
	CreateSource(logger common.Logger) (config.ConfigSource, error)
}

// =============================================================================
// CACHE CONFIGURATION OPTIONS
// =============================================================================

// WithCacheSupport stores cache configuration for later initialization
func WithCacheSupport(cacheConfig *cache.CacheConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.CacheConfig = cacheConfig
		return nil
	}
}

// WithCacheService stores a pre-built cache service instance
func WithCacheService(cacheService *cache.CacheService) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.CacheService = cacheService
		return nil
	}
}

// WithCacheManager stores a pre-built cache manager instance
func WithCacheManager(cacheManager *cache.CacheManager) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.CacheManager = cacheManager
		return nil
	}
}

// WithDefaultCache enables cache with default configuration
func WithDefaultCache() ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.CacheConfig = cache.DefaultCacheConfig
		return nil
	}
}

// WithRouteAdapter enables cache with default configuration
func WithRouteAdapter(adapter RouterAdapter) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.RouterAdapter = adapter
		return nil
	}
}

// WithMemoryCache enables memory-based caching with custom configuration
func WithMemoryCache(maxSize int64, maxMemory int64) ApplicationOption {
	return func(config *ApplicationConfig) error {
		cacheConfig := &cache.CacheConfig{
			Enabled:      true,
			DefaultCache: "memory",
			Namespace:    "forge",
			GlobalTTL:    time.Hour,
			MaxKeyLength: 250,
			MaxValueSize: 1024 * 1024, // 1MB
			Backends: map[string]*cache.CacheBackendConfig{
				"memory": {
					Type:    cache.CacheTypeMemory,
					Enabled: true,
					Config: &cache.MemoryConfig{
						MaxSize:        maxSize,
						MaxMemory:      maxMemory,
						EvictionPolicy: "lru",
						ShardCount:     16,
					},
				},
			},
			Serialization: cache.SerializationConfig{
				DefaultFormat: "json",
				Formats: map[string]cache.SerializerConfig{
					"json": {Enabled: true},
					"gob":  {Enabled: true},
				},
			},
			Invalidation: cache.InvalidationConfig{
				Enabled:       true,
				Strategy:      "event",
				BatchSize:     100,
				BufferSize:    1000,
				FlushInterval: time.Second,
			},
			EnableMetrics: true,
			EnableTracing: false,
		}
		config.CacheConfig = cacheConfig
		return nil
	}
}

// WithRedisCache enables Redis-based caching with custom configuration
func WithRedisCache(addresses []string, password string, database int) ApplicationOption {
	return func(config *ApplicationConfig) error {
		cacheConfig := &cache.CacheConfig{
			Enabled:      true,
			DefaultCache: "redis",
			Namespace:    "forge",
			GlobalTTL:    time.Hour,
			MaxKeyLength: 250,
			MaxValueSize: 1024 * 1024, // 1MB
			Backends: map[string]*cache.CacheBackendConfig{
				"redis": {
					Type:    cache.CacheTypeRedis,
					Enabled: true,
					Config: &cache.RedisConfig{
						Addresses:   addresses,
						Password:    password,
						Database:    database,
						PoolSize:    10,
						DialTimeout: 10 * time.Second,
					},
				},
			},
			Serialization: cache.SerializationConfig{
				DefaultFormat: "json",
				Formats: map[string]cache.SerializerConfig{
					"json": {Enabled: true},
					"gob":  {Enabled: true},
				},
			},
			Invalidation: cache.InvalidationConfig{
				Enabled:       true,
				Strategy:      "event",
				BatchSize:     100,
				BufferSize:    1000,
				FlushInterval: time.Second,
			},
			EnableMetrics: true,
			EnableTracing: false,
		}
		config.CacheConfig = cacheConfig
		return nil
	}
}

// WithHybridCache enables hybrid caching (L1 memory + L2 Redis)
func WithHybridCache(l1Config, l2Config cache.CacheBackendConfig) ApplicationOption {
	return func(config *ApplicationConfig) error {
		cacheConfig := &cache.CacheConfig{
			Enabled:      true,
			DefaultCache: "hybrid",
			Namespace:    "forge",
			GlobalTTL:    time.Hour,
			MaxKeyLength: 250,
			MaxValueSize: 1024 * 1024, // 1MB
			Backends: map[string]*cache.CacheBackendConfig{
				"hybrid": {
					Type:    cache.CacheTypeHybrid,
					Enabled: true,
					Config: cache.HybridConfig{
						L1Cache:            l1Config,
						L2Cache:            l2Config,
						PromotionInterval:  10 * time.Minute,
						PromotionThreshold: 5,
						DemotionThreshold:  2,
						ConsistencyLevel:   "eventual",
					},
				},
			},
			Serialization: cache.SerializationConfig{
				DefaultFormat: "json",
				Formats: map[string]cache.SerializerConfig{
					"json": {Enabled: true},
					"gob":  {Enabled: true},
				},
			},
			Invalidation: cache.InvalidationConfig{
				Enabled:       true,
				Strategy:      "event",
				BatchSize:     100,
				BufferSize:    1000,
				FlushInterval: time.Second,
			},
			EnableMetrics: true,
			EnableTracing: false,
		}
		config.CacheConfig = cacheConfig
		return nil
	}
}

// WithCacheInvalidation enables cache invalidation with custom configuration
func WithCacheInvalidation(strategy string, batchSize int, bufferSize int, flushInterval time.Duration) ApplicationOption {
	return func(config *ApplicationConfig) error {
		if config.CacheConfig == nil {
			config.CacheConfig = cache.DefaultCacheConfig
		}

		config.CacheConfig.Invalidation = cache.InvalidationConfig{
			Enabled:       true,
			Strategy:      strategy,
			BatchSize:     batchSize,
			BufferSize:    bufferSize,
			FlushInterval: flushInterval,
		}
		return nil
	}
}

// WithCacheWarming enables cache warming with custom configuration
func WithCacheWarming(strategy string, concurrency int, batchSize int) ApplicationOption {
	return func(config *ApplicationConfig) error {
		if config.CacheConfig == nil {
			config.CacheConfig = cache.DefaultCacheConfig
		}

		config.CacheConfig.Warming = cache.WarmingConfig{
			Enabled:       true,
			Strategy:      strategy,
			Concurrency:   concurrency,
			BatchSize:     batchSize,
			Timeout:       30 * time.Second,
			RetryAttempts: 3,
			RetryDelay:    time.Second,
		}
		return nil
	}
}

// WithCacheMetrics enables cache metrics collection
func WithCacheMetrics() ApplicationOption {
	return func(config *ApplicationConfig) error {
		if config.CacheConfig == nil {
			config.CacheConfig = cache.DefaultCacheConfig
		}

		config.CacheConfig.EnableMetrics = true
		config.CacheConfig.Monitoring = cache.MonitoringConfig{
			Enabled:         true,
			MetricsInterval: 30 * time.Second,
			HealthInterval:  10 * time.Second,
			AlertThresholds: cache.AlertThresholds{
				HitRatio:        0.8,
				ErrorRate:       0.05,
				ResponseTime:    100 * time.Millisecond,
				MemoryUsage:     0.9,
				ConnectionCount: 100,
			},
			EnableAlerts: true,
		}
		return nil
	}
}

// WithCacheEndpoints enables cache management HTTP endpoints
func WithCacheEndpoints() ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.EnableCacheEndpoints = true
		return nil
	}
}

// Helper methods for creating plugin sources

// NewFilePluginSource creates a plugin source from a file
func NewFilePluginSource(filePath string, config map[string]interface{}) PluginSource {
	return &FilePluginSource{
		Path:   filePath,
		Config: config,
	}
}

// NewMarketplacePluginSource creates a plugin source from marketplace
func NewMarketplacePluginSource(pluginID, version string, config map[string]interface{}) PluginSource {
	return &MarketplacePluginSource{
		PluginID: pluginID,
		Version:  version,
		Config:   config,
	}
}

// NewURLPluginSource creates a plugin source from URL
func NewURLPluginSource(url, version string, config map[string]interface{}) PluginSource {
	return &URLPluginSource{
		URL:     url,
		Version: version,
		Config:  config,
	}
}

// NewDirectPluginSource creates a plugin source from an existing plugin instance
func NewDirectPluginSource(plugin plugins.Plugin) PluginSource {
	return &DirectPluginSource{
		Plugin: plugin,
	}
}

// Application configuration options for plugins

// WithPluginSources adds plugin sources to load during application initialization
func WithPluginSources(sources ...PluginSource) ApplicationOption {
	return func(config *ApplicationConfig) error {
		config.PluginSources = append(config.PluginSources, sources...)
		return nil
	}
}

// WithPluginFromFile adds a file-based plugin during initialization
func WithPluginFromFile(filePath string, config map[string]interface{}) ApplicationOption {
	return WithPluginSources(NewFilePluginSource(filePath, config))
}

// WithPluginFromMarketplace adds a marketplace plugin during initialization
func WithPluginFromMarketplace(pluginID, version string, config map[string]interface{}) ApplicationOption {
	return WithPluginSources(NewMarketplacePluginSource(pluginID, version, config))
}

// Enhanced application initialization to handle plugin sources
func (app *ForgeApplication) loadPluginSources(config *ApplicationConfig) error {
	if len(config.PluginSources) == 0 {
		return nil
	}

	ctx := context.Background()
	for i, source := range config.PluginSources {
		if err := app.AddPluginFromSource(ctx, source); err != nil {
			app.logger.Warn("failed to load plugin from source",
				logger.Int("source_index", i),
				logger.String("source_type", fmt.Sprintf("%T", source)),
				logger.Error(err),
			)
			// Continue loading other plugins instead of failing completely
		}
	}

	return nil
}

// =============================================================================
// CONVENIENCE FUNCTIONS FOR APPLICATION CREATION
// =============================================================================

// NewDevelopmentApplication creates an application configured for development
func NewDevelopmentApplication(name, version string, options ...ApplicationOption) (Application, error) {
	allOptions := []ApplicationOption{
		WithDescription("Development application"),
	}
	allOptions = append(allOptions, options...)

	return NewApplication(name, version, allOptions...)
}

// NewProductionApplication creates an application configured for production
func NewProductionApplication(name, version string, options ...ApplicationOption) (Application, error) {
	allOptions := []ApplicationOption{
		WithProductionMode(),
		WithDescription("Production application"),
	}
	allOptions = append(allOptions, options...)

	return NewApplication(name, version, allOptions...)
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// RegisterServiceByType is a helper to register a service by its type
func RegisterServiceByType[T any](container common.Container, name string, constructor interface{}, singleton bool, dependencies ...string) error {
	var serviceType T
	return container.Register(common.ServiceDefinition{
		Name:         name,
		Type:         &serviceType,
		Constructor:  constructor,
		Singleton:    singleton,
		Dependencies: dependencies,
	})
}

// MustRegisterService is a helper that panics if service registration fails
func MustRegisterService(container common.Container, definition common.ServiceDefinition) {
	if err := container.Register(definition); err != nil {
		panic(fmt.Sprintf("failed to register service %s: %v", definition.Name, err))
	}
}
