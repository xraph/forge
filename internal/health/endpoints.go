package health

import (
	"fmt"

	healthcore "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// HealthEndpointManager manages HTTP endpoints for health checks.
type HealthEndpointManager = healthcore.HealthEndpointManager

// EndpointConfig contains configuration for health endpoints.
type EndpointConfig = healthcore.EndpointConfig

// DefaultEndpointConfig returns default configuration for health endpoints.
func DefaultEndpointConfig() *EndpointConfig {
	return healthcore.DefaultEndpointConfig()
}

// NewHealthEndpointManager creates a new health endpoint manager.
func NewHealthEndpointManager(healthService shared.HealthManager, logger logger.Logger, metrics shared.Metrics, config *EndpointConfig) *HealthEndpointManager {
	return healthcore.NewHealthEndpointManager(healthService, logger, metrics, config)
}

// HealthEndpointHandlers provides direct handler functions for integration.
type HealthEndpointHandlers = healthcore.HealthEndpointHandlers

// NewHealthEndpointHandlers creates new health endpoint handlers.
func NewHealthEndpointHandlers(manager *HealthEndpointManager) *HealthEndpointHandlers {
	return healthcore.NewHealthEndpointHandlers(manager)
}

// CreateHealthEndpoints creates health endpoints for a router.
func CreateHealthEndpoints(router shared.Router, healthService shared.HealthManager, logger logger.Logger, metrics shared.Metrics) error {
	return healthcore.CreateHealthEndpoints(router, healthService, logger, metrics)
}

// CreateHealthEndpointsWithConfig creates health endpoints with custom configuration.
func CreateHealthEndpointsWithConfig(router shared.Router, healthService shared.HealthManager, logger logger.Logger, metrics shared.Metrics, config *EndpointConfig) error {
	return healthcore.CreateHealthEndpointsWithConfig(router, healthService, logger, metrics, config)
}

// RegisterHealthEndpointsWithContainer registers health endpoints using the DI container.
func RegisterHealthEndpointsWithContainer(container shared.Container, router shared.Router) error {
	// Get health service from container
	healthService, err := GetHealthServiceFromContainer(container)
	if err != nil {
		return fmt.Errorf("failed to get health service from container: %w", err)
	}

	// Get logger and metrics from container
	l, _ := container.Resolve(shared.LoggerKey)
	m, _ := container.Resolve(shared.MetricsCollectorKey)

	// Create endpoints
	config := DefaultEndpointConfig()
	manager := NewHealthEndpointManager(healthService, l.(logger.Logger), m.(shared.Metrics), config)

	return manager.RegisterEndpoints(router)
}
