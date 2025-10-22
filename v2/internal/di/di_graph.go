package di

import (
	errors2 "github.com/xraph/forge/v2/internal/errors"
)

// DependencyGraph manages service dependencies
type DependencyGraph struct {
	nodes map[string]*node
}

type node struct {
	name         string
	dependencies []string
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*node),
	}
}

// AddNode adds a node with its dependencies
func (g *DependencyGraph) AddNode(name string, dependencies []string) {
	g.nodes[name] = &node{
		name:         name,
		dependencies: dependencies,
	}
}

// TopologicalSort returns nodes in dependency order
// Returns error if circular dependency detected
func (g *DependencyGraph) TopologicalSort() ([]string, error) {
	// Track visited nodes
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	result := make([]string, 0, len(g.nodes))

	// Visit each node
	for name := range g.nodes {
		if err := g.visit(name, visited, visiting, &result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// visit performs DFS traversal
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
