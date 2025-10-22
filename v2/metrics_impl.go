package forge

import (
	"github.com/xraph/forge/v2/internal/logger"
	"github.com/xraph/forge/v2/internal/metrics"
)

// NewMetrics creates a new metrics instance
func NewMetrics(config *metrics.CollectorConfig, logger logger.Logger) Metrics {
	return metrics.New(config, logger)
}
