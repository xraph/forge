package di

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors2 "github.com/xraph/forge/v2/internal/errors"
)

type testService struct {
	value string
}

type testInterface interface {
	GetValue() string
}

type testImpl struct {
	value string
}

func (t *testImpl) GetValue() string {
	return t.value
}

func TestResolve_TypeSafe(t *testing.T) {
	c := NewContainer()

	err := RegisterSingleton(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "hello"}, nil
	})
	require.NoError(t, err)

	// Resolve with correct type
	svc, err := Resolve[*testService](c, "test")
	assert.NoError(t, err)
	assert.Equal(t, "hello", svc.value)
}

func TestResolve_TypeMismatch(t *testing.T) {
	c := NewContainer()

	err := RegisterSingleton(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "hello"}, nil
	})
	require.NoError(t, err)

	// Resolve with wrong type
	_, err = Resolve[string](c, "test")
	assert.ErrorIs(t, err, errors2.ErrTypeMismatch)
}

func TestResolveHelper_NotFound(t *testing.T) {
	c := NewContainer()

	_, err := Resolve[*testService](c, "nonexistent")
	assert.ErrorIs(t, err, errors2.ErrServiceNotFoundSentinel)
}

func TestMust_Success(t *testing.T) {
	c := NewContainer()

	err := RegisterSingleton(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "hello"}, nil
	})
	require.NoError(t, err)

	// Must should not panic
	svc := Must[*testService](c, "test")
	assert.Equal(t, "hello", svc.value)
}

func TestMust_Panic(t *testing.T) {
	c := NewContainer()

	// Must should panic
	assert.Panics(t, func() {
		Must[*testService](c, "nonexistent")
	})
}

func TestRegisterSingleton_Generic(t *testing.T) {
	c := NewContainer()

	err := RegisterSingleton(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "singleton"}, nil
	})
	require.NoError(t, err)

	svc1 := Must[*testService](c, "test")
	svc2 := Must[*testService](c, "test")
	assert.Same(t, svc1, svc2)
}

func TestRegisterTransient_Generic(t *testing.T) {
	c := NewContainer()

	err := RegisterTransient(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "transient"}, nil
	})
	require.NoError(t, err)

	svc1 := Must[*testService](c, "test")
	svc2 := Must[*testService](c, "test")
	assert.NotSame(t, svc1, svc2)
}

func TestRegisterScoped_Generic(t *testing.T) {
	c := NewContainer()

	err := RegisterScoped(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "scoped"}, nil
	})
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	svc1, err := ResolveScope[*testService](scope, "test")
	require.NoError(t, err)

	svc2, err := ResolveScope[*testService](scope, "test")
	require.NoError(t, err)

	assert.Same(t, svc1, svc2)
}

func TestRegisterInterface_Success(t *testing.T) {
	c := NewContainer()

	err := RegisterInterface[testInterface, *testImpl](c, "test",
		func(c Container) (*testImpl, error) {
			return &testImpl{value: "interface"}, nil
		},
		Singleton(),
	)
	require.NoError(t, err)

	// Resolve as interface
	impl, err := Resolve[testInterface](c, "test")
	assert.NoError(t, err)
	assert.Equal(t, "interface", impl.GetValue())
}

func TestRegisterInterface_FactoryError(t *testing.T) {
	c := NewContainer()

	err := RegisterInterface[testInterface, *testImpl](c, "test",
		func(c Container) (*testImpl, error) {
			return nil, assert.AnError
		},
		Singleton(),
	)
	require.NoError(t, err)

	// Resolve should return factory error
	_, err = c.Resolve("test")
	assert.Error(t, err)
	assert.ErrorContains(t, err, "assert.AnError")
}

func TestRegisterInterface_AllLifecycles(t *testing.T) {
	c := NewContainer()

	// Singleton (via option)
	err := RegisterInterface[testInterface, *testImpl](c, "singleton",
		func(c Container) (*testImpl, error) {
			return &testImpl{value: "singleton"}, nil
		},
		Singleton(),
	)
	require.NoError(t, err)

	// Scoped (via option)
	err = RegisterInterface[testInterface, *testImpl](c, "scoped",
		func(c Container) (*testImpl, error) {
			return &testImpl{value: "scoped"}, nil
		},
		Scoped(),
	)
	require.NoError(t, err)

	// Transient (via option)
	err = RegisterInterface[testInterface, *testImpl](c, "transient",
		func(c Container) (*testImpl, error) {
			return &testImpl{value: "transient"}, nil
		},
		Transient(),
	)
	require.NoError(t, err)

	// Verify singleton behavior
	svc1 := Must[testInterface](c, "singleton")
	svc2 := Must[testInterface](c, "singleton")
	assert.Same(t, svc1, svc2)

	// Verify transient behavior
	svc3 := Must[testInterface](c, "transient")
	svc4 := Must[testInterface](c, "transient")
	assert.NotSame(t, svc3, svc4)

	// Verify scoped behavior
	scope := c.BeginScope()
	svc5, _ := ResolveScope[testInterface](scope, "scoped")
	svc6, _ := ResolveScope[testInterface](scope, "scoped")
	assert.Same(t, svc5, svc6)
	scope.End()
}

