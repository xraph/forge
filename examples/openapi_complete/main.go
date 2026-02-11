package main

import (
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
)

// Domain models
type User struct {
	ID        string    `json:"id" description:"User's unique identifier"`
	Name      string    `json:"name" description:"User's full name" minLength:"1" maxLength:"100"`
	Email     string    `json:"email" description:"User's email address" format:"email"`
	Age       int       `json:"age,omitempty" description:"User's age" minimum:"0" maximum:"150"`
	Role      string    `json:"role" description:"User's role" enum:"admin,user,guest" default:"user"`
	CreatedAt time.Time `json:"createdAt" format:"date-time"`
}

type CreateUserRequest struct {
	Name  string `json:"name" description:"User's full name" minLength:"1" maxLength:"100"`
	Email string `json:"email" description:"User's email address" format:"email"`
	Age   int    `json:"age,omitempty" description:"User's age" minimum:"0" maximum:"150"`
	Role  string `json:"role,omitempty" description:"User's role" enum:"admin,user,guest" default:"user"`
}

type UpdateUserRequest struct {
	Name *string `json:"name,omitempty" description:"User's full name" minLength:"1" maxLength:"100"`
	Age  *int    `json:"age,omitempty" description:"User's age" minimum:"0" maximum:"150"`
	Role *string `json:"role,omitempty" description:"User's role" enum:"admin,user,guest"`
}

type ListUsersQuery struct {
	Page     int    `query:"page" description:"Page number" minimum:"1" default:"1"`
	PageSize int    `query:"pageSize" description:"Items per page" minimum:"1" maximum:"100" default:"20"`
	Search   string `query:"search,omitempty" description:"Search term"`
	Role     string `query:"role,omitempty" description:"Filter by role" enum:"admin,user,guest"`
}

type CustomHeaders struct {
	RequestID string `header:"X-Request-ID,omitempty" description:"Request correlation ID" format:"uuid"`
	APIKey    string `header:"X-API-Key,omitempty" description:"API authentication key"`
}

// File upload request
type FileUploadRequest struct {
	File        []byte `json:"file" format:"binary" description:"File to upload"`
	Description string `json:"description,omitempty" description:"File description" maxLength:"500"`
}

// Event notification for callbacks
type UserEventNotification struct {
	EventType string    `json:"eventType" description:"Type of event" enum:"created,updated,deleted"`
	User      User      `json:"user" description:"User data"`
	Timestamp time.Time `json:"timestamp" format:"date-time"`
}

