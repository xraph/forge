package coordination

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/logger"
)

// AIAgent interface to break circular dependency.
type AIAgent interface {
	ID() string
	Name() string
	Type() string
	Capabilities() []Capability
	Process(ctx context.Context, input any) (any, error)
	GetStats() any
	GetHealth() any
}

// Capability represents a capability that an agent can perform.
type Capability struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Metadata    map[string]any `json:"metadata"`
}

// CoordinationStrategy defines different coordination strategies.
type CoordinationStrategy string

const (
	CoordinationStrategyHierarchical CoordinationStrategy = "hierarchical" // Top-down coordination
	CoordinationStrategyDistributed  CoordinationStrategy = "distributed"  // Peer-to-peer coordination
	CoordinationStrategyConsensus    CoordinationStrategy = "consensus"    // Consensus-based coordination
	CoordinationStrategyMarket       CoordinationStrategy = "market"       // Market-based coordination
	CoordinationStrategySwarm        CoordinationStrategy = "swarm"        // Swarm intelligence
	CoordinationStrategyFederated    CoordinationStrategy = "federated"    // Federated coordination
	CoordinationStrategyHybrid       CoordinationStrategy = "hybrid"       // Hybrid approach
)

// CoordinationObjective defines what the coordination is trying to achieve.
type CoordinationObjective string

const (
	CoordinationObjectiveOptimization   CoordinationObjective = "optimization"    // Performance optimization
	CoordinationObjectiveLoadBalance    CoordinationObjective = "load_balance"    // Load balancing
	CoordinationObjectiveResourceAlloc  CoordinationObjective = "resource_alloc"  // Resource allocation
	CoordinationObjectiveThreatResponse CoordinationObjective = "threat_response" // Security threat response
	CoordinationObjectiveFailover       CoordinationObjective = "failover"        // Failover coordination
	CoordinationObjectiveScaling        CoordinationObjective = "scaling"         // Auto-scaling
	CoordinationObjectiveHealthManage   CoordinationObjective = "health_manage"   // Health management
)

// CoordinationPlan represents a coordination plan.
type CoordinationPlan struct {
	ID           string                   `json:"id"`
	Objective    CoordinationObjective    `json:"objective"`
	Strategy     CoordinationStrategy     `json:"strategy"`
	Participants []string                 `json:"participants"`
	Actions      []CoordinationAction     `json:"actions"`
	Timeline     []CoordinationPhase      `json:"timeline"`
	Constraints  []CoordinationConstraint `json:"constraints"`
	Success      SuccessMetrics           `json:"success_metrics"`
	Status       PlanStatus               `json:"status"`
	CreatedAt    time.Time                `json:"created_at"`
	StartedAt    time.Time                `json:"started_at"`
	CompletedAt  time.Time                `json:"completed_at"`
	Metadata     map[string]any           `json:"metadata"`
}

// CoordinationAction represents an action within a coordination plan.
type CoordinationAction struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Agent        string         `json:"agent"`
	Description  string         `json:"description"`
	Parameters   map[string]any `json:"parameters"`
	Dependencies []string       `json:"dependencies"`
	Priority     int            `json:"priority"`
	Timeout      time.Duration  `json:"timeout"`
	Retries      int            `json:"retries"`
	Status       ActionStatus   `json:"status"`
	Result       any            `json:"result"`
	StartedAt    time.Time      `json:"started_at"`
	CompletedAt  time.Time      `json:"completed_at"`
	Error        string         `json:"error,omitempty"`
}

// CoordinationPhase represents a phase in the coordination timeline.
type CoordinationPhase struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Actions     []string       `json:"actions"` // Action IDs
	StartTime   time.Time      `json:"start_time"`
	EndTime     time.Time      `json:"end_time"`
	Status      PhaseStatus    `json:"status"`
	Metadata    map[string]any `json:"metadata"`
}

// CoordinationConstraint represents a constraint on the coordination.
type CoordinationConstraint struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Violations  int            `json:"violations"`
}

