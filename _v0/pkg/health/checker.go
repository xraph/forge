package health

import (
	"github.com/xraph/forge/v0/pkg/common"
	healthcore "github.com/xraph/forge/v0/pkg/health/core"
)

// HealthChecker implements comprehensive health monitoring for all services
type HealthChecker = healthcore.HealthChecker

// HealthCheckerConfig contains configuration for the health checker
type HealthCheckerConfig = healthcore.HealthCheckerConfig

// DefaultHealthCheckerConfig returns default configuration
func DefaultHealthCheckerConfig() *HealthCheckerConfig {
	return healthcore.DefaultHealthCheckerConfig()
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(config *HealthCheckerConfig, logger common.Logger, metrics common.Metrics, container common.Container) common.HealthChecker {
	return healthcore.NewHealthChecker(config, logger, metrics, container)
}

// HealthCheckerStats contains statistics about the health checker
type HealthCheckerStats = healthcore.HealthCheckerStats
