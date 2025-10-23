package main

import (
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
)

// User represents a user in the system
type User struct {
	ID        string    `json:"id" description:"Unique user identifier" format:"uuid" example:"123e4567-e89b-12d3-a456-426614174000"`
	Name      string    `json:"name" description:"User's full name" minLength:"1" maxLength:"100" example:"John Doe"`
	Email     string    `json:"email" description:"User's email address" format:"email" example:"john@example.com"`
	Age       int       `json:"age,omitempty" description:"User's age" minimum:"0" maximum:"150" example:"30"`
	Role      string    `json:"role" description:"User role" enum:"user,admin,moderator" example:"user"`
	CreatedAt time.Time `json:"createdAt" description:"Account creation timestamp" format:"date-time"`
	Tags      []string  `json:"tags,omitempty" description:"User tags" maxItems:"10" example:"premium,verified"`
}

// ========================================
// UNIFIED REQUEST SCHEMA EXAMPLES
// ========================================

// GetUserRequest demonstrates a unified request with path, query, and header parameters
type GetUserRequest struct {
	// Path parameters
	UserID string `path:"userId" description:"User ID to retrieve" format:"uuid" required:"true"`

	// Query parameters
	Include string `query:"include,omitempty" description:"Related resources to include" enum:"profile,settings,both" example:"profile"`
	Format  string `query:"format,omitempty" description:"Response format" enum:"json,xml" default:"json"`

	// Header parameters
	RequestID   string `header:"X-Request-ID,omitempty" description:"Request correlation ID" format:"uuid"`
	AcceptLang  string `header:"Accept-Language,omitempty" description:"Preferred language" example:"en-US"`
	IfNoneMatch string `header:"If-None-Match,omitempty" description:"ETag for conditional requests"`
}

// CreateUserRequest demonstrates a unified request with query, header, and body parameters
type CreateUserRequest struct {
	// Query parameters
	DryRun   bool `query:"dryRun,omitempty" description:"Preview changes without persisting" default:"false"`
	SendMail bool `query:"sendMail,omitempty" description:"Send welcome email" default:"true"`

	// Header parameters
	RequestID string `header:"X-Request-ID,omitempty" description:"Request correlation ID" format:"uuid"`
	APIKey    string `header:"X-API-Key" description:"API authentication key" required:"true"`

	// Body fields
	Name  string   `json:"name" body:"" description:"User's full name" minLength:"1" maxLength:"100" required:"true" example:"John Doe"`
	Email string   `json:"email" body:"" description:"User's email" format:"email" required:"true" example:"john@example.com"`
	Age   int      `json:"age,omitempty" body:"" description:"User's age" minimum:"0" maximum:"150" example:"30"`
	Role  string   `json:"role,omitempty" body:"" description:"User role" enum:"user,admin,moderator" default:"user"`
	Tags  []string `json:"tags,omitempty" body:"" description:"User tags" maxItems:"10"`
}

// UpdateUserRequest demonstrates a unified request with path, query, header, and body parameters
type UpdateUserRequest struct {
	// Path parameters
	UserID string `path:"userId" description:"User ID to update" format:"uuid" required:"true"`

	// Query parameters
	Validate bool `query:"validate,omitempty" description:"Validate changes before applying" default:"true"`

	// Header parameters
	RequestID string `header:"X-Request-ID,omitempty" description:"Request correlation ID" format:"uuid"`
	IfMatch   string `header:"If-Match,omitempty" description:"ETag for optimistic locking"`

	// Body fields (all optional for PATCH)
	Name  *string   `json:"name,omitempty" body:"" description:"User's full name" minLength:"1" maxLength:"100"`
	Email *string   `json:"email,omitempty" body:"" description:"User's email" format:"email"`
	Age   *int      `json:"age,omitempty" body:"" description:"User's age" minimum:"0" maximum:"150"`
	Role  *string   `json:"role,omitempty" body:"" description:"User role" enum:"user,admin,moderator"`
	Tags  *[]string `json:"tags,omitempty" body:"" description:"User tags" maxItems:"10"`
}

