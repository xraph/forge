package distribution

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// LoadBalancer manages load-aware job distribution
type LoadBalancer struct {
	config   *LoadBalancerConfig
	nodeID   string
	strategy LoadBalancingStrategy

	// Load tracking
	nodeMetrics    map[string]*NodeMetrics
	loadHistory    map[string]*LoadHistory
	algorithmState map[string]interface{}
	mu             sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// LoadBalancerConfig contains configuration for load balancer
type LoadBalancerConfig struct {
	Strategy                LoadBalancingStrategy `json:"strategy" yaml:"strategy"`
	HealthCheckInterval     time.Duration         `json:"health_check_interval" yaml:"health_check_interval"`
	MetricsUpdateInterval   time.Duration         `json:"metrics_update_interval" yaml:"metrics_update_interval"`
	LoadHistorySize         int                   `json:"load_history_size" yaml:"load_history_size"`
	ResponseTimeThreshold   time.Duration         `json:"response_time_threshold" yaml:"response_time_threshold"`
	ErrorRateThreshold      float64               `json:"error_rate_threshold" yaml:"error_rate_threshold"`
	CPUThreshold            float64               `json:"cpu_threshold" yaml:"cpu_threshold"`
	MemoryThreshold         float64               `json:"memory_threshold" yaml:"memory_threshold"`
	ConnectionsThreshold    int                   `json:"connections_threshold" yaml:"connections_threshold"`
	WeightAdjustmentFactor  float64               `json:"weight_adjustment_factor" yaml:"weight_adjustment_factor"`
	StickySessionEnabled    bool                  `json:"sticky_session_enabled" yaml:"sticky_session_enabled"`
	HealthCheckPath         string                `json:"health_check_path" yaml:"health_check_path"`
	FailureDetectionWindow  time.Duration         `json:"failure_detection_window" yaml:"failure_detection_window"`
	RecoveryDetectionWindow time.Duration         `json:"recovery_detection_window" yaml:"recovery_detection_window"`
}

// LoadBalancingStrategy defines the load balancing strategy
type LoadBalancingStrategy string

const (
	RoundRobinStrategy       LoadBalancingStrategy = "round_robin"
	LeastConnectionsStrategy LoadBalancingStrategy = "least_connections"
	WeightedRoundRobin       LoadBalancingStrategy = "weighted_round_robin"
	IPHashStrategy           LoadBalancingStrategy = "ip_hash"
	LeastResponseTime        LoadBalancingStrategy = "least_response_time"
	ResourceBasedStrategy    LoadBalancingStrategy = "resource_based"
	AdaptiveStrategy         LoadBalancingStrategy = "adaptive"
)

// NodeMetrics contains metrics for a node
type NodeMetrics struct {
	NodeID              string             `json:"node_id"`
	CPUUsage            float64            `json:"cpu_usage"`
	MemoryUsage         float64            `json:"memory_usage"`
	NetworkUsage        float64            `json:"network_usage"`
	DiskUsage           float64            `json:"disk_usage"`
	ActiveConnections   int                `json:"active_connections"`
	RequestsPerSecond   float64            `json:"requests_per_second"`
	ErrorRate           float64            `json:"error_rate"`
	AverageResponseTime time.Duration      `json:"average_response_time"`
	HealthStatus        HealthStatus       `json:"health_status"`
	Weight              float64            `json:"weight"`
	Capacity            int                `json:"capacity"`
	LastUpdateTime      time.Time          `json:"last_update_time"`
	CustomMetrics       map[string]float64 `json:"custom_metrics"`
}

// LoadHistory contains historical load data for a node
type LoadHistory struct {
	NodeID      string           `json:"node_id"`
	DataPoints  []*LoadDataPoint `json:"data_points"`
	MaxSize     int              `json:"max_size"`
	WindowSize  time.Duration    `json:"window_size"`
	LastUpdated time.Time        `json:"last_updated"`
}

// LoadDataPoint represents a single load measurement
type LoadDataPoint struct {
	Timestamp    time.Time     `json:"timestamp"`
	CPUUsage     float64       `json:"cpu_usage"`
	MemoryUsage  float64       `json:"memory_usage"`
	NetworkUsage float64       `json:"network_usage"`
	Connections  int           `json:"connections"`
	ResponseTime time.Duration `json:"response_time"`
	ErrorRate    float64       `json:"error_rate"`
}

// HealthStatus represents the health status of a node
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// LoadBalancingDecision represents a load balancing decision
type LoadBalancingDecision struct {
	SelectedNode     string                `json:"selected_node"`
	Strategy         LoadBalancingStrategy `json:"strategy"`
	DecisionFactors  map[string]float64    `json:"decision_factors"`
	AlternativeNodes []string              `json:"alternative_nodes"`
	Timestamp        time.Time             `json:"timestamp"`
	DecisionTime     time.Duration         `json:"decision_time"`
}

// LoadBalancerStats contains load balancer statistics
type LoadBalancerStats struct {
	Strategy            LoadBalancingStrategy     `json:"strategy"`
	TotalRequests       int64                     `json:"total_requests"`
	TotalDecisions      int64                     `json:"total_decisions"`
	AverageDecisionTime time.Duration             `json:"average_decision_time"`
	NodeDistribution    map[string]*NodeSelection `json:"node_distribution"`
	LastDecision        *LoadBalancingDecision    `json:"last_decision"`
	HealthyNodes        int                       `json:"healthy_nodes"`
	DegradedNodes       int                       `json:"degraded_nodes"`
	UnhealthyNodes      int                       `json:"unhealthy_nodes"`
	LastUpdate          time.Time                 `json:"last_update"`
}

// NodeSelection contains selection statistics for a node
type NodeSelection struct {
	NodeID              string    `json:"node_id"`
	SelectionCount      int64     `json:"selection_count"`
	SelectionPercentage float64   `json:"selection_percentage"`
	LastSelected        time.Time `json:"last_selected"`
	AverageLoadAtSelect float64   `json:"average_load_at_select"`
}

// DefaultLoadBalancerConfig returns default load balancer configuration
func DefaultLoadBalancerConfig() *LoadBalancerConfig {
	return &LoadBalancerConfig{
		Strategy:                AdaptiveStrategy,
		HealthCheckInterval:     30 * time.Second,
		MetricsUpdateInterval:   10 * time.Second,
		LoadHistorySize:         100,
		ResponseTimeThreshold:   500 * time.Millisecond,
		ErrorRateThreshold:      0.05,
		CPUThreshold:            0.8,
		MemoryThreshold:         0.85,
		ConnectionsThreshold:    1000,
		WeightAdjustmentFactor:  0.1,
		StickySessionEnabled:    false,
		HealthCheckPath:         "/health",
		FailureDetectionWindow:  5 * time.Minute,
		RecoveryDetectionWindow: 2 * time.Minute,
	}
}

// NewLoadBalancer creates a new load balancer
func NewLoadBalancer(config *LoadBalancerConfig, logger common.Logger, metrics common.Metrics) (*LoadBalancer, error) {
	if config == nil {
		config = DefaultLoadBalancerConfig()
	}

	if err := validateLoadBalancerConfig(config); err != nil {
		return nil, err
	}

	lb := &LoadBalancer{
		config:         config,
		strategy:       config.Strategy,
		nodeMetrics:    make(map[string]*NodeMetrics),
		loadHistory:    make(map[string]*LoadHistory),
		algorithmState: make(map[string]interface{}),
		stopChannel:    make(chan struct{}),
		logger:         logger,
		metrics:        metrics,
	}

	// Initialize algorithm state
	lb.initializeAlgorithmState()

	return lb, nil
}

// Start starts the load balancer
func (lb *LoadBalancer) Start(ctx context.Context) error {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.started {
		return common.ErrLifecycleError("start", fmt.Errorf("load balancer already started"))
	}

	// OnStart background tasks
	lb.wg.Add(2)
	go lb.updateMetricsLoop(ctx)
	go lb.healthCheckLoop(ctx)

	lb.started = true

	if lb.logger != nil {
		lb.logger.Info("load balancer started",
			logger.String("strategy", string(lb.strategy)),
		)
	}

	if lb.metrics != nil {
		lb.metrics.Counter("forge.cron.load_balancer_started").Inc()
	}

	return nil
}

// Stop stops the load balancer
func (lb *LoadBalancer) Stop(ctx context.Context) error {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if !lb.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("load balancer not started"))
	}

	if lb.logger != nil {
		lb.logger.Info("stopping load balancer")
	}

	// Signal stop
	close(lb.stopChannel)

	// Wait for background tasks to finish
	lb.wg.Wait()

	lb.started = false

	if lb.logger != nil {
		lb.logger.Info("load balancer stopped")
	}

	if lb.metrics != nil {
		lb.metrics.Counter("forge.cron.load_balancer_stopped").Inc()
	}

	return nil
}

