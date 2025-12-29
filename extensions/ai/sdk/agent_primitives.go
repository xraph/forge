package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// EnhancedAgent extends Agent with fine-grained execution control.
type EnhancedAgent struct {
	*Agent

	// Enhanced control
	stopConditions []StepCondition
	preparers      []StepPreparer
	stepCallbacks  []StepCallback

	// Execution state
	history   *StepHistory
	execution *AgentExecution
	mu        sync.RWMutex
}

// StepCallback is called after each step.
type StepCallback func(step *AgentStep)

// AgentExecution represents a single agent execution session.
type AgentExecution struct {
	ID          string
	AgentID     string
	StartTime   time.Time
	EndTime     time.Time
	Status      ExecutionStatus
	Steps       []*AgentStep
	FinalOutput string
	Error       string
	TotalTokens int
	Metadata    map[string]any
}

// ExecutionStatus represents the status of an execution.
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
	ExecutionStatusCancelled ExecutionStatus = "cancelled"
)

// EnhancedAgentBuilder builds an enhanced agent.
type EnhancedAgentBuilder struct {
	agent       *EnhancedAgent
	baseBuilder *AgentBuilder
}

// NewEnhancedAgentBuilder creates a new enhanced agent builder.
func NewEnhancedAgentBuilder(name string) *EnhancedAgentBuilder {
	baseBuilder := NewAgentBuilder().WithName(name)
	return &EnhancedAgentBuilder{
		agent: &EnhancedAgent{
			stopConditions: make([]StepCondition, 0),
			preparers:      make([]StepPreparer, 0),
			stepCallbacks:  make([]StepCallback, 0),
			history:        NewStepHistory(),
		},
		baseBuilder: baseBuilder,
	}
}

// WithID sets the agent ID.
func (b *EnhancedAgentBuilder) WithID(id string) *EnhancedAgentBuilder {
	b.baseBuilder.WithID(id)
	return b
}

// WithDescription sets the agent description.
func (b *EnhancedAgentBuilder) WithDescription(desc string) *EnhancedAgentBuilder {
	b.baseBuilder.WithDescription(desc)
	return b
}

// WithModel sets the model.
func (b *EnhancedAgentBuilder) WithModel(model string) *EnhancedAgentBuilder {
	b.baseBuilder.WithModel(model)
	return b
}

// WithProvider sets the provider.
func (b *EnhancedAgentBuilder) WithProvider(provider string) *EnhancedAgentBuilder {
	b.baseBuilder.WithProvider(provider)
	return b
}

// WithSystemPrompt sets the system prompt.
func (b *EnhancedAgentBuilder) WithSystemPrompt(prompt string) *EnhancedAgentBuilder {
	b.baseBuilder.WithSystemPrompt(prompt)
	return b
}

// WithTools sets the tools.
func (b *EnhancedAgentBuilder) WithTools(tools ...Tool) *EnhancedAgentBuilder {
	b.baseBuilder.WithTools(tools...)
	return b
}

// WithLLMManager sets the LLM manager.
func (b *EnhancedAgentBuilder) WithLLMManager(mgr LLMManager) *EnhancedAgentBuilder {
	b.baseBuilder.WithLLMManager(mgr)
	return b
}

// WithLogger sets the logger.
func (b *EnhancedAgentBuilder) WithLogger(logger forge.Logger) *EnhancedAgentBuilder {
	b.baseBuilder.WithLogger(logger)
	return b
}

// WithMetrics sets the metrics.
func (b *EnhancedAgentBuilder) WithMetrics(metrics forge.Metrics) *EnhancedAgentBuilder {
	b.baseBuilder.WithMetrics(metrics)
	return b
}

// WithMaxIterations sets max iterations.
func (b *EnhancedAgentBuilder) WithMaxIterations(max int) *EnhancedAgentBuilder {
	b.baseBuilder.WithMaxIterations(max)
	return b
}

// WithTemperature sets the temperature.
func (b *EnhancedAgentBuilder) WithTemperature(temp float64) *EnhancedAgentBuilder {
	b.baseBuilder.WithTemperature(temp)
	return b
}

// WithGuardrails sets guardrails.
func (b *EnhancedAgentBuilder) WithGuardrails(guardrails *GuardrailManager) *EnhancedAgentBuilder {
	b.baseBuilder.WithGuardrails(guardrails)
	return b
}

// StopWhen adds a stop condition.
func (b *EnhancedAgentBuilder) StopWhen(condition StepCondition) *EnhancedAgentBuilder {
	b.agent.stopConditions = append(b.agent.stopConditions, condition)
	return b
}

