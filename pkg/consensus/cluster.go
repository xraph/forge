package consensus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/discovery"
	"github.com/xraph/forge/pkg/consensus/raft"
	"github.com/xraph/forge/pkg/consensus/statemachine"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/consensus/transport"
	"github.com/xraph/forge/pkg/logger"
)

// Cluster defines the interface for a consensus cluster
type Cluster interface {
	ID() string
	GetNodes() []Node
	GetLeader() Node
	ProposeChange(ctx context.Context, change StateChange) error
	GetState() interface{}
	Subscribe(callback StateChangeCallback) error
	GetHealth() ClusterHealth
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	HealthCheck(ctx context.Context) error
	JoinNode(ctx context.Context, nodeID string) error
	LeaveNode(ctx context.Context, nodeID string) error
	GetStats() ClusterStats
}

// ClusterImpl implements the Cluster interface
type ClusterImpl struct {
	id           string
	config       ClusterConfig
	nodeID       string
	nodes        map[string]*NodeImpl
	leader       *NodeImpl
	raftNode     raft.RaftNode
	stateMachine statemachine.StateMachine
	transport    transport.Transport
	storage      storage.Storage
	discovery    discovery.Discovery
	logger       common.Logger
	metrics      common.Metrics
	subscribers  []StateChangeCallback
	mu           sync.RWMutex
	started      bool
	shutdownCh   chan struct{}
	lastElection time.Time
	startTime    time.Time
}

type StateMachineConfig = statemachine.StateMachineConfig

// ClusterConfig contains configuration for a cluster
type ClusterConfig struct {
	ID           string             `json:"id"`
	Nodes        []string           `json:"nodes"`
	StateMachine string             `json:"state_machine"`
	Config       StateMachineConfig `json:"config"`
	InitialState StateMachineConfig `json:"initial_state"`
}

// ClusterHealth represents the health status of a cluster
type ClusterHealth struct {
	Status        common.HealthStatus   `json:"status"`
	LeaderPresent bool                  `json:"leader_present"`
	ActiveNodes   int                   `json:"active_nodes"`
	TotalNodes    int                   `json:"total_nodes"`
	LastElection  time.Time             `json:"last_election"`
	NodeHealth    map[string]NodeHealth `json:"node_health"`
	Issues        []string              `json:"issues"`
}

// NodeHealth represents the health status of a node
type NodeHealth struct {
	NodeID        string        `json:"node_id"`
	Status        NodeStatus    `json:"status"`
	LastHeartbeat time.Time     `json:"last_heartbeat"`
	Latency       time.Duration `json:"latency"`
	IsReachable   bool          `json:"is_reachable"`
	Role          NodeRole      `json:"role"`
}