// SuccessMetrics defines success criteria for coordination.
type SuccessMetrics struct {
	TargetLatency     time.Duration  `json:"target_latency"`
	MinSuccessRate    float64        `json:"min_success_rate"`
	MaxResourceUsage  float64        `json:"max_resource_usage"`
	RequiredConsensus float64        `json:"required_consensus"`
	CustomMetrics     map[string]any `json:"custom_metrics"`
}

// Enum types for statuses.
type PlanStatus string

const (
	PlanStatusDraft     PlanStatus = "draft"
	PlanStatusApproved  PlanStatus = "approved"
	PlanStatusExecuting PlanStatus = "executing"
	PlanStatusCompleted PlanStatus = "completed"
	PlanStatusFailed    PlanStatus = "failed"
	PlanStatusCancelled PlanStatus = "cancelled"
)

type ActionStatus string

const (
	ActionStatusPending   ActionStatus = "pending"
	ActionStatusExecuting ActionStatus = "executing"
	ActionStatusCompleted ActionStatus = "completed"
	ActionStatusFailed    ActionStatus = "failed"
	ActionStatusSkipped   ActionStatus = "skipped"
)

type PhaseStatus string

const (
	PhaseStatusPending   PhaseStatus = "pending"
	PhaseStatusExecuting PhaseStatus = "executing"
	PhaseStatusCompleted PhaseStatus = "completed"
	PhaseStatusFailed    PhaseStatus = "failed"
)

// CoordinationExecutor executes coordination plans using different strategies.
type CoordinationExecutor struct {
	strategy     CoordinationStrategy
	communicator *CommunicationManager
	consensus    *ConsensusManager
	agents       map[string]AIAgent
	activePlans  map[string]*CoordinationPlan
	planHistory  []*CoordinationPlan
	strategies   map[CoordinationStrategy]StrategyHandler
	logger       logger.Logger
	metrics      forge.Metrics
	mu           sync.RWMutex
}

// StrategyHandler defines the interface for coordination strategy implementations.
type StrategyHandler interface {
	Name() string
	Execute(ctx context.Context, plan *CoordinationPlan, executor *CoordinationExecutor) error
	ValidatePlan(plan *CoordinationPlan) error
	EstimateDuration(plan *CoordinationPlan) time.Duration
	CalculateResourceRequirements(plan *CoordinationPlan) ResourceRequirements
}

// ResourceRequirements represents the resources needed for coordination.
type ResourceRequirements struct {
	CPUCores    float64       `json:"cpu_cores"`
	MemoryMB    int64         `json:"memory_mb"`
	NetworkMBps float64       `json:"network_mbps"`
	Agents      int           `json:"agents"`
	Duration    time.Duration `json:"duration"`
}

// NewCoordinationExecutor creates a new coordination executor.
func NewCoordinationExecutor(
	strategy CoordinationStrategy,
	communicator *CommunicationManager,
	consensus *ConsensusManager,
	logger logger.Logger,
	metrics forge.Metrics,
) *CoordinationExecutor {
	executor := &CoordinationExecutor{
		strategy:     strategy,
		communicator: communicator,
		consensus:    consensus,
		agents:       make(map[string]AIAgent),
		activePlans:  make(map[string]*CoordinationPlan),
		planHistory:  make([]*CoordinationPlan, 0),
		strategies:   make(map[CoordinationStrategy]StrategyHandler),
		logger:       logger,
		metrics:      metrics,
	}

	// Register strategy handlers
	executor.registerStrategies()

	return executor
}

// registerStrategies registers all available coordination strategies.
func (ce *CoordinationExecutor) registerStrategies() {
	ce.strategies[CoordinationStrategyHierarchical] = &HierarchicalStrategy{}
	ce.strategies[CoordinationStrategyDistributed] = &DistributedStrategy{}
	ce.strategies[CoordinationStrategyConsensus] = &ConsensusStrategy{consensus: ce.consensus}
	ce.strategies[CoordinationStrategyMarket] = &MarketStrategy{}
	ce.strategies[CoordinationStrategySwarm] = &SwarmStrategy{}
	ce.strategies[CoordinationStrategyFederated] = &FederatedStrategy{}
	ce.strategies[CoordinationStrategyHybrid] = &HybridStrategy{}
}

