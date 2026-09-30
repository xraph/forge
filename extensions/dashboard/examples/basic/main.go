// Package main demonstrates a basic dashboard setup with default configuration
// and no contributors registered.
//
// This example creates a Forge application with the dashboard extension. The
// extension serves the dashboard's data, not its pages: overview, health,
// metrics, services and traces come back as contract intents from
// {BasePath}/api/dashboard/v1. Build the UI with `forge dashboard new` and
// point it at that endpoint.
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

	// Register the dashboard extension with custom configuration. The core
	// intents are registered automatically; nothing else is required.
	if err := app.RegisterExtension(dashboard.NewExtension(
		dashboard.WithBasePath("/dashboard"),
		dashboard.WithRefreshInterval(30*time.Second),
		dashboard.WithExport(true),
		dashboard.WithHistoryDuration(1*time.Hour),
		dashboard.WithMaxDataPoints(1000),
	)); err != nil {
		log.Fatalf("failed to register dashboard extension: %v", err)
	}

	// Start the application. The dashboard's data is served at:
	//   POST /dashboard/api/dashboard/v1               contract envelope
	//   GET  /dashboard/api/dashboard/v1/capabilities  registered contributors
	//   GET  /dashboard/api/dashboard/v1/stream        subscriptions (SSE)
	//
	// Export endpoints (when enabled):
	//   GET /dashboard/export/json
	//   GET /dashboard/export/csv
	//   GET /dashboard/export/prometheus
	if err := app.Run(); err != nil {
		log.Fatalf("application error: %v", err)
	}
}
