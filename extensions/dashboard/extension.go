package dashboard

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// Extension implements a simple health and metrics dashboard.
type Extension struct {
	*forge.BaseExtension

	config Config
	server *DashboardServer
}

// NewExtension creates a new dashboard extension.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension(
		"dashboard",
		"2.0.0",
		"Simple health and metrics dashboard with web UI",
	)

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// Register registers the dashboard extension.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load config from ConfigManager with dual-key support
	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("dashboard", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("dashboard: failed to load required config: %w", err)
		}

		e.Logger().Warn("dashboard: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("dashboard config validation failed: %w", err)
	}

	// Create dashboard server
	e.server = NewDashboardServer(
		e.config,
		app.HealthManager(),
		app.Metrics(),
		app.Logger(),
		app.Container(),
	)

	// Register dashboard service with DI container
	if err := forge.RegisterSingleton(app.Container(), "dashboard", func(c forge.Container) (*DashboardServer, error) {
		return e.server, nil
	}); err != nil {
		return fmt.Errorf("failed to register dashboard service: %w", err)
	}

	e.Logger().Info("dashboard extension registered",
		forge.F("port", e.config.Port),
		forge.F("base_path", e.config.BasePath),
		forge.F("realtime", e.config.EnableRealtime),
		forge.F("export", e.config.EnableExport),
	)

	return nil
}

// Start starts the dashboard extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting dashboard extension")

	if err := e.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start dashboard server: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("dashboard extension started",
		forge.F("url", fmt.Sprintf("http://localhost:%d%s", e.config.Port, e.config.BasePath)),
	)

	return nil
}

// Stop stops the dashboard extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping dashboard extension")

	if e.server != nil {
		if err := e.server.Stop(ctx); err != nil {
			e.Logger().Error("failed to stop dashboard server",
				forge.F("error", err),
			)
		}
	}

	e.MarkStopped()
	e.Logger().Info("dashboard extension stopped")

	return nil
}

// Health checks if the dashboard is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if e.server == nil {
		return errors.New("dashboard server not initialized")
	}

	if !e.server.IsRunning() {
		return errors.New("dashboard server not running")
	}

	return nil
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{} // No hard dependencies
}

// Server returns the dashboard server instance (for advanced usage).
func (e *Extension) Server() *DashboardServer {
	return e.server
}
