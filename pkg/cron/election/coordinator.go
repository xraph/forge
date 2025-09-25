package election

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// NodeCoordinator manages coordination between nodes in the cluster
type NodeCoordinator struct {
	// Configuration
	config *CoordinatorConfig
	nodeID string

	// Node management
	nodes     map[string]*NodeInfo
	nodesMu   sync.RWMutex
	localNode *NodeInfo

	// Leadership tracking
	currentLeader string
	leaderHistory []LeadershipEvent
	leaderMu      sync.RWMutex

	// Service discovery
	discovery Discovery

	// Health monitoring
	healthChecker *NodeHealthChecker

	// Communication
	transport ClusterTransport

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics

	// Event callbacks
	callbacks []CoordinatorCallback
}

// CoordinatorConfig contains configuration for node coordination
type CoordinatorConfig struct {
	NodeID              string                 `json:"node_id" yaml:"node_id"`
	ClusterID           string                 `json:"cluster_id" yaml:"cluster_id"`
	HeartbeatInterval   time.Duration          `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	NodeTimeout         time.Duration          `json:"node_timeout" yaml:"node_timeout"`
	DiscoveryInterval   time.Duration          `json:"discovery_interval" yaml:"discovery_interval"`
	MaxNodes            int                    `json:"max_nodes" yaml:"max_nodes"`
	DiscoveryType       string                 `json:"discovery_type" yaml:"discovery_type"` // "static", "dns", "consul"
	DiscoveryConfig     map[string]interface{} `json:"discovery_config" yaml:"discovery_config"`
	TransportConfig     map[string]interface{} `json:"transport_config" yaml:"transport_config"`
	EnableHealthMonitor bool                   `json:"enable_health_monitor" yaml:"enable_health_monitor"`
	EnableMetrics       bool                   `json:"enable_metrics" yaml:"enable_metrics"`
}

// NodeInfo represents information about a cluster node
type NodeInfo struct {
	ID           string                 `json:"id"`
	Address      string                 `json:"address"`
	Port         int                    `json:"port"`
	Status       NodeStatus             `json:"status"`
	Role         NodeRole               `json:"role"`
	Metadata     map[string]interface{} `json:"metadata"`
	Capabilities []string               `json:"capabilities"`
	LoadInfo     *NodeLoadInfo          `json:"load_info,omitempty"`
	LastSeen     time.Time              `json:"last_seen"`
	JoinedAt     time.Time              `json:"joined_at"`
	Version      string                 `json:"version"`
}

// NodeLoadInfo represents load information for a node
type NodeLoadInfo struct {
	CPUUsage    float64   `json:"cpu_usage"`
	MemoryUsage float64   `json:"memory_usage"`
	ActiveJobs  int       `json:"active_jobs"`
	QueuedJobs  int       `json:"queued_jobs"`
	JobCapacity int       `json:"job_capacity"`
	LoadScore   float64   `json:"load_score"`
	LastUpdated time.Time `json:"last_updated"`
}

// NodeStatus represents the status of a node
type NodeStatus string

const (
	NodeStatusActive    NodeStatus = "active"
	NodeStatusInactive  NodeStatus = "inactive"
	NodeStatusSuspected NodeStatus = "suspected"
	NodeStatusFailed    NodeStatus = "failed"
	NodeStatusLeaving   NodeStatus = "leaving"
	NodeStatusJoining   NodeStatus = "joining"
)

// NodeRole represents the role of a node
type NodeRole string

const (
	NodeRoleLeader    NodeRole = "leader"
	NodeRoleFollower  NodeRole = "follower"
	NodeRoleCandidate NodeRole = "candidate"
	NodeRoleObserver  NodeRole = "observer"
)

// LeadershipEvent represents a leadership change event
type LeadershipEvent struct {
	PreviousLeader string    `json:"previous_leader"`
	NewLeader      string    `json:"new_leader"`
	Term           int64     `json:"term"`
	Timestamp      time.Time `json:"timestamp"`
	Reason         string    `json:"reason"`
}

// CoordinatorCallback defines callbacks for coordinator events
type CoordinatorCallback interface {
	OnNodeJoined(node *NodeInfo)
	OnNodeLeft(node *NodeInfo)
	OnNodeFailed(node *NodeInfo)
	OnLeaderChanged(event *LeadershipEvent)
	OnClusterStateChanged(nodes map[string]*NodeInfo)
}

// Discovery defines the interface for node discovery
type Discovery interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	DiscoverNodes(ctx context.Context) ([]*NodeInfo, error)
	RegisterNode(ctx context.Context, node *NodeInfo) error
	UnregisterNode(ctx context.Context, nodeID string) error
	HealthCheck(ctx context.Context) error
}

// ClusterTransport defines the interface for cluster communication
type ClusterTransport interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	SendMessage(ctx context.Context, nodeID string, message *ClusterMessage) error
	BroadcastMessage(ctx context.Context, message *ClusterMessage) error
	SetMessageHandler(handler MessageHandler)
	GetNodeAddress(nodeID string) (string, error)
}

// ClusterMessage represents a message sent between cluster nodes
type ClusterMessage struct {
	Type      MessageType            `json:"type"`
	FromNode  string                 `json:"from_node"`
	ToNode    string                 `json:"to_node"`
	Data      interface{}            `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// MessageType represents the type of cluster message
type MessageType string

const (
	MessageTypeHeartbeat     MessageType = "heartbeat"
	MessageTypeJoin          MessageType = "join"
	MessageTypeLeave         MessageType = "leave"
	MessageTypeElection      MessageType = "election"
	MessageTypeLoadUpdate    MessageType = "load_update"
	MessageTypeJobAssignment MessageType = "job_assignment"
	MessageTypeJobStatus     MessageType = "job_status"
	MessageTypeConfig        MessageType = "config"
)

// MessageHandler handles incoming cluster messages
type MessageHandler func(ctx context.Context, message *ClusterMessage) error

// DefaultCoordinatorConfig returns default coordinator configuration
func DefaultCoordinatorConfig() *CoordinatorConfig {
	return &CoordinatorConfig{
		NodeID:              "node-1",
		ClusterID:           "cron-cluster",
		HeartbeatInterval:   5 * time.Second,
		NodeTimeout:         30 * time.Second,
		DiscoveryInterval:   10 * time.Second,
		MaxNodes:            100,
		DiscoveryType:       "static",
		DiscoveryConfig:     make(map[string]interface{}),
		TransportConfig:     make(map[string]interface{}),
		EnableHealthMonitor: true,
		EnableMetrics:       true,
	}
}

// NewNodeCoordinator creates a new node coordinator
func NewNodeCoordinator(config *CoordinatorConfig, transport ClusterTransport, discovery Discovery, logger common.Logger, metrics common.Metrics) (*NodeCoordinator, error) {
	if config == nil {
		return nil, common.ErrInvalidConfig("config", fmt.Errorf("config cannot be nil"))
	}

	if err := validateCoordinatorConfig(config); err != nil {
		return nil, err
	}

	localNode := &NodeInfo{
		ID:           config.NodeID,
		Status:       NodeStatusJoining,
		Role:         NodeRoleFollower,
		Metadata:     make(map[string]interface{}),
		Capabilities: []string{"job_execution", "leader_election"},
		LoadInfo:     &NodeLoadInfo{},
		JoinedAt:     time.Now(),
		Version:      "1.0.0",
	}

	coordinator := &NodeCoordinator{
		config:        config,
		nodeID:        config.NodeID,
		nodes:         make(map[string]*NodeInfo),
		localNode:     localNode,
		leaderHistory: make([]LeadershipEvent, 0),
		discovery:     discovery,
		transport:     transport,
		stopChannel:   make(chan struct{}),
		logger:        logger,
		metrics:       metrics,
		callbacks:     make([]CoordinatorCallback, 0),
	}

	// Create health checker
	if config.EnableHealthMonitor {
		coordinator.healthChecker = NewNodeHealthChecker(config, logger, metrics)
	}

	// Set up message handler
	transport.SetMessageHandler(coordinator.handleMessage)

	return coordinator, nil
}

// Start starts the node coordinator
func (nc *NodeCoordinator) Start(ctx context.Context) error {
	nc.nodesMu.Lock()
	defer nc.nodesMu.Unlock()

	if nc.started {
		return common.ErrLifecycleError("start", fmt.Errorf("coordinator already started"))
	}

	if nc.logger != nil {
		nc.logger.Info("starting node coordinator",
			logger.String("node_id", nc.nodeID),
			logger.String("cluster_id", nc.config.ClusterID),
		)
	}

	// OnStart transport
	if err := nc.transport.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("coordinator", err)
	}

	// OnStart discovery
	if err := nc.discovery.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("coordinator", err)
	}

	// OnStart health checker
	if nc.healthChecker != nil {
		if err := nc.healthChecker.Start(ctx); err != nil {
			return common.ErrServiceStartFailed("coordinator", err)
		}
	}

	// Register local node
	if err := nc.discovery.RegisterNode(ctx, nc.localNode); err != nil {
		return common.ErrServiceStartFailed("coordinator", err)
	}

	// Add local node to cluster
	nc.localNode.Status = NodeStatusActive
	nc.nodes[nc.nodeID] = nc.localNode

	// OnStart background tasks
	nc.wg.Add(3)
	go nc.heartbeatLoop(ctx)
	go nc.discoveryLoop(ctx)
	go nc.healthMonitorLoop(ctx)

	nc.started = true

	if nc.logger != nil {
		nc.logger.Info("node coordinator started")
	}

	if nc.metrics != nil {
		nc.metrics.Counter("forge.cron.coordinator_started").Inc()
	}

	return nil
}

