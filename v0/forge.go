// Package forge provides a comprehensive, production-ready backend framework for Go.
//
// Forge is designed for building scalable, maintainable applications with enterprise-grade
// features including dependency injection, caching, metrics, health checks, and more.
//
// Quick Start:
//
//	app, err := forge.NewApplication(forge.ApplicationConfig{
//		Description: "my-app",
//		Port: 8080,
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	app.Router().GET("/", func(w http.ResponseWriter, r *http.Request) {
//		w.Write([]byte("Hello, Forge!"))
//	})
//
//	if err := app.Start(context.Background()); err != nil {
//		log.Fatal(err)
//	}
//
// Features:
//   - Dependency Injection: Powerful DI container with lifecycle management
//   - HTTP Routing: Multiple router backends (HTTPRouter, BunRouter)
//   - Caching: Redis, in-memory, and distributed caching
//   - Metrics: Prometheus metrics collection
//   - Health Checks: Built-in health monitoring
//   - Event System: Event-driven architecture
//   - Plugin System: Extensible plugin architecture
//   - Configuration: Multi-source configuration management
//   - Observability: Structured logging and distributed tracing
//
// For more information, see the README.md file and examples in the examples/ directory.
package v0

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xraph/forge/pkg/cache"
	cachemiddleware "github.com/xraph/forge/pkg/cache/middleware"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/config"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/di"
	"github.com/xraph/forge/pkg/events"
	"github.com/xraph/forge/pkg/health"
	healthcore "github.com/xraph/forge/pkg/health/core"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
	"github.com/xraph/forge/pkg/middleware"
	"github.com/xraph/forge/pkg/plugins"
	"github.com/xraph/forge/pkg/router"
	"github.com/xraph/forge/pkg/streaming"
)

// ForgeApplication represents the main application instance.
//
// It orchestrates all components of the Forge framework including the DI container,
// router, services, and lifecycle management. The application follows a clear
// lifecycle: initialization -> start -> running -> stop.
//
// Example:
//
//	app := &ForgeApplication{
//		name: "my-app",
//		config: ApplicationConfig{
//			Description: "My Application",
//			Port: 8080,
//		},
//	}
//
//	// Configure routes
//	app.Router().GET("/", handler)
//
//	// Start the application
//	if err := app.Start(ctx); err != nil {
//		return err
//	}
//
//	// Stop gracefully
//	defer app.Stop(ctx)
type ForgeApplication struct {
	// Basic information
	name        string
	version     string
	description string

	// Core components
	container      common.Container
	router         common.Router
	logger         common.Logger
	metrics        metrics.MetricsCollector
	config         common.ConfigManager
	healthCheck    common.HealthChecker
	lifecycle      common.LifecycleManager
	errorHandler   common.ErrorHandler
	metricsHandler *metrics.MetricsEndpointHandler
	pluginsManager *plugins.Manager

	// Add streaming manager
	streamingManager streaming.StreamingManager

	// Cache components
	cacheService *cache.CacheService
	cacheManager cache.CacheManager

	// HTTP Server
	server    *http.Server
	serverMux sync.RWMutex

	// Component collections
	services    map[string]common.Service
	controllers map[string]common.Controller

	sharedConfig commonConfig
	appConfig    *ApplicationConfig

	// OpenAPI configuration
	openAPIConfig  *common.OpenAPIConfig
	asyncAPIConfig *common.AsyncAPIConfig

	// State management
	mu          sync.RWMutex
	status      common.ApplicationStatus
	startTime   time.Time
	stopTimeout time.Duration
}

// NewApplication creates a new Forge application instance.
//
// It initializes the application with the provided name and version, and applies
// the given options to configure the application. The application is created in
// a stopped state and must be explicitly started.
//
// Parameters:
//   - name: The application name (required, cannot be empty)
//   - version: The application version (required, cannot be empty)
//   - options: Optional configuration options to customize the application
//
// Returns:
//   - Forge: The application instance
//   - error: Any error that occurred during initialization
//
// Example:
//
//	app, err := forge.NewApplication("my-app", "1.0.0",
//		forge.WithPort(8080),
//		forge.WithLogger(logger),
//		forge.WithMetrics(metrics),
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Configure and start the application
//	app.Router().GET("/", handler)
//	if err := app.Start(ctx); err != nil {
//		log.Fatal(err)
//	}
func NewApplication(name, version string, options ...ApplicationOption) (Forge, error) {
	if name == "" {
		return nil, common.ErrValidationError("name", fmt.Errorf("application name cannot be empty"))
	}
	if version == "" {
		return nil, common.ErrValidationError("version", fmt.Errorf("application version cannot be empty"))
	}

	// Step 1: Create configuration storage and apply all options
	appConfig := NewApplicationConfig()
	for _, option := range options {
		if err := option(appConfig); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// Step 2: Create the application with basic info
	app := &ForgeApplication{
		name:         name,
		version:      version,
		description:  appConfig.Description,
		services:     make(map[string]common.Service),
		controllers:  make(map[string]common.Controller),
		status:       common.ApplicationStatusNotStarted,
		stopTimeout:  appConfig.StopTimeout,
		sharedConfig: commonConfig{}, // Initialize shared config
		appConfig:    appConfig,
	}

	if app.logger == nil {
		app.logger = logger.GetGlobalLogger()
	}

	// Step 3: Initialize components in dependency order using container registration
	if err := app.initializeFromConfig(appConfig); err != nil {
		return nil, fmt.Errorf("failed to initialize application: %w", err)
	}

	return app, nil
}

// initializeFromConfig initializes all application components from the stored configuration
// Components are registered in the DI container and then resolved
func (app *ForgeApplication) initializeFromConfig(config *ApplicationConfig) error {
	// Create basic DI container (minimal, no dependencies)
	if err := app.createBasicContainer(config); err != nil {
		return fmt.Errorf("failed to create basic container: %w", err)
	}

	// Register all component service definitions in container
	if err := app.registerComponentDefinitions(config); err != nil {
		return fmt.Errorf("failed to register component definitions: %w", err)
	}

	// Register user-provided service definitions
	if err := app.registerUserServiceDefinitions(config); err != nil {
		return fmt.Errorf("failed to register user service definitions: %w", err)
	}

	// Resolve core components from container
	if err := app.resolveCoreComponents(); err != nil {
		return fmt.Errorf("failed to resolve core components: %w", err)
	}

	// Initialize router with resolved components
	if err := app.initializeRouter(config); err != nil {
		return fmt.Errorf("failed to initialize router: %w", err)
	}

	// Register application components (services, controllers, plugins)
	if err := app.registerApplicationComponents(config); err != nil {
		return fmt.Errorf("failed to register application components: %w", err)
	}

	// Enable additional features
	if err := app.enableFeatures(config); err != nil {
		return fmt.Errorf("failed to enable features: %w", err)
	}

	// Final validation
	if err := app.validateCoreComponents(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Register plugin manager service definition
	if err := app.registerPluginDefinition(config); err != nil {
		return fmt.Errorf("failed to register plugin manager: %w", err)
	}

	return nil
}

// Create basic DI container
func (app *ForgeApplication) createBasicContainer(config *ApplicationConfig) error {
	// Use provided container or create new one
	if config.Container != nil {
		app.container = config.Container
		return nil
	}

	// Create container with minimal configuration (no component dependencies yet)
	var containerConfig di.ContainerConfig
	if config.ContainerConfig != nil {
		containerConfig = *config.ContainerConfig
	}

	app.container = di.NewContainer(containerConfig)

	if err := app.container.Register(common.ServiceDefinition{
		Name:      common.ContainerKey,
		Type:      (*common.Container)(nil),
		Instance:  app.container,
		Singleton: true,
	}); err != nil {
		return err
	}

	return nil
}

// Register all component service definitions
func (app *ForgeApplication) registerComponentDefinitions(config *ApplicationConfig) error {
	// Register logger service definition
	if err := app.registerLoggerDefinition(config); err != nil {
		return fmt.Errorf("failed to register logger: %w", err)
	}

	// Register error handler service definition
	if err := app.registerErrorHandlerDefinition(config); err != nil {
		return fmt.Errorf("failed to register error handler: %w", err)
	}

	// Register metrics service definition
	if err := app.registerMetricsDefinition(config); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	// Register config manager service definition
	if err := app.registerConfigDefinition(config); err != nil {
		return fmt.Errorf("failed to register config manager: %w", err)
	}

	// Register health checker service definition
	if err := app.registerHealthCheckerDefinition(config); err != nil {
		return fmt.Errorf("failed to register health checker: %w", err)
	}

	// Register lifecycle manager service definition
	if err := app.registerLifecycleDefinition(config); err != nil {
		return fmt.Errorf("failed to register lifecycle manager: %w", err)
	}

	return nil
}

// Logger registration
func (app *ForgeApplication) registerLoggerDefinition(config *ApplicationConfig) error {
	if config.Logger != nil {
		// Use provided instance
		return app.container.Register(common.ServiceDefinition{
			Name:      common.LoggerKey,
			Type:      (*common.Logger)(nil),
			Instance:  config.Logger,
			Singleton: true,
		})
	}

	// Register factory for logger
	return app.container.Register(common.ServiceDefinition{
		Name: common.LoggerKey,
		Type: (*common.Logger)(nil),
		Constructor: func() (common.Logger, error) {
			if config.LoggerConfig != nil {
				return logger.NewLogger(*config.LoggerConfig), nil
			}
			if config.EnableProduction {
				return logger.NewProductionLogger(), nil
			}
			return logger.GetGlobalLogger(), nil
		},
		Singleton: true,
	})
}

// Error handler registration
func (app *ForgeApplication) registerErrorHandlerDefinition(opts *ApplicationConfig) error {
	if opts.ErrorHandler != nil {
		// Use provided instance
		return app.container.Register(common.ServiceDefinition{
			Name:      common.ErrorHandlerKey,
			Type:      (*common.ErrorHandler)(nil),
			Instance:  opts.ErrorHandler,
			Singleton: true,
		})
	}

	// Register factory for error handler
	return app.container.Register(common.ServiceDefinition{
		Name: common.ErrorHandlerKey,
		Type: (*common.ErrorHandler)(nil),
		Constructor: func(logger common.Logger) (common.ErrorHandler, error) {
			return NewDefaultErrorHandler(logger), nil
		},
		Singleton:    true,
		Dependencies: []string{common.LoggerKey},
	})
}

// Metrics registration
func (app *ForgeApplication) registerMetricsDefinition(config *ApplicationConfig) error {
	if config.Metrics != nil {
		// Use provided instance
		return app.container.Register(common.ServiceDefinition{
			Name:      metrics.MetricsKey,
			Type:      (*common.Metrics)(nil),
			Instance:  config.Metrics,
			Singleton: true,
		})
	}

	// Note: Metrics registration is handled by metrics.RegisterMetricsCollector()
	// See ADR-001 for architectural decision on service registration patterns

	// Use existing registered metrics service to create metrics service registration
	if app.sharedConfig.metrics == nil {
		app.sharedConfig.metrics = metrics.DefaultServiceConfig()
		app.sharedConfig.metrics.AutoRegister = true
		app.sharedConfig.metrics.CollectorConfig.EnableSystemMetrics = false
		app.sharedConfig.metrics.CollectorConfig.EnableRuntimeMetrics = false
	}

	return metrics.RegisterMetricsCollector(app.container, app.sharedConfig.metrics, app.logger)
}

// Config manager registration
func (app *ForgeApplication) registerConfigDefinition(opts *ApplicationConfig) error {
	if opts.Config != nil {
		// Use provided instance, but still need to load config sources
		app.container.Register(common.ServiceDefinition{
			Name:      common.ConfigKey,
			Type:      (*common.ConfigManager)(nil),
			Instance:  opts.Config,
			Singleton: true,
		})
		return app.loadConfigSourcesDeferred(opts)
	}

	// Store config sources for deferred loading
	app.sharedConfig.configSources = opts.ConfigSources

	// Note: Config registration is handled by config.RegisterConfigService()
	// See ADR-001 for architectural decision on service registration patterns

	var managerConfig config.ManagerConfig
	if opts.ConfigConfig != nil {
		managerConfig = *opts.ConfigConfig
	} else {
		managerConfig = config.ManagerConfig{
			CacheEnabled:   true,
			ReloadOnChange: true,
			MetricsEnabled: !opts.EnableProduction,
		}

		if opts.EnableProduction {
			managerConfig.ValidationMode = config.ValidationModeStrict
			managerConfig.SecretsEnabled = true
			managerConfig.WatchInterval = 60 * time.Second
			managerConfig.ErrorRetryCount = 5
			managerConfig.ErrorRetryDelay = 10 * time.Second
		}
	}

	// Ensure required dependencies are set
	managerConfig.Logger = app.logger
	managerConfig.Metrics = app.metrics
	managerConfig.ErrorHandler = app.errorHandler

	// Store config for shared access
	app.sharedConfig.config = &managerConfig

	if err := config.RegisterConfigService(app.container, managerConfig); err != nil {
		return err
	}

	configMgrService, err := app.container.ResolveNamed(common.ConfigKey)
	if err != nil {
		return err
	}

	configMgr, ok := configMgrService.(common.ConfigManager)
	if !ok {
		return fmt.Errorf("resolved config manager is not of correct type")
	}

	// Load config sources after manager creation
	if err := app.loadConfigSources(configMgr, opts, app.logger); err != nil {
		return fmt.Errorf("failed to load config sources: %w", err)
	}

	return nil
}

// Health checker registration
func (app *ForgeApplication) registerHealthCheckerDefinition(config *ApplicationConfig) error {
	if config.HealthChecker != nil {
		// Use provided instance
		return app.container.Register(common.ServiceDefinition{
			Name:      common.HealthCheckerKey,
			Type:      (*common.HealthChecker)(nil),
			Instance:  config.HealthChecker,
			Singleton: true,
		})
	}

	// Register factory for health checker
	var healthConfig *health.HealthCheckerConfig
	if config.HealthConfig != nil {
		healthConfig = config.HealthConfig
	} else {
		healthConfig = health.DefaultHealthCheckerConfig()
		if config.EnableProduction {
			healthConfig.CheckInterval = 30 * time.Second
			healthConfig.DefaultTimeout = 10 * time.Second
			healthConfig.EnableAutoDiscovery = true
			healthConfig.EnableAlerting = true
		}
	}

	healthServiceConf := health.DefaultHealthServiceConfig()
	healthServiceConf.HealthChecker = healthConfig

	// Register health service
	return health.RegisterHealthService(app.container, healthServiceConf)
}

// Lifecycle manager registration
func (app *ForgeApplication) registerLifecycleDefinition(config *ApplicationConfig) error {
	if config.Lifecycle != nil {
		// Use provided instance
		return app.container.Register(common.ServiceDefinition{
			Name:      common.LifecycleManagerKey,
			Type:      (*common.LifecycleManager)(nil),
			Instance:  config.Lifecycle,
			Singleton: true,
		})
	}

	// Register factory for lifecycle manager - it's typically provided by the container itself
	return app.container.Register(common.ServiceDefinition{
		Name: common.LifecycleManagerKey,
		Type: (*common.LifecycleManager)(nil),
		Constructor: func(container common.Container) (common.LifecycleManager, error) {
			return container.LifecycleManager(), nil
		},
		Singleton:    true,
		Dependencies: []string{common.ContainerKey},
	})
}

// Plugin manager registration
func (app *ForgeApplication) registerPluginDefinition(config *ApplicationConfig) error {
	// Create simplified plugin manager
	app.pluginsManager = plugins.NewManager(app, app.router, app.logger)
	return nil
}

// Register user-provided service definitions
func (app *ForgeApplication) registerUserServiceDefinitions(config *ApplicationConfig) error {
	for _, definition := range config.ServiceDefinitions {
		if err := app.container.Register(definition); err != nil {
			return fmt.Errorf("failed to register service definition %s: %w", definition.Name, err)
		}
	}
	return nil
}

// Resolve core components from container
func (app *ForgeApplication) resolveCoreComponents() error {
	// Resolve logger
	if l, err := app.container.ResolveNamed(common.LoggerKey); err != nil {
		return fmt.Errorf("failed to resolve logger: %w", err)
	} else if l, ok := l.(common.Logger); ok {
		app.logger = l
	} else {
		return fmt.Errorf("resolved logger is not of correct type")
	}

	// Resolve error handler
	if errorHandler, err := app.container.ResolveNamed(common.ErrorHandlerKey); err != nil {
		return fmt.Errorf("failed to resolve error handler: %w", err)
	} else if eh, ok := errorHandler.(common.ErrorHandler); ok {
		app.errorHandler = eh
	} else {
		return fmt.Errorf("resolved error handler is not of correct type")
	}

	// Resolve metrics
	if met, err := app.container.ResolveNamed(metrics.CollectorKey); err != nil {
		return fmt.Errorf("failed to resolve metrics: %w", err)
	} else if m, ok := met.(common.Metrics); ok {
		app.metrics = m
	} else {
		return fmt.Errorf("resolved metrics is not of correct type")
	}

	// Resolve config
	if conf, err := app.container.ResolveNamed(common.ConfigKey); err != nil {
		return fmt.Errorf("failed to resolve config: %w", err)
	} else if c, ok := conf.(common.ConfigManager); ok {
		app.config = c
	} else {
		return fmt.Errorf("resolved config is not of correct type")
	}

	// Resolve health checker
	if healthServ, err := app.container.ResolveNamed(common.HealthServiceKey); err != nil {
		return fmt.Errorf("failed to resolve health checker: %w", err)
	} else if hc, ok := healthServ.(healthcore.HealthService); ok {
		app.healthCheck = hc.GetChecker()
	} else {
		return fmt.Errorf("resolved health checker is not of correct type")
	}

	// Resolve lifecycle manager
	if lifecycle, err := app.container.ResolveNamed(common.LifecycleManagerKey); err != nil {
		return fmt.Errorf("failed to resolve lifecycle manager: %w", err)
	} else if lm, ok := lifecycle.(common.LifecycleManager); ok {
		app.lifecycle = lm
	} else {
		return fmt.Errorf("resolved lifecycle manager is not of correct type")
	}

	return nil
}

// Helper method to load config sources
func (app *ForgeApplication) loadConfigSources(configMgr common.ConfigManager, opts *ApplicationConfig, logger common.Logger) error {
	if len(opts.ConfigSources) == 0 {
		return nil
	}

	manager, ok := configMgr.(*config.Manager)
	if !ok {
		return fmt.Errorf("config manager does not support loading from sources")
	}

	var sources []config.ConfigSource
	for _, source := range opts.ConfigSources {
		switch s := source.(type) {
		case ConfigSourceCreator:
			// Create deferred sources with proper logger
			actualSource, err := s.CreateSource(logger)
			if err != nil {
				return fmt.Errorf("failed to create config source: %w", err)
			}
			sources = append(sources, actualSource)
		case config.ConfigSource:
			// Direct source
			sources = append(sources, s)
		default:
			return fmt.Errorf("unsupported config source type: %T", source)
		}
	}

	return manager.LoadFrom(sources...)
}

// Deferred config source loading for provided config instances
func (app *ForgeApplication) loadConfigSourcesDeferred(config *ApplicationConfig) error {
	// Store for later loading after logger is resolved
	app.sharedConfig.configSources = config.ConfigSources
	return nil
}

// Initialize router with resolved components
func (app *ForgeApplication) initializeRouter(config *ApplicationConfig) error {
	if app.container == nil {
		return common.ErrValidationError("container", fmt.Errorf("container must be set before initializing router"))
	}

	routerConfig := router.ForgeRouterConfig{
		Container:     app.container,
		Logger:        app.logger,
		Metrics:       app.metrics,
		Config:        app.config,
		HealthChecker: app.healthCheck,
		ErrorHandler:  app.errorHandler,
		OpenAPI:       config.OpenAPIConfig,
		AsyncAPI:      config.AsyncAPIConfig,
		Adapter:       config.RouterAdapter,
	}

	app.router = router.NewForgeRouter(routerConfig)

	// Enable cache middleware if cache is configured (ADD THIS)
	if config.CacheConfig != nil && config.CacheConfig.Enabled {
		if err := app.enableCacheMiddleware(config.CacheConfig); err != nil {
			return fmt.Errorf("failed to enable cache middleware: %w", err)
		}
	}

	// Enable OpenAPI if configured
	if config.OpenAPIConfig != nil {
		app.router.EnableOpenAPI(*config.OpenAPIConfig)
		app.openAPIConfig = config.OpenAPIConfig
	}

	// Enable AsyncAPI if configured
	if config.AsyncAPIConfig != nil {
		app.router.EnableAsyncAPI(*config.AsyncAPIConfig)
		app.asyncAPIConfig = config.AsyncAPIConfig
	}

	return nil
}

// Register application components
func (app *ForgeApplication) registerApplicationComponents(config *ApplicationConfig) error {
	// Register services
	for _, service := range config.Services {
		if err := app.AddService(service); err != nil {
			return fmt.Errorf("failed to add service %s: %w", service.Name(), err)
		}
	}

	// Register controllers
	for _, controller := range config.Controllers {
		if err := app.AddController(controller); err != nil {
			return fmt.Errorf("failed to add controller %s: %w", controller.Name(), err)
		}
	}

	// Register plugins
	for _, plugin := range config.Plugins {
		if err := app.AddPlugin(plugin); err != nil {
			return fmt.Errorf("failed to add plugin %s: %w", plugin.Name(), err)
		}
	}

	return nil
}

// Enable additional features
func (app *ForgeApplication) enableFeatures(config *ApplicationConfig) error {
	// Register standard services (now that all core components are available)
	if err := app.registerStandardServices(); err != nil {
		return fmt.Errorf("failed to register standard services: %w", err)
	}

	// Enable streaming if configured
	if config.StreamingConfig != nil {
		if err := app.enableStreamingFeature(config.StreamingConfig); err != nil {
			return fmt.Errorf("failed to enable streaming: %w", err)
		}
	}

	// Enable database if configured
	if config.DatabaseConfig != nil {
		if err := app.enableDatabaseFeature(config.DatabaseConfig); err != nil {
			return fmt.Errorf("failed to enable database: %w", err)
		}
	}

	// Enable event bus if configured
	if config.EventBusConfig != nil {
		if err := app.enableEventBusFeature(config.EventBusConfig); err != nil {
			return fmt.Errorf("failed to enable event bus: %w", err)
		}
	}

	// Enable middleware if configured
	if config.MiddlewareConfig != nil {
		if err := app.enableMiddlewareFeature(config.MiddlewareConfig); err != nil {
			return fmt.Errorf("failed to enable middleware: %w", err)
		}
	}

	// Enable endpoints if requested
	if config.EnableMetricsEndpoints {
		if err := app.EnableMetricsEndpoints(); err != nil {
			return fmt.Errorf("failed to enable metrics endpoints: %w", err)
		}
	}

	if config.EnableHealthEndpoints {
		if err := app.EnableHealthEndpoints(); err != nil {
			return fmt.Errorf("failed to enable health endpoints: %w", err)
		}
	}

	// Enable cache endpoints if requested (ADD THIS)
	if config.EnableCacheEndpoints {
		if err := app.EnableCacheEndpoints(); err != nil {
			return fmt.Errorf("failed to enable cache endpoints: %w", err)
		}
	}

	return nil
}

// Register standard services - now using container registration pattern
func (app *ForgeApplication) registerStandardServices() error {
	// Note: Standard services are registered using helper functions
	// See ADR-001 for architectural decision on service registration patterns

	return nil
}

// Feature enablers - using container registration
func (app *ForgeApplication) enableStreamingFeature(config *streaming.StreamingConfig) error {
	err := streaming.RegisterStreamingService(app.container, config)
	if err != nil {
		return err
	}

	// Get streaming manager from service
	if service, err := app.container.ResolveNamed(common.StreamingServiceKey); err == nil {
		if streamingService, ok := service.(streaming.StreamingService); ok {
			app.streamingManager = streamingService.GetManager()
		}
	}

	return nil
}

func (app *ForgeApplication) enableDatabaseFeature(config *database.DatabaseConfig) error {
	return app.container.Register(common.ServiceDefinition{
		Name: common.DatabaseManagerKey,
		Type: (*database.DatabaseManager)(nil),
		Constructor: func(logger common.Logger, metrics common.Metrics, configMgr common.ConfigManager) (database.DatabaseManager, error) {
			manager := database.NewManager(logger, metrics, configMgr)
			if err := manager.SetConfig(config); err != nil {
				return nil, err
			}
			return manager, nil
		},
		Singleton:    true,
		Dependencies: []string{common.LoggerKey, metrics.MetricsKey, common.ConfigKey},
	})
}

func (app *ForgeApplication) enableEventBusFeature(config *events.EventServiceConfig) error {
	return app.container.Register(common.ServiceDefinition{
		Name: common.EventBusKey,
		Type: (*events.EventBus)(nil),
		Constructor: func(logger common.Logger, metrics common.Metrics, dbManager common.DatabaseManager, configMgr common.ConfigManager) (events.EventBus, error) {
			service := events.NewEventService(config, dbManager, logger, metrics)
			if err := service.Validate(); err != nil {
				return nil, err
			}
			return service.GetEventBus(), nil
		},
		Singleton:    true,
		Dependencies: []string{common.LoggerKey, metrics.MetricsKey, common.DatabaseManagerKey, common.ConfigKey},
	})
}

func (app *ForgeApplication) enableMiddlewareFeature(config *middleware.ManagerConfig) error {
	return app.container.Register(common.ServiceDefinition{
		Name: "middleware-manager",
		Type: (*middleware.Manager)(nil),
		Constructor: func(logger common.Logger, metrics common.Metrics, configMgr common.ConfigManager) (*middleware.Manager, error) {
			return middleware.NewManager(app.container, logger, metrics, configMgr), nil
		},
		Singleton:    true,
		Dependencies: []string{common.LoggerKey, metrics.MetricsKey, common.ConfigKey},
	})
}

// enableCacheMiddleware adds cache middleware to the router
func (app *ForgeApplication) enableCacheMiddleware(conf *cache.CacheConfig) error {
	if app.router == nil || app.cacheManager == nil {
		return nil // Not ready yet
	}

	// Create cache middleware
	cacheMiddleware := cachemiddleware.NewCachingMiddleware(
		app.cacheManager,
		app.logger,
		app.metrics,
		&cachemiddleware.CachingConfig{
			CacheName: conf.DefaultCache,
		},
	)

	// Add middleware to router
	app.router.UseMiddleware(cacheMiddleware.Handler())

	if app.logger != nil {
		app.logger.Info("cache middleware enabled")
	}

	return nil
}

// =============================================================================
// BASIC INFORMATION
// =============================================================================

func (app *ForgeApplication) Name() string {
	return app.name
}

func (app *ForgeApplication) Version() string {
	return app.version
}

func (app *ForgeApplication) Description() string {
	return app.description
}

// =============================================================================
// CORE COMPONENTS
// =============================================================================

func (app *ForgeApplication) Container() common.Container {
	return app.container
}

// Router returns the HTTP router instance.
//
// The router is used to register HTTP routes, middleware, and handlers.
// It supports multiple backends including HTTPRouter and BunRouter.
//
// Returns:
//   - common.Router: The router instance
//
// Example:
//
//	// Register a GET route
//	app.Router().GET("/api/users", getUserHandler)
//
//	// Register a POST route
//	app.Router().POST("/api/users", createUserHandler)
//
//	// Add middleware
//	app.Router().Use(middleware.CORS())
//	app.Router().Use(middleware.RateLimit(100))
func (app *ForgeApplication) Router() common.Router {
	return app.router
}

func (app *ForgeApplication) Logger() common.Logger {
	return app.logger
}

func (app *ForgeApplication) Metrics() metrics.MetricsCollector {
	return app.metrics
}

func (app *ForgeApplication) Config() common.ConfigManager {
	return app.config
}

func (app *ForgeApplication) HealthChecker() common.HealthChecker {
	return app.healthCheck
}

// SetContainer sets the DI container and initializes the router
func (app *ForgeApplication) SetContainer(container common.Container) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("set_container", fmt.Errorf("cannot set container after application has started"))
	}

	app.container = container
	return app.initializeRouter(app.appConfig)
}

func (app *ForgeApplication) SetLogger(logger common.Logger) {
	app.logger = logger
}

func (app *ForgeApplication) SetMetrics(metrics metrics.MetricsCollector) {
	app.metrics = metrics
}

func (app *ForgeApplication) SetConfig(config common.ConfigManager) {
	app.config = config
}

func (app *ForgeApplication) SetHealthChecker(healthChecker common.HealthChecker) {
	app.healthCheck = healthChecker
}

func (app *ForgeApplication) SetLifecycleManager(lifecycle common.LifecycleManager) {
	app.lifecycle = lifecycle
}

func (app *ForgeApplication) SetErrorHandler(errorHandler common.ErrorHandler) {
	app.errorHandler = errorHandler
}

// =============================================================================
// SERVICE MANAGEMENT
// =============================================================================

func (app *ForgeApplication) AddService(service common.Service) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("add_service", fmt.Errorf("cannot add service after application has started"))
	}

	serviceName := service.Name()
	if _, exists := app.services[serviceName]; exists {
		return common.ErrServiceAlreadyExists(serviceName)
	}

	app.services[serviceName] = service

	// Add to lifecycle manager if available
	if app.lifecycle != nil {
		if err := app.lifecycle.AddService(service); err != nil {
			delete(app.services, serviceName)
			return err
		}
	}

	// Register service in DI container if possible
	if app.container != nil {
		if err := app.registerServiceInContainer(service); err != nil {
			if app.logger != nil {
				app.logger.Warn("failed to register service in DI container",
					logger.String("service", serviceName),
					logger.Error(err),
				)
			}
		}
	}

	if app.logger != nil {
		app.logger.Info("service added",
			logger.String("service", serviceName),
			logger.String("type", fmt.Sprintf("%T", service)),
			logger.String("dependencies", fmt.Sprintf("%v", service.Dependencies())),
		)
	}

	return nil
}

