package sdk

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/xraph/forge"
)

// AgentBuilder provides a fluent API for building AI agents.
type AgentBuilder struct {
	id           string
	name         string
	description  string
	model        string
	provider     string
	systemPrompt string
	tools        []Tool
	subAgents    []*Agent // For delegation/handoff
	maxIters     int
	temperature  float64

	// Dependencies
	llmManager LLMManager
	stateStore StateStore
	logger     forge.Logger
	metrics    forge.Metrics

	// Features
	guardrails     *GuardrailManager
	handoffManager *HandoffManager
	callbacks      AgentCallbacks

	// Validation errors
	errors []error
}

// NewAgentBuilder creates a new agent builder.
func NewAgentBuilder() *AgentBuilder {
	return &AgentBuilder{
		tools:       make([]Tool, 0),
		subAgents:   make([]*Agent, 0),
		maxIters:    10,
		temperature: 0.7,
		errors:      make([]error, 0),
	}
}

// WithID sets the agent ID.
func (b *AgentBuilder) WithID(id string) *AgentBuilder {
	b.id = id

	return b
}

// WithName sets the agent name.
func (b *AgentBuilder) WithName(name string) *AgentBuilder {
	b.name = name

	return b
}

// WithDescription sets the agent description.
func (b *AgentBuilder) WithDescription(desc string) *AgentBuilder {
	b.description = desc

	return b
}

// WithModel sets the LLM model to use.
func (b *AgentBuilder) WithModel(model string) *AgentBuilder {
	b.model = model

	return b
}

// WithProvider sets the LLM provider.
func (b *AgentBuilder) WithProvider(provider string) *AgentBuilder {
	b.provider = provider

	return b
}

// WithSystemPrompt sets the system prompt.
func (b *AgentBuilder) WithSystemPrompt(prompt string) *AgentBuilder {
	b.systemPrompt = prompt

	return b
}

// WithLLMManager sets the LLM manager.
func (b *AgentBuilder) WithLLMManager(mgr LLMManager) *AgentBuilder {
	b.llmManager = mgr

	return b
}

// WithStateStore sets the state store.
func (b *AgentBuilder) WithStateStore(store StateStore) *AgentBuilder {
	b.stateStore = store

	return b
}

// WithLogger sets the logger.
func (b *AgentBuilder) WithLogger(logger forge.Logger) *AgentBuilder {
	b.logger = logger

	return b
}

// WithMetrics sets the metrics.
func (b *AgentBuilder) WithMetrics(metrics forge.Metrics) *AgentBuilder {
	b.metrics = metrics

	return b
}

// WithTool adds a tool to the agent.
func (b *AgentBuilder) WithTool(tool Tool) *AgentBuilder {
	b.tools = append(b.tools, tool)

	return b
}

// WithTools adds multiple tools to the agent.
func (b *AgentBuilder) WithTools(tools ...Tool) *AgentBuilder {
	b.tools = append(b.tools, tools...)

	return b
}

// WithSubAgent adds a sub-agent for delegation.
func (b *AgentBuilder) WithSubAgent(agent *Agent) *AgentBuilder {
	b.subAgents = append(b.subAgents, agent)

	return b
}

// WithSubAgents adds multiple sub-agents for delegation.
func (b *AgentBuilder) WithSubAgents(agents ...*Agent) *AgentBuilder {
	b.subAgents = append(b.subAgents, agents...)

	return b
}

// WithMaxIterations sets the maximum number of iterations.
func (b *AgentBuilder) WithMaxIterations(max int) *AgentBuilder {
	b.maxIters = max

	return b
}

// WithTemperature sets the LLM temperature.
func (b *AgentBuilder) WithTemperature(temp float64) *AgentBuilder {
	b.temperature = temp

	return b
}

// WithGuardrails sets the guardrail manager.
func (b *AgentBuilder) WithGuardrails(gm *GuardrailManager) *AgentBuilder {
	b.guardrails = gm

	return b
}

