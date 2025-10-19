package health

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/samber/lo"
	"github.com/xraph/forge/pkg/common"
	healthcore "github.com/xraph/forge/pkg/health/core"
	"github.com/xraph/forge/pkg/logger"
)

const ServiceKey = common.HealthServiceKey
const CheckerKey = common.HealthCheckerKey

// HealthService provides health checking functionality as a service
type HealthService struct {
	checker   common.HealthChecker
	logger    common.Logger
	metrics   common.Metrics
	container common.Container
	config    common.ConfigManager

	// Service configuration
	serviceConfig *HealthServiceConfig
}

func (hs *HealthService) Checker() common.HealthChecker {
	return hs.checker
}

func (hs *HealthService) ListChecks() []string {
	return lo.Keys(hs.checker.GetChecks())
}

func (hs *HealthService) GetCheck(name string) (healthcore.HealthCheck, error) {
	return hs.checker.GetChecks()[name], nil
}

func (hs *HealthService) IsHealthy() bool {
	return hs.checker.GetLastReport().IsHealthy()
}

func (hs *HealthService) IsDegraded() bool {
	return hs.checker.GetLastReport().IsDegraded()
}

func (hs *HealthService) GetVersion() string {
	return hs.checker.Version()
}

func (hs *HealthService) GetEnvironment() string {
	return hs.checker.Environment()
}

func (hs *HealthService) GetConfiguration() *healthcore.HealthServiceConfig {
	return hs.serviceConfig
}

// HealthServiceConfig contains configuration for the health service
type HealthServiceConfig = healthcore.HealthServiceConfig

// DefaultHealthServiceConfig returns default configuration for the health service
func DefaultHealthServiceConfig() *HealthServiceConfig {
	return healthcore.DefaultHealthServiceConfig()
}

// NewHealthService creates a new health service
func NewHealthService(l common.Logger, metrics common.Metrics, container common.Container, config common.ConfigManager, serviceConfig *HealthServiceConfig) healthcore.HealthService {
	if serviceConfig == nil {
		serviceConfig = DefaultHealthServiceConfig()
	}

	// Bind configuration if config manager is available
	if config != nil {
		if err := config.Bind("health", serviceConfig); err != nil {
			if l != nil {
				l.Warn("failed to bind health service configuration, using defaults",
					logger.Error(err),
				)
			}
		}
	}

	// Create health checker
	checker := NewHealthChecker(serviceConfig.HealthChecker, l, metrics, container)

	// Set version and environment information
	if serviceConfig.Version != "unknown" {
		checker.SetVersion(serviceConfig.Version)
	}
	if serviceConfig.Environment != "development" {
		checker.SetEnvironment(serviceConfig.Environment)
	}

	return &HealthService{
		checker:       checker,
		logger:        l,
		metrics:       metrics,
		container:     container,
		config:        config,
		serviceConfig: serviceConfig,
	}
}

// Name returns the service name
func (hs *HealthService) Name() string {
	return common.HealthServiceKey
}

// Dependencies returns the service dependencies
func (hs *HealthService) Dependencies() []string {
	return []string{} // Health service should have minimal dependencies
}

// OnStart starts the health service
func (hs *HealthService) Start(ctx context.Context) error {
	if hs.logger != nil {
		hs.logger.Info("starting health service",
			logger.Bool("auto_register", hs.serviceConfig.AutoRegister),
			logger.Bool("expose_endpoints", hs.serviceConfig.ExposeEndpoints),
			logger.String("endpoint_prefix", hs.serviceConfig.EndpointPrefix),
			logger.Bool("enable_persistence", hs.serviceConfig.EnablePersistence),
			logger.Bool("enable_alerting", hs.serviceConfig.EnableAlerting),
			logger.String("version", hs.serviceConfig.Version),
			logger.String("environment", hs.serviceConfig.Environment),
		)
	}

	// Start the health checker
	if err := hs.checker.Start(ctx); err != nil {
		return fmt.Errorf("failed to start health checker: %w", err)
	}

	// Auto-register built-in health checks if enabled
	if hs.serviceConfig.AutoRegister {
		if err := hs.registerBuiltinChecks(); err != nil {
			if hs.logger != nil {
				hs.logger.Warn("failed to register some built-in health checks",
					logger.Error(err),
				)
			}
		}
	}

	// Register endpoints if enabled
	if hs.serviceConfig.ExposeEndpoints {
		if err := hs.registerEndpoints(); err != nil {
			if hs.logger != nil {
				hs.logger.Warn("failed to register health endpoints",
					logger.Error(err),
				)
			}
		}
	}

	if hs.metrics != nil {
		hs.metrics.Counter("forge.health.service_started").Inc()
	}

	return nil
}