func (app *ForgeApplication) RemoveService(serviceName string) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("remove_service", fmt.Errorf("cannot remove service after application has started"))
	}

	if _, exists := app.services[serviceName]; !exists {
		return common.ErrServiceNotFound(serviceName)
	}

	delete(app.services, serviceName)

	// Remove from lifecycle manager if available
	if app.lifecycle != nil {
		if err := app.lifecycle.RemoveService(serviceName); err != nil {
			return err
		}
	}

	if app.logger != nil {
		app.logger.Info("service removed", logger.String("service", serviceName))
	}

	return nil
}

func (app *ForgeApplication) GetService(serviceName string) (common.Service, error) {
	app.mu.RLock()
	defer app.mu.RUnlock()

	service, exists := app.services[serviceName]
	if !exists {
		return nil, common.ErrServiceNotFound(serviceName)
	}

	return service, nil
}

func (app *ForgeApplication) GetServices() []common.Service {
	app.mu.RLock()
	defer app.mu.RUnlock()

	services := make([]common.Service, 0, len(app.services))
	for _, service := range app.services {
		services = append(services, service)
	}

	return services
}

// =============================================================================
// CONTROLLER MANAGEMENT
// =============================================================================

func (app *ForgeApplication) AddController(controller common.Controller) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("add_controller", fmt.Errorf("cannot add controller after application has started"))
	}

	controllerName := controller.Name()
	if _, exists := app.controllers[controllerName]; exists {
		return common.ErrServiceAlreadyExists(controllerName)
	}

	// Initialize controller with container
	if err := controller.Initialize(app.container); err != nil {
		return common.ErrContainerError("initialize_controller", err)
	}

	app.controllers[controllerName] = controller

	// Register controller with router if router is available
	if app.router != nil {
		if err := app.router.RegisterController(controller); err != nil {
			delete(app.controllers, controllerName)
			return err
		}
	}

	if app.logger != nil {
		app.logger.Info("controller added",
			logger.String("controller", controllerName),
			logger.String("type", fmt.Sprintf("%T", controller)),
			logger.String("dependencies", fmt.Sprintf("%v", controller.Dependencies())),
			logger.Int("middleware", len(controller.Middleware())),
		)
	}

	return nil
}