func TestRegisterValue(t *testing.T) {
	c := NewContainer()

	instance := &testService{value: "prebuilt"}
	err := RegisterValue(c, "test", instance)
	require.NoError(t, err)

	svc := Must[*testService](c, "test")
	assert.Same(t, instance, svc)
}

func TestRegisterSingletonInterface_Convenience(t *testing.T) {
	c := NewContainer()

	err := RegisterSingletonInterface[testInterface, *testImpl](c, "test",
		func(c Container) (*testImpl, error) {
			return &testImpl{value: "singleton-interface"}, nil
		},
	)
	require.NoError(t, err)

	svc1 := Must[testInterface](c, "test")
	svc2 := Must[testInterface](c, "test")
	assert.Same(t, svc1, svc2)
}

func TestRegisterScopedInterface_Convenience(t *testing.T) {
	c := NewContainer()

	err := RegisterScopedInterface[testInterface, *testImpl](c, "test",
		func(c Container) (*testImpl, error) {
			return &testImpl{value: "scoped-interface"}, nil
		},
	)
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	svc1, err := ResolveScope[testInterface](scope, "test")
	require.NoError(t, err)

	svc2, err := ResolveScope[testInterface](scope, "test")
	require.NoError(t, err)

	assert.Same(t, svc1, svc2)
}

func TestRegisterTransientInterface_Convenience(t *testing.T) {
	c := NewContainer()

	err := RegisterTransientInterface[testInterface, *testImpl](c, "test",
		func(c Container) (*testImpl, error) {
			return &testImpl{value: "transient-interface"}, nil
		},
	)
	require.NoError(t, err)

	svc1 := Must[testInterface](c, "test")
	svc2 := Must[testInterface](c, "test")
	assert.NotSame(t, svc1, svc2)
}

func TestResolveScope_TypeSafe(t *testing.T) {
	c := NewContainer()

	err := RegisterScoped(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "scoped"}, nil
	})
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	svc, err := ResolveScope[*testService](scope, "test")
	assert.NoError(t, err)
	assert.Equal(t, "scoped", svc.value)
}

func TestResolveScope_TypeMismatch(t *testing.T) {
	c := NewContainer()

	err := RegisterScoped(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "scoped"}, nil
	})
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	_, err = ResolveScope[string](scope, "test")
	assert.ErrorIs(t, err, errors2.ErrTypeMismatch)
}

func TestMustScope_Success(t *testing.T) {
	c := NewContainer()

	err := RegisterScoped(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "scoped"}, nil
	})
	require.NoError(t, err)

	scope := c.BeginScope()
	defer scope.End()

	svc := MustScope[*testService](scope, "test")
	assert.Equal(t, "scoped", svc.value)
}

func TestMustScope_Panic(t *testing.T) {
	c := NewContainer()
	scope := c.BeginScope()
	defer scope.End()

	assert.Panics(t, func() {
		MustScope[*testService](scope, "nonexistent")
	})
}

// Test complex scenarios
func TestComplexDependencies(t *testing.T) {
	c := NewContainer()

	// Register logger
	err := RegisterSingleton(c, "logger", func(c Container) (*testService, error) {
		return &testService{value: "logger"}, nil
	})
	require.NoError(t, err)

	// Register database with logger dependency
	err = c.Register("database", func(c Container) (any, error) {
		logger := Must[*testService](c, "logger")
		return &testService{value: "db-with-" + logger.value}, nil
	}, WithDependencies("logger"))
	require.NoError(t, err)

	// Start container
	ctx := context.Background()
	err = c.Start(ctx)
	require.NoError(t, err)

	// Resolve database
	db := Must[*testService](c, "database")
	assert.Equal(t, "db-with-logger", db.value)

	// Stop container
	err = c.Stop(ctx)
	assert.NoError(t, err)
}
