package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// Agent represents an AI agent with state management
type Agent struct {
	ID          string
	Name        string
	Description string
	Model       string
	Provider    string

	// State management
	state      *AgentState
	stateStore StateStore
	stateMu    sync.RWMutex

	// Configuration
	llmManager LLMManager
	logger     forge.Logger
	metrics    forge.Metrics

	// Behavior
	systemPrompt string
	tools        []Tool
	guardrails   *GuardrailManager

	// Execution
	maxIterations int
	temperature   float64
	callbacks     AgentCallbacks
}

// AgentState represents the persistent state of an agent
type AgentState struct {
	AgentID   string                 `json:"agent_id"`
	SessionID string                 `json:"session_id"`
	Version   int                    `json:"version"`
	Data      map[string]interface{} `json:"data"`
	History   []AgentMessage         `json:"history"`
	Context   map[string]interface{} `json:"context"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// AgentMessage represents a message in the agent's history
type AgentMessage struct {
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// AgentCallbacks provides hooks into agent execution
type AgentCallbacks struct {
	OnStart      func(context.Context) error
	OnMessage    func(AgentMessage)
	OnToolCall   func(string, map[string]interface{})
	OnIteration  func(int)
	OnComplete   func(*AgentState)
	OnError      func(error)
	OnStateWrite func(*AgentState)
}

// AgentOptions configures an agent
type AgentOptions struct {
	SessionID     string
	SystemPrompt  string
	Tools         []Tool
	MaxIterations int
	Temperature   float64
	Guardrails    *GuardrailManager
	Callbacks     AgentCallbacks
}

// Tool represents a tool/function the agent can use
type Tool struct {
	Name        string                                      `json:"name"`
	Description string                                      `json:"description"`
	Parameters  map[string]interface{}                      `json:"parameters"`
	Handler     func(context.Context, map[string]interface{}) (interface{}, error) `json:"-"`
}

// AgentResponse represents the result of an agent execution
type AgentResponse struct {
	Content    string
	ToolCalls  []ToolExecution
	Iterations int
	State      *AgentState
	Metadata   map[string]interface{}
}

// ToolExecution represents a tool that was executed
type ToolExecution struct {
	Name      string
	Arguments map[string]interface{}
	Result    interface{}
	Error     error
	Duration  time.Duration
}

// NewAgent creates a new AI agent
func NewAgent(
	id string,
	name string,
	llmManager LLMManager,
	stateStore StateStore,
	logger forge.Logger,
	metrics forge.Metrics,
	opts *AgentOptions,
) (*Agent, error) {
	agent := &Agent{
		ID:            id,
		Name:          name,
		llmManager:    llmManager,
		stateStore:    stateStore,
		logger:        logger,
		metrics:       metrics,
		maxIterations: 10,
		temperature:   0.7,
		tools:         make([]Tool, 0),
	}

	// Determine session ID
	sessionID := fmt.Sprintf("%s_%d", id, time.Now().Unix())
	if opts != nil && opts.SessionID != "" {
		sessionID = opts.SessionID
	}

	// Try to load existing state
	existingState, err := stateStore.Load(context.Background(), id, sessionID)
	if err == nil && existingState != nil {
		agent.state = existingState
	} else {
		// Create new state
		agent.state = &AgentState{
			AgentID:   id,
			SessionID: sessionID,
			Version:   1,
			Data:      make(map[string]interface{}),
			History:   make([]AgentMessage, 0),
			Context:   make(map[string]interface{}),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	// Apply options if provided
	if opts != nil {
		if opts.SystemPrompt != "" {
			agent.systemPrompt = opts.SystemPrompt
		}
		if len(opts.Tools) > 0 {
			agent.tools = opts.Tools
		}
		if opts.MaxIterations > 0 {
			agent.maxIterations = opts.MaxIterations
		}
		if opts.Temperature > 0 {
			agent.temperature = opts.Temperature
		}
		agent.guardrails = opts.Guardrails
		agent.callbacks = opts.Callbacks
	}

	return agent, nil
}

// Execute runs the agent with the given input
func (a *Agent) Execute(ctx context.Context, input string) (*AgentResponse, error) {
	startTime := time.Now()

	if a.logger != nil {
		a.logger.Info("Agent executing",
			F("agent_id", a.ID),
			F("session_id", a.state.SessionID),
			F("input_length", len(input)),
		)
	}

	// Call onStart callback
	if a.callbacks.OnStart != nil {
		if err := a.callbacks.OnStart(ctx); err != nil {
			return nil, fmt.Errorf("onStart callback failed: %w", err)
		}
	}

	// Validate input with guardrails
	if a.guardrails != nil {
		violations, err := a.guardrails.ValidateInput(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("guardrail validation failed: %w", err)
		}
		if ShouldBlock(violations) {
			return nil, fmt.Errorf("input blocked by guardrails: %s", FormatViolations(violations))
		}
	}

	// Add user message to history
	userMsg := AgentMessage{
		Role:      "user",
		Content:   input,
		Timestamp: time.Now(),
	}
	if err := a.addToHistory(userMsg); err != nil {
		return nil, fmt.Errorf("failed to add message to history: %w", err)
	}

	response := &AgentResponse{
		ToolCalls: make([]ToolExecution, 0),
		Metadata:  make(map[string]interface{}),
	}

	// Execute agent loop with max iterations
	for iteration := 0; iteration < a.maxIterations; iteration++ {
		response.Iterations = iteration + 1

		if a.callbacks.OnIteration != nil {
			a.callbacks.OnIteration(iteration + 1)
		}

		// Generate response
		result, err := a.generateResponse(ctx)
		if err != nil {
			if a.callbacks.OnError != nil {
				a.callbacks.OnError(err)
			}
			return nil, fmt.Errorf("generation failed at iteration %d: %w", iteration+1, err)
		}

		// Check if we have a final answer
		if result.FinishReason == "stop" || result.FinishReason == "complete" {
			response.Content = result.Content

			// Validate output with guardrails
			if a.guardrails != nil {
				violations, err := a.guardrails.ValidateOutput(ctx, result.Content)
				if err != nil {
					return nil, fmt.Errorf("output guardrail validation failed: %w", err)
				}
				if ShouldBlock(violations) {
					return nil, fmt.Errorf("output blocked by guardrails: %s", FormatViolations(violations))
				}
			}

			// Add assistant message to history
			assistantMsg := AgentMessage{
				Role:      "assistant",
				Content:   result.Content,
				Timestamp: time.Now(),
			}
			if err := a.addToHistory(assistantMsg); err != nil {
				return nil, fmt.Errorf("failed to add message to history: %w", err)
			}

			break
		}

		// Handle tool calls if any
		if len(result.ToolCalls) > 0 {
			for _, toolCall := range result.ToolCalls {
				execution := a.executeTool(ctx, toolCall)
				response.ToolCalls = append(response.ToolCalls, execution)

				// Add tool result to history
				toolMsg := AgentMessage{
					Role:      "tool",
					Content:   fmt.Sprintf("Tool: %s, Result: %v", toolCall.Name, execution.Result),
					Timestamp: time.Now(),
					Metadata: map[string]interface{}{
						"tool_name": toolCall.Name,
						"duration":  execution.Duration.Milliseconds(),
					},
				}
				if err := a.addToHistory(toolMsg); err != nil {
					return nil, fmt.Errorf("failed to add tool message to history: %w", err)
				}
			}
		}
	}

	// Save final state
	if err := a.SaveState(ctx); err != nil {
		if a.logger != nil {
			a.logger.Warn("Failed to save agent state", F("error", err.Error()))
		}
	}

	// Set final state in response
	response.State = a.GetState()
	response.Metadata["duration"] = time.Since(startTime).Milliseconds()
	response.Metadata["version"] = a.state.Version

	if a.callbacks.OnComplete != nil {
		a.callbacks.OnComplete(response.State)
	}

	if a.metrics != nil {
		a.metrics.Counter("forge.ai.sdk.agent.executions").Inc()
		a.metrics.Histogram("forge.ai.sdk.agent.iterations").Observe(float64(response.Iterations))
		a.metrics.Histogram("forge.ai.sdk.agent.duration").Observe(time.Since(startTime).Seconds())
	}

	return response, nil
}

// generateResponse generates a response from the LLM
func (a *Agent) generateResponse(ctx context.Context) (*Result, error) {
	// Build messages from history
	agentMessages := a.buildMessages()

	// Convert AgentMessage to string messages for generate builder
	var prompt string
	for i, msg := range agentMessages {
		if msg.Role == "system" {
			// System messages are handled separately
			continue
		}
		if i > 0 {
			prompt += "\n"
		}
		prompt += fmt.Sprintf("%s: %s", msg.Role, msg.Content)
	}

	// Create generate builder
	builder := NewGenerateBuilder(ctx, a.llmManager, a.logger, a.metrics).
		WithProvider(a.Provider).
		WithModel(a.Model).
		WithPrompt(prompt).
		WithTemperature(a.temperature)

	// Add system prompt if present
	if a.systemPrompt != "" {
		builder.WithSystemPrompt(a.systemPrompt)
	}

	// Add tools if available
	if len(a.tools) > 0 {
		// Convert to LLM tools format (simplified)
		// In production, you'd have proper conversion
		builder.WithToolChoice("auto")
	}

	return builder.Execute()
}

// executeTool executes a tool and returns the result
func (a *Agent) executeTool(ctx context.Context, toolCall ToolCallResult) ToolExecution {
	startTime := time.Now()

	execution := ToolExecution{
		Name:      toolCall.Name,
		Arguments: toolCall.Arguments,
	}

	// Find the tool
	var tool *Tool
	for i := range a.tools {
		if a.tools[i].Name == toolCall.Name {
			tool = &a.tools[i]
			break
		}
	}

	if tool == nil {
		execution.Error = fmt.Errorf("tool not found: %s", toolCall.Name)
		execution.Duration = time.Since(startTime)
		return execution
	}

	// Execute tool handler
	if tool.Handler != nil {
		result, err := tool.Handler(ctx, toolCall.Arguments)
		execution.Result = result
		execution.Error = err
	}

	execution.Duration = time.Since(startTime)

	if a.callbacks.OnToolCall != nil {
		a.callbacks.OnToolCall(toolCall.Name, toolCall.Arguments)
	}

	if a.metrics != nil {
		a.metrics.Counter("forge.ai.sdk.agent.tool_calls", "tool", toolCall.Name).Inc()
		a.metrics.Histogram("forge.ai.sdk.agent.tool_duration", "tool", toolCall.Name).Observe(execution.Duration.Seconds())
	}

	return execution
}

// buildMessages builds messages for LLM from history
func (a *Agent) buildMessages() []AgentMessage {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	messages := make([]AgentMessage, 0, len(a.state.History)+1)

	// Add system prompt if set
	if a.systemPrompt != "" {
		messages = append(messages, AgentMessage{
			Role:    "system",
			Content: a.systemPrompt,
		})
	}

	// Add history
	messages = append(messages, a.state.History...)

	return messages
}

// addToHistory adds a message to the agent's history
func (a *Agent) addToHistory(msg AgentMessage) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	a.state.History = append(a.state.History, msg)
	a.state.UpdatedAt = time.Now()

	if a.callbacks.OnMessage != nil {
		a.callbacks.OnMessage(msg)
	}

	return nil
}

// GetState returns a copy of the current state
func (a *Agent) GetState() *AgentState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	// Deep copy to prevent external modifications
	stateCopy := &AgentState{
		AgentID:   a.state.AgentID,
		SessionID: a.state.SessionID,
		Version:   a.state.Version,
		Data:      make(map[string]interface{}),
		History:   make([]AgentMessage, len(a.state.History)),
		Context:   make(map[string]interface{}),
		CreatedAt: a.state.CreatedAt,
		UpdatedAt: a.state.UpdatedAt,
	}

	// Copy data
	for k, v := range a.state.Data {
		stateCopy.Data[k] = v
	}

	// Copy history
	copy(stateCopy.History, a.state.History)

	// Copy context
	for k, v := range a.state.Context {
		stateCopy.Context[k] = v
	}

	return stateCopy
}

// SetStateData sets a value in the agent's state data
func (a *Agent) SetStateData(key string, value interface{}) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	a.state.Data[key] = value
	a.state.UpdatedAt = time.Now()

	return nil
}

// GetStateData gets a value from the agent's state data
func (a *Agent) GetStateData(key string) (interface{}, bool) {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	val, ok := a.state.Data[key]
	return val, ok
}

// SaveState persists the agent's state
func (a *Agent) SaveState(ctx context.Context) error {
	a.stateMu.Lock()
	a.state.Version++
	a.state.UpdatedAt = time.Now()
	a.stateMu.Unlock()

	state := a.GetState()

	if err := a.stateStore.Save(ctx, state); err != nil {
		if a.metrics != nil {
			a.metrics.Counter("forge.ai.sdk.agent.state_save_errors").Inc()
		}
		return fmt.Errorf("failed to save state: %w", err)
	}

	if a.callbacks.OnStateWrite != nil {
		a.callbacks.OnStateWrite(state)
	}

	if a.metrics != nil {
		a.metrics.Counter("forge.ai.sdk.agent.state_saves").Inc()
	}

	return nil
}

// LoadState loads the agent's state from storage
func (a *Agent) LoadState(ctx context.Context, sessionID string) error {
	state, err := a.stateStore.Load(ctx, a.ID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	if state == nil {
		return fmt.Errorf("state not found for session %s", sessionID)
	}

	a.stateMu.Lock()
	a.state = state
	a.stateMu.Unlock()

	if a.logger != nil {
		a.logger.Info("Agent state loaded",
			F("agent_id", a.ID),
			F("session_id", sessionID),
			F("version", state.Version),
		)
	}

	return nil
}

// ClearHistory clears the agent's conversation history
func (a *Agent) ClearHistory() {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	a.state.History = make([]AgentMessage, 0)
	a.state.UpdatedAt = time.Now()
}

// Reset resets the agent to a fresh state
func (a *Agent) Reset() {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	a.state.Version++
	a.state.Data = make(map[string]interface{})
	a.state.History = make([]AgentMessage, 0)
	a.state.Context = make(map[string]interface{})
	a.state.UpdatedAt = time.Now()
}

// MarshalState marshals the agent state to JSON
func (a *Agent) MarshalState() ([]byte, error) {
	state := a.GetState()
	return json.Marshal(state)
}

// UnmarshalState unmarshals agent state from JSON
func (a *Agent) UnmarshalState(data []byte) error {
	var state AgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	a.stateMu.Lock()
	a.state = &state
	a.stateMu.Unlock()

	return nil
}

// GetHistoryLength returns the number of messages in history
func (a *Agent) GetHistoryLength() int {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return len(a.state.History)
}

// GetSessionID returns the current session ID
func (a *Agent) GetSessionID() string {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.state.SessionID
}