// SelectNode selects a node for job execution based on load balancing strategy
func (lb *LoadBalancer) SelectNode(nodes []*Node, criteria *SelectionCriteria) (*Node, *LoadBalancingDecision, error) {
	if len(nodes) == 0 {
		return nil, nil, common.ErrServiceNotFound("available_nodes")
	}

	startTime := time.Now()

	lb.mu.RLock()
	defer lb.mu.RUnlock()

	// Filter healthy nodes
	healthyNodes := lb.filterHealthyNodes(nodes)
	if len(healthyNodes) == 0 {
		return nil, nil, common.ErrServiceNotFound("healthy_nodes")
	}

	// Select node based on strategy
	var selectedNode *Node
	var decisionFactors map[string]float64
	var alternativeNodes []string

	switch lb.strategy {
	case RoundRobinStrategy:
		selectedNode, decisionFactors = lb.selectRoundRobin(healthyNodes)
	case LeastConnectionsStrategy:
		selectedNode, decisionFactors = lb.selectLeastConnections(healthyNodes)
	case WeightedRoundRobin:
		selectedNode, decisionFactors = lb.selectWeightedRoundRobin(healthyNodes)
	case IPHashStrategy:
		selectedNode, decisionFactors = lb.selectIPHash(healthyNodes, criteria)
	case LeastResponseTime:
		selectedNode, decisionFactors = lb.selectLeastResponseTime(healthyNodes)
	case ResourceBasedStrategy:
		selectedNode, decisionFactors = lb.selectResourceBased(healthyNodes)
	case AdaptiveStrategy:
		selectedNode, decisionFactors = lb.selectAdaptive(healthyNodes, criteria)
	default:
		selectedNode, decisionFactors = lb.selectRoundRobin(healthyNodes)
	}

	if selectedNode == nil {
		return nil, nil, common.ErrServiceNotFound("suitable_node")
	}

	// Get alternative nodes (top 3 candidates)
	for _, node := range healthyNodes {
		if node.ID != selectedNode.ID && len(alternativeNodes) < 3 {
			alternativeNodes = append(alternativeNodes, node.ID)
		}
	}

	// Create decision record
	decision := &LoadBalancingDecision{
		SelectedNode:     selectedNode.ID,
		Strategy:         lb.strategy,
		DecisionFactors:  decisionFactors,
		AlternativeNodes: alternativeNodes,
		Timestamp:        time.Now(),
		DecisionTime:     time.Since(startTime),
	}

	// Update statistics
	lb.updateSelectionStats(selectedNode.ID, decision)

	if lb.logger != nil {
		lb.logger.Debug("node selected",
			logger.String("node_id", selectedNode.ID),
			logger.String("strategy", string(lb.strategy)),
			logger.Duration("decision_time", decision.DecisionTime),
		)
	}

	if lb.metrics != nil {
		lb.metrics.Counter("forge.cron.load_balancer_selections").Inc()
		lb.metrics.Histogram("forge.cron.load_balancer_decision_time").Observe(decision.DecisionTime.Seconds())
	}

	return selectedNode, decision, nil
}

