package handlers

import (
	"github.com/xraph/forge"
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
