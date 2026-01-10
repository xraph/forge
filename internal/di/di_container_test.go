package di

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors2 "github.com/xraph/forge/errors"
)

// Mock service for testing.
type mockService struct {
	name       string
	started    bool
	stopped    bool
	healthy    bool
	startErr   error
	stopErr    error
	healthErr  error
	configured bool
	disposed   bool
}

func (m *mockService) Name() string {
	return m.name
}

func (m *mockService) Start(ctx context.Context) error {
	if m.startErr != nil {
		return m.startErr
	}

	m.started = true

	return nil
}

func (m *mockService) Stop(ctx context.Context) error {
	if m.stopErr != nil {
		return m.stopErr
	}

	m.stopped = true

	return nil
}

func (m *mockService) Health(ctx context.Context) error {
	if m.healthErr != nil {
		return m.healthErr
	}

	if !m.healthy {
		return errors.New("unhealthy")
	}

	return nil
}

func (m *mockService) Configure(config any) error {
	m.configured = true

	return nil
}

func (m *mockService) Dispose() error {
	m.disposed = true

	return nil
}

// Mock service with callback for testing lifecycle order.
type mockServiceWithCallback struct {
	mockService

	onStart func()
	onStop  func()
}

func (m *mockServiceWithCallback) Name() string {
	return m.name
}

func (m *mockServiceWithCallback) Start(ctx context.Context) error {
	if m.onStart != nil {
		m.onStart()
	}

	return m.mockService.Start(ctx)
}

func (m *mockServiceWithCallback) Stop(ctx context.Context) error {
	if m.onStop != nil {
		m.onStop()
	}

	return m.mockService.Stop(ctx)
}

func TestNew(t *testing.T) {
	c := NewContainer()
	assert.NotNil(t, c)
	assert.Empty(t, c.Services())
}

func TestRegister_Success(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return "value", nil
	})

	assert.NoError(t, err)
	assert.True(t, c.Has("test"))
}

func TestRegister_EmptyName(t *testing.T) {
	c := NewContainer()

	err := c.Register("", func(c Container) (any, error) {
		return "value", nil
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestRegister_NilFactory(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", nil)

	assert.ErrorIs(t, err, errors2.ErrInvalidFactory)
}

func TestRegister_AlreadyExists(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return "value1", nil
	})
	require.NoError(t, err)

	err = c.Register("test", func(c Container) (any, error) {
		return "value2", nil
	})

	assert.ErrorIs(t, err, errors2.ErrServiceAlreadyExists("test"))
}

func TestRegister_WithOptions(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return "value", nil
	},
		Transient(),
		WithDependencies("dep1", "dep2"),
		WithDIMetadata("key", "value"),
		WithGroup("group1"),
	)

	require.NoError(t, err)

	info := c.Inspect("test")
	assert.Equal(t, "transient", info.Lifecycle)
	assert.Equal(t, []string{"dep1", "dep2"}, info.Dependencies)
	assert.Equal(t, "value", info.Metadata["key"])
}

func TestResolve_Singleton(t *testing.T) {
	c := NewContainer()
	callCount := 0

	err := c.Register("test", func(c Container) (any, error) {
		callCount++

		return &mockService{name: "singleton"}, nil
	}, Singleton())
	require.NoError(t, err)

	// First resolve
	val1, err := c.Resolve("test")
	assert.NoError(t, err)
	assert.NotNil(t, val1)
	assert.Equal(t, 1, callCount)

	// Second resolve - should use cached instance
	val2, err := c.Resolve("test")
	assert.NoError(t, err)
	assert.NotNil(t, val2)
	assert.Equal(t, 1, callCount)
	assert.Same(t, val1, val2)
}

func TestResolve_Transient(t *testing.T) {
	c := NewContainer()
	callCount := 0

	err := c.Register("test", func(c Container) (any, error) {
		callCount++

		return &mockService{name: "test"}, nil
	}, Transient())
	require.NoError(t, err)

	// First resolve
	val1, err := c.Resolve("test")
	assert.NoError(t, err)
	assert.NotNil(t, val1)
	assert.Equal(t, 1, callCount)

	// Second resolve - should create new instance
	val2, err := c.Resolve("test")
	assert.NoError(t, err)
	assert.NotNil(t, val2)
	assert.Equal(t, 2, callCount)
	assert.NotSame(t, val1, val2)
}