// UpdateNodeMetrics updates metrics for a node
func (lb *LoadBalancer) UpdateNodeMetrics(nodeID string, metrics *NodeMetrics) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	metrics.LastUpdateTime = time.Now()
	lb.nodeMetrics[nodeID] = metrics

	// Update load history
	lb.updateLoadHistory(nodeID, metrics)

	// Update node health status
	lb.updateNodeHealth(nodeID, metrics)
}

// GetNodeMetrics returns metrics for a node
func (lb *LoadBalancer) GetNodeMetrics(nodeID string) *NodeMetrics {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	return lb.nodeMetrics[nodeID]
}

// GetLoadBalancerStats returns load balancer statistics
func (lb *LoadBalancer) GetLoadBalancerStats() *LoadBalancerStats {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	stats := &LoadBalancerStats{
		Strategy:         lb.strategy,
		NodeDistribution: make(map[string]*NodeSelection),
		LastUpdate:       time.Now(),
	}

	// Count nodes by health status
	for _, metrics := range lb.nodeMetrics {
		switch metrics.HealthStatus {
		case HealthStatusHealthy:
			stats.HealthyNodes++
		case HealthStatusDegraded:
			stats.DegradedNodes++
		case HealthStatusUnhealthy:
			stats.UnhealthyNodes++
		}
	}

	// TODO: Add more statistics from algorithm state
	if selectionCounts, ok := lb.algorithmState["selection_counts"].(map[string]int64); ok {
		for nodeID, count := range selectionCounts {
			stats.NodeDistribution[nodeID] = &NodeSelection{
				NodeID:         nodeID,
				SelectionCount: count,
				// TODO: Calculate selection percentage
			}
		}
	}

	return stats
}