// Stop stops the node coordinator
func (nc *NodeCoordinator) Stop(ctx context.Context) error {
	nc.nodesMu.Lock()
	defer nc.nodesMu.Unlock()

	if !nc.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("coordinator not started"))
	}

	if nc.logger != nil {
		nc.logger.Info("stopping node coordinator")
	}

	// Set local node status to leaving
	nc.localNode.Status = NodeStatusLeaving

	// Send leave message to cluster
	leaveMessage := &ClusterMessage{
		Type:      MessageTypeLeave,
		FromNode:  nc.nodeID,
		Data:      nc.localNode,
		Timestamp: time.Now(),
	}
	nc.transport.BroadcastMessage(ctx, leaveMessage)

	// Signal stop
	close(nc.stopChannel)

	// Wait for background tasks to finish
	nc.wg.Wait()

	// Unregister local node
	nc.discovery.UnregisterNode(ctx, nc.nodeID)

	// OnStop health checker
	if nc.healthChecker != nil {
		nc.healthChecker.Stop(ctx)
	}

	// OnStop discovery
	if err := nc.discovery.Stop(ctx); err != nil {
		if nc.logger != nil {
			nc.logger.Error("failed to stop discovery", logger.Error(err))
		}
	}

	// OnStop transport
	if err := nc.transport.Stop(ctx); err != nil {
		if nc.logger != nil {
			nc.logger.Error("failed to stop transport", logger.Error(err))
		}
	}

	nc.started = false

	if nc.logger != nil {
		nc.logger.Info("node coordinator stopped")
	}

	if nc.metrics != nil {
		nc.metrics.Counter("forge.cron.coordinator_stopped").Inc()
	}

	return nil
}

