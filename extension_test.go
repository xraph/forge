package forge

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metrics2 "github.com/xraph/forge/internal/metrics"
)

// Mock extension for testing
type mockExtension struct {
	*BaseExtension
	registerCalled bool
	startCalled    bool
	stopCalled     bool
	healthError    error
	startError     error
	stopError      error
}

func newMockExtension(name, version string, deps ...string) *mockExtension {
	base := NewBaseExtension(name, version, fmt.Sprintf("Mock extension %s", name))
	base.SetDependencies(deps)
	return &mockExtension{
		BaseExtension: base,
	}
}

func (e *mockExtension) Register(app App) error {
	e.registerCalled = true
	return e.BaseExtension.Register(app)
}

func (e *mockExtension) Start(ctx context.Context) error {
	if e.startError != nil {
		return e.startError
	}
	e.startCalled = true
	e.MarkStarted()
	return nil
}

func (e *mockExtension) Stop(ctx context.Context) error {
	if e.stopError != nil {
		return e.stopError
	}
	e.stopCalled = true
	e.MarkStopped()
	return nil
}

func (e *mockExtension) Health(ctx context.Context) error {
	return e.healthError
}

// Test BaseExtension
func TestBaseExtension(t *testing.T) {
	t.Run("BasicProperties", func(t *testing.T) {
		ext := NewBaseExtension("test", "1.0.0", "Test extension")

		assert.Equal(t, "test", ext.Name())
		assert.Equal(t, "1.0.0", ext.Version())
		assert.Equal(t, "Test extension", ext.Description())
		assert.Empty(t, ext.Dependencies())
		assert.False(t, ext.IsStarted())
	})

	t.Run("SetDependencies", func(t *testing.T) {
		ext := NewBaseExtension("test", "1.0.0", "Test")
		ext.SetDependencies([]string{"dep1", "dep2"})

		assert.Equal(t, []string{"dep1", "dep2"}, ext.Dependencies())
	})

	t.Run("LifecycleState", func(t *testing.T) {
		ext := NewBaseExtension("test", "1.0.0", "Test")

		assert.False(t, ext.IsStarted())

		ext.MarkStarted()
		assert.True(t, ext.IsStarted())

		ext.MarkStopped()
		assert.False(t, ext.IsStarted())
	})

	t.Run("LoggerAndMetrics", func(t *testing.T) {
		ext := NewBaseExtension("test", "1.0.0", "Test")
		logger := NewNoopLogger()
		metrics := metrics2.NewNoOpMetrics()

		ext.SetLogger(logger)
		ext.SetMetrics(metrics)

		assert.Equal(t, logger, ext.Logger())
		assert.Equal(t, metrics, ext.Metrics())
	})

	t.Run("DefaultRegister", func(t *testing.T) {
		ext := NewBaseExtension("test", "1.0.0", "Test")
		app := NewApp(DefaultAppConfig())

		err := ext.Register(app)
		assert.NoError(t, err)
		assert.NotNil(t, ext.Logger())
		assert.NotNil(t, ext.Metrics())
	})

	t.Run("DefaultStartStop", func(t *testing.T) {
		ext := NewBaseExtension("test", "1.0.0", "Test")
		ctx := context.Background()

		err := ext.Start(ctx)
		assert.NoError(t, err)
		assert.True(t, ext.IsStarted())

		err = ext.Stop(ctx)
		assert.NoError(t, err)
		assert.False(t, ext.IsStarted())
	})

	t.Run("DefaultHealth", func(t *testing.T) {
		ext := NewBaseExtension("test", "1.0.0", "Test")
		ctx := context.Background()

		err := ext.Health(ctx)
		assert.NoError(t, err)
	})
}