// filterHealthyNodes filters nodes based on health status
func (lb *LoadBalancer) filterHealthyNodes(nodes []*Node) []*Node {
	var healthyNodes []*Node

	for _, node := range nodes {
		if metrics, exists := lb.nodeMetrics[node.ID]; exists {
			if metrics.HealthStatus == HealthStatusHealthy || metrics.HealthStatus == HealthStatusDegraded {
				healthyNodes = append(healthyNodes, node)
			}
		} else {
			// Node with no metrics is considered healthy
			healthyNodes = append(healthyNodes, node)
		}
	}

	return healthyNodes
}

// selectRoundRobin selects a node using round-robin algorithm
func (lb *LoadBalancer) selectRoundRobin(nodes []*Node) (*Node, map[string]float64) {
	if len(nodes) == 0 {
		return nil, nil
	}

	// Get current index
	currentIndex, _ := lb.algorithmState["round_robin_index"].(int)
	if currentIndex >= len(nodes) {
		currentIndex = 0
	}

	selectedNode := nodes[currentIndex]

	// Update index for next selection
	lb.algorithmState["round_robin_index"] = (currentIndex + 1) % len(nodes)

	return selectedNode, map[string]float64{
		"round_robin_index": float64(currentIndex),
	}
}

// selectLeastConnections selects the node with least connections
func (lb *LoadBalancer) selectLeastConnections(nodes []*Node) (*Node, map[string]float64) {
	if len(nodes) == 0 {
		return nil, nil
	}

	var selectedNode *Node
	minConnections := math.MaxInt32

	decisionFactors := make(map[string]float64)

	for _, node := range nodes {
		connections := 0
		if metrics, exists := lb.nodeMetrics[node.ID]; exists {
			connections = metrics.ActiveConnections
		}

		decisionFactors[node.ID+"_connections"] = float64(connections)

		if connections < minConnections {
			minConnections = connections
			selectedNode = node
		}
	}

	return selectedNode, decisionFactors
}

// selectWeightedRoundRobin selects a node using weighted round-robin
func (lb *LoadBalancer) selectWeightedRoundRobin(nodes []*Node) (*Node, map[string]float64) {
	if len(nodes) == 0 {
		return nil, nil
	}

	// Get current weights
	currentWeights, _ := lb.algorithmState["weighted_current_weights"].(map[string]float64)
	if currentWeights == nil {
		currentWeights = make(map[string]float64)
	}

	var selectedNode *Node
	maxWeight := float64(-1)
	totalWeight := float64(0)

	decisionFactors := make(map[string]float64)

	// Calculate weights and find max
	for _, node := range nodes {
		weight := float64(1) // Default weight
		if metrics, exists := lb.nodeMetrics[node.ID]; exists {
			weight = metrics.Weight
		}

		currentWeights[node.ID] += weight
		totalWeight += weight
		decisionFactors[node.ID+"_weight"] = weight
		decisionFactors[node.ID+"_current_weight"] = currentWeights[node.ID]

		if currentWeights[node.ID] > maxWeight {
			maxWeight = currentWeights[node.ID]
			selectedNode = node
		}
	}

	// Decrease selected node's current weight
	if selectedNode != nil {
		currentWeights[selectedNode.ID] -= totalWeight
	}

	// Update state
	lb.algorithmState["weighted_current_weights"] = currentWeights

	return selectedNode, decisionFactors
}

