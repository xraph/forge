package distribution

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	cron "github.com/xraph/forge/v0/pkg/cron/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// JobDistributor manages job distribution across cluster nodes
type JobDistributor struct {
	// Configuration
	config *Config
	nodeID string

	// Node management
	nodeManager  *NodeManager
	loadBalancer *LoadBalancer
	rebalancer   *Rebalancer

	// Distribution state
	assignments     map[string]string // job_id -> node_id
	nodeLoads       map[string]*NodeLoad
	distributionLog []*DistributionEvent
	mu              sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// Config contains configuration for job distribution
type Config struct {
	NodeID                string               `json:"node_id" yaml:"node_id"`
	ClusterID             string               `json:"cluster_id" yaml:"cluster_id"`
	RebalanceInterval     time.Duration        `json:"rebalance_interval" yaml:"rebalance_interval"`
	LoadThreshold         float64              `json:"load_threshold" yaml:"load_threshold"`
	MaxJobsPerNode        int                  `json:"max_jobs_per_node" yaml:"max_jobs_per_node"`
	DistributionStrategy  DistributionStrategy `json:"distribution_strategy" yaml:"distribution_strategy"`
	FailoverEnabled       bool                 `json:"failover_enabled" yaml:"failover_enabled"`
	FailoverTimeout       time.Duration        `json:"failover_timeout" yaml:"failover_timeout"`
	LoadBalancerConfig    *LoadBalancerConfig  `json:"load_balancer" yaml:"load_balancer"`
	RebalancerConfig      *RebalancerConfig    `json:"rebalancer" yaml:"rebalancer"`
	NodeManagerConfig     *NodeManagerConfig   `json:"node_manager" yaml:"node_manager"`
	HealthCheckInterval   time.Duration        `json:"health_check_interval" yaml:"health_check_interval"`
	MetricsUpdateInterval time.Duration        `json:"metrics_update_interval" yaml:"metrics_update_interval"`
}

// DistributionStrategy defines how jobs are distributed
type DistributionStrategy string

const (
	RoundRobin       DistributionStrategy = "round_robin"
	LeastConnections DistributionStrategy = "least_connections"
	WeightedRandom   DistributionStrategy = "weighted_random"
	ConsistentHash   DistributionStrategy = "consistent_hash"
	LoadAware        DistributionStrategy = "load_aware"
)

// NodeLoad represents the load on a node
type NodeLoad struct {
	NodeID             string    `json:"node_id"`
	RunningJobs        int       `json:"running_jobs"`
	QueuedJobs         int       `json:"queued_jobs"`
	CPUUsage           float64   `json:"cpu_usage"`
	MemoryUsage        float64   `json:"memory_usage"`
	NetworkUsage       float64   `json:"network_usage"`
	TotalJobs          int       `json:"total_jobs"`
	SuccessfulJobs     int       `json:"successful_jobs"`
	FailedJobs         int       `json:"failed_jobs"`
	LastUpdateTime     time.Time `json:"last_update_time"`
	HealthStatus       string    `json:"health_status"`
	Capabilities       []string  `json:"capabilities"`
	MaxConcurrentJobs  int       `json:"max_concurrent_jobs"`
	LoadScore          float64   `json:"load_score"`
	DistributionWeight float64   `json:"distribution_weight"`
}

// DistributionEvent represents a job distribution event
type DistributionEvent struct {
	ID        string                `json:"id"`
	JobID     string                `json:"job_id"`
	FromNode  string                `json:"from_node"`
	ToNode    string                `json:"to_node"`
	EventType DistributionEventType `json:"event_type"`
	Reason    string                `json:"reason"`
	Timestamp time.Time             `json:"timestamp"`
	Success   bool                  `json:"success"`
	Error     string                `json:"error,omitempty"`
}

// DistributionEventType represents the type of distribution event
type DistributionEventType string

const (
	EventTypeAssignment   DistributionEventType = "assignment"
	EventTypeRebalance    DistributionEventType = "rebalance"
	EventTypeFailover     DistributionEventType = "failover"
	EventTypeReassignment DistributionEventType = "reassignment"
)

// DistributionStats contains statistics about job distribution
type DistributionStats struct {
	TotalJobs         int                          `json:"total_jobs"`
	ActiveNodes       int                          `json:"active_nodes"`
	Assignments       map[string]string            `json:"assignments"`
	NodeLoads         map[string]*NodeLoad         `json:"node_loads"`
	DistributionStats map[string]*NodeDistribution `json:"distribution_stats"`
	LastRebalance     time.Time                    `json:"last_rebalance"`
	RebalanceCount    int                          `json:"rebalance_count"`
	FailoverCount     int                          `json:"failover_count"`
	Strategy          DistributionStrategy         `json:"strategy"`
}

// NodeDistribution contains distribution statistics for a node
type NodeDistribution struct {
	NodeID         string    `json:"node_id"`
	AssignedJobs   int       `json:"assigned_jobs"`
	CompletedJobs  int       `json:"completed_jobs"`
	FailedJobs     int       `json:"failed_jobs"`
	LoadPercentage float64   `json:"load_percentage"`
	LastAssignment time.Time `json:"last_assignment"`
}

// DefaultConfig returns default distribution configuration
func DefaultConfig() *Config {
	return &Config{
		NodeID:                "node-1",
		ClusterID:             "cron-cluster",
		RebalanceInterval:     5 * time.Minute,
		LoadThreshold:         0.8,
		MaxJobsPerNode:        100,
		DistributionStrategy:  LoadAware,
		FailoverEnabled:       true,
		FailoverTimeout:       30 * time.Second,
		LoadBalancerConfig:    DefaultLoadBalancerConfig(),
		RebalancerConfig:      DefaultRebalancerConfig(),
		NodeManagerConfig:     DefaultNodeManagerConfig(),
		HealthCheckInterval:   30 * time.Second,
		MetricsUpdateInterval: 10 * time.Second,
	}
}

// NewJobDistributor creates a new job distributor
func NewJobDistributor(config *Config, logger common.Logger, metrics common.Metrics) (*JobDistributor, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if err := validateConfig(config); err != nil {
		return nil, err
	}

	// Create node manager
	nodeManager, err := NewNodeManager(config.NodeManagerConfig, logger, metrics)
	if err != nil {
		return nil, common.ErrServiceStartFailed("job-distributor", err)
	}

	// Create load balancer
	loadBalancer, err := NewLoadBalancer(config.LoadBalancerConfig, logger, metrics)
	if err != nil {
		return nil, common.ErrServiceStartFailed("job-distributor", err)
	}

	// Create rebalancer
	rebalancer, err := NewRebalancer(config.RebalancerConfig, logger, metrics)
	if err != nil {
		return nil, common.ErrServiceStartFailed("job-distributor", err)
	}

	distributor := &JobDistributor{
		config:          config,
		nodeID:          config.NodeID,
		nodeManager:     nodeManager,
		loadBalancer:    loadBalancer,
		rebalancer:      rebalancer,
		assignments:     make(map[string]string),
		nodeLoads:       make(map[string]*NodeLoad),
		distributionLog: make([]*DistributionEvent, 0),
		stopChannel:     make(chan struct{}),
		logger:          logger,
		metrics:         metrics,
	}

	return distributor, nil
}

// Start starts the job distributor
func (jd *JobDistributor) Start(ctx context.Context) error {
	jd.mu.Lock()
	defer jd.mu.Unlock()

	if jd.started {
		return common.ErrLifecycleError("start", fmt.Errorf("job distributor already started"))
	}

	// Start node manager
	if err := jd.nodeManager.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("job-distributor", err)
	}

	// Start load balancer
	if err := jd.loadBalancer.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("job-distributor", err)
	}

	// Start rebalancer
	if err := jd.rebalancer.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("job-distributor", err)
	}

	// Start background tasks
	jd.wg.Add(3)
	go jd.monitorNodes(ctx)
	go jd.updateMetrics(ctx)
	go jd.periodicRebalance(ctx)

	jd.started = true

	if jd.logger != nil {
		jd.logger.Info("job distributor started",
			logger.String("node_id", jd.nodeID),
			logger.String("strategy", string(jd.config.DistributionStrategy)),
		)
	}

	if jd.metrics != nil {
		jd.metrics.Counter("forge.cron.distributor_started").Inc()
	}

	return nil
}

