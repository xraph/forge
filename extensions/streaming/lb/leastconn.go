package lb

import (
	"context"
	"fmt"
	"sync"
)

// leastConnectionsBalancer selects node with fewest connections.
type leastConnectionsBalancer struct {
	nodes       map[string]*NodeInfo
	connections map[string]int // nodeID -> connection count
	mu          sync.RWMutex

	nodeStore NodeStore
}

// NewLeastConnectionsBalancer creates a least connections load balancer.
func NewLeastConnectionsBalancer(store NodeStore) LoadBalancer {
	return &leastConnectionsBalancer{
		nodes:       make(map[string]*NodeInfo),
		connections: make(map[string]int),
		nodeStore:   store,
	}
}

// SelectNode selects node with least connections.
func (lcb *leastConnectionsBalancer) SelectNode(ctx context.Context, userID string, metadata map[string]any) (*NodeInfo, error) {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	if len(lcb.nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	// Find node with least connections
	var selected *NodeInfo
	minConnections := int(^uint(0) >> 1) // Max int

	for _, node := range lcb.nodes {
		if !node.Healthy {
			continue
		}

		connections := lcb.connections[node.ID]

		// Weight consideration
		weightedConnections := connections
		if node.Weight > 0 {
			weightedConnections = connections * 100 / node.Weight
		}

		if weightedConnections < minConnections {
			minConnections = weightedConnections
			selected = node
		}
	}

	if selected == nil {
		return nil, fmt.Errorf("no healthy nodes available")
	}

	// Increment connection count
	lcb.connections[selected.ID]++

	return selected, nil
}

// GetNode returns node for connection.
func (lcb *leastConnectionsBalancer) GetNode(ctx context.Context, connID string) (*NodeInfo, error) {
	// This is typically used to release connection
	// For now, just return error as we need more context
	return nil, fmt.Errorf("not implemented for least connections")
}

// RegisterNode adds node.
func (lcb *leastConnectionsBalancer) RegisterNode(ctx context.Context, node *NodeInfo) error {
	lcb.mu.Lock()
	defer lcb.mu.Unlock()

	lcb.nodes[node.ID] = node
	lcb.connections[node.ID] = 0

	if lcb.nodeStore != nil {
		return lcb.nodeStore.Save(ctx, node)
	}

	return nil
}

// UnregisterNode removes node.
func (lcb *leastConnectionsBalancer) UnregisterNode(ctx context.Context, nodeID string) error {
	lcb.mu.Lock()
	defer lcb.mu.Unlock()

	delete(lcb.nodes, nodeID)
	delete(lcb.connections, nodeID)

	if lcb.nodeStore != nil {
		return lcb.nodeStore.Delete(ctx, nodeID)
	}

	return nil
}

// Health checks node health.
func (lcb *leastConnectionsBalancer) Health(ctx context.Context, nodeID string) error {
	lcb.mu.RLock()
	node, ok := lcb.nodes[nodeID]
	lcb.mu.RUnlock()

	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	if !node.Healthy {
		return fmt.Errorf("node unhealthy: %s", nodeID)
	}

	return nil
}

// GetNodes returns all nodes.
func (lcb *leastConnectionsBalancer) GetNodes(ctx context.Context) ([]*NodeInfo, error) {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	nodes := make([]*NodeInfo, 0, len(lcb.nodes))
	for _, node := range lcb.nodes {
		nodeCopy := *node
		nodeCopy.ConnectionCount = lcb.connections[node.ID]
		nodes = append(nodes, &nodeCopy)
	}

	return nodes, nil
}

// GetHealthyNodes returns healthy nodes.
func (lcb *leastConnectionsBalancer) GetHealthyNodes(ctx context.Context) ([]*NodeInfo, error) {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	nodes := make([]*NodeInfo, 0)
	for _, node := range lcb.nodes {
		if node.Healthy {
			nodeCopy := *node
			nodeCopy.ConnectionCount = lcb.connections[node.ID]
			nodes = append(nodes, &nodeCopy)
		}
	}

	return nodes, nil
}

// ReleaseConnection decrements connection count for a node.
func (lcb *leastConnectionsBalancer) ReleaseConnection(nodeID string) {
	lcb.mu.Lock()
	defer lcb.mu.Unlock()

	if count, ok := lcb.connections[nodeID]; ok && count > 0 {
		lcb.connections[nodeID]--
	}
}

// GetConnectionCount returns current connection count for a node.
func (lcb *leastConnectionsBalancer) GetConnectionCount(nodeID string) int {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	return lcb.connections[nodeID]
}
