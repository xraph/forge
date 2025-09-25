package coordination

import (
	"context"
	"fmt"
	"sync"
	"time"

	ai "github.com/xraph/forge/pkg/ai/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// CoordinationStrategy defines different coordination strategies
type CoordinationStrategy string

const (
	CoordinationStrategyHierarchical CoordinationStrategy = "hierarchical" // Top-down coordination
	CoordinationStrategyDistributed  CoordinationStrategy = "distributed"  // Peer-to-peer coordination
	CoordinationStrategyConsensus    CoordinationStrategy = "consensus"    // Consensus-based coordination
	CoordinationStrategyMarket       CoordinationStrategy = "market"       // Market-based coordination
	CoordinationStrategySwarm        CoordinationStrategy = "swarm"        // Swarm intelligence
)

// MultiAgentCoordinatorConfig contains configuration for multi-agent coordination
type MultiAgentCoordinatorConfig struct {
	Strategy              CoordinationStrategy     `yaml:"strategy" default:"distributed"`
	MaxAgents             int                      `yaml:"max_agents" default:"50"`
	CoordinationInterval  time.Duration            `yaml:"coordination_interval" default:"5s"`
	ConsensusTimeout      time.Duration            `yaml:"consensus_timeout" default:"30s"`
	HealthCheckInterval   time.Duration            `yaml:"health_check_interval" default:"10s"`
	ConflictResolution    ConflictResolutionPolicy `yaml:"conflict_resolution" default:"majority_wins"`
	EnableLoadBalancing   bool                     `yaml:"enable_load_balancing" default:"true"`
	EnableFailover        bool                     `yaml:"enable_failover" default:"true"`
	CommunicationProtocol CommunicationProtocol    `yaml:"communication_protocol" default:"direct"`
	LogLevel              string                   `yaml:"log_level" default:"info"`
}

// ConflictResolutionPolicy defines how conflicts between agents are resolved
type ConflictResolutionPolicy string

const (
	ConflictResolutionMajorityWins ConflictResolutionPolicy = "majority_wins"
	ConflictResolutionHighestRank  ConflictResolutionPolicy = "highest_rank"
	ConflictResolutionConsensus    ConflictResolutionPolicy = "consensus"
	ConflictResolutionArbitration  ConflictResolutionPolicy = "arbitration"
)

// CommunicationProtocol defines how agents communicate
type CommunicationProtocol string

const (
	CommunicationProtocolDirect      CommunicationProtocol = "direct"      // Direct method calls
	CommunicationProtocolMessage     CommunicationProtocol = "message"     // Message passing
	CommunicationProtocolEventBus    CommunicationProtocol = "event_bus"   // Event-based communication
	CommunicationProtocolDistributed CommunicationProtocol = "distributed" // Distributed messaging
)

// AgentRole defines the role of an agent in the coordination
type AgentRole string

const (
	AgentRoleLeader   AgentRole = "leader"   // Coordination leader
	AgentRoleFollower AgentRole = "follower" // Regular agent
	AgentRoleArbiter  AgentRole = "arbiter"  // Conflict resolver
	AgentRoleObserver AgentRole = "observer" // Monitoring only
)

// CoordinatedAgent wraps an AI agent with coordination capabilities
type CoordinatedAgent struct {
	Agent         ai.AIAgent
	ID            string
	Role          AgentRole
	Priority      int
	Load          float64
	Health        AgentHealth
	LastHeartbeat time.Time
	Capabilities  []string
	Dependencies  []string
	Constraints   map[string]interface{}
	Metadata      map[string]interface{}
	Statistics    AgentStatistics
	mu            sync.RWMutex
}

// AgentHealth represents agent health status
type AgentHealth struct {
	Status      string    `json:"status"`
	LastCheck   time.Time `json:"last_check"`
	ErrorCount  int64     `json:"error_count"`
	SuccessRate float64   `json:"success_rate"`
}

// AgentStatistics tracks agent performance
type AgentStatistics struct {
	TasksCompleted    int64         `json:"tasks_completed"`
	TasksFailed       int64         `json:"tasks_failed"`
	AverageLatency    time.Duration `json:"average_latency"`
	ResourceUsage     float64       `json:"resource_usage"`
	CollaborationRate float64       `json:"collaboration_rate"`
	ConflictsResolved int64         `json:"conflicts_resolved"`
}

// CoordinationTask represents a task that requires coordination
type CoordinationTask struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	Priority      int                    `json:"priority"`
	Requirements  []string               `json:"requirements"`
	Constraints   map[string]interface{} `json:"constraints"`
	Deadline      time.Time              `json:"deadline"`
	AssignedAgent string                 `json:"assigned_agent"`
	Status        TaskStatus             `json:"status"`
	Result        interface{}            `json:"result"`
	CreatedAt     time.Time              `json:"created_at"`
	StartedAt     time.Time              `json:"started_at"`
	CompletedAt   time.Time              `json:"completed_at"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// TaskStatus represents the status of a coordination task
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusAssigned   TaskStatus = "assigned"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

// CoordinationDecision represents a decision made through coordination
type CoordinationDecision struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Description  string                 `json:"description"`
	Participants []string               `json:"participants"`
	Votes        map[string]interface{} `json:"votes"`
	Result       interface{}            `json:"result"`
	Confidence   float64                `json:"confidence"`
	Timestamp    time.Time              `json:"timestamp"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// MultiAgentCoordinator manages coordination between multiple AI agents
type MultiAgentCoordinator struct {
	config       MultiAgentCoordinatorConfig
	agents       map[string]*CoordinatedAgent
	tasks        map[string]*CoordinationTask
	decisions    map[string]*CoordinationDecision
	leader       string
	strategy     CoordinationStrategy
	communicator CommunicationManager
	consensus    ConsensusManager
	logger       common.Logger
	metrics      common.Metrics
	started      bool
	mu           sync.RWMutex
}

// CoordinationStats contains statistics about the coordination system
type CoordinationStats struct {
	TotalAgents       int                        `json:"total_agents"`
	ActiveAgents      int                        `json:"active_agents"`
	TasksTotal        int64                      `json:"tasks_total"`
	TasksCompleted    int64                      `json:"tasks_completed"`
	TasksFailed       int64                      `json:"tasks_failed"`
	DecisionsMade     int64                      `json:"decisions_made"`
	ConflictsResolved int64                      `json:"conflicts_resolved"`
	AverageLatency    time.Duration              `json:"average_latency"`
	LeaderChanges     int64                      `json:"leader_changes"`
	AgentStats        map[string]AgentStatistics `json:"agent_stats"`
	LastUpdated       time.Time                  `json:"last_updated"`
}

// NewMultiAgentCoordinator creates a new multi-agent coordinator
func NewMultiAgentCoordinator(config MultiAgentCoordinatorConfig, logger common.Logger, metrics common.Metrics) *MultiAgentCoordinator {
	coordinator := &MultiAgentCoordinator{
		config:    config,
		agents:    make(map[string]*CoordinatedAgent),
		tasks:     make(map[string]*CoordinationTask),
		decisions: make(map[string]*CoordinationDecision),
		strategy:  config.Strategy,
		logger:    logger,
		metrics:   metrics,
	}

	// Initialize communication manager
	coordinator.communicator = NewCommunicationManager(config.CommunicationProtocol, logger)

	// Initialize consensus manager
	coordinator.consensus = NewConsensusManager(config.ConsensusTimeout, logger)

	return coordinator
}

// Start starts the multi-agent coordinator
func (mac *MultiAgentCoordinator) Start(ctx context.Context) error {
	mac.mu.Lock()
	defer mac.mu.Unlock()

	if mac.started {
		return fmt.Errorf("multi-agent coordinator already started")
	}

	// OnStart communication manager
	if err := mac.communicator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start communication manager: %w", err)
	}

	// OnStart consensus manager
	if err := mac.consensus.Start(ctx); err != nil {
		return fmt.Errorf("failed to start consensus manager: %w", err)
	}

	// OnStart coordination routines
	go mac.coordinationLoop(ctx)
	go mac.healthMonitoring(ctx)
	go mac.loadBalancing(ctx)

	mac.started = true

	if mac.logger != nil {
		mac.logger.Info("multi-agent coordinator started",
			logger.String("strategy", string(mac.strategy)),
			logger.Int("max_agents", mac.config.MaxAgents),
			logger.Duration("coordination_interval", mac.config.CoordinationInterval),
		)
	}

	if mac.metrics != nil {
		mac.metrics.Counter("forge.ai.coordinator_started").Inc()
	}

	return nil
}

