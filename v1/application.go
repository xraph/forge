package forge

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/database"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/plugins"
	"github.com/xraph/forge/router"
)

// application implements the Application interface
type application struct {
	// Core properties
	name        string
	version     string
	environment string

	// Core components
	config    core.Config
	logger    logger.Logger
	container core.Container
	router    router.Router
	server    *http.Server

	// Service managers
	pluginSystem   *plugins.System
	pluginManager  plugins.Manager
	eventBus       ApplicationEventBus
	serviceManager ServiceManager
	metrics        ApplicationMetrics

	// State management
	ready    bool
	healthy  bool
	started  bool
	shutdown bool
	mu       sync.RWMutex

	// Lifecycle callbacks
	startupCallbacks  []func(context.Context) error
	shutdownCallbacks []func(context.Context) error
	errorCallbacks    []func(error)

	// Development features
	hotReload   bool
	devServer   DevelopmentServer
	testingMode bool

	// Performance tracking
	startTime     time.Time
	startDuration time.Duration
}

// Application interface implementation

func (a *application) Name() string {
	return a.name
}

func (a *application) Version() string {
	return a.version
}

func (a *application) Environment() string {
	return a.environment
}

func (a *application) Container() core.Container {
	return a.container
}

func (a *application) Config() core.Config {
	return a.config
}

func (a *application) Logger() logger.Logger {
	return a.logger
}

func (a *application) Router() router.Router {
	return a.router
}

// Service access methods

func (a *application) Database(name ...string) database.SQLDatabase {
	return a.container.SQL(name...)
}

func (a *application) NoSQL(name ...string) database.NoSQLDatabase {
	return a.container.NoSQL(name...)
}

func (a *application) Cache(name ...string) database.Cache {
	return a.container.Cache(name...)
}

func (a *application) Jobs() jobs.Processor {
	return a.container.Jobs()
}

func (a *application) Metrics() observability.Metrics {
	return a.container.Metrics()
}

func (a *application) Tracer() observability.Tracer {
	return a.container.Tracer()
}

func (a *application) Health() observability.Health {
	return a.container.Health()
}

func (a *application) Plugins() plugins.Manager {
	return a.pluginManager
}

// Lifecycle methods

func (a *application) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		return ErrApplicationAlreadyStarted
	}

	start := time.Now()
	a.logger.Info("Starting application",
		logger.String("name", a.name),
		logger.String("version", a.version),
		logger.String("environment", a.environment),
		logger.String("address", a.server.Addr),
	)

	// Publish starting event
	if a.eventBus != nil {
		a.eventBus.PublishAsync(ctx, ApplicationEvent{
			Type:   EventApplicationStarting,
			Source: a.name,
		})
	}

	// Start container
	if err := a.container.Start(ctx); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Start plugins
	if a.pluginManager != nil {
		if err := a.pluginManager.StartAll(ctx); err != nil {
			return fmt.Errorf("failed to start plugins: %w", err)
		}
	}

	// Run startup callbacks
	for _, callback := range a.startupCallbacks {
		if err := callback(ctx); err != nil {
			return fmt.Errorf("startup callback failed: %w", err)
		}
	}

	// Start development server if enabled
	if a.devServer != nil {
		if err := a.devServer.EnableHotReload(); err != nil {
			a.logger.Warn("Failed to enable hot reload", logger.Error(err))
		}
	}

	a.started = true
	a.ready = true
	a.healthy = true

	duration := time.Since(start)
	a.startDuration = duration

	// Record startup metrics
	if a.metrics != nil {
		a.metrics.RecordStartup(duration.Seconds())
	}

	a.logger.Info("Application started successfully",
		logger.Duration("startup_duration", duration),
		logger.String("address", a.server.Addr),
	)

	// Publish started event
	if a.eventBus != nil {
		a.eventBus.PublishAsync(ctx, ApplicationEvent{
			Type:   EventApplicationStarted,
			Source: a.name,
			Data: map[string]interface{}{
				"duration": duration.String(),
				"address":  a.server.Addr,
			},
		})

		// Publish ready event after a brief delay to ensure everything is fully initialized
		go func() {
			time.Sleep(100 * time.Millisecond)
			a.eventBus.PublishAsync(ctx, ApplicationEvent{
				Type:   EventApplicationReady,
				Source: a.name,
			})
		}()
	}

	return nil
}

