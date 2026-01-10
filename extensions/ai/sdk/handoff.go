package sdk

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// AgentHandoff represents a request to hand off execution from one agent to another.
type AgentHandoff struct {
	FromAgentID string         `json:"fromAgentId"`
	ToAgentID   string         `json:"toAgentId"`
	Reason      string         `json:"reason"`
	Input       string         `json:"input"`
	Context     map[string]any `json:"context"`
	Preserve    []string       `json:"preserve"` // What to preserve: "history", "state", "context"
}

// HandoffResult represents the result of an agent handoff.
type HandoffResult struct {
	FromAgentID  string          `json:"fromAgentId"`
	ToAgentID    string          `json:"toAgentId"`
	Response     *AgentResponse  `json:"response"`
	Duration     time.Duration   `json:"duration"`
	HandoffChain []HandoffRecord `json:"handoffChain"`
}

// HandoffRecord tracks a single handoff in a chain.
type HandoffRecord struct {
	FromAgentID string         `json:"fromAgentId"`
	ToAgentID   string         `json:"toAgentId"`
	Reason      string         `json:"reason"`
	Timestamp   time.Time      `json:"timestamp"`
	Duration    time.Duration  `json:"duration"`
	Context     map[string]any `json:"context"`
}

// AgentRegistry manages a collection of agents for routing and handoffs.
type AgentRegistry struct {
	agents    map[string]*Agent
	aliases   map[string]string // alias -> agent ID
	mu        sync.RWMutex
	logger    forge.Logger
	metrics   forge.Metrics
	listeners []AgentRegistryListener
}

// AgentRegistryListener receives events from the agent registry.
type AgentRegistryListener interface {
	OnAgentRegistered(agent *Agent)
	OnAgentUnregistered(agentID string)
}

// AgentInfo provides metadata about a registered agent.
type AgentInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	Model       string   `json:"model"`
	Provider    string   `json:"provider"`
}

// NewAgentRegistry creates a new agent registry.
func NewAgentRegistry(logger forge.Logger, metrics forge.Metrics) *AgentRegistry {
	return &AgentRegistry{
		agents:    make(map[string]*Agent),
		aliases:   make(map[string]string),
		logger:    logger,
		metrics:   metrics,
		listeners: make([]AgentRegistryListener, 0),
	}
}

// Register adds an agent to the registry.
func (r *AgentRegistry) Register(agent *Agent) error {
	if agent == nil {
		return ErrAgentNil
	}

	if agent.ID == "" {
		return ErrAgentIDRequired
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[agent.ID]; exists {
		return fmt.Errorf("%w: %s", ErrAgentAlreadyRegistered, agent.ID)
	}

	r.agents[agent.ID] = agent

	if r.logger != nil {
		r.logger.Info("Agent registered",
			F("agent_id", agent.ID),
			F("name", agent.Name),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.ai.sdk.registry.agents_registered").Inc()
		r.metrics.Gauge("forge.ai.sdk.registry.agents_count").Set(float64(len(r.agents)))
	}

	// Notify listeners
	for _, listener := range r.listeners {
		listener.OnAgentRegistered(agent)
	}

	return nil
}

// RegisterWithAlias adds an agent to the registry with an alias.
func (r *AgentRegistry) RegisterWithAlias(agent *Agent, alias string) error {
	if err := r.Register(agent); err != nil {
		return err
	}

	r.mu.Lock()
	r.aliases[alias] = agent.ID
	r.mu.Unlock()

	return nil
}

// Unregister removes an agent from the registry.
func (r *AgentRegistry) Unregister(agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[agentID]; !exists {
		return fmt.Errorf("%w: %s", ErrAgentNotFound, agentID)
	}

	delete(r.agents, agentID)

	// Remove any aliases pointing to this agent
	for alias, id := range r.aliases {
		if id == agentID {
			delete(r.aliases, alias)
		}
	}

	if r.logger != nil {
		r.logger.Info("Agent unregistered", F("agent_id", agentID))
	}

	if r.metrics != nil {
		r.metrics.Counter("forge.ai.sdk.registry.agents_unregistered").Inc()
		r.metrics.Gauge("forge.ai.sdk.registry.agents_count").Set(float64(len(r.agents)))
	}

	// Notify listeners
	for _, listener := range r.listeners {
		listener.OnAgentUnregistered(agentID)
	}

	return nil
}

// Get retrieves an agent by ID or alias.
func (r *AgentRegistry) Get(idOrAlias string) (*Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try direct ID lookup first
	if agent, exists := r.agents[idOrAlias]; exists {
		return agent, nil
	}

	// Try alias lookup
	if agentID, exists := r.aliases[idOrAlias]; exists {
		if agent, exists := r.agents[agentID]; exists {
			return agent, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrAgentNotFound, idOrAlias)
}

// List returns all registered agents.
func (r *AgentRegistry) List() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agents := make([]*Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		agents = append(agents, agent)
	}

	return agents
}

// ListInfo returns metadata about all registered agents.
func (r *AgentRegistry) ListInfo() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]AgentInfo, 0, len(r.agents))
	for _, agent := range r.agents {
		tools := make([]string, len(agent.tools))
		for i, tool := range agent.tools {
			tools[i] = tool.Name
		}

		infos = append(infos, AgentInfo{
			ID:          agent.ID,
			Name:        agent.Name,
			Description: agent.Description,
			Tools:       tools,
			Model:       agent.Model,
			Provider:    agent.Provider,
		})
	}

	return infos
}

