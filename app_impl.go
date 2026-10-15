package forge

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	nethttppprof "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	confy "github.com/xraph/confy"
	"github.com/xraph/forge/errors"
	healthinternal "github.com/xraph/forge/internal/health"
	"github.com/xraph/forge/internal/logger"
	metricsinternal "github.com/xraph/forge/internal/metrics"
	"github.com/xraph/forge/internal/metrics/collectors"
	"github.com/xraph/forge/internal/metrics/exporters"
	"github.com/xraph/forge/internal/profiling"
	internalrouter "github.com/xraph/forge/internal/router"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/forge/internal/telemetry"
	"github.com/xraph/vessel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
)

// app implements the App interface.
type app struct {
	// Configuration
	config AppConfig

	// Core components
	container        Container
	router           Router
	configManager    ConfigManager
	logger           Logger
	metrics          Metrics
	healthManager    HealthManager
	lifecycleManager LifecycleManager
	tracing          *telemetry.Provider
	metricsPush      *telemetry.MetricsPusher // nil when metrics aren't pushed
	profiler         *profiling.Profiler

	// codeMetrics is the metrics config the caller passed, or nil when they
	// passed none and the defaults applied. Reloads resolve against it.
	codeMetrics *shared.MetricsConfig

	// HTTP server
	httpServer *http.Server

	// Extensions
	extensions []Extension

	// Lifecycle
	startTime time.Time
	mu        sync.RWMutex
	started   bool
	starting  bool
}

// newApp creates a new app instance.
func newApp(config AppConfig) *app {
	// Apply defaults
	if config.Name == "" {
		config.Name = "forge-app"
	}

	if config.Version == "" {
		config.Version = "1.0.0"
	}

	if config.Environment == "" {
		config.Environment = "development"
	}

	if config.HTTPAddress == "" {
		config.HTTPAddress = ":8080"
	}

	if config.HTTPTimeout == 0 {
		config.HTTPTimeout = 30 * time.Second
	}

	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 30 * time.Second
	}

	if len(config.ShutdownSignals) == 0 {
		config.ShutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
	}

	// Create DI container
	container := NewContainer()

	// Create logger if not provided. Format is chosen automatically: a
	// terminal gets human-readable output, anything else gets JSON, and
	// FORGE_LOG_FORMAT overrides both.
	logger := config.Logger
	if logger == nil {
		logger = NewLogger(LoggingConfig{
			Environment: config.Environment,
			Name:        config.Name,
		})
	}

	// Create error handler
	var errorHandler ErrorHandler
	if config.ErrorHandler == nil {
		errorHandler = shared.NewDefaultErrorHandler(logger)
	}

	// Create config manager if not provided (needed for metrics/health initialization)
	configManager := config.ConfigManager

	// Auto-discover and load config files if ConfigManager not provided and auto-discovery is enabled
	if configManager == nil && config.EnableConfigAutoDiscovery {
		autoConfig := confy.AutoDiscoveryConfig{
			AppName:          config.Name,
			SearchPaths:      config.ConfigSearchPaths,
			ConfigNames:      config.ConfigBaseNames,
			LocalConfigNames: config.ConfigLocalNames,
			EnableAppScoping: config.EnableAppScopedConfig,
			RequireBase:      false,
			RequireLocal:     false,
			MaxDepth:         5,
			Logger:           logger,
			// Environment variable source configuration
			EnableEnvSource:  config.EnableEnvConfig,
			EnvPrefix:        config.EnvPrefix,
			EnvSeparator:     config.EnvSeparator,
			EnvOverridesFile: config.EnvOverridesFile,
		}

		// Try to auto-discover and load configs
		if autoManager, result, err := confy.DiscoverAndLoadConfigs(autoConfig); err == nil {
			configManager = autoManager

			// Log discovery results
			if logger != nil {
				for _, p := range result.BaseConfigPaths {
					logger.Info("auto-discovered base config",
						F("path", p),
					)
				}

				for _, p := range result.LocalConfigPaths {
					logger.Info("auto-discovered local config",
						F("path", p),
					)
				}

				if result.IsMonorepo {
					logger.Info("detected monorepo layout",
						F("app", config.Name),
						F("app_scoped", config.EnableAppScopedConfig),
					)
				}
			}
		} else if logger != nil {
			// Auto-discovery failed, but that's okay - we'll create a default manager
			logger.Debug("config auto-discovery did not find files, using empty config",
				F("error", err.Error()),
			)
		}
	}

	// Explicit deployment overlays are required inputs, including when ordinary discovery found no files.
	if overlaySources, cleanup, err := configOverlaySources(logger); err != nil {
		panic("forge: " + err.Error())
	} else if len(overlaySources) > 0 {
		if configManager == nil {
			configManager = NewDefaultConfigManager(logger, nil, errorHandler)
		}

		loader, ok := configManager.(interface {
			LoadFrom(sources ...confy.ConfigSource) error
		})
		if !ok {
			panic("forge: config manager cannot load deployment overlays")
		}

		if err := loader.LoadFrom(overlaySources...); err != nil {
			cleanup()
			panic("forge: load config overlay: " + err.Error())
		}

		cleanup()
	}

	// Create metrics with full config support
	var metrics Metrics

	// Without a metrics config from code the defaults apply and the config
	// file may change any of them; with one, code values win.
	var codeMetrics *shared.MetricsConfig

	if metricsConfigFromCode(config) {
		code := config.MetricsConfig
		codeMetrics = &code
	}

	metricsConfig, _ := resolveMetricsConfig(codeMetrics, configManager, logger)
	config.MetricsConfig = *metricsConfig // Update config for banner display

	if metricsConfig.Enabled {
		metrics = metricsinternal.New(metricsConfig, logger)
	} else {
		metrics = metricsinternal.NewNoOpMetrics()
	}

	// Initialize config manager if still not provided (after auto-discovery attempt)
	if configManager == nil {
		configManager = NewDefaultConfigManager(logger, metrics, errorHandler)
		if logger != nil {
			logger.Debug("using default empty config manager")
		}
	}

	// Load .forge.yaml for app-level configuration (port, build settings, etc.)
	// This runs BEFORE server initialization to ensure port is configured
	config = loadForgeYAMLConfig(config, logger)

	// Tracing. Built before the router so its middleware is the outermost
	// global middleware, and before extensions register so they can trace.
	tracing := buildTracing(config, configManager, logger)

	// Metrics push. Built after tracing so it shares the tracer's Resource.
	metricsPush := buildMetricsExport(config, configManager, metrics, tracing, logger)

	// Profiling needs to know whether tracing is on, since span labels hang
	// off the server span. Nothing runs until Start.
	profiler := buildProfiling(config, configManager, logger, tracing.Settings().Enabled)

	// Create health manager with full config support
	var healthManager HealthManager

	healthConfig := &config.HealthConfig

	// Apply defaults if health config is empty (not explicitly disabled)
	if !healthConfig.Enabled && healthConfig.Intervals.Check == 0 {
		// User didn't provide config, use defaults
		defaultHealth := DefaultHealthConfig()
		defaultHealth.Intervals.Check = 30 * time.Second
		defaultHealth.Intervals.Report = 60 * time.Second
		defaultHealth.Features.AutoDiscovery = false
		defaultHealth.Performance.MaxConcurrentChecks = 10
		defaultHealth.Performance.DefaultTimeout = 5 * time.Second
		defaultHealth.Features.Aggregation = true
		defaultHealth.Performance.HistorySize = 100
		healthConfig = &defaultHealth
		config.HealthConfig = defaultHealth // Update config for banner display
	}

	// Try to load from ConfigManager first
	if configManager != nil {
		var runtimeConfig shared.HealthConfig
		if _, err := bindConfig(configManager, "health", &runtimeConfig, logger); err == nil {
			if runtimeConfig.Enabled {
				healthConfig = mergeHealthConfig(&runtimeConfig, healthConfig)
				config.HealthConfig = *healthConfig // Update config for banner display
			}
		}
	}

	if healthConfig.Enabled {
		// Pass nil container, will be set after container creation in Start()
		healthManager = healthinternal.New(healthConfig, logger, metrics, nil)
	} else {
		healthManager = healthinternal.NewNoOpHealthManager()
	}

	// Create router with options including observability
	routerOpts := config.RouterOptions
	if routerOpts == nil {
		routerOpts = []RouterOption{}
	}
	// Add observability options
	if config.MetricsConfig.Enabled {
		routerOpts = append(routerOpts, WithMetrics(config.MetricsConfig))
	}

	if config.HealthConfig.Enabled {
		routerOpts = append(routerOpts, WithHealth(config.HealthConfig))
	}

	// Add HTTP address for automatic localhost server in OpenAPI
	if config.HTTPAddress != "" {
		routerOpts = append(routerOpts, internalrouter.WithHTTPAddress(config.HTTPAddress))
	}

	// Internal error text is echoed to clients only outside production, where
	// it aids debugging; in production a 500 carries no detail. Logs always have
	// the full error either way.
	shared.SetExposeInternalErrors(config.Environment != "production")

	// Cap request bodies. Zero leaves the router's own default in place.
	if config.MaxRequestBodySize != 0 {
		routerOpts = append(routerOpts, internalrouter.WithMaxRequestBodySize(config.MaxRequestBodySize))
	}

	// Restrict WebSocket upgrades to the configured origins. When unset the
	// router enforces same-origin, which is the safe default.
	if len(config.WebSocketOrigins) > 0 {
		routerOpts = append(routerOpts, internalrouter.WithWebSocketOrigins(config.WebSocketOrigins...))
	}

	// Enable panic recovery to prevent unhandled panics from crashing the server
	routerOpts = append(routerOpts, internalrouter.WithRecovery())

	// Stream connections are recorded whenever metrics are on, whatever
	// features.http_metrics says. The router names a stream by its route,
	// which it reports through a route-match holder; see below for where the
	// holder comes from.
	if metricsConfig.Enabled {
		routerOpts = append(routerOpts, internalrouter.WithStreamObserver(collectors.NewStreamMetrics(metrics)))
	}

	router := NewRouter(routerOpts...)

	// Telemetry goes in the router's outer slot, ahead of every UseGlobal
	// middleware whenever that is registered, and runs as plain http.Handler
	// middleware with no forge Context of its own. See UseOuterMiddleware.
	if tracing.Settings().Enabled {
		tracingSettings := tracing.Settings()
		tracingSettings.SpanLabels = profiler.Settings().SpanLabels

		internalrouter.UseOuterMiddleware(router,
			telemetry.ServerMiddleware(tracing.TracerProvider(), tracingSettings))
	} else {
		// No spans, but an incoming traceparent still reaches HTTPClient calls.
		internalrouter.UseOuterMiddleware(router, telemetry.PropagationMiddleware())
	}

	// HTTP server metrics sit inside tracing, so exemplars see the span, and
	// outside every extension middleware, so rejected requests are counted.
	if metricsConfig.Enabled && metricsConfig.Features.HTTPMetrics {
		httpMetrics := collectors.NewHTTPServerMetrics(metrics, collectors.HTTPServerOptions{
			ExcludePaths: tracing.Settings().ExcludePaths,
			BodySizes:    metricsConfig.Features.HTTPBodySizes,
			// With tracing off, nothing outside this middleware installs a
			// route-match holder, so it must create a fresh one.
			FreshRouteMatch: !tracing.Settings().Enabled,
		})

		internalrouter.UseOuterMiddleware(router, httpMetrics.Middleware())
	}

	// The tracing and HTTP metrics middleware install the route-match holder
	// the stream observer needs. With both off, install a bare one, or no
	// stream would ever be recorded.
	if metricsConfig.Enabled && !metricsConfig.Features.HTTPMetrics && !tracing.Settings().Enabled {
		internalrouter.UseOuterMiddleware(router, telemetry.RouteMatchMiddleware(tracing.Settings().ExcludePaths))
	}

	// Register core services with DI - Both key-based (legacy) and type-based (new pattern)

	// Key-based registration (backward compatibility)
	// Register with both the full keys and simple keys for vessel compatibility
	_ = Provide(container, func() (Logger, error) {
		return logger, nil
	}, vessel.WithAliases(shared.LoggerKey))

	_ = Provide(container, func() (ConfigManager, error) {
		return configManager, nil
	}, vessel.WithAliases(shared.ConfigKey))

	_ = Provide(container, func() (Metrics, error) {
		return metrics, nil
	}, vessel.WithAliases(shared.MetricsKey))

	_ = Provide(container, func() (HealthManager, error) {
		return healthManager, nil
	}, vessel.WithAliases(shared.HealthManagerKey))

	_ = Provide(container, func() (Router, error) {
		return router, nil
	}, vessel.WithAliases(shared.RouterKey))

	_ = Provide(container, func() (trace.TracerProvider, error) {
		return tracing.TracerProvider(), nil
	})

	// Create lifecycle manager
	lm := NewLifecycleManager(logger)

	a := &app{
		config:           config,
		container:        container,
		router:           router,
		configManager:    configManager,
		logger:           logger,
		metrics:          metrics,
		healthManager:    healthManager,
		lifecycleManager: lm,
		tracing:          tracing,
		metricsPush:      metricsPush,
		profiler:         profiler,
		codeMetrics:      codeMetrics,
		extensions:       []Extension{}, // Initialize empty, will populate below
		startTime:        time.Now(),
	}

	// Register extensions from config
	// This ensures RunnableExtension hooks are registered properly
	for _, ext := range config.Extensions {
		if err := a.RegisterExtension(ext); err != nil {
			logger.Error("failed to register extension from config",
				F("extension", ext.Name()),
				F("error", err),
			)
			// Continue with other extensions
		}
	}

	// Start the debug server when compiled with -tags forge_debug.
	// initDebugServer is a no-op stub in release builds (zero overhead).
	initDebugServer(a)

	// Setup built-in endpoints
	if err := a.setupBuiltinEndpoints(); err != nil {
		logger.Error("failed to setup built-in endpoints", F("error", err))
		panic(fmt.Sprintf("failed to setup built-in endpoints: %v", err))
	}

	return a
}