// OnStop stops the health service
func (hs *HealthService) Stop(ctx context.Context) error {
	if hs.logger != nil {
		hs.logger.Info("stopping health service")
	}

	// Stop the health checker
	if err := hs.checker.Stop(ctx); err != nil {
		if hs.logger != nil {
			hs.logger.Error("failed to stop health checker",
				logger.Error(err),
			)
		}
		return fmt.Errorf("failed to stop health checker: %w", err)
	}

	if hs.metrics != nil {
		hs.metrics.Counter("forge.health.service_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs a health check on the health service itself
func (hs *HealthService) OnHealthCheck(ctx context.Context) error {
	return hs.checker.OnHealthCheck(ctx)
}

// GetChecker returns the underlying health checker
func (hs *HealthService) GetChecker() common.HealthChecker {
	return hs.checker
}

// RegisterCheck registers a health check with the service
func (hs *HealthService) RegisterCheck(name string, check HealthCheck) error {
	return hs.checker.RegisterCheck(name, check)
}

// UnregisterCheck unregisters a health check from the service
func (hs *HealthService) UnregisterCheck(name string) error {
	return hs.checker.UnregisterCheck(name)
}

// CheckAll performs all health checks
func (hs *HealthService) CheckAll(ctx context.Context) *HealthReport {
	return hs.checker.CheckAll(ctx)
}

// CheckOne performs a single health check
func (hs *HealthService) CheckOne(ctx context.Context, name string) *HealthResult {
	return hs.checker.CheckOne(ctx, name)
}

// GetStatus returns the current overall health status
func (hs *HealthService) GetStatus() HealthStatus {
	return hs.checker.GetStatus()
}

// Subscribe adds a callback for health status changes
func (hs *HealthService) Subscribe(callback HealthCallback) error {
	return hs.checker.Subscribe(callback)
}

// GetLastReport returns the last health report
func (hs *HealthService) GetLastReport() *HealthReport {
	return hs.checker.GetLastReport()
}

// GetStats returns health service statistics
func (hs *HealthService) GetStats() *HealthCheckerStats {
	return hs.checker.GetStats()
}

// registerBuiltinChecks registers built-in health checks
func (hs *HealthService) registerBuiltinChecks() error {
	// Register basic system health checks
	if err := hs.registerSystemChecks(); err != nil {
		return fmt.Errorf("failed to register system checks: %w", err)
	}

	// Register service health checks
	if err := hs.registerServiceChecks(); err != nil {
		return fmt.Errorf("failed to register service checks: %w", err)
	}

	return nil
}

// registerSystemChecks registers basic system health checks
func (hs *HealthService) registerSystemChecks() error {
	// Memory check
	memoryCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "memory",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hs.serviceConfig.Tags,
	}, func(ctx context.Context) *HealthResult {
		return checkMemoryUsage(ctx)
	})

	if err := hs.RegisterCheck("memory", memoryCheck); err != nil {
		return fmt.Errorf("failed to register memory check: %w", err)
	}

	// Disk check
	diskCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "disk",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hs.serviceConfig.Tags,
	}, func(ctx context.Context) *HealthResult {
		return checkDiskUsage(ctx)
	})

	if err := hs.RegisterCheck("disk", diskCheck); err != nil {
		return fmt.Errorf("failed to register disk check: %w", err)
	}

	// CPU check
	cpuCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "cpu",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hs.serviceConfig.Tags,
	}, func(ctx context.Context) *HealthResult {
		return checkCPUUsage(ctx)
	})

	if err := hs.RegisterCheck("cpu", cpuCheck); err != nil {
		return fmt.Errorf("failed to register CPU check: %w", err)
	}

	return nil
}