// FindByTool finds agents that have a specific tool.
func (r *AgentRegistry) FindByTool(toolName string) []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Agent, 0)

	for _, agent := range r.agents {
		for _, tool := range agent.tools {
			if tool.Name == toolName {
				result = append(result, agent)

				break
			}
		}
	}

	return result
}

// AddListener adds a listener to receive registry events.
func (r *AgentRegistry) AddListener(listener AgentRegistryListener) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.listeners = append(r.listeners, listener)
}

// AgentRouter determines which agent should handle a request.
type AgentRouter interface {
	// Route determines the best agent for the given input.
	Route(ctx context.Context, input string) (*Agent, error)

	// RouteWithContext routes with additional context.
	RouteWithContext(ctx context.Context, input string, context map[string]any) (*Agent, error)
}

// DefaultAgentRouter provides default routing logic.
type DefaultAgentRouter struct {
	registry   *AgentRegistry
	defaultID  string
	llmManager LLMManager
	logger     forge.Logger
	metrics    forge.Metrics
}

// NewDefaultAgentRouter creates a new default agent router.
func NewDefaultAgentRouter(
	registry *AgentRegistry,
	defaultAgentID string,
	llmManager LLMManager,
	logger forge.Logger,
	metrics forge.Metrics,
) *DefaultAgentRouter {
	return &DefaultAgentRouter{
		registry:   registry,
		defaultID:  defaultAgentID,
		llmManager: llmManager,
		logger:     logger,
		metrics:    metrics,
	}
}

// Route implements AgentRouter.
func (r *DefaultAgentRouter) Route(ctx context.Context, input string) (*Agent, error) {
	return r.RouteWithContext(ctx, input, nil)
}

// RouteWithContext routes with additional context.
func (r *DefaultAgentRouter) RouteWithContext(ctx context.Context, input string, context map[string]any) (*Agent, error) {
	// Get all available agents
	agents := r.registry.List()
	if len(agents) == 0 {
		return nil, ErrNoAgentsAvailable
	}

	// If only one agent, use it
	if len(agents) == 1 {
		return agents[0], nil
	}

	// Try to find the best agent based on tools and description
	// This is a simple keyword-based routing; can be enhanced with LLM-based routing
	bestAgent := r.findBestMatch(input, agents)
	if bestAgent != nil {
		return bestAgent, nil
	}

	// Fall back to default agent
	if r.defaultID != "" {
		agent, err := r.registry.Get(r.defaultID)
		if err == nil {
			return agent, nil
		}
	}

	// Return first available agent
	return agents[0], nil
}

// findBestMatch finds the agent that best matches the input.
func (r *DefaultAgentRouter) findBestMatch(input string, agents []*Agent) *Agent {
	var bestAgent *Agent

	bestScore := 0

	inputLower := strToLower(input)

	for _, agent := range agents {
		score := 0

		// Check if agent name is mentioned
		if strContains(inputLower, strToLower(agent.Name)) {
			score += 10
		}

		// Check if any tool is mentioned
		for _, tool := range agent.tools {
			if strContains(inputLower, strToLower(tool.Name)) {
				score += 5
			}

			if strContains(inputLower, strToLower(tool.Description)) {
				score += 2
			}
		}

		// Check description keywords
		if agent.Description != "" && containsAnyKeyword(inputLower, agent.Description) {
			score += 3
		}

		if score > bestScore {
			bestScore = score
			bestAgent = agent
		}
	}

	if bestScore > 0 {
		return bestAgent
	}

	return nil
}