// Container returns the DI container.
func (a *app) Container() Container {
	return a.container
}

// Router returns the router.
func (a *app) Router() Router {
	return a.router
}

// Config returns the config manager.
func (a *app) Config() ConfigManager {
	return a.configManager
}

// Logger returns the logger.
func (a *app) Logger() Logger {
	return a.logger
}

// Metrics returns the metrics collector.
func (a *app) Metrics() Metrics {
	return a.metrics
}

// HealthManager returns the health manager.
func (a *app) HealthManager() HealthManager {
	return a.healthManager
}

// LifecycleManager returns the lifecycle manager.
func (a *app) LifecycleManager() LifecycleManager {
	return a.lifecycleManager
}

// GetHTTPAddress returns the configured HTTP address
// This is a helper method for extensions that need to know the server address.
func (a *app) GetHTTPAddress() string {
	return a.config.HTTPAddress
}

// RegisterService registers a service with the DI container.
func (a *app) RegisterService(name string, factory Factory, opts ...RegisterOption) error {
	return a.container.Register(name, factory, opts...)
}

// RegisterController registers a controller with the router.
func (a *app) RegisterController(controller Controller) error {
	return a.router.RegisterController(controller)
}

// RegisterExtension registers an extension with the app.
func (a *app) RegisterExtension(ext Extension) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check for duplicate
	for _, existing := range a.extensions {
		if existing.Name() == ext.Name() {
			return fmt.Errorf("extension %s already registered", ext.Name())
		}
	}

	a.extensions = append(a.extensions, ext)

	// Auto-register lifecycle hooks for RunnableExtension implementations
	if runnableExt, ok := ext.(RunnableExtension); ok {
		a.logger.Debug("registering runnable extension hooks",
			F("extension", ext.Name()),
		)

		// Register Run() hook in PhaseAfterRun
		runOpts := DefaultLifecycleHookOptions("run-" + ext.Name())
		if err := a.lifecycleManager.RegisterHook(PhaseAfterRun, func(ctx context.Context, app App) error {
			return runnableExt.Run(ctx)
		}, runOpts); err != nil {
			return fmt.Errorf("failed to register run hook for %s: %w", ext.Name(), err)
		}

		// Register Shutdown() hook in PhaseBeforeStop
		shutdownOpts := DefaultLifecycleHookOptions("shutdown-" + ext.Name())
		if err := a.lifecycleManager.RegisterHook(PhaseBeforeStop, func(ctx context.Context, app App) error {
			return runnableExt.Shutdown(ctx)
		}, shutdownOpts); err != nil {
			return fmt.Errorf("failed to register shutdown hook for %s: %w", ext.Name(), err)
		}

		a.logger.Debug("runnable extension hooks registered",
			F("extension", ext.Name()),
		)
	}

	return nil
}

// RegisterHook registers a lifecycle hook.
func (a *app) RegisterHook(phase LifecyclePhase, hook LifecycleHook, opts LifecycleHookOptions) error {
	return a.lifecycleManager.RegisterHook(phase, hook, opts)
}