// RegisterAgent registers an agent for coordination.
func (ce *CoordinationExecutor) RegisterAgent(agent AIAgent) error {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	ce.agents[agent.ID()] = agent

	if ce.logger != nil {
		ce.logger.Info("agent registered for coordination",
			logger.String("agent_id", agent.ID()),
			logger.String("agent_type", string(agent.Type())),
		)
	}

	return nil
}

// CreatePlan creates a new coordination plan.
func (ce *CoordinationExecutor) CreatePlan(objective CoordinationObjective, participants []string) (*CoordinationPlan, error) {
	plan := &CoordinationPlan{
		ID:           ce.generatePlanID(),
		Objective:    objective,
		Strategy:     ce.strategy,
		Participants: participants,
		Actions:      make([]CoordinationAction, 0),
		Timeline:     make([]CoordinationPhase, 0),
		Constraints:  make([]CoordinationConstraint, 0),
		Success:      SuccessMetrics{},
		Status:       PlanStatusDraft,
		CreatedAt:    time.Now(),
		Metadata:     make(map[string]any),
	}

	// Generate plan based on objective and strategy
	if err := ce.generatePlan(plan); err != nil {
		return nil, fmt.Errorf("failed to generate coordination plan: %w", err)
	}

	return plan, nil
}

// ExecutePlan executes a coordination plan.
func (ce *CoordinationExecutor) ExecutePlan(ctx context.Context, plan *CoordinationPlan) error {
	ce.mu.Lock()

	if _, exists := ce.activePlans[plan.ID]; exists {
		ce.mu.Unlock()

		return fmt.Errorf("plan %s is already executing", plan.ID)
	}

	ce.activePlans[plan.ID] = plan
	ce.mu.Unlock()

	defer func() {
		ce.mu.Lock()
		delete(ce.activePlans, plan.ID)
		ce.planHistory = append(ce.planHistory, plan)
		ce.mu.Unlock()
	}()

	// Validate plan
	strategy, exists := ce.strategies[plan.Strategy]
	if !exists {
		return fmt.Errorf("unknown coordination strategy: %s", plan.Strategy)
	}

	if err := strategy.ValidatePlan(plan); err != nil {
		plan.Status = PlanStatusFailed

		return fmt.Errorf("plan validation failed: %w", err)
	}

	plan.Status = PlanStatusExecuting
	plan.StartedAt = time.Now()

	if ce.logger != nil {
		ce.logger.Info("executing coordination plan",
			logger.String("plan_id", plan.ID),
			logger.String("objective", string(plan.Objective)),
			logger.String("strategy", string(plan.Strategy)),
			logger.Int("participants", len(plan.Participants)),
		)
	}

	// Execute using the specified strategy
	if err := strategy.Execute(ctx, plan, ce); err != nil {
		plan.Status = PlanStatusFailed
		plan.CompletedAt = time.Now()

		if ce.logger != nil {
			ce.logger.Error("coordination plan execution failed",
				logger.String("plan_id", plan.ID),
				logger.Error(err),
			)
		}

		return err
	}

	plan.Status = PlanStatusCompleted
	plan.CompletedAt = time.Now()

	if ce.logger != nil {
		ce.logger.Info("coordination plan completed successfully",
			logger.String("plan_id", plan.ID),
			logger.Duration("execution_time", plan.CompletedAt.Sub(plan.StartedAt)),
		)
	}

	if ce.metrics != nil {
		ce.metrics.Counter("forge.ai.coordination.plans_completed").Inc()
		ce.metrics.Histogram("forge.ai.coordination.plan_duration").Observe(plan.CompletedAt.Sub(plan.StartedAt).Seconds())
	}

	return nil
}

// generatePlan generates a coordination plan based on objective and strategy.
func (ce *CoordinationExecutor) generatePlan(plan *CoordinationPlan) error {
	switch plan.Objective {
	case CoordinationObjectiveOptimization:
		return ce.generateOptimizationPlan(plan)
	case CoordinationObjectiveLoadBalance:
		return ce.generateLoadBalancePlan(plan)
	case CoordinationObjectiveResourceAlloc:
		return ce.generateResourceAllocationPlan(plan)
	case CoordinationObjectiveThreatResponse:
		return ce.generateThreatResponsePlan(plan)
	case CoordinationObjectiveFailover:
		return ce.generateFailoverPlan(plan)
	case CoordinationObjectiveScaling:
		return ce.generateScalingPlan(plan)
	case CoordinationObjectiveHealthManage:
		return ce.generateHealthManagementPlan(plan)
	default:
		return fmt.Errorf("unknown coordination objective: %s", plan.Objective)
	}
}

