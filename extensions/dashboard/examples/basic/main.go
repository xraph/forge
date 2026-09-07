// Package main demonstrates a basic dashboard setup with default configuration
// and no contributors registered.
//
// This example creates a Forge application with the dashboard extension.
// The dashboard is the prebuilt React shell, mounted at {BasePath}/ui, which
// covers overview, health checks, metrics, and services. The same data is
// also available directly as JSON under {BasePath}/api.
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
	// The React shell (overview, health, metrics, services) and its JSON API
	// are mounted automatically; no contributor registration is required.
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
	//   http://localhost:8080/dashboard/ui
	//
	// JSON API endpoints (same data as the shell):
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