// Stop stops the job distributor
func (jd *JobDistributor) Stop(ctx context.Context) error {
	jd.mu.Lock()
	defer jd.mu.Unlock()

	if !jd.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("job distributor not started"))
	}

	if jd.logger != nil {
		jd.logger.Info("stopping job distributor")
	}

	// Signal stop
	close(jd.stopChannel)

	// Wait for background tasks to finish
	jd.wg.Wait()

	// Stop components
	if err := jd.rebalancer.Stop(ctx); err != nil {
		if jd.logger != nil {
			jd.logger.Error("failed to stop rebalancer", logger.Error(err))
		}
	}

	if err := jd.loadBalancer.Stop(ctx); err != nil {
		if jd.logger != nil {
			jd.logger.Error("failed to stop load balancer", logger.Error(err))
		}
	}

	if err := jd.nodeManager.Stop(ctx); err != nil {
		if jd.logger != nil {
			jd.logger.Error("failed to stop node manager", logger.Error(err))
		}
	}

	jd.started = false

	if jd.logger != nil {
		jd.logger.Info("job distributor stopped")
	}

	if jd.metrics != nil {
		jd.metrics.Counter("forge.cron.distributor_stopped").Inc()
	}

	return nil
}

// SelectNode selects a node for job execution
func (jd *JobDistributor) SelectNode(ctx context.Context, job *cron.Job) (string, error) {
	jd.mu.RLock()
	defer jd.mu.RUnlock()

	if !jd.started {
		return "", common.ErrLifecycleError("select_node", fmt.Errorf("job distributor not started"))
	}

	// Check if job is already assigned
	if assignedNode, exists := jd.assignments[job.Definition.ID]; exists {
		// Verify node is still healthy
		if jd.isNodeHealthy(assignedNode) {
			return assignedNode, nil
		}
		// Node is unhealthy, need to reassign
		delete(jd.assignments, job.Definition.ID)
	}

	// Get available nodes
	nodes := jd.nodeManager.GetAvailableNodes()
	if len(nodes) == 0 {
		return "", common.ErrServiceNotFound("available_nodes")
	}

	// Select node based on strategy
	selectedNode, err := jd.selectNodeByStrategy(job, nodes)
	if err != nil {
		return "", err
	}

	// Update assignment
	jd.assignments[job.Definition.ID] = selectedNode.ID

	// Log distribution event
	jd.logDistributionEvent(&DistributionEvent{
		ID:        generateEventID(),
		JobID:     job.Definition.ID,
		ToNode:    selectedNode.ID,
		EventType: EventTypeAssignment,
		Reason:    fmt.Sprintf("Selected by strategy: %s", jd.config.DistributionStrategy),
		Timestamp: time.Now(),
		Success:   true,
	})

	if jd.logger != nil {
		jd.logger.Info("job assigned to node",
			logger.String("job_id", job.Definition.ID),
			logger.String("node_id", selectedNode.ID),
			logger.String("strategy", string(jd.config.DistributionStrategy)),
		)
	}

	if jd.metrics != nil {
		jd.metrics.Counter("forge.cron.jobs_assigned").Inc()
	}

	return selectedNode.ID, nil
}

