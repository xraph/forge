package di

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors2 "github.com/xraph/forge/internal/errors"
)

// Mock service for testing
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

// Mock service with callback for testing lifecycle order
type mockServiceWithCallback struct {
	mockService
	onStart func()
	onStop  func()
}

func (m *mockServiceWithCallback) Name() string {
	return m.mockService.name
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
	assert.True(t, errors.As(err, &serviceErr))
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

	// Second start should fail
	err = c.Start(ctx)
	assert.ErrorIs(t, err, errors2.ErrContainerStarted)
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
	assert.True(t, errors.As(err, &serviceErr))
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
	assert.True(t, errors.As(err, &serviceErr))
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

	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := c.Resolve("test")
			assert.NoError(t, err)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
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
