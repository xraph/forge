package di

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors2 "github.com/xraph/forge/errors"
)

// Additional tests to achieve 100% coverage

func TestResolve_Transient_NonService(t *testing.T) {
	c := NewContainer()

	// Register non-Service type as transient
	err := c.Register("test", func(c Container) (any, error) {
		return "transient-value", nil
	}, Transient())
	require.NoError(t, err)

	// Resolve multiple times
	val1, err := c.Resolve("test")
	assert.NoError(t, err)
	assert.Equal(t, "transient-value", val1)

	val2, err := c.Resolve("test")
	assert.NoError(t, err)
	assert.Equal(t, "transient-value", val2)
}

func TestStart_RollbackOnError(t *testing.T) {
	c := NewContainer()

	// Register two services, second one fails
	err := c.Register("dep1", func(c Container) (any, error) {
		return &mockService{name: "dep1"}, nil
	})
	require.NoError(t, err)

	expectedErr := errors.New("start failed")
	err = c.Register("main", func(c Container) (any, error) {
		return &mockService{
			name:     "main",
			startErr: expectedErr,
		}, nil
	}, WithDependencies("dep1"))
	require.NoError(t, err)

	// Start should fail and rollback
	ctx := context.Background()
	err = c.Start(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)

	// Container should not be marked as started
	cont := c.(*containerImpl)
	assert.False(t, cont.started)
}

func TestStart_CircularDependencyError(t *testing.T) {
	c := NewContainer()

	// Register services with circular dependency
	err := c.Register("a", func(c Container) (any, error) {
		return &mockService{name: "a"}, nil
	}, WithDependencies("b"))
	require.NoError(t, err)

	err = c.Register("b", func(c Container) (any, error) {
		return &mockService{name: "b"}, nil
	}, WithDependencies("a"))
	require.NoError(t, err)

	// Start should fail with circular dependency error
	ctx := context.Background()
	err = c.Start(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, errors2.ErrCircularDependencySentinel)
}

func TestStop_WithError(t *testing.T) {
	c := NewContainer()

	expectedErr := errors.New("stop failed")
	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{
			name:    "test",
			stopErr: expectedErr,
		}, nil
	})
	require.NoError(t, err)

	// Start service
	ctx := context.Background()
	err = c.Start(ctx)
	require.NoError(t, err)

	// Stop should return error
	err = c.Stop(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestStop_CircularDependencyError(t *testing.T) {
	c := NewContainer()

	// Register services with circular dependency
	err := c.Register("a", func(c Container) (any, error) {
		return &mockService{name: "a"}, nil
	}, WithDependencies("b"))
	require.NoError(t, err)

	err = c.Register("b", func(c Container) (any, error) {
		return &mockService{name: "b"}, nil
	}, WithDependencies("a"))
	require.NoError(t, err)

	// Manually mark as started to test Stop path
	cont := c.(*containerImpl)
	cont.started = true

	// Stop should fail with circular dependency error
	ctx := context.Background()
	err = c.Stop(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, errors2.ErrCircularDependencySentinel)
}

func TestStartService_NonService(t *testing.T) {
	c := NewContainer()

	// Register non-Service type
	err := c.Register("test", func(c Container) (any, error) {
		return "not-a-service", nil
	})
	require.NoError(t, err)

	// Start should succeed (no-op for non-Service types)
	ctx := context.Background()
	err = c.Start(ctx)
	assert.NoError(t, err)
}

func TestStopService_NotStarted(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{name: "test"}, nil
	})
	require.NoError(t, err)

	// Manually test stopService without starting
	cont := c.(*containerImpl)
	cont.started = true // Mark container as started

	// Resolve to create instance
	_, err = c.Resolve("test")
	require.NoError(t, err)

	// stopService should be no-op (service not started)
	ctx := context.Background()
	err = cont.stopService(ctx, "test")
	assert.NoError(t, err)
}

func TestStopService_NilInstance(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{name: "test"}, nil
	})
	require.NoError(t, err)

	cont := c.(*containerImpl)
	cont.started = true

	// Don't resolve (instance is nil)
	// stopService should be no-op
	ctx := context.Background()
	err = cont.stopService(ctx, "test")
	assert.NoError(t, err)
}

func TestScope_ResolveTransient_NonService(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return "transient-value", nil
	}, Transient())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// Resolve transient from scope
	val, err := scope.Resolve("test")
	assert.NoError(t, err)
	assert.Equal(t, "transient-value", val)
}

