package sharding

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"sort"
	"sync"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// HashSharding implements hash-based sharding for cache distribution
type HashSharding struct {
	nodes     []*ShardNode
	nodeMap   map[string]*ShardNode
	hashFunc  HashFunction
	algorithm HashAlgorithm
	logger    common.Logger
	metrics   common.Metrics
	stats     HashShardingStats
	mu        sync.RWMutex
}

// ShardNode represents a node in the hash sharding
type ShardNode struct {
	ID       string                 `json:"id"`
	Index    int                    `json:"index"`
	Address  string                 `json:"address"`
	Weight   float64                `json:"weight"`
	Capacity int64                  `json:"capacity"`
	Used     int64                  `json:"used"`
	Status   NodeStatus             `json:"status"`
	Metadata map[string]interface{} `json:"metadata"`
	Tags     []string               `json:"tags"`
}

// HashFunction defines the interface for hash functions
type HashFunction interface {
	Hash(key string) uint64
	Name() string
}

// HashAlgorithm represents different hash algorithms
type HashAlgorithm string

const (
	HashAlgorithmMD5    HashAlgorithm = "md5"
	HashAlgorithmSHA1   HashAlgorithm = "sha1"
	HashAlgorithmSHA256 HashAlgorithm = "sha256"
	HashAlgorithmCRC32  HashAlgorithm = "crc32"
	HashAlgorithmFNV32  HashAlgorithm = "fnv32"
	HashAlgorithmFNV64  HashAlgorithm = "fnv64"
	HashAlgorithmCustom HashAlgorithm = "custom"
)

// HashShardingStats contains statistics for hash sharding
type HashShardingStats struct {
	TotalNodes       int                `json:"total_nodes"`
	ActiveNodes      int                `json:"active_nodes"`
	TotalCapacity    int64              `json:"total_capacity"`
	UsedCapacity     int64              `json:"used_capacity"`
	KeysDistributed  int64              `json:"keys_distributed"`
	HashCollisions   int64              `json:"hash_collisions"`
	RebalanceCount   int64              `json:"rebalance_count"`
	NodeDistribution map[string]int64   `json:"node_distribution"`
	LoadBalance      LoadBalanceMetrics `json:"load_balance"`
}

// LoadBalanceMetrics contains load balancing metrics
type LoadBalanceMetrics struct {
	MinLoad      float64 `json:"min_load"`
	MaxLoad      float64 `json:"max_load"`
	AverageLoad  float64 `json:"average_load"`
	LoadVariance float64 `json:"load_variance"`
	Imbalance    float64 `json:"imbalance"`
}

// HashShardingConfig contains configuration for hash sharding
type HashShardingConfig struct {
	Algorithm          HashAlgorithm `yaml:"algorithm" json:"algorithm"`
	ReplicationFactor  int           `yaml:"replication_factor" json:"replication_factor"`
	LoadBalancing      bool          `yaml:"load_balancing" json:"load_balancing"`
	AutoRebalance      bool          `yaml:"auto_rebalance" json:"auto_rebalance"`
	RebalanceThreshold float64       `yaml:"rebalance_threshold" json:"rebalance_threshold"`
}

// NewHashSharding creates a new hash sharding instance
func NewHashSharding(config HashShardingConfig, logger common.Logger, metrics common.Metrics) *HashSharding {
	return &HashSharding{
		nodes:     make([]*ShardNode, 0),
		nodeMap:   make(map[string]*ShardNode),
		hashFunc:  NewHashFunction(config.Algorithm),
		algorithm: config.Algorithm,
		logger:    logger,
		metrics:   metrics,
		stats: HashShardingStats{
			NodeDistribution: make(map[string]int64),
		},
	}
}

// AddNode adds a node to the hash sharding
func (hs *HashSharding) AddNode(node *ShardNode) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if _, exists := hs.nodeMap[node.ID]; exists {
		return fmt.Errorf("node %s already exists", node.ID)
	}

	// Set index
	node.Index = len(hs.nodes)

	// Add to collections
	hs.nodes = append(hs.nodes, node)
	hs.nodeMap[node.ID] = node
	hs.stats.NodeDistribution[node.ID] = 0

	// Update stats
	hs.updateStats()

	hs.logger.Info("node added to hash sharding",
		logger.String("node_id", node.ID),
		logger.String("address", node.Address),
		logger.Int("index", node.Index),
		logger.Float64("weight", node.Weight),
	)

	if hs.metrics != nil {
		hs.metrics.Counter("forge.cache.sharding.hash.nodes.added").Inc()
		hs.metrics.Gauge("forge.cache.sharding.hash.nodes.total").Set(float64(hs.stats.TotalNodes))
	}

	return nil
}

