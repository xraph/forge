package main

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/discovery"
)

func main() {
	fmt.Println("=== FARP + Discovery Integration Example ===\n")

	app := forge.NewApp(forge.AppConfig{
		Name:    "user-service",
		Version: "v1.2.3",
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("memory"),
				discovery.WithServiceName("user-service"),
				discovery.WithServiceAddress("localhost", 8080),
				discovery.WithServiceTags("rest", "production"),
				
				// Enable FARP
				discovery.WithConfig(discovery.Config{
					Enabled: true,
					Backend: "memory",
					Service: discovery.ServiceConfig{
						Name:    "user-service",
						Version: "v1.2.3",
						Address: "localhost",
						Port:    8080,
						Tags:    []string{"rest", "production"},
					},
					FARP: discovery.FARPConfig{
						Enabled:      true,
						AutoRegister: true,
						Strategy:     "push",
						Schemas: []discovery.FARPSchemaConfig{
							{
								Type:        "openapi",
								SpecVersion: "3.1.0",
								Location: discovery.FARPLocationConfig{
									Type:         "registry",
									RegistryPath: "/schemas/user-service/v1/openapi",
								},
								ContentType: "application/json",
							},
							{
								Type:        "asyncapi",
								SpecVersion: "3.0.0",
								Location: discovery.FARPLocationConfig{
									Type:         "registry",
									RegistryPath: "/schemas/user-service/v1/asyncapi",
								},
								ContentType: "application/json",
							},
						},
						Endpoints: discovery.FARPEndpointsConfig{
							Health:   "/health",
							Metrics:  "/metrics",
							OpenAPI:  "/openapi.json",
							AsyncAPI: "/asyncapi.json",
						},
						Capabilities: []string{"rest", "websocket"},
					},
				}),
			),
		},
	})

	// Configure router with OpenAPI
	router := app.Router()
	
	// Add some example routes
	router.GET("/users", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]interface{}{
			"users": []map[string]string{
				{"id": "1", "name": "Alice"},
				{"id": "2", "name": "Bob"},
			},
		})
	}, forge.WithSummary("List users"), forge.WithTags("users"))

	router.POST("/users", func(ctx forge.Context) error {
		return ctx.JSON(201, map[string]interface{}{
			"id":   "3",
			"name": "Charlie",
		})
	}, forge.WithSummary("Create user"), forge.WithTags("users"))

	router.GET("/users/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")
		return ctx.JSON(200, map[string]interface{}{
			"id":   id,
			"name": "User " + id,
		})
	}, forge.WithSummary("Get user by ID"), forge.WithTags("users"))

	router.DELETE("/users/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")
		return ctx.JSON(200, map[string]interface{}{
			"message": "User " + id + " deleted",
		})
	}, forge.WithSummary("Delete user"), forge.WithTags("users"))

	// Health endpoint
	router.GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]interface{}{
			"status": "healthy",
		})
	})

	fmt.Println("✓ Service configured with 5 routes")
	fmt.Println("✓ Discovery extension enabled")
	fmt.Println("✓ FARP schema registration enabled")
	fmt.Println()
	fmt.Println("When service starts:")
	fmt.Println("  1. Service registers with discovery backend")
	fmt.Println("  2. OpenAPI schema auto-generated from routes")
	fmt.Println("  3. AsyncAPI schema auto-generated from streaming routes")
	fmt.Println("  4. Schemas published to registry")
	fmt.Println("  5. SchemaManifest registered with FARP")
	fmt.Println("  6. API gateways can discover and auto-configure routes")
	fmt.Println()
	fmt.Println("Starting server on :8080...")
	fmt.Println("OpenAPI spec available at: http://localhost:8080/openapi.json")
	fmt.Println("AsyncAPI spec available at: http://localhost:8080/asyncapi.json")
	fmt.Println()

	app.Run()
}

