package handlers

import (
	"encoding/csv"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/collector"
)

// HandleExportJSON exports dashboard data as JSON.
func HandleExportJSON(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		snapshot := buildSnapshot(deps, ctx)

		ctx.SetHeader("Content-Type", "application/json")
		ctx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=dashboard-export-%s.json", time.Now().Format("2006-01-02-150405")))

		return ctx.JSON(200, snapshot)
	}
}

// HandleExportCSV exports dashboard service data as CSV.
func HandleExportCSV(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		services := deps.Collector.CollectServices(ctx.Context())

		ctx.SetHeader("Content-Type", "text/csv")
		ctx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=services-export-%s.csv", time.Now().Format("2006-01-02-150405")))

		w := csv.NewWriter(ctx.Response())
		defer w.Flush()

		// Header
		if err := w.Write([]string{"Name", "Type", "Status", "Registered At"}); err != nil {
			return fmt.Errorf("failed to write CSV header: %w", err)
		}

		// Rows
		for _, svc := range services {
			registeredAt := ""
			if !svc.RegisteredAt.IsZero() {
				registeredAt = svc.RegisteredAt.Format(time.RFC3339)
			}

			if err := w.Write([]string{svc.Name, svc.Type, svc.Status, registeredAt}); err != nil {
				return fmt.Errorf("failed to write CSV row: %w", err)
			}
		}

		return nil
	}
}

// HandleExportPrometheus exports metrics in Prometheus text format.
func HandleExportPrometheus(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		metrics := deps.Collector.CollectMetrics(ctx.Context())
		overview := deps.Collector.CollectOverview(ctx.Context())

		ctx.SetHeader("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		lines := ""

		// Overview metrics
		lines += fmt.Sprintf("# HELP forge_services_total Total number of registered services\n")
		lines += fmt.Sprintf("# TYPE forge_services_total gauge\n")
		lines += fmt.Sprintf("forge_services_total %d\n\n", overview.TotalServices)

		lines += fmt.Sprintf("# HELP forge_services_healthy Number of healthy services\n")
		lines += fmt.Sprintf("# TYPE forge_services_healthy gauge\n")
		lines += fmt.Sprintf("forge_services_healthy %d\n\n", overview.HealthyServices)

		lines += fmt.Sprintf("# HELP forge_metrics_total Total number of tracked metrics\n")
		lines += fmt.Sprintf("# TYPE forge_metrics_total gauge\n")
		lines += fmt.Sprintf("forge_metrics_total %d\n\n", metrics.Stats.TotalMetrics)

		lines += fmt.Sprintf("# HELP forge_uptime_seconds System uptime in seconds\n")
		lines += fmt.Sprintf("# TYPE forge_uptime_seconds gauge\n")
		lines += fmt.Sprintf("forge_uptime_seconds %.0f\n\n", overview.Uptime.Seconds())

		return ctx.String(200, lines)
	}
}

// buildSnapshot builds a complete dashboard snapshot for export.
func buildSnapshot(deps *Deps, ctx forge.Context) collector.DashboardSnapshot {
	return collector.DashboardSnapshot{
		Timestamp:   time.Now(),
		Overview:    *deps.Collector.CollectOverview(ctx.Context()),
		Health:      *deps.Collector.CollectHealth(ctx.Context()),
		Metrics:     *deps.Collector.CollectMetrics(ctx.Context()),
		Services:    deps.Collector.CollectServices(ctx.Context()),
		GeneratedBy: "Forge Dashboard v3.0.0",
	}
}
