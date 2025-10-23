package distribution

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Rebalancer manages job rebalancing across cluster nodes
type Rebalancer struct {
	config    *RebalancerConfig
	nodeID    string
	strategy  RebalanceStrategy
	history   []*RebalanceEvent
	analytics *RebalanceAnalytics

	// Rebalancing state
	lastRebalance    time.Time
	rebalanceCount   int64
	isRebalancing    bool
	thresholdTracker *ThresholdTracker
	mu               sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// RebalancerConfig contains configuration for job rebalancer
type RebalancerConfig struct {
	Strategy             RebalanceStrategy `json:"strategy" yaml:"strategy"`
	RebalanceInterval    time.Duration     `json:"rebalance_interval" yaml:"rebalance_interval"`
	LoadThreshold        float64           `json:"load_threshold" yaml:"load_threshold"`
	ImbalanceThreshold   float64           `json:"imbalance_threshold" yaml:"imbalance_threshold"`
	MinRebalanceInterval time.Duration     `json:"min_rebalance_interval" yaml:"min_rebalance_interval"`
	MaxJobsToMove        int               `json:"max_jobs_to_move" yaml:"max_jobs_to_move"`
	CooldownPeriod       time.Duration     `json:"cooldown_period" yaml:"cooldown_period"`
	EnablePreemption     bool              `json:"enable_preemption" yaml:"enable_preemption"`
	PreemptionThreshold  float64           `json:"preemption_threshold" yaml:"preemption_threshold"`
	AnalyticsWindow      time.Duration     `json:"analytics_window" yaml:"analytics_window"`
	AnalyticsEnabled     bool              `json:"analytics_enabled" yaml:"analytics_enabled"`
	DryRunMode           bool              `json:"dry_run_mode" yaml:"dry_run_mode"`
	ResourceWeights      ResourceWeights   `json:"resource_weights" yaml:"resource_weights"`
	StabilityWindow      time.Duration     `json:"stability_window" yaml:"stability_window"`
	FlappingThreshold    int               `json:"flapping_threshold" yaml:"flapping_threshold"`
	BatchSize            int               `json:"batch_size" yaml:"batch_size"`
	MaxConcurrentMoves   int               `json:"max_concurrent_moves" yaml:"max_concurrent_moves"`
}

// RebalanceStrategy defines the rebalancing strategy
type RebalanceStrategy string

const (
	LoadBasedStrategy              RebalanceStrategy = "load_based"
	RebalanceResourceBasedStrategy RebalanceStrategy = "resource_based"
	LatencyBasedStrategy           RebalanceStrategy = "latency_based"
	HybridStrategy                 RebalanceStrategy = "hybrid"
	PredictiveStrategy             RebalanceStrategy = "predictive"
	RebalanceAdaptiveStrategy      RebalanceStrategy = "adaptive"
)

// ResourceWeights defines weights for different resource types
type ResourceWeights struct {
	CPU     float64 `json:"cpu" yaml:"cpu"`
	Memory  float64 `json:"memory" yaml:"memory"`
	Network float64 `json:"network" yaml:"network"`
	Disk    float64 `json:"disk" yaml:"disk"`
	Jobs    float64 `json:"jobs" yaml:"jobs"`
}

// RebalanceEvent represents a rebalancing event
type RebalanceEvent struct {
	ID               string            `json:"id"`
	Strategy         RebalanceStrategy `json:"strategy"`
	StartTime        time.Time         `json:"start_time"`
	EndTime          time.Time         `json:"end_time"`
	Duration         time.Duration     `json:"duration"`
	JobsMoved        int               `json:"jobs_moved"`
	NodesInvolved    []string          `json:"nodes_involved"`
	Reason           string            `json:"reason"`
	Success          bool              `json:"success"`
	Error            string            `json:"error,omitempty"`
	BeforeState      *ClusterState     `json:"before_state"`
	AfterState       *ClusterState     `json:"after_state"`
	ImprovementScore float64           `json:"improvement_score"`
	Moves            []*JobMove        `json:"moves"`
	DryRun           bool              `json:"dry_run"`
}

// JobMove represents a job movement during rebalancing
type JobMove struct {
	JobID    string                 `json:"job_id"`
	FromNode string                 `json:"from_node"`
	ToNode   string                 `json:"to_node"`
	Reason   string                 `json:"reason"`
	Priority int                    `json:"priority"`
	Success  bool                   `json:"success"`
	Error    string                 `json:"error,omitempty"`
	MoveTime time.Time              `json:"move_time"`
	Duration time.Duration          `json:"duration"`
	Metadata map[string]interface{} `json:"metadata"`
}

// ClusterState represents the state of the cluster
type ClusterState struct {
	Timestamp      time.Time             `json:"timestamp"`
	NodeStates     map[string]*NodeState `json:"node_states"`
	TotalJobs      int                   `json:"total_jobs"`
	TotalNodes     int                   `json:"total_nodes"`
	LoadVariance   float64               `json:"load_variance"`
	AverageLoad    float64               `json:"average_load"`
	MaxLoad        float64               `json:"max_load"`
	MinLoad        float64               `json:"min_load"`
	HealthyNodes   int                   `json:"healthy_nodes"`
	UnhealthyNodes int                   `json:"unhealthy_nodes"`
	ImbalanceScore float64               `json:"imbalance_score"`
}

// NodeState represents the state of a node
type NodeState struct {
	NodeID          string  `json:"node_id"`
	JobCount        int     `json:"job_count"`
	LoadScore       float64 `json:"load_score"`
	CPUUsage        float64 `json:"cpu_usage"`
	MemoryUsage     float64 `json:"memory_usage"`
	NetworkUsage    float64 `json:"network_usage"`
	DiskUsage       float64 `json:"disk_usage"`
	Capacity        int     `json:"capacity"`
	UtilizationRate float64 `json:"utilization_rate"`
	HealthStatus    string  `json:"health_status"`
	Weight          float64 `json:"weight"`
}

// ThresholdTracker tracks threshold violations
type ThresholdTracker struct {
	violations      map[string]int
	lastViolation   map[string]time.Time
	stabilityWindow time.Duration
	mu              sync.RWMutex
}

// RebalanceAnalytics tracks rebalancing analytics
type RebalanceAnalytics struct {
	TotalRebalances          int64                     `json:"total_rebalances"`
	SuccessfulMoves          int64                     `json:"successful_moves"`
	FailedMoves              int64                     `json:"failed_moves"`
	TotalJobsMoved           int64                     `json:"total_jobs_moved"`
	AverageMovesPerRebalance float64                   `json:"average_moves_per_rebalance"`
	AverageImprovement       float64                   `json:"average_improvement"`
	RebalanceEfficiency      float64                   `json:"rebalance_efficiency"`
	LastRebalanceTime        time.Time                 `json:"last_rebalance_time"`
	RebalanceHistory         []*RebalanceEvent         `json:"rebalance_history"`
	NodeMoveStats            map[string]*NodeMoveStats `json:"node_move_stats"`
}

// NodeMoveStats tracks move statistics for a node
type NodeMoveStats struct {
	NodeID       string    `json:"node_id"`
	JobsMovedIn  int64     `json:"jobs_moved_in"`
	JobsMovedOut int64     `json:"jobs_moved_out"`
	NetMoves     int64     `json:"net_moves"`
	LastMoveTime time.Time `json:"last_move_time"`
}

// RebalanceDecision represents a rebalancing decision
type RebalanceDecision struct {
	ShouldRebalance bool               `json:"should_rebalance"`
	Reason          string             `json:"reason"`
	Strategy        RebalanceStrategy  `json:"strategy"`
	Urgency         RebalanceUrgency   `json:"urgency"`
	ExpectedMoves   int                `json:"expected_moves"`
	ImpactScore     float64            `json:"impact_score"`
	RiskScore       float64            `json:"risk_score"`
	Confidence      float64            `json:"confidence"`
	Factors         map[string]float64 `json:"factors"`
	Recommendations []string           `json:"recommendations"`
}

// RebalanceUrgency defines the urgency of rebalancing
type RebalanceUrgency string

const (
	UrgencyLow      RebalanceUrgency = "low"
	UrgencyMedium   RebalanceUrgency = "medium"
	UrgencyHigh     RebalanceUrgency = "high"
	UrgencyCritical RebalanceUrgency = "critical"
)

// DefaultRebalancerConfig returns default rebalancer configuration
func DefaultRebalancerConfig() *RebalancerConfig {
	return &RebalancerConfig{
		Strategy:             HybridStrategy,
		RebalanceInterval:    5 * time.Minute,
		LoadThreshold:        0.8,
		ImbalanceThreshold:   0.3,
		MinRebalanceInterval: 2 * time.Minute,
		MaxJobsToMove:        10,
		CooldownPeriod:       30 * time.Second,
		EnablePreemption:     false,
		PreemptionThreshold:  0.9,
		AnalyticsWindow:      24 * time.Hour,
		AnalyticsEnabled:     true,
		DryRunMode:           false,
		ResourceWeights: ResourceWeights{
			CPU:     0.3,
			Memory:  0.3,
			Network: 0.2,
			Disk:    0.1,
			Jobs:    0.1,
		},
		StabilityWindow:    5 * time.Minute,
		FlappingThreshold:  3,
		BatchSize:          5,
		MaxConcurrentMoves: 3,
	}
}

// NewRebalancer creates a new job rebalancer
func NewRebalancer(config *RebalancerConfig, logger common.Logger, metrics common.Metrics) (*Rebalancer, error) {
	if config == nil {
		config = DefaultRebalancerConfig()
	}

	if err := validateRebalancerConfig(config); err != nil {
		return nil, err
	}

	rb := &Rebalancer{
		config:   config,
		strategy: config.Strategy,
		history:  make([]*RebalanceEvent, 0),
		analytics: &RebalanceAnalytics{
			NodeMoveStats: make(map[string]*NodeMoveStats),
		},
		thresholdTracker: &ThresholdTracker{
			violations:      make(map[string]int),
			lastViolation:   make(map[string]time.Time),
			stabilityWindow: config.StabilityWindow,
		},
		stopChannel: make(chan struct{}),
		logger:      logger,
		metrics:     metrics,
	}

	return rb, nil
}

// Start starts the rebalancer
func (rb *Rebalancer) Start(ctx context.Context) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.started {
		return common.ErrLifecycleError("start", fmt.Errorf("rebalancer already started"))
	}

	// Start background tasks
	rb.wg.Add(2)
	go rb.analyticsLoop(ctx)
	go rb.thresholdMonitorLoop(ctx)

	rb.started = true

	if rb.logger != nil {
		rb.logger.Info("rebalancer started",
			logger.String("strategy", string(rb.strategy)),
			logger.Bool("dry_run", rb.config.DryRunMode),
		)
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.rebalancer_started").Inc()
	}

	return nil
}

