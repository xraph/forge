package main

import (
	"fmt"
	"log"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/orpc"
)

func main() {
	// Create Forge app
	app := forge.NewApp(forge.AppConfig{
		Name:        "orpc-example",
		Version:     "1.0.0",
		Description: "Example application demonstrating oRPC auto-exposure",
		HTTPAddress: ":8086",
	})

	// Register oRPC extension with auto-exposure enabled
	app.RegisterExtension(orpc.NewExtension(
		orpc.WithEnabled(true),
		orpc.WithEndpoint("/rpc"),
		orpc.WithAutoExposeRoutes(true),
		orpc.WithMethodPrefix("api."),
		orpc.WithExcludePatterns([]string{"/_/*"}), // Exclude internal routes
		orpc.WithOpenRPC(true),                     // Enable OpenRPC schema
		orpc.WithDiscovery(true),                   // Enable method discovery
		orpc.WithBatch(true),                       // Enable batch requests
	))

	// Register routes - these will be auto-exposed as JSON-RPC methods
	setupRoutes(app.Router())

	// Start the app
	log.Printf("Starting server on :8080")
	log.Printf("JSON-RPC endpoint: http://localhost:8080/rpc")
	log.Printf("OpenRPC schema: http://localhost:8080/rpc/schema")
	log.Printf("Methods list: http://localhost:8080/rpc/methods")
	log.Println()
	log.Println("Try these JSON-RPC calls:")
	log.Println(`  curl -X POST http://localhost:8080/rpc -d '{"jsonrpc":"2.0","method":"api.get.users.id","params":{"id":"123"},"id":1}'`)
	log.Println(`  curl -X POST http://localhost:8080/rpc -d '{"jsonrpc":"2.0","method":"api.user.list","params":{},"id":2}'`)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func setupRoutes(router forge.Router) {
	// User routes
	users := router.Group("/users", forge.WithGroupTags("users"))

	// GET /users - list users (custom RPC method name)
	users.GET("", listUsersHandler,
		forge.WithName("list-users"),
		forge.WithSummary("List all users"),
		forge.WithORPCMethod("user.list"), // Custom method name
	)

	// GET /users/:id - get user by ID
	users.GET("/:id", getUserHandler,
		forge.WithName("get-user"),
		forge.WithSummary("Get user by ID"),
		forge.WithDescription("Retrieves a user by their unique identifier"),
		forge.WithORPCParams(&orpc.ParamsSchema{
			Type: "object",
			Properties: map[string]*orpc.PropertySchema{
				"id": {
					Type:        "string",
					Description: "User ID",
				},
			},
			Required: []string{"id"},
		}),
		forge.WithORPCResult(&orpc.ResultSchema{
			Type:        "object",
			Description: "User object",
			Properties: map[string]*orpc.PropertySchema{
				"id":    {Type: "string"},
				"name":  {Type: "string"},
				"email": {Type: "string"},
			},
		}),
	)

	// POST /users - create user
	users.POST("", createUserHandler,
		forge.WithName("create-user"),
		forge.WithSummary("Create a new user"),
	)

	// PUT /users/:id - update user
	users.PUT("/:id", updateUserHandler,
		forge.WithName("update-user"),
		forge.WithSummary("Update an existing user"),
	)

	// DELETE /users/:id - delete user
	users.DELETE("/:id", deleteUserHandler,
		forge.WithName("delete-user"),
		forge.WithSummary("Delete a user"),
	)

	// Product routes
	products := router.Group("/products", forge.WithGroupTags("products"))

	products.GET("", listProductsHandler,
		forge.WithSummary("List all products"),
	)

	products.GET("/:id", getProductHandler,
		forge.WithSummary("Get product by ID"),
	)

	// Internal routes - excluded from RPC
	router.GET("/internal/debug", debugHandler,
		forge.WithORPCExclude(), // Explicitly exclude from oRPC
		forge.WithSummary("Debug endpoint (not exposed via RPC)"),
	)

	// Health check - excluded by pattern
	router.GET("/_/health", healthHandler,
		forge.WithSummary("Health check"),
	)
}

// Handler implementations

func listUsersHandler(ctx forge.Context) error {
	users := []map[string]interface{}{
		{"id": "1", "name": "Alice", "email": "alice@example.com"},
		{"id": "2", "name": "Bob", "email": "bob@example.com"},
		{"id": "3", "name": "Charlie", "email": "charlie@example.com"},
	}
	return ctx.JSON(200, map[string]interface{}{
		"users": users,
		"total": len(users),
	})
}

func getUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	user := map[string]interface{}{
		"id":    id,
		"name":  fmt.Sprintf("User %s", id),
		"email": fmt.Sprintf("user%s@example.com", id),
	}
	return ctx.JSON(200, user)
}

func createUserHandler(ctx forge.Context) error {
	var body map[string]interface{}
	if err := ctx.Bind(&body); err != nil {
		return forge.BadRequest("Invalid request body")
	}

	// Simulate user creation
	user := map[string]interface{}{
		"id":    "new-123",
		"name":  body["name"],
		"email": body["email"],
	}
	return ctx.JSON(201, user)
}

func updateUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	var body map[string]interface{}
	if err := ctx.Bind(&body); err != nil {
		return forge.BadRequest("Invalid request body")
	}

	// Simulate user update
	user := map[string]interface{}{
		"id":      id,
		"name":    body["name"],
		"email":   body["email"],
		"updated": true,
	}
	return ctx.JSON(200, user)
}

func deleteUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(200, map[string]interface{}{
		"deleted": true,
		"id":      id,
	})
}

func listProductsHandler(ctx forge.Context) error {
	products := []map[string]interface{}{
		{"id": "1", "name": "Laptop", "price": 999.99},
		{"id": "2", "name": "Mouse", "price": 29.99},
	}
	return ctx.JSON(200, map[string]interface{}{
		"products": products,
		"total":    len(products),
	})
}

func getProductHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	product := map[string]interface{}{
		"id":    id,
		"name":  fmt.Sprintf("Product %s", id),
		"price": 99.99,
	}
	return ctx.JSON(200, product)
}

func debugHandler(ctx forge.Context) error {
	return ctx.JSON(200, map[string]interface{}{
		"debug": "This route is excluded from oRPC",
	})
}

func healthHandler(ctx forge.Context) error {
	return ctx.JSON(200, map[string]interface{}{
		"status": "healthy",
	})
}