// selectIPHash selects a node using IP hash (consistent hashing)
func (lb *LoadBalancer) selectIPHash(nodes []*Node, criteria *SelectionCriteria) (*Node, map[string]float64) {
	if len(nodes) == 0 {
		return nil, nil
	}

	// Use job ID as hash key if available
	hashKey := ""
	if criteria != nil && criteria.JobID != "" {
		hashKey = criteria.JobID
	} else {
		hashKey = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Calculate hash
	hash := hashString(hashKey)
	index := hash % len(nodes)

	selectedNode := nodes[index]

	return selectedNode, map[string]float64{
		"hash_value": float64(hash),
		"node_index": float64(index),
	}
}

// selectLeastResponseTime selects the node with least response time
func (lb *LoadBalancer) selectLeastResponseTime(nodes []*Node) (*Node, map[string]float64) {
	if len(nodes) == 0 {
		return nil, nil
	}

	var selectedNode *Node
	minResponseTime := time.Duration(math.MaxInt64)

	decisionFactors := make(map[string]float64)

	for _, node := range nodes {
		responseTime := time.Duration(0)
		if metrics, exists := lb.nodeMetrics[node.ID]; exists {
			responseTime = metrics.AverageResponseTime
		}

		decisionFactors[node.ID+"_response_time"] = float64(responseTime.Milliseconds())

		if selectedNode == nil || responseTime < minResponseTime {
			minResponseTime = responseTime
			selectedNode = node
		}
	}

	return selectedNode, decisionFactors
}

// selectResourceBased selects a node based on resource usage
func (lb *LoadBalancer) selectResourceBased(nodes []*Node) (*Node, map[string]float64) {
	if len(nodes) == 0 {
		return nil, nil
	}

	var selectedNode *Node
	bestScore := float64(-1)

	decisionFactors := make(map[string]float64)

	for _, node := range nodes {
		score := lb.calculateResourceScore(node)
		decisionFactors[node.ID+"_resource_score"] = score

		if score > bestScore {
			bestScore = score
			selectedNode = node
		}
	}

	return selectedNode, decisionFactors
}

// selectAdaptive selects a node using adaptive algorithm
func (lb *LoadBalancer) selectAdaptive(nodes []*Node, criteria *SelectionCriteria) (*Node, map[string]float64) {
	if len(nodes) == 0 {
		return nil, nil
	}

	// Combine multiple factors for adaptive selection
	var selectedNode *Node
	bestScore := float64(-1)

	decisionFactors := make(map[string]float64)

	for _, node := range nodes {
		score := lb.calculateAdaptiveScore(node, criteria)
		decisionFactors[node.ID+"_adaptive_score"] = score

		if score > bestScore {
			bestScore = score
			selectedNode = node
		}
	}

	return selectedNode, decisionFactors
}

// calculateResourceScore calculates a resource-based score for a node
func (lb *LoadBalancer) calculateResourceScore(node *Node) float64 {
	metrics, exists := lb.nodeMetrics[node.ID]
	if !exists {
		return 1.0 // Default score for unknown nodes
	}

	score := 1.0

	// CPU factor (lower usage is better)
	if metrics.CPUUsage > 0 {
		score *= (1.0 - metrics.CPUUsage)
	}

	// Memory factor (lower usage is better)
	if metrics.MemoryUsage > 0 {
		score *= (1.0 - metrics.MemoryUsage)
	}

	// Network factor (lower usage is better)
	if metrics.NetworkUsage > 0 {
		score *= (1.0 - metrics.NetworkUsage)
	}

	// Connection factor (fewer connections is better)
	if metrics.Capacity > 0 {
		connectionRatio := float64(metrics.ActiveConnections) / float64(metrics.Capacity)
		score *= (1.0 - connectionRatio)
	}

	return score
}

// calculateAdaptiveScore calculates an adaptive score combining multiple factors
func (lb *LoadBalancer) calculateAdaptiveScore(node *Node, criteria *SelectionCriteria) float64 {
	metrics, exists := lb.nodeMetrics[node.ID]
	if !exists {
		return 1.0 // Default score for unknown nodes
	}

	score := 1.0

	// Base resource score
	resourceScore := lb.calculateResourceScore(node)
	score *= resourceScore

	// Response time factor
	if metrics.AverageResponseTime > 0 {
		responseTimeFactor := 1.0 - (float64(metrics.AverageResponseTime.Milliseconds()) / 1000.0)
		if responseTimeFactor < 0 {
			responseTimeFactor = 0
		}
		score *= responseTimeFactor
	}

	// Error rate factor
	if metrics.ErrorRate > 0 {
		score *= (1.0 - metrics.ErrorRate)
	}

	// Weight factor
	if metrics.Weight > 0 {
		score *= metrics.Weight
	}

	// Health status factor
	switch metrics.HealthStatus {
	case HealthStatusHealthy:
		score *= 1.0
	case HealthStatusDegraded:
		score *= 0.7
	case HealthStatusUnhealthy:
		score *= 0.1
	default:
		score *= 0.8
	}

	return score
}

// updateLoadHistory updates the load history for a node
func (lb *LoadBalancer) updateLoadHistory(nodeID string, metrics *NodeMetrics) {
	history, exists := lb.loadHistory[nodeID]
	if !exists {
		history = &LoadHistory{
			NodeID:     nodeID,
			DataPoints: make([]*LoadDataPoint, 0),
			MaxSize:    lb.config.LoadHistorySize,
		}
		lb.loadHistory[nodeID] = history
	}

	// Add new data point
	dataPoint := &LoadDataPoint{
		Timestamp:    time.Now(),
		CPUUsage:     metrics.CPUUsage,
		MemoryUsage:  metrics.MemoryUsage,
		NetworkUsage: metrics.NetworkUsage,
		Connections:  metrics.ActiveConnections,
		ResponseTime: metrics.AverageResponseTime,
		ErrorRate:    metrics.ErrorRate,
	}

	history.DataPoints = append(history.DataPoints, dataPoint)

	// Keep only recent data points
	if len(history.DataPoints) > history.MaxSize {
		history.DataPoints = history.DataPoints[len(history.DataPoints)-history.MaxSize:]
	}

	history.LastUpdated = time.Now()
}

// updateNodeHealth updates the health status of a node
func (lb *LoadBalancer) updateNodeHealth(nodeID string, metrics *NodeMetrics) {
	// Simple health check based on thresholds
	if metrics.CPUUsage > lb.config.CPUThreshold ||
		metrics.MemoryUsage > lb.config.MemoryThreshold ||
		metrics.ErrorRate > lb.config.ErrorRateThreshold ||
		metrics.AverageResponseTime > lb.config.ResponseTimeThreshold {

		metrics.HealthStatus = HealthStatusDegraded
	} else {
		metrics.HealthStatus = HealthStatusHealthy
	}

	// Additional health checks can be added here
}

// updateSelectionStats updates selection statistics
func (lb *LoadBalancer) updateSelectionStats(nodeID string, decision *LoadBalancingDecision) {
	// Update selection counts
	selectionCounts, _ := lb.algorithmState["selection_counts"].(map[string]int64)
	if selectionCounts == nil {
		selectionCounts = make(map[string]int64)
		lb.algorithmState["selection_counts"] = selectionCounts
	}

	selectionCounts[nodeID]++

	// Update total decisions
	if totalDecisions, ok := lb.algorithmState["total_decisions"].(int64); ok {
		lb.algorithmState["total_decisions"] = totalDecisions + 1
	} else {
		lb.algorithmState["total_decisions"] = int64(1)
	}

	// Update average decision time
	if avgTime, ok := lb.algorithmState["avg_decision_time"].(time.Duration); ok {
		total := lb.algorithmState["total_decisions"].(int64)
		newAvg := (avgTime*time.Duration(total-1) + decision.DecisionTime) / time.Duration(total)
		lb.algorithmState["avg_decision_time"] = newAvg
	} else {
		lb.algorithmState["avg_decision_time"] = decision.DecisionTime
	}

	// Store last decision
	lb.algorithmState["last_decision"] = decision
}

// initializeAlgorithmState initializes algorithm-specific state
func (lb *LoadBalancer) initializeAlgorithmState() {
	lb.algorithmState["round_robin_index"] = 0
	lb.algorithmState["weighted_current_weights"] = make(map[string]float64)
	lb.algorithmState["selection_counts"] = make(map[string]int64)
	lb.algorithmState["total_decisions"] = int64(0)
	lb.algorithmState["avg_decision_time"] = time.Duration(0)
}

// updateMetricsLoop periodically updates metrics
func (lb *LoadBalancer) updateMetricsLoop(ctx context.Context) {
	defer lb.wg.Done()

	ticker := time.NewTicker(lb.config.MetricsUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lb.stopChannel:
			return
		case <-ticker.C:
			lb.updateInternalMetrics()
		case <-ctx.Done():
			return
		}
	}
}