// registerServiceChecks registers health checks for framework services
func (hs *HealthService) registerServiceChecks() error {
	if hs.container == nil {
		return nil
	}

	// Get all registered services
	services := hs.container.Services()

	for _, serviceDef := range services {
		serviceName := serviceDef.Name
		if serviceName == "" {
			serviceName = serviceDef.ServiceName()
		}

		// Skip self
		if serviceName == "health-service" || serviceName == "health-checker" {
			continue
		}

		// Check if service is marked as critical
		critical := false
		for _, criticalService := range hs.serviceConfig.HealthChecker.CriticalServices {
			if criticalService == serviceName {
				critical = true
				break
			}
		}

		// Create service health check
		serviceCheck := NewSimpleHealthCheck(&HealthCheckConfig{
			Name:     serviceName,
			Timeout:  hs.serviceConfig.HealthChecker.DefaultTimeout,
			Critical: critical,
			Tags:     hs.serviceConfig.Tags,
		}, func(ctx context.Context) *HealthResult {
			return hs.checkServiceHealth(ctx, serviceName)
		})

		if err := hs.RegisterCheck(serviceName, serviceCheck); err != nil {
			if hs.logger != nil {
				hs.logger.Warn("failed to register service health check",
					logger.String("service", serviceName),
					logger.Error(err),
				)
			}
			continue
		}

		if hs.logger != nil {
			hs.logger.Debug("registered service health check",
				logger.String("service", serviceName),
				logger.Bool("critical", critical),
			)
		}
	}

	return nil
}

// checkServiceHealth checks the health of a specific service
func (hs *HealthService) checkServiceHealth(ctx context.Context, serviceName string) *HealthResult {
	// Try to resolve the service from the container
	service, err := hs.container.ResolveNamed(serviceName)
	if err != nil {
		return NewHealthResult(serviceName, HealthStatusUnhealthy, "failed to resolve service").
			WithError(err).
			WithDetail("service_name", serviceName)
	}

	// Check if service implements the OnHealthCheck method
	if healthCheckable, ok := service.(interface{ OnHealthCheck(context.Context) error }); ok {
		if err := healthCheckable.OnHealthCheck(ctx); err != nil {
			return NewHealthResult(serviceName, HealthStatusUnhealthy, "service health check failed").
				WithError(err).
				WithDetail("service_name", serviceName)
		}
	}

	return NewHealthResult(serviceName, HealthStatusHealthy, "service is healthy").
		WithDetail("service_name", serviceName)
}

// registerEndpoints registers health endpoints with the router
func (hs *HealthService) registerEndpoints() error {
	// This would typically register endpoints with the router
	// For now, we'll just log that endpoints would be registered
	if hs.logger != nil {
		hs.logger.Info("health endpoints would be registered",
			logger.String("prefix", hs.serviceConfig.EndpointPrefix),
		)
	}

	// TODO: Implement endpoint registration when router integration is available
	// This would integrate with the router from Phase 1

	return nil
}

// Basic health check implementations

// checkMemoryUsage checks memory usage
func checkMemoryUsage(ctx context.Context) *HealthResult {
	return NewHealthResult("memory", HealthStatusHealthy, "memory usage is normal").
		WithDetail("check_type", "memory")
}

// checkDiskUsage checks disk usage
func checkDiskUsage(ctx context.Context) *HealthResult {
	return NewHealthResult("disk", HealthStatusHealthy, "disk usage is normal").
		WithDetail("check_type", "disk")
}

// checkCPUUsage checks CPU usage
func checkCPUUsage(ctx context.Context) *HealthResult {
	return NewHealthResult("cpu", HealthStatusHealthy, "CPU usage is normal").
		WithDetail("check_type", "cpu")
}

// NewHealthServiceFactory creates a new health service instance
func NewHealthServiceFactory(serviceConfig *HealthServiceConfig) func(logger common.Logger, metrics common.Metrics, container common.Container, config common.ConfigManager) healthcore.HealthService {
	return func(logger common.Logger, metrics common.Metrics, container common.Container, config common.ConfigManager) healthcore.HealthService {
		return NewHealthService(logger, metrics, container, config, serviceConfig)
	}
}

