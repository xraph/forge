package di

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors2 "github.com/xraph/forge/errors"
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

// Test complex scenarios.
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

// TestResolveReady_TypeSafe tests ResolveReady with type safety
func TestResolveReady_TypeSafe(t *testing.T) {
	c := NewContainer()

	// Create a service that implements shared.Service
	svc := &mockService{name: "test-svc", healthy: true}

	err := RegisterSingleton(c, "test", func(c Container) (*mockService, error) {
		return svc, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	// ResolveReady with correct type
	result, err := ResolveReady[*mockService](ctx, c, "test")
	assert.NoError(t, err)
	assert.Same(t, svc, result)
	assert.True(t, svc.started)
}

func TestResolveReady_TypeMismatch(t *testing.T) {
	c := NewContainer()

	err := RegisterSingleton(c, "test", func(c Container) (*testService, error) {
		return &testService{value: "hello"}, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	// ResolveReady with wrong type
	_, err = ResolveReady[string](ctx, c, "test")
	assert.ErrorIs(t, err, errors2.ErrTypeMismatch)
}

func TestResolveReady_Helper_NotFound(t *testing.T) {
	c := NewContainer()
	ctx := context.Background()

	_, err := ResolveReady[*testService](ctx, c, "nonexistent")
	assert.ErrorIs(t, err, errors2.ErrServiceNotFoundSentinel)
}

func TestMustResolveReady_Success(t *testing.T) {
	c := NewContainer()
	svc := &mockService{name: "test-svc", healthy: true}

	err := RegisterSingleton(c, "test", func(c Container) (*mockService, error) {
		return svc, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	// MustResolveReady should not panic
	result := MustResolveReady[*mockService](ctx, c, "test")
	assert.Same(t, svc, result)
	assert.True(t, svc.started)
}

func TestMustResolveReady_Panic(t *testing.T) {
	c := NewContainer()
	ctx := context.Background()

	// MustResolveReady should panic
	assert.Panics(t, func() {
		MustResolveReady[*testService](ctx, c, "nonexistent")
	})
}

func TestResolveReady_EagerInitialization(t *testing.T) {
	c := NewContainer()

	// Track the order of operations
	order := []string{}

	// Register a service that tracks when it's instantiated and started
	err := RegisterSingleton(c, "database", func(c Container) (*mockService, error) {
		order = append(order, "database-factory")
		return &mockService{
			name:    "database",
			healthy: true,
		}, nil
	})
	require.NoError(t, err)

	ctx := context.Background()

	// ResolveReady should instantiate and start the service
	svc, err := ResolveReady[*mockService](ctx, c, "database")
	require.NoError(t, err)

	order = append(order, "resolved")

	// Verify the service was instantiated
	assert.Contains(t, order, "database-factory")
	assert.Contains(t, order, "resolved")
	assert.True(t, svc.started)
	assert.True(t, c.IsStarted("database"))
}

// =============================================================================
// Tests for *With functions (typed injection pattern)
// =============================================================================

type dbService struct {
	connStr string
}

type logService struct {
	prefix string
}

type userServiceWithDeps struct {
	db     *dbService
	logger *logService
}

func TestRegisterSingletonWith_Basic(t *testing.T) {
	c := NewContainer()

	// Register dependencies
	err := RegisterSingleton(c, "db", func(c Container) (*dbService, error) {
		return &dbService{connStr: "postgres://localhost"}, nil
	})
	require.NoError(t, err)

	// Register with typed injection
	err = RegisterSingletonWith[*userServiceWithDeps](c, "userService",
		Inject[*dbService]("db"),
		func(db *dbService) (*userServiceWithDeps, error) {
			return &userServiceWithDeps{db: db}, nil
		},
	)
	require.NoError(t, err)

	// Resolve
	svc, err := Resolve[*userServiceWithDeps](c, "userService")
	require.NoError(t, err)
	assert.Equal(t, "postgres://localhost", svc.db.connStr)
}

func TestRegisterSingletonWith_MultipleDependencies(t *testing.T) {
	c := NewContainer()

	// Register dependencies
	_ = RegisterSingleton(c, "db", func(c Container) (*dbService, error) {
		return &dbService{connStr: "multi-db"}, nil
	})
	_ = RegisterSingleton(c, "logger", func(c Container) (*logService, error) {
		return &logService{prefix: "[APP]"}, nil
	})

	// Register with multiple dependencies
	err := RegisterSingletonWith[*userServiceWithDeps](c, "userService",
		Inject[*dbService]("db"),
		Inject[*logService]("logger"),
		func(db *dbService, logger *logService) (*userServiceWithDeps, error) {
			return &userServiceWithDeps{db: db, logger: logger}, nil
		},
	)
	require.NoError(t, err)

	svc, err := Resolve[*userServiceWithDeps](c, "userService")
	require.NoError(t, err)
	assert.Equal(t, "multi-db", svc.db.connStr)
	assert.Equal(t, "[APP]", svc.logger.prefix)
}

func TestRegisterSingletonWith_LazyDependency(t *testing.T) {
	c := NewContainer()

	dbResolved := false
	_ = RegisterSingleton(c, "db", func(c Container) (*dbService, error) {
		dbResolved = true
		return &dbService{connStr: "lazy-db"}, nil
	})

	// Register with lazy injection
	err := RegisterSingletonWith[*userServiceWithDeps](c, "userService",
		LazyInject[*dbService]("db"),
		func(db *LazyAny) (*userServiceWithDeps, error) {
			return &userServiceWithDeps{}, nil
		},
	)
	require.NoError(t, err)

	// Resolve user service
	_, err = Resolve[*userServiceWithDeps](c, "userService")
	require.NoError(t, err)

	// DB should NOT be resolved yet (lazy)
	assert.False(t, dbResolved)
}

func TestRegisterSingletonWith_OptionalDependency_Found(t *testing.T) {
	c := NewContainer()

	_ = RegisterSingleton(c, "db", func(c Container) (*dbService, error) {
		return &dbService{connStr: "optional-found"}, nil
	})

	var resolvedDB *dbService

	err := RegisterSingletonWith[*userServiceWithDeps](c, "userService",
		OptionalInject[*dbService]("db"),
		func(db *dbService) (*userServiceWithDeps, error) {
			resolvedDB = db
			return &userServiceWithDeps{db: db}, nil
		},
	)
	require.NoError(t, err)

	_, err = Resolve[*userServiceWithDeps](c, "userService")
	require.NoError(t, err)

	assert.NotNil(t, resolvedDB)
	assert.Equal(t, "optional-found", resolvedDB.connStr)
}

func TestRegisterSingletonWith_OptionalDependency_NotFound(t *testing.T) {
	c := NewContainer()

	var resolvedDB *dbService

	err := RegisterSingletonWith[*userServiceWithDeps](c, "userService",
		OptionalInject[*dbService]("db"), // Not registered
		func(db *dbService) (*userServiceWithDeps, error) {
			resolvedDB = db
			return &userServiceWithDeps{db: db}, nil
		},
	)
	require.NoError(t, err)

	_, err = Resolve[*userServiceWithDeps](c, "userService")
	require.NoError(t, err)

	// Should be nil since db is not registered
	assert.Nil(t, resolvedDB)
}

func TestRegisterTransientWith_Basic(t *testing.T) {
	c := NewContainer()

	counter := 0
	_ = RegisterSingleton(c, "db", func(c Container) (*dbService, error) {
		return &dbService{connStr: "transient-db"}, nil
	})

	err := RegisterTransientWith[*userServiceWithDeps](c, "userService",
		Inject[*dbService]("db"),
		func(db *dbService) (*userServiceWithDeps, error) {
			counter++
			return &userServiceWithDeps{db: db}, nil
		},
	)
	require.NoError(t, err)

	// Resolve twice - should create two instances
	_, err = Resolve[*userServiceWithDeps](c, "userService")
	require.NoError(t, err)
	_, err = Resolve[*userServiceWithDeps](c, "userService")
	require.NoError(t, err)

	assert.Equal(t, 2, counter)
}

func TestRegisterScopedWith_Basic(t *testing.T) {
	c := NewContainer()

	_ = RegisterSingleton(c, "db", func(c Container) (*dbService, error) {
		return &dbService{connStr: "scoped-db"}, nil
	})

	err := RegisterScopedWith[*userServiceWithDeps](c, "userService",
		Inject[*dbService]("db"),
		func(db *dbService) (*userServiceWithDeps, error) {
			return &userServiceWithDeps{db: db}, nil
		},
	)
	require.NoError(t, err)

	// Create scope and resolve
	scope := c.BeginScope()
	defer scope.End()

	svc, err := ResolveScope[*userServiceWithDeps](scope, "userService")
	require.NoError(t, err)
	assert.Equal(t, "scoped-db", svc.db.connStr)
}

func TestRegisterSingletonWith_MissingDependency(t *testing.T) {
	c := NewContainer()

	err := RegisterSingletonWith[*userServiceWithDeps](c, "userService",
		Inject[*dbService]("db"), // Not registered
		func(db *dbService) (*userServiceWithDeps, error) {
			return &userServiceWithDeps{db: db}, nil
		},
	)
	require.NoError(t, err)

	// Should fail when resolving because dependency is missing
	_, err = Resolve[*userServiceWithDeps](c, "userService")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db")
}

func TestRegisterSingletonWith_NoFactory(t *testing.T) {
	c := NewContainer()

	err := RegisterSingletonWith[*userServiceWithDeps](c, "userService",
		Inject[*dbService]("db"),
		// No factory function
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no factory function")
}

func TestRegisterSingletonWith_SingleReturnFactory(t *testing.T) {
	c := NewContainer()

	_ = RegisterSingleton(c, "db", func(c Container) (*dbService, error) {
		return &dbService{connStr: "single-return"}, nil
	})

	// Factory that returns only the service (no error)
	err := RegisterSingletonWith[*userServiceWithDeps](c, "userService",
		Inject[*dbService]("db"),
		func(db *dbService) *userServiceWithDeps {
			return &userServiceWithDeps{db: db}
		},
	)
	require.NoError(t, err)

	svc, err := Resolve[*userServiceWithDeps](c, "userService")
	require.NoError(t, err)
	assert.Equal(t, "single-return", svc.db.connStr)
}
