package core

import (
	"context"

	"github.com/xraph/forge/pkg/common"
)

// HealthService defines the contract for health monitoring services
type HealthService interface {
	// ServiceLifecycle Core service lifecycle methods
	ServiceLifecycle

	// HealthCheckManager Health check registration and management
	HealthCheckManager

	// HealthChecker2 Health checking operations
	HealthChecker2

	// HealthMonitor Health monitoring and reporting
	HealthMonitor

	// ServiceInfo Service information
	ServiceInfo

	GetChecker() common.HealthChecker
}

// ServiceLifecycle defines the basic service lifecycle methods
type ServiceLifecycle interface {
	// Name returns the service name
	Name() string

	// Dependencies returns the service dependencies
	Dependencies() []string

	// OnStart starts the health service
	OnStart(ctx context.Context) error

	// OnStop stops the health service
	OnStop(ctx context.Context) error

	// OnHealthCheck performs a health check on the service itself
	OnHealthCheck(ctx context.Context) error
}

// HealthCheckManager handles registration and management of health checks
type HealthCheckManager interface {
	// RegisterCheck registers a health check with the service
	RegisterCheck(name string, check HealthCheck) error

	// UnregisterCheck unregisters a health check from the service
	UnregisterCheck(name string) error

	// ListChecks returns all registered health check names
	ListChecks() []string

	// GetCheck returns a specific health check by name
	GetCheck(name string) (HealthCheck, error)
}

// HealthChecker2 defines methods for performing health checks
type HealthChecker2 interface {
	// CheckAll performs all health checks and returns a comprehensive report
	CheckAll(ctx context.Context) *HealthReport

	// CheckOne performs a single health check by name
	CheckOne(ctx context.Context, name string) *HealthResult

	// GetStatus returns the current overall health status
	GetStatus() HealthStatus

	// IsHealthy returns true if the service is healthy
	IsHealthy() bool

	// IsDegraded returns true if the service is in a degraded state
	IsDegraded() bool

	Checker() common.HealthChecker
}

// HealthMonitor defines methods for monitoring and reporting health status
type HealthMonitor interface {
	// Subscribe adds a callback for health status changes
	Subscribe(callback HealthCallback) error

	// // Unsubscribe removes a health status change callback
	// Unsubscribe(callback HealthCallback) error

	// GetLastReport returns the last health report
	GetLastReport() *HealthReport

	// GetStats returns health service statistics
	GetStats() *HealthCheckerStats

	// // GetHistory returns health check history for a specific duration
	// GetHistory(duration time.Duration) []*HealthReport
}

// ServiceInfo provides information about the health service
type ServiceInfo interface {
	// // GetServiceInfo returns detailed information about the service
	// GetServiceInfo() map[string]interface{}

	// GetVersion returns the service version
	GetVersion() string

	// GetEnvironment returns the service environment
	GetEnvironment() string

	// GetConfiguration returns the current service configuration
	GetConfiguration() *HealthServiceConfig
}

// HealthSummary provides a summary of health check results
type HealthSummary struct {
	Total       int `json:"total"`
	Healthy     int `json:"healthy"`
	Degraded    int `json:"degraded"`
	Unhealthy   int `json:"unhealthy"`
	Critical    int `json:"critical"`
	NonCritical int `json:"non_critical"`
}

// HealthServiceConfig defines configuration for the health service
type HealthServiceConfig struct {
	// Health checker configuration
	HealthChecker *HealthCheckerConfig `yaml:"health_checker" json:"health_checker"`

	// Service specific configuration
	AutoRegister      bool              `yaml:"auto_register" json:"auto_register"`
	ExposeEndpoints   bool              `yaml:"expose_endpoints" json:"expose_endpoints"`
	EndpointPrefix    string            `yaml:"endpoint_prefix" json:"endpoint_prefix"`
	EnablePersistence bool              `yaml:"enable_persistence" json:"enable_persistence"`
	EnableAlerting    bool              `yaml:"enable_alerting" json:"enable_alerting"`
	EnableMetrics     bool              `yaml:"enable_metrics" json:"enable_metrics"`
	Tags              map[string]string `yaml:"tags" json:"tags"`

	// Environment information
	Version     string `yaml:"version" json:"version"`
	Environment string `yaml:"environment" json:"environment"`
}

// HealthServiceFactory defines a factory function for creating health services
type HealthServiceFactory func(config *HealthServiceConfig) (HealthService, error)

// HealthCheckFactory defines a factory function for creating health checks
type HealthCheckFactory func(config *HealthCheckConfig) (HealthCheck, error)

// HealthServiceRegistry defines an interface for registering and discovering health services
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

// HealthServiceProvider defines a provider interface for health services
type HealthServiceProvider interface {
	// Provide returns a health service instance
	Provide(config *HealthServiceConfig) (HealthService, error)

	// ProviderName returns the name of the provider
	ProviderName() string

	// ProviderVersion returns the version of the provider
	ProviderVersion() string

	// SupportedFeatures returns the features supported by this provider
	SupportedFeatures() []string
}

// HealthServiceMiddleware defines middleware for health service operations
type HealthServiceMiddleware interface {
	// BeforeCheck is called before a health check is performed
	BeforeCheck(ctx context.Context, checkName string) context.Context

	// AfterCheck is called after a health check is performed
	AfterCheck(ctx context.Context, checkName string, result *HealthResult) *HealthResult

	// OnStatusChange is called when the overall health status changes
	OnStatusChange(ctx context.Context, oldStatus, newStatus HealthStatus)

	// OnError is called when an error occurs during health checking
	OnError(ctx context.Context, err error)
}

// HealthServiceBuilder provides a builder pattern for constructing health services
type HealthServiceBuilder interface {
	// WithConfig sets the service configuration
	WithConfig(config *HealthServiceConfig) HealthServiceBuilder

	// WithCheck adds a health check
	WithCheck(name string, check HealthCheck) HealthServiceBuilder

	// WithMiddleware adds middleware
	WithMiddleware(middleware HealthServiceMiddleware) HealthServiceBuilder

	// WithCallback adds a status change callback
	WithCallback(callback HealthCallback) HealthServiceBuilder

	// Build constructs the health service
	Build() (HealthService, error)
}

// DefaultHealthServiceConfig returns default configuration for the health service
func DefaultHealthServiceConfig() *HealthServiceConfig {
	return &HealthServiceConfig{
		HealthChecker:     DefaultHealthCheckerConfig(),
		AutoRegister:      true,
		ExposeEndpoints:   true,
		EndpointPrefix:    "/health",
		EnablePersistence: false,
		EnableAlerting:    false,
		EnableMetrics:     true,
		Tags:              make(map[string]string),
		Version:           "unknown",
		Environment:       "development",
	}
}