func TestResolve_Scoped_FromContainer(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return "scoped-value", nil
	}, Scoped())
	require.NoError(t, err)

	// Resolving scoped service directly from container should fail
	_, err = c.Resolve("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be resolved from a scope")
}

func TestResolve_NotFound(t *testing.T) {
	c := NewContainer()

	_, err := c.Resolve("nonexistent")
	assert.ErrorIs(t, err, errors2.ErrServiceNotFound("nonexistent"))
}

func TestResolve_FactoryError(t *testing.T) {
	c := NewContainer()
	expectedErr := errors.New("factory error")

	err := c.Register("test", func(c Container) (any, error) {
		return nil, expectedErr
	})
	require.NoError(t, err)

	_, err = c.Resolve("test")
	assert.Error(t, err)

	var serviceErr *errors2.ServiceError
	assert.ErrorAs(t, err, &serviceErr)
	assert.Equal(t, "test", serviceErr.Service)
	assert.ErrorIs(t, serviceErr.Err, expectedErr)
}

func TestHas(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return "value", nil
	})
	require.NoError(t, err)

	assert.True(t, c.Has("test"))
	assert.False(t, c.Has("nonexistent"))
}

func TestServices(t *testing.T) {
	c := NewContainer()

	err := c.Register("service1", func(c Container) (any, error) {
		return "value1", nil
	})
	require.NoError(t, err)

	err = c.Register("service2", func(c Container) (any, error) {
		return "value2", nil
	})
	require.NoError(t, err)

	services := c.Services()
	assert.Len(t, services, 2)
	assert.Contains(t, services, "service1")
	assert.Contains(t, services, "service2")
}

func TestStart_Success(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test", healthy: true}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = c.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, svc.started)
}

func TestStart_WithDependencies(t *testing.T) {
	c := NewContainer()
	startOrder := []string{}

	// Register services with dependencies
	err := c.Register("dep1", func(c Container) (any, error) {
		return &mockService{
			name: "dep1",
			startErr: func() error {
				startOrder = append(startOrder, "dep1")

				return nil
			}(),
		}, nil
	})
	require.NoError(t, err)

	err = c.Register("dep2", func(c Container) (any, error) {
		return &mockService{
			name: "dep2",
			startErr: func() error {
				startOrder = append(startOrder, "dep2")

				return nil
			}(),
		}, nil
	}, WithDependencies("dep1"))
	require.NoError(t, err)

	err = c.Register("main", func(c Container) (any, error) {
		return &mockService{
			name: "main",
			startErr: func() error {
				startOrder = append(startOrder, "main")

				return nil
			}(),
		}, nil
	}, WithDependencies("dep1", "dep2"))
	require.NoError(t, err)

	ctx := context.Background()
	err = c.Start(ctx)
	assert.NoError(t, err)

	// Verify order: dep1 -> dep2 -> main
	assert.Equal(t, []string{"dep1", "dep2", "main"}, startOrder)
}

func TestStart_AlreadyStarted(t *testing.T) {
	c := NewContainer()

	ctx := context.Background()
	err := c.Start(ctx)
	require.NoError(t, err)

	// Second start should be idempotent (no error)
	err = c.Start(ctx)
	assert.NoError(t, err, "Container.Start() should be idempotent")
}

func TestStart_ServiceError(t *testing.T) {
	c := NewContainer()
	svc1 := &mockService{name: "svc1", healthy: true}
	svc2 := &mockService{name: "svc2", startErr: errors.New("start failed")}

	err := c.Register("svc1", func(c Container) (any, error) {
		return svc1, nil
	})
	require.NoError(t, err)

	err = c.Register("svc2", func(c Container) (any, error) {
		return svc2, nil
	}, WithDependencies("svc1"))
	require.NoError(t, err)

	ctx := context.Background()
	err = c.Start(ctx)
	assert.Error(t, err)

	var serviceErr *errors2.ServiceError
	assert.ErrorAs(t, err, &serviceErr)
	assert.Equal(t, "svc2", serviceErr.Service)
}

func TestStop_Success(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test", healthy: true}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = c.Start(ctx)
	require.NoError(t, err)

	err = c.Stop(ctx)
	assert.NoError(t, err)
	assert.True(t, svc.stopped)
}