func (app *ForgeApplication) RemoveController(controllerName string) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("remove_controller", fmt.Errorf("cannot remove controller after application has started"))
	}

	if _, exists := app.controllers[controllerName]; !exists {
		return common.ErrServiceNotFound(controllerName)
	}

	// Unregister from router if available
	if app.router != nil {
		if err := app.router.UnregisterController(controllerName); err != nil {
			return err
		}
	}

	delete(app.controllers, controllerName)

	if app.logger != nil {
		app.logger.Info("controller removed", logger.String("controller", controllerName))
	}

	return nil
}

func (app *ForgeApplication) GetController(controllerName string) (common.Controller, error) {
	app.mu.RLock()
	defer app.mu.RUnlock()

	controller, exists := app.controllers[controllerName]
	if !exists {
		return nil, common.ErrServiceNotFound(controllerName)
	}

	return controller, nil
}

func (app *ForgeApplication) GetControllers() []common.Controller {
	app.mu.RLock()
	defer app.mu.RUnlock()

	controllers := make([]common.Controller, 0, len(app.controllers))
	for _, controller := range app.controllers {
		controllers = append(controllers, controller)
	}

	return controllers
}

// =============================================================================
// PLUGIN MANAGEMENT
// =============================================================================

func (app *ForgeApplication) AddPlugin(plugin common.Plugin) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("add_plugin", fmt.Errorf("cannot add plugin after application has started"))
	}

	pluginName := plugin.Name()
	err := app.pluginsManager.AddPlugin(context.Background(), plugin)
	if err != nil {
		return err
	}

	if app.logger != nil {
		app.logger.Info("plugin added",
			logger.String("plugin", pluginName),
			logger.String("version", plugin.Version()),
		)
	}

	return nil
}