// Strategy-specific plan generators

func (ce *CoordinationExecutor) generateOptimizationPlan(plan *CoordinationPlan) error {
	// Phase 1: Analyze current performance
	plan.Timeline = append(plan.Timeline, CoordinationPhase{
		ID:          "analysis",
		Name:        "Performance Analysis",
		Description: "Analyze current system performance metrics",
		StartTime:   time.Now(),
		EndTime:     time.Now().Add(2 * time.Minute),
		Status:      PhaseStatusPending,
	})

	// Phase 2: Generate optimization recommendations
	plan.Timeline = append(plan.Timeline, CoordinationPhase{
		ID:          "recommendation",
		Name:        "Optimization Recommendations",
		Description: "Generate optimization recommendations from multiple agents",
		StartTime:   time.Now().Add(2 * time.Minute),
		EndTime:     time.Now().Add(5 * time.Minute),
		Status:      PhaseStatusPending,
	})

	// Phase 3: Apply optimizations
	plan.Timeline = append(plan.Timeline, CoordinationPhase{
		ID:          "application",
		Name:        "Apply Optimizations",
		Description: "Apply approved optimization changes",
		StartTime:   time.Now().Add(5 * time.Minute),
		EndTime:     time.Now().Add(10 * time.Minute),
		Status:      PhaseStatusPending,
	})

	// Generate actions for each participant
	for i, participant := range plan.Participants {
		action := CoordinationAction{
			ID:          fmt.Sprintf("optimize-%s-%d", participant, i),
			Type:        "optimize",
			Agent:       participant,
			Description: "Analyze and optimize assigned components",
			Parameters: map[string]any{
				"scope":       "assigned_components",
				"max_changes": 5,
				"safe_mode":   true,
			},
			Priority: 5,
			Timeout:  5 * time.Minute,
			Retries:  2,
			Status:   ActionStatusPending,
		}
		plan.Actions = append(plan.Actions, action)
	}

	// Success metrics
	plan.Success = SuccessMetrics{
		TargetLatency:    200 * time.Millisecond,
		MinSuccessRate:   0.95,
		MaxResourceUsage: 0.8,
	}

	return nil
}

func (ce *CoordinationExecutor) generateLoadBalancePlan(plan *CoordinationPlan) error {
	// Create actions for load balancing coordination
	for i, participant := range plan.Participants {
		action := CoordinationAction{
			ID:          fmt.Sprintf("balance-%s-%d", participant, i),
			Type:        "balance_load",
			Agent:       participant,
			Description: "Participate in load balancing coordination",
			Parameters: map[string]any{
				"strategy":      "weighted_round_robin",
				"health_check":  true,
				"gradual_shift": true,
			},
			Priority: 7,
			Timeout:  3 * time.Minute,
			Status:   ActionStatusPending,
		}
		plan.Actions = append(plan.Actions, action)
	}

	plan.Success = SuccessMetrics{
		TargetLatency:    100 * time.Millisecond,
		MinSuccessRate:   0.99,
		MaxResourceUsage: 0.7,
	}

	return nil
}

func (ce *CoordinationExecutor) generateResourceAllocationPlan(plan *CoordinationPlan) error {
	// Create resource allocation actions
	for i, participant := range plan.Participants {
		action := CoordinationAction{
			ID:          fmt.Sprintf("allocate-%s-%d", participant, i),
			Type:        "allocate_resources",
			Agent:       participant,
			Description: "Negotiate and allocate system resources",
			Parameters: map[string]any{
				"resource_types": []string{"cpu", "memory", "network"},
				"priority":       "medium",
				"flexible":       true,
			},
			Priority: 6,
			Timeout:  4 * time.Minute,
			Status:   ActionStatusPending,
		}
		plan.Actions = append(plan.Actions, action)
	}

	return nil
}

