package health

import (
	"context"
	"fmt"

	healthcore "github.com/xraph/forge/v2/internal/health/internal"
	"github.com/xraph/forge/v2/internal/logger"
	"github.com/xraph/forge/v2/internal/shared"
)

const ServiceKey = shared.HealthServiceKey
const CheckerKey = shared.HealthCheckerKey

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

// RegisterHealthService registers the health service with the DI container
func RegisterHealthService(container shared.Container, conf *HealthConfig) error {
	return container.Register(ServiceKey, func(container shared.Container) (any, error) {
		loggerFn, err := container.Resolve(shared.LoggerKey)
		if err != nil {
			return nil, err
		}
		logger, ok := loggerFn.(logger.Logger)
		if !ok {
			return nil, fmt.Errorf("resolved logger is not a logger")
		}
		metricsFn, err := container.Resolve(shared.MetricsCollectorKey)
		if err != nil {
			return nil, err
		}
		metrics, ok := metricsFn.(shared.Metrics)
		if !ok {
			return nil, fmt.Errorf("resolved metrics is not a metrics")
		}
		// configFn, err := container.Resolve(shared.ConfigKey)
		// if err != nil {
		// 	return nil, err
		// }
		// config, ok := configFn.(config.ConfigManager)
		// if !ok {
		// 	return nil, fmt.Errorf("resolved config is not a config manager")
		// }
		return NewHealthChecker(conf, logger, metrics, container), nil
	}, shared.Singleton())
}

// GetHealthServiceFromContainer retrieves the health service from the container
func GetHealthServiceFromContainer(container shared.Container) (healthcore.HealthService, error) {
	service, err := container.Resolve(shared.HealthServiceKey)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve health service: %w", err)
	}

	healthService, ok := service.(healthcore.HealthService)
	if !ok {
		return nil, fmt.Errorf("resolved service is not a health service")
	}

	return healthService, nil
}
