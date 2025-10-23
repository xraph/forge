package distribution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// NodeManager manages cluster nodes for job distribution
type NodeManager struct {
	config *NodeManagerConfig
	nodeID string

	// Node registry
	nodes          map[string]*Node
	nodeHeartbeats map[string]time.Time
	nodeLoads      map[string]*NodeLoad
	mu             sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// NodeManagerConfig contains configuration for node manager
type NodeManagerConfig struct {
	NodeID                string        `json:"node_id" yaml:"node_id"`
	ClusterID             string        `json:"cluster_id" yaml:"cluster_id"`
	HeartbeatInterval     time.Duration `json:"heartbeat_interval" yaml:"heartbeat_interval"`
	NodeTimeout           time.Duration `json:"node_timeout" yaml:"node_timeout"`
	HealthCheckInterval   time.Duration `json:"health_check_interval" yaml:"health_check_interval"`
	MetricsUpdateInterval time.Duration `json:"metrics_update_interval" yaml:"metrics_update_interval"`
	MaxNodes              int           `json:"max_nodes" yaml:"max_nodes"`
	NodeDiscoveryEnabled  bool          `json:"node_discovery_enabled" yaml:"node_discovery_enabled"`
	LoadUpdateInterval    time.Duration `json:"load_update_interval" yaml:"load_update_interval"`
	NodeTags              []string      `json:"node_tags" yaml:"node_tags"`
	NodeCapabilities      []string      `json:"node_capabilities" yaml:"node_capabilities"`
	NodeWeight            float64       `json:"node_weight" yaml:"node_weight"`
	MaxConcurrentJobs     int           `json:"max_concurrent_jobs" yaml:"max_concurrent_jobs"`
}

// Node represents a cluster node
type Node struct {
	ID           string                 `json:"id"`
	Address      string                 `json:"address"`
	Port         int                    `json:"port"`
	Status       NodeStatus             `json:"status"`
	Capabilities []string               `json:"capabilities"`
	Tags         map[string]string      `json:"tags"`
	Weight       float64                `json:"weight"`
	Capacity     int                    `json:"capacity"`
	Version      string                 `json:"version"`
	Metadata     map[string]interface{} `json:"metadata"`
	JoinedAt     time.Time              `json:"joined_at"`
	LastSeen     time.Time              `json:"last_seen"`
	StartTime    time.Time              `json:"start_time"`
	Uptime       time.Duration          `json:"uptime"`
}

// NodeStatus represents the status of a node
type NodeStatus string

const (
	NodeStatusActive      NodeStatus = "active"
	NodeStatusInactive    NodeStatus = "inactive"
	NodeStatusSuspended   NodeStatus = "suspended"
	NodeStatusMaintenance NodeStatus = "maintenance"
	NodeStatusUnknown     NodeStatus = "unknown"
)

// NodeEvent represents a node lifecycle event
type NodeEvent struct {
	Type      NodeEventType          `json:"type"`
	NodeID    string                 `json:"node_id"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details"`
}

// NodeEventType represents the type of node event
type NodeEventType string

const (
	NodeEventTypeJoined    NodeEventType = "node.joined"
	NodeEventTypeLeft      NodeEventType = "node.left"
	NodeEventTypeUpdated   NodeEventType = "node.updated"
	NodeEventTypeFailed    NodeEventType = "node.failed"
	NodeEventTypeRecovered NodeEventType = "node.recovered"
)

// NodeStats contains statistics for a node
type NodeStats struct {
	NodeID          string        `json:"node_id"`
	Status          NodeStatus    `json:"status"`
	Uptime          time.Duration `json:"uptime"`
	JobsAssigned    int           `json:"jobs_assigned"`
	JobsCompleted   int           `json:"jobs_completed"`
	JobsFailed      int           `json:"jobs_failed"`
	JobsRunning     int           `json:"jobs_running"`
	JobsQueued      int           `json:"jobs_queued"`
	SuccessRate     float64       `json:"success_rate"`
	AverageJobTime  time.Duration `json:"average_job_time"`
	CPUUsage        float64       `json:"cpu_usage"`
	MemoryUsage     float64       `json:"memory_usage"`
	NetworkUsage    float64       `json:"network_usage"`
	DiskUsage       float64       `json:"disk_usage"`
	LoadAverage     float64       `json:"load_average"`
	LastHeartbeat   time.Time     `json:"last_heartbeat"`
	LastHealthCheck time.Time     `json:"last_health_check"`
	HealthStatus    string        `json:"health_status"`
	ErrorCount      int           `json:"error_count"`
	WarningCount    int           `json:"warning_count"`
	LastError       string        `json:"last_error,omitempty"`
	NodeVersion     string        `json:"node_version"`
	RuntimeVersion  string        `json:"runtime_version"`
	ConfigVersion   string        `json:"config_version"`
}

// NodeManagerStats contains statistics for the node manager
type NodeManagerStats struct {
	TotalNodes       int                   `json:"total_nodes"`
	ActiveNodes      int                   `json:"active_nodes"`
	InactiveNodes    int                   `json:"inactive_nodes"`
	SuspendedNodes   int                   `json:"suspended_nodes"`
	UnknownNodes     int                   `json:"unknown_nodes"`
	NodeStats        map[string]*NodeStats `json:"node_stats"`
	ClusterHealth    string                `json:"cluster_health"`
	LastUpdate       time.Time             `json:"last_update"`
	HeartbeatCount   int64                 `json:"heartbeat_count"`
	FailedHeartbeats int64                 `json:"failed_heartbeats"`
	NodeEvents       []*NodeEvent          `json:"recent_events"`
}

// DefaultNodeManagerConfig returns default node manager configuration
func DefaultNodeManagerConfig() *NodeManagerConfig {
	return &NodeManagerConfig{
		NodeID:                "node-1",
		ClusterID:             "cron-cluster",
		HeartbeatInterval:     30 * time.Second,
		NodeTimeout:           90 * time.Second,
		HealthCheckInterval:   60 * time.Second,
		MetricsUpdateInterval: 15 * time.Second,
		MaxNodes:              100,
		NodeDiscoveryEnabled:  true,
		LoadUpdateInterval:    10 * time.Second,
		NodeTags:              []string{},
		NodeCapabilities:      []string{"job-execution"},
		NodeWeight:            1.0,
		MaxConcurrentJobs:     10,
	}
}

// NewNodeManager creates a new node manager
func NewNodeManager(config *NodeManagerConfig, logger common.Logger, metrics common.Metrics) (*NodeManager, error) {
	if config == nil {
		config = DefaultNodeManagerConfig()
	}

	if err := validateNodeManagerConfig(config); err != nil {
		return nil, err
	}

	nm := &NodeManager{
		config:         config,
		nodeID:         config.NodeID,
		nodes:          make(map[string]*Node),
		nodeHeartbeats: make(map[string]time.Time),
		nodeLoads:      make(map[string]*NodeLoad),
		stopChannel:    make(chan struct{}),
		logger:         logger,
		metrics:        metrics,
	}

	// Register self as a node
	if err := nm.registerSelf(); err != nil {
		return nil, err
	}

	return nm, nil
}

// Start starts the node manager
func (nm *NodeManager) Start(ctx context.Context) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if nm.started {
		return common.ErrLifecycleError("start", fmt.Errorf("node manager already started"))
	}

	// Start background tasks
	nm.wg.Add(4)
	go nm.heartbeatLoop(ctx)
	go nm.healthCheckLoop(ctx)
	go nm.metricsUpdateLoop(ctx)
	go nm.nodeDiscoveryLoop(ctx)

	nm.started = true

	if nm.logger != nil {
		nm.logger.Info("node manager started",
			logger.String("node_id", nm.nodeID),
			logger.String("cluster_id", nm.config.ClusterID),
		)
	}

	if nm.metrics != nil {
		nm.metrics.Counter("forge.cron.node_manager_started").Inc()
	}

	return nil
}

