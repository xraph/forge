package cluster

import (
	"fmt"
	"sort"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// QuorumManager manages quorum calculations and checks
type QuorumManager struct {
	manager *Manager
	logger  forge.Logger
}

// QuorumInfo contains quorum information
type QuorumInfo struct {
	TotalNodes    int
	HealthyNodes  int
	QuorumSize    int
	HasQuorum     bool
	MajorityIndex int
}

// NewQuorumManager creates a new quorum manager
func NewQuorumManager(manager *Manager, logger forge.Logger) *QuorumManager {
	return &QuorumManager{
		manager: manager,
		logger:  logger,
	}
}

// CalculateQuorumSize calculates the quorum size for a given cluster size
func (qm *QuorumManager) CalculateQuorumSize(clusterSize int) int {
	if clusterSize <= 0 {
		return 0
	}

	// Quorum is majority: (n/2) + 1
	return (clusterSize / 2) + 1
}

// HasQuorum checks if the cluster currently has quorum
func (qm *QuorumManager) HasQuorum() bool {
	info := qm.GetQuorumInfo()
	return info.HasQuorum
}

// GetQuorumInfo returns detailed quorum information
func (qm *QuorumManager) GetQuorumInfo() QuorumInfo {
	nodes := qm.manager.GetNodes()

	totalNodes := len(nodes)
	healthyNodes := 0

	for _, node := range nodes {
		if node.Status == internal.StatusActive {
			healthyNodes++
		}
	}

	quorumSize := qm.CalculateQuorumSize(totalNodes)
	hasQuorum := healthyNodes >= quorumSize

	return QuorumInfo{
		TotalNodes:    totalNodes,
		HealthyNodes:  healthyNodes,
		QuorumSize:    quorumSize,
		HasQuorum:     hasQuorum,
		MajorityIndex: quorumSize - 1,
	}
}

// CheckIndexCommitted checks if a log index has been replicated to quorum
func (qm *QuorumManager) CheckIndexCommitted(replicatedIndexes map[string]uint64) (uint64, bool) {
	if len(replicatedIndexes) == 0 {
		return 0, false
	}

	// Get all indexes and sort them
	indexes := make([]uint64, 0, len(replicatedIndexes))
	for _, index := range replicatedIndexes {
		indexes = append(indexes, index)
	}

	// Sort in descending order
	sort.Slice(indexes, func(i, j int) bool {
		return indexes[i] > indexes[j]
	})

	// Get quorum size
	nodes := qm.manager.GetNodes()
	quorumSize := qm.CalculateQuorumSize(len(nodes))

	// Check if we have quorum
	if len(indexes) < quorumSize {
		return 0, false
	}

	// The index at position (quorumSize-1) is replicated to quorum
	committedIndex := indexes[quorumSize-1]

	return committedIndex, true
}

// GetFaultTolerance returns the number of nodes that can fail
func (qm *QuorumManager) GetFaultTolerance() int {
	nodes := qm.manager.GetNodes()
	totalNodes := len(nodes)

	if totalNodes == 0 {
		return 0
	}

	quorumSize := qm.CalculateQuorumSize(totalNodes)

	// Fault tolerance is: totalNodes - quorumSize
	return totalNodes - quorumSize
}

// WouldHaveQuorum checks if quorum would exist after a change
func (qm *QuorumManager) WouldHaveQuorum(healthyNodesAfterChange int, totalNodesAfterChange int) bool {
	quorumSize := qm.CalculateQuorumSize(totalNodesAfterChange)
	return healthyNodesAfterChange >= quorumSize
}

// GetMinimumNodesForQuorum returns minimum nodes needed for quorum
func (qm *QuorumManager) GetMinimumNodesForQuorum(totalNodes int) int {
	return qm.CalculateQuorumSize(totalNodes)
}

// ValidateQuorumForRemoval checks if removing nodes would maintain quorum
func (qm *QuorumManager) ValidateQuorumForRemoval(nodeIDs []string) (bool, string) {
	nodes := qm.manager.GetNodes()

	currentHealthy := 0
	for _, node := range nodes {
		if node.Status == internal.StatusActive {
			currentHealthy++
		}
	}

	// Assume removed nodes are currently healthy (worst case)
	healthyAfterRemoval := currentHealthy
	for _, nodeID := range nodeIDs {
		node, err := qm.manager.GetNode(nodeID)
		if err == nil && node.Status == internal.StatusActive {
			healthyAfterRemoval--
		}
	}

	totalAfterRemoval := len(nodes) - len(nodeIDs)

	if qm.WouldHaveQuorum(healthyAfterRemoval, totalAfterRemoval) {
		return true, ""
	}

	quorumNeeded := qm.CalculateQuorumSize(totalAfterRemoval)
	return false, fmt.Sprintf(
		"removing nodes would break quorum (healthy after: %d, quorum needed: %d)",
		healthyAfterRemoval, quorumNeeded)
}

// GetQuorumStatus returns a detailed status of quorum health
func (qm *QuorumManager) GetQuorumStatus() map[string]interface{} {
	info := qm.GetQuorumInfo()
	faultTolerance := qm.GetFaultTolerance()

	status := "healthy"
	if !info.HasQuorum {
		status = "no_quorum"
	} else if info.HealthyNodes == info.QuorumSize {
		status = "at_risk" // One more failure loses quorum
	}

	return map[string]interface{}{
		"status":           status,
		"has_quorum":       info.HasQuorum,
		"total_nodes":      info.TotalNodes,
		"healthy_nodes":    info.HealthyNodes,
		"quorum_size":      info.QuorumSize,
		"fault_tolerance":  faultTolerance,
		"nodes_until_loss": info.HealthyNodes - info.QuorumSize,
	}
}

// RecommendClusterSize recommends optimal cluster sizes
func (qm *QuorumManager) RecommendClusterSize() map[int]ClusterSizeRecommendation {
	recommendations := make(map[int]ClusterSizeRecommendation)

	sizes := []int{1, 3, 5, 7, 9}
	for _, size := range sizes {
		quorum := qm.CalculateQuorumSize(size)
		faultTolerance := size - quorum

		rec := ClusterSizeRecommendation{
			ClusterSize:    size,
			QuorumSize:     quorum,
			FaultTolerance: faultTolerance,
		}

		// Add notes
		switch size {
		case 1:
			rec.Notes = "No fault tolerance. Suitable only for development."
		case 3:
			rec.Notes = "Minimum production size. Tolerates 1 failure."
		case 5:
			rec.Notes = "Recommended production size. Tolerates 2 failures."
		case 7:
			rec.Notes = "High availability. Tolerates 3 failures."
		case 9:
			rec.Notes = "Maximum recommended. Tolerates 4 failures."
		}

		recommendations[size] = rec
	}

	return recommendations
}

// ClusterSizeRecommendation contains cluster size recommendations
type ClusterSizeRecommendation struct {
	ClusterSize    int
	QuorumSize     int
	FaultTolerance int
	Notes          string
}

// GetNodesByMatchIndex returns nodes sorted by their match index
func (qm *QuorumManager) GetNodesByMatchIndex(matchIndexes map[string]uint64) []NodeMatchIndex {
	result := make([]NodeMatchIndex, 0, len(matchIndexes))

	for nodeID, index := range matchIndexes {
		result = append(result, NodeMatchIndex{
			NodeID:     nodeID,
			MatchIndex: index,
		})
	}

	// Sort by match index descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].MatchIndex > result[j].MatchIndex
	})

	return result
}

