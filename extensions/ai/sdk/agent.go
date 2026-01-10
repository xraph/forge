package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// Agent represents an AI agent with state management.
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

	// Structured response support
	parseStructured   bool
	responseParser    *ResponseParser
	artifactRegistry  *ArtifactRegistry
	citationManager   *CitationManager
	suggestionManager *SuggestionManager
}

// AgentState represents the persistent state of an agent.
type AgentState struct {
	AgentID   string         `json:"agent_id"`
	SessionID string         `json:"session_id"`
	Version   int            `json:"version"`
	Data      map[string]any `json:"data"`
	History   []AgentMessage `json:"history"`
	Context   map[string]any `json:"context"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// AgentMessage represents a message in the agent's history.
type AgentMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// AgentCallbacks provides hooks into agent execution.
type AgentCallbacks struct {
	OnStart      func(context.Context) error
	OnMessage    func(AgentMessage)
	OnToolCall   func(string, map[string]any)
	OnIteration  func(int)
	OnComplete   func(*AgentState)
	OnError      func(error)
	OnStateWrite func(*AgentState)
}

// AgentOptions configures an agent.
type AgentOptions struct {
	SessionID     string
	SystemPrompt  string
	Tools         []Tool
	MaxIterations int
	Temperature   float64
	Guardrails    *GuardrailManager
	Callbacks     AgentCallbacks
}

// Tool represents a tool/function the agent can use.
type Tool struct {
	Name        string                                             `json:"name"`
	Description string                                             `json:"description"`
	Parameters  map[string]any                                     `json:"parameters"`
	Handler     func(context.Context, map[string]any) (any, error) `json:"-"`
}

// AgentResponse represents the result of an agent execution.
type AgentResponse struct {
	Content            string
	ToolCalls          []ToolExecution
	Iterations         int
	State              *AgentState
	Metadata           map[string]any
	StructuredResponse *StructuredResponse
	Artifacts          []Artifact
	Citations          []Citation
	Suggestions        []Suggestion
}

// ToolExecution represents a tool that was executed.
type ToolExecution struct {
	Name      string
	Arguments map[string]any
	Result    any
	Error     error
	Duration  time.Duration
}

// NewAgent creates a new AI agent.
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
			Data:      make(map[string]any),
			History:   make([]AgentMessage, 0),
			Context:   make(map[string]any),
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

// Execute runs the agent with the given input.
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
		Metadata:  make(map[string]any),
	}

	// Execute agent loop with max iterations
	for iteration := range a.maxIterations {
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
			// Execute tools in parallel for better performance
			executions := a.executeToolsParallel(ctx, result.ToolCalls)

			for _, execution := range executions {
				response.ToolCalls = append(response.ToolCalls, execution)

				// Add tool result to history
				toolMsg := AgentMessage{
					Role:      "tool",
					Content:   fmt.Sprintf("Tool: %s, Result: %v", execution.Name, execution.Result),
					Timestamp: time.Now(),
					Metadata: map[string]any{
						"tool_name": execution.Name,
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

	// Parse into structured response if enabled
	if a.parseStructured && a.responseParser != nil {
		response.StructuredResponse = a.parseToStructuredResponse(response)
	}

	// Generate suggestions if manager is configured
	if a.suggestionManager != nil {
		response.Suggestions = a.generateSuggestions(ctx, response, input)
	}

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

// generateResponse generates a response from the LLM.
func (a *Agent) generateResponse(ctx context.Context) (*Result, error) {
	// Build messages from history
	agentMessages := a.buildMessages()

	// Convert AgentMessage to string messages for generate builder
	var (
		prompt      string
		promptSb322 strings.Builder
	)

	for i, msg := range agentMessages {
		if msg.Role == "system" {
			// System messages are handled separately
			continue
		}

		if i > 0 {
			promptSb322.WriteString("\n")
		}

		promptSb322.WriteString(fmt.Sprintf("%s: %s", msg.Role, msg.Content))
	}

	prompt += promptSb322.String()

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
		// Convert SDK tools to LLM tools format
		llmTools := make([]llm.Tool, 0, len(a.tools))
		for _, tool := range a.tools {
			llmTools = append(llmTools, llm.Tool{
				Type: "function",
				Function: &llm.FunctionDefinition{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.Parameters,
				},
			})
		}

		builder.WithTools(llmTools...)
		builder.WithToolChoice("auto")
	}

	return builder.Execute()
}

// executeTool executes a tool and returns the result.
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

// executeToolsParallel executes multiple tools concurrently.
// This significantly improves performance when an LLM requests multiple independent tool calls.
func (a *Agent) executeToolsParallel(ctx context.Context, toolCalls []ToolCallResult) []ToolExecution {
	if len(toolCalls) == 0 {
		return []ToolExecution{}
	}

	// For a single tool call, execute directly without goroutine overhead
	if len(toolCalls) == 1 {
		return []ToolExecution{a.executeTool(ctx, toolCalls[0])}
	}

	// Execute tools in parallel
	results := make([]ToolExecution, len(toolCalls))

	var wg sync.WaitGroup

	for i, toolCall := range toolCalls {
		wg.Add(1)

		go func(idx int, tc ToolCallResult) {
			defer wg.Done()

			results[idx] = a.executeTool(ctx, tc)
		}(i, toolCall)
	}

	wg.Wait()

	if a.logger != nil && len(toolCalls) > 1 {
		a.logger.Debug("Executed tools in parallel",
			F("agent_id", a.ID),
			F("tool_count", len(toolCalls)),
		)
	}

	if a.metrics != nil {
		a.metrics.Histogram("forge.ai.sdk.agent.parallel_tool_count").Observe(float64(len(toolCalls)))
	}

	return results
}

// buildMessages builds messages for LLM from history.
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

// addToHistory adds a message to the agent's history.
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

// GetState returns a copy of the current state.
func (a *Agent) GetState() *AgentState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	// Deep copy to prevent external modifications
	stateCopy := &AgentState{
		AgentID:   a.state.AgentID,
		SessionID: a.state.SessionID,
		Version:   a.state.Version,
		Data:      make(map[string]any),
		History:   make([]AgentMessage, len(a.state.History)),
		Context:   make(map[string]any),
		CreatedAt: a.state.CreatedAt,
		UpdatedAt: a.state.UpdatedAt,
	}

	// Copy data
	maps.Copy(stateCopy.Data, a.state.Data)

	// Copy history
	copy(stateCopy.History, a.state.History)

	// Copy context
	maps.Copy(stateCopy.Context, a.state.Context)

	return stateCopy
}

// SetStateData sets a value in the agent's state data.
func (a *Agent) SetStateData(key string, value any) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	a.state.Data[key] = value
	a.state.UpdatedAt = time.Now()

	return nil
}

// GetStateData gets a value from the agent's state data.
func (a *Agent) GetStateData(key string) (any, bool) {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	val, ok := a.state.Data[key]

	return val, ok
}

// SaveState persists the agent's state.
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

// LoadState loads the agent's state from storage.
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

// ClearHistory clears the agent's conversation history.
func (a *Agent) ClearHistory() {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	a.state.History = make([]AgentMessage, 0)
	a.state.UpdatedAt = time.Now()
}

// Reset resets the agent to a fresh state.
func (a *Agent) Reset() {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()

	a.state.Version++
	a.state.Data = make(map[string]any)
	a.state.History = make([]AgentMessage, 0)
	a.state.Context = make(map[string]any)
	a.state.UpdatedAt = time.Now()
}

// MarshalState marshals the agent state to JSON.
func (a *Agent) MarshalState() ([]byte, error) {
	state := a.GetState()

	return json.Marshal(state)
}

// UnmarshalState unmarshals agent state from JSON.
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

// GetHistoryLength returns the number of messages in history.
func (a *Agent) GetHistoryLength() int {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	return len(a.state.History)
}

// GetSessionID returns the current session ID.
func (a *Agent) GetSessionID() string {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()

	return a.state.SessionID
}

// AsTool converts this agent into a Tool that can be used by other agents.
// This enables the agent-as-tool pattern where agents can delegate to other agents.
func (a *Agent) AsTool() Tool {
	return Tool{
		Name:        "call_" + a.Name,
		Description: a.getToolDescription(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "The input/question to send to the " + a.Name + " agent",
				},
				"context": map[string]any{
					"type":        "object",
					"description": "Optional context data to pass to the agent",
				},
			},
			"required": []string{"input"},
		},
		Handler: a.toolHandler,
	}
}

// getToolDescription builds a description for the agent-as-tool.
func (a *Agent) getToolDescription() string {
	desc := a.Description
	if desc == "" {
		desc = fmt.Sprintf("Delegate task to the %s agent", a.Name)
	}

	// Add information about the agent's capabilities
	if len(a.tools) > 0 {
		toolNames := make([]string, len(a.tools))
		for i, tool := range a.tools {
			toolNames[i] = tool.Name
		}

		desc += fmt.Sprintf(" This agent has access to tools: %s.", strings.Join(toolNames, ", "))
	}

	return desc
}

// toolHandler is the handler function when the agent is used as a tool.
func (a *Agent) toolHandler(ctx context.Context, params map[string]any) (any, error) {
	input, ok := params["input"].(string)
	if !ok {
		return nil, errors.New("input parameter is required and must be a string")
	}

	// Apply context if provided
	if contextData, ok := params["context"].(map[string]any); ok {
		for k, v := range contextData {
			a.SetContext(k, v)
		}
	}

	// Execute the agent
	response, err := a.Execute(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("agent %s execution failed: %w", a.Name, err)
	}

	// Return structured result
	return map[string]any{
		"content":    response.Content,
		"iterations": response.Iterations,
		"tool_calls": len(response.ToolCalls),
		"metadata":   response.Metadata,
	}, nil
}

// AsLLMTool converts this agent into an llm.Tool format.
func (a *Agent) AsLLMTool() llm.Tool {
	sdkTool := a.AsTool()

	return llm.Tool{
		Type: "function",
		Function: &llm.FunctionDefinition{
			Name:        sdkTool.Name,
			Description: sdkTool.Description,
			Parameters:  sdkTool.Parameters,
		},
	}
}

// GetTools returns the agent's available tools.
func (a *Agent) GetTools() []Tool {
	return a.tools
}

// AddTool adds a tool to the agent.
func (a *Agent) AddTool(tool Tool) {
	a.tools = append(a.tools, tool)
}

// AddTools adds multiple tools to the agent.
func (a *Agent) AddTools(tools ...Tool) {
	a.tools = append(a.tools, tools...)
}

// SetSystemPrompt updates the agent's system prompt.
func (a *Agent) SetSystemPrompt(prompt string) {
	a.systemPrompt = prompt
}

// EnableStructuredResponse enables parsing responses into structured content parts.
func (a *Agent) EnableStructuredResponse(enabled bool) {
	a.parseStructured = enabled
	if enabled && a.responseParser == nil {
		a.responseParser = NewResponseParser()
	}
}

// SetResponseParser sets a custom response parser.
func (a *Agent) SetResponseParser(parser *ResponseParser) {
	a.responseParser = parser
	a.parseStructured = true
}

// SetArtifactRegistry sets the artifact registry for storing artifacts.
func (a *Agent) SetArtifactRegistry(registry *ArtifactRegistry) {
	a.artifactRegistry = registry
}

// SetCitationManager sets the citation manager for tracking citations.
func (a *Agent) SetCitationManager(manager *CitationManager) {
	a.citationManager = manager
}

// SetSuggestionManager sets the suggestion manager for generating follow-up suggestions.
func (a *Agent) SetSuggestionManager(manager *SuggestionManager) {
	a.suggestionManager = manager
}

// parseToStructuredResponse parses the response content into a structured format.
func (a *Agent) parseToStructuredResponse(response *AgentResponse) *StructuredResponse {
	if a.responseParser == nil {
		return nil
	}

	builder := NewResponseBuilder().
		WithMetadata(a.Model, a.Provider, time.Duration(response.Metadata["duration"].(int64))*time.Millisecond)

	// Parse content into parts
	parts := a.responseParser.Parse(response.Content)
	for _, part := range parts {
		builder.AddPart(part)

		// Extract artifacts from code blocks
		if a.artifactRegistry != nil {
			if codePart, ok := part.(*CodePart); ok {
				artifact := NewCodeArtifact(
					fmt.Sprintf("code_%d", time.Now().UnixNano()),
					codePart.Language,
					codePart.Code,
				)
				if err := a.artifactRegistry.Create(artifact); err == nil {
					response.Artifacts = append(response.Artifacts, *artifact)
					builder.AddArtifact(*artifact)
				}
			}
		}
	}

	// Add citations if available
	if a.citationManager != nil {
		for _, citation := range a.citationManager.GetCitations() {
			builder.AddCitation(citation)
			response.Citations = append(response.Citations, citation)
		}
	}

	return builder.Build()
}

// generateSuggestions generates follow-up suggestions based on the response.
func (a *Agent) generateSuggestions(ctx context.Context, response *AgentResponse, query string) []Suggestion {
	if a.suggestionManager == nil {
		return nil
	}

	input := SuggestionInput{
		Content: response.Content,
		Query:   query,
		Context: response.Metadata,
	}

	return a.suggestionManager.GenerateSuggestions(ctx, input)
}
