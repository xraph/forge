package consensus

import (
	"sync"
	"time"
)

// Node defines the interface for a consensus node
type Node interface {
	ID() string
	Address() string
	Role() NodeRole
	Status() NodeStatus
	LastHeartbeat() time.Time
	GetMetadata() map[string]interface{}
	SetMetadata(key string, value interface{})
	IsLocal() bool
	UpdateHeartbeat()
	SetRole(role NodeRole)
	SetStatus(status NodeStatus)
	SetAddress(address string)
	GetStats() NodeStats
}

// NodeRole represents the role of a node in the consensus algorithm
type NodeRole string

const (
	NodeRoleLeader    NodeRole = "leader"
	NodeRoleFollower  NodeRole = "follower"
	NodeRoleCandidate NodeRole = "candidate"
)

// NodeStatus represents the status of a node
type NodeStatus string

const (
	NodeStatusActive    NodeStatus = "active"
	NodeStatusInactive  NodeStatus = "inactive"
	NodeStatusSuspected NodeStatus = "suspected"
	NodeStatusFailed    NodeStatus = "failed"
)

// NodeImpl implements the Node interface
type NodeImpl struct {
	id            string
	address       string
	role          NodeRole
	status        NodeStatus
	lastHeartbeat time.Time
	metadata      map[string]interface{}
	isLocal       bool
	createdAt     time.Time
	mu            sync.RWMutex
}

