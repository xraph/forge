package lb

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// consistentHashBalancer implements consistent hashing.
type consistentHashBalancer struct {
	nodes    map[string]*NodeInfo
	ring     []uint32
	ringMap  map[uint32]string // hash -> nodeID
	replicas int               // Virtual nodes per physical node
	mu       sync.RWMutex

	nodeStore NodeStore
}

// NewConsistentHashBalancer creates a consistent hash load balancer.
func NewConsistentHashBalancer(replicas int, store NodeStore) LoadBalancer {
	return &consistentHashBalancer{
		nodes:     make(map[string]*NodeInfo),
		ring:      make([]uint32, 0),
		ringMap:   make(map[uint32]string),
		replicas:  replicas,
		nodeStore: store,
	}
}

// SelectNode selects node using consistent hashing.
func (chb *consistentHashBalancer) SelectNode(ctx context.Context, userID string, metadata map[string]any) (*NodeInfo, error) {
	chb.mu.RLock()
	defer chb.mu.RUnlock()

	if len(chb.nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	// Hash the user ID
	hash := chb.hash(userID)

	// Find the first node in the ring >= hash
	idx := sort.Search(len(chb.ring), func(i int) bool {
		return chb.ring[i] >= hash
	})

	// Wrap around if needed
	if idx == len(chb.ring) {
		idx = 0
	}

	nodeID := chb.ringMap[chb.ring[idx]]
	node, ok := chb.nodes[nodeID]
	if !ok || !node.Healthy {
		// Node not healthy, try next
		return chb.selectNextHealthyNode(idx)
	}

	return node, nil
}

// GetNode returns node for connection.
func (chb *consistentHashBalancer) GetNode(ctx context.Context, connID string) (*NodeInfo, error) {
	// Use consistent hash on connection ID
	return chb.SelectNode(ctx, connID, nil)
}

// RegisterNode adds node to the ring.
func (chb *consistentHashBalancer) RegisterNode(ctx context.Context, node *NodeInfo) error {
	chb.mu.Lock()
	defer chb.mu.Unlock()

	chb.nodes[node.ID] = node

	// Add virtual nodes to ring
	for i := 0; i < chb.replicas; i++ {
		virtualKey := fmt.Sprintf("%s:%d", node.ID, i)
		hash := chb.hash(virtualKey)
		chb.ring = append(chb.ring, hash)
		chb.ringMap[hash] = node.ID
	}

	// Sort ring
	sort.Slice(chb.ring, func(i, j int) bool {
		return chb.ring[i] < chb.ring[j]
	})

	// Persist to store
	if chb.nodeStore != nil {
		return chb.nodeStore.Save(ctx, node)
	}

	return nil
}

// UnregisterNode removes node from the ring.
func (chb *consistentHashBalancer) UnregisterNode(ctx context.Context, nodeID string) error {
	chb.mu.Lock()
	defer chb.mu.Unlock()

	delete(chb.nodes, nodeID)

	// Remove virtual nodes from ring
	newRing := make([]uint32, 0, len(chb.ring))
	for _, hash := range chb.ring {
		if chb.ringMap[hash] != nodeID {
			newRing = append(newRing, hash)
		} else {
			delete(chb.ringMap, hash)
		}
	}
	chb.ring = newRing

	// Persist to store
	if chb.nodeStore != nil {
		return chb.nodeStore.Delete(ctx, nodeID)
	}

	return nil
}

// Health checks node health.
func (chb *consistentHashBalancer) Health(ctx context.Context, nodeID string) error {
	chb.mu.RLock()
	node, ok := chb.nodes[nodeID]
	chb.mu.RUnlock()

	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	if !node.Healthy {
		return fmt.Errorf("node unhealthy: %s", nodeID)
	}

	return nil
}

// GetNodes returns all nodes.
func (chb *consistentHashBalancer) GetNodes(ctx context.Context) ([]*NodeInfo, error) {
	chb.mu.RLock()
	defer chb.mu.RUnlock()

	nodes := make([]*NodeInfo, 0, len(chb.nodes))
	for _, node := range chb.nodes {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// GetHealthyNodes returns healthy nodes.
func (chb *consistentHashBalancer) GetHealthyNodes(ctx context.Context) ([]*NodeInfo, error) {
	chb.mu.RLock()
	defer chb.mu.RUnlock()

	nodes := make([]*NodeInfo, 0)
	for _, node := range chb.nodes {
		if node.Healthy {
			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

func (chb *consistentHashBalancer) selectNextHealthyNode(startIdx int) (*NodeInfo, error) {
	// Try next nodes in the ring
	for i := 0; i < len(chb.ring); i++ {
		idx := (startIdx + i) % len(chb.ring)
		nodeID := chb.ringMap[chb.ring[idx]]
		node, ok := chb.nodes[nodeID]
		if ok && node.Healthy {
			return node, nil
		}
	}

	return nil, fmt.Errorf("no healthy nodes available")
}

func (chb *consistentHashBalancer) hash(key string) uint32 {
	h := md5.New()
	h.Write([]byte(key))
	hashBytes := h.Sum(nil)
	return binary.BigEndian.Uint32(hashBytes)
}

// inMemoryNodeStore implements NodeStore in memory.
type inMemoryNodeStore struct {
	nodes map[string]*NodeInfo
	mu    sync.RWMutex
}

// NewInMemoryNodeStore creates an in-memory node store.
func NewInMemoryNodeStore() NodeStore {
	return &inMemoryNodeStore{
		nodes: make(map[string]*NodeInfo),
	}
}

func (ins *inMemoryNodeStore) Save(ctx context.Context, node *NodeInfo) error {
	ins.mu.Lock()
	defer ins.mu.Unlock()

	ins.nodes[node.ID] = node
	return nil
}

func (ins *inMemoryNodeStore) Get(ctx context.Context, nodeID string) (*NodeInfo, error) {
	ins.mu.RLock()
	defer ins.mu.RUnlock()

	node, ok := ins.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	return node, nil
}

func (ins *inMemoryNodeStore) List(ctx context.Context) ([]*NodeInfo, error) {
	ins.mu.RLock()
	defer ins.mu.RUnlock()

	nodes := make([]*NodeInfo, 0, len(ins.nodes))
	for _, node := range ins.nodes {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

func (ins *inMemoryNodeStore) Delete(ctx context.Context, nodeID string) error {
	ins.mu.Lock()
	defer ins.mu.Unlock()

	delete(ins.nodes, nodeID)
	return nil
}

func (ins *inMemoryNodeStore) UpdateHealth(ctx context.Context, nodeID string, healthy bool) error {
	ins.mu.Lock()
	defer ins.mu.Unlock()

	node, ok := ins.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	node.Healthy = healthy
	return nil
}

func (ins *inMemoryNodeStore) UpdateConnectionCount(ctx context.Context, nodeID string, count int) error {
	ins.mu.Lock()
	defer ins.mu.Unlock()

	node, ok := ins.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	node.ConnectionCount = count
	return nil
}
