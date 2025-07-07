package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/xraph/forge/database"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/router"
	"github.com/xraph/forge/v1"
)

// User represents a user in our system
type User struct {
	ID        int       `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Email     string    `json:"email" db:"email"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// UserService handles user operations
type UserService struct {
	db     database.SQLDatabase
	cache  database.Cache
	logger logger.Logger
}

// NewUserService creates a new user service
func NewUserService(db database.SQLDatabase, cache database.Cache, logger logger.Logger) *UserService {
	return &UserService{
		db:     db,
		cache:  cache,
		logger: logger.Named("user-service"),
	}
}

// CreateUser creates a new user
func (s *UserService) CreateUser(ctx context.Context, user *User) error {
	query := `
		INSERT INTO users (name, email, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`

	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	err := s.db.QueryRow(ctx, query, user.Name, user.Email, user.CreatedAt, user.UpdatedAt).Scan(&user.ID)
	if err != nil {
		s.logger.Error("Failed to create user", logger.Error(err))
		return fmt.Errorf("failed to create user: %w", err)
	}

	// Invalidate cache
	s.cache.Delete(ctx, "users:all")

	s.logger.Info("User created successfully",
		logger.Int("user_id", user.ID),
		logger.String("email", user.Email),
	)

	return nil
}

// GetUser retrieves a user by ID
func (s *UserService) GetUser(ctx context.Context, id int) (*User, error) {
	// Try cache first
	cacheKey := fmt.Sprintf("user:%d", id)
	if data, err := s.cache.Get(ctx, cacheKey); err == nil {
		var user User
		if err := json.Unmarshal(data, &user); err == nil {
			s.logger.Debug("User retrieved from cache", logger.Int("user_id", id))
			return &user, nil
		}
	}

	// Query database
	query := `SELECT id, name, email, created_at, updated_at FROM users WHERE id = $1`

	var user User
	err := s.db.QueryRow(ctx, query, id).Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		s.logger.Error("Failed to get user", logger.Error(err), logger.Int("user_id", id))
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Cache the result
	if data, err := json.Marshal(user); err == nil {
		s.cache.Set(ctx, cacheKey, data, 5*time.Minute)
	}

	s.logger.Debug("User retrieved from database", logger.Int("user_id", id))
	return &user, nil
}

// GetUsers retrieves all users
func (s *UserService) GetUsers(ctx context.Context) ([]*User, error) {
	// Try cache first
	cacheKey := "users:all"
	if data, err := s.cache.Get(ctx, cacheKey); err == nil {
		var users []*User
		if err := json.Unmarshal(data, &users); err == nil {
			s.logger.Debug("Users retrieved from cache")
			return users, nil
		}
	}

	// Query database
	query := `SELECT id, name, email, created_at, updated_at FROM users ORDER BY created_at DESC`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		s.logger.Error("Failed to get users", logger.Error(err))
		return nil, fmt.Errorf("failed to get users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var user User
		err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			s.logger.Error("Failed to scan user", logger.Error(err))
			continue
		}
		users = append(users, &user)
	}

	// Cache the result
	if data, err := json.Marshal(users); err == nil {
		s.cache.Set(ctx, cacheKey, data, 2*time.Minute)
	}

	s.logger.Debug("Users retrieved from database", logger.Int("count", len(users)))
	return users, nil
}

