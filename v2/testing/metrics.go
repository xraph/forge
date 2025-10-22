package testing

import (
	"github.com/xraph/forge/v2/internal/metrics"
)

func NewNoOpMetrics() metrics.Metrics {
	return metrics.NewNoOpMetrics()
}
