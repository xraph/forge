package di

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/shared"
)

type lazyTestService struct {
	svcName string
}

func (s *lazyTestService) Name() string {
	return s.svcName
}

func (s *lazyTestService) Start(_ context.Context) error {
	return nil
}

func (s *lazyTestService) Stop(_ context.Context) error {
	return nil
}

func TestLazy_Get(t *testing.T) {
	c := newContainerImpl()

	// Register a service
	err := c.Register("test", func(c Container) (any, error) {
		return &lazyTestService{svcName: "test-service"}, nil
	})
	require.NoError(t, err)

	// Create lazy wrapper
	lazy := NewLazy[*lazyTestService](c, "test")

	// Should not be resolved yet
	assert.False(t, lazy.IsResolved())

	// Get the service
	svc, err := lazy.Get()
	require.NoError(t, err)
	assert.Equal(t, "test-service", svc.Name())

	// Should be resolved now
	assert.True(t, lazy.IsResolved())

	// Calling Get again should return the same instance
	svc2, err := lazy.Get()
	require.NoError(t, err)
	assert.Same(t, svc, svc2)
}

func TestLazy_MustGet(t *testing.T) {
	c := newContainerImpl()

	err := c.Register("test", func(c Container) (any, error) {
		return &lazyTestService{svcName: "must-get"}, nil
	})
	require.NoError(t, err)

	lazy := NewLazy[*lazyTestService](c, "test")
	svc := lazy.MustGet()
	assert.Equal(t, "must-get", svc.Name())
}

func TestLazy_MustGet_Panic(t *testing.T) {
	c := newContainerImpl()

	// Create lazy wrapper for non-existent service
	lazy := NewLazy[*lazyTestService](c, "non-existent")

	assert.Panics(t, func() {
		lazy.MustGet()
	})
}

func TestLazy_TypeMismatch(t *testing.T) {
	c := newContainerImpl()

	// Register a string service
	err := c.Register("test", func(c Container) (any, error) {
		return "not a service", nil
	})
	require.NoError(t, err)

	// Create lazy wrapper expecting a different type
	lazy := NewLazy[*lazyTestService](c, "test")

	_, err = lazy.Get()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected type")
}

func TestOptionalLazy_Get_Found(t *testing.T) {
	c := newContainerImpl()

	err := c.Register("test", func(c Container) (any, error) {
		return &lazyTestService{svcName: "optional-found"}, nil
	})
	require.NoError(t, err)

	lazy := NewOptionalLazy[*lazyTestService](c, "test")

	assert.False(t, lazy.IsResolved())

	svc, err := lazy.Get()
	require.NoError(t, err)
	assert.Equal(t, "optional-found", svc.Name())
	assert.True(t, lazy.IsResolved())
	assert.True(t, lazy.IsFound())
}

func TestOptionalLazy_Get_NotFound(t *testing.T) {
	c := newContainerImpl()

	lazy := NewOptionalLazy[*lazyTestService](c, "non-existent")

	svc, err := lazy.Get()
	require.NoError(t, err)
	assert.Nil(t, svc)
	assert.True(t, lazy.IsResolved())
	assert.False(t, lazy.IsFound())
}

func TestOptionalLazy_MustGet(t *testing.T) {
	c := newContainerImpl()

	err := c.Register("test", func(c Container) (any, error) {
		return &lazyTestService{svcName: "optional-must"}, nil
	})
	require.NoError(t, err)

	lazy := NewOptionalLazy[*lazyTestService](c, "test")
	svc := lazy.MustGet()
	assert.Equal(t, "optional-must", svc.Name())
}

func TestOptionalLazy_MustGet_NotFound(t *testing.T) {
	c := newContainerImpl()

	lazy := NewOptionalLazy[*lazyTestService](c, "non-existent")

	// Should not panic even when not found
	assert.NotPanics(t, func() {
		svc := lazy.MustGet()
		assert.Nil(t, svc)
	})
}

func TestProvider_Provide(t *testing.T) {
	c := newContainerImpl()

	counter := 0
	err := c.Register("test", func(c Container) (any, error) {
		counter++
		return &lazyTestService{svcName: "provider-" + string(rune('0'+counter))}, nil
	}, shared.Transient())
	require.NoError(t, err)

	provider := NewProvider[*lazyTestService](c, "test")

	// Each call should create a new instance
	svc1, err := provider.Provide()
	require.NoError(t, err)

	svc2, err := provider.Provide()
	require.NoError(t, err)

	// Instances should be different
	assert.NotSame(t, svc1, svc2)
	assert.Equal(t, 2, counter)
}

func TestProvider_MustProvide(t *testing.T) {
	c := newContainerImpl()

	err := c.Register("test", func(c Container) (any, error) {
		return &lazyTestService{svcName: "must-provide"}, nil
	}, shared.Transient())
	require.NoError(t, err)

	provider := NewProvider[*lazyTestService](c, "test")
	svc := provider.MustProvide()
	assert.Equal(t, "must-provide", svc.Name())
}

func TestProvider_MustProvide_Panic(t *testing.T) {
	c := newContainerImpl()

	provider := NewProvider[*lazyTestService](c, "non-existent")

	assert.Panics(t, func() {
		provider.MustProvide()
	})
}