// WithHandoffManager sets the handoff manager for agent delegation.
func (b *AgentBuilder) WithHandoffManager(hm *HandoffManager) *AgentBuilder {
	b.handoffManager = hm

	return b
}

// WithCallbacks sets the agent callbacks.
func (b *AgentBuilder) WithCallbacks(callbacks AgentCallbacks) *AgentBuilder {
	b.callbacks = callbacks

	return b
}

// OnStart sets the start callback.
func (b *AgentBuilder) OnStart(fn func(context.Context) error) *AgentBuilder {
	b.callbacks.OnStart = fn

	return b
}

// OnMessage sets the message callback.
func (b *AgentBuilder) OnMessage(fn func(AgentMessage)) *AgentBuilder {
	b.callbacks.OnMessage = fn

	return b
}

// OnToolCall sets the tool call callback.
func (b *AgentBuilder) OnToolCall(fn func(string, map[string]any)) *AgentBuilder {
	b.callbacks.OnToolCall = fn

	return b
}

// OnIteration sets the iteration callback.
func (b *AgentBuilder) OnIteration(fn func(int)) *AgentBuilder {
	b.callbacks.OnIteration = fn

	return b
}

// OnComplete sets the complete callback.
func (b *AgentBuilder) OnComplete(fn func(*AgentState)) *AgentBuilder {
	b.callbacks.OnComplete = fn

	return b
}

// OnError sets the error callback.
func (b *AgentBuilder) OnError(fn func(error)) *AgentBuilder {
	b.callbacks.OnError = fn

	return b
}

// validate validates the builder configuration.
func (b *AgentBuilder) validate() error {
	if b.id == "" {
		return ErrAgentIDRequired
	}

	if b.name == "" {
		b.name = b.id // Default name to ID
	}

	if b.llmManager == nil {
		return fmt.Errorf("%w: llmManager", ErrMissingConfig)
	}

	if b.stateStore == nil {
		return fmt.Errorf("%w: stateStore", ErrMissingConfig)
	}

	return nil
}

// Build constructs the agent.
func (b *AgentBuilder) Build() (*Agent, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}

	opts := &AgentOptions{
		SystemPrompt:  b.systemPrompt,
		Tools:         b.tools,
		MaxIterations: b.maxIters,
		Temperature:   b.temperature,
		Guardrails:    b.guardrails,
		Callbacks:     b.callbacks,
	}

	agent, err := NewAgent(
		b.id,
		b.name,
		b.llmManager,
		b.stateStore,
		b.logger,
		b.metrics,
		opts,
	)
	if err != nil {
		return nil, err
	}

	// Set additional properties
	agent.Description = b.description
	agent.Model = b.model
	agent.Provider = b.provider

	// If handoff manager is provided and we have sub-agents, add handoff tools
	if b.handoffManager != nil && len(b.subAgents) > 0 {
		handoffTool := b.handoffManager.CreateHandoffTool(agent.ID)
		routingTool := b.handoffManager.CreateToolRoutingTool(agent.ID)
		agent.tools = append(agent.tools, handoffTool, routingTool)
	}

	return agent, nil
}

// BuildWithHandoff constructs an agent with handoff capabilities.
func (b *AgentBuilder) BuildWithHandoff() (*AgentWithHandoff, error) {
	if b.handoffManager == nil {
		return nil, fmt.Errorf("%w: handoffManager required for BuildWithHandoff", ErrMissingConfig)
	}

	agent, err := b.Build()
	if err != nil {
		return nil, err
	}

	return NewAgentWithHandoff(agent, b.handoffManager), nil
}

// WorkflowBuilder provides a fluent API for building workflows.
type WorkflowBuilder struct {
	id          string
	name        string
	description string
	version     string
	nodes       []*WorkflowNode
	edges       [][2]string // pairs of [from, to]
	startNodes  []string

	// Dependencies
	toolRegistry  *ToolRegistry
	agentRegistry *AgentRegistry
	logger        forge.Logger
	metrics       forge.Metrics

	// Validation errors
	errors []error
}