// PrepareStep adds a step preparer.
func (b *EnhancedAgentBuilder) PrepareStep(preparer StepPreparer) *EnhancedAgentBuilder {
	b.agent.preparers = append(b.agent.preparers, preparer)
	return b
}

// OnStep adds a step callback.
func (b *EnhancedAgentBuilder) OnStep(callback StepCallback) *EnhancedAgentBuilder {
	b.agent.stepCallbacks = append(b.agent.stepCallbacks, callback)
	return b
}

// MaxSteps is a convenience method for setting max steps as a stop condition.
func (b *EnhancedAgentBuilder) MaxSteps(max int) *EnhancedAgentBuilder {
	return b.StopWhen(StopOnMaxSteps(max))
}

// Build creates the enhanced agent.
func (b *EnhancedAgentBuilder) Build() (*EnhancedAgent, error) {
	agent, err := b.baseBuilder.Build()
	if err != nil {
		return nil, err
	}
	b.agent.Agent = agent
	return b.agent, nil
}

// Execute runs the agent with enhanced control.
func (a *EnhancedAgent) Execute(ctx context.Context, input string) (*AgentExecution, error) {
	a.mu.Lock()

	// Create execution
	execution := &AgentExecution{
		ID:        generateExecutionID(),
		AgentID:   a.ID,
		StartTime: time.Now(),
		Status:    ExecutionStatusRunning,
		Steps:     make([]*AgentStep, 0),
		Metadata:  make(map[string]any),
	}
	a.execution = execution
	a.history = NewStepHistory()
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		execution.EndTime = time.Now()
		a.mu.Unlock()
	}()

	// Execute steps
	currentInput := input
	stepIndex := 0

	for {
		select {
		case <-ctx.Done():
			a.mu.Lock()
			execution.Status = ExecutionStatusCancelled
			execution.Error = ctx.Err().Error()
			a.mu.Unlock()
			return execution, ctx.Err()
		default:
		}

		// Create step
		step := NewStepBuilder(a.ID, execution.ID, stepIndex).
			WithInput(currentInput).
			WithState(StepStatePending).
			Build()

		// Prepare step
		var err error
		for _, preparer := range a.preparers {
			step, err = preparer(ctx, step)
			if err != nil {
				step.State = StepStateFailed
				step.Error = err.Error()
				a.addStep(step, execution)

				a.mu.Lock()
				execution.Status = ExecutionStatusFailed
				execution.Error = err.Error()
				a.mu.Unlock()
				return execution, err
			}
		}

		// Execute step
		step.State = StepStateRunning
		step, err = a.executeStep(ctx, step)

		// Add step to history
		a.addStep(step, execution)

		// Call step callbacks
		for _, callback := range a.stepCallbacks {
			callback(step)
		}

		if err != nil {
			a.mu.Lock()
			execution.Status = ExecutionStatusFailed
			execution.Error = err.Error()
			a.mu.Unlock()
			return execution, err
		}

		// Check stop conditions
		shouldStop := false
		for _, condition := range a.stopConditions {
			if condition(step, a.history.Steps) {
				shouldStop = true
				break
			}
		}

		// Default stop conditions
		if !shouldStop {
			// Stop if no tool calls (final answer)
			if step.IsFinal() {
				shouldStop = true
			}
			// Stop on max iterations (from base agent)
			if a.maxIterations > 0 && stepIndex >= a.maxIterations-1 {
				shouldStop = true
			}
		}

		if shouldStop {
			a.mu.Lock()
			execution.Status = ExecutionStatusCompleted
			execution.FinalOutput = step.Output
			a.mu.Unlock()
			return execution, nil
		}

		// Prepare for next iteration
		currentInput = step.Output
		stepIndex++
	}
}