// DeleteUserRequest demonstrates a unified request with only path and query parameters (no body)
type DeleteUserRequest struct {
	// Path parameters
	UserID string `path:"userId" description:"User ID to delete" format:"uuid" required:"true"`

	// Query parameters
	Force  bool   `query:"force,omitempty" description:"Force deletion even if user has dependencies" default:"false"`
	Reason string `query:"reason,omitempty" description:"Reason for deletion" maxLength:"500"`

	// Header parameters
	RequestID string `header:"X-Request-ID,omitempty" description:"Request correlation ID" format:"uuid"`
}

// SearchUsersRequest demonstrates complex query parameters
type SearchUsersRequest struct {
	// Query parameters only (no path or body for GET /users)
	Query    string `query:"q,omitempty" description:"Search query" maxLength:"200" example:"john"`
	Role     string `query:"role,omitempty" description:"Filter by role" enum:"user,admin,moderator"`
	MinAge   int    `query:"minAge,omitempty" description:"Minimum age" minimum:"0" maximum:"150"`
	MaxAge   int    `query:"maxAge,omitempty" description:"Maximum age" minimum:"0" maximum:"150"`
	Tags     string `query:"tags,omitempty" description:"Comma-separated tags" example:"premium,verified"`
	Page     int    `query:"page,omitempty" description:"Page number" minimum:"1" default:"1"`
	PageSize int    `query:"pageSize,omitempty" description:"Items per page" minimum:"1" maximum:"100" default:"20"`
	SortBy   string `query:"sortBy,omitempty" description:"Sort field" enum:"name,email,createdAt" default:"createdAt"`
	SortDir  string `query:"sortDir,omitempty" description:"Sort direction" enum:"asc,desc" default:"desc"`

	// Header parameters
	RequestID string `header:"X-Request-ID,omitempty" description:"Request correlation ID" format:"uuid"`
}

// PaginatedUsers represents a paginated list of users
type PaginatedUsers struct {
	Data       []User `json:"data" description:"Array of users"`
	TotalCount int    `json:"totalCount" description:"Total number of users available"`
	Page       int    `json:"page" description:"Current page number"`
	PageSize   int    `json:"pageSize" description:"Number of items per page"`
	TotalPages int    `json:"totalPages" description:"Total number of pages"`
}

// ErrorResponse represents an API error
type ErrorResponse struct {
	Error   string `json:"error" description:"Error message"`
	Code    string `json:"code,omitempty" description:"Error code"`
	Details any    `json:"details,omitempty" description:"Additional error details"`
}

