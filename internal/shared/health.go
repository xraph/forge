package shared

import (
	"github.com/xraph/go-utils/metrics"
)

// HealthManager defines the interface for health checking.
type HealthManager = metrics.HealthManager

// HealthCheckFn represents a single health check.
type HealthCheckFn = metrics.HealthCheckFn

// HealthCheck defines the interface for health checks.
type HealthCheck = metrics.HealthCheck

// HealthStatus represents the health status of a service or component.
type HealthStatus = metrics.HealthStatus

const (
	HealthStatusHealthy   HealthStatus = metrics.HealthStatusHealthy
	HealthStatusDegraded  HealthStatus = metrics.HealthStatusDegraded
	HealthStatusUnhealthy HealthStatus = metrics.HealthStatusUnhealthy
	HealthStatusUnknown   HealthStatus = metrics.HealthStatusUnknown
)

// HealthResult represents the result of a health check.
type HealthResult = metrics.HealthResult

// NewHealthResult creates a new health result.
func NewHealthResult(name string, status HealthStatus, message string) *HealthResult {
	return metrics.NewHealthResult(name, status, message)
}

// HealthReport represents a comprehensive health report.
type HealthReport = metrics.HealthReport

// NewHealthReport creates a new health report.
func NewHealthReport() *HealthReport {
	return metrics.NewHealthReport()
}

// HealthCallback is a callback function for health status changes.
type HealthCallback = metrics.HealthCallback

// HealthReportCallback is a callback function for health report changes.
type HealthReportCallback = metrics.HealthReportCallback

// HealthCheckerStats contains statistics about the health checker.
type HealthCheckerStats = metrics.HealthCheckerStats

// HealthConfig configures health checks.
type HealthConfig = metrics.HealthConfig

// DefaultHealthConfig returns default health configuration.
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		Enabled: true,
		// HealthPath:         "/_/health",
		// LivenessPath:       "/_/health/live",
		// ReadinessPath:      "/_/health/ready",
		// HealthCheckTimeout: 5 * time.Second,
	}
}
