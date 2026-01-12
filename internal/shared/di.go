package shared

import "github.com/xraph/go-utils/di"

// Container provides dependency injection with lifecycle management.
type Container = di.Container

// Scope represents a lifetime scope for scoped services
// Typically used for HTTP requests or other bounded operations.
type Scope = di.Scope

// Factory creates a service instance.
type Factory = di.Factory

// ServiceInfo contains diagnostic information.
type ServiceInfo = di.ServiceInfo
