package forge

import (
	"context"
	"fmt"
	"net/http"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/database"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/plugins"
	"github.com/xraph/forge/router"
)

// Application represents the main application interface
type Application interface {
	// Core metadata
	Name() string
	Version() string
	Environment() string

	// Core components
	Container() core.Container
	Config() core.Config
	Logger() logger.Logger
	Router() router.Router

	// Service access
	Database(name ...string) database.SQLDatabase
	NoSQL(name ...string) database.NoSQLDatabase
	Cache(name ...string) database.Cache
	Jobs() jobs.Processor
	Metrics() observability.Metrics
	Tracer() observability.Tracer
	Health() observability.Health

	// Plugin management
	Plugins() plugins.Manager

	// Lifecycle management
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Run() error
	Shutdown(ctx context.Context) error

	// Health and readiness
	Healthy(ctx context.Context) error
	Ready(ctx context.Context) error
	IsReady() bool
	IsHealthy() bool

	// Callbacks
	OnStartup(callback func(context.Context) error)
	OnShutdown(callback func(context.Context) error)
	OnError(callback func(error))

	// Development helpers
	Reload(ctx context.Context) error
	Reset(ctx context.Context) error
}

// Builder represents the application builder interface with fluent API
type Builder interface {
	// Core Configuration Methods

	// WithConfig sets the application configuration
	WithConfig(config core.Config) Builder

	// WithConfigFile loads configuration from a file
	WithConfigFile(path string) Builder

	// WithConfigFromEnv loads configuration from environment variables
	WithConfigFromEnv() Builder

	// WithConfigFromBytes loads configuration from a byte slice
	WithConfigFromBytes(data []byte) Builder

	// Database Configuration Methods

	// WithDatabase adds a database configuration
	WithDatabase(config database.Config) Builder

	// WithSQL adds a SQL database configuration
	WithSQL(name string, config database.SQLConfig) Builder

	// WithNoSQL adds a NoSQL database configuration
	WithNoSQL(name string, config database.NoSQLConfig) Builder

	// WithCache adds a cache configuration
	WithCache(name string, config database.CacheConfig) Builder

	// Database Convenience Methods

	// WithPostgreSQL adds a PostgreSQL database
	WithPostgreSQL(name, dsn string) Builder

	// WithMySQL adds a MySQL database
	WithMySQL(name, dsn string) Builder

	// WithSQLite adds a SQLite database
	WithSQLite(name, path string) Builder

	// WithMongoDB adds a MongoDB database
	WithMongoDB(name, uri string) Builder

	// WithRedis adds a Redis cache
	WithRedis(name, addr string) Builder

	// WithMemoryCache adds an in-memory cache
	WithMemoryCache(name string) Builder

	// Observability Configuration Methods

	// WithTracing configures distributed tracing
	WithTracing(config observability.TracingConfig) Builder

	// WithMetrics configures metrics collection
	WithMetrics(config observability.MetricsConfig) Builder

	// WithHealth configures health checks
	WithHealth(config observability.HealthConfig) Builder

	// WithLogging configures logging
	WithLogging(config logger.LoggingConfig) Builder

	// Feature Configuration Methods

	// WithJobs configures background job processing
	WithJobs(config jobs.Config) Builder

	// WithPlugins adds plugins to the application
	WithPlugins(plugins ...plugins.Plugin) Builder

	// WithPlugin adds a single plugin to the application
	WithPlugin(plugin plugins.Plugin) Builder

	// HTTP and Routing Configuration Methods

	// WithMiddleware adds HTTP middleware
	WithMiddleware(middleware ...func(http.Handler) http.Handler) Builder

	// WithGroups adds route groups (updated from WithRoutes)
	WithGroups(groups ...router.Group) Builder

	// WithGroup adds a single route group
	WithGroup(group router.Group) Builder

	// WithRouter configures the HTTP router
	WithRouter(config router.Config) Builder

	// WithCORS configures Cross-Origin Resource Sharing
	WithCORS(config router.CORSConfig) Builder

	// WithRateLimit configures rate limiting
	WithRateLimit(config router.RateLimitConfig) Builder

	// Documentation Configuration Methods

	// WithDocumentation configures API documentation
	WithDocumentation(config router.DocumentationConfig) Builder

	// WithOpenAPI configures OpenAPI specification generation
	WithOpenAPI(title, version, description string) Builder

	// WithSwaggerUI enables Swagger UI at the specified path
	WithSwaggerUI(path string) Builder

	// WithReDoc enables ReDoc documentation at the specified path
	WithReDoc(path string) Builder

	// WithAsyncAPI configures AsyncAPI specification for WebSocket/SSE
	WithAsyncAPI(config router.AsyncAPIConfig) Builder

	// WithPostmanCollection enables Postman collection export
	WithPostmanCollection(path string) Builder

	// WithAPIDocumentation enables comprehensive API documentation
	WithAPIDocumentation() Builder

	// Server Configuration Methods

	// WithServer configures the HTTP server
	WithServer(config core.ServerConfig) Builder

	// WithTLS configures TLS/SSL
	WithTLS(certFile, keyFile string) Builder

	// WithHost sets the server host
	WithHost(host string) Builder

	// WithPort sets the server port
	WithPort(port int) Builder

	// Feature Toggle Methods (Enable)

	// EnableJobs enables background job processing
	EnableJobs() Builder

	// EnableCache enables caching functionality
	EnableCache() Builder

	// EnableTracing enables distributed tracing
	EnableTracing() Builder

	// EnableMetrics enables metrics collection
	EnableMetrics() Builder

	// EnableHealthChecks enables health check endpoints
	EnableHealthChecks() Builder

	// EnablePlugins enables plugin system
	EnablePlugins() Builder

	// EnableWebSocket enables WebSocket support
	EnableWebSocket() Builder

	// EnableSSE enables Server-Sent Events support
	EnableSSE() Builder

	// EnableHotReload enables hot reload for development
	EnableHotReload() Builder

	// EnableDocumentation enables API documentation with defaults
	EnableDocumentation() Builder

	// Feature Toggle Methods (Disable)

	// DisableJobs disables background job processing
	DisableJobs() Builder

	// DisableCache disables caching functionality
	DisableCache() Builder

	// DisableTracing disables distributed tracing
	DisableTracing() Builder

	// DisableMetrics disables metrics collection
	DisableMetrics() Builder

	// DisableHealthChecks disables health check endpoints
	DisableHealthChecks() Builder

	// DisablePlugins disables plugin system
	DisablePlugins() Builder

	// DisableDocumentation disables API documentation
	DisableDocumentation() Builder

	// Application Mode Methods

	// WithDevelopmentMode configures the application for development
	WithDevelopmentMode() Builder

	// WithTestingMode configures the application for testing
	WithTestingMode() Builder

	// WithProductionMode configures the application for production
	WithProductionMode() Builder

	// WithHotReload enables hot reload functionality
	WithHotReload() Builder

	// WithAutoMigration enables automatic database migrations
	WithAutoMigration() Builder

	// Build Methods

	// Build creates the application instance
	Build() (Application, error)

	// MustBuild creates the application instance and panics on error
	MustBuild() Application

	// BuildAndStart creates and starts the application
	BuildAndStart(ctx context.Context) (Application, error)

	// MustBuildAndStart creates and starts the application, panics on error
	MustBuildAndStart(ctx context.Context) Application
}