// RemoveNode removes a node from the hash sharding
func (hs *HashSharding) RemoveNode(nodeID string) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	_, exists := hs.nodeMap[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Remove from slice
	for i, n := range hs.nodes {
		if n.ID == nodeID {
			hs.nodes = append(hs.nodes[:i], hs.nodes[i+1:]...)
			break
		}
	}

	// Update indices
	for i, n := range hs.nodes {
		n.Index = i
	}

	// Remove from map
	delete(hs.nodeMap, nodeID)
	delete(hs.stats.NodeDistribution, nodeID)

	// Update stats
	hs.updateStats()

	hs.logger.Info("node removed from hash sharding",
		logger.String("node_id", nodeID),
	)

	if hs.metrics != nil {
		hs.metrics.Counter("forge.cache.sharding.hash.nodes.removed").Inc()
		hs.metrics.Gauge("forge.cache.sharding.hash.nodes.total").Set(float64(hs.stats.TotalNodes))
	}

	return nil
}

// GetNode returns the node responsible for a given key
func (hs *HashSharding) GetNode(key string) (*ShardNode, error) {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	if len(hs.nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	// Get active nodes only
	activeNodes := hs.getActiveNodes()
	if len(activeNodes) == 0 {
		return nil, fmt.Errorf("no active nodes available")
	}

	// Hash the key and find the node
	hash := hs.hashFunc.Hash(key)
	nodeIndex := int(hash % uint64(len(activeNodes)))
	node := activeNodes[nodeIndex]

	// Update distribution stats
	hs.stats.NodeDistribution[node.ID]++
	hs.stats.KeysDistributed++

	if hs.metrics != nil {
		hs.metrics.Counter("forge.cache.sharding.hash.keys.distributed", "node", node.ID).Inc()
	}

	return node, nil
}

// GetNodes returns multiple nodes for replication
func (hs *HashSharding) GetNodes(key string, count int) ([]*ShardNode, error) {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	if len(hs.nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	activeNodes := hs.getActiveNodes()
	if len(activeNodes) == 0 {
		return nil, fmt.Errorf("no active nodes available")
	}

	if count <= 0 {
		count = 1
	}

	if count > len(activeNodes) {
		count = len(activeNodes)
	}

	// Hash the key to get starting point
	hash := hs.hashFunc.Hash(key)
	startIndex := int(hash % uint64(len(activeNodes)))

	// Get consecutive nodes for replication
	var nodes []*ShardNode
	for i := 0; i < count; i++ {
		nodeIndex := (startIndex + i) % len(activeNodes)
		node := activeNodes[nodeIndex]
		nodes = append(nodes, node)

		// Update distribution stats
		hs.stats.NodeDistribution[node.ID]++
	}

	hs.stats.KeysDistributed++

	return nodes, nil
}

// GetNodeByIndex returns a node by its index
func (hs *HashSharding) GetNodeByIndex(index int) (*ShardNode, error) {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	if index < 0 || index >= len(hs.nodes) {
		return nil, fmt.Errorf("node index %d out of range", index)
	}

	return hs.nodes[index], nil
}

// GetAllNodes returns all nodes
func (hs *HashSharding) GetAllNodes() []*ShardNode {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	nodes := make([]*ShardNode, len(hs.nodes))
	copy(nodes, hs.nodes)
	return nodes
}

// GetActiveNodes returns all active nodes
func (hs *HashSharding) GetActiveNodes() []*ShardNode {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	return hs.getActiveNodes()
}

// getActiveNodes returns active nodes (caller must hold lock)
func (hs *HashSharding) getActiveNodes() []*ShardNode {
	var activeNodes []*ShardNode
	for _, node := range hs.nodes {
		if node.Status == NodeStatusActive {
			activeNodes = append(activeNodes, node)
		}
	}
	return activeNodes
}

// UpdateNodeStatus updates the status of a node
func (hs *HashSharding) UpdateNodeStatus(nodeID string, status NodeStatus) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	node, exists := hs.nodeMap[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	oldStatus := node.Status
	node.Status = status

	// Update stats
	hs.updateStats()

	hs.logger.Info("node status updated",
		logger.String("node_id", nodeID),
		logger.String("old_status", string(oldStatus)),
		logger.String("new_status", string(status)),
	)

	if hs.metrics != nil {
		hs.metrics.Gauge("forge.cache.sharding.hash.nodes.active").Set(float64(hs.stats.ActiveNodes))
	}

	return nil
}

// UpdateNodeCapacity updates the capacity of a node
func (hs *HashSharding) UpdateNodeCapacity(nodeID string, capacity, used int64) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	node, exists := hs.nodeMap[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	node.Capacity = capacity
	node.Used = used

	// Update stats
	hs.updateStats()

	hs.logger.Info("node capacity updated",
		logger.String("node_id", nodeID),
		logger.Int64("capacity", capacity),
		logger.Int64("used", used),
	)

	return nil
}

// GetStats returns hash sharding statistics
func (hs *HashSharding) GetStats() HashShardingStats {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	// Calculate load balance metrics
	hs.calculateLoadBalance()

	return hs.stats
}

// updateStats updates internal statistics (caller must hold lock)
func (hs *HashSharding) updateStats() {
	hs.stats.TotalNodes = len(hs.nodes)
	hs.stats.ActiveNodes = 0
	hs.stats.TotalCapacity = 0
	hs.stats.UsedCapacity = 0

	for _, node := range hs.nodes {
		if node.Status == NodeStatusActive {
			hs.stats.ActiveNodes++
		}
		hs.stats.TotalCapacity += node.Capacity
		hs.stats.UsedCapacity += node.Used
	}
}

// calculateLoadBalance calculates load balancing metrics
func (hs *HashSharding) calculateLoadBalance() {
	if len(hs.stats.NodeDistribution) == 0 {
		return
	}

	var loads []float64
	var total int64

	for _, count := range hs.stats.NodeDistribution {
		total += count
		loads = append(loads, float64(count))
	}

	if len(loads) == 0 {
		return
	}

	// Calculate average
	average := float64(total) / float64(len(loads))
	hs.stats.LoadBalance.AverageLoad = average

	// Find min and max
	sort.Float64s(loads)
	hs.stats.LoadBalance.MinLoad = loads[0]
	hs.stats.LoadBalance.MaxLoad = loads[len(loads)-1]

	// Calculate variance
	var variance float64
	for _, load := range loads {
		diff := load - average
		variance += diff * diff
	}
	hs.stats.LoadBalance.LoadVariance = variance / float64(len(loads))

	// Calculate imbalance (max deviation from average)
	maxDeviation := 0.0
	for _, load := range loads {
		deviation := abs(load - average)
		if deviation > maxDeviation {
			maxDeviation = deviation
		}
	}

	if average > 0 {
		hs.stats.LoadBalance.Imbalance = maxDeviation / average
	}
}

// Rebalance redistributes load across nodes
func (hs *HashSharding) Rebalance() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	// Calculate current load balance
	hs.calculateLoadBalance()

	// Check if rebalancing is needed
	if hs.stats.LoadBalance.Imbalance < 0.1 { // 10% threshold
		return nil // No rebalancing needed
	}

	// Reset distribution stats for rebalancing
	for nodeID := range hs.stats.NodeDistribution {
		hs.stats.NodeDistribution[nodeID] = 0
	}

	hs.stats.RebalanceCount++

	hs.logger.Info("hash sharding rebalanced",
		logger.Float64("imbalance", hs.stats.LoadBalance.Imbalance),
		logger.Int64("rebalance_count", hs.stats.RebalanceCount),
	)

	if hs.metrics != nil {
		hs.metrics.Counter("forge.cache.sharding.hash.rebalances").Inc()
	}

	return nil
}

// GetNodeDistribution returns the distribution of keys across nodes
func (hs *HashSharding) GetNodeDistribution() map[string]int64 {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	distribution := make(map[string]int64)
	for nodeID, count := range hs.stats.NodeDistribution {
		distribution[nodeID] = count
	}

	return distribution
}

// ResetStats resets the statistics
func (hs *HashSharding) ResetStats() {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hs.stats.KeysDistributed = 0
	hs.stats.HashCollisions = 0
	hs.stats.RebalanceCount = 0

	for nodeID := range hs.stats.NodeDistribution {
		hs.stats.NodeDistribution[nodeID] = 0
	}

	hs.logger.Info("hash sharding statistics reset")
}

// SetHashFunction sets a custom hash function
func (hs *HashSharding) SetHashFunction(hashFunc HashFunction) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hs.hashFunc = hashFunc
	hs.algorithm = HashAlgorithmCustom

	hs.logger.Info("hash function updated", logger.String("function", hashFunc.Name()))
}