func main() {
	// Create Forge app with OpenAPI configuration
	app := forge.NewApp(forge.AppConfig{
		Name:        "openapi-unified",
		Version:     "2.0.0",
		Description: "Unified Request Schema API - demonstrates path, query, header, and body parameters in a single struct",
		Environment: "development",
		HTTPAddress: ":8085",
		RouterOptions: []forge.RouterOption{
			forge.WithOpenAPI(forge.OpenAPIConfig{
				Title:       "Unified Request Schema API",
				Description: "Demonstrates unified request schema with path, query, header, and body parameters in a single struct",
				Version:     "2.0.0",
				Servers: []forge.OpenAPIServer{
					{URL: "http://localhost:8085", Description: "Development server"},
					{URL: "https://api.example.com", Description: "Production server"},
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

	// ========================================
	// UNIFIED SCHEMA ROUTES
	// ========================================

	// GET /users/:userId - Unified request with path, query, and headers
	router.GET("/users/:userId", handleGetUser,
		forge.WithSummary("Get user by ID"),
		forge.WithDescription("Retrieves a single user by their unique identifier with optional related resources"),
		forge.WithTags("Users"),
		forge.WithRequestSchema(&GetUserRequest{}), // 🎯 Single unified schema!
		forge.WithResponseSchema(200, "User found", &User{}),
		forge.WithResponseSchema(304, "Not modified (ETag match)", nil),
		forge.WithResponseSchema(404, "User not found", &ErrorResponse{}),
		forge.WithValidation(true),
	)

	// POST /users - Unified request with query, headers, and body
	router.POST("/users", handleCreateUser,
		forge.WithSummary("Create new user"),
		forge.WithDescription("Creates a new user account with optional email notification"),
		forge.WithTags("Users"),
		forge.WithRequestSchema(&CreateUserRequest{}), // 🎯 Includes query, headers, and body!
		forge.WithResponseSchema(201, "User created", &User{}),
		forge.WithResponseSchema(400, "Invalid request", &ErrorResponse{}),
		forge.WithResponseSchema(409, "User already exists", &ErrorResponse{}),
		forge.WithStrictValidation(), // Validates both request and response
	)

	// PATCH /users/:userId - Unified request with all parameter types
	router.PATCH("/users/:userId", handleUpdateUser,
		forge.WithSummary("Update user"),
		forge.WithDescription("Partially updates a user with optimistic locking support"),
		forge.WithTags("Users"),
		forge.WithRequestSchema(&UpdateUserRequest{}), // 🎯 Path + query + headers + body!
		forge.WithResponseSchema(200, "User updated", &User{}),
		forge.WithResponseSchema(304, "Not modified", nil),
		forge.WithResponseSchema(404, "User not found", &ErrorResponse{}),
		forge.WithResponseSchema(412, "Precondition failed (ETag mismatch)", &ErrorResponse{}),
		forge.WithValidation(true),
	)

	// DELETE /users/:userId - Unified request with path, query, and headers (no body)
	router.DELETE("/users/:userId", handleDeleteUser,
		forge.WithSummary("Delete user"),
		forge.WithDescription("Deletes a user account with optional force flag"),
		forge.WithTags("Users"),
		forge.WithRequestSchema(&DeleteUserRequest{}), // 🎯 No body fields, only params!
		forge.WithNoContentResponse(),                 // 204 No Content
		forge.WithResponseSchema(404, "User not found", &ErrorResponse{}),
		forge.WithResponseSchema(409, "Cannot delete user with dependencies", &ErrorResponse{}),
	)

	// GET /users - Complex query parameters for search
	router.GET("/users", handleSearchUsers,
		forge.WithSummary("Search users"),
		forge.WithDescription("Search and filter users with pagination and sorting"),
		forge.WithTags("Users"),
		forge.WithRequestSchema(&SearchUsersRequest{}), // 🎯 Complex query params!
		forge.WithResponseSchema(200, "Paginated list of users", &PaginatedUsers{}),
		forge.WithResponseSchema(400, "Invalid query parameters", &ErrorResponse{}),
		forge.WithValidation(true),
	)

	// ========================================
	// COMPARISON: Old vs New Approach
	// ========================================

	// OLD WAY (verbose, disconnected)
	// router.POST("/users/:userId/profile", handler,
	//     // Path params auto-extracted but not validated
	//     forge.WithQuerySchema(&QueryParams{}),
	//     forge.WithHeaderSchema(&HeaderParams{}),
	//     forge.WithRequestBodySchema(&BodyParams{}),
	// )

	// NEW WAY (clean, unified, type-safe)
	// router.POST("/users/:userId/profile", handler,
	//     forge.WithRequestSchema(&UnifiedRequest{}), // All in one!
	// )

	// Print startup information
	fmt.Println("")
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║         Unified Request Schema - OpenAPI 3.1.0 Demo             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Println("")
	fmt.Println("🚀 Server starting on http://localhost:8085")
	fmt.Println("")
	fmt.Println("📚 OpenAPI Documentation:")
	fmt.Println("   • Swagger UI:    http://localhost:8085/swagger")
	fmt.Println("   • OpenAPI Spec:  http://localhost:8085/openapi.json")
	fmt.Println("")
	fmt.Println("💡 Features Demonstrated:")
	fmt.Println("   ✓ Unified request schemas (path + query + headers + body)")
	fmt.Println("   ✓ Automatic schema generation from struct tags")
	fmt.Println("   ✓ Type-safe path parameters")
	fmt.Println("   ✓ Comprehensive validation rules")
	fmt.Println("   ✓ Multiple response status codes")
	fmt.Println("")
	fmt.Println("📌 Example API Calls:")
	fmt.Println("  GET    http://localhost:8085/users/123e4567-e89b-12d3-a456-426614174000?include=profile")
	fmt.Println("  POST   http://localhost:8085/users?dryRun=false&sendMail=true")
	fmt.Println("  PATCH  http://localhost:8085/users/123e4567-e89b-12d3-a456-426614174000?validate=true")
	fmt.Println("  DELETE http://localhost:8085/users/123e4567-e89b-12d3-a456-426614174000?force=false")
	fmt.Println("  GET    http://localhost:8085/users?q=john&role=user&page=1&pageSize=20")
	fmt.Println("")
	fmt.Println("Press Ctrl+C to stop...")
	fmt.Println("")

	// Run the application (blocks until SIGINT/SIGTERM)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// ========================================
// HANDLER IMPLEMENTATIONS
// ========================================

func handleGetUser(ctx forge.Context, req *GetUserRequest) (*User, error) {
	// Handler receives fully typed and validated request!
	fmt.Printf("GetUser - UserID: %s, Include: %s, Format: %s\n",
		req.UserID, req.Include, req.Format)
	fmt.Printf("  Headers - RequestID: %s, AcceptLang: %s\n",
		req.RequestID, req.AcceptLang)

	// Mock response
	user := &User{
		ID:        req.UserID,
		Name:      "John Doe",
		Email:     "john@example.com",
		Age:       30,
		Role:      "user",
		CreatedAt: time.Now(),
		Tags:      []string{"premium", "verified"},
	}

	return user, nil
}

func handleCreateUser(ctx forge.Context, req *CreateUserRequest) (*User, error) {
	// All parameters are validated and typed!
	fmt.Printf("CreateUser - DryRun: %v, SendMail: %v\n", req.DryRun, req.SendMail)
	fmt.Printf("  Headers - RequestID: %s, APIKey: %s\n", req.RequestID, req.APIKey)
	fmt.Printf("  Body - Name: %s, Email: %s, Age: %d, Role: %s\n",
		req.Name, req.Email, req.Age, req.Role)

	// Mock response
	user := &User{
		ID:        "123e4567-e89b-12d3-a456-426614174000",
		Name:      req.Name,
		Email:     req.Email,
		Age:       req.Age,
		Role:      req.Role,
		CreatedAt: time.Now(),
		Tags:      req.Tags,
	}

	return user, nil
}

func handleUpdateUser(ctx forge.Context, req *UpdateUserRequest) (*User, error) {
	fmt.Printf("UpdateUser - UserID: %s, Validate: %v\n", req.UserID, req.Validate)
	fmt.Printf("  Headers - RequestID: %s, IfMatch: %s\n", req.RequestID, req.IfMatch)
	if req.Name != nil {
		fmt.Printf("  Updating name to: %s\n", *req.Name)
	}
	if req.Email != nil {
		fmt.Printf("  Updating email to: %s\n", *req.Email)
	}

	// Mock response
	user := &User{
		ID:        req.UserID,
		Name:      "John Doe Updated",
		Email:     "john.updated@example.com",
		Age:       31,
		Role:      "user",
		CreatedAt: time.Now().Add(-24 * time.Hour),
		Tags:      []string{"premium", "verified", "updated"},
	}

	return user, nil
}

func handleDeleteUser(ctx forge.Context, req *DeleteUserRequest) error {
	fmt.Printf("DeleteUser - UserID: %s, Force: %v, Reason: %s\n",
		req.UserID, req.Force, req.Reason)
	fmt.Printf("  Headers - RequestID: %s\n", req.RequestID)

	// Return no content (204)
	return nil
}

func handleSearchUsers(ctx forge.Context, req *SearchUsersRequest) (*PaginatedUsers, error) {
	fmt.Printf("SearchUsers - Query: %s, Role: %s, MinAge: %d, MaxAge: %d\n",
		req.Query, req.Role, req.MinAge, req.MaxAge)
	fmt.Printf("  Pagination - Page: %d, PageSize: %d, SortBy: %s, SortDir: %s\n",
		req.Page, req.PageSize, req.SortBy, req.SortDir)
	fmt.Printf("  Headers - RequestID: %s\n", req.RequestID)

	// Mock paginated response
	users := []User{
		{
			ID:        "123e4567-e89b-12d3-a456-426614174001",
			Name:      "John Doe",
			Email:     "john@example.com",
			Age:       30,
			Role:      "user",
			CreatedAt: time.Now(),
			Tags:      []string{"premium"},
		},
		{
			ID:        "123e4567-e89b-12d3-a456-426614174002",
			Name:      "Jane Smith",
			Email:     "jane@example.com",
			Age:       28,
			Role:      "admin",
			CreatedAt: time.Now(),
			Tags:      []string{"verified"},
		},
	}

	result := &PaginatedUsers{
		Data:       users,
		TotalCount: 42,
		Page:       req.Page,
		PageSize:   req.PageSize,
		TotalPages: 3,
	}

	return result, nil
}