func (ce *CoordinationExecutor) generateThreatResponsePlan(plan *CoordinationPlan) error {
	// Emergency response coordination
	for i, participant := range plan.Participants {
		action := CoordinationAction{
			ID:          fmt.Sprintf("respond-%s-%d", participant, i),
			Type:        "threat_response",
			Agent:       participant,
			Description: "Coordinate security threat response",
			Parameters: map[string]any{
				"response_mode": "defensive",
				"isolate":       true,
				"alert_level":   "high",
			},
			Priority: 10,
			Timeout:  1 * time.Minute,
			Status:   ActionStatusPending,
		}
		plan.Actions = append(plan.Actions, action)
	}

	plan.Success = SuccessMetrics{
		TargetLatency:     30 * time.Second,
		MinSuccessRate:    1.0,
		RequiredConsensus: 0.8,
	}

	return nil
}

func (ce *CoordinationExecutor) generateFailoverPlan(plan *CoordinationPlan) error {
	// Failover coordination
	for i, participant := range plan.Participants {
		action := CoordinationAction{
			ID:          fmt.Sprintf("failover-%s-%d", participant, i),
			Type:        "failover",
			Agent:       participant,
			Description: "Coordinate service failover",
			Parameters: map[string]any{
				"backup_services": true,
				"data_sync":       true,
				"health_check":    true,
			},
			Priority: 9,
			Timeout:  2 * time.Minute,
			Status:   ActionStatusPending,
		}
		plan.Actions = append(plan.Actions, action)
	}

	return nil
}

func (ce *CoordinationExecutor) generateScalingPlan(plan *CoordinationPlan) error {
	// Auto-scaling coordination
	for i, participant := range plan.Participants {
		action := CoordinationAction{
			ID:          fmt.Sprintf("scale-%s-%d", participant, i),
			Type:        "auto_scale",
			Agent:       participant,
			Description: "Coordinate auto-scaling decisions",
			Parameters: map[string]any{
				"scale_direction": "up",
				"max_instances":   10,
				"gradual":         true,
			},
			Priority: 7,
			Timeout:  3 * time.Minute,
			Status:   ActionStatusPending,
		}
		plan.Actions = append(plan.Actions, action)
	}

	return nil
}

func (ce *CoordinationExecutor) generateHealthManagementPlan(plan *CoordinationPlan) error {
	// Health management coordination
	for i, participant := range plan.Participants {
		action := CoordinationAction{
			ID:          fmt.Sprintf("health-%s-%d", participant, i),
			Type:        "health_management",
			Agent:       participant,
			Description: "Coordinate health monitoring and recovery",
			Parameters: map[string]any{
				"monitoring_level": "detailed",
				"auto_recovery":    true,
				"escalation":       true,
			},
			Priority: 6,
			Timeout:  4 * time.Minute,
			Status:   ActionStatusPending,
		}
		plan.Actions = append(plan.Actions, action)
	}

	return nil
}

func (ce *CoordinationExecutor) generatePlanID() string {
	return fmt.Sprintf("plan-%d", time.Now().UnixNano())
}

// Strategy Implementations

// HierarchicalStrategy implements top-down hierarchical coordination.
type HierarchicalStrategy struct{}

func (hs *HierarchicalStrategy) Name() string {
	return "hierarchical"
}

func (hs *HierarchicalStrategy) Execute(ctx context.Context, plan *CoordinationPlan, executor *CoordinationExecutor) error {
	// Execute actions in priority order with leader coordination
	actions := make([]CoordinationAction, len(plan.Actions))
	copy(actions, plan.Actions)

	// Sort by priority (higher priority first)
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Priority > actions[j].Priority
	})

	for _, action := range actions {
		if err := executor.executeAction(ctx, &action); err != nil {
			return fmt.Errorf("hierarchical execution failed: %w", err)
		}
	}

	return nil
}

func (hs *HierarchicalStrategy) ValidatePlan(plan *CoordinationPlan) error {
	if len(plan.Participants) < 1 {
		return errors.New("hierarchical strategy requires at least 1 participant")
	}

	return nil
}

