package forge

import (
	"github.com/xraph/vessel"
)

// Container provides dependency injection with lifecycle management.
type Container = vessel.Vessel

// Scope represents a lifetime scope for scoped services
// Typically used for HTTP requests or other bounded operations.
type Scope = vessel.Scope

// Factory creates a service instance.
type Factory = vessel.Factory

// ServiceInfo contains diagnostic information.
type ServiceInfo = vessel.ServiceInfo

// NewContainer creates a new DI container.
func NewContainer() Container {
	return vessel.New()
}
