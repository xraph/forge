package di

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors2 "github.com/xraph/forge/errors"
)

func TestDependencyGraph_TopologicalSort_Simple(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode("a", nil)
	g.AddNode("b", []string{"a"})
	g.AddNode("c", []string{"b"})

	result, err := g.TopologicalSort()
	require.NoError(t, err)

	// Should be in dependency order: a, b, c
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestDependencyGraph_TopologicalSort_Complex(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode("a", nil)
	g.AddNode("b", []string{"a"})
	g.AddNode("c", []string{"a"})
	g.AddNode("d", []string{"b", "c"})

	result, err := g.TopologicalSort()
	require.NoError(t, err)

	// "a" must come before "b" and "c"
	// "b" and "c" must come before "d"
	aIdx := indexOf(result, "a")
	bIdx := indexOf(result, "b")
	cIdx := indexOf(result, "c")
	dIdx := indexOf(result, "d")

	assert.Less(t, aIdx, bIdx)
	assert.Less(t, aIdx, cIdx)
	assert.Less(t, bIdx, dIdx)
	assert.Less(t, cIdx, dIdx)
}

func TestDependencyGraph_TopologicalSort_CircularDependency(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode("a", []string{"b"})
	g.AddNode("b", []string{"a"})

	_, err := g.TopologicalSort()
	assert.ErrorIs(t, err, errors2.ErrCircularDependencySentinel)
}

func TestDependencyGraph_TopologicalSort_SelfReference(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode("a", []string{"a"})

	_, err := g.TopologicalSort()
	assert.ErrorIs(t, err, errors2.ErrCircularDependencySentinel)
}

func TestDependencyGraph_TopologicalSort_MissingDependency(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode("a", []string{"nonexistent"})

	// Should not error - missing dependencies are skipped
	result, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, result)
}

func TestDependencyGraph_TopologicalSort_Empty(t *testing.T) {
	g := NewDependencyGraph()

	result, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestDependencyGraph_TopologicalSort_PreservesRegistrationOrder(t *testing.T) {
	// Test that nodes without dependencies maintain registration order (FIFO)
	g := NewDependencyGraph()
	
	// Add nodes in specific order without dependencies
	g.AddNode("first", nil)
	g.AddNode("second", nil)
	g.AddNode("third", nil)
	g.AddNode("fourth", nil)

	result, err := g.TopologicalSort()
	require.NoError(t, err)

	// Should maintain registration order (FIFO) for nodes without dependencies
	assert.Equal(t, []string{"first", "second", "third", "fourth"}, result)
}

func TestDependencyGraph_TopologicalSort_MixedDependenciesAndOrder(t *testing.T) {
	// Test that dependency constraints are respected while preserving registration order for independent nodes
	g := NewDependencyGraph()
	
	// Add nodes with mixed dependencies
	g.AddNode("independent1", nil)    // No deps - position 0
	g.AddNode("dependent", []string{"base"}) // Depends on base
	g.AddNode("base", nil)            // No deps - position 2
	g.AddNode("independent2", nil)    // No deps - position 3

	result, err := g.TopologicalSort()
	require.NoError(t, err)

	// Verify constraints:
	// 1. "base" must come before "dependent"
	baseIdx := indexOf(result, "base")
	dependentIdx := indexOf(result, "dependent")
	assert.Less(t, baseIdx, dependentIdx, "base must come before dependent")

	// 2. Independent nodes without shared dependencies should maintain relative registration order
	ind1Idx := indexOf(result, "independent1")
	ind2Idx := indexOf(result, "independent2")
	assert.Less(t, ind1Idx, ind2Idx, "independent1 registered before independent2, should maintain order")
}

func TestDependencyGraph_Visit_AlreadyVisited(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNode("a", nil)

	visited := map[string]bool{"a": true}
	visiting := make(map[string]bool)
	result := []string{}

	err := g.visit("a", visited, visiting, &result)
	assert.NoError(t, err)
	assert.Empty(t, result) // Should not add again
}

// Helper function.
func indexOf(slice []string, value string) int {
	for i, v := range slice {
		if v == value {
			return i
		}
	}

	return -1
}
