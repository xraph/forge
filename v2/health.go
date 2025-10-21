package forge

import (
	"context"
	"time"
)

// HealthManager performs health checks across the system
type HealthManager interface {
	// Check performs all health checks
	Check(ctx context.Context) HealthReport

	// Register registers a health check
	Register(name string, check HealthCheck)
}

// HealthCheck represents a single health check
type HealthCheck func(ctx context.Context) HealthResult

// HealthResult represents the result of a health check
type HealthResult struct {
	Status  HealthStatus   `json:"status"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// HealthStatus represents overall health
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// HealthReport contains results of all checks
type HealthReport struct {
	Status    HealthStatus            `json:"status"`
	Timestamp time.Time               `json:"timestamp"`
	Duration  time.Duration           `json:"duration"`
	Checks    map[string]HealthResult `json:"checks"`
}

// HealthConfig configures health checks
type HealthConfig struct {
	Enabled            bool
	HealthPath         string
	LivenessPath       string
	ReadinessPath      string
	HealthCheckTimeout time.Duration
}

// DefaultHealthConfig returns default health configuration
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		Enabled:            true,
		HealthPath:         "/_/health",
		LivenessPath:       "/_/health/live",
		ReadinessPath:      "/_/health/ready",
		HealthCheckTimeout: 5 * time.Second,
	}
}