// RegisterHookFn is a convenience method to register a hook with default options.
func (a *app) RegisterHookFn(phase LifecyclePhase, name string, hook LifecycleHook) error {
	return a.lifecycleManager.RegisterHookFn(phase, name, hook)
}

// Extensions returns all registered extensions.
func (a *app) Extensions() []Extension {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Return a copy to prevent modification
	extensions := make([]Extension, len(a.extensions))
	copy(extensions, a.extensions)

	return extensions
}

// GetExtension returns an extension by name.
func (a *app) GetExtension(name string) (Extension, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, ext := range a.extensions {
		if ext.Name() == name {
			return ext, nil
		}
	}

	return nil, fmt.Errorf("extension %s not found", name)
}

// Name returns the application name.
func (a *app) Name() string {
	return a.config.Name
}

// Version returns the application version.
func (a *app) Version() string {
	return a.config.Version
}

// Environment returns the application environment.
func (a *app) Environment() string {
	return a.config.Environment
}

// MigrationsDisabled returns true if auto-migrations are disabled via config or .forge.yaml.
func (a *app) MigrationsDisabled() bool {
	return a.config.DisableMigrations
}

// SetMigrationsDisabled overrides the DisableMigrations config flag at runtime.
func (a *app) SetMigrationsDisabled(v bool) {
	a.config.DisableMigrations = v
}

// CentralMigrationsEnabled reports whether the single-pass migration lifecycle
// is enabled via config or .forge.yaml.
func (a *app) CentralMigrationsEnabled() bool {
	return a.config.CentralMigrations
}

// CentralMigrator resolves the CentralMigrator registered in the DI container.
// Returns nil, false when the container is nil or no CentralMigrator has been
// contributed (e.g. CentralMigrations is disabled or grove is not registered).
func (a *app) CentralMigrator() (CentralMigrator, bool) {
	if a.container == nil {
		return nil, false
	}

	if !vessel.HasType[CentralMigrator](a.container) {
		return nil, false
	}

	cm, err := vessel.Inject[CentralMigrator](a.container)
	if err != nil {
		return nil, false
	}

	return cm, true
}

// StartTime returns the application start time.
func (a *app) StartTime() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.startTime
}

// Uptime returns the application uptime.
func (a *app) Uptime() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return time.Since(a.startTime)
}

// Start starts the application.
func (a *app) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.started || a.starting {
		a.mu.Unlock()

		return errors.New("app already started")
	}

	a.starting = true
	a.mu.Unlock()

	a.logger.Info("starting application",
		F("name", a.config.Name),
		F("version", a.config.Version),
		F("environment", a.config.Environment),
		F("extensions", len(a.extensions)),
	)

	// Profile startup too. A Start that fails stops the profiler again, so it
	// doesn't outlive an app that never ran. WithoutCancel gives a cancelled
	// start context the same short, capped attempt at the final upload.
	a.startProfiler()

	defer func() {
		if !a.started {
			a.stopProfiler(context.WithoutCancel(ctx))
		}
	}()

	// Metrics push begins further down. A Start that returns an error stops
	// it again, even when an after-start hook fails once the app counts as
	// started, so no push outlives a failed Start.
	startOK := false

	defer func() {
		if !startOK {
			a.stopMetricsPush(context.WithoutCancel(ctx))
		}
	}()

	// Execute before start hooks
	if err := a.lifecycleManager.ExecuteHooks(ctx, PhaseBeforeStart, a); err != nil {
		return fmt.Errorf("before start hooks failed: %w", err)
	}

	// Build extension dependency graph for proper lifecycle ordering
	graph, extMap, order, err := a.buildExtensionGraph()
	if err != nil {
		return err
	}

	levels := extensionLevels(order, graph, extMap)

	if a.config.CentralMigrations {
		// Central-migrations path: split Register-all / migrate / Start-all.

		// Phase 1: Register ALL extensions in dependency order. Each
		// MigratableExtension contributes its migration groups to the shared
		// registry here, but runs nothing yet.
		if err := a.runExtensions(levels, extMap, func(ext Extension) error {
			a.logger.Info("registering extension",
				F("extension", ext.Name()),
				F("version", ext.Version()),
			)

			if err := ext.Register(a); err != nil {
				return fmt.Errorf("failed to register extension %s: %w", ext.Name(), err)
			}

			return nil
		}); err != nil {
			return err
		}

		// Phase 2: Run the single ordered migration pass before any Start, so
		// schema exists for extensions that seed/query during Start. The grove
		// MigrationRegistry registered a PhaseAfterRegister hook during Register.
		if err := a.lifecycleManager.ExecuteHooks(ctx, PhaseAfterRegister, a); err != nil {
			return fmt.Errorf("central migration phase failed: %w", err)
		}

		// Phase 3: Start ALL extensions in dependency order.
		if err := a.runExtensions(levels, extMap, func(ext Extension) error {
			a.logger.Info("starting extension",
				F("extension", ext.Name()),
			)

			if err := ext.Start(ctx); err != nil {
				return fmt.Errorf("failed to start extension %s: %w", ext.Name(), err)
			}

			a.logger.Info("extension ready",
				F("extension", ext.Name()),
			)

			return nil
		}); err != nil {
			return err
		}
	} else {
		// Default path: interleaved Register+Start.
		// This ensures dependencies are fully ready (Register + Start) before dependents begin.
		if err := a.runExtensions(levels, extMap, func(ext Extension) error {
			// Phase 1: Register extension's services
			a.logger.Info("registering extension",
				F("extension", ext.Name()),
				F("version", ext.Version()),
			)

			if err := ext.Register(a); err != nil {
				return fmt.Errorf("failed to register extension %s: %w", ext.Name(), err)
			}

			// Phase 2: Start the extension (services auto-start on Resolve)
			a.logger.Info("starting extension",
				F("extension", ext.Name()),
			)

			if err := ext.Start(ctx); err != nil {
				return fmt.Errorf("failed to start extension %s: %w", ext.Name(), err)
			}

			a.logger.Info("extension ready",
				F("extension", ext.Name()),
			)

			return nil
		}); err != nil {
			return err
		}

		// Execute after register hooks (all extensions now registered and started).
		// In the central path this hook already fired between Register-all and Start-all,
		// so we only fire it here in the default (non-central) path to prevent double-fire.
		if err := a.lifecycleManager.ExecuteHooks(ctx, PhaseAfterRegister, a); err != nil {
			return fmt.Errorf("after register hooks failed: %w", err)
		}
	}

	// Apply global middleware from extensions
	a.logger.Debug("applying extension middlewares")

	if err := a.applyExtensionMiddlewares(); err != nil {
		return fmt.Errorf("failed to apply extension middlewares: %w", err)
	}

	a.logger.Debug("extension middlewares applied")

	// Start DI container (idempotent - skips already-started services)
	a.logger.Debug("finalizing DI container")

	if err := a.container.Start(ctx); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	a.logger.Debug("DI container finalized")

	// Start health manager explicitly after container is ready so that
	// auto-discovery can see all registered services. The health manager
	// is NOT started by container.Start() because it is registered lazily
	// (no WithEager), so we must start it here with the container set.
	if healthMgr, ok := a.healthManager.(*healthinternal.ManagerImpl); ok {
		healthMgr.SetContainer(a.container)

		if !healthMgr.IsStarted() {
			if err := healthMgr.Start(ctx); err != nil {
				return fmt.Errorf("failed to start health manager: %w", err)
			}
		}
	}

	// The metrics collector is lazy too, so container.Start only starts it
	// when something resolved it first. Start it here; until it runs, the
	// built-in system collector is never registered and nothing feeds the
	// time-series storage.
	if svc, ok := a.metrics.(startableService); ok && !svc.IsStarted() {
		if err := svc.Start(ctx); err != nil {
			return fmt.Errorf("failed to start metrics collector: %w", err)
		}
	}

	// Push only once the collector runs, so the first push carries its series.
	a.startMetricsPush(ctx)

	// Reload configs from ConfigManager (hot-reload support)
	a.logger.Debug("reloading configs from ConfigManager")

	if err := a.reloadConfigsFromManager(); err != nil {
		a.logger.Warn("failed to reload configs from ConfigManager", F("error", err))
	}

	a.logger.Debug("configs reloaded")

	// 4. Setup observability endpoints (including extension health checks)
	if err := a.setupObservabilityEndpoints(); err != nil {
		return fmt.Errorf("failed to setup observability endpoints: %w", err)
	}

	a.registerExtensionHealthChecks()

	// Note: Health manager is already started by container.Start()
	// No need to start it again here

	a.started = true
	a.startTime = time.Now()

	// Execute after start hooks
	if err := a.lifecycleManager.ExecuteHooks(ctx, PhaseAfterStart, a); err != nil {
		return fmt.Errorf("after start hooks failed: %w", err)
	}

	a.logger.Info("application started successfully")

	startOK = true

	return nil
}