// NewWorkflowBuilder creates a new workflow builder.
func NewWorkflowBuilder() *WorkflowBuilder {
	return &WorkflowBuilder{
		version:    "1.0.0",
		nodes:      make([]*WorkflowNode, 0),
		edges:      make([][2]string, 0),
		startNodes: make([]string, 0),
		errors:     make([]error, 0),
	}
}

// WithID sets the workflow ID.
func (b *WorkflowBuilder) WithID(id string) *WorkflowBuilder {
	b.id = id

	return b
}

// WithName sets the workflow name.
func (b *WorkflowBuilder) WithName(name string) *WorkflowBuilder {
	b.name = name

	return b
}

// WithDescription sets the workflow description.
func (b *WorkflowBuilder) WithDescription(desc string) *WorkflowBuilder {
	b.description = desc

	return b
}

// WithVersion sets the workflow version.
func (b *WorkflowBuilder) WithVersion(version string) *WorkflowBuilder {
	b.version = version

	return b
}

// WithToolRegistry sets the tool registry.
func (b *WorkflowBuilder) WithToolRegistry(tr *ToolRegistry) *WorkflowBuilder {
	b.toolRegistry = tr

	return b
}

// WithAgentRegistry sets the agent registry.
func (b *WorkflowBuilder) WithAgentRegistry(ar *AgentRegistry) *WorkflowBuilder {
	b.agentRegistry = ar

	return b
}

// WithLogger sets the logger.
func (b *WorkflowBuilder) WithLogger(logger forge.Logger) *WorkflowBuilder {
	b.logger = logger

	return b
}

// WithMetrics sets the metrics.
func (b *WorkflowBuilder) WithMetrics(metrics forge.Metrics) *WorkflowBuilder {
	b.metrics = metrics

	return b
}

// AddNode adds a node to the workflow.
func (b *WorkflowBuilder) AddNode(node *WorkflowNode) *WorkflowBuilder {
	b.nodes = append(b.nodes, node)

	return b
}

// AddAgentNode adds an agent node to the workflow.
func (b *WorkflowBuilder) AddAgentNode(id, name string, agent *Agent) *WorkflowBuilder {
	node := &WorkflowNode{
		ID:      id,
		Type:    NodeTypeAgent,
		Name:    name,
		AgentID: agent.ID,
		Timeout: 5 * time.Minute,
	}

	return b.AddNode(node)
}

// AddToolNode adds a tool node to the workflow.
func (b *WorkflowBuilder) AddToolNode(id, name string, tool Tool) *WorkflowBuilder {
	node := &WorkflowNode{
		ID:       id,
		Type:     NodeTypeTool,
		Name:     name,
		ToolName: tool.Name,
		Timeout:  1 * time.Minute,
	}

	return b.AddNode(node)
}

// AddConditionNode adds a condition node to the workflow.
func (b *WorkflowBuilder) AddConditionNode(id, name, condition string) *WorkflowBuilder {
	node := &WorkflowNode{
		ID:        id,
		Type:      NodeTypeCondition,
		Name:      name,
		Condition: condition,
		Timeout:   30 * time.Second,
	}

	return b.AddNode(node)
}

// AddTransformNode adds a transform node to the workflow.
func (b *WorkflowBuilder) AddTransformNode(id, name, transform string) *WorkflowBuilder {
	node := &WorkflowNode{
		ID:        id,
		Type:      NodeTypeTransform,
		Name:      name,
		Transform: transform,
		Timeout:   30 * time.Second,
	}

	return b.AddNode(node)
}

