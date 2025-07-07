package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/plugins"
	"github.com/xraph/forge/router"
	"github.com/xraph/forge/v1"
)

// Example 1: Simple Web Application
func ExampleSimpleWebApp() {
	application := forge.New("my-web-app").
		WithPort(8080).
		WithDevelopmentMode().
		MustBuild()

	// Add some routes
	application.Router().Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Hello, World!",
			"app":     application.Name(),
			"version": application.Version(),
		})
	})

	// Add health check
	application.Router().Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := application.Healthy(r.Context()); err != nil {
			http.Error(w, "Unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Run the application
	if err := application.Run(); err != nil {
		panic(err)
	}
}

// Example 2: API Application with Database
func ExampleAPIWithDatabase() {
	application := forge.NewAPIApplication("user-api").
		WithPostgreSQL("primary", "postgres://user:pass@localhost/mydb?sslmode=disable").
		WithRedis("cache", "localhost:6379").
		WithAutoMigration().
		MustBuild()

	// Add API routes
	api := application.Router().Route("/api/v1", func(r router.Router) {
		r.Get("/users", getUsersHandler(application))
		r.Post("/users", createUserHandler(application))
		r.Get("/users/{id}", getUserHandler(application))
		r.Put("/users/{id}", updateUserHandler(application))
		r.Delete("/users/{id}", deleteUserHandler(application))
	})

	// Add middleware
	api.Use(
		AuthMiddleware(),
		ValidationMiddleware(),
		RateLimitMiddleware(),
	)

	// Run the application
	if err := application.Run(); err != nil {
		panic(err)
	}
}

// Example 3: Microservice with Jobs and Tracing
func ExampleMicroservice() {
	application := forge.NewMicroservice("notification-service").
		WithConfigFile("config.yaml").
		WithTracing(observability.TracingConfig{
			Enabled:     true,
			ServiceName: "notification-service",
			Endpoint:    "http://jaeger:14268/api/traces",
			SampleRate:  0.1,
		}).
		WithJobs(jobs.Config{
			Enabled:     true,
			Backend:     "redis",
			Concurrency: 10,
			Queues: map[string]jobs.QueueConfig{
				"emails":        {Workers: 5, Priority: 1},
				"notifications": {Workers: 3, Priority: 2},
				"reports":       {Workers: 2, Priority: 3},
			},
		}).
		MustBuild()

	// Register job handlers
	emailHandler := &EmailJobHandler{
		emailService: application.Container().MustResolve("emailService"),
		logger:       application.Logger().Named("email-handler"),
	}

	application.Jobs().RegisterHandler("send_email", emailHandler)
	application.Jobs().RegisterHandler("send_sms", &SMSJobHandler{})

	// Add API routes
	application.Router().Route("/api", func(r router.Router) {
		r.Post("/send-email", sendEmailHandler(application))
		r.Post("/send-sms", sendSMSHandler(application))
		r.Get("/jobs/stats", jobStatsHandler(application))
	})

	// Run the application
	if err := application.Run(); err != nil {
		panic(err)
	}
}

// Example 4: Application with Custom Plugin
func ExampleWithPlugin() {
	// Create custom plugin
	authPlugin := &AuthenticationPlugin{
		config: AuthConfig{
			JWTSecret:     "my-secret",
			TokenDuration: 24 * time.Hour,
		},
	}

	application := forge.New("auth-app").
		WithPlugin(authPlugin).
		WithPlugins(
			&LoggingPlugin{},
			&MetricsPlugin{},
		).
		MustBuild()

	// The plugin automatically adds auth routes and middleware
	application.Router().Group(func(r router.Router) {
		r.Use(authPlugin.AuthMiddleware())
		r.Get("/protected", protectedHandler)
		r.Get("/admin", adminOnlyHandler)
	})

	if err := application.Run(); err != nil {
		panic(err)
	}
}

// Example 5: Complex Enterprise Application
func ExampleEnterpriseApp() {
	application := forge.New("enterprise-app").
		WithConfigFromEnv().
		WithProductionMode().
		WithPostgreSQL("primary", "postgres://user:pass@primary-db:5432/app").
		WithPostgreSQL("analytics", "postgres://user:pass@analytics-db:5432/analytics").
		WithRedis("cache", "redis-cluster:6379").
		WithRedis("sessions", "redis-sessions:6379").
		WithTracing(observability.TracingConfig{
			Enabled:     true,
			ServiceName: "enterprise-app",
			Endpoint:    "http://jaeger:14268/api/traces",
		}).
		WithMetrics(observability.MetricsConfig{
			Enabled:     true,
			ServiceName: "enterprise-app",
			Port:        9090,
		}).
		WithJobs(jobs.Config{
			Enabled:     true,
			Backend:     "redis",
			Concurrency: 20,
		}).
		WithCORS(router.CORSConfig{
			AllowedOrigins:   []string{"https://forge.example.com", "https://admin.example.com"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
			AllowedHeaders:   []string{"Authorization", "Content-Type"},
			AllowCredentials: true,
		}).
		WithTLS("cert.pem", "key.pem").
		MustBuild()

	// Add complex middleware stack
	application.Router().Use(
		SecurityHeadersMiddleware(),
		AuthenticationMiddleware(),
		AuthorizationMiddleware(),
		AuditLoggingMiddleware(),
		RequestValidationMiddleware(),
	)

	// Add multiple route groups
	application.Router().Route("/api/v1", func(r router.Router) {
		r.Route("/users", userRoutes)
		r.Route("/organizations", organizationRoutes)
		r.Route("/billing", billingRoutes)
		r.Route("/analytics", analyticsRoutes)
	})

	application.Router().Route("/admin", func(r router.Router) {
		r.Use(AdminRequiredMiddleware())
		r.Route("/users", adminUserRoutes)
		r.Route("/system", systemRoutes)
		r.Route("/monitoring", monitoringRoutes)
	})

	// Add lifecycle callbacks
	application.OnStartup(func(ctx context.Context) error {
		application.Logger().Info("Running startup tasks...")
		// Initialize external services, warm caches, etc.
		return nil
	})

	application.OnShutdown(func(ctx context.Context) error {
		application.Logger().Info("Running cleanup tasks...")
		// Cleanup resources, save state, etc.
		return nil
	})

	// Run the application
	if err := application.Run(); err != nil {
		panic(err)
	}
}

// Example 6: Testing Application
func ExampleTestingApp() {
	testApp := forge.NewTestApplication("test-app").
		WithMemoryCache("default").
		WithSQLite("test", ":memory:").
		MustBuild()

	// Mock external services
	mockEmailService := &MockEmailService{}
	testApp.Container().Register("emailService", mockEmailService)

	// Add test routes
	testApp.Router().Post("/test/email", func(w http.ResponseWriter, r *http.Request) {
		// Test email functionality
	})

	// Use in tests
	testClient := &http.Client{}
	resp, err := testClient.Post("http://localhost:8080/test/email", "application/json", nil)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
}

// Example Job Handlers

type EmailJobHandler struct {
	emailService interface{}
	logger       logger.Logger
}

func (h *EmailJobHandler) Handle(ctx context.Context, job jobs.Job) (interface{}, error) {
	to := job.Payload["to"].(string)
	subject := job.Payload["subject"].(string)
	body := job.Payload["body"].(string)

	h.logger.Info("Sending email",
		logger.String("to", to),
		logger.String("subject", subject),
		logger.String("body", body),
	)

	// Simulate email sending
	time.Sleep(100 * time.Millisecond)

	return map[string]interface{}{
		"sent_to": to,
		"sent_at": time.Now(),
	}, nil
}

func (h *EmailJobHandler) CanHandle(jobType string) bool {
	return jobType == "send_email"
}

func (h *EmailJobHandler) GetTimeout() time.Duration {
	return 30 * time.Second
}

func (h *EmailJobHandler) GetRetryPolicy() jobs.RetryPolicy {
	return jobs.RetryPolicy{
		MaxRetries:      3,
		InitialInterval: 5 * time.Second,
		MaxInterval:     60 * time.Second,
		Multiplier:      2.0,
	}
}

type SMSJobHandler struct{}

func (h *SMSJobHandler) Handle(ctx context.Context, job jobs.Job) (interface{}, error) {
	// SMS handling logic
	return nil, nil
}

func (h *SMSJobHandler) CanHandle(jobType string) bool {
	return jobType == "send_sms"
}

func (h *SMSJobHandler) GetTimeout() time.Duration {
	return 15 * time.Second
}

func (h *SMSJobHandler) GetRetryPolicy() jobs.RetryPolicy {
	return jobs.DefaultRetryPolicy()
}

// Example Route Handlers

func getUsersHandler(app forge.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := app.Database("primary")
		cache := app.Cache("cache")

		// Try cache first
		cacheKey := "users:all"
		var cached []User
		if err := cache.GetJSON(r.Context(), cacheKey, &cached); err == nil {
			json.NewEncoder(w).Encode(cached)
			return
		}

		// Query database
		var users []User
		if err := db.Select(r.Context(), &users, "SELECT * FROM users ORDER BY created_at DESC LIMIT 100"); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Cache result
		cache.SetJSON(r.Context(), cacheKey, users, 5*time.Minute)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
	}
}

func createUserHandler(app forge.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var user User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		db := app.Database("primary")

		// Insert user
		result, err := db.Exec(r.Context(),
			"INSERT INTO users (name, email, created_at) VALUES ($1, $2, NOW()) RETURNING id",
			user.Name, user.Email)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Enqueue welcome email job
		emailJob := jobs.NewJob("send_email", map[string]interface{}{
			"to":      user.Email,
			"subject": "Welcome!",
			"body":    fmt.Sprintf("Welcome %s!", user.Name),
		})

		if err := app.Jobs().Enqueue(r.Context(), emailJob); err != nil {
			app.Logger().Error("Failed to enqueue welcome email", logger.Error(err))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      result,
			"message": "User created successfully",
		})
	}
}

