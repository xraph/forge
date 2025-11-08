package di

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors2 "github.com/xraph/forge/internal/errors"
)

func TestScope_ResolveSingleton(t *testing.T) {
	c := NewContainer()

	err := c.Register("singleton", func(c Container) (any, error) {
		return &mockService{name: "singleton"}, nil
	}, Singleton())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// Resolve singleton from scope
	val, err := scope.Resolve("singleton")
	assert.NoError(t, err)
	assert.NotNil(t, val)

	// Should be same instance as container
	containerVal, err := c.Resolve("singleton")
	require.NoError(t, err)
	assert.Same(t, containerVal, val)
}

func TestScope_ResolveScoped(t *testing.T) {
	c := NewContainer()
	callCount := 0

	err := c.Register("scoped", func(c Container) (any, error) {
		callCount++

		return &mockService{name: "scoped"}, nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// First resolve
	val1, err := scope.Resolve("scoped")
	assert.NoError(t, err)
	assert.NotNil(t, val1)
	assert.Equal(t, 1, callCount)

	// Second resolve in same scope - should use cached instance
	val2, err := scope.Resolve("scoped")
	assert.NoError(t, err)
	assert.Same(t, val1, val2)
	assert.Equal(t, 1, callCount)
}

func TestScope_ResolveScoped_DifferentScopes(t *testing.T) {
	c := NewContainer()
	callCount := 0

	err := c.Register("scoped", func(c Container) (any, error) {
		callCount++

		return &mockService{name: "scoped"}, nil
	}, Scoped())
	require.NoError(t, err)

	// First scope
	scope1 := c.BeginScope()
	val1, err := scope1.Resolve("scoped")
	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
	scope1.End()

	// Second scope - should create new instance
	scope2 := c.BeginScope()
	val2, err := scope2.Resolve("scoped")
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.NotSame(t, val1, val2)
	scope2.End()
}

func TestScope_ResolveTransient(t *testing.T) {
	c := NewContainer()
	callCount := 0

	err := c.Register("transient", func(c Container) (any, error) {
		callCount++

		return &mockService{name: "transient"}, nil
	}, Transient())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// First resolve
	val1, err := scope.Resolve("transient")
	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)

	// Second resolve - should create new instance
	val2, err := scope.Resolve("transient")
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
	assert.NotSame(t, val1, val2)
}

func TestScope_ResolveNotFound(t *testing.T) {
	c := NewContainer()

	scope := c.BeginScope()
	defer scope.End()

	_, err := scope.Resolve("nonexistent")
	assert.ErrorIs(t, err, errors2.ErrServiceNotFoundSentinel)
}

func TestScope_ResolveAfterEnd(t *testing.T) {
	c := NewContainer()

	err := c.Register("test", func(c Container) (any, error) {
		return "value", nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()
	err = scope.End()
	require.NoError(t, err)

	// Resolve after end should fail
	_, err = scope.Resolve("test")
	assert.ErrorIs(t, err, errors2.ErrScopeEnded)
}

func TestScope_EndWithDisposable(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test"}

	err := c.Register("test", func(c Container) (any, error) {
		return svc, nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()

	// Resolve to create instance
	_, err = scope.Resolve("test")
	require.NoError(t, err)

	// End should call Dispose
	err = scope.End()
	assert.NoError(t, err)
	assert.True(t, svc.disposed)
}

func TestScope_EndTwice(t *testing.T) {
	c := NewContainer()
	scope := c.BeginScope()

	err := scope.End()
	require.NoError(t, err)

	// Second end should fail
	err = scope.End()
	assert.ErrorIs(t, err, errors2.ErrScopeEnded)
}

func TestScope_ConcurrentResolve(t *testing.T) {
	c := NewContainer()
	callCount := 0

	var mu sync.Mutex

	err := c.Register("scoped", func(c Container) (any, error) {
		mu.Lock()

		callCount++

		mu.Unlock()

		return &mockService{name: "scoped"}, nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	// Resolve concurrently
	const goroutines = 10

	done := make(chan bool, goroutines)
	values := make(chan any, goroutines)

	for range goroutines {
		go func() {
			val, err := scope.Resolve("scoped")
			assert.NoError(t, err)

			values <- val

			done <- true
		}()
	}

	// Wait for all goroutines
	for range goroutines {
		<-done
	}

	close(values)

	// All should get the same instance
	var first any
	for val := range values {
		if first == nil {
			first = val
		} else {
			assert.Same(t, first, val)
		}
	}

	// Factory should be called only once
	mu.Lock()
	assert.Equal(t, 1, callCount)
	mu.Unlock()
}
