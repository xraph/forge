package ai

import (
	"context"
	"fmt"
	"sync"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/ai/internal"
)

// AgentTeam represents a group of agents working together
type AgentTeam struct {
	id      string
	name    string
	agents  []internal.AIAgent
	logger  forge.Logger
	mu      sync.RWMutex
}

// NewAgentTeam creates a new agent team
func NewAgentTeam(id, name string, logger forge.Logger) *AgentTeam {
	return &AgentTeam{
		id:     id,
		name:   name,
		agents: make([]internal.AIAgent, 0),
		logger: logger,
	}
}

// ID returns the team ID
func (t *AgentTeam) ID() string {
	return t.id
}

// Name returns the team name
func (t *AgentTeam) Name() string {
	return t.name
}

// AddAgent adds an agent to the team
func (t *AgentTeam) AddAgent(agent internal.AIAgent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.agents = append(t.agents, agent)

	if t.logger != nil {
		t.logger.Info("agent added to team",
			forge.F("team_id", t.id),
			forge.F("agent_id", agent.ID()),
		)
	}
}

// RemoveAgent removes an agent from the team
func (t *AgentTeam) RemoveAgent(agentID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, agent := range t.agents {
		if agent.ID() == agentID {
			t.agents = append(t.agents[:i], t.agents[i+1:]...)
			if t.logger != nil {
				t.logger.Info("agent removed from team",
					forge.F("team_id", t.id),
					forge.F("agent_id", agentID),
				)
			}
			return nil
		}
	}

	return fmt.Errorf("agent %s not found in team", agentID)
}

// Agents returns all agents in the team
func (t *AgentTeam) Agents() []internal.AIAgent {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.agents
}

// Execute executes a task sequentially across all agents
// Each agent receives the output of the previous agent as input
func (t *AgentTeam) Execute(ctx context.Context, input internal.AgentInput) (internal.AgentOutput, error) {
	t.mu.RLock()
	agents := make([]internal.AIAgent, len(t.agents))
	copy(agents, t.agents)
	t.mu.RUnlock()

	if len(agents) == 0 {
		return internal.AgentOutput{}, fmt.Errorf("team %s has no agents", t.id)
	}

	if t.logger != nil {
		t.logger.Info("executing team task (sequential)",
			forge.F("team_id", t.id),
			forge.F("team_name", t.name),
			forge.F("agent_count", len(agents)),
			forge.F("task_type", input.Type),
		)
	}

	// Sequential execution with output passing
	var output internal.AgentOutput
	currentInput := input

	for i, agent := range agents {
		// Pass previous agent's output as input (except for first agent)
		if i > 0 && output.Data != nil {
			currentInput.Data = output.Data
			if currentInput.Context == nil {
				currentInput.Context = make(map[string]interface{})
			}
			currentInput.Context["previous_agent"] = agents[i-1].ID()
			currentInput.Context["previous_output"] = output.Data
		}

		agentOutput, err := agent.Process(ctx, currentInput)
		if err != nil {
			return internal.AgentOutput{}, fmt.Errorf("agent %s failed: %w", agent.ID(), err)
		}

		output = agentOutput

		if t.logger != nil {
			t.logger.Info("agent completed task",
				forge.F("team_id", t.id),
				forge.F("agent_id", agent.ID()),
				forge.F("agent_name", agent.Name()),
				forge.F("step", i+1),
			)
		}
	}

	return output, nil
}

// ExecuteParallel executes a task in parallel across all agents
// All agents receive the same input and work simultaneously
func (t *AgentTeam) ExecuteParallel(ctx context.Context, input internal.AgentInput) ([]internal.AgentOutput, error) {
	t.mu.RLock()
	agents := make([]internal.AIAgent, len(t.agents))
	copy(agents, t.agents)
	t.mu.RUnlock()

	if len(agents) == 0 {
		return nil, fmt.Errorf("team %s has no agents", t.id)
	}

	if t.logger != nil {
		t.logger.Info("executing team task (parallel)",
			forge.F("team_id", t.id),
			forge.F("team_name", t.name),
			forge.F("agent_count", len(agents)),
			forge.F("task_type", input.Type),
		)
	}

	outputs := make([]internal.AgentOutput, len(agents))
	errors := make([]error, len(agents))

	var wg sync.WaitGroup
	for i, agent := range agents {
		wg.Add(1)
		go func(idx int, a internal.AIAgent) {
			defer wg.Done()
			outputs[idx], errors[idx] = a.Process(ctx, input)

			if t.logger != nil && errors[idx] == nil {
				t.logger.Info("agent completed task",
					forge.F("team_id", t.id),
					forge.F("agent_id", a.ID()),
					forge.F("agent_name", a.Name()),
				)
			}
		}(i, agent)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("agent %s failed: %w", agents[i].ID(), err)
		}
	}

	return outputs, nil
}