// healthCheckLoop periodically performs health checks
func (lb *LoadBalancer) healthCheckLoop(ctx context.Context) {
	defer lb.wg.Done()

	ticker := time.NewTicker(lb.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lb.stopChannel:
			return
		case <-ticker.C:
			lb.performHealthChecks()
		case <-ctx.Done():
			return
		}
	}
}

// updateInternalMetrics updates internal metrics
func (lb *LoadBalancer) updateInternalMetrics() {
	if lb.metrics == nil {
		return
	}

	lb.mu.RLock()
	defer lb.mu.RUnlock()

	// Update node health metrics
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0

	for _, metrics := range lb.nodeMetrics {
		switch metrics.HealthStatus {
		case HealthStatusHealthy:
			healthyCount++
		case HealthStatusDegraded:
			degradedCount++
		case HealthStatusUnhealthy:
			unhealthyCount++
		}
	}

	lb.metrics.Gauge("forge.cron.load_balancer_healthy_nodes").Set(float64(healthyCount))
	lb.metrics.Gauge("forge.cron.load_balancer_degraded_nodes").Set(float64(degradedCount))
	lb.metrics.Gauge("forge.cron.load_balancer_unhealthy_nodes").Set(float64(unhealthyCount))
}

// performHealthChecks performs health checks on nodes
func (lb *LoadBalancer) performHealthChecks() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	now := time.Now()

	// Check for stale metrics
	for _, metrics := range lb.nodeMetrics {
		if now.Sub(metrics.LastUpdateTime) > lb.config.HealthCheckInterval*3 {
			metrics.HealthStatus = HealthStatusUnknown
		}
	}
}

