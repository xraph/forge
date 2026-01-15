package main

import (
	"context"
	"time"

	forge "github.com/xraph/forge"
)

// This example demonstrates observability features:
// - Metrics collection
// - Health checks
// - Prometheus export
// - Application information

func main() {
	// Create app with observability enabled
	app := forge.NewApp(forge.AppConfig{
		Name:        "observability-example",
		Version:     "1.0.0",
		Description: "Example demonstrating observability features",
		Environment: "production",

		// Enable metrics
		MetricsConfig: forge.MetricsConfig{
			Enabled: true,
			Collection: forge.MetricsCollection{
				Interval:  15 * time.Second,
				Namespace: "example",
				Path:      "/_/metrics",
				DefaultTags: map[string]string{
					"environment": "production",
				},
			},
		},

		// Enable health checks
		HealthConfig: forge.HealthConfig{
			Enabled: true,
			Features: forge.HealthFeatures{
				CheckInterval:          30 * time.Second,
				ReportInterval:         60 * time.Second,
				DefaultTimeout:         5 * time.Second,
				EnableEndpoints:        true,
				EndpointPrefix:         "/_",
				EnableAutoDiscovery:    true,
				MaxConcurrentChecks:    10,
				EnableSmartAggregation: true,
			},
			CheckInterval:          30 * time.Second,
			ReportInterval:         60 * time.Second,
			DefaultTimeout:         5 * time.Second,
			EnableEndpoints:        true,
			EndpointPrefix:         "/_",
			EnableAutoDiscovery:    true,
			MaxConcurrentChecks:    10,
			EnableSmartAggregation: true,
		},
	})

	// Register health checks
	healthManager := app.HealthManager()

	// Database health check
	healthManager.RegisterFn("database", func(ctx context.Context) *forge.HealthResult {
		// Simulate database ping
		time.Sleep(50 * time.Millisecond)

		return &forge.HealthResult{
			Status:  forge.HealthStatusHealthy,
			Message: "database connection OK",
			Details: map[string]any{
				"connections": 10,
				"ping_ms":     5,
			},
		}
	})

	// Cache health check
	healthManager.RegisterFn("cache", func(ctx context.Context) *forge.HealthResult {
		return &forge.HealthResult{
			Status:  forge.HealthStatusHealthy,
			Message: "cache connection OK",
			Details: map[string]any{
				"hit_rate": 0.95,
			},
		}
	})

	// External API health check
	healthManager.RegisterFn("external_api", func(ctx context.Context) *forge.HealthResult {
		// Simulate API check
		return &forge.HealthResult{
			Status:  forge.HealthStatusHealthy,
			Message: "external API reachable",
		}
	})

	// Register routes with metrics
	router := app.Router()
	metrics := app.Metrics()

	router.GET("/", func(ctx forge.Context) error {
		// Increment request counter
		metrics.Counter("requests_total").WithLabels(map[string]string{
			"endpoint": "root",
		}).Inc()

		return ctx.JSON(200, map[string]string{
			"message": "Observability Example",
			"app":     app.Name(),
		})
	})

	router.GET("/api/data", func(ctx forge.Context) error {
		// Track request with metrics
		counter := metrics.Counter("api_requests_total")
		counter.Inc()

		// Track duration
		histogram := metrics.Histogram("api_request_duration_seconds")
		start := time.Now()
		defer histogram.ObserveDuration(start)

		// Simulate work
		time.Sleep(100 * time.Millisecond)

		// Track active requests (gauge)
		gauge := metrics.Gauge("api_active_requests")
		gauge.Inc()
		defer gauge.Dec()

		return ctx.JSON(200, map[string]interface{}{
			"data": "sample data",
			"time": time.Now(),
		})
	})

	// Built-in observability endpoints:
	// - /_/info          - Application information
	// - /_/metrics       - Prometheus metrics
	// - /_/health        - Aggregate health status
	// - /_/health/live   - Liveness probe (always returns 200 if server is up)
	// - /_/health/ready  - Readiness probe (returns 200 if all checks pass)

	app.Logger().Info("starting observability example",
		forge.F("metrics_enabled", true),
		forge.F("health_enabled", true),
	)

	// Run the application
	if err := app.Run(); err != nil {
		app.Logger().Fatal("app failed", forge.F("error", err))
	}
}
