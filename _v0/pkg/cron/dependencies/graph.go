package dependencies

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/xraph/forge/v0/pkg/common"
)

// DependencyGraph represents a directed acyclic graph (DAG) of job dependencies
type DependencyGraph struct {
	// Nodes in the graph
	nodes map[string]*GraphNode

	// Edges represent dependencies (job -> dependencies)
	edges map[string][]string

	// Reverse edges for efficient dependent lookup (dependency -> dependents)
	reverseEdges map[string][]string

	// Concurrency control
	mu sync.RWMutex
}

// GraphNode represents a node in the dependency graph
type GraphNode struct {
	ID           string
	Dependencies []string
	Dependents   []string
	Level        int  // Topological level in the graph
	Visited      bool // Used for cycle detection
	InStack      bool // Used for cycle detection
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes:        make(map[string]*GraphNode),
		edges:        make(map[string][]string),
		reverseEdges: make(map[string][]string),
	}
}

// AddJob adds a job and its dependencies to the graph
func (dg *DependencyGraph) AddJob(jobID string, dependencies []string) error {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	// Check if job already exists
	if _, exists := dg.nodes[jobID]; exists {
		return common.ErrServiceAlreadyExists(jobID)
	}

	// Create node
	node := &GraphNode{
		ID:           jobID,
		Dependencies: make([]string, len(dependencies)),
		Dependents:   make([]string, 0),
		Level:        -1,
	}
	copy(node.Dependencies, dependencies)

	// Add to nodes
	dg.nodes[jobID] = node

	// Add edges
	dg.edges[jobID] = make([]string, len(dependencies))
	copy(dg.edges[jobID], dependencies)

	// Add reverse edges
	for _, dep := range dependencies {
		if dg.reverseEdges[dep] == nil {
			dg.reverseEdges[dep] = make([]string, 0)
		}
		dg.reverseEdges[dep] = append(dg.reverseEdges[dep], jobID)

		// Update dependent's dependents list
		if depNode, exists := dg.nodes[dep]; exists {
			depNode.Dependents = append(depNode.Dependents, jobID)
		}
	}

	// Recalculate levels
	dg.calculateLevels()

	return nil
}

// RemoveJob removes a job from the graph
func (dg *DependencyGraph) RemoveJob(jobID string) error {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	node, exists := dg.nodes[jobID]
	if !exists {
		return common.ErrServiceNotFound(jobID)
	}

	// Remove from dependencies' dependents
	for _, dep := range node.Dependencies {
		if dependents, exists := dg.reverseEdges[dep]; exists {
			dg.reverseEdges[dep] = removeFromSlice(dependents, jobID)
		}

		// Update dependency node's dependents list
		if depNode, exists := dg.nodes[dep]; exists {
			depNode.Dependents = removeFromSlice(depNode.Dependents, jobID)
		}
	}

	// Remove from dependents' dependencies
	for _, dependent := range node.Dependents {
		if dependencies, exists := dg.edges[dependent]; exists {
			dg.edges[dependent] = removeFromSlice(dependencies, jobID)
		}

		// Update dependent node's dependencies list
		if depNode, exists := dg.nodes[dependent]; exists {
			depNode.Dependencies = removeFromSlice(depNode.Dependencies, jobID)
		}
	}

	// Remove from graph
	delete(dg.nodes, jobID)
	delete(dg.edges, jobID)
	delete(dg.reverseEdges, jobID)

	// Recalculate levels
	dg.calculateLevels()

	return nil
}

// GetDependencies returns the dependencies of a job
func (dg *DependencyGraph) GetDependencies(jobID string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	if dependencies, exists := dg.edges[jobID]; exists {
		result := make([]string, len(dependencies))
		copy(result, dependencies)
		return result
	}

	return nil
}

// GetDependents returns the dependents of a job
func (dg *DependencyGraph) GetDependents(jobID string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	if dependents, exists := dg.reverseEdges[jobID]; exists {
		result := make([]string, len(dependents))
		copy(result, dependents)
		return result
	}

	return nil
}

// GetAllDependencies returns all dependencies of a job (including transitive)
func (dg *DependencyGraph) GetAllDependencies(jobID string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	visited := make(map[string]bool)
	var result []string

	dg.collectAllDependencies(jobID, visited, &result)

	return result
}

// GetAllDependents returns all dependents of a job (including transitive)
func (dg *DependencyGraph) GetAllDependents(jobID string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	visited := make(map[string]bool)
	var result []string

	dg.collectAllDependents(jobID, visited, &result)

	return result
}