// AddWaitNode adds a wait node to the workflow.
func (b *WorkflowBuilder) AddWaitNode(id, name string, duration time.Duration) *WorkflowBuilder {
	node := &WorkflowNode{
		ID:      id,
		Type:    NodeTypeWait,
		Name:    name,
		Config:  map[string]any{"duration": duration},
		Timeout: duration + 10*time.Second,
	}

	return b.AddNode(node)
}

// AddEdge adds an edge between two nodes.
func (b *WorkflowBuilder) AddEdge(from, to string) *WorkflowBuilder {
	b.edges = append(b.edges, [2]string{from, to})

	return b
}

// AddSequence adds a sequence of node IDs (creates edges between consecutive nodes).
func (b *WorkflowBuilder) AddSequence(nodeIDs ...string) *WorkflowBuilder {
	for i := range len(nodeIDs) - 1 {
		b.AddEdge(nodeIDs[i], nodeIDs[i+1])
	}

	return b
}

// SetStartNode marks a node as a starting point.
func (b *WorkflowBuilder) SetStartNode(nodeID string) *WorkflowBuilder {
	b.startNodes = append(b.startNodes, nodeID)

	return b
}

// SetStartNodes marks multiple nodes as starting points.
func (b *WorkflowBuilder) SetStartNodes(nodeIDs ...string) *WorkflowBuilder {
	b.startNodes = append(b.startNodes, nodeIDs...)

	return b
}

// validate validates the builder configuration.
func (b *WorkflowBuilder) validate() error {
	if b.id == "" {
		return fmt.Errorf("%w: workflow ID is required", ErrInvalidConfig)
	}

	if b.name == "" {
		b.name = b.id
	}

	if len(b.nodes) == 0 {
		return fmt.Errorf("%w: workflow must have at least one node", ErrInvalidConfig)
	}

	if len(b.startNodes) == 0 {
		return ErrWorkflowNoEntryPoint
	}

	return nil
}

// Build constructs the workflow.
func (b *WorkflowBuilder) Build() (*Workflow, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}

	workflow := NewWorkflow(b.id, b.name, b.logger, b.metrics)
	workflow.Description = b.description
	workflow.Version = b.version

	// Add all nodes
	for _, node := range b.nodes {
		if err := workflow.AddNode(node); err != nil {
			return nil, fmt.Errorf("failed to add node %s: %w", node.ID, err)
		}
	}

	// Add all edges
	for _, edge := range b.edges {
		if err := workflow.AddEdge(edge[0], edge[1]); err != nil {
			return nil, fmt.Errorf("failed to add edge %s -> %s: %w", edge[0], edge[1], err)
		}
	}

	// Set start nodes
	for _, nodeID := range b.startNodes {
		if err := workflow.SetStartNode(nodeID); err != nil {
			return nil, fmt.Errorf("failed to set start node %s: %w", nodeID, err)
		}
	}

	return workflow, nil
}

// BuildWithRegistries constructs the workflow with connected registries.
func (b *WorkflowBuilder) BuildWithRegistries() (*WorkflowWithRegistries, error) {
	workflow, err := b.Build()
	if err != nil {
		return nil, err
	}

	return &WorkflowWithRegistries{
		Workflow:      workflow,
		ToolRegistry:  b.toolRegistry,
		AgentRegistry: b.agentRegistry,
	}, nil
}

// WorkflowWithRegistries is a workflow with access to tool and agent registries.
type WorkflowWithRegistries struct {
	*Workflow

	ToolRegistry  *ToolRegistry
	AgentRegistry *AgentRegistry
}

// ExecuteWithContext executes the workflow using the connected registries.
func (w *WorkflowWithRegistries) ExecuteWithContext(ctx context.Context, input map[string]any) (*WorkflowExecution, error) {
	// Store registries in input for node execution
	enrichedInput := make(map[string]any)
	maps.Copy(enrichedInput, input)

	enrichedInput["__tool_registry"] = w.ToolRegistry
	enrichedInput["__agent_registry"] = w.AgentRegistry

	return w.Execute(ctx, enrichedInput)
}
