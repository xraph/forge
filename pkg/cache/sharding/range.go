package sharding

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// RangeSharding implements range-based sharding for cache distribution
type RangeSharding struct {
	ranges   []*ShardRange
	nodeMap  map[string]*RangeNode
	strategy RangeStrategy
	logger   common.Logger
	metrics  common.Metrics
	stats    RangeShardingStats
	mu       sync.RWMutex
}

// RangeNode represents a node in range sharding
type RangeNode struct {
	ID       string                 `json:"id"`
	Address  string                 `json:"address"`
	Weight   float64                `json:"weight"`
	Capacity int64                  `json:"capacity"`
	Used     int64                  `json:"used"`
	Status   NodeStatus             `json:"status"`
	Ranges   []*ShardRange          `json:"ranges"`
	Metadata map[string]interface{} `json:"metadata"`
	Tags     []string               `json:"tags"`
}

// ShardRange represents a range of keys assigned to a node
type ShardRange struct {
	ID       string      `json:"id"`
	Start    interface{} `json:"start"`
	End      interface{} `json:"end"`
	NodeID   string      `json:"node_id"`
	Size     int64       `json:"size"`
	KeyCount int64       `json:"key_count"`
	Created  int64       `json:"created"`
	Updated  int64       `json:"updated"`
}

// RangeStrategy defines different range sharding strategies
type RangeStrategy string

const (
	RangeStrategyAlphabetic RangeStrategy = "alphabetic"
	RangeStrategyNumeric    RangeStrategy = "numeric"
	RangeStrategyDateTime   RangeStrategy = "datetime"
	RangeStrategyCustom     RangeStrategy = "custom"
)

// RangeShardingStats contains statistics for range sharding
type RangeShardingStats struct {
	TotalNodes       int                `json:"total_nodes"`
	ActiveNodes      int                `json:"active_nodes"`
	TotalRanges      int                `json:"total_ranges"`
	TotalCapacity    int64              `json:"total_capacity"`
	UsedCapacity     int64              `json:"used_capacity"`
	KeysDistributed  int64              `json:"keys_distributed"`
	RebalanceCount   int64              `json:"rebalance_count"`
	RangeStats       map[string]int64   `json:"range_stats"`
	NodeDistribution map[string]int64   `json:"node_distribution"`
	LoadBalance      LoadBalanceMetrics `json:"load_balance"`
}

// RangeShardingConfig contains configuration for range sharding
type RangeShardingConfig struct {
	Strategy           RangeStrategy `yaml:"strategy" json:"strategy"`
	InitialRanges      int           `yaml:"initial_ranges" json:"initial_ranges"`
	AutoRebalance      bool          `yaml:"auto_rebalance" json:"auto_rebalance"`
	RebalanceThreshold float64       `yaml:"rebalance_threshold" json:"rebalance_threshold"`
	RangeSize          int64         `yaml:"range_size" json:"range_size"`
	SplitThreshold     int64         `yaml:"split_threshold" json:"split_threshold"`
	MergeThreshold     int64         `yaml:"merge_threshold" json:"merge_threshold"`
}

// RangeComparator defines interface for comparing range keys
type RangeComparator interface {
	Compare(a, b interface{}) int
	InRange(key interface{}, start, end interface{}) bool
	Split(start, end interface{}) interface{}
}

// NewRangeSharding creates a new range sharding instance
func NewRangeSharding(config RangeShardingConfig, logger common.Logger, metrics common.Metrics) *RangeSharding {
	return &RangeSharding{
		ranges:   make([]*ShardRange, 0),
		nodeMap:  make(map[string]*RangeNode),
		strategy: config.Strategy,
		logger:   logger,
		metrics:  metrics,
		stats: RangeShardingStats{
			RangeStats:       make(map[string]int64),
			NodeDistribution: make(map[string]int64),
		},
	}
}

