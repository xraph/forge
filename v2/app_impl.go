package forge

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/xraph/forge/v2/internal/config"
	"github.com/xraph/forge/v2/internal/di"
	healthinternal "github.com/xraph/forge/v2/internal/health"
	"github.com/xraph/forge/v2/internal/logger"
	metricsinternal "github.com/xraph/forge/v2/internal/metrics"
	"github.com/xraph/forge/v2/internal/shared"
)

// app implements the App interface
type app struct {
	// Configuration
	config AppConfig

	// Core components
	container     Container
	router        Router
	configManager ConfigManager
	logger        Logger
	metrics       Metrics
	healthManager HealthManager

	// HTTP server
	httpServer *http.Server

	// Extensions
	extensions []Extension

	// Lifecycle
	startTime time.Time
	mu        sync.RWMutex
	started   bool
}

// newApp creates a new app instance
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

	// Create logger if not provided
	logger := config.Logger
	if logger == nil {
		// Use development logger for dev, production for prod, noop otherwise
		switch config.Environment {
		case "development":
			logger = NewDevelopmentLogger()
		case "production":
			logger = NewProductionLogger()
		default:
			logger = NewNoopLogger()
		}
	}

	// Create error handler
	var errorHandler ErrorHandler
	if config.ErrorHandler == nil {
		errorHandler = shared.NewDefaultErrorHandler(logger)
	}

	// Create config manager if not provided (needed for metrics/health initialization)
	configManager := config.ConfigManager
	// Will initialize later once metrics is available

	// Create metrics with full config support
	var metrics Metrics
	metricsConfig := &config.MetricsConfig

	// Apply defaults if metrics config is empty (not explicitly disabled)
	if !metricsConfig.Enabled && metricsConfig.Namespace == "" {
		// User didn't provide config, use defaults
		defaultMetrics := DefaultMetricsConfig()
		metricsConfig = &defaultMetrics
	}

	// Try to load from ConfigManager first
	if configManager != nil {
		var runtimeConfig shared.MetricsConfig
		if err := configManager.Bind("metrics", &runtimeConfig); err == nil {
			// Merge: runtime values override defaults, programmatic values override runtime
			if runtimeConfig.Enabled {
				metricsConfig = mergeMetricsConfig(&runtimeConfig, metricsConfig)
			}
		}
	}

	if metricsConfig.Enabled {
		metrics = metricsinternal.New(metricsConfig, logger)
	} else {
		metrics = metricsinternal.NewNoOpMetrics()
	}

	// Initialize config manager if not provided
	if configManager == nil {
		configManager = NewDefaultConfigManager(logger, metrics, errorHandler)
	}

	// Create health manager with full config support
	var healthManager HealthManager
	healthConfig := &config.HealthConfig

	// Apply defaults if health config is empty (not explicitly disabled)
	if !healthConfig.Enabled && healthConfig.CheckInterval == 0 {
		// User didn't provide config, use defaults
		defaultHealth := DefaultHealthConfig()
		defaultHealth.CheckInterval = 30 * time.Second
		defaultHealth.ReportInterval = 60 * time.Second
		defaultHealth.EnableAutoDiscovery = true
		defaultHealth.MaxConcurrentChecks = 10
		defaultHealth.DefaultTimeout = 5 * time.Second
		defaultHealth.EnableSmartAggregation = true
		defaultHealth.HistorySize = 100
		healthConfig = &defaultHealth
	}

	// Try to load from ConfigManager first
	if configManager != nil {
		var runtimeConfig shared.HealthConfig
		if err := configManager.Bind("health", &runtimeConfig); err == nil {
			if runtimeConfig.Enabled {
				healthConfig = mergeHealthConfig(&runtimeConfig, healthConfig)
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
	router := NewRouter(routerOpts...)

	// Register core services with DI
	_ = RegisterSingleton(container, "logger", func(c Container) (Logger, error) {
		return logger, nil
	})
	_ = RegisterSingleton(container, "config", func(c Container) (ConfigManager, error) {
		return configManager, nil
	})
	_ = RegisterSingleton(container, "metrics", func(c Container) (Metrics, error) {
		return metrics, nil
	})
	_ = RegisterSingleton(container, "health", func(c Container) (HealthManager, error) {
		return healthManager, nil
	})
	_ = RegisterSingleton(container, "router", func(c Container) (Router, error) {
		return router, nil
	})

	a := &app{
		config:        config,
		container:     container,
		router:        router,
		configManager: configManager,
		logger:        logger,
		metrics:       metrics,
		healthManager: healthManager,
		extensions:    config.Extensions,
		startTime:     time.Now(),
	}

	// Setup built-in endpoints
	a.setupBuiltinEndpoints()

	return a
}

// Container returns the DI container
func (a *app) Container() Container {
	return a.container
}

// Router returns the router
func (a *app) Router() Router {
	return a.router
}

// Config returns the config manager
func (a *app) Config() ConfigManager {
	return a.configManager
}

// Logger returns the logger
func (a *app) Logger() Logger {
	return a.logger
}

// Metrics returns the metrics collector
func (a *app) Metrics() Metrics {
	return a.metrics
}

// HealthManager returns the health manager
func (a *app) HealthManager() HealthManager {
	return a.healthManager
}

// RegisterService registers a service with the DI container
func (a *app) RegisterService(name string, factory Factory, opts ...RegisterOption) error {
	return a.container.Register(name, factory, opts...)
}

// RegisterController registers a controller with the router
func (a *app) RegisterController(controller Controller) error {
	return a.router.RegisterController(controller)
}

// RegisterExtension registers an extension with the app
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
	return nil
}

// Extensions returns all registered extensions
func (a *app) Extensions() []Extension {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Return a copy to prevent modification
	extensions := make([]Extension, len(a.extensions))
	copy(extensions, a.extensions)
	return extensions
}

// GetExtension returns an extension by name
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

// Name returns the application name
func (a *app) Name() string {
	return a.config.Name
}

// Version returns the application version
func (a *app) Version() string {
	return a.config.Version
}

// Environment returns the application environment
func (a *app) Environment() string {
	return a.config.Environment
}

// StartTime returns the application start time
func (a *app) StartTime() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.startTime
}

