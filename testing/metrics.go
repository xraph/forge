package testing

import (
	"github.com/xraph/forge/internal/metrics"
)

func NewNoOpMetrics() metrics.Metrics {
	return metrics.NewNoOpMetrics()
}
