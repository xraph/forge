package metrics

import (
	"github.com/xraph/go-utils/metrics"
)

// NewNoOpMetrics creates a no-op metrics collector that implements the full
// Metrics interface but performs no actual metric collection or storage.
// Useful for testing, benchmarking, or when metrics are disabled.
func NewNoOpMetrics() Metrics {
	return metrics.NewMockMetrics()
}