// startMetricsPush starts pushing metrics when push is configured. Like
// tracing, it never fails Start: an exporter that can't be built is logged
// and the app runs without pushing.
func (a *app) startMetricsPush(ctx context.Context) {
	if a.metricsPush == nil {
		return
	}

	if err := a.metricsPush.Start(ctx); err != nil {
		a.logger.Error("telemetry: metrics push disabled", F("error", err))

		return
	}

	s := a.metricsPush.Settings()
	a.logger.Info("telemetry: pushing metrics over OTLP",
		F("endpoint", s.Endpoint),
		F("interval", s.Interval.String()),
		F("temporality", s.Temporality))
}

// stopMetricsPush pushes one last time and stops pushing. It does nothing when
// push isn't running.
func (a *app) stopMetricsPush(ctx context.Context) {
	if err := a.metricsPush.Shutdown(ctx); err != nil {
		a.logger.Warn("metrics push shutdown did not finish", F("error", err))
	}
}

// startableService is a core service the app starts and stops itself, because
// it is registered lazily and the container may never have started it.
type startableService interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsStarted() bool
}

// Stop stops the application.
func (a *app) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.started {
		return nil
	}

	a.logger.Info("stopping application")

	// Execute before stop hooks
	if err := a.lifecycleManager.ExecuteHooks(ctx, PhaseBeforeStop, a); err != nil {
		a.logger.Warn("before stop hooks failed", F("error", err))
	}

	// 1. Stop health manager
	if healthMgr, ok := a.healthManager.(*healthinternal.ManagerImpl); ok {
		if err := healthMgr.Stop(ctx); err != nil {
			a.logger.Error("failed to stop health manager", F("error", err))
		}
	}

	// 2. Stop extensions in reverse order
	a.stopExtensions(ctx)

	// 3. Push metrics one last time and stop pushing, while the collector
	// still holds them: its Stop resets the registry. The drain and the
	// extension stops are counted.
	a.stopMetricsPush(ctx)

	// 4. Stop DI container (stops all services in reverse order)
	if err := a.container.Stop(ctx); err != nil {
		a.logger.Error("failed to stop container", F("error", err))
	}

	// The container stops the metrics collector only when it started it, so
	// stop whatever Start started here.
	if svc, ok := a.metrics.(startableService); ok && svc.IsStarted() {
		if err := svc.Stop(ctx); err != nil {
			a.logger.Error("failed to stop metrics collector", F("error", err))
		}
	}

	// 5. Flush spans last, so the drain and the extension stops are exported.
	if a.tracing != nil {
		if err := a.tracing.Shutdown(ctx); err != nil {
			a.logger.Warn("tracing shutdown did not finish", F("error", err))
		}
	}

	// 6. Stop profiling last, so shutdown shows up in the final profiles.
	a.stopProfiler(ctx)

	a.started = false

	// Execute after stop hooks
	if err := a.lifecycleManager.ExecuteHooks(ctx, PhaseAfterStop, a); err != nil {
		a.logger.Warn("after stop hooks failed", F("error", err))
	}

	a.logger.Info("application stopped")

	return nil
}

// Run starts the HTTP server and blocks until a shutdown signal is received.
func (a *app) Run() error {
	if os.Getenv("FORGE_INTROSPECT") == "1" {
		return WriteInfraRequirements(a, os.Stdout)
	}

	// Start the application
	if err := a.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}

	// Create HTTP server.
	// WriteTimeout is set to 0 (disabled) because SSE, WebSocket, and gRPC
	// streaming handlers need indefinite write durations. A server-wide
	// WriteTimeout kills ALL long-lived connections after the deadline,
	// making streaming impossible. Regular REST handlers are protected by
	// per-route timeouts applied at the router level instead.
	a.httpServer = &http.Server{
		Addr:              a.config.HTTPAddress,
		Handler:           a.router,
		ReadTimeout:       a.config.HTTPTimeout,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // disabled — streaming requires indefinite writes
		IdleTimeout:       a.config.HTTPTimeout * 2,
	}

	// Print startup banner
	a.printStartupBanner()

	// Execute before run hooks (before HTTP server starts)
	if err := a.lifecycleManager.ExecuteHooks(context.Background(), PhaseBeforeRun, a); err != nil {
		return fmt.Errorf("before run hooks failed: %w", err)
	}

	// Channel for shutdown signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, a.config.ShutdownSignals...)

	// Channel for server errors
	errChan := make(chan error, 1)

	// Start HTTP server in goroutine
	go func() {
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Execute after run hooks (after HTTP server starts, non-blocking)
	// Run in background to not block main loop
	go func() {
		if err := a.lifecycleManager.ExecuteHooks(context.Background(), PhaseAfterRun, a); err != nil {
			a.logger.Warn("after run hooks failed", F("error", err))
		}
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		return fmt.Errorf("http server error: %w", err)
	case sig := <-shutdown:
		a.logger.Info("shutdown signal received", F("signal", sig.String()))
	}

	// Graceful shutdown
	return a.gracefulShutdown()
}

// gracefulShutdown performs a graceful shutdown.
func (a *app) gracefulShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), a.config.ShutdownTimeout)
	defer cancel()

	a.logger.Info("starting graceful shutdown", F("timeout", a.config.ShutdownTimeout))

	// 1. End open streams, so they don't hold Shutdown for its whole timeout.
	// Shutdown waits for every active request and an event stream never
	// returns on its own; it does not track hijacked WebSocket connections at
	// all. This runs alongside Shutdown, which still lets plain requests
	// finish.
	drained := make(chan error, 1)

	if d, ok := a.router.(interface {
		DrainStreams(ctx context.Context) error
	}); ok {
		go func() { drained <- d.DrainStreams(ctx) }()
	} else {
		drained <- nil
	}

	// 2. Stop accepting new requests and wait for active requests to complete
	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(ctx); err != nil {
			a.logger.Error("http server shutdown error", F("error", err))
		}
	}

	if err := <-drained; err != nil {
		a.logger.Warn("streams still open at the end of the shutdown timeout", F("error", err))
	}

	// 3. Stop the application (which stops all services)
	if err := a.Stop(ctx); err != nil {
		a.logger.Error("app shutdown error", F("error", err))
	}

	a.logger.Info("graceful shutdown complete")

	return nil
}

// printStartupBanner prints a styled startup banner with app info and endpoints.
func (a *app) printStartupBanner() {
	bannerCfg := shared.BannerConfig{
		AppName:     a.config.Name,
		Version:     a.config.Version,
		Environment: a.config.Environment,
		HTTPAddress: a.config.HTTPAddress,
		StartTime:   a.startTime,
	}

	// Add OpenAPI paths if enabled
	if spec := a.router.OpenAPISpec(); spec != nil {
		// Default paths for OpenAPI (standard Forge defaults)
		bannerCfg.OpenAPISpec = "/openapi.json"
		bannerCfg.OpenAPIUI = "/swagger"
	}

	// Add AsyncAPI path if enabled
	if spec := a.router.AsyncAPISpec(); spec != nil {
		// Default path for AsyncAPI UI
		bannerCfg.AsyncAPIUI = "/asyncapi"
	}

	// Add observability endpoints
	if a.config.HealthConfig.Enabled {
		bannerCfg.HealthPath = "/_/health"
	}

	if a.config.MetricsConfig.Enabled {
		bannerCfg.MetricsPath = "/_/metrics"
	}

	if a.config.EnablePprof {
		prefix := a.config.PprofPrefix
		if prefix == "" {
			prefix = "/_/debug/pprof"
		}

		bannerCfg.PprofPath = prefix
	}

	// Print the banner
	shared.PrintStartupBanner(bannerCfg)
}

// setupBuiltinEndpoints sets up built-in endpoints.
func (a *app) setupBuiltinEndpoints() error {
	// Info endpoint
	if err := a.router.GET("/_/info", a.handleInfo); err != nil {
		return fmt.Errorf("failed to register info endpoint: %w", err)
	}

	return nil
}