// NodeStats contains statistics about a node
type NodeStats struct {
	ID            string                 `json:"id"`
	Address       string                 `json:"address"`
	Role          NodeRole               `json:"role"`
	Status        NodeStatus             `json:"status"`
	LastHeartbeat time.Time              `json:"last_heartbeat"`
	IsLocal       bool                   `json:"is_local"`
	CreatedAt     time.Time              `json:"created_at"`
	Uptime        time.Duration          `json:"uptime"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// NewNode creates a new node
func NewNode(id string, isLocal bool) *NodeImpl {
	return &NodeImpl{
		id:            id,
		role:          NodeRoleFollower,
		status:        NodeStatusActive,
		lastHeartbeat: time.Now(),
		metadata:      make(map[string]interface{}),
		isLocal:       isLocal,
		createdAt:     time.Now(),
	}
}

// ID returns the node ID
func (n *NodeImpl) ID() string {
	return n.id
}

// Address returns the node address
func (n *NodeImpl) Address() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.address
}

// SetAddress sets the node address
func (n *NodeImpl) SetAddress(address string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.address = address
}

// Role returns the node role
func (n *NodeImpl) Role() NodeRole {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.role
}

// SetRole sets the node role
func (n *NodeImpl) SetRole(role NodeRole) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.role = role
}

// Status returns the node status
func (n *NodeImpl) Status() NodeStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.status
}

// SetStatus sets the node status
func (n *NodeImpl) SetStatus(status NodeStatus) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.status = status
}

// LastHeartbeat returns the last heartbeat time
func (n *NodeImpl) LastHeartbeat() time.Time {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.lastHeartbeat
}

// UpdateHeartbeat updates the last heartbeat time
func (n *NodeImpl) UpdateHeartbeat() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastHeartbeat = time.Now()
}

// GetMetadata returns all metadata
func (n *NodeImpl) GetMetadata() map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()

	metadata := make(map[string]interface{})
	for k, v := range n.metadata {
		metadata[k] = v
	}

	return metadata
}

// SetMetadata sets metadata for the node
func (n *NodeImpl) SetMetadata(key string, value interface{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.metadata[key] = value
}

// IsLocal returns true if this is the local node
func (n *NodeImpl) IsLocal() bool {
	return n.isLocal
}

// GetStats returns node statistics
func (n *NodeImpl) GetStats() NodeStats {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return NodeStats{
		ID:            n.id,
		Address:       n.address,
		Role:          n.role,
		Status:        n.status,
		LastHeartbeat: n.lastHeartbeat,
		IsLocal:       n.isLocal,
		CreatedAt:     n.createdAt,
		Uptime:        time.Since(n.createdAt),
		Metadata:      n.GetMetadata(),
	}
}

// IsHealthy returns true if the node is healthy
func (n *NodeImpl) IsHealthy() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// Consider a node unhealthy if we haven't heard from it in 30 seconds
	if time.Since(n.lastHeartbeat) > 30*time.Second {
		return false
	}

	return n.status == NodeStatusActive
}

// IsSuspected returns true if the node is suspected of failure
func (n *NodeImpl) IsSuspected() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.status == NodeStatusSuspected || n.status == NodeStatusFailed
}

// IsLeader returns true if this node is the leader
func (n *NodeImpl) IsLeader() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.role == NodeRoleLeader
}

// IsFollower returns true if this node is a follower
func (n *NodeImpl) IsFollower() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.role == NodeRoleFollower
}

// IsCandidate returns true if this node is a candidate
func (n *NodeImpl) IsCandidate() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.role == NodeRoleCandidate
}

// GetHeartbeatAge returns the age of the last heartbeat
func (n *NodeImpl) GetHeartbeatAge() time.Duration {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return time.Since(n.lastHeartbeat)
}

// String returns a string representation of the node
func (n *NodeImpl) String() string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.id
}

// NodeManager manages a collection of nodes
type NodeManager struct {
	nodes     map[string]*NodeImpl
	localNode *NodeImpl
	mu        sync.RWMutex
}

// NewNodeManager creates a new node manager
func NewNodeManager() *NodeManager {
	return &NodeManager{
		nodes: make(map[string]*NodeImpl),
	}
}

// AddNode adds a node to the manager
func (nm *NodeManager) AddNode(node *NodeImpl) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	nm.nodes[node.ID()] = node

	if node.IsLocal() {
		nm.localNode = node
	}
}

// RemoveNode removes a node from the manager
func (nm *NodeManager) RemoveNode(nodeID string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if node, exists := nm.nodes[nodeID]; exists {
		if node.IsLocal() {
			nm.localNode = nil
		}
		delete(nm.nodes, nodeID)
	}
}

// GetNode returns a node by ID
func (nm *NodeManager) GetNode(nodeID string) (*NodeImpl, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	node, exists := nm.nodes[nodeID]
	return node, exists
}

// GetNodes returns all nodes
func (nm *NodeManager) GetNodes() []*NodeImpl {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	nodes := make([]*NodeImpl, 0, len(nm.nodes))
	for _, node := range nm.nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// GetLocalNode returns the local node
func (nm *NodeManager) GetLocalNode() *NodeImpl {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	return nm.localNode
}

// GetActiveNodes returns all active nodes
func (nm *NodeManager) GetActiveNodes() []*NodeImpl {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var activeNodes []*NodeImpl
	for _, node := range nm.nodes {
		if node.Status() == NodeStatusActive {
			activeNodes = append(activeNodes, node)
		}
	}

	return activeNodes
}

// GetHealthyNodes returns all healthy nodes
func (nm *NodeManager) GetHealthyNodes() []*NodeImpl {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var healthyNodes []*NodeImpl
	for _, node := range nm.nodes {
		if node.IsHealthy() {
			healthyNodes = append(healthyNodes, node)
		}
	}

	return healthyNodes
}

// GetLeader returns the current leader node
func (nm *NodeManager) GetLeader() *NodeImpl {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	for _, node := range nm.nodes {
		if node.IsLeader() {
			return node
		}
	}

	return nil
}

// GetFollowers returns all follower nodes
func (nm *NodeManager) GetFollowers() []*NodeImpl {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var followers []*NodeImpl
	for _, node := range nm.nodes {
		if node.IsFollower() {
			followers = append(followers, node)
		}
	}

	return followers
}

// GetCandidates returns all candidate nodes
func (nm *NodeManager) GetCandidates() []*NodeImpl {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var candidates []*NodeImpl
	for _, node := range nm.nodes {
		if node.IsCandidate() {
			candidates = append(candidates, node)
		}
	}

	return candidates
}

// HasQuorum returns true if there is a quorum of healthy nodes
func (nm *NodeManager) HasQuorum() bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	healthyNodes := 0
	for _, node := range nm.nodes {
		if node.IsHealthy() {
			healthyNodes++
		}
	}

	quorum := (len(nm.nodes) / 2) + 1
	return healthyNodes >= quorum
}

// Count returns the total number of nodes
func (nm *NodeManager) Count() int {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	return len(nm.nodes)
}

// ActiveCount returns the number of active nodes
func (nm *NodeManager) ActiveCount() int {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	count := 0
	for _, node := range nm.nodes {
		if node.Status() == NodeStatusActive {
			count++
		}
	}

	return count
}

// HealthyCount returns the number of healthy nodes
func (nm *NodeManager) HealthyCount() int {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	count := 0
	for _, node := range nm.nodes {
		if node.IsHealthy() {
			count++
		}
	}

	return count
}

// UpdateHeartbeats updates heartbeats for all nodes
func (nm *NodeManager) UpdateHeartbeats() {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	for _, node := range nm.nodes {
		if node.IsLocal() {
			node.UpdateHeartbeat()
		}
	}
}

// CheckHealth checks the health of all nodes
func (nm *NodeManager) CheckHealth() {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	for _, node := range nm.nodes {
		if !node.IsHealthy() && node.Status() == NodeStatusActive {
			node.SetStatus(NodeStatusSuspected)
		}
	}
}

// GetStats returns statistics for all nodes
func (nm *NodeManager) GetStats() map[string]NodeStats {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	stats := make(map[string]NodeStats)
	for id, node := range nm.nodes {
		stats[id] = node.GetStats()
	}

	return stats
}