func (app *ForgeApplication) RemovePlugin(pluginName string) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("remove_plugin", fmt.Errorf("cannot remove plugin after application has started"))
	}

	// Remove plugin from plugin manager
	if app.pluginsManager != nil {
		if err := app.pluginsManager.RemovePlugin(context.Background(), pluginName); err != nil {
			// Log warning but don't fail - plugin might not have been loaded via manager
			if app.logger != nil {
				app.logger.Warn("failed to remove plugin from manager",
					logger.String("plugin", pluginName),
					logger.Error(err),
				)
			}
		}
	}

	if app.logger != nil {
		app.logger.Info("plugin removed", logger.String("plugin", pluginName))
	}

	return nil
}

func (app *ForgeApplication) GetPlugin(pluginName string) (common.Plugin, error) {
	return app.pluginsManager.GetPlugin(pluginName)
}

func (app *ForgeApplication) GetPlugins() []common.Plugin {
	return app.pluginsManager.GetPlugins()
}

// =============================================================================
// LIFECYCLE MANAGEMENT
// =============================================================================

// Start initializes and starts the application.
//
// It performs the following operations in order:
// 1. Validates the application configuration
// 2. Initializes core components (logger, metrics, config, etc.)
// 3. Starts the dependency injection container
// 4. Starts all registered services
// 5. Starts the HTTP server
// 6. Sets up graceful shutdown handling
//
// The application must be in a stopped state to start successfully.
// If the application is already running, this method returns an error.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//
// Returns:
//   - error: Any error that occurred during startup
//
// Example:
//
//	ctx := context.Background()
//	if err := app.Start(ctx); err != nil {
//		log.Fatalf("Failed to start application: %v", err)
//	}
//
//	// Application is now running and ready to serve requests
func (app *ForgeApplication) Start(ctx context.Context) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusNotStarted {
		return common.ErrLifecycleError("start", fmt.Errorf("application already started or in invalid state: %s", app.status))
	}

	app.startTime = time.Now()

	if app.logger != nil {
		app.logger.Info("starting application",
			logger.String("name", app.name),
			logger.String("version", app.version),
			logger.Int("services", len(app.services)),
			logger.Int("controllers", len(app.controllers)),
			// logger.Int("plugins", len(app.plugins)),
		)
	}

	// Validate configuration
	if err := app.validateConfiguration(); err != nil {
		app.status = common.ApplicationStatusError
		return err
	}

	// Start plugins BEFORE container starts to allow service and controller registration
	// Keep status as NotStarted during plugin initialization
	if app.pluginsManager != nil {
		if err := app.pluginsManager.StartPlugins(ctx); err != nil {
			app.status = common.ApplicationStatusError
			return fmt.Errorf("failed to start plugins: %w", err)
		}
	}

	// Now set status to Starting after plugins have registered their components
	app.status = common.ApplicationStatusStarting

	// Start container - this locks service registration
	if app.container != nil {
		if err := app.container.Start(ctx); err != nil {
			app.status = common.ApplicationStatusError
			return common.ErrContainerError("start", err)
		}
	}

	// Start services through lifecycle manager
	if app.lifecycle != nil {
		if err := app.lifecycle.StartServices(ctx); err != nil {
			app.status = common.ApplicationStatusError
			return err
		}
	}

	// Start router
	if app.router != nil {
		if err := app.router.Start(ctx); err != nil {
			app.status = common.ApplicationStatusError
			return err
		}
	}

	app.status = common.ApplicationStatusRunning

	if app.logger != nil {
		app.logger.Info("application started",
			logger.String("name", app.name),
			logger.String("version", app.version),
			logger.Duration("startup_time", time.Since(app.startTime)),
			logger.String("status", string(app.status)),
		)
	}

	// Record startup metrics
	if app.metrics != nil {
		app.metrics.Counter("forge.application.started").Inc()
		app.metrics.Histogram("forge.application.startup_time").Observe(time.Since(app.startTime).Seconds())
		app.metrics.Gauge("forge.application.services").Set(float64(len(app.services)))
		app.metrics.Gauge("forge.application.controllers").Set(float64(len(app.controllers)))
		app.metrics.Gauge("forge.application.plugins").Set(float64(len(app.pluginsManager.GetPlugins())))
	}

	return nil
}