// collectAllDependencies recursively collects all dependencies
func (dg *DependencyGraph) collectAllDependencies(jobID string, visited map[string]bool, result *[]string) {
	if visited[jobID] {
		return
	}

	visited[jobID] = true

	if dependencies, exists := dg.edges[jobID]; exists {
		for _, dep := range dependencies {
			*result = append(*result, dep)
			dg.collectAllDependencies(dep, visited, result)
		}
	}
}

// collectAllDependents recursively collects all dependents
func (dg *DependencyGraph) collectAllDependents(jobID string, visited map[string]bool, result *[]string) {
	if visited[jobID] {
		return
	}

	visited[jobID] = true

	if dependents, exists := dg.reverseEdges[jobID]; exists {
		for _, dep := range dependents {
			*result = append(*result, dep)
			dg.collectAllDependents(dep, visited, result)
		}
	}
}

// HasCycles checks if the graph has any cycles
func (dg *DependencyGraph) HasCycles() bool {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	// Reset visit state
	for _, node := range dg.nodes {
		node.Visited = false
		node.InStack = false
	}

	// Check for cycles using DFS
	for jobID := range dg.nodes {
		if !dg.nodes[jobID].Visited {
			if dg.hasCyclesDFS(jobID) {
				return true
			}
		}
	}

	return false
}

// ValidateNoCycles validates that the graph has no cycles
func (dg *DependencyGraph) ValidateNoCycles() error {
	cycle := dg.FindCycle()
	if cycle != nil {
		return common.ErrCircularDependency(cycle)
	}
	return nil
}

// FindCycle finds a cycle in the graph and returns it
func (dg *DependencyGraph) FindCycle() []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	// Reset visit state
	for _, node := range dg.nodes {
		node.Visited = false
		node.InStack = false
	}

	// Find cycle using DFS
	for jobID := range dg.nodes {
		if !dg.nodes[jobID].Visited {
			if cycle := dg.findCycleDFS(jobID, make([]string, 0)); cycle != nil {
				return cycle
			}
		}
	}

	return nil
}

// hasCyclesDFS performs DFS to detect cycles
func (dg *DependencyGraph) hasCyclesDFS(jobID string) bool {
	node := dg.nodes[jobID]
	node.Visited = true
	node.InStack = true

	// Visit all dependencies
	if dependencies, exists := dg.edges[jobID]; exists {
		for _, dep := range dependencies {
			if depNode, exists := dg.nodes[dep]; exists {
				if !depNode.Visited {
					if dg.hasCyclesDFS(dep) {
						return true
					}
				} else if depNode.InStack {
					return true
				}
			}
		}
	}

	node.InStack = false
	return false
}

// findCycleDFS finds a cycle using DFS and returns the cycle path
func (dg *DependencyGraph) findCycleDFS(jobID string, path []string) []string {
	node := dg.nodes[jobID]
	node.Visited = true
	node.InStack = true
	path = append(path, jobID)

	// Visit all dependencies
	if dependencies, exists := dg.edges[jobID]; exists {
		for _, dep := range dependencies {
			if depNode, exists := dg.nodes[dep]; exists {
				if !depNode.Visited {
					if cycle := dg.findCycleDFS(dep, path); cycle != nil {
						return cycle
					}
				} else if depNode.InStack {
					// Found cycle - return the cycle path
					cycleStart := -1
					for i, nodeID := range path {
						if nodeID == dep {
							cycleStart = i
							break
						}
					}
					if cycleStart != -1 {
						cycle := make([]string, len(path)-cycleStart+1)
						copy(cycle, path[cycleStart:])
						cycle[len(cycle)-1] = dep
						return cycle
					}
				}
			}
		}
	}

	node.InStack = false
	return nil
}

// GetTopologicalOrder returns a topological ordering of the jobs
func (dg *DependencyGraph) GetTopologicalOrder() ([]string, error) {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	// Check for cycles first
	if dg.HasCycles() {
		return nil, common.ErrCircularDependency(dg.FindCycle())
	}

	// Kahn's algorithm for topological sorting
	inDegree := make(map[string]int)
	for jobID := range dg.nodes {
		inDegree[jobID] = len(dg.edges[jobID])
	}

	// Queue for nodes with no incoming edges
	queue := make([]string, 0)
	for jobID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, jobID)
		}
	}

	var result []string
	for len(queue) > 0 {
		// Remove node from queue
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// Process dependents
		if dependents, exists := dg.reverseEdges[current]; exists {
			for _, dependent := range dependents {
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					queue = append(queue, dependent)
				}
			}
		}
	}

	// Check if all nodes were processed
	if len(result) != len(dg.nodes) {
		return nil, common.ErrCircularDependency([]string{"unknown cycle"})
	}

	return result, nil
}

