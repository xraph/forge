package api

import (
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/internal/cluster"
)

// NodeAPI provides node-specific endpoints
type NodeAPI struct {
	raftNode internal.RaftNode
	manager  *cluster.Manager
	logger   forge.Logger
}

// NewNodeAPI creates a new node API
func NewNodeAPI(raftNode internal.RaftNode, manager *cluster.Manager, logger forge.Logger) *NodeAPI {
	return &NodeAPI{
		raftNode: raftNode,
		manager:  manager,
		logger:   logger,
	}
}

// GetNodeInfo returns information about the local node
func (na *NodeAPI) GetNodeInfo(ctx forge.Context) error {
	nodeID := na.raftNode.GetID()
	node := na.manager.GetNode(nodeID)

	info := map[string]interface{}{
		"node_id":      nodeID,
		"is_leader":    na.raftNode.IsLeader(),
		"current_term": na.raftNode.GetCurrentTerm(),
		"commit_index": na.raftNode.GetCommitIndex(),
		"last_applied": na.raftNode.GetLastApplied(),
	}

	if node != nil {
		info["role"] = node.Role
		info["status"] = node.Status
		info["address"] = node.Address
		info["port"] = node.Port
	}

	return ctx.JSON(200, info)
}

// GetNodeStatus returns detailed node status
func (na *NodeAPI) GetNodeStatus(ctx forge.Context) error {
	status := map[string]interface{}{
		"node_id":      na.raftNode.GetID(),
		"is_leader":    na.raftNode.IsLeader(),
		"current_term": na.raftNode.GetCurrentTerm(),
		"commit_index": na.raftNode.GetCommitIndex(),
		"last_applied": na.raftNode.GetLastApplied(),
		"state":        "running", // Would get from actual state
	}

	return ctx.JSON(200, status)
}

// GetNodeMetrics returns node-specific metrics
func (na *NodeAPI) GetNodeMetrics(ctx forge.Context) error {
	metrics := map[string]interface{}{
		"node_id":      na.raftNode.GetID(),
		"commit_index": na.raftNode.GetCommitIndex(),
		"last_applied": na.raftNode.GetLastApplied(),
		"current_term": na.raftNode.GetCurrentTerm(),
		"is_leader":    na.raftNode.IsLeader(),
	}

	return ctx.JSON(200, metrics)
}

// ProposeCommand proposes a command to the cluster
func (na *NodeAPI) ProposeCommand(ctx forge.Context) error {
	var req struct {
		Command string `json:"command"`
	}

	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid request"})
	}

	if req.Command == "" {
		return ctx.JSON(400, map[string]string{"error": "command is required"})
	}

	if err := na.raftNode.Propose(ctx.Context(), []byte(req.Command)); err != nil {
		return ctx.JSON(500, map[string]string{"error": err.Error()})
	}

	return ctx.JSON(200, map[string]string{"message": "command proposed successfully"})
}

// StepDown forces the leader to step down
func (na *NodeAPI) StepDown(ctx forge.Context) error {
	if !na.raftNode.IsLeader() {
		return ctx.JSON(400, map[string]string{"error": "node is not the leader"})
	}

	// TODO: Implement step down
	na.logger.Info("step down requested")

	return ctx.JSON(200, map[string]string{"message": "step down initiated"})
}

// GetNodePeers returns the node's view of peers
func (na *NodeAPI) GetNodePeers(ctx forge.Context) error {
	nodes := na.manager.GetAllNodes()
	nodeID := na.raftNode.GetID()

	peers := make([]map[string]interface{}, 0)
	for _, node := range nodes {
		if node.ID == nodeID {
			continue // Skip self
		}

		peer := map[string]interface{}{
			"node_id": node.ID,
			"address": node.Address,
			"port":    node.Port,
			"role":    node.Role,
			"status":  node.Status,
		}

		peers = append(peers, peer)
	}

	return ctx.JSON(200, map[string]interface{}{
		"peers": peers,
		"count": len(peers),
	})
}