// Stop stops the node manager
func (nm *NodeManager) Stop(ctx context.Context) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if !nm.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("node manager not started"))
	}

	if nm.logger != nil {
		nm.logger.Info("stopping node manager")
	}

	// Signal stop
	close(nm.stopChannel)

	// Wait for background tasks to finish
	nm.wg.Wait()

	nm.started = false

	if nm.logger != nil {
		nm.logger.Info("node manager stopped")
	}

	if nm.metrics != nil {
		nm.metrics.Counter("forge.cron.node_manager_stopped").Inc()
	}

	return nil
}

// RegisterNode registers a new node
func (nm *NodeManager) RegisterNode(node *Node) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if _, exists := nm.nodes[node.ID]; exists {
		return common.ErrServiceAlreadyExists(node.ID)
	}

	// Set initial values
	node.JoinedAt = time.Now()
	node.LastSeen = time.Now()
	node.Status = NodeStatusActive

	// Register node
	nm.nodes[node.ID] = node
	nm.nodeHeartbeats[node.ID] = time.Now()

	// Initialize load
	nm.nodeLoads[node.ID] = &NodeLoad{
		NodeID:             node.ID,
		MaxConcurrentJobs:  nm.config.MaxConcurrentJobs,
		DistributionWeight: node.Weight,
		HealthStatus:       "healthy",
		LastUpdateTime:     time.Now(),
	}

	if nm.logger != nil {
		nm.logger.Info("node registered",
			logger.String("node_id", node.ID),
			logger.String("address", node.Address),
			logger.Int("port", node.Port),
		)
	}

	if nm.metrics != nil {
		nm.metrics.Counter("forge.cron.nodes_registered").Inc()
		nm.metrics.Gauge("forge.cron.total_nodes").Set(float64(len(nm.nodes)))
	}

	// Emit event
	nm.emitNodeEvent(NodeEventTypeJoined, node.ID, map[string]interface{}{
		"address": node.Address,
		"port":    node.Port,
	})

	return nil
}

