package common

import "context"

// =============================================================================
// CONTAINER INTERFACE
// =============================================================================

// Container defines the dependency injection container interface
type Container interface {

	// Register adds a service definition to the container for dependency resolution and lifecycle management.
	Register(definition ServiceDefinition) error

	// Resolve resolves and retrieves the specified service instance from the dependency injection container.
	Resolve(serviceType interface{}) (interface{}, error)

	// ResolveNamed resolves a service from the container by its registered name and returns the service instance or an error.
	ResolveNamed(name string) (interface{}, error)

	// Has checks if a service of the specified type is registered in the container.
	Has(serviceType interface{}) bool

	// HasNamed checks if a service with the specified name is registered in the container. Returns true if found, otherwise false.
	HasNamed(name string) bool

	// Services returns a slice of all registered service definitions in the container.
	Services() []ServiceDefinition

	// Start initializes and begins the lifecycle of all registered services within the container.
	// It ensures dependencies between services are resolved and services are launched in the correct order.
	// Returns an error if any service fails to start or if there are unresolved dependencies.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the container, releasing resources and stopping managed services.
	Stop(ctx context.Context) error

	// HealthCheck checks the health status of the container and its dependencies and returns an error if any health check fails.
	HealthCheck(ctx context.Context) error

	// GetValidator retrieves the Validator instance associated with the container.
	GetValidator() Validator

	// LifecycleManager handles the management and control of service lifecycles including start, stop, and health checks.
	LifecycleManager() LifecycleManager
}

// =============================================================================
// LIFECYCLE MANAGER
// =============================================================================

// LifecycleManager manages the lifecycle of services
type LifecycleManager interface {
	AddService(service Service) error
	RemoveService(name string) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	StartServices(ctx context.Context) error
	StopServices(ctx context.Context) error
	HealthCheck(ctx context.Context) error
	GetService(name string) (Service, error)
	GetServices() []Service
}

// =============================================================================
// VALIDATOR
// =============================================================================

// Validator defines the interface for dependency validation
type Validator interface {
	ValidateService(serviceName string) error
	GetValidationReport() ValidationReport
	ValidateServiceHandlers(serviceHandlers map[string]*RouteHandlerInfo) error
	ValidateAll() error
	ValidateAllWithHandlers(serviceHandlers map[string]*RouteHandlerInfo) error
}

// ValidationReport contains validation results
type ValidationReport struct {
	Services                map[string]ServiceValidationResult `json:"services"`
	Valid                   bool                               `json:"valid"`
	CircularDependencyError string                             `json:"circular_dependency_error,omitempty"`
}

// ServiceValidationResult contains validation results for a service
type ServiceValidationResult struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Dependencies []string `json:"dependencies"`
	Valid        bool     `json:"valid"`
	Error        string   `json:"error,omitempty"`
}
