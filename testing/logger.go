package testing

import (
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics"
)

func NewTestLogger() logger.Logger {
	return logger.NewTestLogger()
}

func NewMetrics() metrics.Metrics {
	return metrics.NewNoOpMetrics()
}
