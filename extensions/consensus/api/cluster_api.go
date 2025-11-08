package api

import (
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/cluster"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// ClusterAPI provides cluster management endpoints.
type ClusterAPI struct {
	manager    *cluster.Manager
	membership *cluster.MembershipManager
	topology   *cluster.TopologyManager
	quorum     *cluster.QuorumManager
	rebalance  *cluster.RebalanceManager
	logger     forge.Logger
}

// NewClusterAPI creates a new cluster API.
func NewClusterAPI(
	manager *cluster.Manager,
	membership *cluster.MembershipManager,
	topology *cluster.TopologyManager,
	quorum *cluster.QuorumManager,
	rebalance *cluster.RebalanceManager,
	logger forge.Logger,
) *ClusterAPI {
	return &ClusterAPI{
		manager:    manager,
		membership: membership,
		topology:   topology,
		quorum:     quorum,
		rebalance:  rebalance,
		logger:     logger,
	}
}

// GetClusterStatus returns cluster status.
func (ca *ClusterAPI) GetClusterStatus(ctx forge.Context) error {
	nodes := ca.manager.GetNodes()

	healthyCount := 0
	unhealthyCount := 0

	for _, node := range nodes {
		if node.Status == internal.StatusActive {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	quorumInfo := ca.quorum.GetQuorumInfo()

	status := map[string]any{
		"total_nodes":     len(nodes),
		"healthy_nodes":   healthyCount,
		"unhealthy_nodes": unhealthyCount,
		"has_quorum":      quorumInfo.HasQuorum,
		"quorum_size":     quorumInfo.QuorumSize,
		"fault_tolerance": ca.quorum.GetFaultTolerance(),
	}

	return ctx.JSON(200, status)
}

// ListNodes returns all cluster nodes.
func (ca *ClusterAPI) ListNodes(ctx forge.Context) error {
	nodes := ca.manager.GetNodes()

	return ctx.JSON(200, map[string]any{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// GetNode returns information about a specific node.
func (ca *ClusterAPI) GetNode(ctx forge.Context) error {
	nodeID := ctx.Param("node_id")
	if nodeID == "" {
		return ctx.JSON(400, map[string]string{"error": "node_id is required"})
	}

	node, err := ca.manager.GetNode(nodeID)
	if err != nil || node == nil {
		return ctx.JSON(404, map[string]string{"error": "node not found"})
	}

	return ctx.JSON(200, node)
}

// AddNode adds a node to the cluster.
func (ca *ClusterAPI) AddNode(ctx forge.Context) error {
	var req struct {
		NodeID  string `json:"node_id"`
		Address string `json:"address"`
		Port    int    `json:"port"`
	}

	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid request"})
	}

	if req.NodeID == "" || req.Address == "" || req.Port == 0 {
		return ctx.JSON(400, map[string]string{"error": "node_id, address, and port are required"})
	}

	nodeInfo := internal.NodeInfo{
		ID:      req.NodeID,
		Address: req.Address,
		Port:    req.Port,
		Role:    internal.RoleFollower,
		Status:  internal.StatusActive,
	}

	if err := ca.membership.AddNode(ctx.Context(), nodeInfo); err != nil {
		return ctx.JSON(500, map[string]string{"error": err.Error()})
	}

	return ctx.JSON(201, map[string]any{
		"message": "node added successfully",
		"node":    nodeInfo,
	})
}

// RemoveNode removes a node from the cluster.
func (ca *ClusterAPI) RemoveNode(ctx forge.Context) error {
	nodeID := ctx.Param("node_id")
	if nodeID == "" {
		return ctx.JSON(400, map[string]string{"error": "node_id is required"})
	}

	// Check if removal is safe
	canRemove, reason := ca.membership.CanSafelyRemoveNode(nodeID)
	if !canRemove {
		return ctx.JSON(400, map[string]string{"error": reason})
	}

	if err := ca.membership.RemoveNode(ctx.Context(), nodeID); err != nil {
		return ctx.JSON(500, map[string]string{"error": err.Error()})
	}

	return ctx.JSON(200, map[string]string{"message": "node removed successfully"})
}

// GetQuorumStatus returns quorum information.
func (ca *ClusterAPI) GetQuorumStatus(ctx forge.Context) error {
	status := ca.quorum.GetQuorumStatus()

	return ctx.JSON(200, status)
}

// GetTopology returns cluster topology.
func (ca *ClusterAPI) GetTopology(ctx forge.Context) error {
	view := ca.topology.GetTopologyView()

	return ctx.JSON(200, view)
}

// AnalyzeBalance analyzes cluster balance.
func (ca *ClusterAPI) AnalyzeBalance(ctx forge.Context) error {
	analysis := ca.rebalance.AnalyzeBalance()

	return ctx.JSON(200, analysis)
}

// Rebalance triggers cluster rebalancing.
func (ca *ClusterAPI) Rebalance(ctx forge.Context) error {
	result, err := ca.rebalance.Rebalance(ctx.Context())
	if err != nil {
		return ctx.JSON(500, map[string]string{"error": err.Error()})
	}

	return ctx.JSON(200, result)
}

// GetMembershipChanges returns pending membership changes.
func (ca *ClusterAPI) GetMembershipChanges(ctx forge.Context) error {
	changes := ca.membership.GetPendingChanges()

	return ctx.JSON(200, map[string]any{
		"changes": changes,
		"count":   len(changes),
	})
}

// ValidateTopology validates cluster topology.
func (ca *ClusterAPI) ValidateTopology(ctx forge.Context) error {
	warnings := ca.topology.ValidateTopology()

	return ctx.JSON(200, map[string]any{
		"valid":    len(warnings) == 0,
		"warnings": warnings,
	})
}

// ValidateResilience validates cluster resilience.
func (ca *ClusterAPI) ValidateResilience(ctx forge.Context) error {
	warnings := ca.quorum.ValidateClusterResilience()

	return ctx.JSON(200, map[string]any{
		"valid":    len(warnings) == 0,
		"warnings": warnings,
	})
}

// GetClusterSize recommendations.
func (ca *ClusterAPI) GetClusterSizeRecommendations(ctx forge.Context) error {
	recommendations := ca.quorum.RecommendClusterSize()

	return ctx.JSON(200, recommendations)
}

// TransferLeadership initiates leadership transfer.
func (ca *ClusterAPI) TransferLeadership(ctx forge.Context) error {
	var req struct {
		TargetNode string `json:"target_node"`
	}

	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid request"})
	}

	if req.TargetNode == "" {
		return ctx.JSON(400, map[string]string{"error": "target_node is required"})
	}

	// Verify target node exists
	node, err := ca.manager.GetNode(req.TargetNode)
	if err != nil || node == nil {
		return ctx.JSON(404, map[string]string{"error": "target node not found"})
	}

	// TODO: Implement leadership transfer
	ca.logger.Info("leadership transfer requested",
		forge.F("target", req.TargetNode),
	)

	return ctx.JSON(200, map[string]string{
		"message": fmt.Sprintf("leadership transfer to %s initiated", req.TargetNode),
	})
}

// GetClusterHealth returns overall cluster health.
func (ca *ClusterAPI) GetClusterHealth(ctx forge.Context) error {
	nodes := ca.manager.GetNodes()
	quorumInfo := ca.quorum.GetQuorumInfo()

	healthStatus := "healthy"
	if !quorumInfo.HasQuorum {
		healthStatus = "critical"
	} else if !ca.quorum.IsQuorumHealthy() {
		healthStatus = "degraded"
	}

	health := map[string]any{
		"status":          healthStatus,
		"total_nodes":     len(nodes),
		"healthy_nodes":   quorumInfo.HealthyNodes,
		"has_quorum":      quorumInfo.HasQuorum,
		"quorum_margin":   ca.quorum.GetQuorumMargin(),
		"fault_tolerance": ca.quorum.GetFaultTolerance(),
		"warnings":        ca.quorum.ValidateClusterResilience(),
	}

	statusCode := 200

	switch healthStatus {
	case "critical":
		statusCode = 503
	case "degraded":
		statusCode = 200 // Still operational
	}

	return ctx.JSON(statusCode, health)
}
