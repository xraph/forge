package forge

import (
	"github.com/xraph/vessel"
)

// Container provides dependency injection with lifecycle management.
type Container = vessel.Vessel

// ProvideOption is an alias for vessel.ConstructorOption, used to configure options for constructing objects.
type ProvideOption = vessel.ConstructorOption

// DIScope represents a lifetime scope for scoped services in the DI container.
// Typically used for HTTP requests or other bounded operations.
type DIScope = vessel.Scope

// Factory creates a service instance.
type Factory = vessel.Factory

// ServiceInfo contains diagnostic information.
type ServiceInfo = vessel.ServiceInfo

// NewContainer creates a new DI container.
func NewContainer() Container {
	return vessel.New()
}