func getUserHandler(app forge.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Implementation for getting single user
	}
}

func updateUserHandler(app forge.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Implementation for updating user
	}
}

func deleteUserHandler(app forge.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Implementation for deleting user
	}
}

func sendEmailHandler(app forge.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EmailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Enqueue email job
		emailJob := jobs.NewJob("send_email", map[string]interface{}{
			"to":      req.To,
			"subject": req.Subject,
			"body":    req.Body,
		})

		if err := app.Jobs().Enqueue(r.Context(), emailJob); err != nil {
			http.Error(w, "Failed to enqueue email", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Email queued for sending",
			"job_id":  emailJob.ID,
		})
	}
}

func sendSMSHandler(app forge.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// SMS sending implementation
	}
}

func jobStatsHandler(app forge.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := app.Jobs().GetStats(r.Context())
		if err != nil {
			http.Error(w, "Failed to get job stats", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

// Example Plugin Implementation

type AuthenticationPlugin struct {
	config AuthConfig
}

type AuthConfig struct {
	JWTSecret     string
	TokenDuration time.Duration
}

func (p *AuthenticationPlugin) Name() string        { return "authentication" }
func (p *AuthenticationPlugin) Version() string     { return "1.0.0" }
func (p *AuthenticationPlugin) Description() string { return "JWT authentication plugin" }
func (p *AuthenticationPlugin) Author() string      { return "Forge Team" }

func (p *AuthenticationPlugin) Initialize(container forge.Container) error {
	// Initialize JWT service
	jwtService := NewJWTService(p.config.JWTSecret, p.config.TokenDuration)
	container.Register("jwtService", jwtService)
	return nil
}

func (p *AuthenticationPlugin) Start(ctx context.Context) error {
	return nil
}

func (p *AuthenticationPlugin) Stop(ctx context.Context) error {
	return nil
}

func (p *AuthenticationPlugin) Routes() []plugins.RouteDefinition {
	return []plugins.RouteDefinition{
		{
			Method:  "POST",
			Pattern: "/auth/login",
			Handler: p.loginHandler,
		},
		{
			Method:  "POST",
			Pattern: "/auth/refresh",
			Handler: p.refreshHandler,
		},
		{
			Method:  "POST",
			Pattern: "/auth/logout",
			Handler: p.logoutHandler,
		},
	}
}

func (p *AuthenticationPlugin) Jobs() []plugins.JobDefinition {
	return nil
}

func (p *AuthenticationPlugin) Middleware() []plugins.MiddlewareDefinition {
	return []plugins.MiddlewareDefinition{
		{
			Name:     "jwt-auth",
			Handler:  p.AuthMiddleware(),
			Priority: 100,
		},
	}
}

func (p *AuthenticationPlugin) Commands() []plugins.CommandDefinition {
	return nil
}

func (p *AuthenticationPlugin) HealthChecks() []plugins.HealthCheckDefinition {
	return nil
}

func (p *AuthenticationPlugin) ConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"jwt_secret": map[string]interface{}{
			"type":        "string",
			"required":    true,
			"description": "JWT signing secret",
		},
		"token_duration": map[string]interface{}{
			"type":        "duration",
			"default":     "24h",
			"description": "Token expiration duration",
		},
	}
}

func (p *AuthenticationPlugin) DefaultConfig() map[string]interface{} {
	return map[string]interface{}{
		"token_duration": "24h",
	}
}

func (p *AuthenticationPlugin) ValidateConfig(config map[string]interface{}) error {
	if _, ok := config["jwt_secret"]; !ok {
		return fmt.Errorf("jwt_secret is required")
	}
	return nil
}

func (p *AuthenticationPlugin) Dependencies() []plugins.Dependency {
	return nil
}

func (p *AuthenticationPlugin) IsEnabled() bool {
	return true
}

func (p *AuthenticationPlugin) IsInitialized() bool {
	return true
}

func (p *AuthenticationPlugin) Status() plugins.PluginStatus {
	return plugins.PluginStatus{
		Enabled:     true,
		Initialized: true,
		Started:     true,
		Health:      "healthy",
	}
}

func (p *AuthenticationPlugin) AuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// JWT authentication logic
			next.ServeHTTP(w, r)
		})
	}
}

