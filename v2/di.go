package forge

import (
	"github.com/xraph/forge/v2/internal/shared"
)

// Container provides dependency injection with lifecycle management
type Container = shared.Container

// Scope represents a lifetime scope for scoped services
// Typically used for HTTP requests or other bounded operations
type Scope = shared.Scope

// Factory creates a service instance
type Factory = shared.Factory

// ServiceInfo contains diagnostic information
type ServiceInfo = shared.ServiceInfo

// NewContainer creates a new DI container
func NewContainer() Container {
	return newContainer()
}
