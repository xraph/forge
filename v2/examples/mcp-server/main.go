package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/mcp"
)

// User represents a user in our system
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// CreateUserRequest represents a request to create a user
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Example: MCP Server with Forge
//
// This example demonstrates how to expose Forge routes as MCP (Model Context Protocol)
// tools, allowing AI assistants like Claude to interact with your API.
//
// The MCP extension automatically:
// - Exposes routes as MCP tools
// - Generates JSON schemas from request/response types
// - Provides MCP server endpoints
//
// Run this example:
//
//	go run main.go
//
// Then access MCP endpoints:
//
//	curl http://localhost:8080/_/mcp/info
//	curl http://localhost:8080/_/mcp/tools
//	curl -X POST http://localhost:8080/_/mcp/tools/create_user -d '{"name":"create_user","arguments":{"name":"John","email":"john@example.com"}}'
func main() {
	// Create a new Forge application with MCP extension
	app := forge.NewApp(forge.AppConfig{
		Name:    "mcp-demo",
		Version: "1.0.0",
		Extensions: []forge.Extension{
			// MCP extension - exposes routes as MCP tools
			mcp.NewExtension(mcp.WithConfig(mcp.Config{
				BasePath:         "/_/mcp",
				AutoExposeRoutes: true,
				ExcludePatterns: []string{
					"/_/*",
					"/internal/*",
				},
			})),
		},
	})

	// In-memory user storage (for demo purposes)
	users := make(map[string]*User)
	nextID := 1

	// Define routes - these will automatically become MCP tools

	// GET /users - List all users
	app.Router().GET("/users", func(ctx forge.Context) error {
		userList := make([]*User, 0, len(users))
		for _, user := range users {
			userList = append(userList, user)
		}
		return ctx.JSON(http.StatusOK, userList)
	})

	// GET /users/:id - Get user by ID
	app.Router().GET("/users/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")
		user, exists := users[id]
		if !exists {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "user not found",
			})
		}
		return ctx.JSON(http.StatusOK, user)
	})

	// POST /users - Create a new user
	app.Router().POST("/users", func(ctx forge.Context) error {
		var req CreateUserRequest
		if err := ctx.Bind(&req); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid request: " + err.Error(),
			})
		}

		// Create new user
		id := fmt.Sprintf("%d", nextID)
		nextID++

		user := &User{
			ID:    id,
			Name:  req.Name,
			Email: req.Email,
		}
		users[id] = user

		return ctx.JSON(http.StatusCreated, user)
	})

	// PUT /users/:id - Update user
	app.Router().PUT("/users/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")
		user, exists := users[id]
		if !exists {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "user not found",
			})
		}

		var req CreateUserRequest
		if err := ctx.Bind(&req); err != nil {
			return ctx.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid request: " + err.Error(),
			})
		}

		// Update user
		user.Name = req.Name
		user.Email = req.Email

		return ctx.JSON(http.StatusOK, user)
	})

	// DELETE /users/:id - Delete user
	app.Router().DELETE("/users/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")
		if _, exists := users[id]; !exists {
			return ctx.JSON(http.StatusNotFound, map[string]string{
				"error": "user not found",
			})
		}

		delete(users, id)

		return ctx.JSON(http.StatusNoContent, nil)
	})

	// Start the application
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start application: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ MCP Server started successfully!")
	fmt.Println("")
	fmt.Println("🔧 API Endpoints:")
	fmt.Println("  GET    http://localhost:8080/users")
	fmt.Println("  GET    http://localhost:8080/users/:id")
	fmt.Println("  POST   http://localhost:8080/users")
	fmt.Println("  PUT    http://localhost:8080/users/:id")
	fmt.Println("  DELETE http://localhost:8080/users/:id")
	fmt.Println("")
	fmt.Println("🤖 MCP Endpoints:")
	fmt.Println("  GET    http://localhost:8080/_/mcp/info      - Server information")
	fmt.Println("  GET    http://localhost:8080/_/mcp/tools     - List available tools")
	fmt.Println("  POST   http://localhost:8080/_/mcp/tools/:name - Execute tool")
	fmt.Println("")
	fmt.Println("📝 Available MCP Tools:")
	fmt.Println("  - get_users      : List all users")
	fmt.Println("  - get_users_id   : Get user by ID")
	fmt.Println("  - create_users   : Create a new user")
	fmt.Println("  - update_users_id: Update user")
	fmt.Println("  - delete_users_id: Delete user")
	fmt.Println("")
	fmt.Println("Example curl commands:")
	fmt.Println("  # List all MCP tools")
	fmt.Println("  curl http://localhost:8080/_/mcp/tools | jq")
	fmt.Println("")
	fmt.Println("  # Get MCP server info")
	fmt.Println("  curl http://localhost:8080/_/mcp/info | jq")
	fmt.Println("")
	fmt.Println("  # Create a user (regular API)")
	fmt.Println("  curl -X POST http://localhost:8080/users \\")
	fmt.Println("    -H 'Content-Type: application/json' \\")
	fmt.Println("    -d '{\"name\":\"Alice\",\"email\":\"alice@example.com\"}' | jq")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop the server")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n🛑 Shutting down gracefully...")
	app.Stop(ctx)
	fmt.Println("✅ Server stopped")
}
