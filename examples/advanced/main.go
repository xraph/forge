package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/router"
	"github.com/xraph/forge/v1"
)

// Example models for documentation
type User struct {
	ID        int       `json:"id" validate:"required" description:"User ID"`
	Username  string    `json:"username" validate:"required,min=3,max=50" description:"Username"`
	Email     string    `json:"email" validate:"required,email" description:"Email address"`
	FullName  string    `json:"full_name,omitempty" description:"Full name"`
	CreatedAt time.Time `json:"created_at" description:"Creation timestamp"`
	UpdatedAt time.Time `json:"updated_at" description:"Last update timestamp"`
	Active    bool      `json:"active" description:"Whether user is active"`
}

type CreateUserRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50" description:"Username"`
	Email    string `json:"email" validate:"required,email" description:"Email address"`
	FullName string `json:"full_name,omitempty" description:"Full name"`
}

type UpdateUserRequest struct {
	Username *string `json:"username,omitempty" validate:"omitempty,min=3,max=50" description:"Username"`
	Email    *string `json:"email,omitempty" validate:"omitempty,email" description:"Email address"`
	FullName *string `json:"full_name,omitempty" description:"Full name"`
	Active   *bool   `json:"active,omitempty" description:"Whether user is active"`
}

type PaginatedResponse struct {
	Data       interface{} `json:"data" description:"Response data"`
	Page       int         `json:"page" description:"Current page number"`
	PageSize   int         `json:"page_size" description:"Items per page"`
	TotalItems int         `json:"total_items" description:"Total number of items"`
	TotalPages int         `json:"total_pages" description:"Total number of pages"`
}

type ErrorResponse struct {
	Error   string                 `json:"error" description:"Error message"`
	Code    string                 `json:"code" description:"Error code"`
	Details map[string]interface{} `json:"details,omitempty" description:"Error details"`
}

// WebSocket message types
type WSMessage struct {
	Type      string      `json:"type" description:"Message type"`
	Channel   string      `json:"channel" description:"Channel name"`
	Data      interface{} `json:"data" description:"Message data"`
	Timestamp time.Time   `json:"timestamp" description:"Message timestamp"`
}

type WSSubscribeMessage struct {
	Type    string `json:"type" description:"Message type (subscribe)"`
	Channel string `json:"channel" description:"Channel to subscribe to"`
}

type WSEventMessage struct {
	Type      string      `json:"type" description:"Message type (event)"`
	Event     string      `json:"event" description:"Event name"`
	Data      interface{} `json:"data" description:"Event data"`
	Timestamp time.Time   `json:"timestamp" description:"Event timestamp"`
}