// ApplicationContext provides application-wide context
type ApplicationContext interface {
	context.Context

	// Application reference
	Application() Application
	Logger() logger.Logger
	Config() core.Config
	Container() core.Container

	// Request context (when available)
	RequestID() string
	UserID() string
	SessionID() string
	TraceID() string
	SpanID() string

	// Enrichment
	WithValue(key, value interface{}) ApplicationContext
	WithLogger(logger logger.Logger) ApplicationContext
	WithRequestID(requestID string) ApplicationContext
	WithUserID(userID string) ApplicationContext
	WithTraceID(traceID string) ApplicationContext

	// Service access
	Database(name ...string) database.SQLDatabase
	Cache(name ...string) database.Cache
	Jobs() jobs.Processor
}

// HealthChecker provides application health checking
type HealthChecker interface {
	// Core health check
	Check(ctx context.Context) error

	// Component health checks
	CheckDatabase(ctx context.Context) error
	CheckCache(ctx context.Context) error
	CheckJobs(ctx context.Context) error
	CheckExternal(ctx context.Context) error

	// Health status
	IsHealthy() bool
	LastCheck() map[string]interface{}
	HealthReport() HealthReport
}

// HealthReport represents a comprehensive health report
type HealthReport struct {
	Status     HealthStatus           `json:"status"`
	Components map[string]interface{} `json:"components"`
	Metadata   map[string]interface{} `json:"metadata"`
	Timestamp  string                 `json:"timestamp"`
	Duration   string                 `json:"duration"`
}

// HealthStatus represents overall health status
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// Plugin interfaces for application