// setupObservabilityEndpoints sets up observability endpoints.
func (a *app) setupObservabilityEndpoints() error {
	// Setup metrics endpoint if enabled
	if a.config.MetricsConfig.Enabled {
		if err := a.router.GET("/_/metrics", a.handleMetrics); err != nil {
			return fmt.Errorf("failed to register metrics endpoint: %w", err)
		}
	}

	// Setup health endpoints if enabled
	if a.config.HealthConfig.Enabled {
		if err := a.router.GET("/_/health", a.handleHealth); err != nil {
			return fmt.Errorf("failed to register health endpoint: %w", err)
		}

		if err := a.router.GET("/_/health/live", a.handleHealthLive); err != nil {
			return fmt.Errorf("failed to register live health endpoint: %w", err)
		}

		if err := a.router.GET("/_/health/ready", a.handleHealthReady); err != nil {
			return fmt.Errorf("failed to register ready health endpoint: %w", err)
		}
	}

	// Setup pprof profiling endpoints if enabled
	if a.config.EnablePprof {
		if err := a.setupPprofEndpoints(); err != nil {
			return fmt.Errorf("failed to register pprof endpoints: %w", err)
		}
	}

	return nil
}

// setupPprofEndpoints registers net/http/pprof handlers on the router
// and a styled index page for browsing profiles.
func (a *app) setupPprofEndpoints() error {
	prefix := a.config.PprofPrefix
	if prefix == "" {
		prefix = "/_/debug/pprof"
	}

	prefix = strings.TrimRight(prefix, "/")

	// Dispatch table for specific pprof handlers.
	specific := map[string]http.HandlerFunc{
		"/cmdline": nethttppprof.Cmdline,
		"/profile": nethttppprof.Profile,
		"/symbol":  nethttppprof.Symbol,
		"/trace":   nethttppprof.Trace,
	}

	// Single wildcard route handles everything: index page, specific
	// handlers, and named profiles (heap, goroutine, etc.).
	if err := a.router.Handle(prefix+"/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authorize before doing anything else. 404 rather than 403 so the
		// endpoints are not discoverable by an unauthorized caller.
		if a.config.PprofGuard != nil && !a.config.PprofGuard(r) {
			http.NotFound(w, r)

			return
		}

		// Strip prefix to get the sub-path (e.g. "/heap", "/profile", "").
		sub := strings.TrimPrefix(r.URL.Path, prefix)

		// Index page: empty path or just "/".
		if sub == "" || sub == "/" {
			pprofIndexPage(w, r, prefix)

			return
		}

		// Specific handlers (cmdline, profile, symbol, trace).
		if h, ok := specific[sub]; ok {
			h.ServeHTTP(w, r)

			return
		}

		// Everything else: named profiles via pprof.Index (heap, goroutine, allocs, etc.).
		// pprof.Index expects the profile name at the end of the URL path after "/debug/pprof/".
		r.URL.Path = "/debug/pprof" + sub
		nethttppprof.Index(w, r)
	})); err != nil {
		return err
	}

	if a.config.PprofGuard == nil {
		a.logger.Warn("pprof endpoints enabled without PprofGuard: heap, goroutine and cmdline data are unauthenticated",
			F("prefix", prefix),
			F("environment", a.config.Environment),
		)
	}

	a.logger.Info("pprof profiling endpoints enabled",
		F("prefix", prefix),
		F("guarded", a.config.PprofGuard != nil),
	)

	return nil
}

// handleInfo handles the /_/info endpoint.
func (a *app) handleInfo(ctx Context) error {
	// Get service names from container
	services := a.container.Services()

	// Get route count
	routes := a.router.Routes()

	// Get extension info
	extensionInfo := make([]ExtensionInfo, 0, len(a.extensions))
	for _, ext := range a.extensions {
		status := "stopped"

		if baseExt, ok := ext.(*BaseExtension); ok {
			if baseExt.IsStarted() {
				status = "started"
			}
		} else {
			// If not BaseExtension, assume started if we're past app start
			if a.started {
				status = "started"
			}
		}

		extensionInfo = append(extensionInfo, ExtensionInfo{
			Name:         ext.Name(),
			Version:      ext.Version(),
			Description:  ext.Description(),
			Dependencies: ext.Dependencies(),
			Status:       status,
		})
	}

	info := AppInfo{
		Name:        a.config.Name,
		Version:     a.config.Version,
		Description: a.config.Description,
		Environment: a.config.Environment,
		StartTime:   a.StartTime(),
		Uptime:      a.Uptime(),
		GoVersion:   runtime.Version(),
		Services:    services,
		Routes:      len(routes),
		Extensions:  extensionInfo,
	}

	return ctx.JSON(200, info)
}

// buildExtensionGraph builds a dependency graph from registered extensions.
// Returns the graph, extension map for lookup, and the topologically sorted order.
func (a *app) buildExtensionGraph() (*vessel.DependencyGraph, map[string]Extension, []string, error) {
	graph := vessel.NewDependencyGraph()
	extMap := make(map[string]Extension)

	for _, ext := range a.extensions {
		// Check if extension uses the new DepsSpec() interface first
		if specExt, ok := ext.(DependencySpecExtension); ok {
			deps := specExt.DepsSpec()
			graph.AddNodeWithDeps(ext.Name(), deps)
		} else {
			// Fall back to legacy Dependencies() []string (treated as eager deps)
			deps := ext.Dependencies()
			graph.AddNode(ext.Name(), deps)
		}

		extMap[ext.Name()] = ext
	}

	order, err := graph.TopologicalSort()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("circular extension dependency detected: %w", err)
	}

	return graph, extMap, order, nil
}

// extensionLevels groups the topological order into dependency levels.
//
// Everything in a level depends only on extensions in earlier levels, so a
// level can be processed as a unit: nothing in it can need anything else in it.
// Level 0 is the extensions with no registered dependencies.
//
// A declared dependency that is not itself a registered extension is skipped,
// matching how the ordering loops treat one: dependencies are allowed to be
// optional.
func extensionLevels(order []string, graph *vessel.DependencyGraph, extMap map[string]Extension) [][]string {
	depth := make(map[string]int, len(order))

	var levels [][]string

	// order is topological, so every dependency has its depth by the time its
	// dependents are reached.
	for _, name := range order {
		if _, ok := extMap[name]; !ok {
			continue
		}

		level := 0

		for _, dep := range graph.GetDependencies(name) {
			if _, ok := extMap[dep]; !ok {
				continue // optional dependency, not registered
			}

			if d := depth[dep] + 1; d > level {
				level = d
			}
		}

		depth[name] = level

		for len(levels) <= level {
			levels = append(levels, nil)
		}

		levels[level] = append(levels[level], name)
	}

	return levels
}

// runExtensions applies fn to every extension, level by level.
//
// Levels always run in order, so an extension still sees its dependencies fully
// processed before its own turn. Within a level the extensions are independent
// of each other by construction, so they run concurrently when
// ParallelExtensionStartup is on and one after another otherwise.
func (a *app) runExtensions(levels [][]string, extMap map[string]Extension, fn func(Extension) error) error {
	for _, level := range levels {
		if !a.config.ParallelExtensionStartup || len(level) == 1 {
			for _, name := range level {
				if err := fn(extMap[name]); err != nil {
					return err
				}
			}

			continue
		}

		errs := make([]error, len(level))

		var wg sync.WaitGroup

		for i, name := range level {
			wg.Add(1)

			go func(i int, ext Extension) {
				defer func() {
					// An extension's Register or Start is arbitrary code. In
					// the serial path a panic unwinds through Start to the
					// caller; here it would kill the process from a goroutine
					// instead, so it is turned back into a startup error.
					if r := recover(); r != nil {
						errs[i] = fmt.Errorf("extension %s panicked during startup: %v", ext.Name(), r)
					}

					wg.Done()
				}()

				errs[i] = fn(ext)
			}(i, extMap[name])
		}

		wg.Wait()

		if err := errors.Join(errs...); err != nil {
			return err
		}
	}

	return nil
}

// stopExtensions stops all extensions in reverse dependency order.
func (a *app) stopExtensions(ctx context.Context) {
	// Build dependency graph to get proper order
	_, extMap, order, err := a.buildExtensionGraph()
	if err != nil {
		// Fallback to reverse add order if graph fails
		a.logger.Warn("failed to build extension graph for stop, using reverse add order",
			F("error", err),
		)

		for _, v := range slices.Backward(a.extensions) {
			ext := v
			a.logger.Info("stopping extension", F("extension", ext.Name()))

			if err := ext.Stop(ctx); err != nil {
				a.logger.Error("failed to stop extension",
					F("extension", ext.Name()),
					F("error", err),
				)
			}
		}

		return
	}

	// Stop in reverse dependency order
	for _, v := range slices.Backward(order) {
		name := v

		ext, ok := extMap[name]
		if !ok {
			continue
		}

		a.logger.Info("stopping extension",
			F("extension", ext.Name()),
		)

		if err := ext.Stop(ctx); err != nil {
			a.logger.Error("failed to stop extension",
				F("extension", ext.Name()),
				F("error", err),
			)
		}
	}
}