// SelectionCriteria contains criteria for node selection
type SelectionCriteria struct {
	JobID        string             `json:"job_id"`
	JobType      string             `json:"job_type"`
	Requirements map[string]string  `json:"requirements"`
	Preferences  map[string]float64 `json:"preferences"`
}

// validateLoadBalancerConfig validates load balancer configuration
func validateLoadBalancerConfig(config *LoadBalancerConfig) error {
	if config.HealthCheckInterval <= 0 {
		return common.ErrValidationError("health_check_interval", fmt.Errorf("health check interval must be positive"))
	}

	if config.MetricsUpdateInterval <= 0 {
		return common.ErrValidationError("metrics_update_interval", fmt.Errorf("metrics update interval must be positive"))
	}

	if config.LoadHistorySize <= 0 {
		return common.ErrValidationError("load_history_size", fmt.Errorf("load history size must be positive"))
	}

	if config.ErrorRateThreshold < 0 || config.ErrorRateThreshold > 1 {
		return common.ErrValidationError("error_rate_threshold", fmt.Errorf("error rate threshold must be between 0 and 1"))
	}

	if config.CPUThreshold < 0 || config.CPUThreshold > 1 {
		return common.ErrValidationError("cpu_threshold", fmt.Errorf("CPU threshold must be between 0 and 1"))
	}

	if config.MemoryThreshold < 0 || config.MemoryThreshold > 1 {
		return common.ErrValidationError("memory_threshold", fmt.Errorf("memory threshold must be between 0 and 1"))
	}

	return nil
}