// Stop stops the rebalancer
func (rb *Rebalancer) Stop(ctx context.Context) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("rebalancer not started"))
	}

	if rb.logger != nil {
		rb.logger.Info("stopping rebalancer")
	}

	// Signal stop
	close(rb.stopChannel)

	// Wait for background tasks to finish
	rb.wg.Wait()

	rb.started = false

	if rb.logger != nil {
		rb.logger.Info("rebalancer stopped")
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.rebalancer_stopped").Inc()
	}

	return nil
}

// ShouldRebalance determines if rebalancing should occur
func (rb *Rebalancer) ShouldRebalance(assignments map[string]string, nodeLoads map[string]*NodeLoad) bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	// Check if rebalancing is already in progress
	if rb.isRebalancing {
		return false
	}

	// Check cooldown period
	if time.Since(rb.lastRebalance) < rb.config.CooldownPeriod {
		return false
	}

	// Check minimum rebalance interval
	if time.Since(rb.lastRebalance) < rb.config.MinRebalanceInterval {
		return false
	}

	// Get rebalance decision
	decision := rb.makeRebalanceDecision(assignments, nodeLoads)

	return decision.ShouldRebalance
}

// Rebalance performs job rebalancing
func (rb *Rebalancer) Rebalance(assignments map[string]string, nodeLoads map[string]*NodeLoad) (map[string]string, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.isRebalancing {
		return assignments, common.ErrLifecycleError("rebalance", fmt.Errorf("rebalancing already in progress"))
	}

	rb.isRebalancing = true
	defer func() {
		rb.isRebalancing = false
	}()

	startTime := time.Now()
	eventID := rb.generateEventID()

	// Create rebalance event
	event := &RebalanceEvent{
		ID:        eventID,
		Strategy:  rb.strategy,
		StartTime: startTime,
		DryRun:    rb.config.DryRunMode,
		Moves:     make([]*JobMove, 0),
	}

	// Capture before state
	event.BeforeState = rb.captureClusterState(assignments, nodeLoads)

	// Make rebalance decision
	decision := rb.makeRebalanceDecision(assignments, nodeLoads)
	if !decision.ShouldRebalance {
		event.Success = true
		event.Reason = "No rebalancing needed"
		event.EndTime = time.Now()
		event.Duration = event.EndTime.Sub(startTime)
		rb.recordEvent(event)
		return assignments, nil
	}

	event.Reason = decision.Reason

	// Generate rebalance plan
	plan, err := rb.generateRebalancePlan(assignments, nodeLoads, decision)
	if err != nil {
		event.Success = false
		event.Error = err.Error()
		event.EndTime = time.Now()
		event.Duration = event.EndTime.Sub(startTime)
		rb.recordEvent(event)
		return assignments, err
	}

	// Execute rebalance plan
	newAssignments, moves, err := rb.executeRebalancePlan(assignments, plan)
	if err != nil {
		event.Success = false
		event.Error = err.Error()
		event.Moves = moves
		event.EndTime = time.Now()
		event.Duration = event.EndTime.Sub(startTime)
		rb.recordEvent(event)
		return assignments, err
	}

	// Capture after state
	event.AfterState = rb.captureClusterState(newAssignments, nodeLoads)

	// Calculate improvement score
	event.ImprovementScore = rb.calculateImprovement(event.BeforeState, event.AfterState)

	// Record successful rebalance
	event.Success = true
	event.JobsMoved = len(moves)
	event.Moves = moves
	event.EndTime = time.Now()
	event.Duration = event.EndTime.Sub(startTime)
	event.NodesInvolved = rb.extractNodesInvolved(moves)

	rb.recordEvent(event)
	rb.lastRebalance = time.Now()
	rb.rebalanceCount++

	if rb.logger != nil {
		rb.logger.Info("rebalancing completed",
			logger.String("event_id", eventID),
			logger.Int("jobs_moved", event.JobsMoved),
			logger.Duration("duration", event.Duration),
			logger.Float64("improvement_score", event.ImprovementScore),
			logger.Bool("dry_run", rb.config.DryRunMode),
		)
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.rebalance_completed").Inc()
		rb.metrics.Counter("forge.cron.jobs_moved").Add(float64(event.JobsMoved))
		rb.metrics.Histogram("forge.cron.rebalance_duration").Observe(event.Duration.Seconds())
		rb.metrics.Histogram("forge.cron.rebalance_improvement").Observe(event.ImprovementScore)
	}

	return newAssignments, nil
}

