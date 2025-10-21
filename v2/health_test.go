package forge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	_ "github.com/stretchr/testify/require" // Avoid unused import error
)

func TestDefaultHealthConfig(t *testing.T) {
	config := DefaultHealthConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, "/_/health", config.HealthPath)
	assert.Equal(t, "/_/health/live", config.LivenessPath)
	assert.Equal(t, "/_/health/ready", config.ReadinessPath)
	assert.Equal(t, 5*time.Second, config.HealthCheckTimeout)
}

func TestHealthConfig_Custom(t *testing.T) {
	config := HealthConfig{
		Enabled:            false,
		HealthPath:         "/custom/health",
		LivenessPath:       "/custom/live",
		ReadinessPath:      "/custom/ready",
		HealthCheckTimeout: 10 * time.Second,
	}

	assert.False(t, config.Enabled)
	assert.Equal(t, "/custom/health", config.HealthPath)
	assert.Equal(t, "/custom/live", config.LivenessPath)
	assert.Equal(t, "/custom/ready", config.ReadinessPath)
	assert.Equal(t, 10*time.Second, config.HealthCheckTimeout)
}

func TestNewHealthManager(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)
	assert.NotNil(t, manager)
}

func TestHealthManager_Register(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	check := func(ctx context.Context) HealthResult {
		return HealthResult{
			Status:  HealthStatusHealthy,
			Message: "test service is healthy",
		}
	}

	manager.Register("test", check)

	// Verify check is registered
	report := manager.Check(context.Background())
	assert.NotNil(t, report.Checks["test"])
}

func TestHealthManager_Check_Healthy(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	manager.Register("service1", func(ctx context.Context) HealthResult {
		return HealthResult{
			Status:  HealthStatusHealthy,
			Message: "service1 is healthy",
		}
	})

	manager.Register("service2", func(ctx context.Context) HealthResult {
		return HealthResult{
			Status:  HealthStatusHealthy,
			Message: "service2 is healthy",
		}
	})

	report := manager.Check(context.Background())

	assert.Equal(t, HealthStatusHealthy, report.Status)
	assert.Len(t, report.Checks, 2)
	assert.Equal(t, HealthStatusHealthy, report.Checks["service1"].Status)
	assert.Equal(t, HealthStatusHealthy, report.Checks["service2"].Status)
}

func TestHealthManager_Check_Degraded(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	manager.Register("service1", func(ctx context.Context) HealthResult {
		return HealthResult{
			Status:  HealthStatusHealthy,
			Message: "service1 is healthy",
		}
	})

	manager.Register("service2", func(ctx context.Context) HealthResult {
		return HealthResult{
			Status:  HealthStatusDegraded,
			Message: "service2 is degraded",
		}
	})

	report := manager.Check(context.Background())

	assert.Equal(t, HealthStatusDegraded, report.Status)
	assert.Len(t, report.Checks, 2)
}

func TestHealthManager_Check_Unhealthy(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	manager.Register("service1", func(ctx context.Context) HealthResult {
		return HealthResult{
			Status:  HealthStatusHealthy,
			Message: "service1 is healthy",
		}
	})

	manager.Register("service2", func(ctx context.Context) HealthResult {
		return HealthResult{
			Status:  HealthStatusUnhealthy,
			Message: "service2 is unhealthy",
		}
	})

	report := manager.Check(context.Background())

	assert.Equal(t, HealthStatusUnhealthy, report.Status)
	assert.Len(t, report.Checks, 2)
}

func TestHealthManager_Check_WithDetails(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	manager.Register("database", func(ctx context.Context) HealthResult {
		return HealthResult{
			Status:  HealthStatusHealthy,
			Message: "database is healthy",
			Details: map[string]any{
				"connections":     10,
				"max_connections": 100,
			},
		}
	})

	report := manager.Check(context.Background())

	assert.Equal(t, HealthStatusHealthy, report.Status)
	assert.NotNil(t, report.Checks["database"].Details)
	assert.Equal(t, 10, report.Checks["database"].Details["connections"])
}