// Stop stops the multi-agent coordinator
func (mac *MultiAgentCoordinator) Stop(ctx context.Context) error {
	mac.mu.Lock()
	defer mac.mu.Unlock()

	if !mac.started {
		return fmt.Errorf("multi-agent coordinator not started")
	}

	// OnStop all agents
	for _, agent := range mac.agents {
		if err := agent.Agent.Stop(ctx); err != nil {
			if mac.logger != nil {
				mac.logger.Error("failed to stop agent",
					logger.String("agent_id", agent.ID),
					logger.Error(err),
				)
			}
		}
	}

	// OnStop consensus manager
	if err := mac.consensus.Stop(ctx); err != nil {
		if mac.logger != nil {
			mac.logger.Error("failed to stop consensus manager", logger.Error(err))
		}
	}

	// OnStop communication manager
	if err := mac.communicator.Stop(ctx); err != nil {
		if mac.logger != nil {
			mac.logger.Error("failed to stop communication manager", logger.Error(err))
		}
	}

	mac.started = false

	if mac.logger != nil {
		mac.logger.Info("multi-agent coordinator stopped")
	}

	if mac.metrics != nil {
		mac.metrics.Counter("forge.ai.coordinator_stopped").Inc()
	}

	return nil
}

// RegisterAgent registers a new agent with the coordinator
func (mac *MultiAgentCoordinator) RegisterAgent(agent ai.AIAgent) error {
	mac.mu.Lock()
	defer mac.mu.Unlock()

	if !mac.started {
		return fmt.Errorf("coordinator not started")
	}

	agentID := agent.ID()
	if _, exists := mac.agents[agentID]; exists {
		return fmt.Errorf("agent %s already registered", agentID)
	}

	if len(mac.agents) >= mac.config.MaxAgents {
		return fmt.Errorf("maximum number of agents (%d) reached", mac.config.MaxAgents)
	}

	// Create coordinated agent
	coordAgent := &CoordinatedAgent{
		Agent:         agent,
		ID:            agentID,
		Role:          mac.determineAgentRole(agent),
		Priority:      mac.calculateAgentPriority(agent),
		Load:          0.0,
		Health:        AgentHealth{Status: "healthy", LastCheck: time.Now()},
		LastHeartbeat: time.Now(),
		Capabilities:  mac.extractCapabilities(agent),
		Dependencies:  []string{},
		Constraints:   make(map[string]interface{}),
		Metadata:      make(map[string]interface{}),
		Statistics:    AgentStatistics{},
	}

	mac.agents[agentID] = coordAgent

	// Elect leader if this is the first agent or no leader exists
	if mac.leader == "" || mac.strategy == CoordinationStrategyHierarchical {
		mac.electLeader()
	}

	// Register agent with communication manager
	if err := mac.communicator.RegisterAgent(agentID); err != nil {
		if mac.logger != nil {
			mac.logger.Warn("failed to register agent with communicator",
				logger.String("agent_id", agentID),
				logger.Error(err),
			)
		}
	}

	if mac.logger != nil {
		mac.logger.Info("agent registered with coordinator",
			logger.String("agent_id", agentID),
			logger.String("role", string(coordAgent.Role)),
			logger.Int("priority", coordAgent.Priority),
		)
	}

	if mac.metrics != nil {
		mac.metrics.Counter("forge.ai.agents_registered").Inc()
		mac.metrics.Gauge("forge.ai.active_agents").Set(float64(len(mac.agents)))
	}

	return nil
}