// GetRebalanceHistory returns rebalancing history
func (rb *Rebalancer) GetRebalanceHistory() []*RebalanceEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return rb.history
}

// GetAnalytics returns rebalancing analytics
func (rb *Rebalancer) GetAnalytics() *RebalanceAnalytics {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return rb.analytics
}

// makeRebalanceDecision makes a decision about whether to rebalance
func (rb *Rebalancer) makeRebalanceDecision(assignments map[string]string, nodeLoads map[string]*NodeLoad) *RebalanceDecision {
	decision := &RebalanceDecision{
		Strategy:        rb.strategy,
		Factors:         make(map[string]float64),
		Recommendations: make([]string, 0),
	}

	// Calculate cluster state
	clusterState := rb.captureClusterState(assignments, nodeLoads)

	// Check load imbalance
	loadImbalance := rb.calculateLoadImbalance(clusterState)
	decision.Factors["load_imbalance"] = loadImbalance

	// Check resource utilization
	resourceUtilization := rb.calculateResourceUtilization(clusterState)
	decision.Factors["resource_utilization"] = resourceUtilization

	// Check threshold violations
	thresholdViolations := rb.checkThresholdViolations(clusterState)
	decision.Factors["threshold_violations"] = float64(thresholdViolations)

	// Determine urgency
	decision.Urgency = rb.determineUrgency(decision.Factors)

	// Make decision based on strategy
	switch rb.strategy {
	case LoadBasedStrategy:
		decision.ShouldRebalance = loadImbalance > rb.config.ImbalanceThreshold
		decision.Reason = fmt.Sprintf("Load imbalance: %.2f", loadImbalance)
	case RebalanceResourceBasedStrategy:
		decision.ShouldRebalance = resourceUtilization > rb.config.LoadThreshold
		decision.Reason = fmt.Sprintf("Resource utilization: %.2f", resourceUtilization)
	case HybridStrategy:
		decision.ShouldRebalance = loadImbalance > rb.config.ImbalanceThreshold || resourceUtilization > rb.config.LoadThreshold
		decision.Reason = fmt.Sprintf("Hybrid check: load=%.2f, resource=%.2f", loadImbalance, resourceUtilization)
	case RebalanceAdaptiveStrategy:
		decision.ShouldRebalance = rb.makeAdaptiveDecision(decision.Factors)
		decision.Reason = "Adaptive strategy decision"
	default:
		decision.ShouldRebalance = loadImbalance > rb.config.ImbalanceThreshold
		decision.Reason = fmt.Sprintf("Default load check: %.2f", loadImbalance)
	}

	// Calculate impact and risk scores
	decision.ImpactScore = rb.calculateImpactScore(decision.Factors)
	decision.RiskScore = rb.calculateRiskScore(clusterState)
	decision.Confidence = rb.calculateConfidence(decision.Factors)

	// Estimate expected moves
	if decision.ShouldRebalance {
		decision.ExpectedMoves = rb.estimateExpectedMoves(clusterState)
	}

	return decision
}