// ApplicationPlugin represents a plugin that can extend the application
type ApplicationPlugin interface {
	plugins.Plugin

	// Application-specific hooks
	OnApplicationStart(ctx context.Context, app Application) error
	OnApplicationStop(ctx context.Context, app Application) error
	OnApplicationReady(ctx context.Context, app Application) error

	// Application configuration
	ConfigureApplication(builder Builder) error
	ConfigureContainer(container core.Container) error
	ConfigureRouter(router router.Router) error
}

// DatabasePlugin represents a database plugin
type DatabasePlugin interface {
	plugins.Plugin

	// Database operations
	CreateConnection(config database.Config) (interface{}, error)
	ValidateConnection(conn interface{}) error
	MigrateSchema(ctx context.Context, conn interface{}) error

	// Database-specific configuration
	Config() database.Config
	SupportedDrivers() []string
}

// MiddlewarePlugin represents a middleware plugin
type MiddlewarePlugin interface {
	plugins.Plugin

	// Middleware creation
	CreateMiddleware(config map[string]interface{}) (func(http.Handler) http.Handler, error)
	DefaultMiddlewareConfig() map[string]interface{}

	// Middleware metadata
	MiddlewareName() string
	MiddlewarePriority() int
	ApplyToRoutes() []string
}

// JobPlugin represents a job processing plugin
type JobPlugin interface {
	plugins.Plugin

	// Job handlers
	JobHandlers() map[string]jobs.JobHandler
	RegisterJobHandlers(processor jobs.Processor) error

	// Job schedules
	JobSchedules() []jobs.Schedule
	RegisterJobSchedules(scheduler jobs.Scheduler) error
}

// Configuration interfaces

// ConfigurationSource represents a configuration source
type ConfigurationSource interface {
	// Load configuration
	Load() (map[string]interface{}, error)
	Watch(callback func(map[string]interface{})) error
	Close() error

	// Source metadata
	Name() string
	Priority() int
	IsWatchable() bool
}

// ConfigurationProvider manages multiple configuration sources
type ConfigurationProvider interface {
	// Source management
	AddSource(source ConfigurationSource) error
	RemoveSource(name string) error
	GetSources() []ConfigurationSource

	// Configuration loading
	LoadConfiguration() (core.Config, error)
	ReloadConfiguration() error

	// Configuration watching
	StartWatching() error
	StopWatching() error
	OnConfigurationChange(callback func(core.Config)) error
}

// Application factories and convenience functions

// ApplicationFactory creates applications with predefined configurations
type ApplicationFactory interface {
	// Standard application types
	CreateWebApplication(name string) Builder
	CreateAPIApplication(name string) Builder
	CreateMicroservice(name string) Builder
	CreateCLIApplication(name string) Builder
	CreateWorkerApplication(name string) Builder

	// Domain-specific applications
	CreateAuthService(name string) Builder
	CreateNotificationService(name string) Builder
	CreateFileService(name string) Builder
	CreateUserService(name string) Builder

	// Testing applications
	CreateTestApplication(name string) Builder
	CreateBenchmarkApplication(name string) Builder
}

// Development and testing interfaces

// DevelopmentServer provides development-specific features
type DevelopmentServer interface {
	// Hot reload
	EnableHotReload() error
	DisableHotReload() error
	IsHotReloadEnabled() bool
	Reload() error

	// Development tools
	OpenBrowser(path string) error
	ShowRoutes() []router.RouteInfo
	ShowMetrics() map[string]interface{}
	ShowHealth() HealthReport

	// Development dashboard
	StartDashboard(port int) error
	StopDashboard() error
	DashboardURL() string
}

// TestingApplication provides testing utilities
type TestingApplication interface {
	Application

	// Test helpers
	TestClient() *http.Client
	TestServer() *http.Server
	TestURL(path string) string

	// Mocking
	MockDatabase(name string) database.SQLDatabase
	MockCache(name string) database.Cache
	MockJobs() jobs.Processor

	// Test lifecycle
	Setup() error
	Teardown() error

	// Test assertions
	AssertHealthy() error
	AssertReady() error
	AssertStarted() error
}

// Error types specific to application
var (
	ErrApplicationNotReady       = fmt.Errorf("application not ready")
	ErrApplicationNotHealthy     = fmt.Errorf("application not healthy")
	ErrApplicationAlreadyStarted = fmt.Errorf("application already started")
	ErrApplicationNotStarted     = fmt.Errorf("application not started")
	ErrInvalidConfiguration      = fmt.Errorf("invalid configuration")
	ErrDependencyNotFound        = fmt.Errorf("dependency not found")
	ErrServiceUnavailable        = fmt.Errorf("service unavailable")
	ErrShutdownTimeout           = fmt.Errorf("shutdown timeout")
	ErrStartupFailed             = fmt.Errorf("startup failed")
	ErrPluginLoadFailed          = fmt.Errorf("plugin load failed")
	ErrDatabaseConnectionFailed  = fmt.Errorf("database connection failed")
	ErrCacheConnectionFailed     = fmt.Errorf("cache connection failed")
)