// executeStep executes a single step.
func (a *EnhancedAgent) executeStep(ctx context.Context, step *AgentStep) (*AgentStep, error) {
	startTime := time.Now()

	// Build messages
	messages := a.buildStepMessages(step)

	// Create request
	request := llm.ChatRequest{
		Provider: a.Provider,
		Model:    a.Model,
		Messages: messages,
	}

	if a.temperature != 0 {
		request.Temperature = &a.temperature
	}

	// Add tools if any
	if len(a.tools) > 0 {
		llmTools := make([]llm.Tool, len(a.tools))
		for i, tool := range a.tools {
			llmTools[i] = llm.Tool{
				Type: "function",
				Function: &llm.FunctionDefinition{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
				},
			}
		}
		request.Tools = llmTools
	}

	// Call LLM
	response, err := a.llmManager.Chat(ctx, request)
	if err != nil {
		step.State = StepStateFailed
		step.Error = err.Error()
		step.EndTime = time.Now()
		step.Duration = step.EndTime.Sub(startTime)
		return step, err
	}

	// Extract content
	if len(response.Choices) == 0 {
		step.State = StepStateFailed
		step.Error = "no response from LLM"
		step.EndTime = time.Now()
		step.Duration = step.EndTime.Sub(startTime)
		return step, fmt.Errorf("no response from LLM")
	}

	choice := response.Choices[0]
	step.Output = choice.Message.Content

	// Track token usage
	if response.Usage != nil {
		step.TokensUsed = int(response.Usage.TotalTokens)
	}

	// Process tool calls
	if len(choice.Message.ToolCalls) > 0 {
		step.State = StepStateWaiting

		for _, tc := range choice.Message.ToolCalls {
			toolCall := StepToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: make(map[string]any),
				StartTime: time.Now(),
			}

			// Parse arguments
			if tc.Function.Arguments != "" {
				json.Unmarshal([]byte(tc.Function.Arguments), &toolCall.Arguments)
			}

			step.ToolCalls = append(step.ToolCalls, toolCall)

			// Execute tool
			result, toolErr := a.executeTool(ctx, tc.Function.Name, toolCall.Arguments)

			toolResult := StepToolResult{
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Result:     result,
				Duration:   time.Since(toolCall.StartTime),
			}
			if toolErr != nil {
				toolResult.Error = toolErr.Error()
			}

			step.ToolResults = append(step.ToolResults, toolResult)
		}

		// Build output from tool results if no direct output
		if step.Output == "" {
			step.Output = a.formatToolResults(step.ToolResults)
		}
	}

	step.State = StepStateCompleted
	step.EndTime = time.Now()
	step.Duration = step.EndTime.Sub(startTime)

	return step, nil
}

// buildStepMessages builds the messages for a step.
func (a *EnhancedAgent) buildStepMessages(step *AgentStep) []llm.ChatMessage {
	messages := make([]llm.ChatMessage, 0)

	// Add system prompt
	if a.systemPrompt != "" {
		messages = append(messages, llm.ChatMessage{
			Role:    "system",
			Content: a.systemPrompt,
		})
	}

	// Add history as context
	for _, prevStep := range a.history.Steps {
		// Add user input
		if prevStep.Input != "" {
			messages = append(messages, llm.ChatMessage{
				Role:    "user",
				Content: prevStep.Input,
			})
		}

		// Add assistant output
		if prevStep.Output != "" {
			messages = append(messages, llm.ChatMessage{
				Role:    "assistant",
				Content: prevStep.Output,
			})
		}

		// Add tool results
		for _, tr := range prevStep.ToolResults {
			resultJSON, _ := json.Marshal(tr.Result)
			messages = append(messages, llm.ChatMessage{
				Role:    "tool",
				Content: string(resultJSON),
			})
		}
	}

	// Add current input
	messages = append(messages, llm.ChatMessage{
		Role:    "user",
		Content: step.Input,
	})

	return messages
}

// executeTool executes a tool by name.
func (a *EnhancedAgent) executeTool(ctx context.Context, name string, args map[string]any) (any, error) {
	for _, tool := range a.tools {
		if tool.Name == name {
			if tool.Handler != nil {
				return tool.Handler(ctx, args)
			}
			return nil, fmt.Errorf("tool %s has no handler", name)
		}
	}
	return nil, fmt.Errorf("tool not found: %s", name)
}

// formatToolResults formats tool results for output.
func (a *EnhancedAgent) formatToolResults(results []StepToolResult) string {
	var output string
	for _, r := range results {
		if r.Error != "" {
			output += fmt.Sprintf("Tool %s error: %s\n", r.Name, r.Error)
		} else {
			resultJSON, _ := json.Marshal(r.Result)
			output += fmt.Sprintf("Tool %s result: %s\n", r.Name, string(resultJSON))
		}
	}
	return output
}

// addStep adds a step to history and execution.
func (a *EnhancedAgent) addStep(step *AgentStep, execution *AgentExecution) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.history.Add(step)
	execution.Steps = append(execution.Steps, step)
	execution.TotalTokens += step.TokensUsed
}

// GetHistory returns the current step history.
func (a *EnhancedAgent) GetHistory() *StepHistory {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.history
}

// GetExecution returns the current execution.
func (a *EnhancedAgent) GetExecution() *AgentExecution {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.execution
}

// generateExecutionID generates a unique execution ID.
func generateExecutionID() string {
	return fmt.Sprintf("exec_%d", time.Now().UnixNano())
}