// generateRebalancePlan generates a rebalancing plan
func (rb *Rebalancer) generateRebalancePlan(assignments map[string]string, nodeLoads map[string]*NodeLoad, decision *RebalanceDecision) ([]*JobMove, error) {
	clusterState := rb.captureClusterState(assignments, nodeLoads)

	switch rb.strategy {
	case LoadBasedStrategy:
		return rb.generateLoadBasedPlan(clusterState, assignments)
	case RebalanceResourceBasedStrategy:
		return rb.generateResourceBasedPlan(clusterState, assignments)
	case HybridStrategy:
		return rb.generateHybridPlan(clusterState, assignments)
	case RebalanceAdaptiveStrategy:
		return rb.generateAdaptivePlan(clusterState, assignments, decision)
	default:
		return rb.generateLoadBasedPlan(clusterState, assignments)
	}
}

// generateLoadBasedPlan generates a load-based rebalancing plan
func (rb *Rebalancer) generateLoadBasedPlan(clusterState *ClusterState, assignments map[string]string) ([]*JobMove, error) {
	moves := make([]*JobMove, 0)

	// Sort nodes by load
	nodesByLoad := rb.sortNodesByLoad(clusterState.NodeStates)

	// Find overloaded and underloaded nodes
	overloadedNodes := make([]*NodeState, 0)
	underloadedNodes := make([]*NodeState, 0)

	for _, node := range nodesByLoad {
		if node.UtilizationRate > rb.config.LoadThreshold {
			overloadedNodes = append(overloadedNodes, node)
		} else if node.UtilizationRate < rb.config.LoadThreshold*0.6 {
			underloadedNodes = append(underloadedNodes, node)
		}
	}

	// Generate moves from overloaded to underloaded nodes
	moveCount := 0
	for _, overloadedNode := range overloadedNodes {
		if moveCount >= rb.config.MaxJobsToMove {
			break
		}

		jobsToMove := rb.selectJobsToMove(overloadedNode, assignments)

		for _, jobID := range jobsToMove {
			if moveCount >= rb.config.MaxJobsToMove {
				break
			}

			// Find best target node
			targetNode := rb.findBestTargetNode(underloadedNodes, jobID)
			if targetNode != nil {
				moves = append(moves, &JobMove{
					JobID:    jobID,
					FromNode: overloadedNode.NodeID,
					ToNode:   targetNode.NodeID,
					Reason:   "Load balancing",
					Priority: 1,
					MoveTime: time.Now(),
				})
				moveCount++
			}
		}
	}

	return moves, nil
}