// HandoffManager handles agent-to-agent handoffs.
type HandoffManager struct {
	registry      *AgentRegistry
	router        AgentRouter
	logger        forge.Logger
	metrics       forge.Metrics
	maxChainDepth int
	callbacks     HandoffCallbacks
}

// HandoffCallbacks provides hooks into handoff execution.
type HandoffCallbacks struct {
	OnHandoffStart    func(handoff *AgentHandoff)
	OnHandoffComplete func(result *HandoffResult)
	OnHandoffError    func(handoff *AgentHandoff, err error)
}

// HandoffManagerOptions configures the handoff manager.
type HandoffManagerOptions struct {
	MaxChainDepth int
	Callbacks     HandoffCallbacks
}

// NewHandoffManager creates a new handoff manager.
func NewHandoffManager(
	registry *AgentRegistry,
	router AgentRouter,
	logger forge.Logger,
	metrics forge.Metrics,
	opts *HandoffManagerOptions,
) *HandoffManager {
	m := &HandoffManager{
		registry:      registry,
		router:        router,
		logger:        logger,
		metrics:       metrics,
		maxChainDepth: 5, // Default max depth
	}

	if opts != nil {
		if opts.MaxChainDepth > 0 {
			m.maxChainDepth = opts.MaxChainDepth
		}

		m.callbacks = opts.Callbacks
	}

	return m
}