// GetNodes returns all known nodes
func (nc *NodeCoordinator) GetNodes() map[string]*NodeInfo {
	nc.nodesMu.RLock()
	defer nc.nodesMu.RUnlock()

	nodes := make(map[string]*NodeInfo)
	for id, node := range nc.nodes {
		nodes[id] = node
	}

	return nodes
}

// GetActiveNodes returns all active nodes
func (nc *NodeCoordinator) GetActiveNodes() map[string]*NodeInfo {
	nc.nodesMu.RLock()
	defer nc.nodesMu.RUnlock()

	nodes := make(map[string]*NodeInfo)
	for id, node := range nc.nodes {
		if node.Status == NodeStatusActive {
			nodes[id] = node
		}
	}

	return nodes
}

// GetNode returns information about a specific node
func (nc *NodeCoordinator) GetNode(nodeID string) (*NodeInfo, error) {
	nc.nodesMu.RLock()
	defer nc.nodesMu.RUnlock()

	node, exists := nc.nodes[nodeID]
	if !exists {
		return nil, common.ErrServiceNotFound(nodeID)
	}

	return node, nil
}

// GetLeader returns the current leader
func (nc *NodeCoordinator) GetLeader() string {
	nc.leaderMu.RLock()
	defer nc.leaderMu.RUnlock()
	return nc.currentLeader
}

// GetLeaderHistory returns the leadership history
func (nc *NodeCoordinator) GetLeaderHistory() []LeadershipEvent {
	nc.leaderMu.RLock()
	defer nc.leaderMu.RUnlock()

	history := make([]LeadershipEvent, len(nc.leaderHistory))
	copy(history, nc.leaderHistory)
	return history
}

