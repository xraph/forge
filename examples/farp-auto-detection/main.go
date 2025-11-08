package main

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/discovery"
)

func main() {
	fmt.Println("=== FARP Auto Schema Detection Example ===")

	// Create app with discovery + FARP auto-detection enabled
	app := forge.NewApp(forge.AppConfig{
		Name:    "users-service",
		Version: "1.0.0",
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("mdns"), // or "memory" for testing
				discovery.WithServiceName("users-service"),
				discovery.WithServiceAddress("localhost", 8080),
				discovery.WithServiceTags("rest", "api"),

				// Enable FARP with auto-detection
				// NOTE: No schemas configured - will auto-detect from router!
				discovery.WithConfig(discovery.Config{
					Enabled: true,
					Backend: "mdns",
					Service: discovery.ServiceConfig{
						Name:    "users-service",
						Version: "1.0.0",
						Address: "localhost",
						Port:    8080,
						Tags:    []string{"rest", "api"},
					},
					FARP: discovery.FARPConfig{
						Enabled:      true,                           // Enable FARP
						AutoRegister: true,                           // Enable auto-detection
						Strategy:     "push",                         // Push schemas to registry
						Schemas:      []discovery.FARPSchemaConfig{}, // Empty = auto-detect!
						Endpoints: discovery.FARPEndpointsConfig{
							Health:   "/_/health",
							Metrics:  "/_/metrics",
							OpenAPI:  "/openapi.json",
							AsyncAPI: "/asyncapi.json",
						},
						Capabilities: []string{"rest", "http"},
					},
				}),
			),
		},
	})

	// Configure router with routes BEFORE running app
	// This is critical - routes must be registered before discovery extension starts
	router := app.Router()

	// Add example routes - these will be detected by FARP
	router.GET("/users", func(c forge.Context) error {
		return c.JSON(200, map[string]interface{}{
			"users": []string{"alice", "bob", "charlie"},
		})
	})

	router.GET("/users/:id", func(c forge.Context) error {
		id := c.Param("id")
		return c.JSON(200, map[string]interface{}{
			"id":   id,
			"name": "User " + id,
		})
	})

	router.POST("/users", func(c forge.Context) error {
		return c.JSON(201, map[string]interface{}{
			"message": "User created",
		})
	})

	// Check if you're testing locally
	fmt.Println("Service: users-service")
	fmt.Println("Version: 1.0.0")
	fmt.Println("Address: http://localhost:8080")
	fmt.Println("\nEndpoints to test:")
	fmt.Println("  - http://localhost:8080/_/health         - Health check")
	fmt.Println("  - http://localhost:8080/_farp/manifest   - FARP manifest (check schema_count)")
	fmt.Println("  - http://localhost:8080/openapi.json     - OpenAPI spec")
	fmt.Println("  - http://localhost:8080/users            - Sample API")
	fmt.Println("\nExpected: schema_count should be > 0 in FARP manifest")
	fmt.Println()

	// Run app - discovery will auto-detect schemas during startup
	if err := app.Run(); err != nil {
		fmt.Printf("Error running app: %v\n", err)
	}
}