// Uptime returns the application uptime
func (a *app) Uptime() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return time.Since(a.startTime)
}

// Start starts the application
func (a *app) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		return fmt.Errorf("app already started")
	}

	a.logger.Info("starting application",
		F("name", a.config.Name),
		F("version", a.config.Version),
		F("environment", a.config.Environment),
		F("extensions", len(a.extensions)),
	)

	// 1. Register extensions with app (registers services with DI)
	for _, ext := range a.extensions {
		a.logger.Info("registering extension",
			F("extension", ext.Name()),
			F("version", ext.Version()),
		)
		if err := ext.Register(a); err != nil {
			return fmt.Errorf("failed to register extension %s: %w", ext.Name(), err)
		}
	}

	// 2. Start DI container (starts all registered services)
	a.logger.Debug("starting DI container")
	if err := a.container.Start(ctx); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	a.logger.Debug("DI container started")

	// 2.5 Set container reference for health manager (but don't start yet)
	if healthMgr, ok := a.healthManager.(*healthinternal.ManagerImpl); ok {
		a.logger.Debug("setting container reference for health manager")
		healthMgr.SetContainer(a.container)
		a.logger.Debug("container reference set")
	}

	// 2.6 Reload configs from ConfigManager (hot-reload support)
	a.logger.Debug("reloading configs from ConfigManager")
	if err := a.reloadConfigsFromManager(); err != nil {
		a.logger.Warn("failed to reload configs from ConfigManager", F("error", err))
	}
	a.logger.Debug("configs reloaded")

	// 3. Start extensions in dependency order
	if err := a.startExtensions(ctx); err != nil {
		return err
	}

	// 4. Setup observability endpoints (including extension health checks)
	a.setupObservabilityEndpoints()
	a.registerExtensionHealthChecks()

	// Note: Health manager is already started by container.Start()
	// No need to start it again here

	a.started = true
	a.startTime = time.Now()
	a.logger.Info("application started successfully")

	return nil
}

// Stop stops the application
func (a *app) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.started {
		return nil
	}

	a.logger.Info("stopping application")

	// 1. Stop health manager
	if healthMgr, ok := a.healthManager.(*healthinternal.ManagerImpl); ok {
		if err := healthMgr.Stop(ctx); err != nil {
			a.logger.Error("failed to stop health manager", F("error", err))
		}
	}

	// 2. Stop extensions in reverse order
	a.stopExtensions(ctx)

	// 3. Stop DI container (stops all services in reverse order)
	if err := a.container.Stop(ctx); err != nil {
		a.logger.Error("failed to stop container", F("error", err))
	}

	a.started = false
	a.logger.Info("application stopped")

	return nil
}

