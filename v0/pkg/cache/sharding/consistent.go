package sharding

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"sort"
	"sync"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ConsistentHash implements consistent hashing for cache sharding
type ConsistentHash struct {
	nodes        map[string]*Node
	ring         []uint64
	nodeMap      map[uint64]string
	virtualNodes int
	hasher       hash.Hash
	mu           sync.RWMutex
	logger       common.Logger
	metrics      common.Metrics
	stats        ConsistentHashStats
}

// Node represents a cache node
type Node struct {
	ID       string                 `json:"id"`
	Address  string                 `json:"address"`
	Weight   int                    `json:"weight"`
	Metadata map[string]interface{} `json:"metadata"`
	Status   NodeStatus             `json:"status"`
	Replicas []string               `json:"replicas"`
}

// NodeStatus represents the status of a cache node
type NodeStatus string

const (
	NodeStatusActive   NodeStatus = "active"
	NodeStatusInactive NodeStatus = "inactive"
	NodeStatusDraining NodeStatus = "draining"
	NodeStatusFailed   NodeStatus = "failed"
)

// ConsistentHashStats contains statistics for consistent hashing
type ConsistentHashStats struct {
	TotalNodes       int              `json:"total_nodes"`
	ActiveNodes      int              `json:"active_nodes"`
	VirtualNodes     int              `json:"virtual_nodes"`
	RingSize         int              `json:"ring_size"`
	KeysDistributed  int64            `json:"keys_distributed"`
	Rebalances       int64            `json:"rebalances"`
	NodeDistribution map[string]int64 `json:"node_distribution"`
	AverageLoad      float64          `json:"average_load"`
	LoadVariance     float64          `json:"load_variance"`
}

// ConsistentHashConfig contains configuration for consistent hashing
type ConsistentHashConfig struct {
	VirtualNodes int    `yaml:"virtual_nodes" json:"virtual_nodes"`
	HashFunction string `yaml:"hash_function" json:"hash_function"`
	Replicas     int    `yaml:"replicas" json:"replicas"`
}

// NewConsistentHash creates a new consistent hash ring
func NewConsistentHash(config ConsistentHashConfig, logger common.Logger, metrics common.Metrics) *ConsistentHash {
	virtualNodes := config.VirtualNodes
	if virtualNodes <= 0 {
		virtualNodes = 150
	}

	return &ConsistentHash{
		nodes:        make(map[string]*Node),
		nodeMap:      make(map[uint64]string),
		virtualNodes: virtualNodes,
		hasher:       sha256.New(),
		logger:       logger,
		metrics:      metrics,
		stats: ConsistentHashStats{
			NodeDistribution: make(map[string]int64),
		},
	}
}

// AddNode adds a node to the hash ring
func (ch *ConsistentHash) AddNode(node *Node) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if _, exists := ch.nodes[node.ID]; exists {
		return fmt.Errorf("node %s already exists", node.ID)
	}

	ch.nodes[node.ID] = node
	ch.stats.NodeDistribution[node.ID] = 0

	// Add virtual nodes to the ring
	weight := node.Weight
	if weight <= 0 {
		weight = 1
	}

	virtualNodeCount := ch.virtualNodes * weight
	for i := 0; i < virtualNodeCount; i++ {
		virtualKey := fmt.Sprintf("%s:%d", node.ID, i)
		hash := ch.hash(virtualKey)
		ch.nodeMap[hash] = node.ID
	}

	// Rebuild the ring
	ch.rebuildRing()

	ch.stats.TotalNodes = len(ch.nodes)
	if node.Status == NodeStatusActive {
		ch.stats.ActiveNodes++
	}
	ch.stats.VirtualNodes = len(ch.nodeMap)
	ch.stats.Rebalances++

	ch.logger.Info("node added to consistent hash ring",
		logger.String("node_id", node.ID),
		logger.String("address", node.Address),
		logger.Int("weight", node.Weight),
		logger.Int("virtual_nodes", virtualNodeCount),
	)

	if ch.metrics != nil {
		ch.metrics.Counter("forge.cache.sharding.nodes.added").Inc()
		ch.metrics.Gauge("forge.cache.sharding.nodes.total").Set(float64(ch.stats.TotalNodes))
		ch.metrics.Gauge("forge.cache.sharding.nodes.active").Set(float64(ch.stats.ActiveNodes))
	}

	return nil
}

