package main

import (
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/cache"
	"github.com/xraph/forge/extensions/dashboard"
)

func main() {
	// Create Forge app with configuration
	app := forge.NewApp(forge.AppConfig{
		Name:        "dashboard-demo",
		Version:     "1.0.0",
		Environment: "development",
		HTTPAddress: ":8080",
	})

	// Register some extensions for the dashboard to monitor
	// These will show up in the health checks and service list
	app.RegisterExtension(cache.NewExtension(
		cache.WithDriver("inmemory"),
		cache.WithDefaultTTL(5*time.Minute),
	))

	// Register the dashboard extension
	// The dashboard will monitor all registered services and use the app's router
	app.RegisterExtension(dashboard.NewExtension(
		dashboard.WithBasePath("/dashboard"),
		dashboard.WithTitle("My Application Dashboard"),
		dashboard.WithTheme("auto"), // auto, light, or dark
		dashboard.WithRealtime(true),
		dashboard.WithRefreshInterval(30*time.Second),
		dashboard.WithExport(true),
		dashboard.WithHistoryDuration(1*time.Hour),
		dashboard.WithMaxDataPoints(1000),
	))

	// Log dashboard URL
	log.Println("=============================================")
	log.Println("Dashboard available at:")
	log.Println("  http://localhost:8080/dashboard")
	log.Println("=============================================")
	log.Println()
	log.Println("API Endpoints:")
	log.Println("  GET  /dashboard/api/overview")
	log.Println("  GET  /dashboard/api/health")
	log.Println("  GET  /dashboard/api/metrics")
	log.Println("  GET  /dashboard/api/services")
	log.Println("  GET  /dashboard/api/history")
	log.Println()
	log.Println("Export Endpoints:")
	log.Println("  GET  /dashboard/export/json")
	log.Println("  GET  /dashboard/export/csv")
	log.Println("  GET  /dashboard/export/prometheus")
	log.Println()
	log.Println("WebSocket:")
	log.Println("  WS   /dashboard/ws")
	log.Println("=============================================")

	if err := app.Run(); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}

}