// Stop gracefully shuts down the application.
//
// It performs the following operations in order:
// 1. Stops the HTTP server gracefully
// 2. Stops the router
// 3. Stops all registered services through the lifecycle manager
// 4. Stops the dependency injection container
// 5. Cleans up resources
//
// The application must be in a running state to stop successfully.
// If the application is already stopped, this method returns an error.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//
// Returns:
//   - error: Any error that occurred during shutdown
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	if err := app.Stop(ctx); err != nil {
//		log.Printf("Error during shutdown: %v", err)
//	}
func (app *ForgeApplication) Stop(ctx context.Context) error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.status != common.ApplicationStatusRunning {
		return common.ErrLifecycleError("stop", fmt.Errorf("application not running, current status: %s", app.status))
	}

	app.status = common.ApplicationStatusStopping
	stopStart := time.Now()

	if app.logger != nil {
		app.logger.Info("stopping application", logger.String("name", app.name))
	}

	// Collect shutdown errors
	var shutdownErrors []error

	// Stop HTTP server first
	if app.server != nil {
		if err := app.stopHTTPServer(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop HTTP server: %w", err))
		}
	}

	// Stop plugins
	if app.pluginsManager != nil {
		if err := app.pluginsManager.StopPlugins(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop plugins: %w", err))
		}
	}

	// Stop router
	if app.router != nil {
		if err := app.router.Stop(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop router: %w", err))
		}
	}

	// Stop services through lifecycle manager
	if app.lifecycle != nil {
		if err := app.lifecycle.StopServices(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop services: %w", err))
		}
	}

	// Stop container
	if app.container != nil {
		if err := app.container.Stop(ctx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop container: %w", err))
		}
	}

	// Return combined shutdown errors if any occurred
	if len(shutdownErrors) > 0 {
		return fmt.Errorf("shutdown completed with errors: %v", shutdownErrors)
	}

	app.status = common.ApplicationStatusStopped

	if app.logger != nil {
		app.logger.Info("application stopped",
			logger.String("name", app.name),
			logger.Duration("shutdown_time", time.Since(stopStart)),
			logger.Duration("uptime", time.Since(app.startTime)),
		)
	}

	// Record shutdown metrics
	if app.metrics != nil {
		app.metrics.Counter("forge.application.stopped").Inc()
		app.metrics.Histogram("forge.application.shutdown_time").Observe(time.Since(stopStart).Seconds())
		app.metrics.Histogram("forge.application.uptime").Observe(time.Since(app.startTime).Seconds())
	}

	return nil
}

