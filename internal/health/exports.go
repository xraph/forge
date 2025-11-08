package health

import (
	healthcore "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// HealthCheck defines the interface for health checks.
type HealthCheck = healthcore.HealthCheck

// HealthCheckConfig contains configuration for health checks.
type HealthCheckConfig = healthcore.HealthCheckConfig

// DefaultHealthCheckConfig returns default configuration for health checks.
func DefaultHealthCheckConfig() *HealthCheckConfig {
	return healthcore.DefaultHealthCheckConfig()
}

// HealthCheckFunc is a function type for simple health checks.
type HealthCheckFunc = healthcore.HealthCheckFunc

// BaseHealthCheck provides base functionality for health checks.
type BaseHealthCheck = healthcore.BaseHealthCheck

// NewBaseHealthCheck creates a new base health check.
func NewBaseHealthCheck(config *HealthCheckConfig) *BaseHealthCheck {
	return healthcore.NewBaseHealthCheck(config)
}

// SimpleHealthCheck implements a simple function-based health check.
type SimpleHealthCheck = healthcore.SimpleHealthCheck

// NewSimpleHealthCheck creates a new simple health check.
func NewSimpleHealthCheck(config *HealthCheckConfig, checkFunc HealthCheckFunc) *SimpleHealthCheck {
	return healthcore.NewSimpleHealthCheck(config, checkFunc)
}

// AsyncHealthCheck implements an asynchronous health check.
type AsyncHealthCheck = healthcore.AsyncHealthCheck

// NewAsyncHealthCheck creates a new asynchronous health check.
func NewAsyncHealthCheck(config *HealthCheckConfig, checkFunc HealthCheckFunc) *AsyncHealthCheck {
	return healthcore.NewAsyncHealthCheck(config, checkFunc)
}

// CompositeHealthCheck implements a health check that combines multiple checks.
type CompositeHealthCheck = healthcore.CompositeHealthCheck

// NewCompositeHealthCheck creates a new composite health check.
func NewCompositeHealthCheck(config *HealthCheckConfig, checks ...HealthCheck) *CompositeHealthCheck {
	return healthcore.NewCompositeHealthCheck(config, checks...)
}

// HealthCheckWrapper wraps a health check with additional functionality.
type HealthCheckWrapper = healthcore.HealthCheckWrapper

// NewHealthCheckWrapper creates a new health check wrapper.
func NewHealthCheckWrapper(check HealthCheck) *HealthCheckWrapper {
	return healthcore.NewHealthCheckWrapper(check)
}

// HealthChecker implements comprehensive health monitoring for all services.
type HealthChecker = healthcore.HealthService

// NewHealthChecker creates a new health checker.
func NewHealthChecker(config *HealthConfig, logger logger.Logger, metrics shared.Metrics, container shared.Container) shared.HealthManager {
	return New(config, logger, metrics, container)
}

// HealthCheckerStats contains statistics about the health checker.
type HealthCheckerStats = healthcore.HealthCheckerStats

// HealthStatus represents the health status of a service or component.
type HealthStatus = shared.HealthStatus

const (
	HealthStatusHealthy   = shared.HealthStatusHealthy
	HealthStatusDegraded  = shared.HealthStatusDegraded
	HealthStatusUnhealthy = shared.HealthStatusUnhealthy
	HealthStatusUnknown   = shared.HealthStatusUnknown
)

// HealthResult represents the result of a health check.
type HealthResult = healthcore.HealthResult

// NewHealthResult creates a new health result.
func NewHealthResult(name string, status HealthStatus, message string) *HealthResult {
	return shared.NewHealthResult(name, status, message)
}

// HealthReport represents a comprehensive health report.
type HealthReport = healthcore.HealthReport

// NewHealthReport creates a new health report.
func NewHealthReport() *HealthReport {
	return healthcore.NewHealthReport()
}

// HealthCallback is a callback function for health status changes.
type HealthCallback = healthcore.HealthCallback

// HealthReportCallback is a callback function for health report changes.
type HealthReportCallback = healthcore.HealthReportCallback