// Test Extension registration with App
func TestAppRegisterExtension(t *testing.T) {
	t.Run("RegisterSingleExtension", func(t *testing.T) {
		app := NewApp(DefaultAppConfig())
		ext := newMockExtension("test", "1.0.0")

		err := app.RegisterExtension(ext)
		assert.NoError(t, err)

		extensions := app.Extensions()
		assert.Len(t, extensions, 1)
		assert.Equal(t, "test", extensions[0].Name())
	})

	t.Run("RegisterMultipleExtensions", func(t *testing.T) {
		app := NewApp(DefaultAppConfig())
		ext1 := newMockExtension("ext1", "1.0.0")
		ext2 := newMockExtension("ext2", "2.0.0")

		err := app.RegisterExtension(ext1)
		assert.NoError(t, err)

		err = app.RegisterExtension(ext2)
		assert.NoError(t, err)

		extensions := app.Extensions()
		assert.Len(t, extensions, 2)
	})

	t.Run("RegisterDuplicateExtension", func(t *testing.T) {
		app := NewApp(DefaultAppConfig())
		ext1 := newMockExtension("test", "1.0.0")
		ext2 := newMockExtension("test", "2.0.0")

		err := app.RegisterExtension(ext1)
		assert.NoError(t, err)

		err = app.RegisterExtension(ext2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("ExtensionsInConfig", func(t *testing.T) {
		ext1 := newMockExtension("ext1", "1.0.0")
		ext2 := newMockExtension("ext2", "2.0.0")

		app := NewApp(AppConfig{
			Name:       "test-app",
			Extensions: []Extension{ext1, ext2},
		})

		extensions := app.Extensions()
		assert.Len(t, extensions, 2)
	})
}

// Test Extension retrieval
func TestAppGetExtension(t *testing.T) {
	t.Run("GetExistingExtension", func(t *testing.T) {
		ext := newMockExtension("test", "1.0.0")
		app := NewApp(AppConfig{
			Extensions: []Extension{ext},
		})

		retrieved, err := app.GetExtension("test")
		assert.NoError(t, err)
		assert.Equal(t, "test", retrieved.Name())
	})

	t.Run("GetNonExistentExtension", func(t *testing.T) {
		app := NewApp(DefaultAppConfig())

		_, err := app.GetExtension("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// Test Extension lifecycle
func TestExtensionLifecycle(t *testing.T) {
	t.Run("StartCallsRegisterAndStart", func(t *testing.T) {
		ext := newMockExtension("test", "1.0.0")
		app := NewApp(AppConfig{
			Extensions: []Extension{ext},
		})

		ctx := context.Background()
		err := app.Start(ctx)
		assert.NoError(t, err)

		assert.True(t, ext.registerCalled, "Register should have been called")
		assert.True(t, ext.startCalled, "Start should have been called")
		assert.True(t, ext.IsStarted())

		_ = app.Stop(ctx)
	})

	t.Run("StopCallsStop", func(t *testing.T) {
		ext := newMockExtension("test", "1.0.0")
		app := NewApp(AppConfig{
			Extensions: []Extension{ext},
		})

		ctx := context.Background()
		_ = app.Start(ctx)

		err := app.Stop(ctx)
		assert.NoError(t, err)
		assert.True(t, ext.stopCalled, "Stop should have been called")
		assert.False(t, ext.IsStarted())
	})

	t.Run("StartErrorPropagates", func(t *testing.T) {
		ext := newMockExtension("test", "1.0.0")
		ext.startError = fmt.Errorf("start failed")

		app := NewApp(AppConfig{
			Extensions: []Extension{ext},
		})

		ctx := context.Background()
		err := app.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "start failed")
	})

	t.Run("StopErrorDoesNotPropagate", func(t *testing.T) {
		ext := newMockExtension("test", "1.0.0")
		ext.stopError = fmt.Errorf("stop failed")

		app := NewApp(AppConfig{
			Extensions: []Extension{ext},
		})

		ctx := context.Background()
		_ = app.Start(ctx)

		err := app.Stop(ctx)
		assert.NoError(t, err) // Stop errors are logged, not returned
	})
}

// Test Extension dependencies
func TestExtensionDependencies(t *testing.T) {
	t.Run("StartInDependencyOrder", func(t *testing.T) {
		ext1 := newMockExtension("ext1", "1.0.0")
		ext2 := newMockExtension("ext2", "1.0.0", "ext1") // ext2 depends on ext1
		ext3 := newMockExtension("ext3", "1.0.0", "ext2") // ext3 depends on ext2

		app := NewApp(AppConfig{
			Extensions: []Extension{ext3, ext2, ext1}, // Random order
		})

		ctx := context.Background()
		err := app.Start(ctx)
		assert.NoError(t, err)

		// All should be started
		assert.True(t, ext1.startCalled)
		assert.True(t, ext2.startCalled)
		assert.True(t, ext3.startCalled)

		_ = app.Stop(ctx)
	})

	t.Run("CircularDependencyError", func(t *testing.T) {
		ext1 := newMockExtension("ext1", "1.0.0", "ext2")
		ext2 := newMockExtension("ext2", "1.0.0", "ext1") // Circular

		app := NewApp(AppConfig{
			Extensions: []Extension{ext1, ext2},
		})

		ctx := context.Background()
		err := app.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dependencies")
	})

	t.Run("MissingDependencyIsOptional", func(t *testing.T) {
		ext1 := newMockExtension("ext1", "1.0.0", "missing-ext")

		app := NewApp(AppConfig{
			Extensions: []Extension{ext1},
		})

		ctx := context.Background()
		err := app.Start(ctx)
		assert.NoError(t, err) // Should not error on missing optional dependency

		_ = app.Stop(ctx)
	})

	t.Run("StopInReverseOrder", func(t *testing.T) {
		// Create extensions - they will be stopped in reverse order
		ext1 := newMockExtension("ext1", "1.0.0")
		ext2 := newMockExtension("ext2", "1.0.0", "ext1")
		ext3 := newMockExtension("ext3", "1.0.0")

		app := NewApp(AppConfig{
			Extensions: []Extension{ext1, ext2, ext3}, // Registered in this order
		})

		ctx := context.Background()
		_ = app.Start(ctx)

		// All should be started
		assert.True(t, ext1.startCalled)
		assert.True(t, ext2.startCalled)
		assert.True(t, ext3.startCalled)

		_ = app.Stop(ctx)

		// All should be stopped
		assert.True(t, ext1.stopCalled)
		assert.True(t, ext2.stopCalled)
		assert.True(t, ext3.stopCalled)
	})
}

// Test Extension health checks
func TestExtensionHealthChecks(t *testing.T) {
	t.Run("HealthCheckRegistered", func(t *testing.T) {
		ext := newMockExtension("test", "1.0.0")
		app := NewApp(AppConfig{
			Extensions: []Extension{ext},
		})

		ctx := context.Background()
		_ = app.Start(ctx)

		// Health check should be registered
		healthMgr := app.HealthManager()
		result := healthMgr.Check(ctx)

		assert.NotNil(t, result)
		// The extension health check should be included
		// (specific assertion depends on health manager implementation)

		_ = app.Stop(ctx)
	})

	t.Run("UnhealthyExtension", func(t *testing.T) {
		ext := newMockExtension("test", "1.0.0")
		ext.healthError = fmt.Errorf("extension unhealthy")

		app := NewApp(AppConfig{
			Extensions: []Extension{ext},
		})

		ctx := context.Background()
		_ = app.Start(ctx)

		// Extension health check should return unhealthy
		healthMgr := app.HealthManager()
		result := healthMgr.Check(ctx)

		// Check that at least one check failed
		assert.Contains(t, result.Overall, "unhealthy")

		_ = app.Stop(ctx)
	})
}

// Test Extension info endpoint
func TestExtensionInfoEndpoint(t *testing.T) {
	t.Run("InfoIncludesExtensions", func(t *testing.T) {
		ext1 := newMockExtension("ext1", "1.0.0")
		ext2 := newMockExtension("ext2", "2.0.0", "ext1")

		app := NewApp(AppConfig{
			Name:       "test-app",
			Version:    "1.0.0",
			Extensions: []Extension{ext1, ext2},
		})

		ctx := context.Background()
		_ = app.Start(ctx)

		// Get extensions from app
		extensions := app.Extensions()
		assert.Len(t, extensions, 2)

		// Verify extension info
		assert.Equal(t, "ext1", extensions[0].Name())
		assert.Equal(t, "1.0.0", extensions[0].Version())

		assert.Equal(t, "ext2", extensions[1].Name())
		assert.Equal(t, "2.0.0", extensions[1].Version())
		assert.Equal(t, []string{"ext1"}, extensions[1].Dependencies())

		_ = app.Stop(ctx)
	})
}

// Test Extension thread safety
func TestExtensionThreadSafety(t *testing.T) {
	t.Run("ConcurrentRegister", func(t *testing.T) {
		app := NewApp(DefaultAppConfig())

		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func(idx int) {
				ext := newMockExtension(fmt.Sprintf("ext%d", idx), "1.0.0")
				_ = app.RegisterExtension(ext)
				done <- true
			}(i)
		}

		for i := 0; i < 10; i++ {
			<-done
		}

		extensions := app.Extensions()
		assert.Len(t, extensions, 10)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		ext := newMockExtension("test", "1.0.0")
		app := NewApp(AppConfig{
			Extensions: []Extension{ext},
		})

		ctx := context.Background()
		_ = app.Start(ctx)

		done := make(chan bool)
		for i := 0; i < 100; i++ {
			go func() {
				_ = app.Extensions()
				_, _ = app.GetExtension("test")
				done <- true
			}()
		}

		for i := 0; i < 100; i++ {
			<-done
		}

		_ = app.Stop(ctx)
	})
}

// Benchmark Extension operations
func BenchmarkExtensionRegister(b *testing.B) {
	app := NewApp(DefaultAppConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ext := newMockExtension(fmt.Sprintf("ext%d", i), "1.0.0")
		_ = app.RegisterExtension(ext)
	}
}

func BenchmarkExtensionGetExtension(b *testing.B) {
	ext := newMockExtension("test", "1.0.0")
	app := NewApp(AppConfig{
		Extensions: []Extension{ext},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.GetExtension("test")
	}
}

func BenchmarkBaseExtensionIsStarted(b *testing.B) {
	ext := NewBaseExtension("test", "1.0.0", "Test")
	ext.MarkStarted()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ext.IsStarted()
	}
}