func (p *AuthenticationPlugin) loginHandler(w http.ResponseWriter, r *http.Request) {
	// Login implementation
}

func (p *AuthenticationPlugin) refreshHandler(w http.ResponseWriter, r *http.Request) {
	// Token refresh implementation
}

func (p *AuthenticationPlugin) logoutHandler(w http.ResponseWriter, r *http.Request) {
	// Logout implementation
}

// Example Data Models

type User struct {
	ID        int64     `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Email     string    `json:"email" db:"email"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type EmailRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Mock Services for Testing

type MockEmailService struct {
	sentEmails []EmailRequest
}

func (m *MockEmailService) SendEmail(to, subject, body string) error {
	m.sentEmails = append(m.sentEmails, EmailRequest{
		To:      to,
		Subject: subject,
		Body:    body,
	})
	return nil
}

// Placeholder Middleware

func AuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func ValidationMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func RateLimitMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func SecurityHeadersMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func AuthenticationMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func AuthorizationMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func AuditLoggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func RequestValidationMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func AdminRequiredMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next
	}
}

func protectedHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Protected content"))
}

func adminOnlyHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Admin only content"))
}

// Placeholder route functions
func userRoutes(r router.Router)         {}
func organizationRoutes(r router.Router) {}
func billingRoutes(r router.Router)      {}
func analyticsRoutes(r router.Router)    {}
func adminUserRoutes(r router.Router)    {}
func systemRoutes(r router.Router)       {}
func monitoringRoutes(r router.Router)   {}

