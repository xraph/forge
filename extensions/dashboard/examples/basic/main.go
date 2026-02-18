// Package main demonstrates a basic dashboard setup with built-in pages only.
//
// This example creates a Forge application with the dashboard extension
// using default configuration. The dashboard will provide built-in pages
// for overview, health checks, metrics, and services.
//
// NOTE: This is an illustrative stub. It requires a full Forge application
// environment to run.
package main

import (
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard"
)

func main() {
	// Create a Forge application using functional options.
	app := forge.New(
		forge.WithAppName("basic-dashboard-example"),
		forge.WithAppVersion("1.0.0"),
	)

	// Register the dashboard extension with custom configuration.
	// All built-in pages (overview, health, metrics, services) are included automatically.
	if err := app.RegisterExtension(dashboard.NewExtension(
		dashboard.WithTitle("My Dashboard"),
		dashboard.WithBasePath("/dashboard"),
		dashboard.WithTheme("auto"),
		dashboard.WithRealtime(true),
		dashboard.WithRefreshInterval(30*time.Second),
		dashboard.WithExport(true),
		dashboard.WithHistoryDuration(1*time.Hour),
		dashboard.WithMaxDataPoints(1000),
	)); err != nil {
		log.Fatalf("failed to register dashboard extension: %v", err)
	}

	// Start the application. The dashboard will be available at:
	//   http://localhost:8080/dashboard
	//
	// Built-in API endpoints:
	//   GET /dashboard/api/overview
	//   GET /dashboard/api/health
	//   GET /dashboard/api/metrics
	//   GET /dashboard/api/services
	//   GET /dashboard/api/history
	//
	// Export endpoints (when enabled):
	//   GET /dashboard/export/json
	//   GET /dashboard/export/csv
	//   GET /dashboard/export/prometheus
	//
	// Real-time events (when enabled):
	//   SSE /dashboard/sse
	if err := app.Run(); err != nil {
		log.Fatalf("application error: %v", err)
	}
}