// RemoveNode removes a node from the hash ring
func (ch *ConsistentHash) RemoveNode(nodeID string) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	node, exists := ch.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Remove virtual nodes from the ring
	weight := node.Weight
	if weight <= 0 {
		weight = 1
	}

	virtualNodeCount := ch.virtualNodes * weight
	for i := 0; i < virtualNodeCount; i++ {
		virtualKey := fmt.Sprintf("%s:%d", nodeID, i)
		hash := ch.hash(virtualKey)
		delete(ch.nodeMap, hash)
	}

	delete(ch.nodes, nodeID)
	delete(ch.stats.NodeDistribution, nodeID)

	// Rebuild the ring
	ch.rebuildRing()

	ch.stats.TotalNodes = len(ch.nodes)
	if node.Status == NodeStatusActive {
		ch.stats.ActiveNodes--
	}
	ch.stats.VirtualNodes = len(ch.nodeMap)
	ch.stats.Rebalances++

	ch.logger.Info("node removed from consistent hash ring",
		logger.String("node_id", nodeID),
	)

	if ch.metrics != nil {
		ch.metrics.Counter("forge.cache.sharding.nodes.removed").Inc()
		ch.metrics.Gauge("forge.cache.sharding.nodes.total").Set(float64(ch.stats.TotalNodes))
		ch.metrics.Gauge("forge.cache.sharding.nodes.active").Set(float64(ch.stats.ActiveNodes))
	}

	return nil
}

// GetNode returns the node responsible for a given key
func (ch *ConsistentHash) GetNode(key string) (*Node, error) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.ring) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	hash := ch.hash(key)
	idx := ch.search(hash)
	nodeID := ch.nodeMap[ch.ring[idx]]

	node, exists := ch.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}

	// Only return active nodes
	if node.Status != NodeStatusActive {
		// Find next active node
		for i := 1; i < len(ch.ring); i++ {
			nextIdx := (idx + i) % len(ch.ring)
			nextNodeID := ch.nodeMap[ch.ring[nextIdx]]
			nextNode, exists := ch.nodes[nextNodeID]
			if exists && nextNode.Status == NodeStatusActive {
				node = nextNode
				nodeID = nextNodeID
				break
			}
		}

		if node.Status != NodeStatusActive {
			return nil, fmt.Errorf("no active nodes available")
		}
	}

	// Update distribution stats
	ch.stats.NodeDistribution[nodeID]++
	ch.stats.KeysDistributed++

	if ch.metrics != nil {
		ch.metrics.Counter("forge.cache.sharding.keys.distributed", "node", nodeID).Inc()
	}

	return node, nil
}

// GetNodes returns multiple nodes for replication
func (ch *ConsistentHash) GetNodes(key string, count int) ([]*Node, error) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.ring) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	if count <= 0 {
		count = 1
	}

	hash := ch.hash(key)
	idx := ch.search(hash)

	var nodes []*Node
	seen := make(map[string]bool)

	for i := 0; len(nodes) < count && i < len(ch.ring); i++ {
		nodeIdx := (idx + i) % len(ch.ring)
		nodeID := ch.nodeMap[ch.ring[nodeIdx]]

		if seen[nodeID] {
			continue
		}

		node, exists := ch.nodes[nodeID]
		if !exists || node.Status != NodeStatusActive {
			continue
		}

		nodes = append(nodes, node)
		seen[nodeID] = true

		// Update distribution stats
		ch.stats.NodeDistribution[nodeID]++
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no active nodes available")
	}

	ch.stats.KeysDistributed++

	return nodes, nil
}