// UnregisterNode unregisters a node
func (nm *NodeManager) UnregisterNode(nodeID string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	node, exists := nm.nodes[nodeID]
	if !exists {
		return common.ErrServiceNotFound(nodeID)
	}

	// Remove node
	delete(nm.nodes, nodeID)
	delete(nm.nodeHeartbeats, nodeID)
	delete(nm.nodeLoads, nodeID)

	if nm.logger != nil {
		nm.logger.Info("node unregistered",
			logger.String("node_id", nodeID),
		)
	}

	if nm.metrics != nil {
		nm.metrics.Counter("forge.cron.nodes_unregistered").Inc()
		nm.metrics.Gauge("forge.cron.total_nodes").Set(float64(len(nm.nodes)))
	}

	// Emit event
	nm.emitNodeEvent(NodeEventTypeLeft, nodeID, map[string]interface{}{
		"address": node.Address,
		"port":    node.Port,
	})

	return nil
}

// GetNode returns a node by ID
func (nm *NodeManager) GetNode(nodeID string) (*Node, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	node, exists := nm.nodes[nodeID]
	if !exists {
		return nil, common.ErrServiceNotFound(nodeID)
	}

	return node, nil
}

// GetAllNodes returns all nodes
func (nm *NodeManager) GetAllNodes() []*Node {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	nodes := make([]*Node, 0, len(nm.nodes))
	for _, node := range nm.nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// GetAvailableNodes returns available nodes for job assignment
func (nm *NodeManager) GetAvailableNodes() []*Node {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var availableNodes []*Node
	for _, node := range nm.nodes {
		if node.Status == NodeStatusActive {
			availableNodes = append(availableNodes, node)
		}
	}

	return availableNodes
}

// GetNodesByCapability returns nodes with specific capability
func (nm *NodeManager) GetNodesByCapability(capability string) []*Node {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var matchingNodes []*Node
	for _, node := range nm.nodes {
		if node.Status == NodeStatusActive && nm.hasCapability(node, capability) {
			matchingNodes = append(matchingNodes, node)
		}
	}

	return matchingNodes
}

// GetNodesByTag returns nodes with specific tag
func (nm *NodeManager) GetNodesByTag(key, value string) []*Node {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var matchingNodes []*Node
	for _, node := range nm.nodes {
		if node.Status == NodeStatusActive && node.Tags[key] == value {
			matchingNodes = append(matchingNodes, node)
		}
	}

	return matchingNodes
}

// UpdateNodeHeartbeat updates the heartbeat timestamp for a node
func (nm *NodeManager) UpdateNodeHeartbeat(nodeID string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	node, exists := nm.nodes[nodeID]
	if !exists {
		return common.ErrServiceNotFound(nodeID)
	}

	now := time.Now()
	nm.nodeHeartbeats[nodeID] = now
	node.LastSeen = now

	// Update uptime
	node.Uptime = now.Sub(node.StartTime)

	if nm.metrics != nil {
		nm.metrics.Counter("forge.cron.node_heartbeats").Inc()
	}

	return nil
}

// UpdateNodeLoad updates the load information for a node
func (nm *NodeManager) UpdateNodeLoad(nodeID string, load *NodeLoad) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if _, exists := nm.nodes[nodeID]; !exists {
		return common.ErrServiceNotFound(nodeID)
	}

	load.NodeID = nodeID
	load.LastUpdateTime = time.Now()
	nm.nodeLoads[nodeID] = load

	if nm.metrics != nil {
		nm.metrics.Gauge("forge.cron.node_running_jobs").Set(float64(load.RunningJobs))
		nm.metrics.Gauge("forge.cron.node_queued_jobs").Set(float64(load.QueuedJobs))
		nm.metrics.Gauge("forge.cron.node_cpu_usage").Set(load.CPUUsage)
		nm.metrics.Gauge("forge.cron.node_memory_usage").Set(load.MemoryUsage)
	}

	return nil
}

