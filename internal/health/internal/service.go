package internal

import (
	"github.com/xraph/forge/internal/shared"
)

// HealthService defines the contract for health monitoring services.
type HealthService = shared.HealthManager

// HealthMonitor defines methods for monitoring and reporting health status.
type HealthMonitor interface {
	// Subscribe adds a callback for health status changes
	Subscribe(callback HealthCallback) error

	// GetLastReport returns the last health report
	GetLastReport() *HealthReport

	// GetStats returns health service statistics
	GetStats() *HealthCheckerStats
}

// HealthSummary provides a summary of health check results.
type HealthSummary struct {
	Total       int `json:"total"`
	Healthy     int `json:"healthy"`
	Degraded    int `json:"degraded"`
	Unhealthy   int `json:"unhealthy"`
	Critical    int `json:"critical"`
	NonCritical int `json:"non_critical"`
}

// HealthCheckFactory defines a factory function for creating health checks.
type HealthCheckFactory func(config *HealthCheckConfig) (HealthCheck, error)

// HealthServiceRegistry defines an interface for registering and discovering health services.
type HealthServiceRegistry interface {
	// RegisterService registers a health service
	RegisterService(name string, service HealthService) error

	// UnregisterService unregisters a health service
	UnregisterService(name string) error

	// GetService retrieves a registered health service
	GetService(name string) (HealthService, error)

	// ListServices returns all registered service names
	ListServices() []string

	// GetServiceStatus returns the status of a specific service
	GetServiceStatus(name string) (HealthStatus, error)

	// GetAllStatuses returns the status of all registered services
	GetAllStatuses() map[string]HealthStatus
}
