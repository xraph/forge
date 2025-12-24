package di

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/shared"
)

type userService struct {
	db     *database
	logger *loggerService
	cache  *cacheService
}

type database struct {
	name string
}

func (d *database) Name() string { return "database" }
func (d *database) Start(_ context.Context) error { return nil }
func (d *database) Stop(_ context.Context) error { return nil }

type loggerService struct {
	prefix string
}

type cacheService struct {
	size int
}

func TestProvide_Basic(t *testing.T) {
	c := newContainerImpl()

	// Register dependencies first
	err := c.Register("db", func(_ Container) (any, error) {
		return &database{name: "test-db"}, nil
	})
	require.NoError(t, err)

	// Use Provide to register a service with typed injection
	err = Provide[*userService](c, "userService",
		Inject[*database]("db"),
		func(db *database) (*userService, error) {
			return &userService{db: db}, nil
		},
	)
	require.NoError(t, err)

	// Resolve the service
	svc, err := c.Resolve("userService")
	require.NoError(t, err)

	us := svc.(*userService)
	assert.Equal(t, "test-db", us.db.name)
}

func TestProvide_MultipleDependencies(t *testing.T) {
	c := newContainerImpl()

	// Register dependencies
	_ = c.Register("db", func(_ Container) (any, error) {
		return &database{name: "multi-db"}, nil
	})
	_ = c.Register("logger", func(_ Container) (any, error) {
		return &loggerService{prefix: "[APP]"}, nil
	})

	// Use Provide with multiple dependencies
	err := Provide[*userService](c, "userService",
		Inject[*database]("db"),
		Inject[*loggerService]("logger"),
		func(db *database, logger *loggerService) (*userService, error) {
			return &userService{db: db, logger: logger}, nil
		},
	)
	require.NoError(t, err)

	svc, err := c.Resolve("userService")
	require.NoError(t, err)

	us := svc.(*userService)
	assert.Equal(t, "multi-db", us.db.name)
	assert.Equal(t, "[APP]", us.logger.prefix)
}

func TestProvide_LazyDependency(t *testing.T) {
	c := newContainerImpl()

	cacheResolved := false
	_ = c.Register("cache", func(_ Container) (any, error) {
		cacheResolved = true
		return &cacheService{size: 100}, nil
	})

	// Use Provide with a lazy dependency
	err := Provide[*userService](c, "userService",
		LazyInject[*cacheService]("cache"),
		func(cache *LazyAny) (*userService, error) {
			return &userService{}, nil
		},
	)
	require.NoError(t, err)

	// Resolve the user service
	_, err = c.Resolve("userService")
	require.NoError(t, err)

	// Cache should NOT be resolved yet (lazy)
	assert.False(t, cacheResolved)
}

func TestProvide_OptionalDependency_Found(t *testing.T) {
	c := newContainerImpl()

	_ = c.Register("cache", func(_ Container) (any, error) {
		return &cacheService{size: 200}, nil
	})

	var resolvedCache *cacheService

	err := Provide[*userService](c, "userService",
		OptionalInject[*cacheService]("cache"),
		func(cache *cacheService) (*userService, error) {
			resolvedCache = cache
			return &userService{cache: cache}, nil
		},
	)
	require.NoError(t, err)

	_, err = c.Resolve("userService")
	require.NoError(t, err)

	assert.NotNil(t, resolvedCache)
	assert.Equal(t, 200, resolvedCache.size)
}

func TestProvide_OptionalDependency_NotFound(t *testing.T) {
	c := newContainerImpl()

	var resolvedCache *cacheService

	err := Provide[*userService](c, "userService",
		OptionalInject[*cacheService]("cache"), // Not registered
		func(cache *cacheService) (*userService, error) {
			resolvedCache = cache
			return &userService{cache: cache}, nil
		},
	)
	require.NoError(t, err)

	_, err = c.Resolve("userService")
	require.NoError(t, err)

	// Should be nil since cache is not registered
	assert.Nil(t, resolvedCache)
}

func TestProvide_FactoryError(t *testing.T) {
	c := newContainerImpl()

	_ = c.Register("db", func(_ Container) (any, error) {
		return &database{name: "error-db"}, nil
	})

	expectedErr := errors.New("factory failed")

	err := Provide[*userService](c, "userService",
		Inject[*database]("db"),
		func(db *database) (*userService, error) {
			return nil, expectedErr
		},
	)
	require.NoError(t, err)

	_, err = c.Resolve("userService")
	assert.Error(t, err)
}

func TestProvide_MissingEagerDependency(t *testing.T) {
	c := newContainerImpl()

	// Register service with missing eager dependency
	err := Provide[*userService](c, "userService",
		Inject[*database]("db"), // Not registered
		func(db *database) (*userService, error) {
			return &userService{db: db}, nil
		},
	)
	require.NoError(t, err)

	// Should fail when resolving because eager dependency is missing
	_, err = c.Resolve("userService")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db")
}

func TestProvide_NoFactory(t *testing.T) {
	c := newContainerImpl()

	err := Provide[*userService](c, "userService",
		Inject[*database]("db"),
		// No factory function
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no factory function")
}

func TestProvide_MultipleFactories(t *testing.T) {
	c := newContainerImpl()

	err := Provide[*userService](c, "userService",
		func() (*userService, error) { return nil, nil },
		func() (*userService, error) { return nil, nil },
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiple factory")
}

func TestProvide_FactorySingleReturn(t *testing.T) {
	c := newContainerImpl()

	_ = c.Register("db", func(_ Container) (any, error) {
		return &database{name: "single-return"}, nil
	})

	// Factory that returns only the service (no error)
	err := Provide[*userService](c, "userService",
		Inject[*database]("db"),
		func(db *database) *userService {
			return &userService{db: db}
		},
	)
	require.NoError(t, err)

	svc, err := c.Resolve("userService")
	require.NoError(t, err)
	assert.Equal(t, "single-return", svc.(*userService).db.name)
}

func TestProvideWithOpts_Transient(t *testing.T) {
	c := newContainerImpl()

	counter := 0
	_ = c.Register("db", func(_ Container) (any, error) {
		return &database{name: "transient-db"}, nil
	})

	err := ProvideWithOpts[*userService](c, "userService",
		[]shared.RegisterOption{shared.Transient()},
		Inject[*database]("db"),
		func(db *database) (*userService, error) {
			counter++
			return &userService{db: db}, nil
		},
	)
	require.NoError(t, err)

	// Resolve twice - should create two instances
	_, err = c.Resolve("userService")
	require.NoError(t, err)
	_, err = c.Resolve("userService")
	require.NoError(t, err)

	assert.Equal(t, 2, counter)
}

