package internal

import (
	"time"

	"github.com/xraph/forge/internal/shared"
)

// HealthConfig contains configuration for the health checker.
type HealthConfig = shared.HealthConfig

// DefaultHealthCheckerConfig returns default configuration.
func DefaultHealthCheckerConfig() *HealthConfig {
	return &HealthConfig{
		CheckInterval:          30 * time.Second,
		ReportInterval:         60 * time.Second,
		EnableAutoDiscovery:    true,
		EnablePersistence:      false,
		EnableAlerting:         false,
		MaxConcurrentChecks:    10,
		DefaultTimeout:         5 * time.Second,
		CriticalServices:       []string{},
		DegradedThreshold:      0.1,
		UnhealthyThreshold:     0.05,
		EnableSmartAggregation: true,
		EnablePrediction:       false,
		HistorySize:            100,
		Tags:                   make(map[string]string),
	}
}

// HealthCheckerStats contains statistics about the health checker.
type HealthCheckerStats = shared.HealthCheckerStats