// Placeholder services
func NewJWTService(secret string, duration time.Duration) interface{} {
	return &struct{}{}
}

// Placeholder plugins
type LoggingPlugin struct{}

func (p *LoggingPlugin) Name() string                                       { return "logging" }
func (p *LoggingPlugin) Version() string                                    { return "1.0.0" }
func (p *LoggingPlugin) Description() string                                { return "Enhanced logging" }
func (p *LoggingPlugin) Author() string                                     { return "Forge Team" }
func (p *LoggingPlugin) Initialize(container forge.Container) error         { return nil }
func (p *LoggingPlugin) Start(ctx context.Context) error                    { return nil }
func (p *LoggingPlugin) Stop(ctx context.Context) error                     { return nil }
func (p *LoggingPlugin) Routes() []plugins.RouteDefinition                  { return nil }
func (p *LoggingPlugin) Jobs() []plugins.JobDefinition                      { return nil }
func (p *LoggingPlugin) Middleware() []plugins.MiddlewareDefinition         { return nil }
func (p *LoggingPlugin) Commands() []plugins.CommandDefinition              { return nil }
func (p *LoggingPlugin) HealthChecks() []plugins.HealthCheckDefinition      { return nil }
func (p *LoggingPlugin) ConfigSchema() map[string]interface{}               { return nil }
func (p *LoggingPlugin) DefaultConfig() map[string]interface{}              { return nil }
func (p *LoggingPlugin) ValidateConfig(config map[string]interface{}) error { return nil }
func (p *LoggingPlugin) Dependencies() []plugins.Dependency                 { return nil }
func (p *LoggingPlugin) IsEnabled() bool                                    { return true }
func (p *LoggingPlugin) IsInitialized() bool                                { return true }
func (p *LoggingPlugin) Status() plugins.PluginStatus                       { return plugins.PluginStatus{} }