// executeRebalancePlan executes a rebalancing plan
func (rb *Rebalancer) executeRebalancePlan(assignments map[string]string, plan []*JobMove) (map[string]string, []*JobMove, error) {
	if rb.config.DryRunMode {
		// In dry run mode, just return the plan without executing
		for _, move := range plan {
			move.Success = true
			move.Duration = time.Millisecond // Simulate instant execution
		}
		return assignments, plan, nil
	}

	newAssignments := make(map[string]string)
	for k, v := range assignments {
		newAssignments[k] = v
	}

	executedMoves := make([]*JobMove, 0)

	// Execute moves in batches
	for i := 0; i < len(plan); i += rb.config.BatchSize {
		end := i + rb.config.BatchSize
		if end > len(plan) {
			end = len(plan)
		}

		batch := plan[i:end]

		// Execute batch
		for _, move := range batch {
			startTime := time.Now()

			// Simulate job movement (in real implementation, this would involve actual job migration)
			if rb.simulateJobMove(move) {
				newAssignments[move.JobID] = move.ToNode
				move.Success = true
				move.Duration = time.Since(startTime)
				executedMoves = append(executedMoves, move)
			} else {
				move.Success = false
				move.Error = "Job move failed"
				move.Duration = time.Since(startTime)
				executedMoves = append(executedMoves, move)
			}
		}

		// Add delay between batches
		time.Sleep(100 * time.Millisecond)
	}

	return newAssignments, executedMoves, nil
}