func main() {
	// Create Forge app with OpenAPI configuration
	app := forge.NewApp(forge.AppConfig{
		Name:        "openapi-complete",
		Version:     "1.0.0",
		Description: "Comprehensive OpenAPI schema generation demonstration",
		Environment: "development",
		HTTPAddress: ":8084",
		RouterOptions: []forge.RouterOption{
			forge.WithOpenAPI(forge.OpenAPIConfig{
				Title:       "User Management API",
				Description: "A comprehensive API demonstrating OpenAPI schema generation with Forge",
				Version:     "1.0.0",
				// Note: Localhost server is automatically added based on HTTPAddress
				Servers: []forge.OpenAPIServer{
					{
						URL:         "https://api.example.com",
						Description: "Production server",
					},
				},
				Security: map[string]forge.SecurityScheme{
					"bearerAuth": {
						Type:         "http",
						Scheme:       "bearer",
						BearerFormat: "JWT",
						Description:  "JWT Bearer token authentication",
					},
					"apiKey": {
						Type:        "apiKey",
						Name:        "X-API-Key",
						In:          "header",
						Description: "API Key authentication",
					},
				},
				Tags: []forge.OpenAPITag{
					{
						Name:        "users",
						Description: "User management operations",
					},
					{
						Name:        "files",
						Description: "File upload operations",
					},
				},
				UIPath:      "/swagger",
				SpecPath:    "/openapi.json",
				UIEnabled:   true,
				SpecEnabled: true,
				PrettyJSON:  true,
			}),
		},
	})

	// Get router from app
	router := app.Router()

	// Register routes with comprehensive OpenAPI features

	// 1. Opinionated handler with automatic schema extraction
	router.POST("/users", createUser,
		forge.WithName("createUser"),
		forge.WithSummary("Create a new user"),
		forge.WithDescription("Creates a new user with the provided information. Automatically extracts request/response schemas from handler signature."),
		forge.WithTags("users"),
		forge.WithOperationID("createUser"),
		forge.WithRequestExample("example1", map[string]interface{}{
			"name":  "John Doe",
			"email": "john@example.com",
			"age":   30,
			"role":  "user",
		}),
		forge.WithResponseExample(201, "success", map[string]interface{}{
			"id":        "123e4567-e89b-12d3-a456-426614174000",
			"name":      "John Doe",
			"email":     "john@example.com",
			"age":       30,
			"role":      "user",
			"createdAt": "2024-01-15T10:30:00Z",
		}),
		forge.WithResponseSchema(201, "Created", &User{}),
		forge.WithErrorResponses(),
		forge.WithValidationErrorResponse(),
		forge.WithCallback(forge.NewEventCallbackConfig(
			"{$request.body#/webhookUrl}",
			&UserEventNotification{},
		)),
	)

	// 2. List users with query parameters and pagination
	router.GET("/users", listUsers,
		forge.WithName("listUsers"),
		forge.WithSummary("List all users"),
		forge.WithDescription("Returns a paginated list of users with optional filtering"),
		forge.WithTags("users"),
		forge.WithQuerySchema(&ListUsersQuery{}),
		forge.WithPaginatedResponse(&User{}, 200),
		forge.WithErrorResponses(),
	)

	// 3. Get user by ID with path parameters
	router.GET("/users/:id", getUser,
		forge.WithName("getUser"),
		forge.WithSummary("Get a user by ID"),
		forge.WithDescription("Retrieves a single user by their unique ID. Path parameters are automatically extracted."),
		forge.WithTags("users"),
		forge.WithResponseSchema(200, "Success", &User{}),
		forge.WithResponseSchema(404, "User not found", &forge.HTTPError{}),
		forge.WithSecurity("bearerAuth"),
	)

	// 4. Update user with PATCH and header validation
	router.PATCH("/users/:id", updateUser,
		forge.WithName("updateUser"),
		forge.WithSummary("Update a user"),
		forge.WithDescription("Partially updates a user's information"),
		forge.WithTags("users"),
		forge.WithHeaderSchema(&CustomHeaders{}),
		forge.WithRequestSchema(&UpdateUserRequest{}),
		forge.WithResponseSchema(200, "Updated", &User{}),
		forge.WithResponseSchema(404, "User not found", &forge.HTTPError{}),
		forge.WithErrorResponses(),
		forge.WithValidationErrorResponse(),
		forge.WithSecurity("bearerAuth"),
	)

	// 5. Delete user with no content response
	router.DELETE("/users/:id", deleteUser,
		forge.WithName("deleteUser"),
		forge.WithSummary("Delete a user"),
		forge.WithDescription("Permanently deletes a user from the system"),
		forge.WithTags("users"),
		forge.WithNoContentResponse(),
		forge.WithResponseSchema(404, "User not found", &forge.HTTPError{}),
		forge.WithSecurity("bearerAuth"),
		forge.WithDeprecated(), // Mark as deprecated
	)

	// 6. File upload with binary content
	router.POST("/files", uploadFile,
		forge.WithName("uploadFile"),
		forge.WithSummary("Upload a file"),
		forge.WithDescription("Uploads a file with optional description"),
		forge.WithTags("files"),
		forge.WithRequestContentTypes("multipart/form-data", "application/octet-stream"),
		forge.WithRequestSchema(&FileUploadRequest{}),
		forge.WithFileUploadResponse(201),
		forge.WithErrorResponses(),
	)

	// 7. Async operation with accepted response
	router.POST("/users/bulk-import", bulkImportUsers,
		forge.WithName("bulkImportUsers"),
		forge.WithSummary("Bulk import users"),
		forge.WithDescription("Imports multiple users asynchronously"),
		forge.WithTags("users"),
		forge.WithRequestSchema(&[]CreateUserRequest{}),
		forge.WithAcceptedResponse(),
		forge.WithCallback(forge.NewCompletionCallbackConfig(
			"{$request.body#/callbackUrl}",
			&struct {
				JobID      string `json:"jobId"`
				Status     string `json:"status"`
				TotalUsers int    `json:"totalUsers"`
			}{},
		)),
	)

	// 8. Batch operation
	router.POST("/users/batch", batchCreateUsers,
		forge.WithName("batchCreateUsers"),
		forge.WithSummary("Batch create users"),
		forge.WithDescription("Creates multiple users in a single request"),
		forge.WithTags("users"),
		forge.WithRequestSchema(&[]CreateUserRequest{}),
		forge.WithBatchResponse(&User{}, 200),
		forge.WithErrorResponses(),
	)

	// Print startup information
	fmt.Println("")
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║    Complete OpenAPI Features - Comprehensive Demo               ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Println("")
	fmt.Println("🚀 Server starting on http://localhost:8084")
	fmt.Println("")
	fmt.Println("📚 OpenAPI Documentation:")
	fmt.Println("   • Swagger UI:    http://localhost:8084/swagger")
	fmt.Println("   • OpenAPI Spec:  http://localhost:8084/openapi.json")
	fmt.Println("")
	fmt.Println("💡 Features Demonstrated:")
	fmt.Println("   ✓ Automatic schema extraction from opinionated handlers")
	fmt.Println("   ✓ Manual schema specification with struct tags")
	fmt.Println("   ✓ Query & header parameter extraction")
	fmt.Println("   ✓ Multiple response status codes")
	fmt.Println("   ✓ Content type negotiation")
	fmt.Println("   ✓ File upload support")
	fmt.Println("   ✓ Discriminators for polymorphic types")
	fmt.Println("   ✓ Examples for requests and responses")
	fmt.Println("   ✓ Schema references and reusability")
	fmt.Println("   ✓ Callbacks/Webhooks")
	fmt.Println("   ✓ Validation middleware")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop...")
	fmt.Println("")

	// Run the application (blocks until SIGINT/SIGTERM)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// Handler implementations

func createUser(ctx forge.Context, req *CreateUserRequest) (*User, error) {
	// Simulate user creation
	user := &User{
		ID:        "123e4567-e89b-12d3-a456-426614174000",
		Name:      req.Name,
		Email:     req.Email,
		Age:       req.Age,
		Role:      req.Role,
		CreatedAt: time.Now(),
	}

	return user, nil
}

func listUsers(ctx forge.Context) error {
	// Simulate paginated response
	users := []User{
		{
			ID:        "123e4567-e89b-12d3-a456-426614174000",
			Name:      "John Doe",
			Email:     "john@example.com",
			Age:       30,
			Role:      "user",
			CreatedAt: time.Now().Add(-24 * time.Hour),
		},
		{
			ID:        "223e4567-e89b-12d3-a456-426614174001",
			Name:      "Jane Smith",
			Email:     "jane@example.com",
			Age:       28,
			Role:      "admin",
			CreatedAt: time.Now().Add(-48 * time.Hour),
		},
	}

	response := map[string]interface{}{
		"data":       users,
		"total":      100,
		"page":       1,
		"pageSize":   20,
		"totalPages": 5,
	}

	return ctx.JSON(200, response)
}

func getUser(ctx forge.Context) error {
	id := ctx.Param("id")

	user := User{
		ID:        id,
		Name:      "John Doe",
		Email:     "john@example.com",
		Age:       30,
		Role:      "user",
		CreatedAt: time.Now(),
	}

	return ctx.JSON(200, user)
}

func updateUser(ctx forge.Context) error {
	id := ctx.Param("id")

	var req UpdateUserRequest
	if err := ctx.Bind(&req); err != nil {
		return forge.BadRequest("Invalid request body")
	}

	user := User{
		ID:        id,
		Name:      "John Doe Updated",
		Email:     "john@example.com",
		Age:       31,
		Role:      "user",
		CreatedAt: time.Now().Add(-24 * time.Hour),
	}

	return ctx.JSON(200, user)
}

func deleteUser(ctx forge.Context) error {
	return ctx.NoContent(204)
}

func uploadFile(ctx forge.Context) error {
	response := map[string]interface{}{
		"fileId":      "file-123",
		"filename":    "document.pdf",
		"size":        1024000,
		"contentType": "application/pdf",
		"url":         "https://example.com/files/file-123",
	}

	return ctx.JSON(201, response)
}

func bulkImportUsers(ctx forge.Context) error {
	response := map[string]interface{}{
		"jobId":     "job-456",
		"status":    "pending",
		"statusUrl": "https://api.example.com/jobs/job-456",
	}

	return ctx.JSON(202, response)
}

func batchCreateUsers(ctx forge.Context) error {
	response := map[string]interface{}{
		"successful": []User{
			{
				ID:        "user-1",
				Name:      "User 1",
				Email:     "user1@example.com",
				CreatedAt: time.Now(),
			},
		},
		"failed": []map[string]interface{}{
			{
				"item":  map[string]string{"email": "invalid-email"},
				"error": "Invalid email format",
			},
		},
		"totalProcessed": 2,
		"successCount":   1,
		"failureCount":   1,
	}

	return ctx.JSON(200, response)
}