// Handoff executes a handoff from one agent to another.
func (m *HandoffManager) Handoff(ctx context.Context, handoff *AgentHandoff) (*HandoffResult, error) {
	startTime := time.Now()

	if m.callbacks.OnHandoffStart != nil {
		m.callbacks.OnHandoffStart(handoff)
	}

	// Get target agent
	toAgent, err := m.registry.Get(handoff.ToAgentID)
	if err != nil {
		if m.callbacks.OnHandoffError != nil {
			m.callbacks.OnHandoffError(handoff, err)
		}

		return nil, fmt.Errorf("failed to get target agent: %w", err)
	}

	// Get source agent for context preservation
	var fromAgent *Agent
	if handoff.FromAgentID != "" {
		fromAgent, _ = m.registry.Get(handoff.FromAgentID)
	}

	// Apply context preservation
	if fromAgent != nil && len(handoff.Preserve) > 0 {
		m.applyPreservation(fromAgent, toAgent, handoff.Preserve)
	}

	// Set handoff context on target agent
	if handoff.Context != nil {
		for k, v := range handoff.Context {
			_ = toAgent.SetStateData(k, v)
		}
	}

	// Execute target agent
	response, err := toAgent.Execute(ctx, handoff.Input)
	if err != nil {
		if m.callbacks.OnHandoffError != nil {
			m.callbacks.OnHandoffError(handoff, err)
		}

		return nil, fmt.Errorf("target agent execution failed: %w", err)
	}

	result := &HandoffResult{
		FromAgentID: handoff.FromAgentID,
		ToAgentID:   handoff.ToAgentID,
		Response:    response,
		Duration:    time.Since(startTime),
		HandoffChain: []HandoffRecord{
			{
				FromAgentID: handoff.FromAgentID,
				ToAgentID:   handoff.ToAgentID,
				Reason:      handoff.Reason,
				Timestamp:   startTime,
				Duration:    time.Since(startTime),
				Context:     handoff.Context,
			},
		},
	}

	if m.callbacks.OnHandoffComplete != nil {
		m.callbacks.OnHandoffComplete(result)
	}

	if m.logger != nil {
		m.logger.Info("Agent handoff completed",
			F("from_agent", handoff.FromAgentID),
			F("to_agent", handoff.ToAgentID),
			F("duration_ms", result.Duration.Milliseconds()),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.sdk.handoff.completed").Inc()
		m.metrics.Histogram("forge.ai.sdk.handoff.duration").Observe(result.Duration.Seconds())
	}

	return result, nil
}

// HandoffToTool finds an agent with a specific tool and hands off to it.
func (m *HandoffManager) HandoffToTool(ctx context.Context, fromAgentID string, toolName string, input string, context map[string]any) (*HandoffResult, error) {
	// Find agents with the required tool
	agents := m.registry.FindByTool(toolName)
	if len(agents) == 0 {
		return nil, fmt.Errorf("%w: no agent has tool %s", ErrToolNotFound, toolName)
	}

	// Use the first available agent with the tool
	targetAgent := agents[0]

	handoff := &AgentHandoff{
		FromAgentID: fromAgentID,
		ToAgentID:   targetAgent.ID,
		Reason:      "Handoff to agent with tool: " + toolName,
		Input:       input,
		Context:     context,
		Preserve:    []string{"context"},
	}

	return m.Handoff(ctx, handoff)
}

// AutoRoute automatically routes input to the best agent.
func (m *HandoffManager) AutoRoute(ctx context.Context, input string, context map[string]any) (*HandoffResult, error) {
	// Use router to find best agent
	agent, err := m.router.RouteWithContext(ctx, input, context)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	// Execute the selected agent
	startTime := time.Now()

	response, err := agent.Execute(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("agent execution failed: %w", err)
	}

	return &HandoffResult{
		FromAgentID: "",
		ToAgentID:   agent.ID,
		Response:    response,
		Duration:    time.Since(startTime),
		HandoffChain: []HandoffRecord{
			{
				FromAgentID: "",
				ToAgentID:   agent.ID,
				Reason:      "Auto-routed",
				Timestamp:   startTime,
				Duration:    time.Since(startTime),
				Context:     context,
			},
		},
	}, nil
}

// ChainedHandoff executes a series of handoffs, passing context along.
func (m *HandoffManager) ChainedHandoff(ctx context.Context, handoffs []*AgentHandoff) (*HandoffResult, error) {
	if len(handoffs) == 0 {
		return nil, ErrEmptyHandoffChain
	}

	if len(handoffs) > m.maxChainDepth {
		return nil, fmt.Errorf("%w: max depth is %d", ErrHandoffChainTooLong, m.maxChainDepth)
	}

	var (
		lastResult   *HandoffResult
		handoffChain = make([]HandoffRecord, 0, len(handoffs))
	)

	startTime := time.Now()

	for i, handoff := range handoffs {
		// Pass context from previous handoff
		if i > 0 && lastResult != nil && lastResult.Response != nil {
			if handoff.Context == nil {
				handoff.Context = make(map[string]any)
			}

			handoff.Context["previous_response"] = lastResult.Response.Content
			handoff.Context["previous_agent"] = lastResult.ToAgentID
		}

		result, err := m.Handoff(ctx, handoff)
		if err != nil {
			return nil, fmt.Errorf("handoff %d failed: %w", i+1, err)
		}

		handoffChain = append(handoffChain, result.HandoffChain...)
		lastResult = result
	}

	// Update the final result with the full chain
	if lastResult != nil {
		lastResult.HandoffChain = handoffChain
		lastResult.Duration = time.Since(startTime)
	}

	return lastResult, nil
}

// applyPreservation applies context preservation from source to target agent.
func (m *HandoffManager) applyPreservation(from, to *Agent, preserve []string) {
	for _, p := range preserve {
		switch p {
		case "history":
			// Copy conversation history
			fromState := from.GetState()
			for _, msg := range fromState.History {
				_ = to.addToHistory(msg)
			}

		case "state":
			// Copy state data
			fromState := from.GetState()
			for k, v := range fromState.Data {
				_ = to.SetStateData(k, v)
			}

		case "context":
			// Copy context data
			fromState := from.GetState()
			for k, v := range fromState.Context {
				_ = to.SetStateData("ctx_"+k, v)
			}
		}
	}
}

// CreateHandoffTool creates a tool that allows an agent to hand off to another agent.
func (m *HandoffManager) CreateHandoffTool(agentID string) Tool {
	return Tool{
		Name:        "handoff_to_agent",
		Description: "Hand off the conversation to another specialized agent. Use this when the current task requires expertise from a different agent.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target_agent": map[string]any{
					"type":        "string",
					"description": "The ID or name of the agent to hand off to",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Why this handoff is needed",
				},
				"context": map[string]any{
					"type":        "string",
					"description": "Additional context to pass to the target agent",
				},
			},
			"required": []string{"target_agent", "reason"},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			targetAgent, ok := params["target_agent"].(string)
			if !ok {
				return nil, ErrInvalidHandoffTarget
			}

			reason, _ := params["reason"].(string)
			contextStr, _ := params["context"].(string)

			handoffCtx := make(map[string]any)
			if contextStr != "" {
				handoffCtx["additional_context"] = contextStr
			}

			handoff := &AgentHandoff{
				FromAgentID: agentID,
				ToAgentID:   targetAgent,
				Reason:      reason,
				Context:     handoffCtx,
				Preserve:    []string{"history", "context"},
			}

			result, err := m.Handoff(ctx, handoff)
			if err != nil {
				return nil, err
			}

			return result.Response.Content, nil
		},
	}
}