func (app *ForgeApplication) Shutdown(ctx context.Context) error {
	// Create a context with timeout for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, app.stopTimeout)
	defer cancel()

	return app.Stop(shutdownCtx)
}

func (app *ForgeApplication) Run() error {
	// Create a context for the application
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the application
	if err := app.Start(ctx); err != nil {
		return err
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if app.logger != nil {
		app.logger.Info("application running",
			logger.String("name", app.name),
			logger.String("version", app.version),
			logger.String("status", string(app.status)),
		)
	}

	// Wait for termination signal
	sig := <-sigChan
	if app.logger != nil {
		app.logger.Info("received termination signal",
			logger.String("signal", sig.String()),
		)
	}

	// Cancel the context to signal shutdown
	cancel()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), app.stopTimeout)
	defer shutdownCancel()

	return app.Shutdown(shutdownCtx)
}

// =============================================================================
// HTTP SERVER MANAGEMENT
// =============================================================================

func (app *ForgeApplication) StartServer(addr string) error {
	app.serverMux.Lock()
	defer app.serverMux.Unlock()

	if app.server != nil {
		return common.ErrLifecycleError("start_server", fmt.Errorf("HTTP server already running"))
	}

	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router must be initialized before starting server"))
	}

	app.server = &http.Server{
		Addr:              addr,
		Handler:           app.router.Handler(),
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	if app.logger != nil {
		app.logger.Info("starting HTTP server",
			logger.String("addr", addr),
		)
	}

	go func() {
		if err := app.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if app.logger != nil {
				app.logger.Error("HTTP server error", logger.Error(err))
			}
		}
	}()

	if app.logger != nil {
		app.logger.Info("HTTP server started", logger.String("addr", addr))
	}

	if app.metrics != nil {
		app.metrics.Counter("forge.application.server_started").Inc()
	}

	return nil
}

func (app *ForgeApplication) StopServer(ctx context.Context) error {
	app.serverMux.Lock()
	defer app.serverMux.Unlock()

	return app.stopHTTPServer(ctx)
}

func (app *ForgeApplication) stopHTTPServer(ctx context.Context) error {
	if app.server == nil {
		return nil
	}

	if app.logger != nil {
		app.logger.Info("stopping HTTP server")
	}

	if err := app.server.Shutdown(ctx); err != nil {
		// Log the graceful shutdown failure but continue with force close
		if app.logger != nil {
			app.logger.Warn("failed to shutdown HTTP server gracefully, forcing close", logger.Error(err))
		}
		// Force close the server and return the original shutdown error
		if closeErr := app.server.Close(); closeErr != nil {
			return fmt.Errorf("failed to shutdown HTTP server gracefully: %w, and failed to force close: %v", err, closeErr)
		}
		return fmt.Errorf("failed to shutdown HTTP server gracefully: %w", err)
	}

	app.server = nil

	if app.logger != nil {
		app.logger.Info("HTTP server stopped")
	}

	if app.metrics != nil {
		app.metrics.Counter("forge.application.server_stopped").Inc()
	}

	return nil
}

func (app *ForgeApplication) GetServer() *http.Server {
	app.serverMux.RLock()
	defer app.serverMux.RUnlock()
	return app.server
}

// =============================================================================
// STATUS AND HEALTH
// =============================================================================

func (app *ForgeApplication) IsRunning() bool {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.status == common.ApplicationStatusRunning
}

func (app *ForgeApplication) GetStatus() common.ApplicationStatus {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.status
}

func (app *ForgeApplication) GetInfo() common.ApplicationInfo {
	app.mu.RLock()
	defer app.mu.RUnlock()

	var routeCount, middlewareCount int
	if app.router != nil {
		stats := app.router.GetStats()
		routeCount = stats.HandlersRegistered
		middlewareCount = stats.MiddlewareCount
	}

	return common.ApplicationInfo{
		Name:        app.name,
		Version:     app.version,
		Description: app.description,
		Status:      app.status,
		StartTime:   app.startTime,
		Uptime:      app.getUptime(),
		Services:    len(app.services),
		Controllers: len(app.controllers),
		Plugins:     len(app.pluginsManager.GetPlugins()),
		Routes:      routeCount,
		Middleware:  middlewareCount,
	}
}