func (hs *HierarchicalStrategy) EstimateDuration(plan *CoordinationPlan) time.Duration {
	total := time.Duration(0)
	for _, action := range plan.Actions {
		total += action.Timeout
	}

	return total
}

func (hs *HierarchicalStrategy) CalculateResourceRequirements(plan *CoordinationPlan) ResourceRequirements {
	return ResourceRequirements{
		CPUCores:    float64(len(plan.Actions)) * 0.1,
		MemoryMB:    int64(len(plan.Actions)) * 10,
		NetworkMBps: float64(len(plan.Actions)) * 1.0,
		Agents:      len(plan.Participants),
		Duration:    hs.EstimateDuration(plan),
	}
}

// DistributedStrategy implements peer-to-peer distributed coordination.
type DistributedStrategy struct{}

func (ds *DistributedStrategy) Name() string {
	return "distributed"
}

func (ds *DistributedStrategy) Execute(ctx context.Context, plan *CoordinationPlan, executor *CoordinationExecutor) error {
	// Execute actions in parallel with peer coordination
	actionCh := make(chan CoordinationAction, len(plan.Actions))
	resultCh := make(chan error, len(plan.Actions))

	// Start all actions in parallel
	for _, action := range plan.Actions {
		actionCh <- action
	}

	close(actionCh)

	// Process actions concurrently
	workers := int(math.Min(float64(len(plan.Actions)), 5)) // Max 5 concurrent actions
	for range workers {
		go func() {
			for action := range actionCh {
				err := executor.executeAction(ctx, &action)
				resultCh <- err
			}
		}()
	}

	// Wait for all actions to complete
	var firstError error
	for range len(plan.Actions) {
		if err := <-resultCh; err != nil && firstError == nil {
			firstError = err
		}
	}

	return firstError
}

func (ds *DistributedStrategy) ValidatePlan(plan *CoordinationPlan) error {
	if len(plan.Participants) < 2 {
		return errors.New("distributed strategy requires at least 2 participants")
	}

	return nil
}

func (ds *DistributedStrategy) EstimateDuration(plan *CoordinationPlan) time.Duration {
	maxTimeout := time.Duration(0)
	for _, action := range plan.Actions {
		if action.Timeout > maxTimeout {
			maxTimeout = action.Timeout
		}
	}

	return maxTimeout // Parallel execution
}

func (ds *DistributedStrategy) CalculateResourceRequirements(plan *CoordinationPlan) ResourceRequirements {
	// Higher resource requirements due to parallel execution
	return ResourceRequirements{
		CPUCores:    float64(len(plan.Actions)) * 0.2,
		MemoryMB:    int64(len(plan.Actions)) * 20,
		NetworkMBps: float64(len(plan.Actions)) * 2.0,
		Agents:      len(plan.Participants),
		Duration:    ds.EstimateDuration(plan),
	}
}

// ConsensusStrategy implements consensus-based coordination.
type ConsensusStrategy struct {
	consensus *ConsensusManager
}

func (cs *ConsensusStrategy) Name() string {
	return "consensus"
}

func (cs *ConsensusStrategy) Execute(ctx context.Context, plan *CoordinationPlan, executor *CoordinationExecutor) error {
	// Use consensus for coordinating actions
	decision := &CoordinationDecision{
		Type:         "execute_plan",
		Description:  "Execute coordination plan through consensus",
		Participants: plan.Participants,
		Metadata: map[string]any{
			"plan_id":   plan.ID,
			"objective": plan.Objective,
		},
	}

	if err := cs.consensus.ReachConsensus(ctx, decision); err != nil {
		return fmt.Errorf("consensus failed: %w", err)
	}

	// Execute actions based on consensus result
	for _, action := range plan.Actions {
		if err := executor.executeAction(ctx, &action); err != nil {
			return fmt.Errorf("consensus execution failed: %w", err)
		}
	}

	return nil
}

func (cs *ConsensusStrategy) ValidatePlan(plan *CoordinationPlan) error {
	if len(plan.Participants) < 3 {
		return errors.New("consensus strategy requires at least 3 participants")
	}

	return nil
}

func (cs *ConsensusStrategy) EstimateDuration(plan *CoordinationPlan) time.Duration {
	consensusTime := 30 * time.Second // Estimated consensus time

	executionTime := time.Duration(0)
	for _, action := range plan.Actions {
		executionTime += action.Timeout
	}

	return consensusTime + executionTime
}

