package core

import (
	"github.com/xraph/forge/pkg/common"
)

// =============================================================================
// METRICS COLLECTOR INTERFACE
// =============================================================================

// MetricsCollector defines the interface for metrics collection
type MetricsCollector = common.Metrics

// CollectorStats contains statistics about the metrics collector
type CollectorStats = common.CollectorStats
