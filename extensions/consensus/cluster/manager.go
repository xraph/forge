package cluster

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// Manager implements cluster management
type Manager struct {
	id      string
	nodes   map[string]*NodeState
	nodesMu sync.RWMutex
	logger  forge.Logger

	// Health checking
	healthCheckInterval time.Duration
	healthTimeout       time.Duration

	// Lifecycle
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// NodeState represents the state of a node in the cluster
type NodeState struct {
	Info          internal.NodeInfo
	LastHeartbeat time.Time
	LastContact   time.Time
	FailureCount  int
	Healthy       bool
	mu            sync.RWMutex
}

// ManagerConfig contains cluster manager configuration
type ManagerConfig struct {
	NodeID              string
	HealthCheckInterval time.Duration
	HealthTimeout       time.Duration
}

// NewManager creates a new cluster manager
func NewManager(config ManagerConfig, logger forge.Logger) *Manager {
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 5 * time.Second
	}

	if config.HealthTimeout == 0 {
		config.HealthTimeout = 10 * time.Second
	}

	return &Manager{
		id:                  config.NodeID,
		nodes:               make(map[string]*NodeState),
		logger:              logger,
		healthCheckInterval: config.HealthCheckInterval,
		healthTimeout:       config.HealthTimeout,
	}
}

// Start starts the cluster manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return internal.ErrAlreadyStarted
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true

	// Start health checker
	m.wg.Add(1)
	go m.runHealthChecker()

	m.logger.Info("cluster manager started",
		forge.F("node_id", m.id),
	)

	return nil
}

// Stop stops the cluster manager
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return internal.ErrNotStarted
	}
	m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("cluster manager stopped", forge.F("node_id", m.id))
	case <-ctx.Done():
		m.logger.Warn("cluster manager stop timed out", forge.F("node_id", m.id))
	}

	return nil
}

// GetNodes returns all nodes in the cluster
func (m *Manager) GetNodes() []internal.NodeInfo {
	m.nodesMu.RLock()
	defer m.nodesMu.RUnlock()

	nodes := make([]internal.NodeInfo, 0, len(m.nodes))
	for _, state := range m.nodes {
		state.mu.RLock()
		nodes = append(nodes, state.Info)
		state.mu.RUnlock()
	}

	return nodes
}

// GetNode returns information about a specific node
func (m *Manager) GetNode(nodeID string) (*internal.NodeInfo, error) {
	m.nodesMu.RLock()
	defer m.nodesMu.RUnlock()

	state, exists := m.nodes[nodeID]
	if !exists {
		return nil, internal.ErrNodeNotFound
	}

	state.mu.RLock()
	info := state.Info
	state.mu.RUnlock()

	return &info, nil
}

// AddNode adds a node to the cluster
func (m *Manager) AddNode(nodeID, address string, port int) error {
	m.nodesMu.Lock()
	defer m.nodesMu.Unlock()

	if _, exists := m.nodes[nodeID]; exists {
		return internal.ErrPeerExists
	}

	now := time.Now()
	m.nodes[nodeID] = &NodeState{
		Info: internal.NodeInfo{
			ID:            nodeID,
			Address:       address,
			Port:          port,
			Role:          internal.RoleFollower,
			Status:        internal.StatusActive,
			LastHeartbeat: now,
			Metadata:      make(map[string]interface{}),
		},
		LastHeartbeat: now,
		LastContact:   now,
		Healthy:       true,
	}

	m.logger.Info("node added to cluster",
		forge.F("node_id", nodeID),
		forge.F("address", address),
		forge.F("port", port),
	)

	return nil
}

// RemoveNode removes a node from the cluster
func (m *Manager) RemoveNode(nodeID string) error {
	m.nodesMu.Lock()
	defer m.nodesMu.Unlock()

	if _, exists := m.nodes[nodeID]; !exists {
		return internal.ErrNodeNotFound
	}

	delete(m.nodes, nodeID)

	m.logger.Info("node removed from cluster",
		forge.F("node_id", nodeID),
	)

	return nil
}

// UpdateNode updates node information
func (m *Manager) UpdateNode(nodeID string, info internal.NodeInfo) error {
	m.nodesMu.RLock()
	state, exists := m.nodes[nodeID]
	m.nodesMu.RUnlock()

	if !exists {
		return internal.ErrNodeNotFound
	}

	state.mu.Lock()
	state.Info = info
	state.mu.Unlock()

	m.logger.Debug("node updated",
		forge.F("node_id", nodeID),
		forge.F("role", info.Role),
		forge.F("status", info.Status),
	)

	return nil
}

// UpdateNodeRole updates a node's role
func (m *Manager) UpdateNodeRole(nodeID string, role internal.NodeRole) error {
	m.nodesMu.RLock()
	state, exists := m.nodes[nodeID]
	m.nodesMu.RUnlock()

	if !exists {
		return internal.ErrNodeNotFound
	}

	state.mu.Lock()
	state.Info.Role = role
	state.mu.Unlock()

	m.logger.Info("node role updated",
		forge.F("node_id", nodeID),
		forge.F("role", role),
	)

	return nil
}

