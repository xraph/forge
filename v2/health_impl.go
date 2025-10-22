package forge

import (
	"github.com/xraph/forge/v2/internal/health"
	"github.com/xraph/forge/v2/internal/logger"
	"github.com/xraph/forge/v2/internal/shared"
)

// NewHealthManager creates a new health manager
func NewHealthManager(config *health.HealthConfig, logger logger.Logger, metrics shared.Metrics, container shared.Container) HealthManager {
	return health.New(config, logger, metrics, container)
}
