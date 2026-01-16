package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	g "maragu.dev/gomponents"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/ui"
	"github.com/xraph/forgeui/router"
)

// ForgeUIIntegration provides ForgeUI-specific functionality for the dashboard.
type ForgeUIIntegration struct {
	config        Config
	collector     *DataCollector
	history       *DataHistory
	hub           *Hub
	healthManager forge.HealthManager
	metrics       forge.Metrics
	logger        forge.Logger
}

// NewForgeUIIntegration creates a new ForgeUI integration for the dashboard.
func NewForgeUIIntegration(
	config Config,
	healthManager forge.HealthManager,
	metrics forge.Metrics,
	logger forge.Logger,
	container forge.Container,
) *ForgeUIIntegration {
	history := NewDataHistory(config.MaxDataPoints, config.HistoryDuration)
	collector := NewDataCollector(healthManager, metrics, container, logger, history)

	var hub *Hub
	if config.EnableRealtime {
		hub = NewHub(logger)
	}

	return &ForgeUIIntegration{
		config:        config,
		collector:     collector,
		history:       history,
		hub:           hub,
		healthManager: healthManager,
		metrics:       metrics,
		logger:        logger,
	}
}

// RegisterRoutes registers dashboard routes with a ForgeUI router.
func (fi *ForgeUIIntegration) RegisterRoutes(r *router.Router) {
	basePath := fi.config.BasePath

	// Main dashboard page
	r.Get(basePath, ui.DashboardPageHandler(
		fi.config.Title,
		fi.config.BasePath,
		fi.config.EnableRealtime,
		fi.config.EnableExport,
	))

	// API endpoints
	r.Get(basePath+"/api/overview", fi.handleAPIOverviewForgeUI)
	r.Get(basePath+"/api/health", fi.handleAPIHealthForgeUI)
	r.Get(basePath+"/api/metrics", fi.handleAPIMetricsForgeUI)
	r.Get(basePath+"/api/services", fi.handleAPIServicesForgeUI)
	r.Get(basePath+"/api/history", fi.handleAPIHistoryForgeUI)
	r.Get(basePath+"/api/service-detail", fi.handleAPIServiceDetailForgeUI)
	r.Get(basePath+"/api/metrics-report", fi.handleAPIMetricsReportForgeUI)

	// Export endpoints
	if fi.config.EnableExport {
		r.Get(basePath+"/export/json", fi.handleExportJSONForgeUI)
		r.Get(basePath+"/export/csv", fi.handleExportCSVForgeUI)
		r.Get(basePath+"/export/prometheus", fi.handleExportPrometheusForgeUI)
	}

	// WebSocket endpoint
	if fi.config.EnableRealtime && fi.hub != nil {
		// Note: WebSocket handling needs special setup
		// This would need to be handled differently in ForgeUI
		r.Get(basePath+"/ws", fi.handleWebSocketForgeUI)
	}
}

// Start starts background services (data collection, WebSocket hub).
func (fi *ForgeUIIntegration) Start(ctx context.Context) error {
	// Start data collection
	go fi.collector.Start(ctx, fi.config.RefreshInterval)

	// Start WebSocket hub if enabled
	if fi.hub != nil {
		go fi.hub.Run()
		go fi.broadcastUpdates(ctx)
	}

	fi.logger.Info("dashboard ForgeUI integration started",
		forge.F("base_path", fi.config.BasePath),
		forge.F("realtime", fi.config.EnableRealtime),
	)

	return nil
}

// Stop stops background services.
func (fi *ForgeUIIntegration) Stop(ctx context.Context) error {
	fi.collector.Stop()
	fi.logger.Info("dashboard ForgeUI integration stopped")

	return nil
}

// API handlers for ForgeUI router

func (fi *ForgeUIIntegration) handleAPIOverviewForgeUI(ctx *router.PageContext) (g.Node, error) {
	overview := fi.collector.CollectOverview(ctx.Request.Context())

	return respondJSON(ctx.ResponseWriter, overview), nil
}

func (fi *ForgeUIIntegration) handleAPIHealthForgeUI(ctx *router.PageContext) (g.Node, error) {
	health := fi.collector.CollectHealth(ctx.Request.Context())

	return respondJSON(ctx.ResponseWriter, health), nil
}

func (fi *ForgeUIIntegration) handleAPIMetricsForgeUI(ctx *router.PageContext) (g.Node, error) {
	metrics := fi.collector.CollectMetrics(ctx.Request.Context())

	return respondJSON(ctx.ResponseWriter, metrics), nil
}

func (fi *ForgeUIIntegration) handleAPIServicesForgeUI(ctx *router.PageContext) (g.Node, error) {
	services := fi.collector.CollectServices(ctx.Request.Context())

	return respondJSON(ctx.ResponseWriter, services), nil
}

func (fi *ForgeUIIntegration) handleAPIHistoryForgeUI(ctx *router.PageContext) (g.Node, error) {
	history := fi.history.GetAll()

	return respondJSON(ctx.ResponseWriter, history), nil
}

func (fi *ForgeUIIntegration) handleAPIServiceDetailForgeUI(ctx *router.PageContext) (g.Node, error) {
	serviceName := ctx.Query("name")
	if serviceName == "" {
		ctx.ResponseWriter.WriteHeader(http.StatusBadRequest)

		return respondJSON(ctx.ResponseWriter, map[string]string{"error": "service name is required"}), nil
	}

	detail := fi.collector.CollectServiceDetail(ctx.Request.Context(), serviceName)

	return respondJSON(ctx.ResponseWriter, detail), nil
}

