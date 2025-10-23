package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/xraph/forge"
)

// Domain models
type User struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Role     string   `json:"role"`
	Tags     []string `json:"tags,omitempty"`
}

type CreateUserRequest struct {
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Role     string   `json:"role,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type UpdateUserRequest struct {
	Username string   `json:"username,omitempty"`
	Email    string   `json:"email,omitempty"`
	Role     string   `json:"role,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// In-memory user store (for demo purposes)
var users = map[string]User{
	"1": {
		ID:       "1",
		Username: "johndoe",
		Email:    "john@example.com",
		Role:     "admin",
		Tags:     []string{"premium", "verified"},
	},
	"2": {
		ID:       "2",
		Username: "janedoe",
		Email:    "jane@example.com",
		Role:     "user",
		Tags:     []string{"verified"},
	},
}

func main() {
	// Create app with OpenAPI configuration
	app := forge.NewApp(forge.AppConfig{
		Name:        "openapi-demo",
		Version:     "1.0.0",
		Description: "OpenAPI demonstration with Swagger UI",
		Environment: "development",
		HTTPAddress: ":8083",
		RouterOptions: []forge.RouterOption{
			forge.WithOpenAPI(forge.OpenAPIConfig{
				Title:       "User Management API",
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

				// Global tags for grouping operations
				Tags: []forge.OpenAPITag{
					{
						Name:        "users",
						Description: "User management operations",
						ExternalDocs: &forge.ExternalDocs{
							Description: "Learn more about users",
							URL:         "https://docs.example.com/users",
						},
					},
					{
						Name:        "health",
						Description: "Health check operations",
					},
					{
						Name:        "admin",
						Description: "Administrative operations",
					},
				},

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
	})

	// Register all routes with OpenAPI metadata

	// 1. List users - GET /users
	app.Router().GET("/users",
		listUsersHandler,
		forge.WithSummary("List all users"),
		forge.WithDescription("Retrieve a list of all users in the system with their details"),
		forge.WithTags("users"),
		forge.WithOperationID("listUsers"),
	)

	// 2. Get user by ID - GET /users/:id
	app.Router().GET("/users/:id",
		getUserHandler,
		forge.WithSummary("Get user by ID"),
		forge.WithDescription("Retrieve detailed information about a specific user by their unique identifier"),
		forge.WithTags("users"),
		forge.WithOperationID("getUser"),
	)

	// 3. Create user - POST /users
	app.Router().POST("/users",
		createUserHandler,
		forge.WithSummary("Create a new user"),
		forge.WithDescription("Create a new user account with the provided information"),
		forge.WithTags("users", "admin"),
		forge.WithOperationID("createUser"),
	)

	// 4. Update user - PUT /users/:id
	app.Router().PUT("/users/:id",
		updateUserHandler,
		forge.WithSummary("Update user"),
		forge.WithDescription("Update an existing user's information by their ID"),
		forge.WithTags("users", "admin"),
		forge.WithOperationID("updateUser"),
	)

	// 5. Delete user - DELETE /users/:id
	app.Router().DELETE("/users/:id",
		deleteUserHandler,
		forge.WithSummary("Delete user"),
		forge.WithDescription("Permanently delete a user account from the system"),
		forge.WithTags("users", "admin"),
		forge.WithOperationID("deleteUser"),
	)

	// 6. Search users - GET /users/search
	app.Router().GET("/users/search",
		searchUsersHandler,
		forge.WithSummary("Search users"),
		forge.WithDescription("Search for users by username, email, or role"),
		forge.WithTags("users"),
		forge.WithOperationID("searchUsers"),
	)

	// 7. Health check
	app.Router().GET("/health",
		healthCheckHandler,
		forge.WithSummary("Health check"),
		forge.WithDescription("Check the health status of the API service"),
		forge.WithTags("health"),
		forge.WithOperationID("healthCheck"),
	)

	// Group example - Admin routes
	adminGroup := app.Router().Group("/admin",
		forge.WithGroupTags("admin"),
		forge.WithGroupMetadata("requiresAuth", true),
	)

	adminGroup.GET("/stats",
		statsHandler,
		forge.WithSummary("Get system statistics"),
		forge.WithDescription("Retrieve system-wide statistics (admin only)"),
		forge.WithOperationID("getStats"),
	)

	adminGroup.POST("/maintenance",
		maintenanceHandler,
		forge.WithSummary("Toggle maintenance mode"),
		forge.WithDescription("Enable or disable maintenance mode for the system"),
		forge.WithOperationID("toggleMaintenance"),
	)

	// Mount the OpenAPI-enabled router
	app.Router().Group("/api/v1").Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			app.Router().ServeHTTP(w, r)
		})
	})

	// Print startup information
	fmt.Println("")
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║           OpenAPI Demo - Swagger UI Example               ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println("")
	fmt.Println("🚀 Server starting on http://localhost:8080")
	fmt.Println("")
	fmt.Println("📚 OpenAPI Documentation:")
	fmt.Printf("   • Swagger UI:    http://localhost:8080/api/v1/swagger\n")
	fmt.Printf("   • OpenAPI Spec:  http://localhost:8080/api/v1/openapi.json\n")
	fmt.Println("")
	fmt.Println("📌 API Endpoints:")
	fmt.Println("   • GET    /api/v1/users           - List all users")
	fmt.Println("   • GET    /api/v1/users/:id       - Get user by ID")
	fmt.Println("   • POST   /api/v1/users           - Create new user")
	fmt.Println("   • PUT    /api/v1/users/:id       - Update user")
	fmt.Println("   • DELETE /api/v1/users/:id       - Delete user")
	fmt.Println("   • GET    /api/v1/users/search    - Search users")
	fmt.Println("   • GET    /api/v1/health          - Health check")
	fmt.Println("   • GET    /api/v1/admin/stats     - System stats")
	fmt.Println("   • POST   /api/v1/admin/maintenance - Toggle maintenance")
	fmt.Println("")
	fmt.Println("🧪 Test Examples:")
	fmt.Println("   curl http://localhost:8080/api/v1/users")
	fmt.Println("   curl http://localhost:8080/api/v1/users/1")
	fmt.Println("   curl -X POST http://localhost:8080/api/v1/users -H 'Content-Type: application/json' \\")
	fmt.Println("        -d '{\"username\":\"alice\",\"email\":\"alice@example.com\",\"password\":\"secret123\"}'")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop...")
	fmt.Println("")

	// Run the application
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// Handler implementations

func listUsersHandler(ctx forge.Context) error {
	userList := make([]User, 0, len(users))
	for _, user := range users {
		userList = append(userList, user)
	}
	return ctx.JSON(200, map[string]interface{}{
		"users": userList,
		"count": len(userList),
	})
}

func getUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	user, ok := users[id]
	if !ok {
		return ctx.JSON(404, ErrorResponse{
			Error:   "not_found",
			Code:    "USER_NOT_FOUND",
			Message: fmt.Sprintf("User with ID %s not found", id),
		})
	}
	return ctx.JSON(200, user)
}