// Helper functions

func (rb *Rebalancer) captureClusterState(assignments map[string]string, nodeLoads map[string]*NodeLoad) *ClusterState {
	state := &ClusterState{
		Timestamp:  time.Now(),
		NodeStates: make(map[string]*NodeState),
		TotalJobs:  len(assignments),
	}

	// Count jobs per node
	jobCounts := make(map[string]int)
	for _, nodeID := range assignments {
		jobCounts[nodeID]++
	}

	// Build node states
	totalLoad := 0.0
	var loadValues []float64

	for nodeID, load := range nodeLoads {
		jobCount := jobCounts[nodeID]
		loadScore := rb.calculateNodeLoadScore(load)

		nodeState := &NodeState{
			NodeID:       nodeID,
			JobCount:     jobCount,
			LoadScore:    loadScore,
			CPUUsage:     load.CPUUsage,
			MemoryUsage:  load.MemoryUsage,
			NetworkUsage: load.NetworkUsage,
			Capacity:     load.MaxConcurrentJobs,
			Weight:       load.DistributionWeight,
			HealthStatus: load.HealthStatus,
		}

		if nodeState.Capacity > 0 {
			nodeState.UtilizationRate = float64(jobCount) / float64(nodeState.Capacity)
		}

		state.NodeStates[nodeID] = nodeState
		totalLoad += loadScore
		loadValues = append(loadValues, loadScore)

		if load.HealthStatus == "healthy" {
			state.HealthyNodes++
		} else {
			state.UnhealthyNodes++
		}
	}

	state.TotalNodes = len(nodeLoads)

	if len(loadValues) > 0 {
		state.AverageLoad = totalLoad / float64(len(loadValues))
		state.MaxLoad = rb.maxFloat64(loadValues)
		state.MinLoad = rb.minFloat64(loadValues)
		state.LoadVariance = rb.calculateVariance(loadValues)
		state.ImbalanceScore = rb.calculateImbalanceScore(loadValues)
	}

	return state
}

func (rb *Rebalancer) calculateNodeLoadScore(load *NodeLoad) float64 {
	weights := rb.config.ResourceWeights
	score := 0.0

	score += load.CPUUsage * weights.CPU
	score += load.MemoryUsage * weights.Memory
	score += load.NetworkUsage * weights.Network
	score += float64(load.RunningJobs) * weights.Jobs

	return score
}

func (rb *Rebalancer) calculateLoadImbalance(state *ClusterState) float64 {
	if state.AverageLoad == 0 {
		return 0
	}

	return (state.MaxLoad - state.MinLoad) / state.AverageLoad
}

func (rb *Rebalancer) calculateResourceUtilization(state *ClusterState) float64 {
	if len(state.NodeStates) == 0 {
		return 0
	}

	totalUtilization := 0.0
	for _, node := range state.NodeStates {
		totalUtilization += node.UtilizationRate
	}

	return totalUtilization / float64(len(state.NodeStates))
}

func (rb *Rebalancer) checkThresholdViolations(state *ClusterState) int {
	violations := 0

	for _, node := range state.NodeStates {
		if node.UtilizationRate > rb.config.LoadThreshold {
			violations++
		}
	}

	return violations
}