// applyExtensionMiddlewares applies global middlewares from extensions that implement MiddlewareExtension.
func (a *app) applyExtensionMiddlewares() error {
	middlewareCount := 0

	for _, ext := range a.extensions {
		// Check if extension implements MiddlewareExtension
		if mwExt, ok := ext.(MiddlewareExtension); ok {
			middlewares := mwExt.Middlewares()

			// A nil entry would panic while the global chain is composed.
			// Refusing it here names the extension that supplied it.
			for i, mw := range middlewares {
				if mw == nil {
					return fmt.Errorf("extension %s: middleware %d of %d is nil; make sure it is built before Middlewares returns it",
						ext.Name(), i+1, len(middlewares))
				}
			}

			if len(middlewares) > 0 {
				a.logger.Info("applying middlewares from extension",
					F("extension", ext.Name()),
					F("count", len(middlewares)),
				)

				// Apply all middlewares from this extension globally
				// Extensions use UseGlobal to ensure their middleware applies to ALL routes
				a.router.UseGlobal(middlewares...)
				middlewareCount += len(middlewares)

				a.logger.Debug("middlewares applied",
					F("extension", ext.Name()),
				)
			}
		}
	}

	if middlewareCount > 0 {
		a.logger.Info("extension middlewares applied",
			F("total_middlewares", middlewareCount),
		)
	}

	return nil
}

// registerExtensionHealthChecks registers health checks for all extensions.
func (a *app) registerExtensionHealthChecks() {
	for _, ext := range a.extensions {
		// Create local copies for closure capture (avoid loop variable capture bug)
		extRef := ext
		extName := ext.Name()

		checkName := "extension:" + extName
		if err := a.healthManager.RegisterFn(checkName, func(ctx context.Context) *HealthResult {
			if err := extRef.Health(ctx); err != nil {
				return &HealthResult{
					Status:  HealthStatusUnhealthy,
					Message: extName + " extension unhealthy",
					Details: map[string]any{"error": err.Error()},
				}
			}

			return &HealthResult{
				Status:  HealthStatusHealthy,
				Message: extName + " extension healthy",
			}
		}); err != nil {
			a.logger.Warn("failed to register health check for extension", F("extension", extName), F("error", err))
		}
	}
}

// NewDefaultConfigManager creates a default config manager (stub for now).
func NewDefaultConfigManager(
	l logger.Logger,
	m Metrics,
	e ErrorHandler,
) ConfigManager {
	if e == nil {
		e = shared.NewDefaultErrorHandler(l)
	}

	return confy.New(confy.WithLogger(l), confy.WithMetrics(m), confy.WithErrorHandler(e))
}

// defaultConfigManager is a stub config manager.
type defaultConfigManager struct{}

func (c *defaultConfigManager) Get(key string) (any, error) {
	return nil, fmt.Errorf("config not found: %s", key)
}
func (c *defaultConfigManager) GetString(key string) (string, error) {
	return "", fmt.Errorf("config not found: %s", key)
}
func (c *defaultConfigManager) GetInt(key string) (int, error) {
	return 0, fmt.Errorf("config not found: %s", key)
}
func (c *defaultConfigManager) GetBool(key string) (bool, error) {
	return false, fmt.Errorf("config not found: %s", key)
}
func (c *defaultConfigManager) Set(key string, value any) error {
	return nil
}
func (c *defaultConfigManager) Bind(key string, target any) error {
	return fmt.Errorf("config not found: %s", key)
}

// ConfigManager is now properly exported from config.go

// =============================================================================
// CONFIG MERGE HELPERS
// =============================================================================

// metricsConfigFromCode reports whether the caller chose a metrics config.
// An empty one (not enabled, no namespace) never counts. Otherwise a
// WithAppMetricsConfig call counts, and so does any config that differs from
// DefaultMetricsConfig, which is what New and DefaultAppConfig start from.
func metricsConfigFromCode(config AppConfig) bool {
	m := config.MetricsConfig
	if !m.Enabled && m.Collection.Namespace == "" {
		return false
	}

	return config.metricsConfigSet || !reflect.DeepEqual(m, DefaultMetricsConfig())
}

// resolveMetricsConfig returns the metrics config the app runs with, and
// whether the config file contributed to it. code is the config the caller
// passed, or nil when they passed none.
//
// With no code config the defaults apply and every key present in the file's
// metrics block replaces its default, so the file can turn a feature off. With
// a code config the file is read only when it says enabled: true, and code
// values win (see mergeMetricsConfig).
//
// A file value that doesn't convert to its field's type is logged to log and
// treated as absent, so the field keeps its default (or the code value).
func resolveMetricsConfig(code *shared.MetricsConfig, cm ConfigManager, log Logger) (*shared.MetricsConfig, bool) {
	if code == nil {
		defaults := DefaultMetricsConfig()

		if cm == nil || !cm.HasKey("metrics") {
			return &defaults, false
		}

		var file shared.MetricsConfig

		bad, err := bindConfig(cm, "metrics", &file, log)
		if err != nil {
			return &defaults, false
		}

		// A key whose value didn't convert counts as absent, so it keeps its default.
		present := func(key string) bool { return cm.HasKey(key) && !bad[key] }

		return overlayMetricsFile(&defaults, &file, present), true
	}

	if cm == nil {
		return code, false
	}

	var file shared.MetricsConfig
	if _, err := bindConfig(cm, "metrics", &file, log); err != nil || !file.Enabled {
		return code, false
	}

	return mergeMetricsConfig(&file, code), true
}

// overlayMetricsFile copies onto base each field whose key is present under
// metrics in the config file. Present means the key exists, so false and zero
// values in the file win. Map fields merge, with file entries winning.
func overlayMetricsFile(base, file *shared.MetricsConfig, hasKey func(string) bool) *shared.MetricsConfig {
	result := *base

	has := func(key string) bool { return hasKey("metrics." + key) }

	if has("enabled") {
		result.Enabled = file.Enabled
	}

	if has("features.system_metrics") {
		result.Features.SystemMetrics = file.Features.SystemMetrics
	}

	if has("features.runtime_metrics") {
		result.Features.RuntimeMetrics = file.Features.RuntimeMetrics
	}

	if has("features.http_metrics") {
		result.Features.HTTPMetrics = file.Features.HTTPMetrics
	}

	if has("features.http_body_sizes") {
		result.Features.HTTPBodySizes = file.Features.HTTPBodySizes
	}

	if has("collection.interval") {
		result.Collection.Interval = file.Collection.Interval
	}

	if has("collection.namespace") {
		result.Collection.Namespace = file.Collection.Namespace
	}

	if has("collection.path") {
		result.Collection.Path = file.Collection.Path
	}

	if has("limits.max_metrics") {
		result.Limits.MaxMetrics = file.Limits.MaxMetrics
	}

	if has("limits.buffer_size") {
		result.Limits.BufferSize = file.Limits.BufferSize
	}

	if has("limits.max_series_per_metric") {
		result.Limits.MaxSeriesPerMetric = file.Limits.MaxSeriesPerMetric
	}

	if has("storage") {
		result.Storage = file.Storage
	}

	result.Collection.DefaultTags = make(map[string]string, len(base.Collection.DefaultTags)+len(file.Collection.DefaultTags))
	maps.Copy(result.Collection.DefaultTags, base.Collection.DefaultTags)
	maps.Copy(result.Collection.DefaultTags, file.Collection.DefaultTags)

	result.Exporters = make(map[string]shared.MetricsExporterConfig[map[string]any], len(base.Exporters)+len(file.Exporters))
	maps.Copy(result.Exporters, base.Exporters)
	maps.Copy(result.Exporters, file.Exporters)

	return &result
}

