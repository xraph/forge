package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/shared"
)

// DashboardServer serves the dashboard HTTP interface.
type DashboardServer struct {
	config        Config
	healthManager forge.HealthManager
	metrics       forge.Metrics
	logger        forge.Logger
	container     shared.Container

	server    *http.Server
	mux       *http.ServeMux
	running   bool
	startTime time.Time

	collector *DataCollector
	history   *DataHistory
	hub       *Hub

	mu sync.RWMutex
}

// NewDashboardServer creates a new dashboard server.
func NewDashboardServer(
	config Config,
	healthManager forge.HealthManager,
	metrics forge.Metrics,
	logger forge.Logger,
	container shared.Container,
) *DashboardServer {
	history := NewDataHistory(config.MaxDataPoints, config.HistoryDuration)

	ds := &DashboardServer{
		config:        config,
		healthManager: healthManager,
		metrics:       metrics,
		logger:        logger,
		container:     container,
		mux:           http.NewServeMux(),
		history:       history,
	}

	// Create data collector
	ds.collector = NewDataCollector(healthManager, metrics, container, logger, history)

	// Create WebSocket hub if real-time is enabled
	if config.EnableRealtime {
		ds.hub = NewHub(logger)
	}

	// Setup HTTP routes
	ds.setupRoutes()

	return ds
}

// Start starts the dashboard server.
func (ds *DashboardServer) Start(ctx context.Context) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if ds.running {
		return errors.New("dashboard server already running")
	}

	// Create HTTP server
	ds.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", ds.config.Port),
		Handler:      ds.mux,
		ReadTimeout:  ds.config.ReadTimeout,
		WriteTimeout: ds.config.WriteTimeout,
	}

	// Start WebSocket hub if enabled
	if ds.hub != nil {
		go ds.hub.Run()
		go ds.broadcastUpdates(ctx)
	}

	// Start data collection
	go ds.collector.Start(ctx, ds.config.RefreshInterval)

	// Start server in goroutine
	go func() {
		ds.logger.Info("dashboard server listening",
			forge.F("addr", ds.server.Addr),
			forge.F("base_path", ds.config.BasePath),
		)

		if err := ds.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ds.logger.Error("dashboard server error", forge.F("error", err))
		}
	}()

	ds.running = true
	ds.startTime = time.Now()

	return nil
}

// Stop stops the dashboard server.
func (ds *DashboardServer) Stop(ctx context.Context) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !ds.running {
		return nil
	}

	// Stop data collection
	ds.collector.Stop()

	// Shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(ctx, ds.config.ShutdownTimeout)
	defer cancel()

	if err := ds.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown dashboard server: %w", err)
	}

	ds.running = false
	ds.logger.Info("dashboard server stopped")

	return nil
}

// IsRunning returns true if the server is running.
func (ds *DashboardServer) IsRunning() bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	return ds.running
}

// setupRoutes configures HTTP routes.
func (ds *DashboardServer) setupRoutes() {
	base := ds.config.BasePath

	// UI route
	ds.mux.HandleFunc(base+"/", ds.handleIndex)

	// API routes
	ds.mux.HandleFunc(base+"/api/overview", ds.handleAPIOverview)
	ds.mux.HandleFunc(base+"/api/health", ds.handleAPIHealth)
	ds.mux.HandleFunc(base+"/api/metrics", ds.handleAPIMetrics)
	ds.mux.HandleFunc(base+"/api/services", ds.handleAPIServices)
	ds.mux.HandleFunc(base+"/api/history", ds.handleAPIHistory)
	ds.mux.HandleFunc(base+"/api/service-detail", ds.handleAPIServiceDetail)
	ds.mux.HandleFunc(base+"/api/metrics-report", ds.handleAPIMetricsReport)

	// Export routes
	if ds.config.EnableExport {
		ds.mux.HandleFunc(base+"/export/json", ds.handleExportJSON)
		ds.mux.HandleFunc(base+"/export/csv", ds.handleExportCSV)
		ds.mux.HandleFunc(base+"/export/prometheus", ds.handleExportPrometheus)
	}

	// WebSocket route
	if ds.config.EnableRealtime {
		ds.mux.HandleFunc(base+"/ws", ds.handleWebSocket)
	}
}

// broadcastUpdates periodically broadcasts updates to WebSocket clients.
func (ds *DashboardServer) broadcastUpdates(ctx context.Context) {
	ticker := time.NewTicker(ds.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ds.hub.ClientCount() == 0 {
				continue
			}

			// Collect and broadcast overview data
			overview := ds.collector.CollectOverview(ctx)

			msg := NewWSMessage("overview", overview)
			if err := ds.hub.Broadcast(msg); err != nil {
				ds.logger.Error("failed to broadcast overview",
					forge.F("error", err),
				)
			}

			// Collect and broadcast health data
			health := ds.collector.CollectHealth(ctx)

			healthMsg := NewWSMessage("health", health)
			if err := ds.hub.Broadcast(healthMsg); err != nil {
				ds.logger.Error("failed to broadcast health",
					forge.F("error", err),
				)
			}

			// Collect and broadcast metrics data
			metrics := ds.collector.CollectMetrics(ctx)

			metricsMsg := NewWSMessage("metrics", metrics)
			if err := ds.hub.Broadcast(metricsMsg); err != nil {
				ds.logger.Error("failed to broadcast metrics",
					forge.F("error", err),
				)
			}

			// Collect and broadcast services data
			services := ds.collector.CollectServices(ctx)

			servicesMsg := NewWSMessage("services", services)
			if err := ds.hub.Broadcast(servicesMsg); err != nil {
				ds.logger.Error("failed to broadcast services",
					forge.F("error", err),
				)
			}
		}
	}
}

// GetHistory returns the data history.
func (ds *DashboardServer) GetHistory() *DataHistory {
	return ds.history
}

// GetCollector returns the data collector.
func (ds *DashboardServer) GetCollector() *DataCollector {
	return ds.collector
}