func createUserHandler(ctx forge.Context) error {
	var req CreateUserRequest
	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, ErrorResponse{
			Error:   "bad_request",
			Code:    "INVALID_REQUEST",
			Message: "Invalid request body",
		})
	}

	// Validation
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return ctx.JSON(400, ErrorResponse{
			Error:   "validation_error",
			Code:    "MISSING_FIELDS",
			Message: "Username, email, and password are required",
		})
	}

	// Create user
	newID := fmt.Sprintf("%d", len(users)+1)
	newUser := User{
		ID:       newID,
		Username: req.Username,
		Email:    req.Email,
		Role:     req.Role,
		Tags:     req.Tags,
	}
	if newUser.Role == "" {
		newUser.Role = "user"
	}

	users[newID] = newUser

	return ctx.JSON(201, newUser)
}

func updateUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	user, ok := users[id]
	if !ok {
		return ctx.JSON(404, ErrorResponse{
			Error:   "not_found",
			Code:    "USER_NOT_FOUND",
			Message: fmt.Sprintf("User with ID %s not found", id),
		})
	}

	var req UpdateUserRequest
	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, ErrorResponse{
			Error:   "bad_request",
			Code:    "INVALID_REQUEST",
			Message: "Invalid request body",
		})
	}

	// Update fields
	if req.Username != "" {
		user.Username = req.Username
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Role != "" {
		user.Role = req.Role
	}
	if req.Tags != nil {
		user.Tags = req.Tags
	}

	users[id] = user

	return ctx.JSON(200, user)
}

func deleteUserHandler(ctx forge.Context) error {
	id := ctx.Param("id")
	if _, ok := users[id]; !ok {
		return ctx.JSON(404, ErrorResponse{
			Error:   "not_found",
			Code:    "USER_NOT_FOUND",
			Message: fmt.Sprintf("User with ID %s not found", id),
		})
	}

	delete(users, id)

	return ctx.JSON(200, SuccessResponse{
		Success: true,
		Message: fmt.Sprintf("User %s deleted successfully", id),
	})
}

func searchUsersHandler(ctx forge.Context) error {
	query := ctx.Query("q")
	role := ctx.Query("role")

	var results []User
	for _, user := range users {
		if query != "" {
			if user.Username == query || user.Email == query {
				results = append(results, user)
			}
		} else if role != "" {
			if user.Role == role {
				results = append(results, user)
			}
		}
	}

	return ctx.JSON(200, map[string]interface{}{
		"results": results,
		"count":   len(results),
		"query":   query,
		"role":    role,
	})
}

func healthCheckHandler(ctx forge.Context) error {
	return ctx.JSON(200, map[string]interface{}{
		"status": "healthy",
		"uptime": "running",
		"checks": map[string]string{
			"database": "ok",
			"cache":    "ok",
			"api":      "ok",
		},
	})
}

func statsHandler(ctx forge.Context) error {
	return ctx.JSON(200, map[string]interface{}{
		"total_users":    len(users),
		"total_requests": 1234,
		"uptime_seconds": 3600,
		"version":        "1.0.0",
	})
}

func maintenanceHandler(ctx forge.Context) error {
	var req struct {
		Enabled bool   `json:"enabled"`
		Message string `json:"message"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return ctx.JSON(400, ErrorResponse{
			Error:   "bad_request",
			Code:    "INVALID_REQUEST",
			Message: "Invalid request body",
		})
	}

	return ctx.JSON(200, SuccessResponse{
		Success: true,
		Message: fmt.Sprintf("Maintenance mode %s", map[bool]string{true: "enabled", false: "disabled"}[req.Enabled]),
	})
}
