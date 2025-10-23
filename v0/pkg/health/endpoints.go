package health

import (
	"fmt"

	"github.com/xraph/forge/pkg/common"
	healthcore "github.com/xraph/forge/pkg/health/core"
)

// HealthEndpointManager manages HTTP endpoints for health checks
type HealthEndpointManager = healthcore.HealthEndpointManager

// EndpointConfig contains configuration for health endpoints
type EndpointConfig = healthcore.EndpointConfig

// DefaultEndpointConfig returns default configuration for health endpoints
func DefaultEndpointConfig() *EndpointConfig {
	return healthcore.DefaultEndpointConfig()
}

// NewHealthEndpointManager creates a new health endpoint manager
func NewHealthEndpointManager(healthService healthcore.HealthService, logger common.Logger, metrics common.Metrics, config *EndpointConfig) *HealthEndpointManager {
	return healthcore.NewHealthEndpointManager(healthService, logger, metrics, config)
}

// HealthEndpointHandlers provides direct handler functions for integration
type HealthEndpointHandlers = healthcore.HealthEndpointHandlers

// NewHealthEndpointHandlers creates new health endpoint handlers
func NewHealthEndpointHandlers(manager *HealthEndpointManager) *HealthEndpointHandlers {
	return healthcore.NewHealthEndpointHandlers(manager)
}

// CreateHealthEndpoints creates health endpoints for a router
func CreateHealthEndpoints(router common.Router, healthService healthcore.HealthService, logger common.Logger, metrics common.Metrics) error {
	return healthcore.CreateHealthEndpoints(router, healthService, logger, metrics)
}

// CreateHealthEndpointsWithConfig creates health endpoints with custom configuration
func CreateHealthEndpointsWithConfig(router common.Router, healthService healthcore.HealthService, logger common.Logger, metrics common.Metrics, config *EndpointConfig) error {
	return healthcore.CreateHealthEndpointsWithConfig(router, healthService, logger, metrics, config)
}

// RegisterHealthEndpointsWithContainer registers health endpoints using the DI container
func RegisterHealthEndpointsWithContainer(container common.Container, router common.Router) error {
	// Get health service from container
	healthService, err := GetHealthServiceFromContainer(container)
	if err != nil {
		return fmt.Errorf("failed to get health service from container: %w", err)
	}

	// Get logger and metrics from container
	logger, _ := container.ResolveNamed(common.LoggerKey)
	metrics, _ := container.ResolveNamed(common.MetricsCollectorKey)

	// Create endpoints
	config := DefaultEndpointConfig()
	manager := NewHealthEndpointManager(healthService, logger.(common.Logger), metrics.(common.Metrics), config)

	return manager.RegisterEndpoints(router)
}