// UnregisterAgent removes an agent from the coordinator
func (mac *MultiAgentCoordinator) UnregisterAgent(agentID string) error {
	mac.mu.Lock()
	defer mac.mu.Unlock()

	agent, exists := mac.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	// OnStop the agent
	if err := agent.Agent.Stop(context.Background()); err != nil {
		if mac.logger != nil {
			mac.logger.Error("failed to stop agent during unregistration",
				logger.String("agent_id", agentID),
				logger.Error(err),
			)
		}
	}

	// Reassign tasks if agent was handling any
	mac.reassignAgentTasks(agentID)

	// Elect new leader if this was the leader
	if mac.leader == agentID {
		mac.electLeader()
	}

	// Unregister from communication manager
	if err := mac.communicator.UnregisterAgent(agentID); err != nil {
		if mac.logger != nil {
			mac.logger.Warn("failed to unregister agent from communicator",
				logger.String("agent_id", agentID),
				logger.Error(err),
			)
		}
	}

	delete(mac.agents, agentID)

	if mac.logger != nil {
		mac.logger.Info("agent unregistered from coordinator",
			logger.String("agent_id", agentID),
		)
	}

	if mac.metrics != nil {
		mac.metrics.Counter("forge.ai.agents_unregistered").Inc()
		mac.metrics.Gauge("forge.ai.active_agents").Set(float64(len(mac.agents)))
	}

	return nil
}

