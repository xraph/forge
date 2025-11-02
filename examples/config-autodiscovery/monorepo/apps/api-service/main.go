package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/xraph/forge"
)

// APIServiceExample demonstrates automatic config loading in a monorepo
//
// Monorepo Config Loading:
// 1. Searches for config.yaml in current directory and parents (finds root config)
// 2. Searches for config.local.yaml for local overrides
// 3. Extracts app-scoped config from "apps.api-service" section
// 4. Merges: global config -> app config -> local overrides
//
// Directory structure:
// monorepo/
//   ├── config.yaml              (root config with all apps)
//   ├── config.local.yaml        (local overrides for all apps)
//   └── apps/
//       ├── api-service/
//       │   └── main.go          (this file)
//       ├── admin-dashboard/
//       │   └── main.go
//       └── worker-service/
//           └── main.go
func main() {
	fmt.Println("=== Forge Monorepo Auto-Discovery Example (API Service) ===")
	fmt.Println()

	// Create app with app name for monorepo support
	// Forge will automatically:
	// 1. Search up the directory tree for config.yaml (finds root config)
	// 2. Search for config.local.yaml for local overrides
	// 3. Extract "apps.api-service" section and merge with globals
	// 4. Apply local overrides from config.local.yaml
	app := forge.NewApp(forge.AppConfig{
		Name:    "api-service", // This name must match the key in apps.{name}
		Version: "1.0.0",
		// EnableAppScopedConfig is true by default for monorepo support
		// EnableConfigAutoDiscovery is true by default
	})

	// Access config values - they'll be loaded from:
	// - Global config in root config.yaml
	// - App-scoped config from apps.api-service
	// - Local overrides from config.local.yaml
	cfg := app.Config()

	// Display loaded config
	displayConfig(cfg)

	// Register a route that shows config
	app.Router().GET("/config", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"app":      cfg.GetSection("app"),
			"database": cfg.GetSection("database"),
			"cache":    cfg.GetSection("extensions.cache"),
			"api":      cfg.GetSection("api"),
			"logging":  cfg.GetSection("logging"),
			"message":  "Config loaded from monorepo root + app-scoped + local overrides",
		})
	})

	// Register API routes
	app.Router().GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{
			"status": "healthy",
			"app":    "api-service",
		})
	})

	// Start the app
	port := cfg.GetInt("app.port", 8080)
	fmt.Printf("\nStarting API service on port %d...\n", port)
	fmt.Printf("Visit: http://localhost:%d/config to see loaded configuration\n", port)
	fmt.Printf("Visit: http://localhost:%d/health for health check\n", port)
	fmt.Println()

	if err := app.Run(); err != nil {
		log.Fatalf("App failed: %v", err)
	}
}

func displayConfig(cfg forge.ConfigManager) {
	fmt.Println("📋 Loaded Configuration for API Service:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	
	// App info
	fmt.Println("\n🚀 App:")
	fmt.Printf("  Name: %s\n", cfg.GetString("app.name"))
	fmt.Printf("  Version: %s\n", cfg.GetString("app.version"))
	fmt.Printf("  Port: %d\n", cfg.GetInt("app.port"))

	// Database config (merged from global + app-scoped + local)
	fmt.Println("\n🗄️  Database:")
	fmt.Printf("  Driver: %s\n", cfg.GetString("database.driver"))
	fmt.Printf("  Host: %s\n", cfg.GetString("database.host"))
	fmt.Printf("  Port: %d\n", cfg.GetInt("database.port"))
	fmt.Printf("  Name: %s\n", cfg.GetString("database.name"))
	fmt.Printf("  User: %s\n", cfg.GetString("database.user"))
	fmt.Printf("  Max Connections: %d\n", cfg.GetInt("database.max_connections"))

	// Cache config
	fmt.Println("\n💾 Cache:")
	fmt.Printf("  Driver: %s\n", cfg.GetString("extensions.cache.driver"))
	if cfg.GetString("extensions.cache.driver") == "redis" {
		fmt.Printf("  URL: %s\n", cfg.GetString("extensions.cache.url"))
		fmt.Printf("  Prefix: %s\n", cfg.GetString("extensions.cache.prefix"))
	} else {
		fmt.Printf("  Max Size: %d\n", cfg.GetInt("extensions.cache.max_size"))
	}
	fmt.Printf("  Default TTL: %s\n", cfg.GetString("extensions.cache.default_ttl"))

	// API-specific config
	fmt.Println("\n🌐 API Settings:")
	fmt.Printf("  Rate Limit: %d\n", cfg.GetInt("api.rate_limit"))
	fmt.Printf("  Timeout: %s\n", cfg.GetString("api.timeout"))

	// Logging config
	fmt.Println("\n📝 Logging:")
	fmt.Printf("  Level: %s\n", cfg.GetString("logging.level"))
	fmt.Printf("  Format: %s\n", cfg.GetString("logging.format"))

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("💡 Config Merge Order:")
	fmt.Println("   1. Global config (database, cache, logging)")
	fmt.Println("   2. App-scoped config (apps.api-service)")
	fmt.Println("   3. Local overrides (config.local.yaml)")
	fmt.Println()
}

