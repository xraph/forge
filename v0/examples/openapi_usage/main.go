package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/router"
)

// =============================================================================
// CONSISTENT TYPE DEFINITIONS
// =============================================================================

// Use core types consistently throughout the application
type (
	// AppLogger Application logger - use the core alias
	AppLogger = common.Logger

	// AppMetrics Application metrics - use core interface
	AppMetrics = common.Metrics

	// AppConfig Application config - use core interface
	AppConfig = common.ConfigManager
)

// =============================================================================
// DOMAIN MODEL
// =============================================================================

type User struct {
	ID    string `json:"id" example:"user-123"`
	Name  string `json:"name" example:"John Doe" validate:"required"`
	Email string `json:"email" example:"john@example.com" validate:"required,email"`
	Age   int    `json:"age" example:"30" validate:"min=0,max=120"`
}

type CreateUserRequest struct {
	Name  string `json:"name" validate:"required" description:"User's full name"`
	Email string `json:"email" validate:"required,email" description:"User's email address"`
	Age   int    `json:"age" validate:"min=0,max=120" description:"User's age"`
}

type GetUserRequest struct {
	ID string `path:"id" validate:"required" description:"User ID to retrieve"`
}

type ListUsersRequest struct {
	Page     int    `query:"page" default:"1" validate:"min=1" description:"Page number"`
	PageSize int    `query:"page_size" default:"10" validate:"min=1,max=100" description:"Number of users per page"`
	Search   string `query:"search" description:"Search term for filtering users"`
}

type ListUsersResponse struct {
	Users      []User `json:"users" description:"List of users"`
	TotalCount int    `json:"total_count" description:"Total number of users"`
	Page       int    `json:"page" description:"Current page number"`
	PageSize   int    `json:"page_size" description:"Number of users per page"`
	TotalPages int    `json:"total_pages" description:"Total number of pages"`
}

// =============================================================================
// BUSINESS SERVICES WITH CONSISTENT TYPES
// =============================================================================

// UserService interface definition
type UserService interface {
	common.Service // Embed core service interface
	CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error)
	GetUser(ctx context.Context, id string) (*User, error)
	ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error)
}

// UserService implementation with consistent type usage
type userService struct {
	logger AppLogger        // Use consistent type
	users  map[string]*User // Simple in-memory storage
}

// NewUserService Constructor uses consistent types
func NewUserService(logger AppLogger) UserService {
	return &userService{
		logger: logger,
		users:  make(map[string]*User),
	}
}

// Name common.Service interface
func (s *userService) Name() string {
	return "user-service"
}

func (s *userService) Dependencies() []string {
	return []string{"logger"} // Declare dependency explicitly
}

func (s *userService) Start(ctx context.Context) error {
	s.logger.Info("user service starting")

	// Seed initial data
	users := []*User{
		{ID: "user-1", Name: "Alice Johnson", Email: "alice@example.com", Age: 28},
		{ID: "user-2", Name: "Bob Smith", Email: "bob@example.com", Age: 35},
		{ID: "user-3", Name: "Charlie Brown", Email: "charlie@example.com", Age: 42},
	}

	for _, user := range users {
		s.users[user.ID] = user
	}

	s.logger.Info("user service started", logger.Int("initial_users", len(users)))
	return nil
}

func (s *userService) Stop(ctx context.Context) error {
	s.logger.Info("user service stopping")
	return nil
}

func (s *userService) OnHealthCheck(ctx context.Context) error {
	if s.users == nil {
		return fmt.Errorf("user storage not initialized")
	}
	return nil
}

// CreateUser Business logic implementation
func (s *userService) CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error) {
	userID := fmt.Sprintf("user-%d", time.Now().UnixNano())
	user := &User{
		ID:    userID,
		Name:  req.Name,
		Email: req.Email,
		Age:   req.Age,
	}

	s.users[userID] = user
	s.logger.Info("user created", logger.String("user_id", userID), logger.String("name", req.Name))

	return user, nil
}

func (s *userService) GetUser(ctx context.Context, id string) (*User, error) {
	user, exists := s.users[id]
	if !exists {
		return nil, fmt.Errorf("user not found: %s", id)
	}
	return user, nil
}

func (s *userService) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	users := make([]User, 0)
	for _, user := range s.users {
		// Simple search filtering
		if req.Search == "" ||
			containsIgnoreCase(user.Name, req.Search) ||
			containsIgnoreCase(user.Email, req.Search) {
			users = append(users, *user)
		}
	}

	totalCount := len(users)
	totalPages := (totalCount + req.PageSize - 1) / req.PageSize

	// Simple pagination
	start := (req.Page - 1) * req.PageSize
	end := start + req.PageSize
	if start >= len(users) {
		users = []User{}
	} else {
		if end > len(users) {
			end = len(users)
		}
		users = users[start:end]
	}

	return &ListUsersResponse{
		Users:      users,
		TotalCount: totalCount,
		Page:       req.Page,
		PageSize:   req.PageSize,
		TotalPages: totalPages,
	}, nil
}