// GetExecutionLevels returns jobs grouped by their execution level
func (dg *DependencyGraph) GetExecutionLevels() [][]string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	levelMap := make(map[int][]string)
	maxLevel := 0

	for jobID, node := range dg.nodes {
		if node.Level > maxLevel {
			maxLevel = node.Level
		}
		levelMap[node.Level] = append(levelMap[node.Level], jobID)
	}

	levels := make([][]string, maxLevel+1)
	for level := 0; level <= maxLevel; level++ {
		levels[level] = levelMap[level]
		// Sort jobs within each level for deterministic ordering
		sort.Strings(levels[level])
	}

	return levels
}

// calculateLevels calculates the topological level of each node
func (dg *DependencyGraph) calculateLevels() {
	// Reset levels
	for _, node := range dg.nodes {
		node.Level = -1
	}

	// Calculate levels using DFS
	for jobID := range dg.nodes {
		if dg.nodes[jobID].Level == -1 {
			dg.calculateLevelDFS(jobID)
		}
	}
}

// calculateLevelDFS calculates level using DFS
func (dg *DependencyGraph) calculateLevelDFS(jobID string) int {
	node := dg.nodes[jobID]

	if node.Level != -1 {
		return node.Level
	}

	// Base case: no dependencies
	if len(node.Dependencies) == 0 {
		node.Level = 0
		return 0
	}

	// Calculate level as max dependency level + 1
	maxDepLevel := -1
	for _, dep := range node.Dependencies {
		if _, exists := dg.nodes[dep]; exists {
			depLevel := dg.calculateLevelDFS(dep)
			if depLevel > maxDepLevel {
				maxDepLevel = depLevel
			}
		}
	}

	node.Level = maxDepLevel + 1
	return node.Level
}

// GetTotalDependencies returns the total number of dependencies in the graph
func (dg *DependencyGraph) GetTotalDependencies() int {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	total := 0
	for _, dependencies := range dg.edges {
		total += len(dependencies)
	}

	return total
}

// GetMaxChainLength returns the maximum dependency chain length
func (dg *DependencyGraph) GetMaxChainLength() int {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	maxLength := 0
	for _, node := range dg.nodes {
		if node.Level > maxLength {
			maxLength = node.Level
		}
	}

	return maxLength + 1
}

// GetGraphStats returns statistics about the dependency graph
func (dg *DependencyGraph) GetGraphStats() GraphStats {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	stats := GraphStats{
		TotalNodes:        len(dg.nodes),
		TotalDependencies: dg.GetTotalDependencies(),
		MaxChainLength:    dg.GetMaxChainLength(),
		HasCycles:         dg.HasCycles(),
	}

	// Calculate level distribution
	levelCounts := make(map[int]int)
	for _, node := range dg.nodes {
		levelCounts[node.Level]++
	}

	stats.LevelDistribution = levelCounts

	// Calculate degree distribution
	inDegreeCount := make(map[int]int)
	outDegreeCount := make(map[int]int)

	for _, node := range dg.nodes {
		inDegree := len(node.Dependencies)
		outDegree := len(node.Dependents)

		inDegreeCount[inDegree]++
		outDegreeCount[outDegree]++

		if inDegree > stats.MaxInDegree {
			stats.MaxInDegree = inDegree
		}
		if outDegree > stats.MaxOutDegree {
			stats.MaxOutDegree = outDegree
		}
	}

	stats.InDegreeDistribution = inDegreeCount
	stats.OutDegreeDistribution = outDegreeCount

	return stats
}

// GraphStats contains statistics about the dependency graph
type GraphStats struct {
	TotalNodes            int         `json:"total_nodes"`
	TotalDependencies     int         `json:"total_dependencies"`
	MaxChainLength        int         `json:"max_chain_length"`
	HasCycles             bool        `json:"has_cycles"`
	MaxInDegree           int         `json:"max_in_degree"`
	MaxOutDegree          int         `json:"max_out_degree"`
	LevelDistribution     map[int]int `json:"level_distribution"`
	InDegreeDistribution  map[int]int `json:"in_degree_distribution"`
	OutDegreeDistribution map[int]int `json:"out_degree_distribution"`
}