// mergeMetricsConfig merges the file config (runtime) with a config the
// caller passed in code (programmatic). Programmatic non-zero values and true
// booleans take precedence, so the file can't turn off what code turned on.
func mergeMetricsConfig(runtime, programmatic *shared.MetricsConfig) *shared.MetricsConfig {
	result := *runtime // Start with runtime

	// Override with programmatic non-zero values
	if programmatic.Collection.Namespace != "" {
		result.Collection.Namespace = programmatic.Collection.Namespace
	}

	if programmatic.Collection.Path != "" {
		result.Collection.Path = programmatic.Collection.Path
	}

	if programmatic.Collection.Interval > 0 {
		result.Collection.Interval = programmatic.Collection.Interval
	}

	if programmatic.Limits.MaxMetrics > 0 {
		result.Limits.MaxMetrics = programmatic.Limits.MaxMetrics
	}

	if programmatic.Limits.BufferSize > 0 {
		result.Limits.BufferSize = programmatic.Limits.BufferSize
	}

	if programmatic.Limits.MaxSeriesPerMetric > 0 {
		result.Limits.MaxSeriesPerMetric = programmatic.Limits.MaxSeriesPerMetric
	}

	// Boolean fields (prefer programmatic if set explicitly in config)
	// For booleans, we can't distinguish zero value from explicit false,
	// so runtime takes precedence unless we have explicit true
	if programmatic.Features.SystemMetrics {
		result.Features.SystemMetrics = true
	}

	if programmatic.Features.RuntimeMetrics {
		result.Features.RuntimeMetrics = true
	}

	if programmatic.Features.HTTPMetrics {
		result.Features.HTTPMetrics = true
	}

	if programmatic.Features.HTTPBodySizes {
		result.Features.HTTPBodySizes = true
	}

	if programmatic.Storage != nil {
		result.Storage = programmatic.Storage
	}

	// Merge maps (programmatic values override)
	if len(programmatic.Collection.DefaultTags) > 0 {
		if result.Collection.DefaultTags == nil {
			result.Collection.DefaultTags = make(map[string]string)
		}

		maps.Copy(result.Collection.DefaultTags, programmatic.Collection.DefaultTags)
	}

	if len(programmatic.Exporters) > 0 {
		if result.Exporters == nil {
			result.Exporters = make(map[string]shared.MetricsExporterConfig[map[string]any])
		}

		maps.Copy(result.Exporters, programmatic.Exporters)
	}

	return &result
}

// mergeHealthConfig merges runtime and programmatic configs
// Programmatic non-zero values take precedence over runtime values.
func mergeHealthConfig(runtime, programmatic *shared.HealthConfig) *shared.HealthConfig {
	result := *runtime // Start with runtime

	// Override with programmatic non-zero values
	if programmatic.Features.AutoDiscovery {
		result.Features.AutoDiscovery = true
	}

	if programmatic.Intervals.Check > 0 {
		result.Intervals.Check = programmatic.Intervals.Check
	}

	if programmatic.Intervals.Report > 0 {
		result.Intervals.Report = programmatic.Intervals.Report
	}

	if programmatic.Performance.DefaultTimeout > 0 {
		result.Performance.DefaultTimeout = programmatic.Performance.DefaultTimeout
	}

	if programmatic.Performance.MaxConcurrentChecks > 0 {
		result.Performance.MaxConcurrentChecks = programmatic.Performance.MaxConcurrentChecks
	}

	if programmatic.Thresholds.Degraded > 0 {
		result.Thresholds.Degraded = programmatic.Thresholds.Degraded
	}

	if programmatic.Thresholds.Unhealthy > 0 {
		result.Thresholds.Unhealthy = programmatic.Thresholds.Unhealthy
	}

	if programmatic.Performance.HistorySize > 0 {
		result.Performance.HistorySize = programmatic.Performance.HistorySize
	}

	// Boolean fields (prefer programmatic if explicitly set)
	if programmatic.Features.AutoDiscovery {
		result.Features.AutoDiscovery = true
	}

	if programmatic.Features.Persistence {
		result.Features.Persistence = true
	}

	if programmatic.Features.Alerting {
		result.Features.Alerting = true
	}

	if programmatic.Features.Aggregation {
		result.Features.Aggregation = true
	}

	if programmatic.Features.Prediction {
		result.Features.Prediction = true
	}

	if programmatic.Endpoints.Enabled {
		result.Endpoints.Enabled = true
	}

	if programmatic.AutoRegister {
		result.AutoRegister = true
	}

	if programmatic.ExposeEndpoints {
		result.ExposeEndpoints = true
	}

	if programmatic.Features.Metrics {
		result.Features.Metrics = true
	}

	// Slices (programmatic overrides)
	if len(programmatic.CriticalServices) > 0 {
		result.CriticalServices = programmatic.CriticalServices
	}

	// Maps (merge)
	if len(programmatic.Tags) > 0 {
		if result.Tags == nil {
			result.Tags = make(map[string]string)
		}

		maps.Copy(result.Tags, programmatic.Tags)
	}

	return &result
}

// reloadConfigsFromManager reloads metrics and health configs from ConfigManager.
func (a *app) reloadConfigsFromManager() error {
	if a.configManager == nil {
		return nil
	}

	// This runs from Start and re-reads the config newApp already read, so
	// the binds below pass a nil logger: the warnings about invalid values
	// were logged when the app was built.

	// Reload metrics config, resolved the same way newApp resolved it.
	if metricsConfig, fromFile := resolveMetricsConfig(a.codeMetrics, a.configManager, nil); fromFile && metricsConfig.Enabled {
		if err := a.metrics.Reload(metricsConfig); err != nil {
			a.logger.Warn("failed to reload metrics config", F("error", err))
		} else {
			a.logger.Debug("metrics config reloaded from ConfigManager")
		}
	}

	// Reload health config
	var healthConfig shared.HealthConfig
	if _, err := bindConfig(a.configManager, "health", &healthConfig, nil); err == nil {
		if healthConfig.Enabled {
			if err := a.healthManager.Reload(&healthConfig); err != nil {
				a.logger.Warn("failed to reload health config", F("error", err))
			} else {
				a.logger.Debug("health config reloaded from ConfigManager")
			}
		}
	}

	return nil
}

// handleMetrics handles the /_/metrics endpoint.
func (a *app) handleMetrics(ctx Context) error {
	if a.metrics == nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "metrics not available",
		})
	}

	// If the collector implements PrometheusProvider, delegate to the real
	// promhttp handler so that content negotiation (OpenMetrics, compression,
	// etc.) is handled correctly by the Prometheus client library.
	if p, ok := a.metrics.(shared.PrometheusProvider); ok {
		p.PrometheusHandler().ServeHTTP(ctx.Response(), ctx.Request())

		return nil
	}

	// Fallback: export metrics manually for collectors that do not implement
	// PrometheusProvider (e.g. the no-op collector used in tests).
	data, err := a.metrics.Export(shared.ExportFormatPrometheus)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to export metrics",
		})
	}

	ctx.Response().Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	ctx.Response().WriteHeader(http.StatusOK)

	if _, err := ctx.Response().Write(data); err != nil {
		return fmt.Errorf("failed to write metrics data: %w", err)
	}

	return nil
}

// handleHealth handles the /_/health endpoint.
func (a *app) handleHealth(ctx Context) error {
	if a.healthManager == nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "health manager not available",
		})
	}

	// During startup grace period, always return healthy to prevent
	// PaaS platforms from killing the app before health checks stabilize
	if time.Since(a.startTime) < a.config.HealthGracePeriod {
		return ctx.JSON(http.StatusOK, map[string]any{
			"status": "starting",
			"uptime": time.Since(a.startTime).String(),
		})
	}

	report, fresh := a.healthReport(ctx.Request().Context())

	if !fresh {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]any{
			"status": "stale",
			"error":  "health report is older than the staleness bound",
			"age":    time.Since(report.Timestamp).String(),
			"report": report,
		})
	}

	// Set status code based on health
	statusCode := http.StatusOK
	if report.Overall == HealthStatusUnhealthy {
		statusCode = http.StatusServiceUnavailable

		a.logger.Warn("health check reporting unhealthy status",
			F("services", len(report.Services)),
		)
	}

	return ctx.JSON(statusCode, report)
}

// healthStalenessBound is how far behind the background check loop a cached
// report may fall before the endpoints stop trusting it.
//
// Two intervals gives one cycle of slack, so a single slow or skipped round
// does not flap a readiness probe.
const healthStalenessFactor = 2

// healthReport returns the report the background check loop cached, and whether
// it is recent enough to answer with.
//
// The endpoints deliberately do not run the checks themselves. Every extension
// registers a health check and auto-discovery registers one per DI service, so
// a live Check fans out across every one of them, doing real database and
// cache round trips, and a readiness probe runs every few seconds. That put the
// probe cost in proportion to the number of installed extensions, to recompute
// an answer the check loop had already cached.
//
// Only the very first request can find no report at all: the health manager
// runs one check during Start. That case falls back to a live check rather than
// reporting a failure the app has not actually had.
func (a *app) healthReport(ctx context.Context) (*HealthReport, bool) {
	report := a.healthManager.LastReport()
	if report == nil {
		return a.healthManager.Check(ctx), true
	}

	interval := a.config.HealthConfig.Intervals.Check
	if interval <= 0 {
		interval = DefaultHealthConfig().Intervals.Check
	}

	return report, time.Since(report.Timestamp) <= healthStalenessFactor*interval
}