func (cs *ConsensusStrategy) CalculateResourceRequirements(plan *CoordinationPlan) ResourceRequirements {
	// Higher requirements due to consensus overhead
	return ResourceRequirements{
		CPUCores:    float64(len(plan.Actions)) * 0.3,
		MemoryMB:    int64(len(plan.Actions)) * 30,
		NetworkMBps: float64(len(plan.Actions)) * 3.0,
		Agents:      len(plan.Participants),
		Duration:    cs.EstimateDuration(plan),
	}
}

// MarketStrategy implements market-based coordination.
type MarketStrategy struct{}

func (ms *MarketStrategy) Name() string {
	return "market"
}

func (ms *MarketStrategy) Execute(ctx context.Context, plan *CoordinationPlan, executor *CoordinationExecutor) error {
	// Implement market-based auction for action assignment
	// For now, simplified implementation
	for _, action := range plan.Actions {
		if err := executor.executeAction(ctx, &action); err != nil {
			return fmt.Errorf("market execution failed: %w", err)
		}
	}

	return nil
}

func (ms *MarketStrategy) ValidatePlan(plan *CoordinationPlan) error {
	return nil // Market strategy is flexible
}

func (ms *MarketStrategy) EstimateDuration(plan *CoordinationPlan) time.Duration {
	return time.Duration(len(plan.Actions)) * 30 * time.Second // Auction + execution time
}

func (ms *MarketStrategy) CalculateResourceRequirements(plan *CoordinationPlan) ResourceRequirements {
	return ResourceRequirements{
		CPUCores:    float64(len(plan.Actions)) * 0.15,
		MemoryMB:    int64(len(plan.Actions)) * 15,
		NetworkMBps: float64(len(plan.Actions)) * 1.5,
		Agents:      len(plan.Participants),
		Duration:    ms.EstimateDuration(plan),
	}
}

// SwarmStrategy implements swarm intelligence coordination.
type SwarmStrategy struct{}

func (ss *SwarmStrategy) Name() string {
	return "swarm"
}

func (ss *SwarmStrategy) Execute(ctx context.Context, plan *CoordinationPlan, executor *CoordinationExecutor) error {
	// Implement emergent behavior coordination
	// Simplified implementation for now
	for _, action := range plan.Actions {
		if err := executor.executeAction(ctx, &action); err != nil {
			return fmt.Errorf("swarm execution failed: %w", err)
		}
	}

	return nil
}

func (ss *SwarmStrategy) ValidatePlan(plan *CoordinationPlan) error {
	if len(plan.Participants) < 4 {
		return errors.New("swarm strategy works best with at least 4 participants")
	}

	return nil
}

func (ss *SwarmStrategy) EstimateDuration(plan *CoordinationPlan) time.Duration {
	// Swarm behavior can be unpredictable
	baseTime := time.Duration(len(plan.Actions)) * 20 * time.Second

	return baseTime + time.Duration(float64(baseTime)*0.2) // Add 20% variance
}

func (ss *SwarmStrategy) CalculateResourceRequirements(plan *CoordinationPlan) ResourceRequirements {
	return ResourceRequirements{
		CPUCores:    float64(len(plan.Actions)) * 0.25,
		MemoryMB:    int64(len(plan.Actions)) * 25,
		NetworkMBps: float64(len(plan.Actions)) * 2.5,
		Agents:      len(plan.Participants),
		Duration:    ss.EstimateDuration(plan),
	}
}

// FederatedStrategy implements federated coordination.
type FederatedStrategy struct{}

func (fs *FederatedStrategy) Name() string {
	return "federated"
}

func (fs *FederatedStrategy) Execute(ctx context.Context, plan *CoordinationPlan, executor *CoordinationExecutor) error {
	// Group agents by type/capability and coordinate in federated manner
	for _, action := range plan.Actions {
		if err := executor.executeAction(ctx, &action); err != nil {
			return fmt.Errorf("federated execution failed: %w", err)
		}
	}

	return nil
}