func TestStop_NotStarted(t *testing.T) {
	c := NewContainer()

	ctx := context.Background()
	err := c.Stop(ctx)
	assert.NoError(t, err) // Should be no-op
}

func TestStop_ReverseOrder(t *testing.T) {
	c := NewContainer()
	stopOrder := []string{}

	var mu sync.Mutex

	// Register services with dependencies that track stop order
	err := c.Register("dep1", func(c Container) (any, error) {
		return &mockServiceWithCallback{
			mockService: mockService{name: "dep1"},
			onStop: func() {
				mu.Lock()

				stopOrder = append(stopOrder, "dep1")

				mu.Unlock()
			},
		}, nil
	})
	require.NoError(t, err)

	err = c.Register("main", func(c Container) (any, error) {
		return &mockServiceWithCallback{
			mockService: mockService{name: "main"},
			onStop: func() {
				mu.Lock()

				stopOrder = append(stopOrder, "main")

				mu.Unlock()
			},
		}, nil
	}, WithDependencies("dep1"))
	require.NoError(t, err)

	// Start services
	ctx := context.Background()
	err = c.Start(ctx)
	require.NoError(t, err)

	err = c.Stop(ctx)
	assert.NoError(t, err)

	// Verify reverse order: main -> dep1
	mu.Lock()
	assert.Equal(t, []string{"main", "dep1"}, stopOrder)
	mu.Unlock()
}

func TestHealth_Success(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test", healthy: true}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	// Resolve to create instance
	_, err = c.Resolve("test")
	require.NoError(t, err)

	ctx := context.Background()
	err = c.Health(ctx)
	assert.NoError(t, err)
}

func TestHealth_Failed(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test", healthy: false}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	// Resolve to create instance
	_, err = c.Resolve("test")
	require.NoError(t, err)

	ctx := context.Background()
	err = c.Health(ctx)
	assert.Error(t, err)

	var serviceErr *errors2.ServiceError
	assert.ErrorAs(t, err, &serviceErr)
	assert.Equal(t, "test", serviceErr.Service)
}

func TestInspect(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return &mockService{name: "test"}, nil
	},
		Singleton(),
		WithDependencies("dep1"),
		WithDIMetadata("version", "1.0"),
	)
	require.NoError(t, err)

	// Inspect before resolution
	info := c.Inspect("test")
	assert.Equal(t, "test", info.Name)
	assert.Equal(t, "singleton", info.Lifecycle)
	assert.Equal(t, []string{"dep1"}, info.Dependencies)
	assert.Equal(t, "1.0", info.Metadata["version"])
	assert.False(t, info.Started)

	// Resolve to create instance
	_, err = c.Resolve("test")
	require.NoError(t, err)

	// Inspect after resolution
	info = c.Inspect("test")
	assert.Contains(t, info.Type, "mockService")
}

func TestInspect_NotFound(t *testing.T) {
	c := NewContainer()

	info := c.Inspect("nonexistent")
	assert.Equal(t, "nonexistent", info.Name)
	assert.Empty(t, info.Type)
}

func TestInspect_Lifecycles(t *testing.T) {
	c := NewContainer()

	// Singleton
	err := c.Register("singleton", func(c Container) (any, error) {
		return "value", nil
	}, Singleton())
	require.NoError(t, err)

	info := c.Inspect("singleton")
	assert.Equal(t, "singleton", info.Lifecycle)

	// Scoped
	err = c.Register("scoped", func(c Container) (any, error) {
		return "value", nil
	}, Scoped())
	require.NoError(t, err)

	info = c.Inspect("scoped")
	assert.Equal(t, "scoped", info.Lifecycle)

	// Transient
	err = c.Register("transient", func(c Container) (any, error) {
		return "value", nil
	}, Transient())
	require.NoError(t, err)

	info = c.Inspect("transient")
	assert.Equal(t, "transient", info.Lifecycle)
}

func TestConcurrentResolve(t *testing.T) {
	c := NewContainer()
	callCount := 0

	err := c.Register("test", func(c Container) (any, error) {
		time.Sleep(10 * time.Millisecond)

		callCount++

		return "value", nil
	}, Singleton())
	require.NoError(t, err)

	// Resolve concurrently
	const goroutines = 10

	done := make(chan bool, goroutines)

	for range goroutines {
		go func() {
			_, err := c.Resolve("test")
			assert.NoError(t, err)

			done <- true
		}()
	}

	// Wait for all goroutines
	for range goroutines {
		<-done
	}

	// Factory should be called only once
	assert.Equal(t, 1, callCount)
}