// GetNodeLoad returns the load information for a node
func (nm *NodeManager) GetNodeLoad(nodeID string) *NodeLoad {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	return nm.nodeLoads[nodeID]
}

// IsNodeHealthy checks if a node is healthy
func (nm *NodeManager) IsNodeHealthy(nodeID string) bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	node, exists := nm.nodes[nodeID]
	if !exists {
		return false
	}

	// Check node status
	if node.Status != NodeStatusActive {
		return false
	}

	// Check heartbeat
	lastHeartbeat, exists := nm.nodeHeartbeats[nodeID]
	if !exists {
		return false
	}

	// Check if heartbeat is recent
	return time.Since(lastHeartbeat) <= nm.config.NodeTimeout
}

// GetNodeStats returns statistics for a node
func (nm *NodeManager) GetNodeStats(nodeID string) (*NodeStats, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	node, exists := nm.nodes[nodeID]
	if !exists {
		return nil, common.ErrServiceNotFound(nodeID)
	}

	load := nm.nodeLoads[nodeID]
	lastHeartbeat := nm.nodeHeartbeats[nodeID]

	stats := &NodeStats{
		NodeID:          nodeID,
		Status:          node.Status,
		Uptime:          node.Uptime,
		LastHeartbeat:   lastHeartbeat,
		LastHealthCheck: time.Now(),
		NodeVersion:     node.Version,
	}

	if load != nil {
		stats.JobsRunning = load.RunningJobs
		stats.JobsQueued = load.QueuedJobs
		stats.JobsCompleted = load.SuccessfulJobs
		stats.JobsFailed = load.FailedJobs
		stats.CPUUsage = load.CPUUsage
		stats.MemoryUsage = load.MemoryUsage
		stats.NetworkUsage = load.NetworkUsage
		stats.HealthStatus = load.HealthStatus

		if load.TotalJobs > 0 {
			stats.SuccessRate = float64(load.SuccessfulJobs) / float64(load.TotalJobs)
		}
	}

	return stats, nil
}

