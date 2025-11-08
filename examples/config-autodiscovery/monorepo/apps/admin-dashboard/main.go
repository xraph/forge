package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/xraph/forge"
)

// AdminDashboardExample demonstrates automatic config loading in a monorepo
// Each app gets its own config section from the root config.yaml
func main() {
	fmt.Println("=== Forge Monorepo Auto-Discovery Example (Admin Dashboard) ===")
	fmt.Println()

	// Create app - will automatically load config from monorepo root
	// and extract the "apps.admin-dashboard" section
	app := forge.NewApp(forge.AppConfig{
		Name:    "admin-dashboard", // Matches apps.admin-dashboard in config
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
			"admin":    cfg.GetSection("admin"),
			"logging":  cfg.GetSection("logging"),
			"message":  "Admin dashboard config from monorepo",
		})
	})

	app.Router().GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{
			"status": "healthy",
			"app":    "admin-dashboard",
		})
	})

	// Start the app
	port := cfg.GetInt("app.port", 8081)
	fmt.Printf("\nStarting Admin Dashboard on port %d...\n", port)
	fmt.Printf("Visit: http://localhost:%d/config\n", port)
	fmt.Println()

	if err := app.Run(); err != nil {
		log.Fatalf("App failed: %v", err)
	}
}

func displayConfig(cfg forge.ConfigManager) {
	fmt.Println("📋 Loaded Configuration for Admin Dashboard:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	fmt.Println("\n🚀 App:")
	fmt.Printf("  Name: %s\n", cfg.GetString("app.name"))
	fmt.Printf("  Port: %d\n", cfg.GetInt("app.port"))

	fmt.Println("\n🗄️  Database:")
	fmt.Printf("  Host: %s\n", cfg.GetString("database.host"))
	fmt.Printf("  Name: %s\n", cfg.GetString("database.name"))
	fmt.Printf("  User: %s\n", cfg.GetString("database.user"))

	fmt.Println("\n👤 Admin Settings:")
	fmt.Printf("  Session Timeout: %s\n", cfg.GetString("admin.session_timeout"))
	fmt.Printf("  Max Upload Size: %s\n", cfg.GetString("admin.max_upload_size"))

	fmt.Println("\n📝 Logging:")
	fmt.Printf("  Level: %s\n", cfg.GetString("logging.level"))
	fmt.Printf("  Format: %s\n", cfg.GetString("logging.format"))

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}
