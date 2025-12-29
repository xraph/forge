package dashboard

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/xraph/forge"
)

// handleIndex serves the main dashboard HTML.
func (e *Extension) handleIndex(ctx forge.Context) error {
	html := e.generateHTML()
	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	ctx.SetHeader("Cache-Control", "no-cache, no-store, must-revalidate")
	return ctx.String(200, html)
}

// handleAPIOverview returns overview data as JSON.
func (e *Extension) handleAPIOverview(ctx forge.Context) error {
	overview := e.collector.CollectOverview(ctx.Context())
	return ctx.JSON(200, overview)
}

// handleAPIHealth returns health check data as JSON.
func (e *Extension) handleAPIHealth(ctx forge.Context) error {
	health := e.collector.CollectHealth(ctx.Context())
	return ctx.JSON(200, health)
}

// handleAPIMetrics returns metrics data as JSON.
func (e *Extension) handleAPIMetrics(ctx forge.Context) error {
	metrics := e.collector.CollectMetrics(ctx.Context())
	return ctx.JSON(200, metrics)
}

// handleAPIServices returns service list as JSON.
func (e *Extension) handleAPIServices(ctx forge.Context) error {
	services := e.collector.CollectServices(ctx.Context())
	return ctx.JSON(200, services)
}

// handleAPIHistory returns historical data as JSON.
func (e *Extension) handleAPIHistory(ctx forge.Context) error {
	history := e.history.GetAll()
	return ctx.JSON(200, history)
}

// handleAPIServiceDetail returns detailed information about a specific service.
func (e *Extension) handleAPIServiceDetail(ctx forge.Context) error {
	serviceName := ctx.Query("name")
	if serviceName == "" {
		return forge.BadRequest("service name is required")
	}

	detail := e.collector.CollectServiceDetail(ctx.Context(), serviceName)
	return ctx.JSON(200, detail)
}

// handleAPIMetricsReport returns comprehensive metrics report.
func (e *Extension) handleAPIMetricsReport(ctx forge.Context) error {
	report := e.collector.CollectMetricsReport(ctx.Context())
	return ctx.JSON(200, report)
}

// handleExportJSON exports dashboard data as JSON.
func (e *Extension) handleExportJSON(ctx forge.Context) error {
	data := e.collector.CollectOverview(ctx.Context())
	ctx.SetHeader("Content-Disposition", "attachment; filename=dashboard-export.json")
	return ctx.JSON(200, data)
}

// handleExportCSV exports dashboard data as CSV.
func (e *Extension) handleExportCSV(ctx forge.Context) error {
	data := e.collector.CollectOverview(ctx.Context())
	csv := dashboardExportToCSV(*data)

	ctx.SetHeader("Content-Type", "text/csv")
	ctx.SetHeader("Content-Disposition", "attachment; filename=dashboard-export.csv")

	return ctx.String(200, csv)
}

// handleExportPrometheus exports metrics in Prometheus format.
func (e *Extension) handleExportPrometheus(ctx forge.Context) error {
	metrics := e.collector.CollectMetrics(ctx.Context())
	prometheus := dashboardExportToPrometheus(*metrics)

	ctx.SetHeader("Content-Type", "text/plain; version=0.0.4")

	return ctx.String(200, prometheus)
}

// handleWebSocket handles WebSocket connections.
func (e *Extension) handleWebSocket(ctx forge.Context) error {
	if e.hub == nil {
		return forge.NewHTTPError(http.StatusServiceUnavailable, "WebSocket not enabled")
	}

	// Get underlying ResponseWriter and Request for WebSocket upgrade
	w := ctx.Response()
	r := ctx.Request()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		e.Logger().Error("websocket upgrade failed", forge.F("error", err))
		return err
	}

	client := NewClient(e.hub, conn)
	e.hub.register <- client
	client.Start()

	return nil
}

// generateHTML generates the dashboard HTML page.
func (e *Extension) generateHTML() string {
	// Use the existing template generation from templates.go
	return GenerateDashboardHTML(e.config)
}

// Helper function for exporting to CSV (dashboard-specific to avoid conflicts).
func dashboardExportToCSV(data OverviewData) string {
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

// Helper function for exporting to Prometheus format (dashboard-specific).
func dashboardExportToPrometheus(metrics MetricsData) string {
	var sb strings.Builder
	sb.WriteString("# HELP dashboard_metrics Dashboard metrics\n")
	sb.WriteString("# TYPE dashboard_metrics gauge\n")

	for name, value := range metrics.Metrics {
		// Simple conversion - in production, handle different metric types
		sb.WriteString(fmt.Sprintf("dashboard_metrics{name=\"%s\"} %v\n", name, value))
	}

	return sb.String()
}
