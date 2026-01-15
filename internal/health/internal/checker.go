package internal

import (
	"time"

	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/metrics"
)

// HealthConfig contains configuration for the health checker.
type HealthConfig = shared.HealthConfig

// DefaultHealthCheckerConfig returns default configuration.
func DefaultHealthCheckerConfig() *HealthConfig {
	return &HealthConfig{
		Enabled: true,
		Features: metrics.HealthFeatures{
			AutoDiscovery: true,
			Persistence:   true,
			Alerting:      true,
			Aggregation:   true,
			Prediction:    false,
			Metrics:       false,
		},
		Intervals: metrics.HealthIntervals{
			Check:  30 * time.Second,
			Report: 60 * time.Second,
		},
		Thresholds: metrics.HealthThresholds{
			Degraded:  0.1,
			Unhealthy: 0.05,
		},
		Performance: metrics.HealthPerformance{
			MaxConcurrentChecks: 10,
			DefaultTimeout:      5 * time.Second,
			HistorySize:         100,
		},
		CriticalServices: []string{},
		Tags:             make(map[string]string),
	}
}

// HealthCheckerStats contains statistics about the health checker.
type HealthCheckerStats = shared.HealthCheckerStats
