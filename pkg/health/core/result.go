package core

import (
	"github.com/xraph/forge/pkg/common"
)

// HealthStatus represents the health status of a service or component
type HealthStatus = common.HealthStatus

const (
	HealthStatusHealthy   = common.HealthStatusHealthy
	HealthStatusDegraded  = common.HealthStatusDegraded
	HealthStatusUnhealthy = common.HealthStatusUnhealthy
	HealthStatusUnknown   = common.HealthStatusUnknown
)

// HealthResult represents the result of a health check
type HealthResult = common.HealthResult

// NewHealthResult creates a new health result
func NewHealthResult(name string, status HealthStatus, message string) *HealthResult {
	return common.NewHealthResult(name, status, message)
}

// HealthReport represents a comprehensive health report
type HealthReport = common.HealthReport

// NewHealthReport creates a new health report
func NewHealthReport() *HealthReport {
	return common.NewHealthReport()
}

// HealthCallback is a callback function for health status changes
type HealthCallback = common.HealthCallback

// HealthReportCallback is a callback function for health report changes
type HealthReportCallback = common.HealthReportCallback