// SubmitTask submits a task for coordination
func (mac *MultiAgentCoordinator) SubmitTask(task *CoordinationTask) error {
	mac.mu.Lock()
	defer mac.mu.Unlock()

	if !mac.started {
		return fmt.Errorf("coordinator not started")
	}

	task.ID = mac.generateTaskID()
	task.Status = TaskStatusPending
	task.CreatedAt = time.Now()
	mac.tasks[task.ID] = task

	// Find and assign suitable agent
	if err := mac.assignTask(task); err != nil {
		return fmt.Errorf("failed to assign task: %w", err)
	}

	if mac.logger != nil {
		mac.logger.Info("task submitted for coordination",
			logger.String("task_id", task.ID),
			logger.String("type", task.Type),
			logger.String("assigned_agent", task.AssignedAgent),
		)
	}

	if mac.metrics != nil {
		mac.metrics.Counter("forge.ai.tasks_submitted").Inc()
	}

	return nil
}

// MakeDecision coordinates a decision among agents
func (mac *MultiAgentCoordinator) MakeDecision(ctx context.Context, decision *CoordinationDecision) error {
	if !mac.started {
		return fmt.Errorf("coordinator not started")
	}

	decision.ID = mac.generateDecisionID()
	decision.Timestamp = time.Now()
	decision.Votes = make(map[string]interface{})

	// Get relevant agents for this decision
	participants := mac.getDecisionParticipants(decision)
	decision.Participants = participants

	// Coordinate decision based on strategy
	switch mac.strategy {
	case CoordinationStrategyConsensus:
		return mac.consensusDecision(ctx, decision)
	case CoordinationStrategyHierarchical:
		return mac.hierarchicalDecision(ctx, decision)
	case CoordinationStrategyDistributed:
		return mac.distributedDecision(ctx, decision)
	case CoordinationStrategyMarket:
		return mac.marketDecision(ctx, decision)
	case CoordinationStrategySwarm:
		return mac.swarmDecision(ctx, decision)
	default:
		return fmt.Errorf("unsupported coordination strategy: %s", mac.strategy)
	}
}

// GetStats returns coordination statistics
func (mac *MultiAgentCoordinator) GetStats() CoordinationStats {
	mac.mu.RLock()
	defer mac.mu.RUnlock()

	activeAgents := 0
	agentStats := make(map[string]AgentStatistics)

	for id, agent := range mac.agents {
		agentStats[id] = agent.Statistics
		if agent.Health.Status == "healthy" {
			activeAgents++
		}
	}

	var tasksCompleted, tasksFailed int64
	for _, task := range mac.tasks {
		if task.Status == TaskStatusCompleted {
			tasksCompleted++
		} else if task.Status == TaskStatusFailed {
			tasksFailed++
		}
	}

	return CoordinationStats{
		TotalAgents:    len(mac.agents),
		ActiveAgents:   activeAgents,
		TasksTotal:     int64(len(mac.tasks)),
		TasksCompleted: tasksCompleted,
		TasksFailed:    tasksFailed,
		DecisionsMade:  int64(len(mac.decisions)),
		AgentStats:     agentStats,
		LastUpdated:    time.Now(),
	}
}