// ExecuteCollaborative executes a task with agents collaborating
// Agents share context and can communicate through shared memory
func (t *AgentTeam) ExecuteCollaborative(ctx context.Context, input internal.AgentInput) (internal.AgentOutput, error) {
	t.mu.RLock()
	agents := make([]internal.AIAgent, len(t.agents))
	copy(agents, t.agents)
	t.mu.RUnlock()

	if len(agents) == 0 {
		return internal.AgentOutput{}, fmt.Errorf("team %s has no agents", t.id)
	}

	if t.logger != nil {
		t.logger.Info("executing team task (collaborative)",
			forge.F("team_id", t.id),
			forge.F("team_name", t.name),
			forge.F("agent_count", len(agents)),
			forge.F("task_type", input.Type),
		)
	}

	// Create shared context for communication
	sharedContext := &CollaborationContext{
		SharedMemory: make(map[string]interface{}),
		Messages:     make(chan AgentMessage, 100),
		Results:      make(chan internal.AgentOutput, 1),
		Done:         make(chan struct{}),
	}

	// Initialize shared memory with input context
	if input.Context != nil {
		for k, v := range input.Context {
			sharedContext.SharedMemory[k] = v
		}
	}

	// Start all agents in goroutines
	var wg sync.WaitGroup
	for _, agent := range agents {
		wg.Add(1)
		go t.runCollaborativeAgent(ctx, agent, input, sharedContext, &wg)
	}

	// Wait for completion or timeout
	go func() {
		wg.Wait()
		close(sharedContext.Done)
	}()

	select {
	case result := <-sharedContext.Results:
		return result, nil
	case <-ctx.Done():
		return internal.AgentOutput{}, ctx.Err()
	case <-sharedContext.Done:
		// All agents finished but no result was sent
		return internal.AgentOutput{}, fmt.Errorf("no agent produced a final result")
	}
}

// runCollaborativeAgent runs a single agent in collaborative mode
func (t *AgentTeam) runCollaborativeAgent(
	ctx context.Context,
	agent internal.AIAgent,
	input internal.AgentInput,
	collab *CollaborationContext,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	// Add shared context to input
	if input.Context == nil {
		input.Context = make(map[string]interface{})
	}
	input.Context["shared_memory"] = collab.SharedMemory
	input.Context["collaboration_mode"] = true

	// Execute agent
	output, err := agent.Process(ctx, input)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("agent failed in collaborative mode",
				forge.F("team_id", t.id),
				forge.F("agent_id", agent.ID()),
				forge.F("error", err.Error()),
			)
		}
		return
	}

	// Store result in shared memory
	collab.mu.Lock()
	collab.SharedMemory[agent.ID()] = output.Data
	collab.mu.Unlock()

	// First agent to complete successfully sends the result
	select {
	case collab.Results <- output:
		if t.logger != nil {
			t.logger.Info("agent produced final result",
				forge.F("team_id", t.id),
				forge.F("agent_id", agent.ID()),
			)
		}
	default:
		// Results channel already has a result
	}
}

// CollaborationContext holds shared state for collaborative execution
type CollaborationContext struct {
	SharedMemory map[string]interface{}
	Messages     chan AgentMessage
	Results      chan internal.AgentOutput
	Done         chan struct{}
	mu           sync.RWMutex
}

// AgentMessage represents a message between agents
type AgentMessage struct {
	From    string
	To      string
	Type    string
	Content interface{}
}