type MetricsPlugin struct{}

func (p *MetricsPlugin) Name() string                                       { return "metrics" }
func (p *MetricsPlugin) Version() string                                    { return "1.0.0" }
func (p *MetricsPlugin) Description() string                                { return "Enhanced metrics" }
func (p *MetricsPlugin) Author() string                                     { return "Forge Team" }
func (p *MetricsPlugin) Initialize(container forge.Container) error         { return nil }
func (p *MetricsPlugin) Start(ctx context.Context) error                    { return nil }
func (p *MetricsPlugin) Stop(ctx context.Context) error                     { return nil }
func (p *MetricsPlugin) Routes() []plugins.RouteDefinition                  { return nil }
func (p *MetricsPlugin) Jobs() []plugins.JobDefinition                      { return nil }
func (p *MetricsPlugin) Middleware() []plugins.MiddlewareDefinition         { return nil }
func (p *MetricsPlugin) Commands() []plugins.CommandDefinition              { return nil }
func (p *MetricsPlugin) HealthChecks() []plugins.HealthCheckDefinition      { return nil }
func (p *MetricsPlugin) ConfigSchema() map[string]interface{}               { return nil }
func (p *MetricsPlugin) DefaultConfig() map[string]interface{}              { return nil }
func (p *MetricsPlugin) ValidateConfig(config map[string]interface{}) error { return nil }
func (p *MetricsPlugin) Dependencies() []plugins.Dependency                 { return nil }
func (p *MetricsPlugin) IsEnabled() bool                                    { return true }
func (p *MetricsPlugin) IsInitialized() bool                                { return true }
func (p *MetricsPlugin) Status() plugins.PluginStatus                       { return plugins.PluginStatus{} }
