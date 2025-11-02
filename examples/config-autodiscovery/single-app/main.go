package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/xraph/forge"
)

// SingleAppExample demonstrates automatic config loading in a single-app project
//
// Config Loading Hierarchy:
// 1. Searches for config.yaml or config.yml (base config)
// 2. Searches for config.local.yaml or config.local.yml (local overrides)
// 3. Merges local config over base config (local takes precedence)
//
// Directory structure:
// single-app/
//   ├── config.yaml       (base config - committed to git)
//   ├── config.local.yaml (local overrides - in .gitignore)
//   └── main.go
func main() {
	fmt.Println("=== Forge Single-App Auto-Discovery Example ===")
	fmt.Println()

	// Create app with auto-discovery enabled (default behavior)
	// Forge will automatically:
	// 1. Search for config.yaml/yml in current directory and parents
	// 2. Search for config.local.yaml/yml for local overrides
	// 3. Load and merge them (local overrides base)
	app := forge.NewApp(forge.AppConfig{
		Name:    "single-app-example",
		Version: "1.0.0",
		// EnableConfigAutoDiscovery is true by default
		// ConfigManager is nil, so auto-discovery will happen
	})

	// Access config values - they'll be loaded from config.yaml + config.local.yaml
	cfg := app.Config()

	// Display loaded config
	displayConfig(cfg)

	// Register a route that shows config
	app.Router().GET("/config", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"database": cfg.GetSection("database"),
			"cache":    cfg.GetSection("extensions.cache"),
			"logging":  cfg.GetSection("logging"),
			"message":  "Config loaded from config.yaml + config.local.yaml",
		})
	})

	// Start the app
	fmt.Println("\nStarting app...")
	fmt.Println("Visit: http://localhost:8080/config to see loaded configuration")
	fmt.Println()

	if err := app.Run(); err != nil {
		log.Fatalf("App failed: %v", err)
	}
}

func displayConfig(cfg forge.ConfigManager) {
	fmt.Println("📋 Loaded Configuration:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	
	// Database config
	fmt.Println("\n🗄️  Database:")
	fmt.Printf("  Driver: %s\n", cfg.GetString("database.driver"))
	fmt.Printf("  Host: %s\n", cfg.GetString("database.host"))
	fmt.Printf("  Port: %d\n", cfg.GetInt("database.port"))
	fmt.Printf("  Name: %s\n", cfg.GetString("database.name"))
	fmt.Printf("  User: %s\n", cfg.GetString("database.user"))

	// Cache config
	fmt.Println("\n💾 Cache:")
	fmt.Printf("  Driver: %s\n", cfg.GetString("extensions.cache.driver"))
	if cfg.GetString("extensions.cache.driver") == "redis" {
		fmt.Printf("  URL: %s\n", cfg.GetString("extensions.cache.url"))
	} else {
		fmt.Printf("  Max Size: %d\n", cfg.GetInt("extensions.cache.max_size"))
	}

	// Logging config
	fmt.Println("\n📝 Logging:")
	fmt.Printf("  Level: %s\n", cfg.GetString("logging.level"))
	fmt.Printf("  Format: %s\n", cfg.GetString("logging.format"))

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("💡 Tip: Values from config.local.yaml override config.yaml")
	fmt.Println()
}

