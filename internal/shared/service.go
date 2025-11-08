package shared

import "context"

// Service is the standard interface for managed services
// Container auto-detects and calls these methods.
type Service interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// HealthChecker is optional for services that provide health checks.
type HealthChecker interface {
	Health(ctx context.Context) error
}

// Configurable is optional for services that need configuration.
type Configurable interface {
	Configure(config any) error
}

// Disposable is optional for scoped services that need cleanup.
type Disposable interface {
	Dispose() error
}
