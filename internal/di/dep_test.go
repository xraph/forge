package di

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/shared"
)

func TestDepMode_String(t *testing.T) {
	tests := []struct {
		mode     shared.DepMode
		expected string
	}{
		{shared.DepEager, "eager"},
		{shared.DepLazy, "lazy"},
		{shared.DepOptional, "optional"},
		{shared.DepLazyOptional, "lazy_optional"},
		{shared.DepMode(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.mode.String())
	}
}

func TestDepMode_IsLazy(t *testing.T) {
	assert.False(t, shared.DepEager.IsLazy())
	assert.True(t, shared.DepLazy.IsLazy())
	assert.False(t, shared.DepOptional.IsLazy())
	assert.True(t, shared.DepLazyOptional.IsLazy())
}

func TestDepMode_IsOptional(t *testing.T) {
	assert.False(t, shared.DepEager.IsOptional())
	assert.False(t, shared.DepLazy.IsOptional())
	assert.True(t, shared.DepOptional.IsOptional())
	assert.True(t, shared.DepLazyOptional.IsOptional())
}

func TestDep_Helpers(t *testing.T) {
	eager := shared.Eager("db")
	assert.Equal(t, "db", eager.Name)
	assert.Equal(t, shared.DepEager, eager.Mode)

	lazy := shared.Lazy("cache")
	assert.Equal(t, "cache", lazy.Name)
	assert.Equal(t, shared.DepLazy, lazy.Mode)

	optional := shared.Optional("tracer")
	assert.Equal(t, "tracer", optional.Name)
	assert.Equal(t, shared.DepOptional, optional.Mode)

	lazyOptional := shared.LazyOptional("analytics")
	assert.Equal(t, "analytics", lazyOptional.Name)
	assert.Equal(t, shared.DepLazyOptional, lazyOptional.Mode)
}

func TestDepNames(t *testing.T) {
	deps := []shared.Dep{
		shared.Eager("db"),
		shared.Lazy("cache"),
		shared.Optional("tracer"),
	}

	names := shared.DepNames(deps)
	assert.Equal(t, []string{"db", "cache", "tracer"}, names)
}

func TestDepsFromNames(t *testing.T) {
	names := []string{"db", "cache", "logger"}
	deps := shared.DepsFromNames(names)

	require.Len(t, deps, 3)
	for i, dep := range deps {
		assert.Equal(t, names[i], dep.Name)
		assert.Equal(t, shared.DepEager, dep.Mode) // All should be eager
	}
}

func TestDependencyGraph_AddNodeWithDeps(t *testing.T) {
	graph := NewDependencyGraph()

	deps := []shared.Dep{
		shared.Eager("db"),
		shared.Lazy("cache"),
		shared.Optional("tracer"),
	}

	graph.AddNodeWithDeps("service", deps)

	// Should have stored the deps
	retrievedDeps := graph.GetDeps("service")
	require.Len(t, retrievedDeps, 3)
	assert.Equal(t, "db", retrievedDeps[0].Name)
	assert.Equal(t, shared.DepEager, retrievedDeps[0].Mode)
	assert.Equal(t, "cache", retrievedDeps[1].Name)
	assert.Equal(t, shared.DepLazy, retrievedDeps[1].Mode)
}

func TestDependencyGraph_GetEagerDependencies(t *testing.T) {
	graph := NewDependencyGraph()

	deps := []shared.Dep{
		shared.Eager("db"),
		shared.Lazy("cache"),
		shared.Optional("tracer"),
		shared.LazyOptional("analytics"),
	}

	graph.AddNodeWithDeps("service", deps)

	eagerDeps := graph.GetEagerDependencies("service")
	// Only eager and optional (non-lazy) should be returned
	require.Len(t, eagerDeps, 2)
	assert.Contains(t, eagerDeps, "db")
	assert.Contains(t, eagerDeps, "tracer")
}

func TestDependencyGraph_TopologicalSortEagerOnly(t *testing.T) {
	graph := NewDependencyGraph()

	// Service A depends on B (eager) and C (lazy)
	graph.AddNodeWithDeps("A", []shared.Dep{
		shared.Eager("B"),
		shared.Lazy("C"),
	})

	// B has no deps
	graph.AddNode("B", nil)

	// C depends on D (eager)
	graph.AddNodeWithDeps("C", []shared.Dep{
		shared.Eager("D"),
	})

	// D has no deps
	graph.AddNode("D", nil)

	// Eager-only sort should not include C->D dependency
	order, err := graph.TopologicalSortEagerOnly()
	require.NoError(t, err)

	// B should come before A (eager dependency)
	bIdx := sliceIndexOf(order, "B")
	aIdx := sliceIndexOf(order, "A")
	assert.True(t, bIdx < aIdx, "B should come before A")

	// D should come before C (C depends on D eagerly)
	dIdx := sliceIndexOf(order, "D")
	cIdx := sliceIndexOf(order, "C")
	assert.True(t, dIdx < cIdx, "D should come before C")
}

func TestDependencyGraph_HasNode(t *testing.T) {
	graph := NewDependencyGraph()

	graph.AddNode("service", nil)

	assert.True(t, graph.HasNode("service"))
	assert.False(t, graph.HasNode("non-existent"))
}

func TestRegisterOption_GetAllDeps(t *testing.T) {
	// Test with both old and new style dependencies
	opt := shared.RegisterOption{
		Dependencies: []string{"legacy1", "legacy2"},
		Deps: []shared.Dep{
			shared.Eager("new1"),
			shared.Lazy("new2"),
		},
	}

	allDeps := opt.GetAllDeps()
	require.Len(t, allDeps, 4)

	// New deps should come first
	assert.Equal(t, "new1", allDeps[0].Name)
	assert.Equal(t, "new2", allDeps[1].Name)

	// Legacy deps should be converted to eager
	assert.Equal(t, "legacy1", allDeps[2].Name)
	assert.Equal(t, shared.DepEager, allDeps[2].Mode)
	assert.Equal(t, "legacy2", allDeps[3].Name)
	assert.Equal(t, shared.DepEager, allDeps[3].Mode)
}

func TestRegisterOption_GetAllDepNames(t *testing.T) {
	opt := shared.RegisterOption{
		Dependencies: []string{"legacy1"},
		Deps: []shared.Dep{
			shared.Eager("new1"),
		},
	}

	names := opt.GetAllDepNames()
	require.Len(t, names, 2)
	assert.Contains(t, names, "new1")
	assert.Contains(t, names, "legacy1")
}

func TestContainer_RegisterWithDeps(t *testing.T) {
	c := newContainerImpl()

	// Register using the new WithDeps option
	err := c.Register("service", func(_ Container) (any, error) {
		return "test", nil
	}, shared.WithDeps(
		shared.Eager("dep1"),
		shared.Lazy("dep2"),
	))
	require.NoError(t, err)

	// Check the service info
	info := c.Inspect("service")
	require.Len(t, info.Deps, 2)
	assert.Equal(t, "dep1", info.Deps[0].Name)
	assert.Equal(t, shared.DepEager, info.Deps[0].Mode)
	assert.Equal(t, "dep2", info.Deps[1].Name)
	assert.Equal(t, shared.DepLazy, info.Deps[1].Mode)
}

func sliceIndexOf(slice []string, item string) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}

