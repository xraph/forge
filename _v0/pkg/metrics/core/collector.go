package core

import (
	"github.com/xraph/forge/v0/pkg/common"
)

// =============================================================================
// METRICS COLLECTOR INTERFACE
// =============================================================================

// MetricsCollector defines the interface for metrics collection
type MetricsCollector = common.Metrics

// CollectorStats contains statistics about the metrics collector
type CollectorStats = common.CollectorStats
