package shared

import "context"

// Container provides dependency injection with lifecycle management.
type Container interface {
	// Register adds a service factory to the container
	// Returns error if name already registered or factory is invalid
	Register(name string, factory Factory, opts ...RegisterOption) error

	// Resolve returns a service by name
	// Returns error if not found or instantiation fails
	Resolve(name string) (any, error)

	// ResolveReady resolves a service, ensuring it and its dependencies are started first.
	// This is useful during extension Register() phase when you need a dependency
	// to be fully initialized before use.
	// It will call Start() on the service if it implements shared.Service and hasn't been started.
	ResolveReady(ctx context.Context, name string) (any, error)

	// Has checks if a service is registered
	Has(name string) bool

	// IsStarted checks if a service has been started
	// Returns false if service doesn't exist or hasn't been started
	IsStarted(name string) bool

	// Services returns all registered service names
	Services() []string

	// BeginScope creates a new scope for request-scoped services
	// Scopes must be ended with scope.End() to clean up resources
	BeginScope() Scope

	// Start initializes all services in dependency order
	Start(ctx context.Context) error

	// Stop shuts down all services in reverse order
	Stop(ctx context.Context) error

	// Health checks all services
	Health(ctx context.Context) error

	// Inspect returns diagnostic information about a service
	Inspect(name string) ServiceInfo
}

// Scope represents a lifetime scope for scoped services
// Typically used for HTTP requests or other bounded operations.
type Scope interface {
	// Resolve returns a service by name from this scope
	// Scoped services are cached within the scope
	// Singleton services are resolved from parent container
	Resolve(name string) (any, error)

	// End cleans up all scoped services in this scope
	// Must be called when scope is no longer needed (typically in defer)
	End() error
}

// Factory creates a service instance.
type Factory func(c Container) (any, error)

// ServiceInfo contains diagnostic information.
type ServiceInfo struct {
	Name         string
	Type         string
	Lifecycle    string
	Dependencies []string // Backward compat: dependency names
	Deps         []Dep    // New: full dependency specs with modes
	Started      bool
	Healthy      bool
	Metadata     map[string]string
}
