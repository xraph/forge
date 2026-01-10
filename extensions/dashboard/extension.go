package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// Extension implements a simple health and metrics dashboard.
type Extension struct {
	*forge.BaseExtension

	config           Config
	collector        *DataCollector
	history          *DataHistory
	hub              *Hub
	app              forge.App
	routesRegistered bool
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

	// Store app reference
	e.app = app

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

	// Initialize data history
	e.history = NewDataHistory(e.config.MaxDataPoints, e.config.HistoryDuration)

	// Initialize data collector
	e.collector = NewDataCollector(
		app.HealthManager(),
		app.Metrics(),
		app.Container(),
		app.Logger(),
		e.history,
	)

	// Initialize WebSocket hub if realtime is enabled
	if e.config.EnableRealtime {
		e.hub = NewHub(app.Logger())
	}

	// Register dashboard service with DI container (for advanced usage)
	if err := forge.RegisterSingleton(app.Container(), "dashboard", func(c forge.Container) (*Extension, error) {
		return e, nil
	}); err != nil {
		return fmt.Errorf("failed to register dashboard service: %w", err)
	}

	e.Logger().Info("dashboard extension registered",
		forge.F("base_path", e.config.BasePath),
		forge.F("realtime", e.config.EnableRealtime),
		forge.F("export", e.config.EnableExport),
	)

	return nil
}

// Start starts the dashboard extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting dashboard extension")

	// Register routes with the app's router (only once)
	if !e.routesRegistered {
		e.registerRoutes()
		e.routesRegistered = true
	}

	// Start data collection
	go e.collector.Start(ctx, e.config.RefreshInterval)

	// Start WebSocket hub if enabled
	if e.hub != nil {
		go e.hub.Run()
		go e.broadcastUpdates(ctx)
	}

	e.MarkStarted()
	e.Logger().Info("dashboard extension started",
		forge.F("base_path", e.config.BasePath),
	)

	return nil
}

// Stop stops the dashboard extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping dashboard extension")

	// Stop data collection
	if e.collector != nil {
		e.collector.Stop()
	}

	e.MarkStopped()
	e.Logger().Info("dashboard extension stopped")

	return nil
}

// Health checks if the dashboard is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if e.collector == nil {
		return errors.New("dashboard collector not initialized")
	}

	return nil
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{} // No hard dependencies
}

// Collector returns the data collector instance (for advanced usage).
func (e *Extension) Collector() *DataCollector {
	return e.collector
}

// History returns the data history instance (for advanced usage).
func (e *Extension) History() *DataHistory {
	return e.history
}

// registerRoutes registers dashboard routes with the app's router.
func (e *Extension) registerRoutes() {
	router := e.app.Router()
	base := e.config.BasePath

	// Main dashboard page
	router.GET(base, e.handleIndex)

	// API endpoints
	router.GET(base+"/api/overview", e.handleAPIOverview)
	router.GET(base+"/api/health", e.handleAPIHealth)
	router.GET(base+"/api/metrics", e.handleAPIMetrics)
	router.GET(base+"/api/services", e.handleAPIServices)
	router.GET(base+"/api/history", e.handleAPIHistory)
	router.GET(base+"/api/service-detail", e.handleAPIServiceDetail)
	router.GET(base+"/api/metrics-report", e.handleAPIMetricsReport)

	// Export endpoints
	if e.config.EnableExport {
		router.GET(base+"/export/json", e.handleExportJSON)
		router.GET(base+"/export/csv", e.handleExportCSV)
		router.GET(base+"/export/prometheus", e.handleExportPrometheus)
	}

	// WebSocket endpoint
	if e.config.EnableRealtime && e.hub != nil {
		router.GET(base+"/ws", e.handleWebSocket)
	}

	e.Logger().Debug("dashboard routes registered",
		forge.F("base_path", base),
		forge.F("realtime", e.config.EnableRealtime),
		forge.F("export", e.config.EnableExport),
	)
}

// broadcastUpdates periodically broadcasts updates to WebSocket clients.
func (e *Extension) broadcastUpdates(ctx context.Context) {
	ticker := time.NewTicker(e.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.hub.ClientCount() == 0 {
				continue
			}

			// Collect and broadcast overview data
			overview := e.collector.CollectOverview(ctx)

			msg := NewWSMessage("overview", overview)
			if err := e.hub.Broadcast(msg); err != nil {
				e.Logger().Error("failed to broadcast overview", forge.F("error", err))
			}

			// Collect and broadcast health data
			health := e.collector.CollectHealth(ctx)

			healthMsg := NewWSMessage("health", health)
			if err := e.hub.Broadcast(healthMsg); err != nil {
				e.Logger().Error("failed to broadcast health", forge.F("error", err))
			}

			// Collect and broadcast metrics data
			metrics := e.collector.CollectMetrics(ctx)

			metricsMsg := NewWSMessage("metrics", metrics)
			if err := e.hub.Broadcast(metricsMsg); err != nil {
				e.Logger().Error("failed to broadcast metrics", forge.F("error", err))
			}

			// Collect and broadcast services data
			services := e.collector.CollectServices(ctx)

			servicesMsg := NewWSMessage("services", services)
			if err := e.hub.Broadcast(servicesMsg); err != nil {
				e.Logger().Error("failed to broadcast services", forge.F("error", err))
			}
		}
	}
}