// UpdateNodeLoad updates the load information for a node
func (nc *NodeCoordinator) UpdateNodeLoad(nodeID string, loadInfo *NodeLoadInfo) error {
	nc.nodesMu.Lock()
	defer nc.nodesMu.Unlock()

	node, exists := nc.nodes[nodeID]
	if !exists {
		return common.ErrServiceNotFound(nodeID)
	}

	node.LoadInfo = loadInfo
	node.LastSeen = time.Now()

	// Broadcast load update if it's the local node
	if nodeID == nc.nodeID {
		message := &ClusterMessage{
			Type:      MessageTypeLoadUpdate,
			FromNode:  nc.nodeID,
			Data:      loadInfo,
			Timestamp: time.Now(),
		}
		nc.transport.BroadcastMessage(context.Background(), message)
	}

	return nil
}

// UpdateLeader updates the current leader
func (nc *NodeCoordinator) UpdateLeader(newLeader string, term int64, reason string) {
	nc.leaderMu.Lock()
	defer nc.leaderMu.Unlock()

	if nc.currentLeader == newLeader {
		return
	}

	event := &LeadershipEvent{
		PreviousLeader: nc.currentLeader,
		NewLeader:      newLeader,
		Term:           term,
		Timestamp:      time.Now(),
		Reason:         reason,
	}

	nc.currentLeader = newLeader
	nc.leaderHistory = append(nc.leaderHistory, *event)

	// Keep only last 100 events
	if len(nc.leaderHistory) > 100 {
		nc.leaderHistory = nc.leaderHistory[1:]
	}

	// Update node roles
	nc.updateNodeRoles(newLeader)

	// Notify callbacks
	nc.notifyLeaderChanged(event)

	if nc.logger != nil {
		nc.logger.Info("leader changed",
			logger.String("previous_leader", event.PreviousLeader),
			logger.String("new_leader", newLeader),
			logger.String("reason", reason),
		)
	}

	if nc.metrics != nil {
		nc.metrics.Counter("forge.cron.leader_changes").Inc()
	}
}

// AddCallback adds a coordinator callback
func (nc *NodeCoordinator) AddCallback(callback CoordinatorCallback) {
	nc.callbacks = append(nc.callbacks, callback)
}

// HealthCheck performs a health check
func (nc *NodeCoordinator) HealthCheck(ctx context.Context) error {
	if !nc.started {
		return common.ErrHealthCheckFailed("coordinator", fmt.Errorf("coordinator not started"))
	}

	// Check transport health
	if _, err := nc.transport.GetNodeAddress(nc.nodeID); err != nil {
		return common.ErrHealthCheckFailed("coordinator", err)
	}

	// Check discovery health
	if err := nc.discovery.HealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed("coordinator", err)
	}

	return nil
}

// GetStats returns coordinator statistics
func (nc *NodeCoordinator) GetStats() CoordinatorStats {
	nc.nodesMu.RLock()
	defer nc.nodesMu.RUnlock()

	stats := CoordinatorStats{
		NodeID:        nc.nodeID,
		ClusterID:     nc.config.ClusterID,
		TotalNodes:    len(nc.nodes),
		ActiveNodes:   0,
		FailedNodes:   0,
		CurrentLeader: nc.GetLeader(),
		Started:       nc.started,
		LastUpdate:    time.Now(),
	}

	// Count nodes by status
	for _, node := range nc.nodes {
		switch node.Status {
		case NodeStatusActive:
			stats.ActiveNodes++
		case NodeStatusFailed:
			stats.FailedNodes++
		}
	}

	return stats
}