func (rb *Rebalancer) determineUrgency(factors map[string]float64) RebalanceUrgency {
	loadImbalance := factors["load_imbalance"]
	resourceUtilization := factors["resource_utilization"]
	violations := factors["threshold_violations"]

	if violations > 0 && (loadImbalance > 0.8 || resourceUtilization > 0.9) {
		return UrgencyCritical
	} else if violations > 0 || loadImbalance > 0.6 || resourceUtilization > 0.8 {
		return UrgencyHigh
	} else if loadImbalance > 0.4 || resourceUtilization > 0.7 {
		return UrgencyMedium
	}

	return UrgencyLow
}

func (rb *Rebalancer) makeAdaptiveDecision(factors map[string]float64) bool {
	// Adaptive decision based on multiple factors
	score := 0.0

	score += factors["load_imbalance"] * 0.4
	score += factors["resource_utilization"] * 0.3
	score += factors["threshold_violations"] * 0.3

	return score > 0.5
}

func (rb *Rebalancer) calculateImpactScore(factors map[string]float64) float64 {
	// Calculate expected impact of rebalancing
	return (factors["load_imbalance"] + factors["resource_utilization"]) / 2
}

func (rb *Rebalancer) calculateRiskScore(state *ClusterState) float64 {
	// Calculate risk of performing rebalancing
	risk := 0.0

	// Higher risk if cluster is already stressed
	if state.HealthyNodes < state.TotalNodes {
		risk += 0.3
	}

	// Higher risk if recent rebalancing occurred
	if time.Since(rb.lastRebalance) < rb.config.CooldownPeriod {
		risk += 0.4
	}

	return risk
}

func (rb *Rebalancer) calculateConfidence(factors map[string]float64) float64 {
	// Calculate confidence in the decision
	confidence := 1.0

	// Lower confidence if data is inconsistent
	if factors["load_imbalance"] > 0.8 && factors["resource_utilization"] < 0.3 {
		confidence *= 0.7
	}

	return confidence
}

func (rb *Rebalancer) estimateExpectedMoves(state *ClusterState) int {
	// Estimate number of moves needed
	overloadedNodes := 0
	for _, node := range state.NodeStates {
		if node.UtilizationRate > rb.config.LoadThreshold {
			overloadedNodes++
		}
	}

	return overloadedNodes * 2 // Rough estimate
}

func (rb *Rebalancer) generateResourceBasedPlan(state *ClusterState, assignments map[string]string) ([]*JobMove, error) {
	// Similar to load-based but considers resource usage
	return rb.generateLoadBasedPlan(state, assignments)
}

func (rb *Rebalancer) generateHybridPlan(state *ClusterState, assignments map[string]string) ([]*JobMove, error) {
	// Combines load and resource-based strategies
	return rb.generateLoadBasedPlan(state, assignments)
}

func (rb *Rebalancer) generateAdaptivePlan(state *ClusterState, assignments map[string]string, decision *RebalanceDecision) ([]*JobMove, error) {
	// Adapts strategy based on current conditions
	return rb.generateLoadBasedPlan(state, assignments)
}

func (rb *Rebalancer) sortNodesByLoad(nodeStates map[string]*NodeState) []*NodeState {
	nodes := make([]*NodeState, 0, len(nodeStates))
	for _, node := range nodeStates {
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].LoadScore > nodes[j].LoadScore
	})

	return nodes
}

func (rb *Rebalancer) selectJobsToMove(node *NodeState, assignments map[string]string) []string {
	jobs := make([]string, 0)

	// Find jobs assigned to this node
	for jobID, nodeID := range assignments {
		if nodeID == node.NodeID && len(jobs) < rb.config.MaxJobsToMove {
			jobs = append(jobs, jobID)
		}
	}

	return jobs
}

func (rb *Rebalancer) findBestTargetNode(underloadedNodes []*NodeState, jobID string) *NodeState {
	if len(underloadedNodes) == 0 {
		return nil
	}

	// Return node with lowest load
	return underloadedNodes[0]
}

func (rb *Rebalancer) simulateJobMove(move *JobMove) bool {
	// Simulate job movement (always succeeds in simulation)
	return true
}

func (rb *Rebalancer) calculateImprovement(beforeState, afterState *ClusterState) float64 {
	if beforeState.ImbalanceScore == 0 {
		return 0
	}

	return (beforeState.ImbalanceScore - afterState.ImbalanceScore) / beforeState.ImbalanceScore
}

