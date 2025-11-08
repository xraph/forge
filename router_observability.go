package forge

import (
	"github.com/xraph/forge/internal/router"
)

// WithMetrics enables metrics collection.
func WithMetrics(config MetricsConfig) RouterOption {
	return router.WithMetrics(config)
}

// WithHealth enables health checks.
func WithHealth(config HealthConfig) RouterOption {
	return router.WithHealth(config)
}