// GetNodeManagerStats returns statistics for the node manager
func (nm *NodeManager) GetNodeManagerStats() *NodeManagerStats {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	stats := &NodeManagerStats{
		TotalNodes: len(nm.nodes),
		NodeStats:  make(map[string]*NodeStats),
		LastUpdate: time.Now(),
		NodeEvents: make([]*NodeEvent, 0),
	}

	// Count nodes by status
	for _, node := range nm.nodes {
		switch node.Status {
		case NodeStatusActive:
			stats.ActiveNodes++
		case NodeStatusInactive:
			stats.InactiveNodes++
		case NodeStatusSuspended:
			stats.SuspendedNodes++
		default:
			stats.UnknownNodes++
		}

		// Get node stats
		if nodeStats, err := nm.GetNodeStats(node.ID); err == nil {
			stats.NodeStats[node.ID] = nodeStats
		}
	}

	// Determine cluster health
	if stats.ActiveNodes > 0 {
		healthyRatio := float64(stats.ActiveNodes) / float64(stats.TotalNodes)
		if healthyRatio >= 0.8 {
			stats.ClusterHealth = "healthy"
		} else if healthyRatio >= 0.5 {
			stats.ClusterHealth = "degraded"
		} else {
			stats.ClusterHealth = "unhealthy"
		}
	} else {
		stats.ClusterHealth = "critical"
	}

	return stats
}

// registerSelf registers the current node
func (nm *NodeManager) registerSelf() error {
	selfNode := &Node{
		ID:           nm.nodeID,
		Address:      "localhost", // This should be configurable
		Port:         8080,        // This should be configurable
		Status:       NodeStatusActive,
		Capabilities: nm.config.NodeCapabilities,
		Tags:         make(map[string]string),
		Weight:       nm.config.NodeWeight,
		Capacity:     nm.config.MaxConcurrentJobs,
		Version:      "1.0.0", // This should be from build info
		Metadata:     make(map[string]interface{}),
		StartTime:    time.Now(),
	}

	// Add node tags
	for _, tag := range nm.config.NodeTags {
		selfNode.Tags[tag] = "true"
	}

	return nm.RegisterNode(selfNode)
}

// heartbeatLoop sends periodic heartbeats
func (nm *NodeManager) heartbeatLoop(ctx context.Context) {
	defer nm.wg.Done()

	ticker := time.NewTicker(nm.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-nm.stopChannel:
			return
		case <-ticker.C:
			nm.sendHeartbeat()
		case <-ctx.Done():
			return
		}
	}
}

// healthCheckLoop performs periodic health checks
func (nm *NodeManager) healthCheckLoop(ctx context.Context) {
	defer nm.wg.Done()

	ticker := time.NewTicker(nm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-nm.stopChannel:
			return
		case <-ticker.C:
			nm.performHealthChecks()
		case <-ctx.Done():
			return
		}
	}
}

// metricsUpdateLoop updates metrics periodically
func (nm *NodeManager) metricsUpdateLoop(ctx context.Context) {
	defer nm.wg.Done()

	ticker := time.NewTicker(nm.config.MetricsUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-nm.stopChannel:
			return
		case <-ticker.C:
			nm.updateMetrics()
		case <-ctx.Done():
			return
		}
	}
}

// nodeDiscoveryLoop discovers new nodes
func (nm *NodeManager) nodeDiscoveryLoop(ctx context.Context) {
	defer nm.wg.Done()

	if !nm.config.NodeDiscoveryEnabled {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-nm.stopChannel:
			return
		case <-ticker.C:
			nm.discoverNodes()
		case <-ctx.Done():
			return
		}
	}
}

// sendHeartbeat sends a heartbeat for the current node
func (nm *NodeManager) sendHeartbeat() {
	if err := nm.UpdateNodeHeartbeat(nm.nodeID); err != nil {
		if nm.logger != nil {
			nm.logger.Error("failed to send heartbeat", logger.Error(err))
		}
	}
}

// performHealthChecks performs health checks on all nodes
func (nm *NodeManager) performHealthChecks() {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	now := time.Now()
	timeout := nm.config.NodeTimeout

	for nodeID, node := range nm.nodes {
		lastHeartbeat, exists := nm.nodeHeartbeats[nodeID]
		if !exists || now.Sub(lastHeartbeat) > timeout {
			// Node is unhealthy
			if node.Status == NodeStatusActive {
				node.Status = NodeStatusInactive
				nm.emitNodeEvent(NodeEventTypeFailed, nodeID, map[string]interface{}{
					"reason":         "heartbeat timeout",
					"last_heartbeat": lastHeartbeat,
				})

				if nm.logger != nil {
					nm.logger.Warn("node marked as inactive due to heartbeat timeout",
						logger.String("node_id", nodeID),
						logger.Time("last_heartbeat", lastHeartbeat),
					)
				}
			}
		} else if node.Status == NodeStatusInactive {
			// Node has recovered
			node.Status = NodeStatusActive
			nm.emitNodeEvent(NodeEventTypeRecovered, nodeID, map[string]interface{}{
				"reason": "heartbeat resumed",
			})

			if nm.logger != nil {
				nm.logger.Info("node recovered",
					logger.String("node_id", nodeID),
				)
			}
		}
	}
}

