package health

import (
	"fmt"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// registerBuiltinChecks registers the framework's own health checks.
//
// This used to also register one check per container service, duplicating
// autoDiscoverServices, which had already registered exactly the same set.
// Register rejects a duplicate name, so the second pass built a check per
// service only to log a warning for each one. Per-service checks now come from
// autoDiscoverServices alone, which the AutoDiscovery feature gates.
func (hc *ManagerImpl) registerBuiltinChecks() error {
	if err := hc.registerSystemChecks(); err != nil {
		return fmt.Errorf("failed to register system checks: %w", err)
	}

	return nil
}

// registerSystemChecks registers basic system health checks.
func (hc *ManagerImpl) registerSystemChecks() error {
	// Memory check
	memoryCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "memory",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hc.config.Tags,
	}, checkMemoryUsage)

	if err := hc.Register(memoryCheck); err != nil {
		return fmt.Errorf("failed to register memory check: %w", err)
	}

	// Disk check
	diskCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "disk",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hc.config.Tags,
	}, checkDiskUsage)

	if err := hc.Register(diskCheck); err != nil {
		return fmt.Errorf("failed to register disk check: %w", err)
	}

	// CPU check
	cpuCheck := NewSimpleHealthCheck(&HealthCheckConfig{
		Name:     "cpu",
		Timeout:  2 * time.Second,
		Critical: false,
		Tags:     hc.config.Tags,
	}, checkCPUUsage)

	if err := hc.Register(cpuCheck); err != nil {
		return fmt.Errorf("failed to register CPU check: %w", err)
	}

	return nil
}

// registerEndpoints registers health endpoints with the router.
func (hc *ManagerImpl) registerEndpoints() error {
	// This would typically register endpoints with the router
	// For now, we'll just log that endpoints would be registered
	if hc.logger != nil {
		hc.logger.Debug("health endpoints would be registered",
			logger.String("prefix", hc.config.Endpoints.Prefix),
		)
	}

	// TODO: Implement endpoint registration when router integration is available
	// This would integrate with the router from Phase 1

	return nil
}