// StateChange represents a state change in the cluster
type StateChange struct {
	Type      string                 `json:"type"`
	Key       string                 `json:"key"`
	Value     interface{}            `json:"value"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
}

// StateChangeCallback is called when state changes
type StateChangeCallback func(change StateChange) error

// NewCluster creates a new cluster
func NewCluster(id string, config ClusterConfig, nodeID string, transport transport.Transport, storage storage.Storage, discovery discovery.Discovery, logger common.Logger, metrics common.Metrics) *ClusterImpl {
	return &ClusterImpl{
		id:         id,
		config:     config,
		nodeID:     nodeID,
		nodes:      make(map[string]*NodeImpl),
		transport:  transport,
		storage:    storage,
		discovery:  discovery,
		logger:     logger,
		metrics:    metrics,
		shutdownCh: make(chan struct{}),
	}
}

// ID returns the cluster ID
func (c *ClusterImpl) ID() string {
	return c.id
}

// Start starts the cluster
func (c *ClusterImpl) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("cluster %s already started", c.id)
	}

	c.startTime = time.Now()

	// Initialize state machine
	stateMachine, err := c.createStateMachine()
	if err != nil {
		return fmt.Errorf("failed to create state machine: %w", err)
	}
	c.stateMachine = stateMachine

	// Initialize nodes
	for _, nodeID := range c.config.Nodes {
		node := NewNode(nodeID, c.nodeID == nodeID)
		c.nodes[nodeID] = node
	}

	// Create Raft node
	raftConfig := raft.NodeConfig{
		ID: c.nodeID,
		// ClusterID:         c.id,
		// Nodes:             c.config.Nodes,
		ElectionTimeout:   5 * time.Second,
		HeartbeatInterval: 1 * time.Second,
	}

	raftNode, err := raft.NewNode(
		raftConfig,
		c.storage,
		c.transport,
		c.stateMachine,
		c.discovery,
		c.logger,
		c.metrics,
	)
	if err != nil {
		return fmt.Errorf("failed to create Raft node: %w", err)
	}
	c.raftNode = raftNode

	// OnStart Raft node
	if err := c.raftNode.Start(ctx); err != nil {
		return fmt.Errorf("failed to start Raft node: %w", err)
	}

	// OnStart leader election monitoring
	go c.monitorLeadership()

	c.started = true

	if c.logger != nil {
		c.logger.Info("cluster started",
			logger.String("cluster_id", c.id),
			logger.String("node_id", c.nodeID),
			logger.Int("nodes", len(c.nodes)),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.consensus.cluster_started").Inc()
		c.metrics.Gauge("forge.consensus.cluster_nodes", "cluster", c.id).Set(float64(len(c.nodes)))
	}

	return nil
}

// Stop stops the cluster
func (c *ClusterImpl) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return fmt.Errorf("cluster %s not started", c.id)
	}

	// Signal shutdown
	close(c.shutdownCh)

	// OnStop Raft node
	if c.raftNode != nil {
		if err := c.raftNode.Stop(ctx); err != nil {
			if c.logger != nil {
				c.logger.Error("failed to stop Raft node", logger.Error(err))
			}
		}
	}

	c.started = false

	if c.logger != nil {
		c.logger.Info("cluster stopped", logger.String("cluster_id", c.id))
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.consensus.cluster_stopped").Inc()
	}

	return nil
}

// HealthCheck performs health check on the cluster
func (c *ClusterImpl) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return fmt.Errorf("cluster %s not started", c.id)
	}

	// Check Raft node health
	if c.raftNode != nil {
		if err := c.raftNode.HealthCheck(ctx); err != nil {
			return fmt.Errorf("Raft node unhealthy: %w", err)
		}
	}

	// Check node connectivity
	activeNodes := 0
	for nodeID, node := range c.nodes {
		if node.Status() == NodeStatusActive {
			activeNodes++
		} else if c.logger != nil {
			c.logger.Warn("node not active",
				logger.String("cluster_id", c.id),
				logger.String("node_id", nodeID),
				logger.String("status", string(node.Status())),
			)
		}
	}

	// Check if we have a quorum
	requiredNodes := (len(c.nodes) / 2) + 1
	if activeNodes < requiredNodes {
		return fmt.Errorf("insufficient active nodes: %d/%d (required: %d)", activeNodes, len(c.nodes), requiredNodes)
	}

	return nil
}

// GetNodes returns all nodes in the cluster
func (c *ClusterImpl) GetNodes() []Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodes := make([]Node, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// GetLeader returns the current leader node
func (c *ClusterImpl) GetLeader() Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.leader
}

// ProposeChange proposes a state change to the cluster
func (c *ClusterImpl) ProposeChange(ctx context.Context, change StateChange) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return fmt.Errorf("cluster %s not started", c.id)
	}

	if c.raftNode == nil {
		return fmt.Errorf("Raft node not initialized")
	}

	// Create log entry
	entry := storage.LogEntry{
		Type:      storage.EntryTypeApplication,
		Data:      c.serializeStateChange(change),
		Metadata:  change.Metadata,
		Timestamp: time.Now(),
	}

	// Propose through Raft
	if err := c.raftNode.AppendEntries(ctx, []storage.LogEntry{entry}); err != nil {
		return fmt.Errorf("failed to propose change: %w", err)
	}

	// Record metrics
	if c.metrics != nil {
		c.metrics.Counter("forge.consensus.state_changes_proposed", "cluster", c.id).Inc()
	}

	return nil
}

// GetState returns the current state
func (c *ClusterImpl) GetState() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.stateMachine == nil {
		return nil
	}

	return c.stateMachine.GetState()
}

// Subscribe subscribes to state changes
func (c *ClusterImpl) Subscribe(callback StateChangeCallback) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.subscribers = append(c.subscribers, callback)
	return nil
}

// GetHealth returns the cluster health
func (c *ClusterImpl) GetHealth() ClusterHealth {
	c.mu.RLock()
	defer c.mu.RUnlock()

	health := ClusterHealth{
		Status:        common.HealthStatusHealthy,
		LeaderPresent: c.leader != nil,
		ActiveNodes:   0,
		TotalNodes:    len(c.nodes),
		LastElection:  c.lastElection,
		NodeHealth:    make(map[string]NodeHealth),
		Issues:        []string{},
	}

	// Check node health
	for nodeID, node := range c.nodes {
		nodeHealth := NodeHealth{
			NodeID:        nodeID,
			Status:        node.Status(),
			LastHeartbeat: node.LastHeartbeat(),
			IsReachable:   node.Status() == NodeStatusActive,
			Role:          node.Role(),
		}

		health.NodeHealth[nodeID] = nodeHealth

		if node.Status() == NodeStatusActive {
			health.ActiveNodes++
		}
	}

	// Determine overall health
	requiredNodes := (len(c.nodes) / 2) + 1
	if health.ActiveNodes < requiredNodes {
		health.Status = common.HealthStatusUnhealthy
		health.Issues = append(health.Issues, fmt.Sprintf("insufficient active nodes: %d/%d", health.ActiveNodes, len(c.nodes)))
	} else if !health.LeaderPresent {
		health.Status = common.HealthStatusDegraded
		health.Issues = append(health.Issues, "no leader present")
	}

	return health
}

// JoinNode adds a node to the cluster
func (c *ClusterImpl) JoinNode(ctx context.Context, nodeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.nodes[nodeID]; exists {
		return fmt.Errorf("node %s already exists in cluster", nodeID)
	}

	node := NewNode(nodeID, false)
	c.nodes[nodeID] = node

	if c.logger != nil {
		c.logger.Info("node joined cluster",
			logger.String("cluster_id", c.id),
			logger.String("node_id", nodeID),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.consensus.nodes_joined", "cluster", c.id).Inc()
		c.metrics.Gauge("forge.consensus.cluster_nodes", "cluster", c.id).Set(float64(len(c.nodes)))
	}

	return nil
}

// LeaveNode removes a node from the cluster
func (c *ClusterImpl) LeaveNode(ctx context.Context, nodeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.nodes[nodeID]; !exists {
		return fmt.Errorf("node %s not found in cluster", nodeID)
	}

	delete(c.nodes, nodeID)

	if c.logger != nil {
		c.logger.Info("node left cluster",
			logger.String("cluster_id", c.id),
			logger.String("node_id", nodeID),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.consensus.nodes_left", "cluster", c.id).Inc()
		c.metrics.Gauge("forge.consensus.cluster_nodes", "cluster", c.id).Set(float64(len(c.nodes)))
	}

	return nil
}

// GetStats returns cluster statistics
func (c *ClusterImpl) GetStats() ClusterStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := ClusterStats{
		ID:           c.id,
		Status:       c.getStatus(),
		Nodes:        len(c.nodes),
		ActiveNodes:  c.getActiveNodeCount(),
		LastElection: c.lastElection,
	}

	if c.leader != nil {
		stats.LeaderID = c.leader.ID()
	}

	if c.raftNode != nil {
		raftStats := c.raftNode.GetStats()
		stats.Term = raftStats.CurrentTerm
		stats.CommitIndex = raftStats.CommitIndex
		stats.LastApplied = raftStats.LastApplied
		stats.LogSize = raftStats.LogSize
		stats.SnapshotSize = raftStats.SnapshotCount
	}

	return stats
}

// monitorLeadership monitors leadership changes
func (c *ClusterImpl) monitorLeadership() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.updateLeadership()
		case <-c.shutdownCh:
			return
		}
	}
}

// updateLeadership updates the current leader
func (c *ClusterImpl) updateLeadership() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.raftNode == nil {
		return
	}

	leaderID := c.raftNode.GetLeader()
	if leaderID == "" {
		if c.leader != nil {
			c.leader = nil
			c.lastElection = time.Now()

			if c.logger != nil {
				c.logger.Info("leader lost", logger.String("cluster_id", c.id))
			}
		}
		return
	}

	if c.leader == nil || c.leader.ID() != leaderID {
		if node, exists := c.nodes[leaderID]; exists {
			c.leader = node
			c.lastElection = time.Now()

			if c.logger != nil {
				c.logger.Info("new leader elected",
					logger.String("cluster_id", c.id),
					logger.String("leader_id", leaderID),
				)
			}

			if c.metrics != nil {
				c.metrics.Counter("forge.consensus.leader_elections", "cluster", c.id).Inc()
			}
		}
	}
}

// getStatus returns the cluster status
func (c *ClusterImpl) getStatus() ClusterStatus {
	if !c.started {
		return ClusterStatusInactive
	}

	if c.leader == nil {
		return ClusterStatusElecting
	}

	activeNodes := c.getActiveNodeCount()
	requiredNodes := (len(c.nodes) / 2) + 1
	if activeNodes < requiredNodes {
		return ClusterStatusError
	}

	return ClusterStatusActive
}

// getActiveNodeCount returns the number of active nodes
func (c *ClusterImpl) getActiveNodeCount() int {
	count := 0
	for _, node := range c.nodes {
		if node.Status() == NodeStatusActive {
			count++
		}
	}
	return count
}

// createStateMachine creates a state machine based on configuration
func (c *ClusterImpl) createStateMachine() (statemachine.StateMachine, error) {
	switch c.config.StateMachine {
	case "memory":
		return statemachine.NewMemoryStateMachineFactory().Create(c.config.InitialState)
	case "persistent":
		return statemachine.NewPersistentStateMachineFactory(c.logger, c.metrics).Create(c.config.Config)
	case "application":
		return statemachine.NewApplicationStateMachineFactory(c.logger, c.metrics).Create(c.config.Config)
	default:
		return statemachine.NewMemoryStateMachineFactory().Create(c.config.InitialState)
	}
}

// serializeStateChange serializes a state change to bytes
func (c *ClusterImpl) serializeStateChange(change StateChange) []byte {
	// This is a simplified serialization - in production, use a proper serializer
	data := fmt.Sprintf("%s:%s:%v", change.Type, change.Key, change.Value)
	return []byte(data)
}

// notifySubscribers notifies all subscribers of a state change
func (c *ClusterImpl) notifySubscribers(change StateChange) {
	for _, callback := range c.subscribers {
		go func(cb StateChangeCallback) {
			if err := cb(change); err != nil && c.logger != nil {
				c.logger.Error("state change callback failed", logger.Error(err))
			}
		}(callback)
	}
}

// IsStarted returns true if the cluster is started
func (c *ClusterImpl) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}