// CreateToolRoutingTool creates a tool that routes to an agent with a specific tool.
func (m *HandoffManager) CreateToolRoutingTool(currentAgentID string) Tool {
	return Tool{
		Name:        "use_specialized_tool",
		Description: "Request a specialized tool from another agent. Use this when you need a tool you don't have.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool_name": map[string]any{
					"type":        "string",
					"description": "The name of the tool needed",
				},
				"input": map[string]any{
					"type":        "string",
					"description": "The input to pass to the tool/agent",
				},
			},
			"required": []string{"tool_name", "input"},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			toolName, ok := params["tool_name"].(string)
			if !ok {
				return nil, ErrInvalidToolName
			}

			input, _ := params["input"].(string)

			result, err := m.HandoffToTool(ctx, currentAgentID, toolName, input, nil)
			if err != nil {
				return nil, err
			}

			return result.Response.Content, nil
		},
	}
}

// Helper functions

func strToLower(s string) string {
	result := make([]byte, len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}

		result[i] = c
	}

	return string(result)
}

func strContains(s, substr string) bool {
	return indexOf(s, substr) >= 0
}

func containsAnyKeyword(input, description string) bool {
	descLower := strToLower(description)
	words := splitWords(descLower)

	for _, word := range words {
		if len(word) > 3 && strContains(input, word) {
			return true
		}
	}

	return false
}

func splitWords(s string) []string {
	words := make([]string, 0)
	start := -1

	for i := range len(s) {
		c := s[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')

		if isLetter {
			if start == -1 {
				start = i
			}
		} else {
			if start != -1 {
				words = append(words, s[start:i])
				start = -1
			}
		}
	}

	if start != -1 {
		words = append(words, s[start:])
	}

	return words
}

// AgentWithHandoff wraps an agent with handoff capabilities.
type AgentWithHandoff struct {
	*Agent

	handoffManager *HandoffManager
}

// NewAgentWithHandoff creates an agent with built-in handoff capabilities.
func NewAgentWithHandoff(agent *Agent, handoffManager *HandoffManager) *AgentWithHandoff {
	// Add handoff tools to the agent
	handoffTool := handoffManager.CreateHandoffTool(agent.ID)
	routingTool := handoffManager.CreateToolRoutingTool(agent.ID)

	agent.tools = append(agent.tools, handoffTool, routingTool)

	return &AgentWithHandoff{
		Agent:          agent,
		handoffManager: handoffManager,
	}
}

// ExecuteWithHandoff executes the agent with automatic handoff support.
func (a *AgentWithHandoff) ExecuteWithHandoff(ctx context.Context, input string) (*HandoffResult, error) {
	startTime := time.Now()

	response, err := a.Execute(ctx, input)
	if err != nil {
		return nil, err
	}

	return &HandoffResult{
		FromAgentID: "",
		ToAgentID:   a.ID,
		Response:    response,
		Duration:    time.Since(startTime),
		HandoffChain: []HandoffRecord{
			{
				FromAgentID: "",
				ToAgentID:   a.ID,
				Reason:      "Direct execution",
				Timestamp:   startTime,
				Duration:    time.Since(startTime),
				Context:     nil,
			},
		},
	}, nil
}

// SetContext sets context data on the agent state.
func (a *Agent) SetContext(key string, value any) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	if a.state.Context == nil {
		a.state.Context = make(map[string]any)
	}

	a.state.Context[key] = value
}

// GetContext gets context data from the agent state.
func (a *Agent) GetContext(key string) (any, bool) {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	if a.state.Context == nil {
		return nil, false
	}

	val, ok := a.state.Context[key]

	return val, ok
}

// GetAllContext returns a copy of all context data.
func (a *Agent) GetAllContext() map[string]any {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	result := make(map[string]any)
	maps.Copy(result, a.state.Context)

	return result
}
