package internal

import (
	"github.com/xraph/forge/internal/shared"
)

// HealthStatus represents the health status of a service or component.
type HealthStatus = shared.HealthStatus

const (
	HealthStatusHealthy   = shared.HealthStatusHealthy
	HealthStatusDegraded  = shared.HealthStatusDegraded
	HealthStatusUnhealthy = shared.HealthStatusUnhealthy
	HealthStatusUnknown   = shared.HealthStatusUnknown
)

// HealthResult represents the result of a health check.
type HealthResult = shared.HealthResult

// NewHealthResult creates a new health result.
func NewHealthResult(name string, status HealthStatus, message string) *HealthResult {
	return shared.NewHealthResult(name, status, message)
}

// HealthReport represents a comprehensive health report.
type HealthReport = shared.HealthReport

// NewHealthReport creates a new health report.
func NewHealthReport() *HealthReport {
	return shared.NewHealthReport()
}

// HealthCallback is a callback function for health status changes.
type HealthCallback = shared.HealthCallback

// HealthReportCallback is a callback function for health report changes.
type HealthReportCallback = shared.HealthReportCallback