// Run starts the HTTP server and blocks until a shutdown signal is received
func (a *app) Run() error {
	// Start the application
	if err := a.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}

	// Create HTTP server
	a.httpServer = &http.Server{
		Addr:         a.config.HTTPAddress,
		Handler:      a.router,
		ReadTimeout:  a.config.HTTPTimeout,
		WriteTimeout: a.config.HTTPTimeout,
		IdleTimeout:  a.config.HTTPTimeout * 2,
	}

	// Channel for shutdown signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, a.config.ShutdownSignals...)

	// Channel for server errors
	errChan := make(chan error, 1)

	// Start HTTP server in goroutine
	go func() {
		a.logger.Info("starting http server", F("address", a.config.HTTPAddress))
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
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

// gracefulShutdown performs a graceful shutdown
func (a *app) gracefulShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), a.config.ShutdownTimeout)
	defer cancel()

	a.logger.Info("starting graceful shutdown", F("timeout", a.config.ShutdownTimeout))

	// 1. Stop accepting new requests and wait for active requests to complete
	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(ctx); err != nil {
			a.logger.Error("http server shutdown error", F("error", err))
		}
	}

	// 2. Stop the application (which stops all services)
	if err := a.Stop(ctx); err != nil {
		a.logger.Error("app shutdown error", F("error", err))
	}

	a.logger.Info("graceful shutdown complete")
	return nil
}

// setupBuiltinEndpoints sets up built-in endpoints
func (a *app) setupBuiltinEndpoints() {
	// Info endpoint
	a.router.GET("/_/info", a.handleInfo)
}

// setupObservabilityEndpoints sets up observability endpoints
func (a *app) setupObservabilityEndpoints() {
	// These are already setup by router observability integration
	// This is a placeholder for any additional setup
}

// handleInfo handles the /_/info endpoint
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

// startExtensions starts all extensions in dependency order
func (a *app) startExtensions(ctx context.Context) error {
	// Build dependency graph
	graph := di.NewDependencyGraph()
	for _, ext := range a.extensions {
		graph.AddNode(ext.Name(), ext.Dependencies())
	}

	// Get topological sort (dependency order)
	order, err := graph.TopologicalSort()
	if err != nil {
		return fmt.Errorf("failed to resolve extension dependencies: %w", err)
	}

	// Create extension map for lookup
	extMap := make(map[string]Extension)
	for _, ext := range a.extensions {
		extMap[ext.Name()] = ext
	}

	// Start extensions in dependency order
	for _, name := range order {
		ext, ok := extMap[name]
		if !ok {
			continue // Dependency might not be registered (optional)
		}

		a.logger.Info("starting extension",
			F("extension", ext.Name()),
			F("version", ext.Version()),
		)

		if err := ext.Start(ctx); err != nil {
			return fmt.Errorf("failed to start extension %s: %w", ext.Name(), err)
		}

		a.logger.Info("extension started",
			F("extension", ext.Name()),
		)
	}

	return nil
}