func TestBeginScope(t *testing.T) {
	c := NewContainer()

	scope := c.BeginScope()
	assert.NotNil(t, scope)

	err := scope.End()
	assert.NoError(t, err)
}

func TestIsStarted(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test", healthy: true}

	// Test non-existent service
	assert.False(t, c.IsStarted("nonexistent"))

	// Register service
	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	// Service registered but not started
	assert.False(t, c.IsStarted("test"))

	// Start container
	ctx := context.Background()
	err = c.Start(ctx)
	require.NoError(t, err)

	// Service should now be started
	assert.True(t, c.IsStarted("test"))
}

func TestResolveReady_Success(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test", healthy: true}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	// ResolveReady should start the service before returning
	instance, err := c.ResolveReady(ctx, "test")
	assert.NoError(t, err)
	assert.NotNil(t, instance)
	assert.True(t, svc.started)
	assert.True(t, c.IsStarted("test"))
}

func TestResolveReady_NotFound(t *testing.T) {
	c := NewContainer()
	ctx := context.Background()

	_, err := c.ResolveReady(ctx, "nonexistent")
	assert.Error(t, err)
}

func TestResolveReady_AlreadyStarted(t *testing.T) {
	c := NewContainer()
	startCount := 0
	svc := &mockServiceWithCallback{
		mockService: mockService{name: "test", healthy: true},
		onStart: func() {
			startCount++
		},
	}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	// First ResolveReady
	_, err = c.ResolveReady(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, 1, startCount)

	// Second ResolveReady should not call Start again
	_, err = c.ResolveReady(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, 1, startCount) // Should still be 1
}

func TestResolveReady_WithDependencies(t *testing.T) {
	c := NewContainer()
	startOrder := []string{}

	var mu sync.Mutex

	// Register dependency
	err := c.Register("dep", func(c Container) (any, error) {
		return &mockServiceWithCallback{
			mockService: mockService{name: "dep", healthy: true},
			onStart: func() {
				mu.Lock()

				startOrder = append(startOrder, "dep")

				mu.Unlock()
			},
		}, nil
	})
	require.NoError(t, err)

	// Register main service that depends on dep
	err = c.Register("main", func(c Container) (any, error) {
		return &mockServiceWithCallback{
			mockService: mockService{name: "main", healthy: true},
			onStart: func() {
				mu.Lock()

				startOrder = append(startOrder, "main")

				mu.Unlock()
			},
		}, nil
	}, WithDependencies("dep"))
	require.NoError(t, err)

	ctx := context.Background()

	// ResolveReady for main should start the service
	_, err = c.ResolveReady(ctx, "main")
	require.NoError(t, err)

	mu.Lock()
	// Main service should be started (dependency may or may not be started
	// depending on how ResolveReady handles dependencies)
	assert.Contains(t, startOrder, "main")
	mu.Unlock()

	assert.True(t, c.IsStarted("main"))
}

func TestResolveReady_StartError(t *testing.T) {
	c := NewContainer()
	expectedErr := errors.New("start failed")
	svc := &mockService{name: "test", startErr: expectedErr}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	_, err = c.ResolveReady(ctx, "test")
	assert.Error(t, err)

	var serviceErr *errors2.ServiceError
	assert.ErrorAs(t, err, &serviceErr)
}