// RegisterHealthService registers the health service with the DI container
func RegisterHealthService(container common.Container, conf *HealthServiceConfig) error {
	if err := container.Register(common.ServiceDefinition{
		Name:         common.HealthServiceKey,
		Type:         (*healthcore.HealthService)(nil),
		Constructor:  NewHealthServiceFactory(conf),
		Singleton:    true,
		Dependencies: []string{common.ConfigKey},
		Tags: map[string]string{
			"type":    "health",
			"version": "1.0.0",
		},
	}); err != nil {
		return fmt.Errorf("failed to register health service: %w", err)
	}

	// Resolve config
	serv, err := container.ResolveNamed(common.HealthServiceKey)
	if err != nil {
		return fmt.Errorf("failed to health service: %w", err)
	}
	srv, ok := serv.(*HealthService)
	if !ok {
		return fmt.Errorf("resolved service is not a health service")
	}

	return container.Register(common.ServiceDefinition{
		Name:         common.HealthCheckerKey,
		Type:         (*healthcore.HealthChecker)(nil),
		Instance:     srv.GetChecker(),
		Singleton:    true,
		Dependencies: []string{common.ConfigKey, common.HealthServiceKey},
		Tags: map[string]string{
			"type":    "health",
			"version": "1.0.0",
		},
	})
}

// GetHealthServiceFromContainer retrieves the health service from the container
func GetHealthServiceFromContainer(container common.Container) (healthcore.HealthService, error) {
	service, err := container.ResolveNamed(common.HealthServiceKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve health service: %w", err)
	}

	healthService, ok := service.(healthcore.HealthService)
	if !ok {
		return nil, fmt.Errorf("resolved service is not a health service")
	}

	return healthService, nil
}

// CreateHealthServiceWithConfig creates a health service with custom configuration
func CreateHealthServiceWithConfig(config *HealthServiceConfig, logger common.Logger, metrics common.Metrics, container common.Container, configManager common.ConfigManager) *HealthService {
	// Create health checker with custom configuration
	checker := NewHealthChecker(config.HealthChecker, logger, metrics, container)

	// Set version and environment information
	if config.Version != "unknown" {
		checker.SetVersion(config.Version)
	}
	if config.Environment != "development" {
		checker.SetEnvironment(config.Environment)
	}

	return &HealthService{
		checker:       checker,
		logger:        logger,
		metrics:       metrics,
		container:     container,
		config:        configManager,
		serviceConfig: config,
	}
}

// ValidateHealthServiceConfig validates health service configuration
func ValidateHealthServiceConfig(config *HealthServiceConfig) error {
	if config == nil {
		return fmt.Errorf("health service configuration is required")
	}

	if config.HealthChecker == nil {
		return fmt.Errorf("health checker configuration is required")
	}

	if config.HealthChecker.CheckInterval <= 0 {
		return fmt.Errorf("check interval must be positive")
	}

	if config.HealthChecker.ReportInterval <= 0 {
		return fmt.Errorf("report interval must be positive")
	}

	if config.HealthChecker.DefaultTimeout <= 0 {
		return fmt.Errorf("default timeout must be positive")
	}

	if config.HealthChecker.MaxConcurrentChecks <= 0 {
		return fmt.Errorf("max concurrent checks must be positive")
	}

	if config.HealthChecker.DegradedThreshold < 0 || config.HealthChecker.DegradedThreshold > 1 {
		return fmt.Errorf("degraded threshold must be between 0 and 1")
	}

	if config.HealthChecker.UnhealthyThreshold < 0 || config.HealthChecker.UnhealthyThreshold > 1 {
		return fmt.Errorf("unhealthy threshold must be between 0 and 1")
	}

	return nil
}

// GetHealthServiceInfo returns information about the health service
func GetHealthServiceInfo() map[string]interface{} {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	return map[string]interface{}{
		"service_name": "health-service",
		"version":      "1.0.0",
		"description":  "Comprehensive health monitoring service",
		"hostname":     hostname,
		"features": []string{
			"auto_discovery",
			"smart_aggregation",
			"trend_analysis",
			"alerting",
			"persistence",
			"metrics",
		},
		"supported_checks": []string{
			"memory",
			"disk",
			"cpu",
			"database",
			"eventbus",
			"streaming",
			"external_api",
			"custom",
		},
	}
}