// GetHashFunction returns the current hash function
func (hs *HashSharding) GetHashFunction() HashFunction {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	return hs.hashFunc
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Standard hash function implementations

// MD5HashFunction implements MD5 hash function
type MD5HashFunction struct{}

func (h *MD5HashFunction) Hash(key string) uint64 {
	hasher := md5.New()
	hasher.Write([]byte(key))
	hashBytes := hasher.Sum(nil)
	return bytesToUint64(hashBytes)
}

func (h *MD5HashFunction) Name() string {
	return "md5"
}

// SHA1HashFunction implements SHA1 hash function
type SHA1HashFunction struct{}

func (h *SHA1HashFunction) Hash(key string) uint64 {
	hasher := sha1.New()
	hasher.Write([]byte(key))
	hashBytes := hasher.Sum(nil)
	return bytesToUint64(hashBytes)
}

func (h *SHA1HashFunction) Name() string {
	return "sha1"
}

// SHA256HashFunction implements SHA256 hash function
type SHA256HashFunction struct{}

func (h *SHA256HashFunction) Hash(key string) uint64 {
	hasher := sha256.New()
	hasher.Write([]byte(key))
	hashBytes := hasher.Sum(nil)
	return bytesToUint64(hashBytes)
}

func (h *SHA256HashFunction) Name() string {
	return "sha256"
}

// CRC32HashFunction implements CRC32 hash function
type CRC32HashFunction struct{}

func (h *CRC32HashFunction) Hash(key string) uint64 {
	return uint64(crc32.ChecksumIEEE([]byte(key)))
}

func (h *CRC32HashFunction) Name() string {
	return "crc32"
}

// FNV32HashFunction implements FNV32 hash function
type FNV32HashFunction struct{}

func (h *FNV32HashFunction) Hash(key string) uint64 {
	hasher := fnv.New32a()
	hasher.Write([]byte(key))
	return uint64(hasher.Sum32())
}

func (h *FNV32HashFunction) Name() string {
	return "fnv32"
}

// FNV64HashFunction implements FNV64 hash function
type FNV64HashFunction struct{}

func (h *FNV64HashFunction) Hash(key string) uint64 {
	hasher := fnv.New64a()
	hasher.Write([]byte(key))
	return hasher.Sum64()
}

func (h *FNV64HashFunction) Name() string {
	return "fnv64"
}

// NewHashFunction creates a hash function based on algorithm
func NewHashFunction(algorithm HashAlgorithm) HashFunction {
	switch algorithm {
	case HashAlgorithmMD5:
		return &MD5HashFunction{}
	case HashAlgorithmSHA1:
		return &SHA1HashFunction{}
	case HashAlgorithmSHA256:
		return &SHA256HashFunction{}
	case HashAlgorithmCRC32:
		return &CRC32HashFunction{}
	case HashAlgorithmFNV32:
		return &FNV32HashFunction{}
	case HashAlgorithmFNV64:
		return &FNV64HashFunction{}
	default:
		return &FNV64HashFunction{} // Default to FNV64
	}
}

// bytesToUint64 converts hash bytes to uint64
func bytesToUint64(bytes []byte) uint64 {
	var hash uint64
	for i := 0; i < 8 && i < len(bytes); i++ {
		hash = hash*256 + uint64(bytes[i])
	}
	return hash
}
