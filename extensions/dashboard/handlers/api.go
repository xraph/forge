package handlers

import (
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/collector"
)

// HandleAPIOverview returns overview data as JSON.
func HandleAPIOverview(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		overview := deps.Collector.CollectOverview(ctx.Context())

		return ctx.JSON(200, overview)
	}
}

// HandleAPIHealth returns health check data as JSON.
func HandleAPIHealth(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		health := deps.Collector.CollectHealth(ctx.Context())

		return ctx.JSON(200, health)
	}
}

// HandleAPIMetrics returns metrics data as JSON.
func HandleAPIMetrics(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		metrics := deps.Collector.CollectMetrics(ctx.Context())

		return ctx.JSON(200, metrics)
	}
}

// HandleAPIServices returns service list as JSON.
func HandleAPIServices(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		services := deps.Collector.CollectServices(ctx.Context())

		return ctx.JSON(200, services)
	}
}

// HandleAPIHistory returns historical data as JSON.
func HandleAPIHistory(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		history := deps.History.GetAll()

		return ctx.JSON(200, history)
	}
}

// HandleAPIServiceDetail returns detailed information about a specific service.
func HandleAPIServiceDetail(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		serviceName := ctx.Query("name")
		if serviceName == "" {
			return forge.BadRequest("service name is required")
		}

		detail := deps.Collector.CollectServiceDetail(ctx.Context(), serviceName)

		return ctx.JSON(200, detail)
	}
}

// HandleAPIMetricsReport returns comprehensive metrics report.
func HandleAPIMetricsReport(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		report := deps.Collector.CollectMetricsReport(ctx.Context())

		return ctx.JSON(200, report)
	}
}

// HandleAPITraces returns a filtered list of traces and aggregate stats.
func HandleAPITraces(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		if deps.TraceStore == nil {
			return ctx.JSON(200, map[string]any{"traces": []any{}, "stats": collector.TraceStats{}})
		}

		filter := collector.TraceFilter{
			Search:   ctx.Query("search"),
			Status:   ctx.Query("status"),
			Protocol: ctx.Query("protocol"),
			Limit:    50,
		}

		if d := ctx.Query("min_duration"); d != "" {
			if dur, err := time.ParseDuration(d); err == nil {
				filter.MinDuration = dur
			}
		}

		traces, stats := deps.TraceStore.ListTraces(filter)

		return ctx.JSON(200, map[string]any{
			"traces": traces,
			"stats":  stats,
		})
	}
}

// HandleAPIExtensions returns a list of all registered extensions as JSON.
func HandleAPIExtensions(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		names := deps.Registry.ContributorNames()
		result := make([]map[string]any, 0, len(names))

		for _, name := range names {
			m, ok := deps.Registry.GetManifest(name)
			if !ok {
				continue
			}

			displayName := m.DisplayName
			if displayName == "" {
				displayName = name
			}

			result = append(result, map[string]any{
				"name":         m.Name,
				"display_name": displayName,
				"version":      m.Version,
				"icon":         m.Icon,
				"layout":       m.Layout,
				"widgets":      len(m.Widgets),
				"pages":        len(m.Nav),
			})
		}

		return ctx.JSON(200, result)
	}
}

// HandleAPITraceDetail returns the full detail for a single trace.
func HandleAPITraceDetail(deps *Deps) forge.Handler {
	return func(ctx forge.Context) error {
		if deps.TraceStore == nil {
			return forge.NotFound("tracing not enabled")
		}

		traceID := ctx.Query("id")
		if traceID == "" {
			return forge.BadRequest("trace id is required")
		}

		detail := deps.TraceStore.GetTrace(traceID)
		if detail == nil {
			return forge.NotFound("trace not found")
		}

		return ctx.JSON(200, detail)
	}
}