// stopExtensions stops all extensions in reverse order
func (a *app) stopExtensions(ctx context.Context) {
	// Stop in reverse order
	for i := len(a.extensions) - 1; i >= 0; i-- {
		ext := a.extensions[i]

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

// registerExtensionHealthChecks registers health checks for all extensions
func (a *app) registerExtensionHealthChecks() {
	for _, ext := range a.extensions {
		// Create local copies for closure capture (avoid loop variable capture bug)
		extRef := ext
		extName := ext.Name()

		checkName := "extension:" + extName
		a.healthManager.RegisterFn(checkName, func(ctx context.Context) *HealthResult {
			if err := extRef.Health(ctx); err != nil {
				return &HealthResult{
					Status:  HealthStatusUnhealthy,
					Message: fmt.Sprintf("%s extension unhealthy", extName),
					Details: map[string]any{"error": err.Error()},
				}
			}
			return &HealthResult{
				Status:  HealthStatusHealthy,
				Message: fmt.Sprintf("%s extension healthy", extName),
			}
		})
	}
}

// Helper to create a default config manager (stub for now)
func NewDefaultConfigManager(
	l logger.Logger,
	m Metrics,
	e ErrorHandler,
) ConfigManager {
	if e == nil {
		e = shared.NewDefaultErrorHandler(l)
	}

	return config.NewManager(config.ManagerConfig{
		Logger:       l,
		Metrics:      m,
		ErrorHandler: e,
	})
}

// defaultConfigManager is a stub config manager
type defaultConfigManager struct{}

func (c *defaultConfigManager) Get(key string) (interface{}, error) {
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
func (c *defaultConfigManager) Set(key string, value interface{}) error {
	return nil
}
func (c *defaultConfigManager) Bind(key string, target interface{}) error {
	return fmt.Errorf("config not found: %s", key)
}

// ConfigManager is now properly exported from config.go

// =============================================================================
// CONFIG MERGE HELPERS
// =============================================================================

// mergeMetricsConfig merges runtime and programmatic configs
// Programmatic non-zero values take precedence over runtime values
func mergeMetricsConfig(runtime, programmatic *shared.MetricsConfig) *shared.MetricsConfig {
	result := *runtime // Start with runtime

	// Override with programmatic non-zero values
	if programmatic.Namespace != "" {
		result.Namespace = programmatic.Namespace
	}
	if programmatic.MetricsPath != "" {
		result.MetricsPath = programmatic.MetricsPath
	}
	if programmatic.CollectionInterval > 0 {
		result.CollectionInterval = programmatic.CollectionInterval
	}
	if programmatic.MaxMetrics > 0 {
		result.MaxMetrics = programmatic.MaxMetrics
	}
	if programmatic.BufferSize > 0 {
		result.BufferSize = programmatic.BufferSize
	}

	// Boolean fields (prefer programmatic if set explicitly in config)
	// For booleans, we can't distinguish zero value from explicit false,
	// so runtime takes precedence unless we have explicit true
	if programmatic.EnableSystemMetrics {
		result.EnableSystemMetrics = true
	}
	if programmatic.EnableRuntimeMetrics {
		result.EnableRuntimeMetrics = true
	}
	if programmatic.EnableHTTPMetrics {
		result.EnableHTTPMetrics = true
	}

	// Merge maps (programmatic values override)
	if len(programmatic.DefaultTags) > 0 {
		if result.DefaultTags == nil {
			result.DefaultTags = make(map[string]string)
		}
		for k, v := range programmatic.DefaultTags {
			result.DefaultTags[k] = v
		}
	}

	if len(programmatic.Exporters) > 0 {
		if result.Exporters == nil {
			result.Exporters = make(map[string]shared.MetricsExporterConfig[map[string]interface{}])
		}
		for k, v := range programmatic.Exporters {
			result.Exporters[k] = v
		}
	}

	return &result
}

// mergeHealthConfig merges runtime and programmatic configs
// Programmatic non-zero values take precedence over runtime values
func mergeHealthConfig(runtime, programmatic *shared.HealthConfig) *shared.HealthConfig {
	result := *runtime // Start with runtime

	// Override with programmatic non-zero values
	if programmatic.CheckInterval > 0 {
		result.CheckInterval = programmatic.CheckInterval
	}
	if programmatic.ReportInterval > 0 {
		result.ReportInterval = programmatic.ReportInterval
	}
	if programmatic.DefaultTimeout > 0 {
		result.DefaultTimeout = programmatic.DefaultTimeout
	}
	if programmatic.MaxConcurrentChecks > 0 {
		result.MaxConcurrentChecks = programmatic.MaxConcurrentChecks
	}
	if programmatic.DegradedThreshold > 0 {
		result.DegradedThreshold = programmatic.DegradedThreshold
	}
	if programmatic.UnhealthyThreshold > 0 {
		result.UnhealthyThreshold = programmatic.UnhealthyThreshold
	}
	if programmatic.HistorySize > 0 {
		result.HistorySize = programmatic.HistorySize
	}
	if programmatic.EndpointPrefix != "" {
		result.EndpointPrefix = programmatic.EndpointPrefix
	}
	if programmatic.Version != "" {
		result.Version = programmatic.Version
	}
	if programmatic.Environment != "" {
		result.Environment = programmatic.Environment
	}

	// Boolean fields (prefer programmatic if explicitly set)
	if programmatic.EnableAutoDiscovery {
		result.EnableAutoDiscovery = true
	}
	if programmatic.EnablePersistence {
		result.EnablePersistence = true
	}
	if programmatic.EnableAlerting {
		result.EnableAlerting = true
	}
	if programmatic.EnableSmartAggregation {
		result.EnableSmartAggregation = true
	}
	if programmatic.EnablePrediction {
		result.EnablePrediction = true
	}
	if programmatic.EnableEndpoints {
		result.EnableEndpoints = true
	}
	if programmatic.AutoRegister {
		result.AutoRegister = true
	}
	if programmatic.ExposeEndpoints {
		result.ExposeEndpoints = true
	}
	if programmatic.EnableMetrics {
		result.EnableMetrics = true
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
		for k, v := range programmatic.Tags {
			result.Tags[k] = v
		}
	}

	return &result
}

// reloadConfigsFromManager reloads metrics and health configs from ConfigManager
func (a *app) reloadConfigsFromManager() error {
	if a.configManager == nil {
		return nil
	}

	// Reload metrics config
	var metricsConfig shared.MetricsConfig
	if err := a.configManager.Bind("metrics", &metricsConfig); err == nil {
		if metricsConfig.Enabled {
			if err := a.metrics.Reload(&metricsConfig); err != nil {
				a.logger.Warn("failed to reload metrics config", F("error", err))
			} else {
				a.logger.Debug("metrics config reloaded from ConfigManager")
			}
		}
	}

	// Reload health config
	var healthConfig shared.HealthConfig
	if err := a.configManager.Bind("health", &healthConfig); err == nil {
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
