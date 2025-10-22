package main

import (
	"fmt"
	"net/http"

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
		Name:        "mcp-demo",
		Version:     "1.0.0",
		HTTPAddress: ":8087",
		RouterOptions: []forge.RouterOption{
			forge.WithOpenAPI(forge.OpenAPIConfig{
				Title:       "MCP API",
				Description: "A comprehensive REST API for user management operations with full OpenAPI 3.1.0 specification",
				Version:     "1.0.0",

				// Server configuration
				Servers: []forge.OpenAPIServer{
					{
						URL:         "http://localhost:8080",
						Description: "Development server",
					},
					{
						URL:         "https://api.example.com",
						Description: "Production server",
					},
				},

				// Security schemes
				Security: map[string]forge.SecurityScheme{
					"bearerAuth": {
						Type:         "http",
						Scheme:       "bearer",
						BearerFormat: "JWT",
						Description:  "JWT Bearer token authentication",
					},
					"apiKey": {
						Type:        "apiKey",
						In:          "header",
						Name:        "X-API-Key",
						Description: "API key for accessing the API",
					},
				},

				// // Global tags for grouping operations
				// Tags: []forge.OpenAPITag{
				// 	{
				// 		Name:        "users",
				// 		Description: "User management operations",
				// 		ExternalDocs: &forge.ExternalDocs{
				// 			Description: "Learn more about users",
				// 			URL:         "https://docs.example.com/users",
				// 		},
				// 	},
				// 	{
				// 		Name:        "health",
				// 		Description: "Health check operations",
				// 	},
				// 	{
				// 		Name:        "admin",
				// 		Description: "Administrative operations",
				// 	},
				// },

				// External documentation
				ExternalDocs: &forge.ExternalDocs{
					Description: "API Documentation",
					URL:         "https://docs.example.com",
				},

				// Contact information
				Contact: &forge.Contact{
					Name:  "API Support",
					Email: "support@example.com",
					URL:   "https://support.example.com",
				},

				// License
				License: &forge.License{
					Name: "Apache 2.0",
					URL:  "https://www.apache.org/licenses/LICENSE-2.0.html",
				},

				// UI settings
				UIPath:      "/swagger",
				SpecPath:    "/openapi.json",
				UIEnabled:   true,
				SpecEnabled: true,
				PrettyJSON:  true,
			}),
		},
		Extensions: []forge.Extension{
			// MCP extension - exposes routes as MCP tools
			mcp.NewExtension(mcp.WithConfig(mcp.Config{
				Enabled:           true,
				MaxToolNameLength: 90,
				BasePath:          "/_/mcp",
				AutoExposeRoutes:  true,
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

	if err := app.Run(); err != nil {
		app.Logger().Fatal("app failed", forge.F("error", err))
	}
}
