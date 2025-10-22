package health

import (
	"context"
	"fmt"
	"time"

	healthinternal "github.com/xraph/forge/v2/internal/health/internal"
	"github.com/xraph/forge/v2/internal/logger"
	"github.com/xraph/forge/v2/internal/shared"
)

// registerBuiltinChecks registers built-in health checks
func (hc *ManagerImpl) registerBuiltinChecks() error {
	// Register basic system health checks
	if err := hc.registerSystemChecks(); err != nil {
		return fmt.Errorf("failed to register system checks: %w", err)
	}

	// Register service health checks
	if err := hc.registerServiceChecks(); err != nil {
		return fmt.Errorf("failed to register service checks: %w", err)
	}

	return nil
}

// registerSystemChecks registers basic system health checks
func (hc *ManagerImpl) registerSystemChecks() error {
	// Memory check
	memoryCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "memory",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hc.config.Tags,
	}, func(ctx context.Context) *HealthResult {
		return checkMemoryUsage(ctx)
	})

	if err := hc.Register(memoryCheck); err != nil {
		return fmt.Errorf("failed to register memory check: %w", err)
	}

	// Disk check
	diskCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "disk",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hc.config.Tags,
	}, func(ctx context.Context) *shared.HealthResult {
		return checkDiskUsage(ctx)
	})

	if err := hc.Register(diskCheck); err != nil {
		return fmt.Errorf("failed to register disk check: %w", err)
	}

	// CPU check
	cpuCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "cpu",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hc.config.Tags,
	}, func(ctx context.Context) *HealthResult {
		return checkCPUUsage(ctx)
	})

	if err := hc.Register(cpuCheck); err != nil {
		return fmt.Errorf("failed to register CPU check: %w", err)
	}

	return nil
}

// registerServiceChecks registers health checks for framework services
func (hc *ManagerImpl) registerServiceChecks() error {
	if hc.container == nil {
		return nil
	}

	// Get all registered services
	services := hc.container.Services()

	for _, serviceName := range services {
		// Skip self and health checker
		if serviceName == "health-service" || serviceName == "health-checker" {
			continue
		}

		// Check if service is marked as critical
		critical := false
		for _, criticalService := range hc.config.CriticalServices {
			if criticalService == serviceName {
				critical = true
				break
			}
		}

		// Create service health check
		serviceCheck := NewSimpleHealthCheck(&HealthCheckConfig{
			Name:     serviceName,
			Timeout:  hc.config.DefaultTimeout,
			Critical: critical,
			Tags:     hc.config.Tags,
		}, func(ctx context.Context) *HealthResult {
			return hc.checkServiceHealth(ctx, serviceName)
		})

		if err := hc.Register(serviceCheck); err != nil {
			if hc.logger != nil {
				hc.logger.Warn("failed to register service health check",
					logger.String("service", serviceName),
					logger.Error(err),
				)
			}
			continue
		}

		if hc.logger != nil {
			hc.logger.Debug("registered service health check",
				logger.String("service", serviceName),
				logger.Bool("critical", critical),
			)
		}
	}

	return nil
}

// checkServiceHealth checks the health of a specific service
func (hc *ManagerImpl) checkServiceHealth(ctx context.Context, serviceName string) *HealthResult {
	// Try to resolve the service from the container
	service, err := hc.container.Resolve(serviceName)
	if err != nil {
		return healthinternal.NewHealthResult(serviceName, healthinternal.HealthStatusUnhealthy, "failed to resolve service").
			WithError(err).
			WithDetail("service_name", serviceName)
	}

	// Check if service implements the OnHealthCheck method
	if healthCheckable, ok := service.(interface{ OnHealthCheck(context.Context) error }); ok {
		if err := healthCheckable.OnHealthCheck(ctx); err != nil {
			return healthinternal.NewHealthResult(serviceName, healthinternal.HealthStatusUnhealthy, "service health check failed").
				WithError(err).
				WithDetail("service_name", serviceName)
		}
	}

	return healthinternal.NewHealthResult(serviceName, healthinternal.HealthStatusHealthy, "service is healthy").
		WithDetail("service_name", serviceName)
}

// registerEndpoints registers health endpoints with the router
func (hc *ManagerImpl) registerEndpoints() error {
	// This would typically register endpoints with the router
	// For now, we'll just log that endpoints would be registered
	if hc.logger != nil {
		hc.logger.Info("health endpoints would be registered",
			logger.String("prefix", hc.config.EndpointPrefix),
		)
	}

	// TODO: Implement endpoint registration when router integration is available
	// This would integrate with the router from Phase 1

	return nil
}