// Internal methods

func (mac *MultiAgentCoordinator) coordinationLoop(ctx context.Context) {
	ticker := time.NewTicker(mac.config.CoordinationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mac.performCoordination()
		}
	}
}

func (mac *MultiAgentCoordinator) healthMonitoring(ctx context.Context) {
	ticker := time.NewTicker(mac.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mac.performHealthChecks()
		}
	}
}

func (mac *MultiAgentCoordinator) loadBalancing(ctx context.Context) {
	if !mac.config.EnableLoadBalancing {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mac.balanceLoad()
		}
	}
}

func (mac *MultiAgentCoordinator) performCoordination() {
	mac.mu.Lock()
	defer mac.mu.Unlock()

	// Check for pending tasks
	for _, task := range mac.tasks {
		if task.Status == TaskStatusPending {
			if err := mac.assignTask(task); err != nil {
				if mac.logger != nil {
					mac.logger.Error("failed to assign pending task",
						logger.String("task_id", task.ID),
						logger.Error(err),
					)
				}
			}
		}
	}

	// Check for overloaded agents and rebalance if needed
	mac.checkForRebalancing()
}

func (mac *MultiAgentCoordinator) performHealthChecks() {
	mac.mu.Lock()
	defer mac.mu.Unlock()

	for _, agent := range mac.agents {
		health := agent.Agent.GetHealth()

		agent.mu.Lock()
		agent.Health.Status = string(health.Status)
		agent.Health.LastCheck = time.Now()
		agent.mu.Unlock()

		if health.Status != ai.AgentHealthStatusHealthy {
			if mac.config.EnableFailover {
				mac.handleUnhealthyAgent(agent.ID)
			}
		}
	}
}

func (mac *MultiAgentCoordinator) balanceLoad() {
	mac.mu.Lock()
	defer mac.mu.Unlock()

	// Simple load balancing - move tasks from overloaded agents to underloaded ones
	overloaded := make([]*CoordinatedAgent, 0)
	underloaded := make([]*CoordinatedAgent, 0)

	for _, agent := range mac.agents {
		if agent.Load > 0.8 {
			overloaded = append(overloaded, agent)
		} else if agent.Load < 0.3 {
			underloaded = append(underloaded, agent)
		}
	}

	// Redistribute tasks
	for _, overAgent := range overloaded {
		if len(underloaded) > 0 {
			mac.redistributeTasks(overAgent, underloaded[0])
		}
	}
}

func (mac *MultiAgentCoordinator) determineAgentRole(agent ai.AIAgent) AgentRole {
	// Simple role determination based on agent capabilities and type
	agentType := agent.Type()

	switch agentType {
	case ai.AgentTypeLoadBalancer:
		return AgentRoleLeader
	case ai.AgentTypeOptimization:
		return AgentRoleLeader
	default:
		return AgentRoleFollower
	}
}

func (mac *MultiAgentCoordinator) calculateAgentPriority(agent ai.AIAgent) int {
	// Simple priority calculation based on agent type and capabilities
	baseScore := 10

	switch agent.Type() {
	case ai.AgentTypeLoadBalancer:
		baseScore += 20
	case ai.AgentTypeOptimization:
		baseScore += 15
	case ai.AgentTypeSecurityMonitor:
		baseScore += 25
	case ai.AgentTypeAnomalyDetection:
		baseScore += 20
	}

	return baseScore
}

func (mac *MultiAgentCoordinator) extractCapabilities(agent ai.AIAgent) []string {
	capabilities := make([]string, 0)
	for _, cap := range agent.Capabilities() {
		capabilities = append(capabilities, cap.Name)
	}
	return capabilities
}