// UpdateNodeStatus updates a node's status
func (m *Manager) UpdateNodeStatus(nodeID string, status internal.NodeStatus) error {
	m.nodesMu.RLock()
	state, exists := m.nodes[nodeID]
	m.nodesMu.RUnlock()

	if !exists {
		return internal.ErrNodeNotFound
	}

	state.mu.Lock()
	oldStatus := state.Info.Status
	state.Info.Status = status
	if status == internal.StatusActive {
		state.Healthy = true
		state.FailureCount = 0
	}
	state.mu.Unlock()

	if oldStatus != status {
		m.logger.Info("node status updated",
			forge.F("node_id", nodeID),
			forge.F("old_status", oldStatus),
			forge.F("new_status", status),
		)
	}

	return nil
}

// RecordHeartbeat records a heartbeat from a node
func (m *Manager) RecordHeartbeat(nodeID string) error {
	m.nodesMu.RLock()
	state, exists := m.nodes[nodeID]
	m.nodesMu.RUnlock()

	if !exists {
		return internal.ErrNodeNotFound
	}

	now := time.Now()
	state.mu.Lock()
	state.LastHeartbeat = now
	state.LastContact = now
	state.Info.LastHeartbeat = now
	state.Healthy = true
	state.FailureCount = 0
	if state.Info.Status != internal.StatusActive {
		state.Info.Status = internal.StatusActive
	}
	state.mu.Unlock()

	return nil
}

// GetLeader returns the current leader node
func (m *Manager) GetLeader() *internal.NodeInfo {
	m.nodesMu.RLock()
	defer m.nodesMu.RUnlock()

	for _, state := range m.nodes {
		state.mu.RLock()
		if state.Info.Role == internal.RoleLeader {
			info := state.Info
			state.mu.RUnlock()
			return &info
		}
		state.mu.RUnlock()
	}

	return nil
}

// HasQuorum returns true if the cluster has quorum
func (m *Manager) HasQuorum() bool {
	totalNodes := m.GetClusterSize()
	healthyNodes := m.GetHealthyNodes()

	if totalNodes == 0 {
		return false
	}

	majority := (totalNodes / 2) + 1
	return healthyNodes >= majority
}

// GetClusterSize returns the size of the cluster
func (m *Manager) GetClusterSize() int {
	m.nodesMu.RLock()
	defer m.nodesMu.RUnlock()
	return len(m.nodes)
}

// GetHealthyNodes returns the number of healthy nodes
func (m *Manager) GetHealthyNodes() int {
	m.nodesMu.RLock()
	defer m.nodesMu.RUnlock()

	count := 0
	for _, state := range m.nodes {
		state.mu.RLock()
		if state.Healthy && state.Info.Status == internal.StatusActive {
			count++
		}
		state.mu.RUnlock()
	}

	return count
}

// GetClusterInfo returns cluster information
func (m *Manager) GetClusterInfo() *internal.ClusterInfo {
	m.nodesMu.RLock()
	defer m.nodesMu.RUnlock()

	nodes := make([]internal.NodeInfo, 0, len(m.nodes))
	var leaderID string
	var term uint64
	activeNodes := 0

	for _, state := range m.nodes {
		state.mu.RLock()
		nodes = append(nodes, state.Info)
		if state.Info.Role == internal.RoleLeader {
			leaderID = state.Info.ID
			term = state.Info.Term
		}
		if state.Info.Status == internal.StatusActive {
			activeNodes++
		}
		state.mu.RUnlock()
	}

	return &internal.ClusterInfo{
		ID:          m.id,
		Leader:      leaderID,
		Term:        term,
		Nodes:       nodes,
		TotalNodes:  len(nodes),
		ActiveNodes: activeNodes,
		HasQuorum:   m.HasQuorum(),
	}
}

// runHealthChecker runs the health checker
func (m *Manager) runHealthChecker() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return

		case <-ticker.C:
			m.checkNodeHealth()
		}
	}
}

// checkNodeHealth checks the health of all nodes
func (m *Manager) checkNodeHealth() {
	m.nodesMu.RLock()
	nodes := make([]*NodeState, 0, len(m.nodes))
	for _, state := range m.nodes {
		nodes = append(nodes, state)
	}
	m.nodesMu.RUnlock()

	now := time.Now()

	for _, state := range nodes {
		state.mu.Lock()

		// Check if heartbeat timeout has passed
		timeSinceHeartbeat := now.Sub(state.LastHeartbeat)
		if timeSinceHeartbeat > m.healthTimeout {
			if state.Healthy {
				m.logger.Warn("node became unhealthy",
					forge.F("node_id", state.Info.ID),
					forge.F("time_since_heartbeat", timeSinceHeartbeat),
				)
			}

			state.Healthy = false
			state.FailureCount++

			// Update status based on failure count
			if state.FailureCount >= 3 {
				state.Info.Status = internal.StatusFailed
			} else if state.FailureCount >= 2 {
				state.Info.Status = internal.StatusSuspected
			} else {
				state.Info.Status = internal.StatusInactive
			}
		}

		state.mu.Unlock()
	}
}

// GetNodeStats returns statistics about the nodes
func (m *Manager) GetNodeStats() map[string]interface{} {
	m.nodesMu.RLock()
	defer m.nodesMu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_nodes"] = len(m.nodes)
	stats["healthy_nodes"] = m.GetHealthyNodes()
	stats["has_quorum"] = m.HasQuorum()

	roleCount := make(map[internal.NodeRole]int)
	statusCount := make(map[internal.NodeStatus]int)

	for _, state := range m.nodes {
		state.mu.RLock()
		roleCount[state.Info.Role]++
		statusCount[state.Info.Status]++
		state.mu.RUnlock()
	}

	stats["roles"] = roleCount
	stats["statuses"] = statusCount

	return stats
}