func (app *ForgeApplication) getUptime() time.Duration {
	if app.status != common.ApplicationStatusRunning || app.startTime.IsZero() {
		return 0
	}
	return time.Since(app.startTime)
}

func (app *ForgeApplication) HealthCheck(ctx context.Context) error {
	if !app.IsRunning() {
		return common.ErrHealthCheckFailed("application", fmt.Errorf("application is not running, status: %s", app.status))
	}

	// Check container health
	if app.container != nil {
		if err := app.container.HealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed("container", err)
		}
	}

	// Check router health
	if app.router != nil {
		if err := app.router.HealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed("router", err)
		}
	}

	// Check services health through lifecycle manager
	if app.lifecycle != nil {
		if err := app.lifecycle.HealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed("services", err)
		}
	}

	return nil
}

// =============================================================================
// ROUTE REGISTRATION (Delegated to Router)
// =============================================================================

func (app *ForgeApplication) GET(path string, handler interface{}, options ...common.HandlerOption) error {
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}
	return app.router.GET(path, handler, options...)
}

func (app *ForgeApplication) POST(path string, handler interface{}, options ...common.HandlerOption) error {
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}
	return app.router.POST(path, handler, options...)
}

func (app *ForgeApplication) PUT(path string, handler interface{}, options ...common.HandlerOption) error {
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}
	return app.router.PUT(path, handler, options...)
}

func (app *ForgeApplication) DELETE(path string, handler interface{}, options ...common.HandlerOption) error {
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}
	return app.router.DELETE(path, handler, options...)
}

func (app *ForgeApplication) PATCH(path string, handler interface{}, options ...common.HandlerOption) error {
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}
	return app.router.PATCH(path, handler, options...)
}

func (app *ForgeApplication) RegisterHandler(method, path string, handler interface{}, options ...common.HandlerOption) error {
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}
	return app.router.RegisterOpinionatedHandler(method, path, handler, options...)
}

func (app *ForgeApplication) UseMiddleware(handler func(http.Handler) http.Handler) {
	if app.router != nil {
		app.router.UseMiddleware(handler)
	}
}

func (app *ForgeApplication) EnableOpenAPI(config common.OpenAPIConfig) {
	if app.router != nil {
		app.router.EnableOpenAPI(config)
	}
}

func (app *ForgeApplication) EnableAsyncAPI(config AsyncAPIConfig) {
	if app.router != nil {
		app.router.EnableAsyncAPI(config)
	}
}

func (app *ForgeApplication) DatabaseManager() database.DatabaseManager {
	return nil
}

func (app *ForgeApplication) EventBus() events.EventBus {
	return nil
}

func (app *ForgeApplication) MiddlewareManager() *middleware.Manager {
	return nil
}

func (app *ForgeApplication) StreamingManager() streaming.StreamingManager {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.streamingManager
}

// ensureStreamingManager creates a streaming manager if it doesn't exist
func (app *ForgeApplication) ensureStreamingManager() error {
	app.mu.Lock()
	defer app.mu.Unlock()

	if app.streamingManager != nil {
		return nil
	}

	err := streaming.RegisterStreamingService(app.container, nil)
	if err != nil {
		return err
	}

	// err := streaming.RegisterStreamingManager(app.container)
	// if err != nil {
	// 	return err
	// }

	if manager, err := app.container.ResolveNamed(common.StreamManagerKey); err == nil {
		if streamingMgr, ok := manager.(streaming.StreamingManager); ok {
			app.streamingManager = streamingMgr
		}
	}

	// Try to resolve streaming service and get manager from it
	if service, err := app.container.ResolveNamed(common.StreamingServiceKey); err == nil {
		if streamingService, ok := service.(streaming.StreamingService); ok {
			app.streamingManager = streamingService.GetManager()
		}
	}

	if app.logger != nil {
		app.logger.Info("streaming manager created",
			logger.String("type", "default"),
		)
	}

	return nil
}

func (app *ForgeApplication) MetricsCollector() metrics.MetricsCollector {
	if app.metrics == nil {
		// Return a no-op metrics collector if not initialized
		// This prevents panics but logs a warning
		if app.logger != nil {
			app.logger.Warn("metrics collector not initialized, returning no-op collector")
		}
		return metrics.NewMockMetricsCollector()
	}
	return app.metrics
}

func (app *ForgeApplication) HealthService() health.HealthService {
	if app.container == nil {
		// Return a no-op health service if container not initialized
		if app.logger != nil {
			app.logger.Warn("container not initialized, returning no-op health service")
		}
		return health.HealthService{} // Return empty health service
	}

	// Try to resolve health service from container
	if healthServ, err := app.container.ResolveNamed(common.HealthServiceKey); err == nil {
		if hs, ok := healthServ.(health.HealthService); ok {
			return hs
		}
	}

	// If health service not found, log warning and return empty service
	if app.logger != nil {
		app.logger.Warn("health service not found in container, returning empty health service")
	}
	return health.HealthService{}
}

func (app *ForgeApplication) WebSocket(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}

	// Get or create streaming manager
	if err := app.ensureStreamingManager(); err != nil {
		return err
	}

	// Enable streaming on router if not already enabled
	if forgeRouter, ok := app.router.(*router.ForgeRouter); ok {
		if err := forgeRouter.EnableStreaming(app.streamingManager); err != nil {
			// Ignore error if streaming is already enabled
			if !strings.Contains(err.Error(), "already enabled") {
				return err
			}
		}

		return forgeRouter.RegisterWebSocket(path, handler, options...)
	}

	return common.ErrValidationError("router", fmt.Errorf("router type does not support WebSocket"))

	// // Enable streaming on router if not already enabled
	// if forgeRouter, ok := app.router.(*router.ForgeRouter); ok {
	// 	if err := forgeRouter.EnableStreaming(app.streamingManager); err != nil {
	// 		// Ignore error if streaming is already enabled
	// 		if !strings.Contains(err.Error(), "already enabled") {
	// 			return err
	// 		}
	// 	}
	//
	// 	// Use a default WebSocket handler that accepts connections
	// 	defaultHandler := func(ctx common.Context, conn streaming.Connection) error {
	// 		if app.logger != nil {
	// 			app.logger.Info("WebSocket connection established",
	// 				logger.String("path", path),
	// 				logger.String("connection_id", conn.ID()),
	// 			)
	// 		}
	//
	// 		// Keep connection alive until closed
	// 		for conn.IsAlive() {
	// 			select {
	// 			case <-ctx.Done():
	// 				return ctx.Err()
	// 			case <-time.After(30 * time.Second):
	// 				// Send ping to keep connection alive
	// 				if err := conn.Send(ctx, &streaming.Message{
	// 					Type: "ping",
	// 					Data: "keep-alive",
	// 				}); err != nil {
	// 					return err
	// 				}
	// 			}
	// 		}
	// 		return nil
	// 	}
	//
	// 	return forgeRouter.RegisterWebSocket(path, defaultHandler, options...)
	// }
	//
	// return common.ErrValidationError("router", fmt.Errorf("router type does not support WebSocket"))
}

func (app *ForgeApplication) EventStream(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}

	// Get or create streaming manager
	if err := app.ensureStreamingManager(); err != nil {
		return err
	}

	// Enable streaming on router if not already enabled
	if forgeRouter, ok := app.router.(*router.ForgeRouter); ok {
		if err := forgeRouter.EnableStreaming(app.streamingManager); err != nil {
			// Ignore error if streaming is already enabled
			if !strings.Contains(err.Error(), "already enabled") {
				return err
			}
		}

		return forgeRouter.RegisterSSE(path, handler, options...)
	}

	return common.ErrValidationError("router", fmt.Errorf("router type does not support Server-Sent Events"))
}

