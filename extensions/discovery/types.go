package discovery

import "github.com/xraph/forge/extensions/discovery/backends"

// Backend defines the interface for service discovery backends.
type Backend = backends.Backend

// ServiceInstance represents a registered service instance.
type ServiceInstance = backends.ServiceInstance

// HealthStatus represents service health status.
type HealthStatus = backends.HealthStatus

const (
	// HealthStatusPassing indicates the service is healthy.
	HealthStatusPassing = backends.HealthStatusPassing

	// HealthStatusWarning indicates the service has warnings.
	HealthStatusWarning = backends.HealthStatusWarning

	// HealthStatusCritical indicates the service is unhealthy.
	HealthStatusCritical = backends.HealthStatusCritical

	// HealthStatusUnknown indicates the health status is unknown.
	HealthStatusUnknown = backends.HealthStatusUnknown
)

// LoadBalanceStrategy defines load balancing strategies.
type LoadBalanceStrategy string

const (
	// LoadBalanceRoundRobin uses round-robin selection.
	LoadBalanceRoundRobin LoadBalanceStrategy = "round_robin"

	// LoadBalanceRandom uses random selection.
	LoadBalanceRandom LoadBalanceStrategy = "random"

	// LoadBalanceLeastConnections uses least connections selection.
	LoadBalanceLeastConnections LoadBalanceStrategy = "least_connections"

	// LoadBalanceWeightedRoundRobin uses weighted round-robin.
	LoadBalanceWeightedRoundRobin LoadBalanceStrategy = "weighted_round_robin"
)

// ServiceRegistration represents a service registration request.
type ServiceRegistration struct {
	// Service is the service configuration
	Service ServiceConfig

	// HealthCheck is the health check configuration
	HealthCheck HealthCheckConfig
}

// ServiceQuery represents a service discovery query.
type ServiceQuery struct {
	// Name is the service name to query
	Name string

	// Tags are tags to filter by
	Tags []string

	// OnlyHealthy returns only healthy instances
	OnlyHealthy bool

	// LoadBalanceStrategy is the load balancing strategy
	LoadBalanceStrategy LoadBalanceStrategy
}

// ServiceEvent represents a service change event.
type ServiceEvent struct {
	// Type is the event type
	Type ServiceEventType

	// Service is the service instance
	Service *ServiceInstance

	// Timestamp is when the event occurred
	Timestamp int64
}

// ServiceEventType represents types of service events.
type ServiceEventType string

const (
	// ServiceEventRegistered indicates a service was registered.
	ServiceEventRegistered ServiceEventType = "registered"

	// ServiceEventDeregistered indicates a service was deregistered.
	ServiceEventDeregistered ServiceEventType = "deregistered"

	// ServiceEventHealthChanged indicates service health changed.
	ServiceEventHealthChanged ServiceEventType = "health_changed"

	// ServiceEventMetadataChanged indicates service metadata changed.
	ServiceEventMetadataChanged ServiceEventType = "metadata_changed"
)
