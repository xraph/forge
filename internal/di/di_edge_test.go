package di

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Custom disposable that returns error.
type errorDisposable struct {
	name string
	err  error
}

func (e *errorDisposable) Dispose() error {
	return e.err
}

func TestScope_End_DisposeError(t *testing.T) {
	c := NewContainer()
	disposeErr := errors.New("dispose failed")

	err := c.Register("test", func(c Container) (any, error) {
		return &errorDisposable{name: "test", err: disposeErr}, nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()

	// Resolve to create instance
	_, err = scope.Resolve("test")
	require.NoError(t, err)

	// End should return error from Dispose
	err = scope.End()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scope cleanup errors")
	assert.Contains(t, err.Error(), "dispose failed")
}

func TestScope_End_MultipleDisposeErrors(t *testing.T) {
	c := NewContainer()
	err1 := errors.New("dispose error 1")
	err2 := errors.New("dispose error 2")

	err := c.Register("test1", func(c Container) (any, error) {
		return &errorDisposable{name: "test1", err: err1}, nil
	}, Scoped())
	require.NoError(t, err)

	err = c.Register("test2", func(c Container) (any, error) {
		return &errorDisposable{name: "test2", err: err2}, nil
	}, Scoped())
	require.NoError(t, err)

	scope := c.BeginScope()

	// Resolve both
	_, err = scope.Resolve("test1")
	require.NoError(t, err)
	_, err = scope.Resolve("test2")
	require.NoError(t, err)

	// End should collect all dispose errors
	err = scope.End()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scope cleanup errors")
}

func TestResolve_Singleton_RaceCondition(t *testing.T) {
	// This test attempts to hit the double-check path in Resolve
	// where a second goroutine finds the instance already created
	// after acquiring the write lock
	for range 10 {
		c := NewContainer()

		err := c.Register("test", func(c Container) (any, error) {
			// Small delay to increase chance of race
			return &mockService{name: "test"}, nil
		}, Singleton())
		require.NoError(t, err)

		// Resolve many times concurrently
		const goroutines = 100

		done := make(chan any, goroutines)

		for range goroutines {
			go func() {
				val, err := c.Resolve("test")
				if err == nil {
					done <- val
				} else {
					done <- err
				}
			}()
		}

		// Collect all results
		first := <-done
		for i := 1; i < goroutines; i++ {
			val := <-done
			// All should be the same instance
			if err, ok := val.(error); ok {
				t.Fatalf("unexpected error: %v", err)
			}

			assert.Same(t, first, val)
		}
	}
}