func TestHealthManager_Check_Timeout(t *testing.T) {
	manager := NewHealthManager(100 * time.Millisecond)

	manager.Register("slow", func(ctx context.Context) HealthResult {
		select {
		case <-time.After(200 * time.Millisecond):
			return HealthResult{
				Status:  HealthStatusHealthy,
				Message: "completed",
			}
		case <-ctx.Done():
			return HealthResult{
				Status:  HealthStatusUnhealthy,
				Message: "check timed out",
			}
		}
	})

	report := manager.Check(context.Background())

	// Check should timeout and mark as unhealthy
	assert.Equal(t, HealthStatusUnhealthy, report.Status)
}

func TestHealthManager_Check_Parallel(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	// Register multiple checks
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("service%d", i)
		manager.Register(name, func(ctx context.Context) HealthResult {
			// Simulate some work
			time.Sleep(10 * time.Millisecond)
			return HealthResult{
				Status:  HealthStatusHealthy,
				Message: "healthy",
			}
		})
	}

	start := time.Now()
	report := manager.Check(context.Background())
	duration := time.Since(start)

	// Should complete in parallel, not serially
	// 10 checks * 10ms each = 100ms serial, but < 50ms parallel
	assert.Less(t, duration, 50*time.Millisecond)
	assert.Len(t, report.Checks, 10)
	assert.Equal(t, HealthStatusHealthy, report.Status)
}

func TestHealthManager_Check_EmptyChecks(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	report := manager.Check(context.Background())

	assert.Equal(t, HealthStatusHealthy, report.Status)
	assert.Empty(t, report.Checks)
	assert.NotZero(t, report.Timestamp)
	assert.NotZero(t, report.Duration)
}

func TestHealthResult_Status(t *testing.T) {
	result := HealthResult{
		Status:  HealthStatusHealthy,
		Message: "all good",
		Details: map[string]any{"key": "value"},
	}

	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.Equal(t, "all good", result.Message)
	assert.Equal(t, "value", result.Details["key"])
}

func TestHealthStatus_Values(t *testing.T) {
	assert.Equal(t, HealthStatus("healthy"), HealthStatusHealthy)
	assert.Equal(t, HealthStatus("degraded"), HealthStatusDegraded)
	assert.Equal(t, HealthStatus("unhealthy"), HealthStatusUnhealthy)
}

func TestHealthReport_Fields(t *testing.T) {
	report := HealthReport{
		Status:    HealthStatusHealthy,
		Timestamp: time.Now(),
		Duration:  100 * time.Millisecond,
		Checks: map[string]HealthResult{
			"test": {
				Status:  HealthStatusHealthy,
				Message: "OK",
			},
		},
	}

	assert.Equal(t, HealthStatusHealthy, report.Status)
	assert.NotZero(t, report.Timestamp)
	assert.Equal(t, 100*time.Millisecond, report.Duration)
	assert.Len(t, report.Checks, 1)
}

func TestHealthManager_Check_MixedStatus(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	manager.Register("healthy", func(ctx context.Context) HealthResult {
		return HealthResult{Status: HealthStatusHealthy}
	})

	manager.Register("degraded", func(ctx context.Context) HealthResult {
		return HealthResult{Status: HealthStatusDegraded}
	})

	manager.Register("unhealthy", func(ctx context.Context) HealthResult {
		return HealthResult{Status: HealthStatusUnhealthy}
	})

	report := manager.Check(context.Background())

	// Unhealthy takes priority
	assert.Equal(t, HealthStatusUnhealthy, report.Status)
}

func TestHealthManager_Check_ContextCancellation(t *testing.T) {
	manager := NewHealthManager(5 * time.Second)

	manager.Register("test", func(ctx context.Context) HealthResult {
		select {
		case <-ctx.Done():
			return HealthResult{
				Status:  HealthStatusUnhealthy,
				Message: "context cancelled",
			}
		case <-time.After(1 * time.Second):
			return HealthResult{
				Status:  HealthStatusHealthy,
				Message: "completed",
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	report := manager.Check(ctx)

	assert.NotNil(t, report)
}