func main() {
	// Create logger
	log := logger.NewDevelopmentLogger()

	// logger.Config{
	// 	Level:  logger.InfoLevel,
	// 	Format: logger.JSONFormat,
	// }

	// Create application with comprehensive configuration
	app, err := forge.New("forge-demo").
		// Database configuration
		WithSQLite("default", "./demo.db").
		WithMemoryCache("default").
		WithLogging(logger.LoggingConfig{
			Level: "debug",
		}).

		// Observability configuration
		WithTracing(observability.TracingConfig{
			Enabled:     true,
			ServiceName: "forge-demo",
			SampleRate:  1.0, // Sample all requests in demo
		}).
		WithMetrics(observability.MetricsConfig{
			Enabled:     true,
			ServiceName: "forge-demo",
			Port:        9090,
			Path:        "/metrics",
		}).
		WithHealth(observability.HealthConfig{
			Enabled: true,
			Path:    "/health",
			Timeout: 30 * time.Second,
		}).

		// Job processing configuration
		WithJobs(jobs.Config{
			Enabled:     true,
			Backend:     "memory",
			Concurrency: 5,
			Queues: map[string]jobs.QueueConfig{
				"default": {
					Priority: 10,
				},
				"email": {Priority: 5},
			},
		}).

		// HTTP server configuration
		WithHost("0.0.0.0").
		WithPort(8080).
		WithCORS(router.CORSConfig{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{"*"},
		}).

		// Feature flags
		EnableJobs().
		EnableCache().
		EnableTracing().
		EnableMetrics().
		EnableHealthChecks().

		// Development features
		WithDevelopmentMode().
		WithHotReload().

		// Build the application
		Build()

	if err != nil {
		log.Fatalf("Failed to build application: %v", err)
	}

	r := app.Router()

	// Configure comprehensive documentation
	docConfig := router.DocumentationConfig{
		Title:       "Forge API Example",
		Description: "A comprehensive example API built with Forge framework",
		Version:     "1.2.3",
		Contact: router.ContactInfo{
			Name:  "API Team",
			Email: "api@example.com",
			URL:   "https://example.com/contact",
		},
		License: router.LicenseInfo{
			Name: "MIT",
			URL:  "https://opensource.org/licenses/MIT",
		},
		Servers: []router.ServerInfo{
			{
				URL:         "https://api.example.com/v1",
				Description: "Production server",
			},
			{
				URL:         "https://staging-api.example.com/v1",
				Description: "Staging server",
			},
			{
				URL:         "http://localhost:8080/v1",
				Description: "Development server",
			},
		},
		SecurityDefinitions: map[string]router.SecurityScheme{
			"bearerAuth": {
				Type:         "http",
				Scheme:       "bearer",
				BearerFormat: "JWT",
				Description:  "JWT token authentication",
			},
			"apiKeyAuth": {
				Type:        "apiKey",
				In:          "header",
				Name:        "X-API-Key",
				Description: "API key authentication",
			},
		},
		OpenAPI: router.OpenAPIConfig{
			Version:              "3.0.0",
			UseJSONTags:          true,
			UseValidationTags:    true,
			GenerateExamples:     true,
			IncludePrivateFields: false,
			GenerateComponents:   true,
			ReferenceComponents:  true,
			IncludeErrorModels:   true,
			DefaultSecurity: []router.SecurityRequirement{
				router.BearerAuth(),
			},
		},
		AsyncAPI: router.AsyncAPIConfig{
			Version:                 "2.6.0",
			DefaultProtocol:         "ws",
			GenerateMessageExamples: true,
			IncludeMessageHeaders:   true,
		},
		EnableAutoGeneration: true,
		EnableSwaggerUI:      true,
		EnableReDoc:          true,
		EnableAsyncAPIUI:     true,
		EnablePostman:        true,
		SwaggerUIPath:        "/docs",
		ReDocPath:            "/redoc",
		AsyncAPIUIPath:       "/asyncapi",
		OpenAPISpecPath:      "/openapi.json",
		AsyncAPISpecPath:     "/asyncapi.json",
		PostmanPath:          "/postman.json",
	}

	// Enable documentation with auto-generation
	r.EnableDocumentation(docConfig)

	// Register schemas for reuse
	docGen := router.NewDocumentationGenerator(docConfig, log)
	docGen.RegisterSchemaFromType("User", reflect.TypeOf(User{}))
	docGen.RegisterSchemaFromType("CreateUserRequest", reflect.TypeOf(CreateUserRequest{}))
	docGen.RegisterSchemaFromType("UpdateUserRequest", reflect.TypeOf(UpdateUserRequest{}))
	docGen.RegisterSchemaFromType("PaginatedResponse", reflect.TypeOf(PaginatedResponse{}))
	docGen.RegisterSchemaFromType("ErrorResponse", reflect.TypeOf(ErrorResponse{}))

	// API v1 routes with comprehensive documentation
	api := r.APIGroup("/api/v1",
		router.WithDescription("API version 1 endpoints"),
		router.WithTags("api", "v1"),
	)

	// Users API with detailed documentation
	users := api.Group("/users",
		router.WithDescription("User management endpoints"),
		router.WithTags("users"),
	)

	// GET /api/v1/users - List users with pagination
	users.DocGet("", handleListUsers,
		router.NewRouteDoc(
			"List users",
			"Retrieve a paginated list of users with optional filtering",
		).WithTags("users").
			WithParameter("page", "query", "Page number", false, router.IntegerSchema("Page number (default: 1)")).
			WithParameter("page_size", "query", "Page size", false, router.IntegerSchema("Items per page (default: 20, max: 100)")).
			WithParameter("search", "query", "Search term", false, router.StringSchema("Search in username, email, or full name")).
			WithParameter("active", "query", "Filter by active status", false, router.BooleanSchema("Filter by active status")).
			WithResponse("200", "Success", router.RefSchema("#/components/schemas/PaginatedResponse")).
			WithResponse("400", "Bad Request", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithResponse("401", "Unauthorized", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithSecurity(router.BearerAuth()).
			WithExample("success", map[string]interface{}{
				"data": []User{
					{
						ID:        1,
						Username:  "john_doe",
						Email:     "john@example.com",
						FullName:  "John Doe",
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
						Active:    true,
					},
				},
				"page":        1,
				"page_size":   20,
				"total_items": 1,
				"total_pages": 1,
			}),
	)

	// POST /api/v1/users - Create user
	users.DocPost("", handleCreateUser,
		router.NewRouteDoc(
			"Create user",
			"Create a new user account",
		).WithTags("users").
			WithRequestBody("User data", router.RefSchema("#/components/schemas/CreateUserRequest")).
			WithResponse("201", "User created", router.RefSchema("#/components/schemas/User")).
			WithResponse("400", "Bad Request", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithResponse("409", "User already exists", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithSecurity(router.BearerAuth()).
			WithExample("request", CreateUserRequest{
				Username: "jane_doe",
				Email:    "jane@example.com",
				FullName: "Jane Doe",
			}),
	)

	// GET /api/v1/users/{id} - Get user by ID
	users.DocGet("/{id}", handleGetUser,
		router.NewRouteDoc(
			"Get user by ID",
			"Retrieve a specific user by their ID",
		).WithTags("users").
			WithParameter("id", "path", "User ID", true, router.IntegerSchema("User ID")).
			WithResponse("200", "Success", router.RefSchema("#/components/schemas/User")).
			WithResponse("404", "User not found", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithResponse("401", "Unauthorized", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithSecurity(router.BearerAuth()),
	)

	// PUT /api/v1/users/{id} - Update user
	users.DocPut("/{id}", handleUpdateUser,
		router.NewRouteDoc(
			"Update user",
			"Update an existing user's information",
		).WithTags("users").
			WithParameter("id", "path", "User ID", true, router.IntegerSchema("User ID")).
			WithRequestBody("Updated user data", router.RefSchema("#/components/schemas/UpdateUserRequest")).
			WithResponse("200", "User updated", router.RefSchema("#/components/schemas/User")).
			WithResponse("400", "Bad Request", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithResponse("404", "User not found", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithResponse("401", "Unauthorized", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithSecurity(router.BearerAuth()),
	)

	// DELETE /api/v1/users/{id} - Delete user
	users.DocDelete("/{id}", handleDeleteUser,
		router.NewRouteDoc(
			"Delete user",
			"Delete a user account",
		).WithTags("users").
			WithParameter("id", "path", "User ID", true, router.IntegerSchema("User ID")).
			WithResponse("204", "User deleted", nil).
			WithResponse("404", "User not found", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithResponse("401", "Unauthorized", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithResponse("403", "Forbidden", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithSecurity(router.BearerAuth()),
	)

	// Real-time endpoints with AsyncAPI documentation
	realtime := api.WebSocketGroup("/ws",
		router.WithDescription("Real-time WebSocket endpoints"),
		router.WithTags("realtime", "websocket"),
	)

	// WebSocket endpoint for user events
	realtime.DocWebSocket("/users", handleUserEvents,
		router.NewAsyncRouteDoc(
			"User events stream",
			"Real-time stream of user-related events",
		).WithTags("users", "events").
			WithMessage("subscribe", "Subscribe", "Subscribe to user events", map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":        "string",
						"const":       "subscribe",
						"description": "Message type",
					},
					"channel": map[string]interface{}{
						"type":        "string",
						"description": "Channel to subscribe to",
					},
					"filters": map[string]interface{}{
						"type":        "object",
						"description": "Event filters",
						"properties": map[string]interface{}{
							"user_id": map[string]interface{}{
								"type":        "integer",
								"description": "Filter by user ID",
							},
							"event_types": map[string]interface{}{
								"type":        "array",
								"items":       map[string]interface{}{"type": "string"},
								"description": "Filter by event types",
							},
						},
					},
				},
				"required": []string{"type", "channel"},
			}).
			WithMessage("event", "User Event", "User-related event notification", map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":        "string",
						"const":       "event",
						"description": "Message type",
					},
					"event": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"user.created", "user.updated", "user.deleted", "user.activated", "user.deactivated"},
						"description": "Event type",
					},
					"data": map[string]interface{}{
						"type":        "object",
						"description": "Event data",
					},
					"timestamp": map[string]interface{}{
						"type":        "string",
						"format":      "date-time",
						"description": "Event timestamp",
					},
				},
				"required": []string{"type", "event", "data", "timestamp"},
			}).
			WithSecurity(router.BearerAuth()).
			WithExample("subscribe_example", WSSubscribeMessage{
				Type:    "subscribe",
				Channel: "users",
			}).
			WithExample("event_example", WSEventMessage{
				Type:      "event",
				Event:     "user.created",
				Data:      map[string]interface{}{"user_id": 123},
				Timestamp: time.Now(),
			}),
	)

	// Server-Sent Events endpoint
	realtime.DocSSE("/notifications", handleNotifications,
		router.NewAsyncRouteDoc(
			"Notifications stream",
			"Server-sent events stream for real-time notifications",
		).WithTags("notifications", "sse").
			WithMessage("notification", "Notification", "Real-time notification", map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Notification ID",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"info", "warning", "error", "success"},
						"description": "Notification type",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Notification title",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Notification message",
					},
					"timestamp": map[string]interface{}{
						"type":        "string",
						"format":      "date-time",
						"description": "Notification timestamp",
					},
				},
				"required": []string{"id", "type", "title", "message", "timestamp"},
			}).
			WithSecurity(router.BearerAuth()),
	)

	// Admin endpoints with special security
	admin := api.AdminGroup("/admin",
		router.WithDescription("Administrative endpoints"),
		router.WithTags("admin"),
		router.WithRoles("admin"),
	)

	admin.DocGet("/stats", handleAdminStats,
		router.NewRouteDoc(
			"Get system statistics",
			"Retrieve system-wide statistics (admin only)",
		).WithTags("admin", "stats").
			WithResponse("200", "Statistics", router.ObjectSchema("System statistics", map[string]*router.Schema{
				"users_count":       router.IntegerSchema("Total users"),
				"active_users":      router.IntegerSchema("Active users"),
				"api_calls_today":   router.IntegerSchema("API calls today"),
				"avg_response_time": router.IntegerSchema("Average response time (ms)"),
			}, []string{"users_count", "active_users", "api_calls_today", "avg_response_time"})).
			WithResponse("401", "Unauthorized", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithResponse("403", "Forbidden", router.RefSchema("#/components/schemas/ErrorResponse")).
			WithSecurity(router.BearerAuth()),
	)

	// Public endpoints without authentication
	public := api.PublicGroup("/public",
		router.WithDescription("Public endpoints"),
		router.WithTags("public"),
	)

	public.DocGet("/health", handleHealth,
		router.NewRouteDoc(
			"Health check",
			"Check API health status",
		).WithTags("health", "monitoring").
			WithResponse("200", "Healthy", router.ObjectSchema("Health status", map[string]*router.Schema{
				"status":    router.StringSchema("Health status"),
				"timestamp": router.StringSchema("Check timestamp"),
				"version":   router.StringSchema("API version"),
			}, []string{"status", "timestamp", "version"})),
	)

	public.DocGet("/version", handleVersion,
		router.NewRouteDoc(
			"Get API version",
			"Retrieve API version information",
		).WithTags("version", "info").
			WithResponse("200", "Version info", router.ObjectSchema("Version information", map[string]*router.Schema{
				"version":     router.StringSchema("API version"),
				"build_date":  router.StringSchema("Build date"),
				"commit_hash": router.StringSchema("Git commit hash"),
			}, []string{"version", "build_date", "commit_hash"})),
	)

	// Add custom middleware for documentation
	r.Use(router.DocumentationMiddleware(docConfig))

	// Start server
	fmt.Println("🚀 Server starting on :8080")
	fmt.Println("📚 Documentation available at:")
	fmt.Println("  - Swagger UI: http://localhost:8080/docs")
	fmt.Println("  - ReDoc: http://localhost:8080/redoc")
	fmt.Println("  - AsyncAPI: http://localhost:8080/asyncapi")
	fmt.Println("  - OpenAPI JSON: http://localhost:8080/openapi.json")
	fmt.Println("  - AsyncAPI JSON: http://localhost:8080/asyncapi.json")
	fmt.Println("  - Postman Collection: http://localhost:8080/postman.json")

	log.Fatal(http.ListenAndServe(":8080", r.Handler()))
}

// Handler implementations
func handleListUsers(w http.ResponseWriter, r *http.Request) {
	// Implementation would handle pagination, filtering, etc.
	users := []User{
		{
			ID:        1,
			Username:  "john_doe",
			Email:     "john@example.com",
			FullName:  "John Doe",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Active:    true,
		},
	}

	response := PaginatedResponse{
		Data:       users,
		Page:       1,
		PageSize:   20,
		TotalItems: len(users),
		TotalPages: 1,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	user := User{
		ID:        123,
		Username:  req.Username,
		Email:     req.Email,
		FullName:  req.FullName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Active:    true,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func handleGetUser(w http.ResponseWriter, r *http.Request) {
	// Implementation would get user by ID from path
	user := User{
		ID:        1,
		Username:  "john_doe",
		Email:     "john@example.com",
		FullName:  "John Doe",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Active:    true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	user := User{
		ID:        1,
		Username:  "john_doe_updated",
		Email:     "john.updated@example.com",
		FullName:  "John Doe Updated",
		CreatedAt: time.Now().Add(-24 * time.Hour),
		UpdatedAt: time.Now(),
		Active:    true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func handleUserEvents(ctx context.Context, conn router.WebSocketConnection) error {
	// Implementation would handle WebSocket connections for user events
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Handle different message types
		switch msg.Type {
		case "subscribe":
			// Handle subscription
			response := WSEventMessage{
				Type:      "event",
				Event:     "subscribed",
				Data:      map[string]interface{}{"channel": msg.Channel},
				Timestamp: time.Now(),
			}
			conn.WriteJSON(response)
		}
	}
}

func handleNotifications(ctx context.Context, stream router.SSEStream) error {
	// Implementation would handle SSE for notifications
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			notification := map[string]interface{}{
				"id":        fmt.Sprintf("notif_%d", time.Now().Unix()),
				"type":      "info",
				"title":     "Test Notification",
				"message":   "This is a test notification",
				"timestamp": time.Now().Format(time.RFC3339),
			}

			if err := stream.SendJSON(notification); err != nil {
				return err
			}
		}
	}
}

func handleAdminStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"users_count":       1000,
		"active_users":      750,
		"api_calls_today":   50000,
		"avg_response_time": 45,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.2.3",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	version := map[string]interface{}{
		"version":     "1.2.3",
		"build_date":  "2024-01-15T10:00:00Z",
		"commit_hash": "abc123def456",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(version)
}