func (rb *Rebalancer) extractNodesInvolved(moves []*JobMove) []string {
	nodeSet := make(map[string]bool)
	for _, move := range moves {
		nodeSet[move.FromNode] = true
		nodeSet[move.ToNode] = true
	}

	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	return nodes
}

func (rb *Rebalancer) recordEvent(event *RebalanceEvent) {
	rb.history = append(rb.history, event)

	// Keep only recent events
	if len(rb.history) > 100 {
		rb.history = rb.history[len(rb.history)-100:]
	}

	// Update analytics
	rb.analytics.TotalRebalances++
	rb.analytics.LastRebalanceTime = event.StartTime
	rb.analytics.RebalanceHistory = append(rb.analytics.RebalanceHistory, event)

	if event.Success {
		rb.analytics.SuccessfulMoves += int64(len(event.Moves))
		rb.analytics.TotalJobsMoved += int64(len(event.Moves))
	} else {
		rb.analytics.FailedMoves += int64(len(event.Moves))
	}
}

func (rb *Rebalancer) generateEventID() string {
	return fmt.Sprintf("rebalance_%d", time.Now().UnixNano())
}

func (rb *Rebalancer) analyticsLoop(ctx context.Context) {
	defer rb.wg.Done()

	if !rb.config.AnalyticsEnabled {
		return
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-rb.stopChannel:
			return
		case <-ticker.C:
			rb.updateAnalytics()
		case <-ctx.Done():
			return
		}
	}
}

func (rb *Rebalancer) thresholdMonitorLoop(ctx context.Context) {
	defer rb.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rb.stopChannel:
			return
		case <-ticker.C:
			rb.monitorThresholds()
		case <-ctx.Done():
			return
		}
	}
}

func (rb *Rebalancer) updateAnalytics() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Update analytics calculations
	if rb.analytics.TotalRebalances > 0 {
		rb.analytics.AverageMovesPerRebalance = float64(rb.analytics.TotalJobsMoved) / float64(rb.analytics.TotalRebalances)
	}

	// Calculate efficiency
	if rb.analytics.TotalJobsMoved > 0 {
		rb.analytics.RebalanceEfficiency = float64(rb.analytics.SuccessfulMoves) / float64(rb.analytics.TotalJobsMoved)
	}
}

func (rb *Rebalancer) monitorThresholds() {
	// Monitor threshold violations for flapping detection
	rb.thresholdTracker.mu.Lock()
	defer rb.thresholdTracker.mu.Unlock()

	// Clean up old violations
	now := time.Now()
	for nodeID, lastViolation := range rb.thresholdTracker.lastViolation {
		if now.Sub(lastViolation) > rb.thresholdTracker.stabilityWindow {
			delete(rb.thresholdTracker.violations, nodeID)
			delete(rb.thresholdTracker.lastViolation, nodeID)
		}
	}
}

// Utility functions

func (rb *Rebalancer) maxFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

func (rb *Rebalancer) minFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func (rb *Rebalancer) calculateVariance(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))

	variance := 0.0
	for _, v := range values {
		variance += math.Pow(v-mean, 2)
	}
	variance /= float64(len(values))

	return variance
}

func (rb *Rebalancer) calculateImbalanceScore(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	max := rb.maxFloat64(values)
	min := rb.minFloat64(values)

	if max == 0 {
		return 0
	}

	return (max - min) / max
}

func validateRebalancerConfig(config *RebalancerConfig) error {
	if config.RebalanceInterval <= 0 {
		return common.ErrValidationError("rebalance_interval", fmt.Errorf("rebalance interval must be positive"))
	}

	if config.LoadThreshold <= 0 || config.LoadThreshold > 1 {
		return common.ErrValidationError("load_threshold", fmt.Errorf("load threshold must be between 0 and 1"))
	}

	if config.ImbalanceThreshold <= 0 || config.ImbalanceThreshold > 1 {
		return common.ErrValidationError("imbalance_threshold", fmt.Errorf("imbalance threshold must be between 0 and 1"))
	}

	if config.MaxJobsToMove <= 0 {
		return common.ErrValidationError("max_jobs_to_move", fmt.Errorf("max jobs to move must be positive"))
	}

	if config.CooldownPeriod <= 0 {
		return common.ErrValidationError("cooldown_period", fmt.Errorf("cooldown period must be positive"))
	}

	return nil
}