// UpdateUser updates an existing user
func (s *UserService) UpdateUser(ctx context.Context, id int, user *User) error {
	query := `
		UPDATE users 
		SET name = $1, email = $2, updated_at = $3
		WHERE id = $4
	`

	user.UpdatedAt = time.Now()

	result, err := s.db.Exec(ctx, query, user.Name, user.Email, user.UpdatedAt, id)
	if err != nil {
		s.logger.Error("Failed to update user", logger.Error(err), logger.Int("user_id", id))
		return fmt.Errorf("failed to update user: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	// Invalidate cache
	s.cache.Delete(ctx, fmt.Sprintf("user:%d", id))
	s.cache.Delete(ctx, "users:all")

	s.logger.Info("User updated successfully",
		logger.Int("user_id", id),
		logger.String("email", user.Email),
	)

	return nil
}

// DeleteUser deletes a user
func (s *UserService) DeleteUser(ctx context.Context, id int) error {
	query := `DELETE FROM users WHERE id = $1`

	result, err := s.db.Exec(ctx, query, id)
	if err != nil {
		s.logger.Error("Failed to delete user", logger.Error(err), logger.Int("user_id", id))
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	// Invalidate cache
	s.cache.Delete(ctx, fmt.Sprintf("user:%d", id))
	s.cache.Delete(ctx, "users:all")

	s.logger.Info("User deleted successfully", logger.Int("user_id", id))
	return nil
}

// UserController handles HTTP requests for users
type UserController struct {
	service *UserService
	logger  logger.Logger
}

// NewUserController creates a new user controller
func NewUserController(service *UserService, logger logger.Logger) *UserController {
	return &UserController{
		service: service,
		logger:  logger.Named("user-controller"),
	}
}

// CreateUser handles POST /users
func (c *UserController) CreateUser(w http.ResponseWriter, r *http.Request) {
	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		c.logger.Error("Invalid request body", logger.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Basic validation
	if user.Name == "" || user.Email == "" {
		http.Error(w, "Name and email are required", http.StatusBadRequest)
		return
	}

	if err := c.service.CreateUser(r.Context(), &user); err != nil {
		c.logger.Error("Failed to create user", logger.Error(err))
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

// GetUser handles GET /users/{id}
func (c *UserController) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	user, err := c.service.GetUser(r.Context(), id)
	if err != nil {
		c.logger.Error("Failed to get user", logger.Error(err), logger.Int("user_id", id))
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// GetUsers handles GET /users
func (c *UserController) GetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := c.service.GetUsers(r.Context())
	if err != nil {
		c.logger.Error("Failed to get users", logger.Error(err))
		http.Error(w, "Failed to get users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// UpdateUser handles PUT /users/{id}
func (c *UserController) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		c.logger.Error("Invalid request body", logger.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Basic validation
	if user.Name == "" || user.Email == "" {
		http.Error(w, "Name and email are required", http.StatusBadRequest)
		return
	}

	if err := c.service.UpdateUser(r.Context(), id, &user); err != nil {
		c.logger.Error("Failed to update user", logger.Error(err), logger.Int("user_id", id))
		if err.Error() == "user not found" {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to update user", http.StatusInternalServerError)
		}
		return
	}

	user.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// DeleteUser handles DELETE /users/{id}
func (c *UserController) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	if err := c.service.DeleteUser(r.Context(), id); err != nil {
		c.logger.Error("Failed to delete user", logger.Error(err), logger.Int("user_id", id))
		if err.Error() == "user not found" {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Email job handler for demonstrating job processing
func emailJobHandler(ctx context.Context, job jobs.Job) (interface{}, error) {
	// Extract email data from job payload
	to, ok := job.Payload["to"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'to' field in job payload")
	}

	subject, ok := job.Payload["subject"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'subject' field in job payload")
	}

	body, ok := job.Payload["body"].(string)
	if !ok {
		body = "Default email body"
	}

	fmt.Println(body)
	// Simulate sending email
	log.Printf("📧 Sending email to %s with subject: %s", to, subject)
	time.Sleep(100 * time.Millisecond) // Simulate email sending delay

	return map[string]interface{}{
		"status":     "sent",
		"to":         to,
		"subject":    subject,
		"sent_at":    time.Now().Format(time.RFC3339),
		"message_id": fmt.Sprintf("msg_%d", time.Now().Unix()),
	}, nil
}

// Custom middleware for API versioning
func apiVersionMiddleware(version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("API-Version", version)
			next.ServeHTTP(w, r)
		})
	}
}

// Custom middleware for request timing
func timingMiddleware(l logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			next.ServeHTTP(w, r)

			duration := time.Since(start)
			l.Info("Request processed",
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.Duration("duration", duration),
			)
		})
	}
}

// setupRoutes configures application routes
func setupRoutes(app forge.Application, controller *UserController) {
	// Get the router
	r := app.Router()

	// API routes with versioning
	apiV1 := r.Group("/api/v1")

	// User routes
	apiV1.Post("/users", controller.CreateUser)
	// apiV1.Get("/users", controller.GetUsers)
	// GET /api/v1/users - List users with pagination
	apiV1.DocGet("/users", controller.GetUsers,
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
						ID: 1,
						// Username:  "john_doe",
						Email: "john@example.com",
						// FullName:  "John Doe",
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
						// Active:    true,
					},
				},
				"page":        1,
				"page_size":   20,
				"total_items": 1,
				"total_pages": 1,
			}),
	)
	apiV1.Get("/users/{id}", controller.GetUser)
	apiV1.Put("/users/{id}", controller.UpdateUser)
	apiV1.Delete("/users/{id}", controller.DeleteUser)

	// Job endpoint for demonstration
	apiV1.Post("/jobs/email", func(w http.ResponseWriter, r *http.Request) {
		var emailData map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&emailData); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Create and enqueue email job
		job := jobs.Job{
			Type:    "send-email",
			Payload: emailData,
		}

		if err := app.Jobs().Enqueue(r.Context(), job); err != nil {
			http.Error(w, "Failed to enqueue job", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Email job enqueued successfully",
			"job_id":  job.ID,
		})
	})

	// Demo endpoints
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":     "Welcome to Forge Framework Demo",
			"version":     app.Version(),
			"environment": app.Environment(),
			"endpoints": map[string]string{
				"users":      "/api/v1/users",
				"health":     "/health",
				"metrics":    "/metrics",
				"ready":      "/ready",
				"email_jobs": "/api/v1/jobs/email",
			},
		})
	})

	// Custom health check handler
	r.Health("/health", func(ctx context.Context) error {
		// Check database connectivity
		if err := app.Database().Ping(ctx); err != nil {
			return fmt.Errorf("database not available: %w", err)
		}

		// Check cache connectivity
		if err := app.Cache().Ping(ctx); err != nil {
			return fmt.Errorf("cache not available: %w", err)
		}

		return nil
	})
}