func (app *ForgeApplication) EnableMetricsEndpoints() error {
	if app.sharedConfig.metrics == nil {
		return common.ErrValidationError("metrics", fmt.Errorf("metrics collector not initialized"))
	}
	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router not initialized"))
	}
	if app.metricsHandler != nil {
		return common.ErrValidationError("metrics_handler", fmt.Errorf("metrics handler already registered"))
	}

	app.metricsHandler = metrics.NewMetricsEndpointHandler(app.metrics, app.sharedConfig.metrics.EndpointConfig, app.logger)
	return app.metricsHandler.RegisterEndpoints(app.router)
}

func (app *ForgeApplication) EnableHealthEndpoints() error {
	return health.RegisterHealthEndpointsWithContainer(app.container, app.router)
}

// EnableCacheEndpoints enables cache management HTTP endpoints
func (app *ForgeApplication) EnableCacheEndpoints() error {
	if app.cacheManager == nil {
		return fmt.Errorf("cache manager not initialized")
	}
	if app.router == nil {
		return fmt.Errorf("router not initialized")
	}

	// Register cache management endpoints
	cacheEndpoints := cache.NewCacheEndpointHandler(app.cacheManager, app.logger, app.metrics)
	return cacheEndpoints.RegisterEndpoints(app.router)
}

func (app *ForgeApplication) GetStats() map[string]interface{} {
	// TODO implement me
	panic("implement me")
}

// =============================================================================
// INTERNAL HELPER METHODS
// =============================================================================

func (app *ForgeApplication) setupCoreComponents() error {
	if app.logger == nil {
		app.logger = logger.GetGlobalLogger()
	}

	if app.errorHandler == nil {
		app.errorHandler = NewDefaultErrorHandler(app.logger)
	}

	if app.metrics == nil {
		app.metrics = metrics.NewCollector(metrics.DefaultCollectorConfig(), app.logger)
	}

	if app.config == nil {
		app.config = config.NewManager(config.ManagerConfig{
			ErrorHandler:   app.errorHandler,
			Logger:         app.logger,
			Metrics:        app.metrics,
			CacheEnabled:   true,
			ReloadOnChange: true,
			MetricsEnabled: true,
		})
	}

	if app.healthCheck == nil {
		app.healthCheck = health.NewHealthChecker(health.DefaultHealthCheckerConfig(), app.logger, app.metrics, app.container)
	}

	if app.container == nil {
		app.container = di.NewContainer(di.ContainerConfig{
			Logger:       app.logger,
			Metrics:      app.metrics,
			Config:       app.config,
			ErrorHandler: NewDefaultErrorHandler(app.logger),
		})
	}

	if app.lifecycle == nil {
		app.lifecycle = app.container.LifecycleManager()
	}

	return nil
}

func (app *ForgeApplication) validateCoreComponents() error {
	if app.container == nil {
		return common.ErrValidationError("container", fmt.Errorf("DI container must be provided"))
	}
	if app.logger == nil {
		return common.ErrValidationError("logger", fmt.Errorf("logger must be provided"))
	}
	if app.metrics == nil {
		return common.ErrValidationError("metrics", fmt.Errorf("metrics collector must be provided"))
	}
	if app.config == nil {
		return common.ErrValidationError("config", fmt.Errorf("configuration manager must be provided"))
	}
	if app.healthCheck == nil {
		return common.ErrValidationError("health_check", fmt.Errorf("health checker must be provided"))
	}

	return nil
}

func (app *ForgeApplication) validateConfiguration() error {
	if err := app.validateCoreComponents(); err != nil {
		return err
	}

	if app.lifecycle == nil {
		return common.ErrValidationError("lifecycle", fmt.Errorf("lifecycle manager must be provided"))
	}

	if app.router == nil {
		return common.ErrValidationError("router", fmt.Errorf("router must be initialized"))
	}

	return nil
}

// registerServiceInContainer attempts to register a service in the DI container
// This is optional and will not fail if registration fails
func (app *ForgeApplication) registerServiceInContainer(service common.Service) error {
	// Try to register the service instance directly
	serviceName := service.Name()

	return app.container.Register(common.ServiceDefinition{
		Name:         serviceName,
		Type:         service, // Use service instance as type
		Constructor:  func() common.Service { return service },
		Singleton:    true,
		Dependencies: service.Dependencies(),
	})
}

func (app *ForgeApplication) syncServicesWithLifecycle() error {
	// Go through all registered services in container and add them to lifecycle if they implement Service
	for _, registration := range app.container.(*di.Container).NamedServices() {
		serviceInterface := reflect.TypeOf((*common.Service)(nil)).Elem()
		if registration.Type.Implements(serviceInterface) {
			instance, err := app.container.ResolveNamed(registration.Name)
			if err != nil {
				continue // Skip services that can't be resolved
			}

			if service, ok := instance.(common.Service); ok {
				// Check if already in lifecycle manager
				if _, err := app.lifecycle.GetService(service.Name()); err != nil {
					// Not in lifecycle manager, add it
					if err := app.lifecycle.AddService(service); err != nil {
						app.logger.Warn("failed to add service to lifecycle manager",
							logger.String("service", service.Name()),
							logger.Error(err),
						)
					} else {
						app.logger.Info("service synced to lifecycle manager",
							logger.String("service", service.Name()),
						)
					}
				}
			}
		}
	}
	return nil
}

// =============================================================================
// CACHE FEATURE ENABLER
// =============================================================================

// enableCacheFeature initializes cache support using the DI container
func (app *ForgeApplication) enableCacheFeature(config *cache.CacheConfig) error {
	if config == nil {
		return nil // Cache not configured
	}

	app.logger.Info("enabling cache feature",
		logger.Bool("enabled", config.Enabled),
		logger.String("default_cache", config.DefaultCache),
		logger.Int("backends", len(config.Backends)),
	)

	// Register cache service in DI container
	err := cache.RegisterCacheService(app.container)
	if err != nil {
		return fmt.Errorf("failed to register cache service: %w", err)
	}

	// Resolve cache service from container
	if service, err := app.container.ResolveNamed("cache-service"); err == nil {
		if cacheService, ok := service.(*cache.CacheService); ok {
			app.cacheService = cacheService
			app.cacheManager = cacheService.GetManager()

			app.logger.Info("cache service resolved successfully",
				logger.String("service", "cache-service"),
			)
		} else {
			return fmt.Errorf("resolved cache service is not of correct type")
		}
	} else {
		return fmt.Errorf("failed to resolve cache service: %w", err)
	}

	return nil
}

// =============================================================================
// CACHE ACCESSOR METHODS
// =============================================================================

// CacheManager returns the cache manager instance
func (app *ForgeApplication) CacheManager() cache.CacheManager {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.cacheManager
}

// CacheService returns the cache service instance
func (app *ForgeApplication) CacheService() *cache.CacheService {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.cacheService
}

// GetCache returns a cache instance by name, or the default cache if name is empty
func (app *ForgeApplication) GetCache(name string) (cache.Cache, error) {
	if app.cacheManager == nil {
		return nil, fmt.Errorf("cache manager not initialized")
	}
	return app.cacheManager.GetCache(name)
}

// GetDefaultCache returns the default cache instance
func (app *ForgeApplication) GetDefaultCache() (cache.Cache, error) {
	if app.cacheManager == nil {
		return nil, fmt.Errorf("cache manager not initialized")
	}
	return app.cacheManager.GetDefaultCache()
}