func (mac *MultiAgentCoordinator) electLeader() {
	var leader *CoordinatedAgent
	highestPriority := -1

	for _, agent := range mac.agents {
		if agent.Priority > highestPriority && agent.Health.Status == "healthy" {
			highestPriority = agent.Priority
			leader = agent
		}
	}

	if leader != nil {
		oldLeader := mac.leader
		mac.leader = leader.ID
		leader.Role = AgentRoleLeader

		if oldLeader != mac.leader {
			if mac.logger != nil {
				mac.logger.Info("new leader elected",
					logger.String("old_leader", oldLeader),
					logger.String("new_leader", mac.leader),
				)
			}

			if mac.metrics != nil {
				mac.metrics.Counter("forge.ai.leader_changes").Inc()
			}
		}
	}
}

func (mac *MultiAgentCoordinator) assignTask(task *CoordinationTask) error {
	// Find the best agent for this task
	bestAgent := mac.findBestAgentForTask(task)
	if bestAgent == nil {
		return fmt.Errorf("no suitable agent found for task %s", task.ID)
	}

	task.AssignedAgent = bestAgent.ID
	task.Status = TaskStatusAssigned
	task.StartedAt = time.Now()

	// Update agent load
	bestAgent.mu.Lock()
	bestAgent.Load += 0.1 // Simple load calculation
	bestAgent.mu.Unlock()

	return nil
}

func (mac *MultiAgentCoordinator) findBestAgentForTask(task *CoordinationTask) *CoordinatedAgent {
	var bestAgent *CoordinatedAgent
	bestScore := -1.0

	for _, agent := range mac.agents {
		if agent.Health.Status != "healthy" {
			continue
		}

		score := mac.calculateTaskScore(agent, task)
		if score > bestScore {
			bestScore = score
			bestAgent = agent
		}
	}

	return bestAgent
}

func (mac *MultiAgentCoordinator) calculateTaskScore(agent *CoordinatedAgent, task *CoordinationTask) float64 {
	score := float64(agent.Priority)

	// Check capability match
	capabilityMatch := 0
	for _, requirement := range task.Requirements {
		for _, capability := range agent.Capabilities {
			if requirement == capability {
				capabilityMatch++
				break
			}
		}
	}

	if len(task.Requirements) > 0 {
		score += float64(capabilityMatch) / float64(len(task.Requirements)) * 50
	}

	// Penalize high load
	score -= agent.Load * 20

	return score
}

func (mac *MultiAgentCoordinator) reassignAgentTasks(agentID string) {
	// Find tasks assigned to this agent and reassign them
	for _, task := range mac.tasks {
		if task.AssignedAgent == agentID && (task.Status == TaskStatusAssigned || task.Status == TaskStatusInProgress) {
			task.AssignedAgent = ""
			task.Status = TaskStatusPending

			// Try to reassign immediately
			if err := mac.assignTask(task); err != nil {
				if mac.logger != nil {
					mac.logger.Error("failed to reassign task",
						logger.String("task_id", task.ID),
						logger.Error(err),
					)
				}
			}
		}
	}
}

func (mac *MultiAgentCoordinator) handleUnhealthyAgent(agentID string) {
	if mac.logger != nil {
		mac.logger.Warn("handling unhealthy agent",
			logger.String("agent_id", agentID),
		)
	}

	// Reassign tasks from unhealthy agent
	mac.reassignAgentTasks(agentID)

	// Elect new leader if this was the leader
	if mac.leader == agentID {
		mac.electLeader()
	}
}

func (mac *MultiAgentCoordinator) checkForRebalancing() {
	// Simple rebalancing check - implement more sophisticated logic as needed
	totalLoad := 0.0
	agentCount := 0

	for _, agent := range mac.agents {
		if agent.Health.Status == "healthy" {
			totalLoad += agent.Load
			agentCount++
		}
	}

	if agentCount > 0 {
		avgLoad := totalLoad / float64(agentCount)

		// If load is very uneven, trigger rebalancing
		for _, agent := range mac.agents {
			if agent.Load > avgLoad*2 && avgLoad > 0.1 {
				// Find underloaded agents to transfer tasks
				for _, other := range mac.agents {
					if other.Load < avgLoad*0.5 && other.ID != agent.ID {
						mac.redistributeTasks(agent, other)
						break
					}
				}
			}
		}
	}
}

