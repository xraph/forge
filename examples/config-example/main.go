package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/cache"
	"github.com/xraph/forge/extensions/mcp"
	"github.com/xraph/forge/internal/config/sources"
)

func main() {
	fmt.Println("=== Forge v2 Extension Configuration Example ===\n")

	// Example 1: Load all config from config.yaml
	// Extensions will try "extensions.cache" and "extensions.mcp" first,
	// then fall back to "cache" and "mcp" for backward compatibility
	example1()

	// Example 2: Programmatic config overrides
	// example2()

	// Example 3: Require config from ConfigManager
	// example3()
}

// Example 1: Load config from file
func example1() {
	fmt.Println("Example 1: Loading config from config.yaml")
	fmt.Println("-------------------------------------------")

	// Create config manager and load config file
	configManager := forge.NewManager(forge.ManagerConfig{})

	source, err := sources.NewFileSource("./config.yaml", sources.FileSourceOptions{
		Logger: forge.NewLogger(logger.LoggerConfig{
			Level: "info",
		}),
	})
	if err != nil {
		log.Fatalf("Failed to create file source: %v", err)
	}

	if err := configManager.LoadFrom(source); err != nil {
		log.Fatalf("Failed to add config source: %v", err)
	}

	if err := configManager.Load(context.Background()); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create app with extensions
	// Extensions will automatically load their config from ConfigManager
	app := forge.NewApp(forge.AppConfig{
		Name:    "config-example",
		Version: "1.0.0",
		Extensions: []forge.Extension{
			// Load config from "extensions.cache" or "cache" key
			cache.NewExtension(),

			// Load config from "extensions.mcp" or "mcp" key
			mcp.NewExtension(),
		},
	})

	// Register ConfigManager with DI so extensions can use it
	if err := forge.RegisterSingleton(app.Container(), forge.ConfigKey, func(c forge.Container) (forge.ConfigManager, error) {
		return configManager, nil
	}); err != nil {
		log.Fatalf("Failed to register ConfigManager: %v", err)
	}

	// Register a simple test route
	app.Router().GET("/test", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{
			"message": "Config loaded successfully!",
			"tip":     "Visit /_/mcp/info to see MCP server info",
		})
	})

	// Start the app
	fmt.Println("\nStarting app...")
	fmt.Println("Cache and MCP extensions will load config from config.yaml")
	fmt.Println("Visit: http://localhost:8080/test")
	fmt.Println("Visit: http://localhost:8080/_/mcp/info")
	fmt.Println()

	if err := app.Run(context.Background()); err != nil {
		log.Fatalf("App failed: %v", err)
	}
}

// Example 2: Programmatic config overrides
func example2() {
	fmt.Println("Example 2: Programmatic config overrides")
	fmt.Println("----------------------------------------")

	// Create config manager
	configManager := forge.NewManager(forge.ManagerConfig{})
	if err := configManager.AddSource("file", forge.SourceConfig{
		Type: "file",
		Config: map[string]interface{}{
			"path":   "./config.yaml",
			"format": "yaml",
		},
	}); err != nil {
		log.Fatalf("Failed to add config source: %v", err)
	}

	if err := configManager.Load(context.Background()); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create app with programmatic overrides
	app := forge.NewApp(forge.AppConfig{
		Name:    "config-example",
		Version: "1.0.0",
		Extensions: []forge.Extension{
			// Override driver to use inmemory instead of redis from config file
			cache.NewExtension(
				cache.WithDriver("inmemory"),
				cache.WithMaxSize(5000),
			),

			// Override base path
			mcp.NewExtension(
				mcp.WithBasePath("/api/mcp"),
			),
		},
	})

	// Register ConfigManager
	if err := forge.RegisterSingleton(app.Container(), forge.ConfigKey, func(c forge.Container) (forge.ConfigManager, error) {
		return configManager, nil
	}); err != nil {
		log.Fatalf("Failed to register ConfigManager: %v", err)
	}

	fmt.Println("\nStarting app with programmatic overrides...")
	fmt.Println("Cache driver: inmemory (overrides redis from config)")
	fmt.Println("MCP base path: /api/mcp (overrides /_/mcp from config)")
	fmt.Println()

	if err := app.Run(context.Background()); err != nil {
		log.Fatalf("App failed: %v", err)
	}
}

// Example 3: Require config from ConfigManager
func example3() {
	fmt.Println("Example 3: Require config from ConfigManager")
	fmt.Println("---------------------------------------------")

	// Create app without ConfigManager
	// Extensions that require config will fail
	app := forge.NewApp(forge.AppConfig{
		Name:    "config-example",
		Version: "1.0.0",
		Extensions: []forge.Extension{
			// This will fail because config is required but ConfigManager not available
			cache.NewExtension(
				cache.WithRequireConfig(true),
			),
		},
	})

	fmt.Println("\nStarting app without ConfigManager...")
	fmt.Println("This will fail because cache requires config")
	fmt.Println()

	if err := app.Run(context.Background()); err != nil {
		fmt.Printf("Expected error: %v\n", err)
		fmt.Println("\nThis demonstrates the RequireConfig flag working correctly.")
	}
}
