package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/mcp"
)

func main() {
	// Create Forge application
	app := forge.NewApp(forge.AppConfig{
		Name:        "mcp-advanced-example",
		Version:     "1.0.0",
		HTTPAddress: ":8080",
	})

	// Register API routes
	api := app.Router().Group("/api")
	api.GET("/status", statusHandler,
		forge.WithSummary("Get API status"),
	)
	api.GET("/metrics", metricsHandler,
		forge.WithSummary("Get API metrics"),
	)

	// Enable MCP extension with advanced features
	mcpExt := mcp.NewExtension(
		mcp.WithEnabled(true),
		mcp.WithBasePath("/_/mcp"),
		mcp.WithAutoExposeRoutes(true),
		mcp.WithToolPrefix("api_"),

		// Security: Enable authentication
		mcp.WithAuth("X-API-Key", []string{
			"dev-secret-key-123",
			"prod-secret-key-456",
		}),

		// Rate limiting: 60 requests per minute
		mcp.WithRateLimit(60),

		// Pattern matching: Only expose /api/* routes
		mcp.WithIncludePatterns([]string{"/api/*"}),

		// Enable resources and prompts
		mcp.WithResources(true),
		mcp.WithPrompts(true),
	)

	if err := app.RegisterExtension(mcpExt); err != nil {
		fmt.Printf("Failed to register MCP extension: %v\n", err)
		os.Exit(1)
	}

	// Register custom resources and prompts
	registerCustomFeatures(mcpExt)

	// Start application
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		fmt.Printf("Failed to start application: %v\n", err)
		os.Exit(1)
	}
	defer app.Stop(ctx)

	printUsageInfo()

	// Run HTTP server
	if err := app.Run(); err != nil {
		fmt.Printf("Server error: %v\n", err)
		os.Exit(1)
	}
}

func registerCustomFeatures(ext forge.Extension) {
	mcpExt := ext.(*mcp.Extension)
	server := mcpExt.Server()

	// Register a custom resource
	server.RegisterResource(&mcp.Resource{
		URI:         "config://app-settings",
		Name:        "Application Settings",
		Description: "Current application configuration",
		MimeType:    "application/json",
	})

	// Register custom resource reader
	server.RegisterResourceReader("config://app-settings",
		func(ctx context.Context, resource *mcp.Resource) (mcp.Content, error) {
			config := map[string]interface{}{
				"environment": "development",
				"debug":       true,
				"version":     "1.0.0",
				"features": map[string]bool{
					"mcp_enabled":      true,
					"auth_required":    true,
					"rate_limit":       true,
					"custom_resources": true,
					"custom_prompts":   true,
				},
			}

			return mcp.Content{
				Type:     "text",
				Text:     fmt.Sprintf("%+v", config),
				MimeType: "application/json",
			}, nil
		})

	// Register a custom prompt
	server.RegisterPrompt(&mcp.Prompt{
		Name:        "api-documentation",
		Description: "Generates API documentation based on available endpoints",
		Arguments: []mcp.PromptArgument{
			{Name: "format", Description: "Documentation format (markdown/json)", Required: false},
		},
	})

	// Register custom prompt generator
	server.RegisterPromptGenerator("api-documentation",
		func(ctx context.Context, prompt *mcp.Prompt, args map[string]interface{}) ([]mcp.PromptMessage, error) {
			format := "markdown"
			if f, ok := args["format"].(string); ok {
				format = f
			}

			doc := fmt.Sprintf(`# API Documentation

## Endpoints

### GET /api/status
Returns the current API status

### GET /api/metrics
Returns API metrics

## Authentication
All MCP endpoints require the X-API-Key header.

## Rate Limiting
Limited to 60 requests per minute per client.

Format: %s`, format)

			return []mcp.PromptMessage{
				{
					Role: "assistant",
					Content: []mcp.Content{
						{
							Type: "text",
							Text: doc,
						},
					},
				},
			}, nil
		})
}

func statusHandler(ctx forge.Context) error {
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status": "healthy",
		"uptime": "1h 23m",
		"mcp": map[string]interface{}{
			"enabled": true,
			"tools":   2,
		},
	})
}

func metricsHandler(ctx forge.Context) error {
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"requests_total": 1234,
		"requests_rate":  "12.5/sec",
		"errors_total":   5,
		"mcp_calls":      42,
	})
}

func printUsageInfo() {
	fmt.Println("\n🔒 MCP Advanced Example Started (with Auth & Rate Limiting)")
	fmt.Println("\n📋 Available MCP Endpoints:")
	fmt.Println("  - Server Info:    http://localhost:8080/_/mcp/info")
	fmt.Println("  - List Tools:     http://localhost:8080/_/mcp/tools")
	fmt.Println("  - List Resources: http://localhost:8080/_/mcp/resources")
	fmt.Println("  - List Prompts:   http://localhost:8080/_/mcp/prompts")
	fmt.Println("\n🔑 Authentication Required:")
	fmt.Println("  Add header: X-API-Key: dev-secret-key-123")
	fmt.Println("\n⏱️  Rate Limit:")
	fmt.Println("  60 requests per minute per client")
	fmt.Println("\n🛠️  Example Tools:")
	fmt.Println("  - api_get_api_status  → GET /api/status")
	fmt.Println("  - api_get_api_metrics → GET /api/metrics")
	fmt.Println("\n📦 Custom Resources:")
	fmt.Println("  - config://app-settings")
	fmt.Println("\n💬 Custom Prompts:")
	fmt.Println("  - api-documentation")
	fmt.Println("\n📝 Test Commands:")
	fmt.Println("\n  # List tools (with auth)")
	fmt.Println(`  curl -H "X-API-Key: dev-secret-key-123" \
    http://localhost:8080/_/mcp/tools`)
	fmt.Println("\n  # Call a tool")
	fmt.Println(`  curl -X POST http://localhost:8080/_/mcp/tools/api_get_api_status \
    -H "X-API-Key: dev-secret-key-123" \
    -H "Content-Type: application/json" \
    -d '{"name":"api_get_api_status","arguments":{}}'`)
	fmt.Println("\n  # Read a resource")
	fmt.Println(`  curl -X POST http://localhost:8080/_/mcp/resources/read \
    -H "X-API-Key: dev-secret-key-123" \
    -H "Content-Type: application/json" \
    -d '{"uri":"config://app-settings"}'`)
	fmt.Println("\n  # Get a prompt")
	fmt.Println(`  curl -X POST http://localhost:8080/_/mcp/prompts/api-documentation \
    -H "X-API-Key: dev-secret-key-123" \
    -H "Content-Type: application/json" \
    -d '{"name":"api-documentation","arguments":{"format":"markdown"}}'`)
	fmt.Println("\n  # Test rate limiting (exceed 60 req/min)")
	fmt.Println(`  for i in {1..70}; do \
    curl -H "X-API-Key: dev-secret-key-123" \
    http://localhost:8080/_/mcp/tools; \
  done`)
	fmt.Println("\n")
}
