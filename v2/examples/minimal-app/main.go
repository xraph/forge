package main

import (
	forge "github.com/xraph/forge/v2"
)

// This is a minimal Forge v2 application example
func main() {
	// Create app with default configuration
	app := forge.NewApp(forge.AppConfig{
		Name:        "minimal-app",
		Version:     "1.0.0",
		Description: "A minimal Forge v2 application",
		Environment: "development",
	})

	// Register routes
	router := app.Router()

	// Hello World endpoint
	router.GET("/", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{
			"message": "Hello, Forge v2!",
			"app":     app.Name(),
			"version": app.Version(),
		})
	})

	// Health check endpoint
	router.GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{
			"status": "healthy",
			"uptime": app.Uptime().String(),
		})
	})

	// Run the application (blocks until SIGINT/SIGTERM)
	// Built-in endpoints available:
	// - /_/info     - Application information
	// - /_/metrics  - Prometheus metrics
	// - /_/health   - Health checks
	if err := app.Run(); err != nil {
		app.Logger().Fatal("app failed", forge.F("error", err))
	}
}
