package di

import (
	errors2 "github.com/xraph/forge/errors"
)

// DependencyGraph manages service dependencies.
type DependencyGraph struct {
	nodes map[string]*node
	order []string // Preserve registration order
}

type node struct {
	name         string
	dependencies []string
}

// NewDependencyGraph creates a new dependency graph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*node),
		order: make([]string, 0),
	}
}

// AddNode adds a node with its dependencies.
// Nodes are processed in the order they are added (FIFO) when no dependencies exist.
func (g *DependencyGraph) AddNode(name string, dependencies []string) {
	g.nodes[name] = &node{
		name:         name,
		dependencies: dependencies,
	}
	g.order = append(g.order, name)
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