// updateMetrics updates node manager metrics
func (nm *NodeManager) updateMetrics() {
	if nm.metrics == nil {
		return
	}

	nm.mu.RLock()
	defer nm.mu.RUnlock()

	// Update node counts
	activeNodes := 0
	inactiveNodes := 0
	totalJobs := 0

	for _, node := range nm.nodes {
		if node.Status == NodeStatusActive {
			activeNodes++
		} else {
			inactiveNodes++
		}
	}

	for _, load := range nm.nodeLoads {
		totalJobs += load.RunningJobs + load.QueuedJobs
	}

	nm.metrics.Gauge("forge.cron.active_nodes").Set(float64(activeNodes))
	nm.metrics.Gauge("forge.cron.inactive_nodes").Set(float64(inactiveNodes))
	nm.metrics.Gauge("forge.cron.total_cluster_jobs").Set(float64(totalJobs))
}

// discoverNodes discovers new nodes in the cluster
func (nm *NodeManager) discoverNodes() {
	// This is a placeholder for node discovery logic
	// In a real implementation, this would use service discovery
	// mechanisms like Consul, etcd, Kubernetes API, etc.
	if nm.logger != nil {
		nm.logger.Debug("performing node discovery")
	}
}

// hasCapability checks if a node has a specific capability
func (nm *NodeManager) hasCapability(node *Node, capability string) bool {
	for _, cap := range node.Capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

// emitNodeEvent emits a node event
func (nm *NodeManager) emitNodeEvent(eventType NodeEventType, nodeID string, details map[string]interface{}) {
	event := &NodeEvent{
		Type:      eventType,
		NodeID:    nodeID,
		Timestamp: time.Now(),
		Details:   details,
	}

	if nm.logger != nil {
		nm.logger.Info("node event",
			logger.String("event_type", string(event.Type)),
			logger.String("node_id", nodeID),
			logger.Any("details", details),
		)
	}

	// TODO: Publish event to event bus if available
}

// validateNodeManagerConfig validates node manager configuration
func validateNodeManagerConfig(config *NodeManagerConfig) error {
	if config.NodeID == "" {
		return common.ErrValidationError("node_id", fmt.Errorf("node ID is required"))
	}

	if config.ClusterID == "" {
		return common.ErrValidationError("cluster_id", fmt.Errorf("cluster ID is required"))
	}

	if config.HeartbeatInterval <= 0 {
		return common.ErrValidationError("heartbeat_interval", fmt.Errorf("heartbeat interval must be positive"))
	}

	if config.NodeTimeout <= 0 {
		return common.ErrValidationError("node_timeout", fmt.Errorf("node timeout must be positive"))
	}

	if config.NodeTimeout <= config.HeartbeatInterval {
		return common.ErrValidationError("node_timeout", fmt.Errorf("node timeout must be greater than heartbeat interval"))
	}

	if config.HealthCheckInterval <= 0 {
		return common.ErrValidationError("health_check_interval", fmt.Errorf("health check interval must be positive"))
	}

	if config.MetricsUpdateInterval <= 0 {
		return common.ErrValidationError("metrics_update_interval", fmt.Errorf("metrics update interval must be positive"))
	}

	if config.MaxNodes <= 0 {
		return common.ErrValidationError("max_nodes", fmt.Errorf("max nodes must be positive"))
	}

	if config.NodeWeight <= 0 {
		return common.ErrValidationError("node_weight", fmt.Errorf("node weight must be positive"))
	}

	if config.MaxConcurrentJobs <= 0 {
		return common.ErrValidationError("max_concurrent_jobs", fmt.Errorf("max concurrent jobs must be positive"))
	}

	return nil
}
