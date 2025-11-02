package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/xraph/forge"
)

// WorkerServiceExample demonstrates automatic config loading in a monorepo
func main() {
	fmt.Println("=== Forge Monorepo Auto-Discovery Example (Worker Service) ===")
	fmt.Println()

	// Create app - will automatically load config from monorepo root
	// and extract the "apps.worker-service" section
	app := forge.NewApp(forge.AppConfig{
		Name:    "worker-service", // Matches apps.worker-service in config
		Version: "1.0.0",
	})

	cfg := app.Config()
	displayConfig(cfg)

	// Register routes
	app.Router().GET("/config", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"app":      cfg.GetSection("app"),
			"database": cfg.GetSection("database"),
			"cache":    cfg.GetSection("extensions.cache"),
			"worker":   cfg.GetSection("worker"),
			"logging":  cfg.GetSection("logging"),
			"message":  "Worker service config from monorepo",
		})
	})

	app.Router().GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{
			"status": "healthy",
			"app":    "worker-service",
		})
	})

	// Start the app
	port := cfg.GetInt("app.port", 8082)
	fmt.Printf("\nStarting Worker Service on port %d...\n", port)
	fmt.Printf("Visit: http://localhost:%d/config\n", port)
	fmt.Println()

	if err := app.Run(); err != nil {
		log.Fatalf("App failed: %v", err)
	}
}

func displayConfig(cfg forge.ConfigManager) {
	fmt.Println("📋 Loaded Configuration for Worker Service:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	
	fmt.Println("\n🚀 App:")
	fmt.Printf("  Name: %s\n", cfg.GetString("app.name"))
	fmt.Printf("  Port: %d\n", cfg.GetInt("app.port"))

	fmt.Println("\n🗄️  Database:")
	fmt.Printf("  Host: %s\n", cfg.GetString("database.host"))
	fmt.Printf("  Name: %s\n", cfg.GetString("database.name"))
	fmt.Printf("  User: %s\n", cfg.GetString("database.user"))
	fmt.Printf("  Max Connections: %d\n", cfg.GetInt("database.max_connections"))

	fmt.Println("\n⚙️  Worker Settings:")
	fmt.Printf("  Concurrency: %d\n", cfg.GetInt("worker.concurrency"))
	fmt.Printf("  Queue Name: %s\n", cfg.GetString("worker.queue_name"))
	fmt.Printf("  Retry Attempts: %d\n", cfg.GetInt("worker.retry_attempts"))

	fmt.Println("\n📝 Logging:")
	fmt.Printf("  Level: %s\n", cfg.GetString("logging.level"))
	fmt.Printf("  Format: %s\n", cfg.GetString("logging.format"))

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}