func containsIgnoreCase(str, substr string) bool {
	return len(str) >= len(substr) &&
		(str == substr ||
			(len(str) > len(substr) &&
				(str[:len(substr)] == substr ||
					str[len(str)-len(substr):] == substr ||
					contains(str, substr))))
}

func contains(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// OPINIONATED HANDLERS WITH AUTOMATIC DEPENDENCY INJECTION
// =============================================================================

// Service-aware handlers - framework automatically injects UserService
func getUserHandler(ctx common.Context, userService UserService, req GetUserRequest) (*User, error) {
	return userService.GetUser(ctx, req.ID)
}

func createUserHandler(ctx common.Context, userService UserService, req CreateUserRequest) (*User, error) {
	return userService.CreateUser(ctx, &req)
}

func listUsersHandler(ctx common.Context, userService UserService, req ListUsersRequest) (*ListUsersResponse, error) {
	return userService.ListUsers(ctx, &req)
}

// Pure handler (no service injection)
func healthCheckHandler(ctx common.Context, req struct{}) (*map[string]interface{}, error) {
	return &map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"service":   "forge-example",
		"version":   "1.0.0",
	}, nil
}

// Helper function to register business services
func registerBusinessServices(app forge.Forge) error {
	// Register user service with explicit dependency declaration
	if err := app.Container().Register(common.ServiceDefinition{
		Name:         "user-service",
		Type:         (*UserService)(nil),
		Constructor:  NewUserService, // Constructor will automatically get logger injected
		Singleton:    true,
		Dependencies: []string{forge.ContainerKeys.Logger}, // Explicit dependency
	}); err != nil {
		return fmt.Errorf("failed to register user service: %w", err)
	}

	return nil
}

// =============================================================================
// MAIN APPLICATION
// =============================================================================

func main() {
	// Create infrastructure components with consistent types
	appLogger := logger.NewDevelopmentLogger()

	// Create application
	app, err := forge.NewApplication("forge-example", "1.0.0",
		forge.WithDescription("Complete Forge Framework Example with Simplified API"),
		forge.WithLogger(appLogger),           // Colored development logging
		forge.WithStopTimeout(30*time.Second), // Graceful shutdown timeout
		forge.WithOpenAPI(
			forge.OpenAPIConfig{
				Title:       "Forge Example API",
				Description: "Complete example of Forge framework with simplified functional options API",
				EnableUI:    true,
			},
		),
	)
	if err != nil {
		log.Fatal("Failed to create application:", err)
	}

	// Set core components
	app.SetLogger(appLogger)

	// Register business services
	if err := registerBusinessServices(app); err != nil {
		log.Fatal("Failed to register business services:", err)
	}

	// // Validate all registrations before starting
	// validator := container.GetValidator()
	// if err := validator.ValidateAll(); err != nil {
	// 	log.Fatal("Service validation failed:", err)
	// }

	// 🛣️ REGISTER ROUTES - Clean and simple!
	// The router is automatically initialized with the container
	if err := app.RegisterHandler("GET", "/api/v1/users/:id", getUserHandler,
		router.WithOpenAPITags("users"),
		router.WithSummary("Get user by ID"),
		router.WithDescription("Retrieves a specific user by their unique identifier"),
	); err != nil {
		log.Fatal("Failed to register get user handler:", err)
	}

	if err := app.RegisterHandler("POST", "/api/v1/users", createUserHandler,
		router.WithOpenAPITags("users"),
		router.WithSummary("Create new user"),
		router.WithDescription("Creates a new user with the provided information"),
	); err != nil {
		log.Fatal("Failed to register create user handler:", err)
	}

	if err := app.RegisterHandler("GET", "/api/v1/users", listUsersHandler,
		router.WithOpenAPITags("users"),
		router.WithSummary("List users"),
		router.WithDescription("Retrieves a paginated list of users with optional search filtering"),
	); err != nil {
		log.Fatal("Failed to register list users handler:", err)
	}

	app.WebSocket("/chat", router.WithStreamingTags("chat"))

	if err = app.EnableMetricsEndpoints(); err != nil {
		fmt.Println("Failed to enable metrics endpoints:", err)
		return
	}

	if err = app.EnableHealthEndpoints(); err != nil {
		fmt.Println("Failed to enable health endpoints:", err)
		return
	}

	// if err := app.RegisterHandler("GET", "/health", healthCheckHandler,
	// 	router.WithOpenAPITags("health"),
	// 	router.WithSummary("Health check"),
	// 	router.WithDescription("Returns the health status of the application"),
	// ); err != nil {
	// 	log.Fatal("Failed to register health check handler:", err)
	// }

	// Start HTTP server
	go func() {
		if err := app.StartServer(":8080"); err != nil {
			log.Printf("Failed to start HTTP server: %v", err)
		}
	}()

	// Run the application (blocks until shutdown)
	if err := app.Run(); err != nil {
		log.Fatal("Application failed:", err)
	}
}