// setupDatabase initializes database schema
func setupDatabase(app forge.Application) error {
	db := app.Database()

	// Create users table
	createTableQuery := `
		CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			email VARCHAR(255) UNIQUE NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`

	_, err := db.Exec(context.Background(), createTableQuery)
	if err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}

	// Create index on email
	createIndexQuery := `CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`
	_, err = db.Exec(context.Background(), createIndexQuery)
	if err != nil {
		return fmt.Errorf("failed to create email index: %w", err)
	}

	return nil
}

func main() {
	// Create application with comprehensive configuration
	app, err := forge.New("forge-demo").
		WithOpenAPI("My API", "1.0.0", "A sample API").
		EnableDocumentation().

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

		// Add custom middleware
		WithMiddleware(
			apiVersionMiddleware("v1.0.0"),
			timingMiddleware(logger.GetGlobalLogger()),
		).

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
		log.Fatal("Failed to build application:", err)
	}

	// Setup database schema
	if err := setupDatabase(app); err != nil {
		log.Fatal("Failed to setup database:", err)
	}

	// Create services
	userService := NewUserService(
		app.Database(),
		app.Cache(),
		app.Logger(),
	)

	// Create controllers
	userController := NewUserController(userService, app.Logger())

	// Setup routes
	setupRoutes(app, userController)

	// Register job handlers BEFORE startup callbacks
	app.Jobs().RegisterHandler("send-email", jobs.NewEmailJobHandler(app.Logger()))

	// FIXED: Non-blocking startup callback that schedules jobs after startup
	app.OnStartup(func(ctx context.Context) error {
		app.Logger().Info("🚀 Forge Demo Application Starting Up!")

		// Schedule welcome job AFTER a delay to ensure everything is ready
		go func() {
			time.Sleep(2 * time.Second) // Wait for full startup

			welcomeJob := jobs.Job{
				Type: "send-email",
				Payload: map[string]interface{}{
					"to":      "admin@example.com",
					"subject": "Forge Demo Started",
					"body":    "Your Forge demo application has started successfully!",
				},
			}

			if err := app.Jobs().Enqueue(context.Background(), welcomeJob); err != nil {
				app.Logger().Error("Failed to enqueue welcome job", logger.Error(err))
			} else {
				app.Logger().Info("Welcome email job enqueued successfully")
			}
		}()

		// Return immediately - don't block startup
		return nil
	})

	// Add shutdown callback
	app.OnShutdown(func(ctx context.Context) error {
		app.Logger().Info("👋 Forge Demo Application Shutting Down!")
		return nil
	})

	// // Print startup information
	// fmt.Println("🔥 Forge Framework Demo")
	// fmt.Println("========================")
	// fmt.Printf("Server: http://localhost:8080\n")
	// fmt.Printf("Health: http://localhost:8080/health\n")
	// fmt.Printf("Metrics: http://localhost:8080/metrics\n")
	// fmt.Printf("Ready: http://localhost:8080/ready\n")
	// fmt.Println("========================")
	// fmt.Println("API Endpoints:")
	// fmt.Printf("  GET    /api/v1/users\n")
	// fmt.Printf("  POST   /api/v1/users\n")
	// fmt.Printf("  GET    /api/v1/users/{id}\n")
	// fmt.Printf("  PUT    /api/v1/users/{id}\n")
	// fmt.Printf("  DELETE /api/v1/users/{id}\n")
	// fmt.Printf("  POST   /api/v1/jobs/email\n")
	// fmt.Println("========================")
	// fmt.Println("Try these commands:")
	// fmt.Println("  curl http://localhost:8080/")
	// fmt.Println("  curl http://localhost:8080/health")
	// fmt.Println("  curl -X POST http://localhost:8080/api/v1/users -H 'Content-Type: application/json' -d '{\"name\":\"John Doe\",\"email\":\"john@example.com\"}'")
	// fmt.Println("  curl http://localhost:8080/api/v1/users")
	// fmt.Println("========================")

	// Start and run the application
	if err := app.Run(); err != nil {
		log.Fatal("Failed to run application:", err)
	}
}