// AddNode adds a node to the range sharding
func (rs *RangeSharding) AddNode(node *RangeNode) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if _, exists := rs.nodeMap[node.ID]; exists {
		return fmt.Errorf("node %s already exists", node.ID)
	}

	rs.nodeMap[node.ID] = node
	rs.stats.NodeDistribution[node.ID] = 0

	// Initialize ranges for new node if this is the first node
	if len(rs.nodeMap) == 1 {
		if err := rs.initializeRanges(node); err != nil {
			delete(rs.nodeMap, node.ID)
			return err
		}
	}

	// Update stats
	rs.updateStats()

	rs.logger.Info("node added to range sharding",
		logger.String("node_id", node.ID),
		logger.String("address", node.Address),
		logger.Float64("weight", node.Weight),
		logger.Int("ranges", len(node.Ranges)),
	)

	if rs.metrics != nil {
		rs.metrics.Counter("forge.cache.sharding.range.nodes.added").Inc()
		rs.metrics.Gauge("forge.cache.sharding.range.nodes.total").Set(float64(rs.stats.TotalNodes))
	}

	return nil
}

// RemoveNode removes a node from the range sharding
func (rs *RangeSharding) RemoveNode(nodeID string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	node, exists := rs.nodeMap[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Reassign ranges from the removed node
	if err := rs.reassignRanges(node); err != nil {
		return err
	}

	delete(rs.nodeMap, nodeID)
	delete(rs.stats.NodeDistribution, nodeID)

	// Update stats
	rs.updateStats()

	rs.logger.Info("node removed from range sharding",
		logger.String("node_id", nodeID),
		logger.Int("reassigned_ranges", len(node.Ranges)),
	)

	if rs.metrics != nil {
		rs.metrics.Counter("forge.cache.sharding.range.nodes.removed").Inc()
		rs.metrics.Gauge("forge.cache.sharding.range.nodes.total").Set(float64(rs.stats.TotalNodes))
	}

	return nil
}

// GetNode returns the node responsible for a given key
func (rs *RangeSharding) GetNode(key string) (*RangeNode, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if len(rs.ranges) == 0 {
		return nil, fmt.Errorf("no ranges available")
	}

	// Find the range containing the key
	rangeInfo := rs.findRange(key)
	if rangeInfo == nil {
		return nil, fmt.Errorf("no range found for key: %s", key)
	}

	node, exists := rs.nodeMap[rangeInfo.NodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found for range %s", rangeInfo.NodeID, rangeInfo.ID)
	}

	// Only return active nodes
	if node.Status != NodeStatusActive {
		// Find backup node for this range
		backupNode := rs.findBackupNode(rangeInfo)
		if backupNode == nil {
			return nil, fmt.Errorf("no active nodes available for range %s", rangeInfo.ID)
		}
		node = backupNode
	}

	// Update distribution stats
	rs.stats.NodeDistribution[node.ID]++
	rs.stats.KeysDistributed++
	rs.stats.RangeStats[rangeInfo.ID]++

	if rs.metrics != nil {
		rs.metrics.Counter("forge.cache.sharding.range.keys.distributed", "node", node.ID).Inc()
		rs.metrics.Counter("forge.cache.sharding.range.range.accessed", "range", rangeInfo.ID).Inc()
	}

	return node, nil
}

// GetNodes returns multiple nodes for replication within a range
func (rs *RangeSharding) GetNodes(key string, count int) ([]*RangeNode, error) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if len(rs.ranges) == 0 {
		return nil, fmt.Errorf("no ranges available")
	}

	if count <= 0 {
		count = 1
	}

	// Find the primary range for the key
	rangeInfo := rs.findRange(key)
	if rangeInfo == nil {
		return nil, fmt.Errorf("no range found for key: %s", key)
	}

	// Get primary node
	primaryNode, exists := rs.nodeMap[rangeInfo.NodeID]
	if !exists || primaryNode.Status != NodeStatusActive {
		return nil, fmt.Errorf("primary node not available for range %s", rangeInfo.ID)
	}

	var nodes []*RangeNode
	nodes = append(nodes, primaryNode)

	// Get additional nodes for replication if requested
	if count > 1 {
		replicaNodes := rs.findReplicaNodes(rangeInfo, count-1)
		nodes = append(nodes, replicaNodes...)
	}

	// Update stats
	rs.stats.KeysDistributed++
	for _, node := range nodes {
		rs.stats.NodeDistribution[node.ID]++
	}

	return nodes, nil
}

// GetAllNodes returns all nodes
func (rs *RangeSharding) GetAllNodes() []*RangeNode {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	nodes := make([]*RangeNode, 0, len(rs.nodeMap))
	for _, node := range rs.nodeMap {
		nodes = append(nodes, node)
	}

	return nodes
}

