package health

import (
	healthcore "github.com/xraph/forge/pkg/health/core"
)

// HealthStatus represents the health status of a service or component
type HealthStatus = healthcore.HealthStatus

const (
	HealthStatusHealthy   = healthcore.HealthStatusHealthy
	HealthStatusDegraded  = healthcore.HealthStatusDegraded
	HealthStatusUnhealthy = healthcore.HealthStatusUnhealthy
	HealthStatusUnknown   = healthcore.HealthStatusUnknown
)

// HealthResult represents the result of a health check
type HealthResult = healthcore.HealthResult

// NewHealthResult creates a new health result
func NewHealthResult(name string, status HealthStatus, message string) *HealthResult {
	return healthcore.NewHealthResult(name, status, message)
}

// HealthReport represents a comprehensive health report
type HealthReport = healthcore.HealthReport

// NewHealthReport creates a new health report
func NewHealthReport() *HealthReport {
	return healthcore.NewHealthReport()
}

// HealthCallback is a callback function for health status changes
type HealthCallback = healthcore.HealthCallback

// HealthReportCallback is a callback function for health report changes
type HealthReportCallback = healthcore.HealthReportCallback