// ExecuteWithStreaming runs the agent with streaming support.
func (a *EnhancedAgent) ExecuteWithStreaming(ctx context.Context, input string, onStep func(*AgentStep)) (*AgentExecution, error) {
	// Add the streaming callback temporarily
	originalCallbacks := a.stepCallbacks
	a.stepCallbacks = append(a.stepCallbacks, onStep)
	defer func() {
		a.stepCallbacks = originalCallbacks
	}()

	return a.Execute(ctx, input)
}

// ContinueExecution continues from the last step.
func (a *EnhancedAgent) ContinueExecution(ctx context.Context, additionalInput string) (*AgentExecution, error) {
	a.mu.RLock()
	lastStep := a.history.Last()
	a.mu.RUnlock()

	var input string
	if lastStep != nil {
		input = lastStep.Output + "\n" + additionalInput
	} else {
		input = additionalInput
	}

	return a.Execute(ctx, input)
}

// AgentOrchestrator coordinates multiple agents.
type AgentOrchestrator struct {
	agents map[string]*EnhancedAgent
	router EnhancedAgentRouter
	logger forge.Logger
	mu     sync.RWMutex
}

// EnhancedAgentRouter routes requests to enhanced agents.
type EnhancedAgentRouter interface {
	Route(ctx context.Context, input string, agents map[string]*EnhancedAgent) (*EnhancedAgent, error)
}

// NewAgentOrchestrator creates a new orchestrator.
func NewAgentOrchestrator(logger forge.Logger) *AgentOrchestrator {
	return &AgentOrchestrator{
		agents: make(map[string]*EnhancedAgent),
		logger: logger,
	}
}

// AddAgent adds an agent to the orchestrator.
func (o *AgentOrchestrator) AddAgent(name string, agent *EnhancedAgent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.agents[name] = agent
}

// RemoveAgent removes an agent.
func (o *AgentOrchestrator) RemoveAgent(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.agents, name)
}

// GetAgent returns an agent by name.
func (o *AgentOrchestrator) GetAgent(name string) (*EnhancedAgent, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	agent, ok := o.agents[name]
	return agent, ok
}

// SetRouter sets the router.
func (o *AgentOrchestrator) SetRouter(router EnhancedAgentRouter) {
	o.router = router
}

// Execute routes and executes with an appropriate agent.
func (o *AgentOrchestrator) Execute(ctx context.Context, input string) (*AgentExecution, error) {
	o.mu.RLock()
	agents := make(map[string]*EnhancedAgent)
	for k, v := range o.agents {
		agents[k] = v
	}
	o.mu.RUnlock()

	if len(agents) == 0 {
		return nil, fmt.Errorf("no agents available")
	}

	var agent *EnhancedAgent

	if o.router != nil {
		var err error
		agent, err = o.router.Route(ctx, input, agents)
		if err != nil {
			return nil, fmt.Errorf("routing failed: %w", err)
		}
	} else {
		// Use first agent if no router
		for _, a := range agents {
			agent = a
			break
		}
	}

	return agent.Execute(ctx, input)
}

// ExecuteSequential executes multiple agents in sequence.
func (o *AgentOrchestrator) ExecuteSequential(ctx context.Context, input string, agentNames ...string) ([]*AgentExecution, error) {
	executions := make([]*AgentExecution, 0, len(agentNames))
	currentInput := input

	for _, name := range agentNames {
		agent, ok := o.GetAgent(name)
		if !ok {
			return executions, fmt.Errorf("agent not found: %s", name)
		}

		execution, err := agent.Execute(ctx, currentInput)
		executions = append(executions, execution)

		if err != nil {
			return executions, err
		}

		currentInput = execution.FinalOutput
	}

	return executions, nil
}

// ExecuteParallel executes multiple agents in parallel.
func (o *AgentOrchestrator) ExecuteParallel(ctx context.Context, input string, agentNames ...string) ([]*AgentExecution, error) {
	var wg sync.WaitGroup
	executions := make([]*AgentExecution, len(agentNames))
	errors := make([]error, len(agentNames))

	for i, name := range agentNames {
		wg.Add(1)
		go func(idx int, agentName string) {
			defer wg.Done()

			agent, ok := o.GetAgent(agentName)
			if !ok {
				errors[idx] = fmt.Errorf("agent not found: %s", agentName)
				return
			}

			execution, err := agent.Execute(ctx, input)
			executions[idx] = execution
			errors[idx] = err
		}(i, name)
	}

	wg.Wait()

	// Check for errors
	for _, err := range errors {
		if err != nil {
			return executions, err
		}
	}

	return executions, nil
}
