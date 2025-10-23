package forge

import (
	"context"

	"github.com/xraph/forge/internal/health"
	"github.com/xraph/forge/internal/shared"
)

// HealthManager performs health checks across the system
type HealthManager = shared.HealthManager

// HealthCheck represents a single health check
type HealthCheck func(ctx context.Context) HealthResult

// HealthResult represents the result of a health check
type HealthResult = shared.HealthResult

// HealthStatus represents overall health
type HealthStatus = shared.HealthStatus

const (
	HealthStatusHealthy   = shared.HealthStatusHealthy
	HealthStatusDegraded  = shared.HealthStatusDegraded
	HealthStatusUnhealthy = shared.HealthStatusUnhealthy
	HealthStatusUnknown   = shared.HealthStatusUnknown
)

// HealthReport contains results of all checks
type HealthReport = shared.HealthReport

// HealthConfig configures health checks
type HealthConfig = shared.HealthConfig

// DefaultHealthConfig returns default health configuration
func DefaultHealthConfig() HealthConfig {
	return shared.DefaultHealthConfig()
}

// NewNoOpHealthManager creates a no-op health manager
func NewNoOpHealthManager() HealthManager {
	return health.NewNoOpHealthManager()
}

// NewHealthManager creates a new health manager
func NewHealthManager(config *health.HealthConfig, logger Logger, metrics shared.Metrics, container shared.Container) HealthManager {
	return health.New(config, logger, metrics, container)
}