// GetJobsAtLevel returns all jobs at a specific level
func (dg *DependencyGraph) GetJobsAtLevel(level int) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	var jobs []string
	for jobID, node := range dg.nodes {
		if node.Level == level {
			jobs = append(jobs, jobID)
		}
	}

	sort.Strings(jobs)
	return jobs
}

// GetShortestPath returns the shortest path between two jobs
func (dg *DependencyGraph) GetShortestPath(from, to string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	// BFS to find shortest path
	queue := [][]string{{from}}
	visited := make(map[string]bool)
	visited[from] = true

	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		current := path[len(path)-1]

		if current == to {
			return path
		}

		// Explore dependents
		if dependents, exists := dg.reverseEdges[current]; exists {
			for _, dependent := range dependents {
				if !visited[dependent] {
					visited[dependent] = true
					newPath := make([]string, len(path)+1)
					copy(newPath, path)
					newPath[len(path)] = dependent
					queue = append(queue, newPath)
				}
			}
		}
	}

	return nil
}

// IsReachable checks if one job is reachable from another
func (dg *DependencyGraph) IsReachable(from, to string) bool {
	return dg.GetShortestPath(from, to) != nil
}

// GetConnectedComponents returns connected components of the graph
func (dg *DependencyGraph) GetConnectedComponents() [][]string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	visited := make(map[string]bool)
	var components [][]string

	for jobID := range dg.nodes {
		if !visited[jobID] {
			component := make([]string, 0)
			dg.exploreComponent(jobID, visited, &component)
			components = append(components, component)
		}
	}

	return components
}

// exploreComponent explores a connected component using DFS
func (dg *DependencyGraph) exploreComponent(jobID string, visited map[string]bool, component *[]string) {
	visited[jobID] = true
	*component = append(*component, jobID)

	// Explore dependencies
	if dependencies, exists := dg.edges[jobID]; exists {
		for _, dep := range dependencies {
			if !visited[dep] {
				dg.exploreComponent(dep, visited, component)
			}
		}
	}

	// Explore dependents
	if dependents, exists := dg.reverseEdges[jobID]; exists {
		for _, dep := range dependents {
			if !visited[dep] {
				dg.exploreComponent(dep, visited, component)
			}
		}
	}
}

// Clear removes all jobs from the graph
func (dg *DependencyGraph) Clear() {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	dg.nodes = make(map[string]*GraphNode)
	dg.edges = make(map[string][]string)
	dg.reverseEdges = make(map[string][]string)
}

// Clone creates a deep copy of the graph
func (dg *DependencyGraph) Clone() *DependencyGraph {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	newGraph := NewDependencyGraph()

	// Copy nodes
	for jobID, node := range dg.nodes {
		newNode := &GraphNode{
			ID:           node.ID,
			Dependencies: make([]string, len(node.Dependencies)),
			Dependents:   make([]string, len(node.Dependents)),
			Level:        node.Level,
		}
		copy(newNode.Dependencies, node.Dependencies)
		copy(newNode.Dependents, node.Dependents)
		newGraph.nodes[jobID] = newNode
	}

	// Copy edges
	for jobID, dependencies := range dg.edges {
		newGraph.edges[jobID] = make([]string, len(dependencies))
		copy(newGraph.edges[jobID], dependencies)
	}

	// Copy reverse edges
	for jobID, dependents := range dg.reverseEdges {
		newGraph.reverseEdges[jobID] = make([]string, len(dependents))
		copy(newGraph.reverseEdges[jobID], dependents)
	}

	return newGraph
}

// String returns a string representation of the graph
func (dg *DependencyGraph) String() string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("Dependency Graph:\n")

	for jobID, node := range dg.nodes {
		sb.WriteString(fmt.Sprintf("  %s (Level: %d)\n", jobID, node.Level))
		if len(node.Dependencies) > 0 {
			sb.WriteString(fmt.Sprintf("    Dependencies: %v\n", node.Dependencies))
		}
		if len(node.Dependents) > 0 {
			sb.WriteString(fmt.Sprintf("    Dependents: %v\n", node.Dependents))
		}
	}

	return sb.String()
}

// removeFromSlice removes a string from a slice
func removeFromSlice(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}