// GetAllNodes returns all nodes
func (ch *ConsistentHash) GetAllNodes() []*Node {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	nodes := make([]*Node, 0, len(ch.nodes))
	for _, node := range ch.nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// GetActiveNodes returns all active nodes
func (ch *ConsistentHash) GetActiveNodes() []*Node {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	var nodes []*Node
	for _, node := range ch.nodes {
		if node.Status == NodeStatusActive {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// UpdateNodeStatus updates the status of a node
func (ch *ConsistentHash) UpdateNodeStatus(nodeID string, status NodeStatus) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	node, exists := ch.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	oldStatus := node.Status
	node.Status = status

	// Update active node count
	if oldStatus == NodeStatusActive && status != NodeStatusActive {
		ch.stats.ActiveNodes--
	} else if oldStatus != NodeStatusActive && status == NodeStatusActive {
		ch.stats.ActiveNodes++
	}

	ch.logger.Info("node status updated",
		logger.String("node_id", nodeID),
		logger.String("old_status", string(oldStatus)),
		logger.String("new_status", string(status)),
	)

	if ch.metrics != nil {
		ch.metrics.Gauge("forge.cache.sharding.nodes.active").Set(float64(ch.stats.ActiveNodes))
	}

	return nil
}

// GetNodeByKey returns the node ID responsible for a key
func (ch *ConsistentHash) GetNodeByKey(key string) (string, error) {
	node, err := ch.GetNode(key)
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// GetStats returns consistent hash statistics
func (ch *ConsistentHash) GetStats() ConsistentHashStats {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	stats := ch.stats
	stats.RingSize = len(ch.ring)

	// Calculate load distribution metrics
	if len(ch.stats.NodeDistribution) > 0 {
		var total int64
		var values []float64

		for _, count := range ch.stats.NodeDistribution {
			total += count
			values = append(values, float64(count))
		}

		if len(values) > 0 {
			stats.AverageLoad = float64(total) / float64(len(values))

			// Calculate variance
			var variance float64
			for _, value := range values {
				diff := value - stats.AverageLoad
				variance += diff * diff
			}
			stats.LoadVariance = variance / float64(len(values))
		}
	}

	return stats
}

// Rebalance triggers a rebalance of the hash ring
func (ch *ConsistentHash) Rebalance() error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.rebuildRing()
	ch.stats.Rebalances++

	ch.logger.Info("consistent hash ring rebalanced",
		logger.Int("ring_size", len(ch.ring)),
		logger.Int("total_nodes", len(ch.nodes)),
	)

	if ch.metrics != nil {
		ch.metrics.Counter("forge.cache.sharding.rebalances").Inc()
	}

	return nil
}

// rebuildRing rebuilds the hash ring from current nodes
func (ch *ConsistentHash) rebuildRing() {
	ch.ring = make([]uint64, 0, len(ch.nodeMap))
	for hash := range ch.nodeMap {
		ch.ring = append(ch.ring, hash)
	}
	sort.Slice(ch.ring, func(i, j int) bool {
		return ch.ring[i] < ch.ring[j]
	})
}

// hash computes the hash of a key
func (ch *ConsistentHash) hash(key string) uint64 {
	ch.hasher.Reset()
	ch.hasher.Write([]byte(key))
	hashBytes := ch.hasher.Sum(nil)

	// Convert to uint64
	var hash uint64
	for i := 0; i < 8 && i < len(hashBytes); i++ {
		hash = hash*256 + uint64(hashBytes[i])
	}

	return hash
}

// search finds the index of the first node >= hash
func (ch *ConsistentHash) search(hash uint64) int {
	idx := sort.Search(len(ch.ring), func(i int) bool {
		return ch.ring[i] >= hash
	})

	if idx == len(ch.ring) {
		idx = 0
	}

	return idx
}

// GetNodeDistribution returns the distribution of keys across nodes
func (ch *ConsistentHash) GetNodeDistribution() map[string]int64 {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	distribution := make(map[string]int64)
	for nodeID, count := range ch.stats.NodeDistribution {
		distribution[nodeID] = count
	}

	return distribution
}

// ResetStats resets the statistics
func (ch *ConsistentHash) ResetStats() {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.stats.KeysDistributed = 0
	ch.stats.Rebalances = 0
	for nodeID := range ch.stats.NodeDistribution {
		ch.stats.NodeDistribution[nodeID] = 0
	}

	ch.logger.Info("consistent hash statistics reset")
}

// ValidateRing validates the integrity of the hash ring
func (ch *ConsistentHash) ValidateRing() error {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	// Check if ring is sorted
	for i := 1; i < len(ch.ring); i++ {
		if ch.ring[i-1] > ch.ring[i] {
			return fmt.Errorf("ring not properly sorted at index %d", i)
		}
	}

	// Check if all nodes in nodeMap exist in nodes
	for hash, nodeID := range ch.nodeMap {
		if _, exists := ch.nodes[nodeID]; !exists {
			return fmt.Errorf("node %s in nodeMap but not in nodes (hash: %d)", nodeID, hash)
		}
	}

	// Check if virtual node count is correct
	expectedVirtualNodes := 0
	for _, node := range ch.nodes {
		weight := node.Weight
		if weight <= 0 {
			weight = 1
		}
		expectedVirtualNodes += ch.virtualNodes * weight
	}

	if len(ch.nodeMap) != expectedVirtualNodes {
		return fmt.Errorf("virtual node count mismatch: expected %d, got %d", expectedVirtualNodes, len(ch.nodeMap))
	}

	return nil
}

// Migrate returns the keys that need to be migrated when nodes change
func (ch *ConsistentHash) Migrate(oldRing *ConsistentHash, keys []string) map[string]NodeMigration {
	migrations := make(map[string]NodeMigration)

	for _, key := range keys {
		// Get old node
		oldNode, oldErr := oldRing.GetNode(key)

		// Get new node
		newNode, newErr := ch.GetNode(key)

		if oldErr != nil || newErr != nil {
			continue
		}

		// If nodes are different, migration is needed
		if oldNode.ID != newNode.ID {
			migrations[key] = NodeMigration{
				Key:     key,
				OldNode: oldNode.ID,
				NewNode: newNode.ID,
			}
		}
	}

	return migrations
}

// NodeMigration represents a key migration between nodes
type NodeMigration struct {
	Key     string `json:"key"`
	OldNode string `json:"old_node"`
	NewNode string `json:"new_node"`
}

// Clone creates a copy of the consistent hash
func (ch *ConsistentHash) Clone() *ConsistentHash {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	clone := &ConsistentHash{
		nodes:        make(map[string]*Node),
		nodeMap:      make(map[uint64]string),
		virtualNodes: ch.virtualNodes,
		hasher:       sha256.New(),
		logger:       ch.logger,
		metrics:      ch.metrics,
		stats: ConsistentHashStats{
			NodeDistribution: make(map[string]int64),
		},
	}

	// Copy nodes
	for id, node := range ch.nodes {
		nodeClone := *node
		clone.nodes[id] = &nodeClone
		clone.stats.NodeDistribution[id] = 0
	}

	// Copy node map
	for hash, nodeID := range ch.nodeMap {
		clone.nodeMap[hash] = nodeID
	}

	// Copy ring
	clone.ring = make([]uint64, len(ch.ring))
	copy(clone.ring, ch.ring)

	// Update stats
	clone.stats.TotalNodes = len(clone.nodes)
	clone.stats.VirtualNodes = len(clone.nodeMap)
	clone.stats.RingSize = len(clone.ring)

	for _, node := range clone.nodes {
		if node.Status == NodeStatusActive {
			clone.stats.ActiveNodes++
		}
	}

	return clone
}