// ReassignJob reassigns a job to a different node
func (jd *JobDistributor) ReassignJob(ctx context.Context, jobID string, reason string) (string, error) {
	jd.mu.Lock()
	defer jd.mu.Unlock()

	oldNode, exists := jd.assignments[jobID]
	if !exists {
		return "", common.ErrServiceNotFound(jobID)
	}

	// Remove old assignment
	delete(jd.assignments, jobID)

	// Get available nodes (excluding the old node if it's unhealthy)
	nodes := jd.nodeManager.GetAvailableNodes()
	if len(nodes) == 0 {
		return "", common.ErrServiceNotFound("available_nodes")
	}

	// Find a different node
	var availableNodes []*Node
	for _, node := range nodes {
		if node.ID != oldNode {
			availableNodes = append(availableNodes, node)
		}
	}

	if len(availableNodes) == 0 {
		// No alternative nodes available, keep the old assignment
		jd.assignments[jobID] = oldNode
		return oldNode, nil
	}

	// Create dummy job for selection (we don't have the full job here)
	dummyJob := &cron.Job{
		Definition: &cron.JobDefinition{ID: jobID},
	}

	// Select new node
	selectedNode, err := jd.selectNodeByStrategy(dummyJob, availableNodes)
	if err != nil {
		// Fallback to old node
		jd.assignments[jobID] = oldNode
		return oldNode, err
	}

	// Update assignment
	jd.assignments[jobID] = selectedNode.ID

	// Log distribution event
	jd.logDistributionEvent(&DistributionEvent{
		ID:        generateEventID(),
		JobID:     jobID,
		FromNode:  oldNode,
		ToNode:    selectedNode.ID,
		EventType: EventTypeReassignment,
		Reason:    reason,
		Timestamp: time.Now(),
		Success:   true,
	})

	if jd.logger != nil {
		jd.logger.Info("job reassigned",
			logger.String("job_id", jobID),
			logger.String("from_node", oldNode),
			logger.String("to_node", selectedNode.ID),
			logger.String("reason", reason),
		)
	}

	if jd.metrics != nil {
		jd.metrics.Counter("forge.cron.jobs_reassigned").Inc()
	}

	return selectedNode.ID, nil
}