// Event interfaces for application lifecycle

// ApplicationEvent represents an application event
type ApplicationEvent struct {
	Type      ApplicationEventType   `json:"type"`
	Source    string                 `json:"source"`
	Data      map[string]interface{} `json:"data"`
	Timestamp string                 `json:"timestamp"`
	Error     error                  `json:"error,omitempty"`
}

// ApplicationEventType represents the type of application event
type ApplicationEventType string

const (
	EventApplicationStarting  ApplicationEventType = "application.starting"
	EventApplicationStarted   ApplicationEventType = "application.started"
	EventApplicationReady     ApplicationEventType = "application.ready"
	EventApplicationStopping  ApplicationEventType = "application.stopping"
	EventApplicationStopped   ApplicationEventType = "application.stopped"
	EventApplicationError     ApplicationEventType = "application.error"
	EventApplicationHealthy   ApplicationEventType = "application.healthy"
	EventApplicationUnhealthy ApplicationEventType = "application.unhealthy"
	EventPluginLoaded         ApplicationEventType = "plugin.loaded"
	EventPluginUnloaded       ApplicationEventType = "plugin.unloaded"
	EventDatabaseConnected    ApplicationEventType = "database.connected"
	EventDatabaseDisconnected ApplicationEventType = "database.disconnected"
)

// ApplicationEventHandler handles application events
type ApplicationEventHandler interface {
	HandleEvent(ctx context.Context, event ApplicationEvent) error
	CanHandle(eventType ApplicationEventType) bool
}

// ApplicationEventBus manages application events
type ApplicationEventBus interface {
	// Event subscription
	Subscribe(eventType ApplicationEventType, handler ApplicationEventHandler) error
	Unsubscribe(eventType ApplicationEventType, handler ApplicationEventHandler) error

	// Event publishing
	Publish(ctx context.Context, event ApplicationEvent) error
	PublishAsync(ctx context.Context, event ApplicationEvent) error

	// Event bus lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Service interfaces for common functionality

// ServiceManager manages application services
type ServiceManager interface {
	// Service registration
	RegisterService(name string, service interface{}) error
	UnregisterService(name string) error

	// Service access
	GetService(name string) (interface{}, error)
	ListServices() map[string]interface{}

	// Service lifecycle
	StartServices(ctx context.Context) error
	StopServices(ctx context.Context) error

	// Service health
	CheckServices(ctx context.Context) map[string]error
}

// Utility interfaces

// ApplicationMetrics provides application-specific metrics
type ApplicationMetrics interface {
	// Application metrics
	RecordStartup(duration float64)
	RecordShutdown(duration float64)
	RecordRequest(method, path string, status int, duration float64)
	RecordDatabaseQuery(database, operation string, duration float64)
	RecordCacheOperation(cache, operation string, hit bool, duration float64)
	RecordJobExecution(jobType string, success bool, duration float64)

	// System metrics
	RecordMemoryUsage(bytes int64)
	RecordCPUUsage(percent float64)
	RecordGoroutineCount(count int)
	RecordGCDuration(duration float64)

	// Custom metrics
	Counter(name string) observability.Counter
	Gauge(name string) observability.Gauge
	Histogram(name string) observability.Histogram
	Summary(name string) observability.Summary
}

// ApplicationLogger provides application-specific logging
type ApplicationLogger interface {
	logger.Logger

	// Application-specific logging
	LogStartup(fields ...logger.Field)
	LogShutdown(fields ...logger.Field)
	LogRequest(method, path string, status int, duration float64, fields ...logger.Field)
	LogError(err error, context string, fields ...logger.Field)
	LogPanic(recovered interface{}, context string, fields ...logger.Field)

	// Component logging
	DatabaseLogger() logger.Logger
	CacheLogger() logger.Logger
	JobsLogger() logger.Logger
	PluginLogger(pluginName string) logger.Logger

	// Development logging
	LogDevelopment(msg string, fields ...logger.Field)
	LogDebugRequest(req *http.Request, fields ...logger.Field)
	LogDebugResponse(status int, headers http.Header, fields ...logger.Field)
}