// handleHealthLive handles the /_/health/live endpoint.
func (a *app) handleHealthLive(ctx Context) error {
	// Liveness probe - always returns 200 if server is up
	return ctx.JSON(http.StatusOK, map[string]string{
		"status": "alive",
	})
}

// handleHealthReady handles the /_/health/ready endpoint.
func (a *app) handleHealthReady(ctx Context) error {
	if a.healthManager == nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"error":  "health manager not available",
		})
	}

	// During startup grace period, always return ready to prevent
	// PaaS platforms from killing the app before health checks stabilize
	if time.Since(a.startTime) < a.config.HealthGracePeriod {
		return ctx.JSON(http.StatusOK, map[string]string{
			"status": "starting",
		})
	}

	report, fresh := a.healthReport(ctx.Request().Context())

	if !fresh {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"status":  "not ready",
			"message": "health report is stale; the check loop is not keeping up",
		})
	}

	if report.Overall == HealthStatusHealthy {
		return ctx.JSON(http.StatusOK, map[string]string{
			"status": "ready",
		})
	}

	return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
		"status":  "not ready",
		"message": "one or more services unhealthy",
	})
}

// forgeYAMLConfig represents the app-level .forge.yaml configuration
// This is loaded at runtime to provide app-specific settings like port.
type forgeYAMLConfig struct {
	App struct {
		Name    string `yaml:"name"`
		Type    string `yaml:"type"`
		Version string `yaml:"version"`
	} `yaml:"app"`
	Dev struct {
		Port int    `yaml:"port"`
		Host string `yaml:"host"`
	} `yaml:"dev"`
	Build struct {
		Output string `yaml:"output"`
	} `yaml:"build"`
	Database struct {
		DisableMigrations bool `yaml:"disable_migrations"`
		CentralMigrations bool `yaml:"central_migrations"`
	} `yaml:"database"`
}

// loadForgeYAMLConfig searches for and loads .forge.yaml to configure runtime settings.
// This ensures the app can read its configuration when run directly (not through forge dev).
func loadForgeYAMLConfig(config AppConfig, logger Logger) AppConfig {
	// Search for .forge.yaml starting from current directory
	dir, err := os.Getwd()
	if err != nil {
		return config
	}

	var forgeConfigPath string

	maxDepth := 5

	// Search up the directory tree
	for range maxDepth {
		yamlPath := filepath.Join(dir, ".forge.yaml")
		ymlPath := filepath.Join(dir, ".forge.yml")

		if _, err := os.Stat(yamlPath); err == nil {
			forgeConfigPath = yamlPath

			break
		}

		if _, err := os.Stat(ymlPath); err == nil {
			forgeConfigPath = ymlPath

			break
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}

		dir = parent
	}

	// If no .forge.yaml found, return original config
	if forgeConfigPath == "" {
		return config
	}

	// Read and parse .forge.yaml
	data, err := os.ReadFile(forgeConfigPath)
	if err != nil {
		if logger != nil {
			logger.Debug("failed to read .forge.yaml", F("path", forgeConfigPath), F("error", err.Error()))
		}

		return config
	}

	var forgeConfig forgeYAMLConfig
	if err := yaml.Unmarshal(data, &forgeConfig); err != nil {
		if logger != nil {
			logger.Debug("failed to parse .forge.yaml", F("path", forgeConfigPath), F("error", err.Error()))
		}

		return config
	}

	// Apply configuration from .forge.yaml
	// Priority: Explicit AppConfig > .forge.yaml > Defaults

	// Set HTTPAddress from dev.port if:
	// 1. HTTPAddress is still the default ":8080"
	// 2. dev.port is set in .forge.yaml
	// 3. PORT environment variable is not set (env vars take precedence)
	if config.HTTPAddress == ":8080" && forgeConfig.Dev.Port > 0 && os.Getenv("PORT") == "" {
		config.HTTPAddress = fmt.Sprintf(":%d", forgeConfig.Dev.Port)
		if logger != nil {
			logger.Info("loaded port from .forge.yaml", F("port", forgeConfig.Dev.Port), F("path", forgeConfigPath))
		}
	}

	// If PORT env var is set, use it (highest priority for port)
	if portEnv := os.Getenv("PORT"); portEnv != "" {
		config.HTTPAddress = ":" + portEnv
		if logger != nil {
			logger.Info("using port from environment variable", F("port", portEnv))
		}
	}

	// Set DisableMigrations from .forge.yaml if not already set programmatically
	if !config.DisableMigrations && forgeConfig.Database.DisableMigrations {
		config.DisableMigrations = true

		if logger != nil {
			logger.Info("migrations disabled via .forge.yaml", F("path", forgeConfigPath))
		}
	}

	// Set CentralMigrations from .forge.yaml if not already set programmatically.
	if !config.CentralMigrations && forgeConfig.Database.CentralMigrations {
		config.CentralMigrations = true

		if logger != nil {
			logger.Info("central migrations enabled via .forge.yaml", F("path", forgeConfigPath))
		}
	}

	return config
}

// otlpProducerSource is what the metrics collector offers the push path. The
// no-op collector doesn't implement it.
type otlpProducerSource interface {
	OTLPProducer(temporality exporters.Temporality) *exporters.OTLPProducer
}

// buildMetricsExport resolves metrics push and, when an endpoint is
// configured, returns a pusher that app.Start starts. Nothing is pushed
// before Start. Like tracing, it never stops the app from booting.
func buildMetricsExport(config AppConfig, cm ConfigManager, m Metrics, tracing *telemetry.Provider,
	log Logger) *telemetry.MetricsPusher {
	var fromFile shared.MetricsExportConfig
	if cm != nil {
		if err := cm.Bind("metrics_export", &fromFile); err != nil && cm.HasKey("metrics_export") {
			log.Warn("telemetry: could not read metrics_export config, using defaults", F("error", err))

			fromFile = shared.MetricsExportConfig{}
		}
	}

	settings := telemetry.ResolveMetrics(telemetry.MetricsInputs{
		Code:   config.MetricsExportConfig,
		File:   fromFile,
		Getenv: os.Getenv,
	})

	for _, w := range settings.Warnings {
		log.Warn(w)
	}

	if !settings.Enabled {
		return nil
	}

	source, ok := m.(otlpProducerSource)
	if !ok {
		log.Warn("telemetry: metrics are disabled, so nothing is pushed", F("endpoint", settings.Endpoint))

		return nil
	}

	temporality := exporters.Cumulative
	if settings.Temporality == telemetry.TemporalityDelta {
		temporality = exporters.Delta
	}

	var res *resource.Resource
	if tracing != nil {
		res = tracing.Resource()
	}

	pusher, err := telemetry.NewMetricsPusher(settings, res, source.OTLPProducer(temporality), log)
	if err != nil {
		log.Error("telemetry: metrics push disabled", F("error", err))

		return nil
	}

	return pusher
}

// buildTracing resolves the tracing config and builds the provider. Telemetry
// never stops the app from booting: an exporter that can't be built is
// logged and the app carries on creating spans without exporting them.
func buildTracing(config AppConfig, cm ConfigManager, log Logger) *telemetry.Provider {
	fromFile := readTracingFile(cm, log)

	settings := telemetry.Resolve(telemetry.Inputs{
		Code:        config.TracingConfig,
		File:        fromFile,
		Environment: config.Environment,
		Getenv:      os.Getenv,
	})

	for _, w := range settings.Warnings {
		log.Warn(w)
	}

	ctx := context.Background()

	res, err := telemetry.BuildResource(ctx, config.Name, config.Version, config.Environment, settings.Resource)
	if err != nil {
		log.Warn("telemetry: resource detection incomplete", F("error", err))
	}

	provider, err := telemetry.NewProvider(ctx, settings, res, log)
	if err != nil {
		log.Error("telemetry: tracing export disabled", F("error", err))

		settings.Endpoint = ""
		provider, _ = telemetry.NewProvider(ctx, settings, res, log)
	}

	return provider
}

// readTracingFile returns the tracing block from the config file. A block that
// won't bind at all is logged and ignored. A value that won't convert is logged
// and left unset, so its default applies.
func readTracingFile(cm ConfigManager, log Logger) shared.TracingConfig {
	var fromFile shared.TracingConfig
	if cm == nil {
		return fromFile
	}

	// Bind also fails when there is no tracing key, which is the normal
	// case, so only a key that exists and won't bind is worth a warning.
	if _, err := bindConfig(cm, "tracing", &fromFile, log); err != nil && cm.HasKey("tracing") {
		log.Warn("telemetry: could not read tracing config, using defaults", F("error", err))

		return shared.TracingConfig{}
	}

	return fromFile
}