// GetJobAssignment returns the node assignment for a job
func (jd *JobDistributor) GetJobAssignment(jobID string) (string, error) {
	jd.mu.RLock()
	defer jd.mu.RUnlock()

	nodeID, exists := jd.assignments[jobID]
	if !exists {
		return "", common.ErrServiceNotFound(jobID)
	}

	return nodeID, nil
}

// GetNodeJobs returns all jobs assigned to a node
func (jd *JobDistributor) GetNodeJobs(nodeID string) []string {
	jd.mu.RLock()
	defer jd.mu.RUnlock()

	var jobs []string
	for jobID, assignedNode := range jd.assignments {
		if assignedNode == nodeID {
			jobs = append(jobs, jobID)
		}
	}

	return jobs
}

// GetStats returns distribution statistics
func (jd *JobDistributor) GetStats() *DistributionStats {
	jd.mu.RLock()
	defer jd.mu.RUnlock()

	// Count assignments per node
	nodeAssignments := make(map[string]int)
	for _, nodeID := range jd.assignments {
		nodeAssignments[nodeID]++
	}

	// Build distribution stats
	distributionStats := make(map[string]*NodeDistribution)
	for nodeID, count := range nodeAssignments {
		distributionStats[nodeID] = &NodeDistribution{
			NodeID:       nodeID,
			AssignedJobs: count,
			// TODO: Add more statistics
		}
	}

	return &DistributionStats{
		TotalJobs:         len(jd.assignments),
		ActiveNodes:       len(jd.nodeManager.GetAvailableNodes()),
		Assignments:       jd.assignments,
		NodeLoads:         jd.nodeLoads,
		DistributionStats: distributionStats,
		Strategy:          jd.config.DistributionStrategy,
	}
}

// selectNodeByStrategy selects a node based on the configured strategy
func (jd *JobDistributor) selectNodeByStrategy(job *cron.Job, nodes []*Node) (*Node, error) {
	switch jd.config.DistributionStrategy {
	case RoundRobin:
		return jd.selectRoundRobin(nodes), nil
	case LeastConnections:
		return jd.selectLeastConnections(nodes), nil
	case WeightedRandom:
		return jd.selectWeightedRandom(nodes), nil
	case ConsistentHash:
		return jd.selectConsistentHash(job, nodes), nil
	case LoadAware:
		return jd.selectLoadAware(nodes), nil
	default:
		return jd.selectRoundRobin(nodes), nil
	}
}

// selectRoundRobin selects a node using round-robin strategy
func (jd *JobDistributor) selectRoundRobin(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	// Simple round-robin based on current assignments
	minAssignments := -1
	var selectedNode *Node

	for _, node := range nodes {
		assignments := jd.countNodeAssignments(node.ID)
		if minAssignments == -1 || assignments < minAssignments {
			minAssignments = assignments
			selectedNode = node
		}
	}

	return selectedNode
}

// selectLeastConnections selects the node with least active connections
func (jd *JobDistributor) selectLeastConnections(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	var selectedNode *Node
	minConnections := -1

	for _, node := range nodes {
		if load, exists := jd.nodeLoads[node.ID]; exists {
			connections := load.RunningJobs + load.QueuedJobs
			if minConnections == -1 || connections < minConnections {
				minConnections = connections
				selectedNode = node
			}
		} else {
			// Node with no load data gets priority
			return node
		}
	}

	return selectedNode
}

