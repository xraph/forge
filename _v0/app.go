package v0

import (
	"context"
	"net/http"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	metrics "github.com/xraph/forge/v0/pkg/metrics/core"
)

// =============================================================================
// APPLICATION INTERFACE
// =============================================================================

// Application represents the main application with complete lifecycle management
type Application interface {

	// Name returns the unique name of the application.
	Name() string

	// Version retrieves the version information of the application.
	Version() string

	// Description returns a brief textual summary of the application's functionality or purpose.
	Description() string

	// Container returns the dependency injection container used by the application.
	Container() Container

	// Router retrieves the common.Router instance used for HTTP routing within the application.
	Router() common.Router

	// Logger returns the logger interface used for logging within the application.
	Logger() logger.Logger

	// Metrics provides access to the metrics collector for application monitoring and performance tracking.
	Metrics() metrics.MetricsCollector

	// Config retrieves the application's configuration manager instance.
	Config() common.ConfigManager

	// HealthChecker returns the application's health checker implementation, responsible for managing health checks.
	HealthChecker() common.HealthChecker

	// SetContainer sets the dependency injection container for the application and returns an error if the operation fails.
	SetContainer(container Container) error

	// SetLogger sets the logger instance for the application to handle logging functionality.
	SetLogger(logger logger.Logger)

	// SetMetrics assigns a metrics collector to the application for tracking metrics and performance monitoring.
	SetMetrics(metrics metrics.MetricsCollector)

	// SetConfig assigns the provided configuration manager to the application.
	SetConfig(config common.ConfigManager)

	// SetHealthChecker sets the HealthChecker instance for the application.
	SetHealthChecker(healthChecker common.HealthChecker)

	// SetLifecycleManager configures the lifecycle manager responsible for managing services within the application.
	SetLifecycleManager(lifecycle common.LifecycleManager)

	// SetErrorHandler sets the global error handler for the application to handle runtime errors and provide retry strategies.
	SetErrorHandler(errorHandler common.ErrorHandler)

	// AddService adds a new service to the application's lifecycle management.
	// It returns an error if the service cannot be added or already exists.
	AddService(service common.Service) error

	// RemoveService removes a service from the application by its name and returns an error if the operation fails.
	RemoveService(serviceName string) error

	// GetService retrieves a service by its name from the application container or returns an error if not found.
	GetService(serviceName string) (common.Service, error)

	// GetServices returns a slice of all registered services within the application.
	GetServices() []common.Service

	// AddController registers a new controller with the application, enabling its routes, middleware, and lifecycle integration.
	AddController(controller common.Controller) error

	// RemoveController removes a controller by name from the application and returns an error if the operation fails.
	RemoveController(controllerName string) error

	// GetController retrieves a controller by its name. Returns the controller instance and an error if not found.
	GetController(controllerName string) (common.Controller, error)

	// GetControllers returns a list of all registered controllers in the application.
	GetControllers() []common.Controller

	// AddPlugin adds the specified plugin to the application, initializing and integrating it with the existing components.
	AddPlugin(plugin common.Plugin) error

	// RemovePlugin removes a previously registered plugin by its name. Returns an error if the plugin cannot be found or removed.
	RemovePlugin(pluginName string) error

	// GetPlugin retrieves a registered plugin by name. Returns the plugin and a possible error if not found.
	GetPlugin(pluginName string) (common.Plugin, error)

	// GetPlugins retrieves all registered plugins in the application and returns them as a slice of common.Plugin instances.
	GetPlugins() []common.Plugin

	// Start initializes and begins an operation or process, utilizing the provided context for cancellation or timeouts.
	Start(ctx context.Context) error

	// Stop halts the current process or service gracefully, ensuring all resources are properly released.
	Stop(ctx context.Context) error

	// Run starts the application, initializing all required components and services. It blocks until the application stops.
	Run() error

	// Shutdown gracefully shuts down the application, ensuring all components and services are stopped in a safe manner.
	Shutdown(ctx context.Context) error

	// IsRunning checks whether the application is currently running and returns true if it is running, false otherwise.
	IsRunning() bool

	// HealthCheck performs a health check for the application and returns an error if the application is unhealthy.
	HealthCheck(ctx context.Context) error

	// GetStatus retrieves the current status of the application as a common.ApplicationStatus value.
	GetStatus() common.ApplicationStatus

	// GetInfo retrieves detailed information about the application, including name, version, status, uptime, and metrics.
	GetInfo() common.ApplicationInfo

	// StartServer starts the application's HTTP server on the given address and returns an error if the server fails to start.
	StartServer(addr string) error

	// StopServer gracefully stops the currently running HTTP server within the provided context.
	StopServer(ctx context.Context) error

	// GetServer returns the HTTP server associated with the application.
	GetServer() *http.Server

	// GET registers a handler for the HTTP GET method at the specified path with optional configuration options.
	GET(path string, handler interface{}, options ...common.HandlerOption) error

	// POST registers an HTTP POST handler for the specified path with optional handler options and returns an error if any.
	POST(path string, handler interface{}, options ...common.HandlerOption) error

	// PUT registers a route with the HTTP PUT method for the specified path and associates it with a handler.
	PUT(path string, handler interface{}, options ...common.HandlerOption) error

	// DELETE registers an HTTP DELETE route with the specified path, handler, and optional handler options. Returns an error if registration fails.
	DELETE(path string, handler interface{}, options ...common.HandlerOption) error

	// PATCH registers an HTTP PATCH route with the specified path, handler, and optional handler configuration.
	PATCH(path string, handler interface{}, options ...common.HandlerOption) error

	// RegisterHandler registers an HTTP handler for a specific method and path with optional route configuration options.
	RegisterHandler(method, path string, handler interface{}, options ...common.HandlerOption) error

	// WebSocket registers a WebSocket handler at the specified path with optional configuration options.
	WebSocket(path string, handler interface{}, options ...common.StreamingHandlerInfo) error

	// EventStream registers a Server-Sent Events handler at the specified path with optional configuration options.
	EventStream(path string, handler interface{}, options ...common.StreamingHandlerInfo) error

	// UseMiddleware registers a middleware function to be applied to all HTTP handlers.
	UseMiddleware(handler func(http.Handler) http.Handler)

	// EnableOpenAPI initializes and enables OpenAPI specification generation and UI based on the given configuration.
	EnableOpenAPI(config common.OpenAPIConfig)
}