func (a *application) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.started || a.shutdown {
		return nil
	}

	start := time.Now()
	a.logger.Info("Stopping application")

	// Publish stopping event
	if a.eventBus != nil {
		a.eventBus.PublishAsync(ctx, ApplicationEvent{
			Type:   EventApplicationStopping,
			Source: a.name,
		})
	}

	// Mark as unhealthy
	a.healthy = false
	a.ready = false

	// Stop development server
	if a.devServer != nil {
		a.devServer.DisableHotReload()
	}

	// Run shutdown callbacks
	for _, callback := range a.shutdownCallbacks {
		if err := callback(ctx); err != nil {
			a.logger.Error("Shutdown callback failed", logger.Error(err))
		}
	}

	// Stop plugins
	if a.pluginManager != nil {
		if err := a.pluginManager.StopAll(ctx); err != nil {
			a.logger.Error("Failed to stop plugins", logger.Error(err))
		}
	}

	// Stop server
	if a.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			a.logger.Error("Server shutdown failed", logger.Error(err))
			// Force close if graceful shutdown fails
			a.server.Close()
		}
	}

	// Stop container
	if err := a.container.Stop(ctx); err != nil {
		a.logger.Error("Container stop failed", logger.Error(err))
	}

	// Stop event bus last
	if a.eventBus != nil {
		a.eventBus.Stop(ctx)
	}

	a.shutdown = true
	duration := time.Since(start)

	// Record shutdown metrics
	if a.metrics != nil {
		a.metrics.RecordShutdown(duration.Seconds())
	}

	a.logger.Info("Application stopped successfully",
		logger.Duration("shutdown_duration", duration),
	)

	return nil
}

func (a *application) Run() error {
	ctx := context.Background()

	// Start the application
	if err := a.Start(ctx); err != nil {
		return fmt.Errorf("failed to start application: %w", err)
	}

	// Start HTTP server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		a.logger.Info("Starting HTTP server", logger.String("address", a.server.Addr))

		var err error
		if a.config.GetBool("server.tls.enabled") {
			err = a.server.ListenAndServeTLS(
				a.config.GetString("server.tls.cert_file"),
				a.config.GetString("server.tls.key_file"),
			)
		} else {
			err = a.server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	// Wait for interrupt signal
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		a.logger.Error("Server error", logger.Error(err))
		return err
	case sig := <-signalChan:
		a.logger.Info("Received signal", logger.String("signal", sig.String()))

		// Graceful shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		return a.Stop(shutdownCtx)
	}
}

func (a *application) Shutdown(ctx context.Context) error {
	return a.Stop(ctx)
}

// Health and readiness methods

func (a *application) Healthy(ctx context.Context) error {
	if !a.healthy {
		return ErrApplicationNotHealthy
	}

	// Check container health
	if health := a.container.Health(); health != nil {
		status := health.Check(ctx)
		if status.Status == observability.HealthStatusDown || status.Status == observability.HealthStatusUnknown {
			return fmt.Errorf("container service down")
		}
		return nil
	}

	return nil
}

func (a *application) Ready(ctx context.Context) error {
	if !a.ready {
		return ErrApplicationNotReady
	}
	return nil
}

func (a *application) IsReady() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ready
}

func (a *application) IsHealthy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.healthy
}

// Callback methods

func (a *application) OnStartup(callback func(context.Context) error) {
	a.startupCallbacks = append(a.startupCallbacks, callback)
}

func (a *application) OnShutdown(callback func(context.Context) error) {
	a.shutdownCallbacks = append(a.shutdownCallbacks, callback)
}

func (a *application) OnError(callback func(error)) {
	a.errorCallbacks = append(a.errorCallbacks, callback)
}

// Development methods

func (a *application) Reload(ctx context.Context) error {
	if !a.hotReload {
		return fmt.Errorf("hot reload not enabled")
	}

	if a.devServer != nil {
		return a.devServer.Reload()
	}

	return fmt.Errorf("development server not available")
}

func (a *application) Reset(ctx context.Context) error {
	a.logger.Info("Resetting application state")

	// Reset databases
	// Reset caches
	// Reset job queues
	// etc.

	a.logger.Info("Application state reset completed")
	return nil
}