// selectWeightedRandom selects a node using weighted random strategy
func (jd *JobDistributor) selectWeightedRandom(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	// Calculate total weight
	totalWeight := 0.0
	for _, node := range nodes {
		if load, exists := jd.nodeLoads[node.ID]; exists {
			totalWeight += load.DistributionWeight
		} else {
			totalWeight += 1.0 // Default weight
		}
	}

	// Generate random number
	r := rand.Float64() * totalWeight
	currentWeight := 0.0

	for _, node := range nodes {
		weight := 1.0
		if load, exists := jd.nodeLoads[node.ID]; exists {
			weight = load.DistributionWeight
		}

		currentWeight += weight
		if r <= currentWeight {
			return node
		}
	}

	return nodes[0] // Fallback
}

// selectConsistentHash selects a node using consistent hashing
func (jd *JobDistributor) selectConsistentHash(job *cron.Job, nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	// Simple hash-based selection
	hash := hashString(job.Definition.ID)
	index := hash % len(nodes)
	return nodes[index]
}

// selectLoadAware selects a node based on load awareness
func (jd *JobDistributor) selectLoadAware(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}

	var selectedNode *Node
	bestScore := -1.0

	for _, node := range nodes {
		score := jd.calculateNodeScore(node)
		if bestScore == -1 || score > bestScore {
			bestScore = score
			selectedNode = node
		}
	}

	return selectedNode
}

// calculateNodeScore calculates a score for node selection
func (jd *JobDistributor) calculateNodeScore(node *Node) float64 {
	load, exists := jd.nodeLoads[node.ID]
	if !exists {
		return 1.0 // New node gets high score
	}

	// Calculate score based on various factors
	score := 1.0

	// CPU usage factor (lower is better)
	if load.CPUUsage > 0 {
		score *= (1.0 - load.CPUUsage)
	}

	// Memory usage factor (lower is better)
	if load.MemoryUsage > 0 {
		score *= (1.0 - load.MemoryUsage)
	}

	// Running jobs factor (fewer is better)
	if load.MaxConcurrentJobs > 0 {
		utilizationRatio := float64(load.RunningJobs) / float64(load.MaxConcurrentJobs)
		score *= (1.0 - utilizationRatio)
	}

	// Success rate factor (higher is better)
	if load.TotalJobs > 0 {
		successRate := float64(load.SuccessfulJobs) / float64(load.TotalJobs)
		score *= successRate
	}

	return score
}

// countNodeAssignments counts assignments for a node
func (jd *JobDistributor) countNodeAssignments(nodeID string) int {
	count := 0
	for _, assignedNode := range jd.assignments {
		if assignedNode == nodeID {
			count++
		}
	}
	return count
}

// isNodeHealthy checks if a node is healthy
func (jd *JobDistributor) isNodeHealthy(nodeID string) bool {
	return jd.nodeManager.IsNodeHealthy(nodeID)
}

// logDistributionEvent logs a distribution event
func (jd *JobDistributor) logDistributionEvent(event *DistributionEvent) {
	jd.distributionLog = append(jd.distributionLog, event)

	// Keep only recent events
	if len(jd.distributionLog) > 1000 {
		jd.distributionLog = jd.distributionLog[len(jd.distributionLog)-1000:]
	}
}

// monitorNodes monitors node health and loads
func (jd *JobDistributor) monitorNodes(ctx context.Context) {
	defer jd.wg.Done()

	ticker := time.NewTicker(jd.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-jd.stopChannel:
			return
		case <-ticker.C:
			jd.updateNodeLoads()
		case <-ctx.Done():
			return
		}
	}
}

// updateNodeLoads updates node load information
func (jd *JobDistributor) updateNodeLoads() {
	nodes := jd.nodeManager.GetAllNodes()

	jd.mu.Lock()
	defer jd.mu.Unlock()

	for _, node := range nodes {
		load := jd.nodeManager.GetNodeLoad(node.ID)
		if load != nil {
			jd.nodeLoads[node.ID] = load
		}
	}
}

// updateMetrics updates distributor metrics
func (jd *JobDistributor) updateMetrics(ctx context.Context) {
	defer jd.wg.Done()

	ticker := time.NewTicker(jd.config.MetricsUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-jd.stopChannel:
			return
		case <-ticker.C:
			jd.publishMetrics()
		case <-ctx.Done():
			return
		}
	}
}