func (mac *MultiAgentCoordinator) redistributeTasks(from, to *CoordinatedAgent) {
	// Simple task redistribution - move one task from overloaded to underloaded agent
	for _, task := range mac.tasks {
		if task.AssignedAgent == from.ID && task.Status == TaskStatusPending {
			task.AssignedAgent = to.ID

			from.mu.Lock()
			from.Load -= 0.1
			from.mu.Unlock()

			to.mu.Lock()
			to.Load += 0.1
			to.mu.Unlock()

			if mac.logger != nil {
				mac.logger.Info("task redistributed",
					logger.String("task_id", task.ID),
					logger.String("from_agent", from.ID),
					logger.String("to_agent", to.ID),
				)
			}

			break // Only redistribute one task at a time
		}
	}
}

func (mac *MultiAgentCoordinator) generateTaskID() string {
	return fmt.Sprintf("task-%d", time.Now().UnixNano())
}

func (mac *MultiAgentCoordinator) generateDecisionID() string {
	return fmt.Sprintf("decision-%d", time.Now().UnixNano())
}

func (mac *MultiAgentCoordinator) getDecisionParticipants(decision *CoordinationDecision) []string {
	participants := make([]string, 0)

	// Include all healthy agents for now - could be more sophisticated
	for id, agent := range mac.agents {
		if agent.Health.Status == "healthy" {
			participants = append(participants, id)
		}
	}

	return participants
}

// Decision-making strategies - simplified implementations
func (mac *MultiAgentCoordinator) consensusDecision(ctx context.Context, decision *CoordinationDecision) error {
	return mac.consensus.ReachConsensus(ctx, decision)
}

func (mac *MultiAgentCoordinator) hierarchicalDecision(ctx context.Context, decision *CoordinationDecision) error {
	// Leader makes the decision
	if mac.leader != "" {
		if leader, exists := mac.agents[mac.leader]; exists {
			// Simple implementation - leader decides
			decision.Result = "approved_by_leader"
			decision.Confidence = 0.9
			mac.decisions[decision.ID] = decision
			return nil
		}
	}
	return fmt.Errorf("no leader available for hierarchical decision")
}

func (mac *MultiAgentCoordinator) distributedDecision(ctx context.Context, decision *CoordinationDecision) error {
	// Each agent votes with equal weight
	votes := make(map[string]interface{})

	for _, participantID := range decision.Participants {
		if agent, exists := mac.agents[participantID]; exists {
			// Simple voting - agents vote based on their type and capabilities
			vote := mac.getAgentVote(agent, decision)
			votes[participantID] = vote
		}
	}

	decision.Votes = votes
	decision.Result = mac.tallyVotes(votes)
	decision.Confidence = 0.7
	mac.decisions[decision.ID] = decision

	return nil
}

func (mac *MultiAgentCoordinator) marketDecision(ctx context.Context, decision *CoordinationDecision) error {
	// Market-based decision making - agents bid on decisions
	decision.Result = "market_based_result"
	decision.Confidence = 0.6
	mac.decisions[decision.ID] = decision
	return nil
}

func (mac *MultiAgentCoordinator) swarmDecision(ctx context.Context, decision *CoordinationDecision) error {
	// Swarm intelligence - emergent decision making
	decision.Result = "swarm_based_result"
	decision.Confidence = 0.8
	mac.decisions[decision.ID] = decision
	return nil
}

func (mac *MultiAgentCoordinator) getAgentVote(agent *CoordinatedAgent, decision *CoordinationDecision) interface{} {
	// Simple voting logic based on agent type
	switch agent.Agent.Type() {
	case ai.AgentTypeOptimization:
		return "optimize"
	case ai.AgentTypeSecurityMonitor:
		return "secure"
	case ai.AgentTypeLoadBalancer:
		return "balance"
	default:
		return "neutral"
	}
}

func (mac *MultiAgentCoordinator) tallyVotes(votes map[string]interface{}) interface{} {
	voteCounts := make(map[string]int)

	for _, vote := range votes {
		if voteStr, ok := vote.(string); ok {
			voteCounts[voteStr]++
		}
	}

	// Return majority vote
	maxVotes := 0
	var result string
	for vote, count := range voteCounts {
		if count > maxVotes {
			maxVotes = count
			result = vote
		}
	}

	return result
}