// GetActiveNodes returns all active nodes
func (rs *RangeSharding) GetActiveNodes() []*RangeNode {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	var nodes []*RangeNode
	for _, node := range rs.nodeMap {
		if node.Status == NodeStatusActive {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// UpdateNodeStatus updates the status of a node
func (rs *RangeSharding) UpdateNodeStatus(nodeID string, status NodeStatus) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	node, exists := rs.nodeMap[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	oldStatus := node.Status
	node.Status = status

	// Update stats
	rs.updateStats()

	rs.logger.Info("node status updated",
		logger.String("node_id", nodeID),
		logger.String("old_status", string(oldStatus)),
		logger.String("new_status", string(status)),
	)

	if rs.metrics != nil {
		rs.metrics.Gauge("forge.cache.sharding.range.nodes.active").Set(float64(rs.stats.ActiveNodes))
	}

	return nil
}

// SplitRange splits a range into two ranges
func (rs *RangeSharding) SplitRange(rangeID string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rangeInfo := rs.findRangeByID(rangeID)
	if rangeInfo == nil {
		return fmt.Errorf("range %s not found", rangeID)
	}

	comparator := rs.getComparator()
	splitPoint := comparator.Split(rangeInfo.Start, rangeInfo.End)

	// Create new range
	newRange := &ShardRange{
		ID:       fmt.Sprintf("%s-split", rangeID),
		Start:    splitPoint,
		End:      rangeInfo.End,
		NodeID:   rangeInfo.NodeID,
		Size:     rangeInfo.Size / 2,
		KeyCount: rangeInfo.KeyCount / 2,
		Created:  rangeInfo.Created,
		Updated:  rangeInfo.Updated,
	}

	// Update original range
	rangeInfo.End = splitPoint
	rangeInfo.Size = rangeInfo.Size / 2
	rangeInfo.KeyCount = rangeInfo.KeyCount / 2

	// Add new range to collections
	rs.ranges = append(rs.ranges, newRange)
	rs.stats.RangeStats[newRange.ID] = 0

	// Sort ranges
	rs.sortRanges()

	rs.logger.Info("range split",
		logger.String("original_range", rangeID),
		logger.String("new_range", newRange.ID),
		logger.String("split_point", fmt.Sprintf("%v", splitPoint)),
	)

	if rs.metrics != nil {
		rs.metrics.Counter("forge.cache.sharding.range.splits").Inc()
		rs.metrics.Gauge("forge.cache.sharding.range.total").Set(float64(len(rs.ranges)))
	}

	return nil
}

// MergeRanges merges two adjacent ranges
func (rs *RangeSharding) MergeRanges(rangeID1, rangeID2 string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	range1 := rs.findRangeByID(rangeID1)
	range2 := rs.findRangeByID(rangeID2)

	if range1 == nil || range2 == nil {
		return fmt.Errorf("one or both ranges not found")
	}

	if range1.NodeID != range2.NodeID {
		return fmt.Errorf("ranges must be on the same node to merge")
	}

	// Ensure ranges are adjacent
	comparator := rs.getComparator()
	if comparator.Compare(range1.End, range2.Start) != 0 {
		return fmt.Errorf("ranges are not adjacent")
	}

	// Merge range2 into range1
	range1.End = range2.End
	range1.Size += range2.Size
	range1.KeyCount += range2.KeyCount

	// Remove range2
	rs.removeRangeFromSlice(rangeID2)
	delete(rs.stats.RangeStats, rangeID2)

	rs.logger.Info("ranges merged",
		logger.String("merged_range", rangeID1),
		logger.String("removed_range", rangeID2),
	)

	if rs.metrics != nil {
		rs.metrics.Counter("forge.cache.sharding.range.merges").Inc()
		rs.metrics.Gauge("forge.cache.sharding.range.total").Set(float64(len(rs.ranges)))
	}

	return nil
}

// Rebalance redistributes ranges across nodes
func (rs *RangeSharding) Rebalance() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Calculate current load distribution
	rs.calculateLoadBalance()

	// Check if rebalancing is needed
	if rs.stats.LoadBalance.Imbalance < 0.2 { // 20% threshold
		return nil // No rebalancing needed
	}

	// Find overloaded and underloaded nodes
	overloadedNodes := rs.findOverloadedNodes()
	underloadedNodes := rs.findUnderloadedNodes()

	// Move ranges from overloaded to underloaded nodes
	for _, overloaded := range overloadedNodes {
		for _, underloaded := range underloadedNodes {
			if rs.shouldMoveRange(overloaded, underloaded) {
				if err := rs.moveRange(overloaded, underloaded); err != nil {
					rs.logger.Error("failed to move range during rebalance",
						logger.String("from", overloaded.ID),
						logger.String("to", underloaded.ID),
						logger.Error(err),
					)
				}
			}
		}
	}

	rs.stats.RebalanceCount++

	rs.logger.Info("range sharding rebalanced",
		logger.Int("total_ranges", len(rs.ranges)),
		logger.Int("total_nodes", len(rs.nodeMap)),
		logger.Float64("imbalance", rs.stats.LoadBalance.Imbalance),
	)

	if rs.metrics != nil {
		rs.metrics.Counter("forge.cache.sharding.range.rebalances").Inc()
	}

	return nil
}

// GetStats returns range sharding statistics
func (rs *RangeSharding) GetStats() RangeShardingStats {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	// Calculate load balance metrics
	rs.calculateLoadBalance()

	return rs.stats
}

// Helper methods

func (rs *RangeSharding) initializeRanges(node *RangeNode) error {
	// Create initial range covering all possible keys
	initialRange := &ShardRange{
		ID:       "range-0",
		Start:    rs.getMinKey(),
		End:      rs.getMaxKey(),
		NodeID:   node.ID,
		Size:     0,
		KeyCount: 0,
		Created:  0,
		Updated:  0,
	}

	rs.ranges = append(rs.ranges, initialRange)
	rs.stats.RangeStats[initialRange.ID] = 0
	node.Ranges = append(node.Ranges, initialRange)

	return nil
}

func (rs *RangeSharding) findRange(key string) *ShardRange {
	comparator := rs.getComparator()

	for _, rangeInfo := range rs.ranges {
		if comparator.InRange(key, rangeInfo.Start, rangeInfo.End) {
			return rangeInfo
		}
	}

	return nil
}

func (rs *RangeSharding) findRangeByID(rangeID string) *ShardRange {
	for _, rangeInfo := range rs.ranges {
		if rangeInfo.ID == rangeID {
			return rangeInfo
		}
	}
	return nil
}

func (rs *RangeSharding) findBackupNode(rangeInfo *ShardRange) *RangeNode {
	for _, node := range rs.nodeMap {
		if node.ID != rangeInfo.NodeID && node.Status == NodeStatusActive {
			return node
		}
	}
	return nil
}

func (rs *RangeSharding) findReplicaNodes(rangeInfo *ShardRange, count int) []*RangeNode {
	var replicas []*RangeNode

	for _, node := range rs.nodeMap {
		if node.ID != rangeInfo.NodeID && node.Status == NodeStatusActive && len(replicas) < count {
			replicas = append(replicas, node)
		}
	}

	return replicas
}

func (rs *RangeSharding) reassignRanges(removedNode *RangeNode) error {
	activeNodes := rs.getActiveNodesExcept(removedNode.ID)
	if len(activeNodes) == 0 {
		return fmt.Errorf("no active nodes available for range reassignment")
	}

	nodeIndex := 0
	for _, rangeInfo := range removedNode.Ranges {
		// Assign to next available node
		newNode := activeNodes[nodeIndex%len(activeNodes)]
		rangeInfo.NodeID = newNode.ID
		newNode.Ranges = append(newNode.Ranges, rangeInfo)
		nodeIndex++
	}

	return nil
}

func (rs *RangeSharding) getActiveNodesExcept(excludeNodeID string) []*RangeNode {
	var activeNodes []*RangeNode
	for _, node := range rs.nodeMap {
		if node.ID != excludeNodeID && node.Status == NodeStatusActive {
			activeNodes = append(activeNodes, node)
		}
	}
	return activeNodes
}

func (rs *RangeSharding) updateStats() {
	rs.stats.TotalNodes = len(rs.nodeMap)
	rs.stats.ActiveNodes = 0
	rs.stats.TotalRanges = len(rs.ranges)
	rs.stats.TotalCapacity = 0
	rs.stats.UsedCapacity = 0

	for _, node := range rs.nodeMap {
		if node.Status == NodeStatusActive {
			rs.stats.ActiveNodes++
		}
		rs.stats.TotalCapacity += node.Capacity
		rs.stats.UsedCapacity += node.Used
	}
}

func (rs *RangeSharding) calculateLoadBalance() {
	if len(rs.stats.NodeDistribution) == 0 {
		return
	}

	var loads []float64
	var total int64

	for _, count := range rs.stats.NodeDistribution {
		total += count
		loads = append(loads, float64(count))
	}

	if len(loads) == 0 {
		return
	}

	// Calculate average
	average := float64(total) / float64(len(loads))
	rs.stats.LoadBalance.AverageLoad = average

	// Find min and max
	sort.Float64s(loads)
	rs.stats.LoadBalance.MinLoad = loads[0]
	rs.stats.LoadBalance.MaxLoad = loads[len(loads)-1]

	// Calculate variance
	var variance float64
	for _, load := range loads {
		diff := load - average
		variance += diff * diff
	}
	rs.stats.LoadBalance.LoadVariance = variance / float64(len(loads))

	// Calculate imbalance
	if average > 0 {
		rs.stats.LoadBalance.Imbalance = (rs.stats.LoadBalance.MaxLoad - rs.stats.LoadBalance.MinLoad) / average
	}
}

func (rs *RangeSharding) findOverloadedNodes() []*RangeNode {
	var overloaded []*RangeNode
	threshold := rs.stats.LoadBalance.AverageLoad * 1.2 // 20% above average

	for nodeID, load := range rs.stats.NodeDistribution {
		if float64(load) > threshold {
			if node, exists := rs.nodeMap[nodeID]; exists {
				overloaded = append(overloaded, node)
			}
		}
	}

	return overloaded
}

func (rs *RangeSharding) findUnderloadedNodes() []*RangeNode {
	var underloaded []*RangeNode
	threshold := rs.stats.LoadBalance.AverageLoad * 0.8 // 20% below average

	for nodeID, load := range rs.stats.NodeDistribution {
		if float64(load) < threshold {
			if node, exists := rs.nodeMap[nodeID]; exists && node.Status == NodeStatusActive {
				underloaded = append(underloaded, node)
			}
		}
	}

	return underloaded
}

func (rs *RangeSharding) shouldMoveRange(from, to *RangeNode) bool {
	fromLoad := rs.stats.NodeDistribution[from.ID]
	toLoad := rs.stats.NodeDistribution[to.ID]

	// Only move if it would improve balance
	return fromLoad > toLoad+1
}

func (rs *RangeSharding) moveRange(from, to *RangeNode) error {
	if len(from.Ranges) == 0 {
		return fmt.Errorf("no ranges to move from node %s", from.ID)
	}

	// Move the first range
	rangeToMove := from.Ranges[0]
	rangeToMove.NodeID = to.ID

	// Update node ranges
	from.Ranges = from.Ranges[1:]
	to.Ranges = append(to.Ranges, rangeToMove)

	return nil
}

func (rs *RangeSharding) removeRangeFromSlice(rangeID string) {
	for i, rangeInfo := range rs.ranges {
		if rangeInfo.ID == rangeID {
			rs.ranges = append(rs.ranges[:i], rs.ranges[i+1:]...)
			break
		}
	}
}

func (rs *RangeSharding) sortRanges() {
	comparator := rs.getComparator()
	sort.Slice(rs.ranges, func(i, j int) bool {
		return comparator.Compare(rs.ranges[i].Start, rs.ranges[j].Start) < 0
	})
}

func (rs *RangeSharding) getComparator() RangeComparator {
	switch rs.strategy {
	case RangeStrategyAlphabetic:
		return &AlphabeticComparator{}
	case RangeStrategyNumeric:
		return &NumericComparator{}
	case RangeStrategyDateTime:
		return &DateTimeComparator{}
	default:
		return &AlphabeticComparator{}
	}
}

func (rs *RangeSharding) getMinKey() interface{} {
	switch rs.strategy {
	case RangeStrategyNumeric:
		return int64(0)
	default:
		return ""
	}
}

func (rs *RangeSharding) getMaxKey() interface{} {
	switch rs.strategy {
	case RangeStrategyNumeric:
		return int64(9223372036854775807) // MaxInt64
	default:
		return "zzzzzzz"
	}
}

// Range comparator implementations

// AlphabeticComparator implements alphabetic range comparison
type AlphabeticComparator struct{}

func (c *AlphabeticComparator) Compare(a, b interface{}) int {
	strA, _ := a.(string)
	strB, _ := b.(string)
	return strings.Compare(strA, strB)
}

func (c *AlphabeticComparator) InRange(key interface{}, start, end interface{}) bool {
	keyStr, _ := key.(string)
	startStr, _ := start.(string)
	endStr, _ := end.(string)

	return strings.Compare(keyStr, startStr) >= 0 && strings.Compare(keyStr, endStr) < 0
}

func (c *AlphabeticComparator) Split(start, end interface{}) interface{} {
	startStr, _ := start.(string)
	endStr, _ := end.(string)

	if len(startStr) == 0 {
		return string(endStr[0] / 2)
	}

	// Simple midpoint calculation
	startRune := rune(startStr[0])
	endRune := rune(endStr[0])
	midRune := rune((int(startRune) + int(endRune)) / 2)

	return string(midRune)
}

// NumericComparator implements numeric range comparison
type NumericComparator struct{}

func (c *NumericComparator) Compare(a, b interface{}) int {
	numA := c.toInt64(a)
	numB := c.toInt64(b)

	if numA < numB {
		return -1
	} else if numA > numB {
		return 1
	}
	return 0
}

func (c *NumericComparator) InRange(key interface{}, start, end interface{}) bool {
	keyNum := c.toInt64(key)
	startNum := c.toInt64(start)
	endNum := c.toInt64(end)

	return keyNum >= startNum && keyNum < endNum
}

func (c *NumericComparator) Split(start, end interface{}) interface{} {
	startNum := c.toInt64(start)
	endNum := c.toInt64(end)
	return (startNum + endNum) / 2
}

func (c *NumericComparator) toInt64(val interface{}) int64 {
	switch v := val.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		if num, err := strconv.ParseInt(v, 10, 64); err == nil {
			return num
		}
	}
	return 0
}

// DateTimeComparator implements datetime range comparison
type DateTimeComparator struct{}

func (c *DateTimeComparator) Compare(a, b interface{}) int {
	timeA := c.toTimestamp(a)
	timeB := c.toTimestamp(b)

	if timeA < timeB {
		return -1
	} else if timeA > timeB {
		return 1
	}
	return 0
}

func (c *DateTimeComparator) InRange(key interface{}, start, end interface{}) bool {
	keyTime := c.toTimestamp(key)
	startTime := c.toTimestamp(start)
	endTime := c.toTimestamp(end)

	return keyTime >= startTime && keyTime < endTime
}

func (c *DateTimeComparator) Split(start, end interface{}) interface{} {
	startTime := c.toTimestamp(start)
	endTime := c.toTimestamp(end)
	return (startTime + endTime) / 2
}

func (c *DateTimeComparator) toTimestamp(val interface{}) int64 {
	switch v := val.(type) {
	case int64:
		return v
	case string:
		if timestamp, err := strconv.ParseInt(v, 10, 64); err == nil {
			return timestamp
		}
	}
	return 0
}

// GetRanges returns all ranges
func (rs *RangeSharding) GetRanges() []*ShardRange {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	ranges := make([]*ShardRange, len(rs.ranges))
	copy(ranges, rs.ranges)
	return ranges
}

// GetNodeDistribution returns the distribution of keys across nodes
func (rs *RangeSharding) GetNodeDistribution() map[string]int64 {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	distribution := make(map[string]int64)
	for nodeID, count := range rs.stats.NodeDistribution {
		distribution[nodeID] = count
	}

	return distribution
}

// ResetStats resets the statistics
func (rs *RangeSharding) ResetStats() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.stats.KeysDistributed = 0
	rs.stats.RebalanceCount = 0

	for nodeID := range rs.stats.NodeDistribution {
		rs.stats.NodeDistribution[nodeID] = 0
	}

	for rangeID := range rs.stats.RangeStats {
		rs.stats.RangeStats[rangeID] = 0
	}

	rs.logger.Info("range sharding statistics reset")
}