func (fi *ForgeUIIntegration) handleAPIMetricsReportForgeUI(ctx *router.PageContext) (g.Node, error) {
	report := fi.collector.CollectMetricsReport(ctx.Request.Context())

	return respondJSON(ctx.ResponseWriter, report), nil
}

func (fi *ForgeUIIntegration) handleExportJSONForgeUI(ctx *router.PageContext) (g.Node, error) {
	data := fi.collector.CollectOverview(ctx.Request.Context())
	ctx.ResponseWriter.Header().Set("Content-Disposition", "attachment; filename=dashboard-export.json")

	return respondJSON(ctx.ResponseWriter, data), nil
}

func (fi *ForgeUIIntegration) handleExportCSVForgeUI(ctx *router.PageContext) (g.Node, error) {
	// Export CSV functionality
	data := fi.collector.CollectOverview(ctx.Request.Context())
	csv := exportToCSV(*data)

	ctx.ResponseWriter.Header().Set("Content-Type", "text/csv")
	ctx.ResponseWriter.Header().Set("Content-Disposition", "attachment; filename=dashboard-export.csv")

	if _, err := ctx.ResponseWriter.Write([]byte(csv)); err != nil {
		return nil, err
	}

	return nil, nil //nolint:nilnil // No HTML node returned for raw response
}

func (fi *ForgeUIIntegration) handleExportPrometheusForgeUI(ctx *router.PageContext) (g.Node, error) {
	// Export Prometheus format
	metrics := fi.collector.CollectMetrics(ctx.Request.Context())
	prometheus := exportToPrometheus(*metrics)

	ctx.ResponseWriter.Header().Set("Content-Type", "text/plain; version=0.0.4")

	if _, err := ctx.ResponseWriter.Write([]byte(prometheus)); err != nil {
		return nil, err
	}

	return nil, nil //nolint:nilnil // No HTML node returned for raw response
}

func (fi *ForgeUIIntegration) handleWebSocketForgeUI(ctx *router.PageContext) (g.Node, error) {
	if fi.hub == nil {
		ctx.ResponseWriter.WriteHeader(http.StatusServiceUnavailable)

		return nil, nil //nolint:nilnil // No HTML node returned for WebSocket upgrade
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(ctx.ResponseWriter, ctx.Request, nil)
	if err != nil {
		fi.logger.Error("websocket upgrade failed", forge.F("error", err))

		return nil, err
	}

	client := NewClient(fi.hub, conn)
	fi.hub.register <- client

	client.Start()

	return nil, nil //nolint:nilnil // No HTML node returned after WebSocket upgrade
}

// broadcastUpdates periodically broadcasts updates to WebSocket clients.
func (fi *ForgeUIIntegration) broadcastUpdates(ctx context.Context) {
	ticker := time.NewTicker(fi.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if fi.hub.ClientCount() == 0 {
				continue
			}

			// Collect and broadcast overview data
			overview := fi.collector.CollectOverview(ctx)

			msg := NewWSMessage("overview", overview)
			if err := fi.hub.Broadcast(msg); err != nil {
				fi.logger.Error("failed to broadcast overview", forge.F("error", err))
			}

			// Collect and broadcast health data
			health := fi.collector.CollectHealth(ctx)

			healthMsg := NewWSMessage("health", health)
			if err := fi.hub.Broadcast(healthMsg); err != nil {
				fi.logger.Error("failed to broadcast health", forge.F("error", err))
			}

			// Collect and broadcast metrics data
			metrics := fi.collector.CollectMetrics(ctx)

			metricsMsg := NewWSMessage("metrics", metrics)
			if err := fi.hub.Broadcast(metricsMsg); err != nil {
				fi.logger.Error("failed to broadcast metrics", forge.F("error", err))
			}

			// Collect and broadcast services data
			services := fi.collector.CollectServices(ctx)

			servicesMsg := NewWSMessage("services", services)
			if err := fi.hub.Broadcast(servicesMsg); err != nil {
				fi.logger.Error("failed to broadcast services", forge.F("error", err))
			}
		}
	}
}

// Helper functions

func respondJSON(w http.ResponseWriter, data any) g.Node {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	return nil
}

func exportToCSV(data OverviewData) string {
	var sb strings.Builder
	sb.WriteString("Metric,Value\n")
	sb.WriteString(fmt.Sprintf("Overall Health,%s\n", data.OverallHealth))
	sb.WriteString(fmt.Sprintf("Total Services,%d\n", data.TotalServices))
	sb.WriteString(fmt.Sprintf("Healthy Services,%d\n", data.HealthyServices))
	sb.WriteString(fmt.Sprintf("Total Metrics,%d\n", data.TotalMetrics))
	sb.WriteString(fmt.Sprintf("Uptime,%d\n", data.Uptime))
	sb.WriteString(fmt.Sprintf("Version,%s\n", data.Version))
	sb.WriteString(fmt.Sprintf("Environment,%s\n", data.Environment))

	return sb.String()
}

func exportToPrometheus(metrics MetricsData) string {
	var sb strings.Builder
	sb.WriteString("# HELP dashboard_metrics Dashboard metrics\n")
	sb.WriteString("# TYPE dashboard_metrics gauge\n")

	for name, value := range metrics.Metrics {
		// Simple conversion - in production, handle different metric types
		sb.WriteString(fmt.Sprintf("dashboard_metrics{name=\"%s\"} %v\n", name, value))
	}

	return sb.String()
}