func TestScope_End_MultipleDisposables(t *testing.T) {
	c := NewContainer()

	// Register multiple scoped services with disposable
	err := c.Register("test1", func(c Container) (any, error) {
		return &mockService{name: "test1"}, nil
	}, Scoped())
	require.NoError(t, err)

	err = c.Register("test2", func(c Container) (any, error) {
		return &mockService{name: "test2"}, nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()

	// Resolve both to create instances
	_, err = scope.Resolve("test1")
	require.NoError(t, err)

	_, err = scope.Resolve("test2")
	require.NoError(t, err)

	// End should dispose all
	err = scope.End()
	assert.NoError(t, err)
}

func TestHealth_NonHealthChecker(t *testing.T) {
	c := NewContainer()

	// Register non-HealthChecker type
	err := c.Register("test", func(c Container) (any, error) {
		return "not-a-health-checker", nil
	})
	require.NoError(t, err)

	// Resolve to create instance
	_, err = c.Resolve("test")
	require.NoError(t, err)

	// Health should succeed (skip non-HealthChecker types)
	ctx := context.Background()
	err = c.Health(ctx)
	assert.NoError(t, err)
}

func TestHealth_ScopedService(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{name: "test", healthy: true}, nil
	}, Scoped())
	require.NoError(t, err)

	// Health should skip scoped services (they're not singleton)
	ctx := context.Background()
	err = c.Health(ctx)
	assert.NoError(t, err)
}

func TestHealth_TransientService(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{name: "test", healthy: true}, nil
	}, Transient())
	require.NoError(t, err)

	// Health should skip transient services (they're not singleton)
	ctx := context.Background()
	err = c.Health(ctx)
	assert.NoError(t, err)
}

func TestHealth_SingletonNotResolved(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{name: "test", healthy: true}, nil
	}, Singleton())
	require.NoError(t, err)

	// Don't resolve - instance is nil
	// Health should skip nil instances
	ctx := context.Background()
	err = c.Health(ctx)
	assert.NoError(t, err)
}

func TestResolve_TransientWithError(t *testing.T) {
	c := NewContainer()
	expectedErr := errors.New("transient factory error")

	err := c.Register("test", func(c Container) (any, error) {
		return nil, expectedErr
	}, Transient())
	require.NoError(t, err)

	// Resolve should return factory error
	_, err = c.Resolve("test")
	assert.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestStartService_ResolveError(t *testing.T) {
	c := NewContainer()
	expectedErr := errors.New("resolve error")

	err := c.Register("dep", func(c Container) (any, error) {
		return nil, expectedErr
	})
	require.NoError(t, err)

	err = c.Register("main", func(c Container) (any, error) {
		return &mockService{name: "main"}, nil
	}, WithDependencies("dep"))
	require.NoError(t, err)

	// Start should fail when dependency resolution fails
	ctx := context.Background()
	err = c.Start(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestScope_ResolveScopedWithError(t *testing.T) {
	c := NewContainer()
	expectedErr := errors.New("scoped factory error")

	err := c.Register("test", func(c Container) (any, error) {
		return nil, expectedErr
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// Resolve should return factory error
	_, err = scope.Resolve("test")
	assert.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestScope_End_NonDisposable(t *testing.T) {
	c := NewContainer()

	// Register non-Disposable type
	err := c.Register("test", func(c Container) (any, error) {
		return "not-disposable", nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()

	// Resolve to create instance
	_, err = scope.Resolve("test")
	require.NoError(t, err)

	// End should succeed (no-op for non-Disposable)
	err = scope.End()
	assert.NoError(t, err)
}

func TestScope_ResolveSingleton_FromScope(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{name: "test"}, nil
	}, Singleton())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// Resolve singleton from scope - should use parent container
	val1, err := scope.Resolve("test")
	require.NoError(t, err)
	assert.NotNil(t, val1)

	// Should be same as container's instance
	val2, err := c.Resolve("test")
	require.NoError(t, err)
	assert.Same(t, val1, val2)
}

func TestScope_ResolveScoped_Cached(t *testing.T) {
	c := NewContainer()
	callCount := 0

	err := c.Register("test", func(c Container) (any, error) {
		callCount++

		return &mockService{name: "test"}, nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// First resolve
	val1, err := scope.Resolve("test")
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// Second resolve - should use cached instance
	val2, err := scope.Resolve("test")
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
	assert.Same(t, val1, val2)
}

func TestScope_ResolveTransient_Error(t *testing.T) {
	c := NewContainer()
	expectedErr := errors.New("transient error")

	err := c.Register("test", func(c Container) (any, error) {
		return nil, expectedErr
	}, Transient())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// Resolve should return factory error
	_, err = scope.Resolve("test")
	assert.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestScope_End_WithDisposableInstance(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{name: "test"}, nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()

	// Resolve to create instance
	instance, err := scope.Resolve("test")
	require.NoError(t, err)

	// End should dispose the instance
	err = scope.End()
	assert.NoError(t, err)

	// Verify disposed
	ms := instance.(*mockService)
	assert.True(t, ms.disposed)
}

func TestResolve_Singleton_DoubleCheckPath(t *testing.T) {
	c := NewContainer()
	callCount := 0

	var mu sync.Mutex

	err := c.Register("test", func(c Container) (any, error) {
		mu.Lock()

		callCount++

		mu.Unlock()

		return &mockService{name: "test"}, nil
	}, Singleton())
	require.NoError(t, err)

	// First resolve creates instance
	val1, err := c.Resolve("test")
	require.NoError(t, err)
	assert.NotNil(t, val1)

	// Second resolve uses cached (tests double-check path)
	val2, err := c.Resolve("test")
	require.NoError(t, err)
	assert.Same(t, val1, val2)

	mu.Lock()
	assert.Equal(t, 1, callCount)
	mu.Unlock()
}
