package di

import (
	errors2 "github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/shared"
)

// DependencyGraph manages service dependencies.
type DependencyGraph struct {
	nodes map[string]*node
	order []string // Preserve registration order
}

type node struct {
	name         string
	dependencies []string    // Backward compatible: just names
	deps         []shared.Dep // New: full dependency specs with modes
}

// NewDependencyGraph creates a new dependency graph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*node),
		order: make([]string, 0),
	}
}

// AddNode adds a node with its dependencies (string-based, backward compatible).
// Nodes are processed in the order they are added (FIFO) when no dependencies exist.
func (g *DependencyGraph) AddNode(name string, dependencies []string) {
	g.nodes[name] = &node{
		name:         name,
		dependencies: dependencies,
		deps:         shared.DepsFromNames(dependencies), // Convert to Dep specs
	}
	g.order = append(g.order, name)
}

// AddNodeWithDeps adds a node with full Dep specs.
// This is the new API that supports lazy/optional dependencies.
func (g *DependencyGraph) AddNodeWithDeps(name string, deps []shared.Dep) {
	g.nodes[name] = &node{
		name:         name,
		dependencies: shared.DepNames(deps), // Keep string names for backward compat
		deps:         deps,
	}
	g.order = append(g.order, name)
}

// GetDependencies returns the dependency names for a node.
func (g *DependencyGraph) GetDependencies(name string) []string {
	if node, ok := g.nodes[name]; ok {
		return node.dependencies
	}
	return nil
}

// GetDeps returns the full Dep specs for a node.
func (g *DependencyGraph) GetDeps(name string) []shared.Dep {
	if node, ok := g.nodes[name]; ok {
		return node.deps
	}
	return nil
}

// GetEagerDependencies returns only the eager (non-lazy) dependencies.
// These are the ones that must be resolved before the service can be created.
func (g *DependencyGraph) GetEagerDependencies(name string) []string {
	if node, ok := g.nodes[name]; ok {
		var eager []string
		for _, dep := range node.deps {
			if !dep.Mode.IsLazy() {
				eager = append(eager, dep.Name)
			}
		}
		return eager
	}
	return nil
}

// HasNode checks if a node exists in the graph.
func (g *DependencyGraph) HasNode(name string) bool {
	_, ok := g.nodes[name]
	return ok
}

// TopologicalSort returns nodes in dependency order.
// Nodes without dependencies maintain their registration order (FIFO).
// Returns error if circular dependency detected.
func (g *DependencyGraph) TopologicalSort() ([]string, error) {
	// Track visited nodes
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	result := make([]string, 0, len(g.nodes))

	// Visit nodes in registration order to preserve FIFO for nodes without dependencies
	for _, name := range g.order {
		if err := g.visit(name, visited, visiting, &result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// TopologicalSortEagerOnly returns nodes sorted considering only eager dependencies.
// Lazy dependencies are excluded from the ordering since they're resolved on-demand.
func (g *DependencyGraph) TopologicalSortEagerOnly() ([]string, error) {
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	result := make([]string, 0, len(g.nodes))

	for _, name := range g.order {
		if err := g.visitEagerOnly(name, visited, visiting, &result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// visit performs DFS traversal.
func (g *DependencyGraph) visit(name string, visited, visiting map[string]bool, result *[]string) error {
	if visited[name] {
		return nil
	}

	if visiting[name] {
		// Build the cycle chain for better error message
		cycle := []string{name}

		return errors2.ErrCircularDependency(cycle)
	}

	node := g.nodes[name]
	if node == nil {
		// Node not in graph, skip (may be optional dependency)
		return nil
	}

	visiting[name] = true

	// Visit dependencies first
	for _, dep := range node.dependencies {
		if err := g.visit(dep, visited, visiting, result); err != nil {
			return err
		}
	}

	visiting[name] = false
	visited[name] = true
	*result = append(*result, name)

	return nil
}

// visitEagerOnly performs DFS traversal considering only eager dependencies.
func (g *DependencyGraph) visitEagerOnly(name string, visited, visiting map[string]bool, result *[]string) error {
	if visited[name] {
		return nil
	}

	if visiting[name] {
		cycle := []string{name}
		return errors2.ErrCircularDependency(cycle)
	}

	node := g.nodes[name]
	if node == nil {
		return nil
	}

	visiting[name] = true

	// Visit only eager (non-lazy) dependencies
	for _, dep := range node.deps {
		if !dep.Mode.IsLazy() {
			if err := g.visitEagerOnly(dep.Name, visited, visiting, result); err != nil {
				return err
			}
		}
	}

	visiting[name] = false
	visited[name] = true
	*result = append(*result, name)

	return nil
}