// NodeMatchIndex represents a node's match index
type NodeMatchIndex struct {
	NodeID     string
	MatchIndex uint64
}

// CalculateReplicationFactor returns effective replication factor
func (qm *QuorumManager) CalculateReplicationFactor() int {
	nodes := qm.manager.GetNodes()

	healthyNodes := 0
	for _, node := range nodes {
		if node.Status == internal.StatusActive {
			healthyNodes++
		}
	}

	// Replication factor is number of healthy nodes
	return healthyNodes
}

// IsQuorumHealthy checks if quorum is healthy (more than minimum)
func (qm *QuorumManager) IsQuorumHealthy() bool {
	info := qm.GetQuorumInfo()

	if !info.HasQuorum {
		return false
	}

	// Healthy if we have at least one node beyond quorum
	return info.HealthyNodes > info.QuorumSize
}

// GetQuorumMargin returns how many nodes above quorum we have
func (qm *QuorumManager) GetQuorumMargin() int {
	info := qm.GetQuorumInfo()

	if !info.HasQuorum {
		return 0
	}

	return info.HealthyNodes - info.QuorumSize
}

// ValidateClusterResilience validates overall cluster resilience
func (qm *QuorumManager) ValidateClusterResilience() []string {
	var warnings []string

	info := qm.GetQuorumInfo()

	if !info.HasQuorum {
		warnings = append(warnings, "CRITICAL: Cluster does not have quorum")
		return warnings
	}

	// Check fault tolerance
	faultTolerance := qm.GetFaultTolerance()
	if faultTolerance == 0 {
		warnings = append(warnings, "CRITICAL: No fault tolerance - any failure loses quorum")
	} else if faultTolerance == 1 {
		warnings = append(warnings, "WARNING: Limited fault tolerance - can only survive 1 failure")
	}

	// Check if at minimum quorum
	if info.HealthyNodes == info.QuorumSize {
		warnings = append(warnings, "WARNING: At minimum quorum - one more failure loses quorum")
	}

	// Check cluster size
	if info.TotalNodes < 3 {
		warnings = append(warnings, "WARNING: Cluster size less than 3 - not recommended for production")
	}

	// Check if even number
	if info.TotalNodes%2 == 0 {
		warnings = append(warnings, "INFO: Even cluster size - odd numbers provide better fault tolerance")
	}

	return warnings
}
