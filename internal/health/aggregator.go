package health

import (
	healthcore "github.com/xraph/forge/internal/health/internal"
)

// HealthAggregator aggregates health check results into an overall health status.
type HealthAggregator = healthcore.HealthAggregator

// AggregatorConfig contains configuration for the health aggregator.
type AggregatorConfig = healthcore.AggregatorConfig

// DefaultAggregatorConfig returns default configuration for the health aggregator.
func DefaultAggregatorConfig() *AggregatorConfig {
	return healthcore.DefaultAggregatorConfig()
}

// NewHealthAggregator creates a new health aggregator.
func NewHealthAggregator(config *AggregatorConfig) *HealthAggregator {
	return healthcore.NewHealthAggregator(config)
}

// SmartAggregator implements advanced aggregation logic with machine learning-like features.
type SmartAggregator = healthcore.SmartAggregator

// HealthStatusSnapshot represents a point-in-time health status.
type HealthStatusSnapshot = healthcore.HealthStatusSnapshot

// NewSmartAggregator creates a new smart health aggregator.
func NewSmartAggregator(config *AggregatorConfig) *SmartAggregator {
	return healthcore.NewSmartAggregator(config)
}

// PredictiveAggregator implements predictive health aggregation.
type PredictiveAggregator = healthcore.PredictiveAggregator

// NewPredictiveAggregator creates a new predictive health aggregator.
func NewPredictiveAggregator(config *AggregatorConfig) *PredictiveAggregator {
	return healthcore.NewPredictiveAggregator(config)
}

// HealthPrediction represents a health prediction.
type HealthPrediction = healthcore.HealthPrediction