func (fs *FederatedStrategy) ValidatePlan(plan *CoordinationPlan) error {
	return nil // Federated strategy is flexible
}

func (fs *FederatedStrategy) EstimateDuration(plan *CoordinationPlan) time.Duration {
	return time.Duration(len(plan.Actions)) * 25 * time.Second // Coordination overhead
}

func (fs *FederatedStrategy) CalculateResourceRequirements(plan *CoordinationPlan) ResourceRequirements {
	return ResourceRequirements{
		CPUCores:    float64(len(plan.Actions)) * 0.18,
		MemoryMB:    int64(len(plan.Actions)) * 18,
		NetworkMBps: float64(len(plan.Actions)) * 1.8,
		Agents:      len(plan.Participants),
		Duration:    fs.EstimateDuration(plan),
	}
}

// HybridStrategy combines multiple strategies.
type HybridStrategy struct{}

func (hs *HybridStrategy) Name() string {
	return "hybrid"
}

func (hs *HybridStrategy) Execute(ctx context.Context, plan *CoordinationPlan, executor *CoordinationExecutor) error {
	// Use different strategies for different types of actions
	criticalActions := make([]CoordinationAction, 0)
	normalActions := make([]CoordinationAction, 0)

	for _, action := range plan.Actions {
		if action.Priority >= 8 {
			criticalActions = append(criticalActions, action)
		} else {
			normalActions = append(normalActions, action)
		}
	}

	// Use hierarchical for critical actions
	for _, action := range criticalActions {
		if err := executor.executeAction(ctx, &action); err != nil {
			return fmt.Errorf("hybrid critical execution failed: %w", err)
		}
	}

	// Use distributed for normal actions
	actionCh := make(chan CoordinationAction, len(normalActions))
	resultCh := make(chan error, len(normalActions))

	for _, action := range normalActions {
		actionCh <- action
	}

	close(actionCh)

	workers := int(math.Min(float64(len(normalActions)), 3))
	for range workers {
		go func() {
			for action := range actionCh {
				err := executor.executeAction(ctx, &action)
				resultCh <- err
			}
		}()
	}

	var firstError error
	for range normalActions {
		if err := <-resultCh; err != nil && firstError == nil {
			firstError = err
		}
	}

	return firstError
}

func (hs *HybridStrategy) ValidatePlan(plan *CoordinationPlan) error {
	return nil // Hybrid strategy is very flexible
}

func (hs *HybridStrategy) EstimateDuration(plan *CoordinationPlan) time.Duration {
	criticalTime := time.Duration(0)
	maxNormalTime := time.Duration(0)

	for _, action := range plan.Actions {
		if action.Priority >= 8 {
			criticalTime += action.Timeout
		} else if action.Timeout > maxNormalTime {
			maxNormalTime = action.Timeout
		}
	}

	return criticalTime + maxNormalTime // Sequential + parallel
}

func (hs *HybridStrategy) CalculateResourceRequirements(plan *CoordinationPlan) ResourceRequirements {
	return ResourceRequirements{
		CPUCores:    float64(len(plan.Actions)) * 0.22,
		MemoryMB:    int64(len(plan.Actions)) * 22,
		NetworkMBps: float64(len(plan.Actions)) * 2.2,
		Agents:      len(plan.Participants),
		Duration:    hs.EstimateDuration(plan),
	}
}

// executeAction executes a single coordination action.
func (ce *CoordinationExecutor) executeAction(ctx context.Context, action *CoordinationAction) error {
	// Find the agent for this action
	agent, exists := ce.agents[action.Agent]
	if !exists {
		return fmt.Errorf("agent %s not found", action.Agent)
	}

	// Execute the action through the agent
	action.Status = ActionStatusExecuting
	action.StartedAt = time.Now()

	// Create input for the agent
	input := map[string]any{
		"action_type": action.Type,
		"parameters":  action.Parameters,
		"timeout":     action.Timeout,
	}

	// Process through agent
	_, err := agent.Process(ctx, input)
	if err != nil {
		action.Status = ActionStatusFailed
		action.Error = err.Error()
		action.CompletedAt = time.Now()

		return err
	}

	action.Status = ActionStatusCompleted
	action.CompletedAt = time.Now()

	return nil
}
