package main

import (
	"fmt"
	"log"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/orpc"
)

// Request/Response types demonstrating unified schema approach
// These schemas are automatically used for both OpenAPI and oRPC

type ListUsersRequest struct {
	Limit  int    `query:"limit,omitempty" description:"Maximum number of users to return"`
	Offset int    `query:"offset,omitempty" description:"Number of users to skip"`
	Search string `query:"search,omitempty" description:"Search term for filtering users"`
}

type ListUsersResponse struct {
	Users []UserResponse `json:"users" description:"List of users"`
	Total int            `json:"total" description:"Total number of users"`
}

type GetUserRequest struct {
	ID string `path:"id" description:"User ID"`
}

type UserResponse struct {
	ID        string `json:"id" description:"User unique identifier"`
	Name      string `json:"name" description:"User's full name"`
	Email     string `json:"email" description:"User's email address"`
	CreatedAt string `json:"created_at,omitempty" description:"When the user was created"`
}

type CreateUserRequest struct {
	Name  string `json:"name" description:"User's full name"`
	Email string `json:"email" description:"User's email address"`
}

type UpdateUserRequest struct {
	ID    string `path:"id" description:"User ID"`
	Name  string `json:"name,omitempty" description:"User's full name"`
	Email string `json:"email,omitempty" description:"User's email address"`
}

type DeleteUserRequest struct {
	ID string `path:"id" description:"User ID"`
}

type DeleteResponse struct {
	Deleted bool   `json:"deleted" description:"Whether the deletion was successful"`
	ID      string `json:"id" description:"ID of the deleted resource"`
}

type ErrorResponse struct {
	Error   string `json:"error" description:"Error message"`
	Code    string `json:"code,omitempty" description:"Error code"`
	Details string `json:"details,omitempty" description:"Additional error details"`
}

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
	// This demonstrates unified schema reuse for both OpenAPI and oRPC
	users.GET("", listUsersHandler,
		forge.WithName("list-users"),
		forge.WithSummary("List all users"),
		forge.WithORPCMethod("user.list"), // Custom method name
		forge.WithRequestSchema(ListUsersRequest{}),
		forge.WithResponseSchema(200, "List of users", ListUsersResponse{}),
	)

	// GET /users/:id - get user by ID
	// This demonstrates unified schema reuse with path parameter
	users.GET("/:id", getUserHandler,
		forge.WithName("get-user"),
		forge.WithSummary("Get user by ID"),
		forge.WithDescription("Retrieves a user by their unique identifier"),
		forge.WithRequestSchema(GetUserRequest{}),
		forge.WithResponseSchema(200, "User details", UserResponse{}),
		forge.WithResponseSchema(404, "User not found", ErrorResponse{}),
	)

	// POST /users - create user
	// Demonstrates multiple response schemas and explicit primary selection
	users.POST("", createUserHandler,
		forge.WithName("create-user"),
		forge.WithSummary("Create a new user"),
		forge.WithRequestSchema(CreateUserRequest{}),
		forge.WithResponseSchema(200, "User already exists", UserResponse{}),
		forge.WithResponseSchema(201, "User created", UserResponse{}),
		forge.WithORPCPrimaryResponse(201), // Explicitly choose 201 for oRPC
	)

	// PUT /users/:id - update user
	users.PUT("/:id", updateUserHandler,
		forge.WithName("update-user"),
		forge.WithSummary("Update an existing user"),
		forge.WithRequestSchema(UpdateUserRequest{}),
		forge.WithResponseSchema(200, "User updated", UserResponse{}),
		forge.WithResponseSchema(404, "User not found", ErrorResponse{}),
	)

	// DELETE /users/:id - delete user
	users.DELETE("/:id", deleteUserHandler,
		forge.WithName("delete-user"),
		forge.WithSummary("Delete a user"),
		forge.WithRequestSchema(DeleteUserRequest{}),
		forge.WithResponseSchema(200, "User deleted", DeleteResponse{}),
		forge.WithResponseSchema(404, "User not found", ErrorResponse{}),
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
	users := []UserResponse{
		{ID: "1", Name: "Alice", Email: "alice@example.com"},
		{ID: "2", Name: "Bob", Email: "bob@example.com"},
		{ID: "3", Name: "Charlie", Email: "charlie@example.com"},
	}
	return ctx.JSON(200, ListUsersResponse{
		Users: users,
		Total: len(users),
	})
}

func getUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	if id == "999" {
		return ctx.JSON(404, ErrorResponse{
			Error: "User not found",
			Code:  "USER_NOT_FOUND",
		})
	}
	user := UserResponse{
		ID:    id,
		Name:  fmt.Sprintf("User %s", id),
		Email: fmt.Sprintf("user%s@example.com", id),
	}
	return ctx.JSON(200, user)
}

func createUserHandler(ctx forge.Context) error {
	var req CreateUserRequest
	if err := ctx.Bind(&req); err != nil {
		return forge.BadRequest("Invalid request body")
	}

	// Simulate user creation
	user := UserResponse{
		ID:    "new-123",
		Name:  req.Name,
		Email: req.Email,
	}
	return ctx.JSON(201, user)
}

func updateUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	var req UpdateUserRequest
	if err := ctx.Bind(&req); err != nil {
		return forge.BadRequest("Invalid request body")
	}

	if id == "999" {
		return ctx.JSON(404, ErrorResponse{
			Error: "User not found",
			Code:  "USER_NOT_FOUND",
		})
	}

	// Simulate user update
	user := UserResponse{
		ID:    id,
		Name:  req.Name,
		Email: req.Email,
	}
	return ctx.JSON(200, user)
}

func deleteUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	if id == "999" {
		return ctx.JSON(404, ErrorResponse{
			Error: "User not found",
			Code:  "USER_NOT_FOUND",
		})
	}
	return ctx.JSON(200, DeleteResponse{
		Deleted: true,
		ID:      id,
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