func TestResolveReady_NonServiceType(t *testing.T) {
	c := NewContainer()

	// Register a simple value (not implementing Service interface)
	err := c.Register("simple", func(c Container) (any, error) {
		return "simple-value", nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	// ResolveReady should still work for non-Service types
	instance, err := c.ResolveReady(ctx, "simple")
	assert.NoError(t, err)
	assert.Equal(t, "simple-value", instance)
}

// =============================================================================
// AUTO-START ON RESOLVE TESTS
// =============================================================================

func TestResolve_AutoStartsSharedService(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test", healthy: true}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	// Resolve should auto-start the service
	instance, err := c.Resolve("test")
	require.NoError(t, err)
	assert.Same(t, svc, instance)
	assert.True(t, svc.started, "Service should be auto-started on Resolve")
	assert.True(t, c.IsStarted("test"), "Service should be marked as started")
}

func TestResolve_AutoStartOnlyOnce(t *testing.T) {
	c := NewContainer()
	startCount := 0
	svc := &mockServiceWithCallback{
		mockService: mockService{name: "test", healthy: true},
		onStart: func() {
			startCount++
		},
	}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	// First resolve should start
	_, err = c.Resolve("test")
	require.NoError(t, err)
	assert.Equal(t, 1, startCount)

	// Second resolve should not start again
	_, err = c.Resolve("test")
	require.NoError(t, err)
	assert.Equal(t, 1, startCount, "Service should only be started once")
}

func TestResolve_AutoStartError(t *testing.T) {
	c := NewContainer()
	expectedErr := errors.New("auto-start failed")
	svc := &mockService{name: "test", startErr: expectedErr}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	// Resolve should fail if auto-start fails
	_, err = c.Resolve("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "auto_start")
}

func TestResolve_NonServiceTypeNoAutoStart(t *testing.T) {
	c := NewContainer()

	// Register a simple string (not implementing Service)
	err := c.Register("simple", func(c Container) (any, error) {
		return "just-a-string", nil
	})
	require.NoError(t, err)

	// Resolve should work fine for non-Service types
	instance, err := c.Resolve("simple")
	require.NoError(t, err)
	assert.Equal(t, "just-a-string", instance)
}

// =============================================================================
// CONTAINER.START() IDEMPOTENCY TESTS
// =============================================================================

func TestContainerStart_Idempotent(t *testing.T) {
	c := NewContainer()
	startCount := 0
	svc := &mockServiceWithCallback{
		mockService: mockService{name: "test", healthy: true},
		onStart: func() {
			startCount++
		},
	}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	// First Start should work
	err = c.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, startCount)

	// Second Start should be idempotent (no error, no re-start)
	err = c.Start(ctx)
	require.NoError(t, err) // Should NOT error
	assert.Equal(t, 1, startCount, "Service should not be started again")
}

func TestContainerStart_SkipsAlreadyStartedServices(t *testing.T) {
	c := NewContainer()
	startCount := 0
	svc := &mockServiceWithCallback{
		mockService: mockService{name: "test", healthy: true},
		onStart: func() {
			startCount++
		},
	}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	})
	require.NoError(t, err)

	// Resolve first (which auto-starts)
	_, err = c.Resolve("test")
	require.NoError(t, err)
	assert.Equal(t, 1, startCount)

	// Now call Container.Start() - should skip already started services
	ctx := context.Background()
	err = c.Start(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, startCount, "Already started service should be skipped")
}

// =============================================================================
// DEPENDENCY ORDER TESTS
// =============================================================================

func TestResolve_WithDependencies_AutoStartsInOrder(t *testing.T) {
	c := NewContainer()
	startOrder := []string{}

	var mu sync.Mutex

	// Register dependency
	depSvc := &mockServiceWithCallback{
		mockService: mockService{name: "dep", healthy: true},
		onStart: func() {
			mu.Lock()

			startOrder = append(startOrder, "dep")

			mu.Unlock()
		},
	}
	err := c.Register("dep", func(c Container) (any, error) {
		return depSvc, nil
	})
	require.NoError(t, err)

	// Register main service that depends on dep
	mainSvc := &mockServiceWithCallback{
		mockService: mockService{name: "main", healthy: true},
		onStart: func() {
			mu.Lock()

			startOrder = append(startOrder, "main")

			mu.Unlock()
		},
	}
	err = c.Register("main", func(c Container) (any, error) {
		// Factory resolves dependency
		_, err := c.Resolve("dep")
		if err != nil {
			return nil, err
		}

		return mainSvc, nil
	}, WithDependencies("dep"))
	require.NoError(t, err)

	// Resolve main - should auto-start both dep and main
	_, err = c.Resolve("main")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	// Both should be started
	assert.Contains(t, startOrder, "dep")
	assert.Contains(t, startOrder, "main")

	// Dependency should start before main
	depIdx := -1
	mainIdx := -1

	for i, name := range startOrder {
		if name == "dep" {
			depIdx = i
		}

		if name == "main" {
			mainIdx = i
		}
	}

	assert.Less(t, depIdx, mainIdx, "Dependency should start before dependent")
}