// heartbeatLoop sends periodic heartbeats
func (nc *NodeCoordinator) heartbeatLoop(ctx context.Context) {
	defer nc.wg.Done()

	ticker := time.NewTicker(nc.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-nc.stopChannel:
			return
		case <-ticker.C:
			nc.sendHeartbeat(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// discoveryLoop performs periodic node discovery
func (nc *NodeCoordinator) discoveryLoop(ctx context.Context) {
	defer nc.wg.Done()

	ticker := time.NewTicker(nc.config.DiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-nc.stopChannel:
			return
		case <-ticker.C:
			nc.discoverNodes(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// healthMonitorLoop monitors node health
func (nc *NodeCoordinator) healthMonitorLoop(ctx context.Context) {
	defer nc.wg.Done()

	ticker := time.NewTicker(nc.config.NodeTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-nc.stopChannel:
			return
		case <-ticker.C:
			nc.checkNodeHealth(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// sendHeartbeat sends heartbeat to all nodes
func (nc *NodeCoordinator) sendHeartbeat(ctx context.Context) {
	nc.localNode.LastSeen = time.Now()

	message := &ClusterMessage{
		Type:      MessageTypeHeartbeat,
		FromNode:  nc.nodeID,
		Data:      nc.localNode,
		Timestamp: time.Now(),
	}

	if err := nc.transport.BroadcastMessage(ctx, message); err != nil {
		if nc.logger != nil {
			nc.logger.Error("failed to send heartbeat", logger.Error(err))
		}
	}
}

// discoverNodes discovers new nodes
func (nc *NodeCoordinator) discoverNodes(ctx context.Context) {
	discoveredNodes, err := nc.discovery.DiscoverNodes(ctx)
	if err != nil {
		if nc.logger != nil {
			nc.logger.Error("failed to discover nodes", logger.Error(err))
		}
		return
	}

	for _, node := range discoveredNodes {
		nc.addOrUpdateNode(node)
	}
}

// checkNodeHealth checks the health of all nodes
func (nc *NodeCoordinator) checkNodeHealth(ctx context.Context) {
	nc.nodesMu.Lock()
	defer nc.nodesMu.Unlock()

	now := time.Now()
	for _, node := range nc.nodes {
		if node.ID == nc.nodeID {
			continue // Skip local node
		}

		if now.Sub(node.LastSeen) > nc.config.NodeTimeout {
			if node.Status == NodeStatusActive {
				node.Status = NodeStatusSuspected
				if nc.logger != nil {
					nc.logger.Warn("node suspected", logger.String("node_id", node.ID))
				}
			} else if node.Status == NodeStatusSuspected && now.Sub(node.LastSeen) > nc.config.NodeTimeout*2 {
				nc.markNodeAsFailed(node)
			}
		}
	}
}

// handleMessage handles incoming cluster messages
func (nc *NodeCoordinator) handleMessage(ctx context.Context, message *ClusterMessage) error {
	switch message.Type {
	case MessageTypeHeartbeat:
		return nc.handleHeartbeat(message)
	case MessageTypeJoin:
		return nc.handleJoin(message)
	case MessageTypeLeave:
		return nc.handleLeave(message)
	case MessageTypeLoadUpdate:
		return nc.handleLoadUpdate(message)
	default:
		if nc.logger != nil {
			nc.logger.Debug("received unknown message type",
				logger.String("type", string(message.Type)),
				logger.String("from_node", message.FromNode),
			)
		}
	}

	return nil
}

// handleHeartbeat handles heartbeat messages
func (nc *NodeCoordinator) handleHeartbeat(message *ClusterMessage) error {
	if nodeInfo, ok := message.Data.(*NodeInfo); ok {
		nc.addOrUpdateNode(nodeInfo)
	}
	return nil
}

// handleJoin handles join messages
func (nc *NodeCoordinator) handleJoin(message *ClusterMessage) error {
	if nodeInfo, ok := message.Data.(*NodeInfo); ok {
		nc.addOrUpdateNode(nodeInfo)
		nc.notifyNodeJoined(nodeInfo)
	}
	return nil
}

// handleLeave handles leave messages
func (nc *NodeCoordinator) handleLeave(message *ClusterMessage) error {
	nc.nodesMu.Lock()
	defer nc.nodesMu.Unlock()

	if node, exists := nc.nodes[message.FromNode]; exists {
		node.Status = NodeStatusLeaving
		delete(nc.nodes, message.FromNode)
		nc.notifyNodeLeft(node)
	}

	return nil
}

// handleLoadUpdate handles load update messages
func (nc *NodeCoordinator) handleLoadUpdate(message *ClusterMessage) error {
	if loadInfo, ok := message.Data.(*NodeLoadInfo); ok {
		nc.UpdateNodeLoad(message.FromNode, loadInfo)
	}
	return nil
}

// addOrUpdateNode adds or updates a node
func (nc *NodeCoordinator) addOrUpdateNode(node *NodeInfo) {
	nc.nodesMu.Lock()
	defer nc.nodesMu.Unlock()

	existing, exists := nc.nodes[node.ID]
	if exists {
		// Update existing node
		existing.Address = node.Address
		existing.Port = node.Port
		existing.Status = node.Status
		existing.Role = node.Role
		existing.Metadata = node.Metadata
		existing.LoadInfo = node.LoadInfo
		existing.LastSeen = time.Now()
	} else {
		// Add new node
		node.LastSeen = time.Now()
		nc.nodes[node.ID] = node
		nc.notifyNodeJoined(node)
	}
}

// markNodeAsFailed marks a node as failed
func (nc *NodeCoordinator) markNodeAsFailed(node *NodeInfo) {
	node.Status = NodeStatusFailed
	nc.notifyNodeFailed(node)

	if nc.logger != nil {
		nc.logger.Error("node failed", logger.String("node_id", node.ID))
	}

	if nc.metrics != nil {
		nc.metrics.Counter("forge.cron.node_failures").Inc()
	}
}

// updateNodeRoles updates node roles based on leadership
func (nc *NodeCoordinator) updateNodeRoles(leaderID string) {
	nc.nodesMu.Lock()
	defer nc.nodesMu.Unlock()

	for _, node := range nc.nodes {
		if node.ID == leaderID {
			node.Role = NodeRoleLeader
		} else {
			node.Role = NodeRoleFollower
		}
	}
}

// notifyNodeJoined notifies callbacks about node join
func (nc *NodeCoordinator) notifyNodeJoined(node *NodeInfo) {
	for _, callback := range nc.callbacks {
		go callback.OnNodeJoined(node)
	}
}

// notifyNodeLeft notifies callbacks about node leave
func (nc *NodeCoordinator) notifyNodeLeft(node *NodeInfo) {
	for _, callback := range nc.callbacks {
		go callback.OnNodeLeft(node)
	}
}

// notifyNodeFailed notifies callbacks about node failure
func (nc *NodeCoordinator) notifyNodeFailed(node *NodeInfo) {
	for _, callback := range nc.callbacks {
		go callback.OnNodeFailed(node)
	}
}

// notifyLeaderChanged notifies callbacks about leadership change
func (nc *NodeCoordinator) notifyLeaderChanged(event *LeadershipEvent) {
	for _, callback := range nc.callbacks {
		go callback.OnLeaderChanged(event)
	}
}

// notifyClusterStateChanged notifies callbacks about cluster state change
func (nc *NodeCoordinator) notifyClusterStateChanged() {
	nodes := nc.GetNodes()
	for _, callback := range nc.callbacks {
		go callback.OnClusterStateChanged(nodes)
	}
}

// CoordinatorStats contains coordinator statistics
type CoordinatorStats struct {
	NodeID        string    `json:"node_id"`
	ClusterID     string    `json:"cluster_id"`
	TotalNodes    int       `json:"total_nodes"`
	ActiveNodes   int       `json:"active_nodes"`
	FailedNodes   int       `json:"failed_nodes"`
	CurrentLeader string    `json:"current_leader"`
	Started       bool      `json:"started"`
	LastUpdate    time.Time `json:"last_update"`
}

// validateCoordinatorConfig validates coordinator configuration
func validateCoordinatorConfig(config *CoordinatorConfig) error {
	if config.NodeID == "" {
		return common.ErrValidationError("node_id", fmt.Errorf("node ID cannot be empty"))
	}

	if config.ClusterID == "" {
		return common.ErrValidationError("cluster_id", fmt.Errorf("cluster ID cannot be empty"))
	}

	if config.HeartbeatInterval <= 0 {
		return common.ErrValidationError("heartbeat_interval", fmt.Errorf("heartbeat interval must be positive"))
	}

	if config.NodeTimeout <= 0 {
		return common.ErrValidationError("node_timeout", fmt.Errorf("node timeout must be positive"))
	}

	if config.DiscoveryInterval <= 0 {
		return common.ErrValidationError("discovery_interval", fmt.Errorf("discovery interval must be positive"))
	}

	if config.MaxNodes <= 0 {
		return common.ErrValidationError("max_nodes", fmt.Errorf("max nodes must be positive"))
	}

	if config.HeartbeatInterval >= config.NodeTimeout {
		return common.ErrValidationError("heartbeat_interval", fmt.Errorf("heartbeat interval must be less than node timeout"))
	}

	return nil
}

// NodeHealthChecker monitors node health
type NodeHealthChecker struct {
	config  *CoordinatorConfig
	logger  common.Logger
	metrics common.Metrics
}

// NewNodeHealthChecker creates a new node health checker
func NewNodeHealthChecker(config *CoordinatorConfig, logger common.Logger, metrics common.Metrics) *NodeHealthChecker {
	return &NodeHealthChecker{
		config:  config,
		logger:  logger,
		metrics: metrics,
	}
}

// Start starts the health checker
func (nhc *NodeHealthChecker) Start(ctx context.Context) error {
	if nhc.logger != nil {
		nhc.logger.Info("node health checker started")
	}
	return nil
}

// Stop stops the health checker
func (nhc *NodeHealthChecker) Stop(ctx context.Context) error {
	if nhc.logger != nil {
		nhc.logger.Info("node health checker stopped")
	}
	return nil
}