// publishMetrics publishes distributor metrics
func (jd *JobDistributor) publishMetrics() {
	if jd.metrics == nil {
		return
	}

	jd.mu.RLock()
	defer jd.mu.RUnlock()

	// Publish basic metrics
	jd.metrics.Gauge("forge.cron.distributor_total_jobs").Set(float64(len(jd.assignments)))
	jd.metrics.Gauge("forge.cron.distributor_active_nodes").Set(float64(len(jd.nodeManager.GetAvailableNodes())))

	// Publish node load metrics
	for _, load := range jd.nodeLoads {
		jd.metrics.Gauge("forge.cron.distributor_node_cpu").Set(load.CPUUsage)
		jd.metrics.Gauge("forge.cron.distributor_node_memory").Set(load.MemoryUsage)
		jd.metrics.Gauge("forge.cron.distributor_node_running_jobs").Set(float64(load.RunningJobs))
		jd.metrics.Gauge("forge.cron.distributor_node_queued_jobs").Set(float64(load.QueuedJobs))
	}
}

// periodicRebalance performs periodic rebalancing
func (jd *JobDistributor) periodicRebalance(ctx context.Context) {
	defer jd.wg.Done()

	ticker := time.NewTicker(jd.config.RebalanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-jd.stopChannel:
			return
		case <-ticker.C:
			jd.performRebalance()
		case <-ctx.Done():
			return
		}
	}
}

// performRebalance performs job rebalancing
func (jd *JobDistributor) performRebalance() {
	if jd.rebalancer == nil {
		return
	}

	jd.mu.Lock()
	defer jd.mu.Unlock()

	// Check if rebalancing is needed
	if !jd.rebalancer.ShouldRebalance(jd.assignments, jd.nodeLoads) {
		return
	}

	// Perform rebalancing
	newAssignments, err := jd.rebalancer.Rebalance(jd.assignments, jd.nodeLoads)
	if err != nil {
		if jd.logger != nil {
			jd.logger.Error("rebalancing failed", logger.Error(err))
		}
		return
	}

	// Apply new assignments
	for jobID, newNode := range newAssignments {
		if oldNode, exists := jd.assignments[jobID]; exists && oldNode != newNode {
			jd.assignments[jobID] = newNode

			// Log rebalance event
			jd.logDistributionEvent(&DistributionEvent{
				ID:        generateEventID(),
				JobID:     jobID,
				FromNode:  oldNode,
				ToNode:    newNode,
				EventType: EventTypeRebalance,
				Reason:    "Periodic rebalancing",
				Timestamp: time.Now(),
				Success:   true,
			})
		}
	}

	if jd.logger != nil {
		jd.logger.Info("rebalancing completed",
			logger.Int("reassigned_jobs", len(newAssignments)),
		)
	}

	if jd.metrics != nil {
		jd.metrics.Counter("forge.cron.distributor_rebalance_completed").Inc()
	}
}

// validateConfig validates distribution configuration
func validateConfig(config *Config) error {
	if config.NodeID == "" {
		return common.ErrValidationError("node_id", fmt.Errorf("node ID is required"))
	}

	if config.ClusterID == "" {
		return common.ErrValidationError("cluster_id", fmt.Errorf("cluster ID is required"))
	}

	if config.RebalanceInterval <= 0 {
		return common.ErrValidationError("rebalance_interval", fmt.Errorf("rebalance interval must be positive"))
	}

	if config.LoadThreshold <= 0 || config.LoadThreshold > 1 {
		return common.ErrValidationError("load_threshold", fmt.Errorf("load threshold must be between 0 and 1"))
	}

	if config.MaxJobsPerNode <= 0 {
		return common.ErrValidationError("max_jobs_per_node", fmt.Errorf("max jobs per node must be positive"))
	}

	if config.HealthCheckInterval <= 0 {
		return common.ErrValidationError("health_check_interval", fmt.Errorf("health check interval must be positive"))
	}

	if config.MetricsUpdateInterval <= 0 {
		return common.ErrValidationError("metrics_update_interval", fmt.Errorf("metrics update interval must be positive"))
	}

	return nil
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return fmt.Sprintf("evt_%d", time.Now().UnixNano())
}

// hashString creates a hash from a string
func hashString(s string) int {
	h := 0
	for _, c := range s {
		h = 31*h + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h
}
