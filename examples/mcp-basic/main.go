package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/mcp"
)

// User represents a simple user model
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// In-memory user store
var users = map[string]User{
	"1": {ID: "1", Name: "Alice", Email: "alice@example.com"},
	"2": {ID: "2", Name: "Bob", Email: "bob@example.com"},
}

func main() {
	// Create Forge application
	app := forge.NewApp(forge.AppConfig{
		Name:    "mcp-basic-example",
		Version: "1.0.0",
	})

	// Register user routes
	registerUserRoutes(app.Router())

	// Enable MCP extension with auto-exposure
	mcpExt := mcp.NewExtension(
		mcp.WithEnabled(true),
		mcp.WithBasePath("/_/mcp"),
		mcp.WithAutoExposeRoutes(true),
		mcp.WithExcludePatterns([]string{"/_/*"}), // Exclude MCP endpoints themselves
		mcp.WithToolPrefix("example_"),            // Prefix all tools
	)

	if err := app.RegisterExtension(mcpExt); err != nil {
		fmt.Printf("Failed to register MCP extension: %v\n", err)
		os.Exit(1)
	}

	// Start application
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		fmt.Printf("Failed to start application: %v\n", err)
		os.Exit(1)
	}
	defer app.Stop(ctx)

	// Print available MCP tools
	fmt.Println("\n🚀 MCP Basic Example Started")
	fmt.Println("\nAvailable MCP Endpoints:")
	fmt.Println("  - Server Info:  http://localhost:8080/_/mcp/info")
	fmt.Println("  - List Tools:   http://localhost:8080/_/mcp/tools")
	fmt.Println("  - Call Tool:    POST http://localhost:8080/_/mcp/tools/:name")
	fmt.Println("\nExample Tools:")
	fmt.Println("  - example_get_users       → GET /users")
	fmt.Println("  - example_get_users_id    → GET /users/:id")
	fmt.Println("  - example_create_users    → POST /users")
	fmt.Println("  - example_update_users_id → PUT /users/:id")
	fmt.Println("  - example_delete_users_id → DELETE /users/:id")
	fmt.Println("\nTest Commands:")
	fmt.Println("  # List all tools")
	fmt.Println("  curl http://localhost:8080/_/mcp/tools")
	fmt.Println("\n  # Get user by ID")
	fmt.Println(`  curl -X POST http://localhost:8080/_/mcp/tools/example_get_users_id \
    -H "Content-Type: application/json" \
    -d '{"name":"example_get_users_id","arguments":{"id":"1"}}'`)
	fmt.Println("\n  # Create a new user")
	fmt.Println(`  curl -X POST http://localhost:8080/_/mcp/tools/example_create_users \
    -H "Content-Type: application/json" \
    -d '{"name":"example_create_users","arguments":{"body":{"name":"Charlie","email":"charlie@example.com"}}}'`)
	fmt.Println("")

	// Run HTTP server
	if err := app.Run(); err != nil {
		fmt.Printf("Server error: %v\n", err)
		os.Exit(1)
	}
}

func registerUserRoutes(router forge.Router) {
	// List all users
	router.GET("/users", listUsers,
		forge.WithName("list-users"),
		forge.WithSummary("List all users"),
		forge.WithTags("users"),
	)

	// Get user by ID
	router.GET("/users/:id", getUser,
		forge.WithName("get-user"),
		forge.WithSummary("Get a user by ID"),
		forge.WithTags("users"),
	)

	// Create user
	router.POST("/users", createUser,
		forge.WithName("create-user"),
		forge.WithSummary("Create a new user"),
		forge.WithTags("users"),
	)

	// Update user
	router.PUT("/users/:id", updateUser,
		forge.WithName("update-user"),
		forge.WithSummary("Update an existing user"),
		forge.WithTags("users"),
	)

	// Delete user
	router.DELETE("/users/:id", deleteUser,
		forge.WithName("delete-user"),
		forge.WithSummary("Delete a user"),
		forge.WithTags("users"),
	)
}

func listUsers(ctx forge.Context) error {
	userList := make([]User, 0, len(users))
	for _, user := range users {
		userList = append(userList, user)
	}
	return ctx.JSON(http.StatusOK, userList)
}

func getUser(ctx forge.Context) error {
	id := ctx.Param("id")
	user, exists := users[id]
	if !exists {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": "user not found",
		})
	}
	return ctx.JSON(http.StatusOK, user)
}

func createUser(ctx forge.Context) error {
	var user User
	if err := ctx.Bind(&user); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	// Generate simple ID
	user.ID = fmt.Sprintf("%d", len(users)+1)
	users[user.ID] = user

	return ctx.JSON(http.StatusCreated, user)
}

func updateUser(ctx forge.Context) error {
	id := ctx.Param("id")
	existingUser, exists := users[id]
	if !exists {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": "user not found",
		})
	}

	var updates User
	if err := ctx.Bind(&updates); err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	// Update fields
	if updates.Name != "" {
		existingUser.Name = updates.Name
	}
	if updates.Email != "" {
		existingUser.Email = updates.Email
	}
	users[id] = existingUser

	return ctx.JSON(http.StatusOK, existingUser)
}

func deleteUser(ctx forge.Context) error {
	id := ctx.Param("id")
	if _, exists := users[id]; !exists {
		return ctx.JSON(http.StatusNotFound, map[string]string{
			"error": "user not found",
		})
	}

	delete(users, id)
	return ctx.NoContent(http.StatusNoContent)
}
