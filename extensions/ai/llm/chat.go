package llm

import (
	"context"
	"time"
)

// ChatRequest represents a chat completion request.
type ChatRequest struct {
	Provider    string         `json:"provider"`
	Model       string         `json:"model"`
	Messages    []ChatMessage  `json:"messages"`
	Temperature *float64       `json:"temperature,omitempty"`
	MaxTokens   *int           `json:"max_tokens,omitempty"`
	TopP        *float64       `json:"top_p,omitempty"`
	TopK        *int           `json:"top_k,omitempty"`
	Stop        []string       `json:"stop,omitempty"`
	Stream      bool           `json:"stream"`
	Tools       []Tool         `json:"tools,omitempty"`
	ToolChoice  string         `json:"tool_choice,omitempty"`
	Context     map[string]any `json:"context"`
	Metadata    map[string]any `json:"metadata"`
	RequestID   string         `json:"request_id"`
}

// ChatResponse represents a chat completion response.
type ChatResponse struct {
	ID        string         `json:"id"`
	Object    string         `json:"object"`
	Created   int64          `json:"created"`
	Model     string         `json:"model"`
	Provider  string         `json:"provider"`
	Choices   []ChatChoice   `json:"choices"`
	Usage     *LLMUsage      `json:"usage,omitempty"`
	Metadata  map[string]any `json:"metadata"`
	RequestID string         `json:"request_id"`
}

// ChatChoice represents a chat completion choice.
type ChatChoice struct {
	Index        int          `json:"index"`
	Message      ChatMessage  `json:"message"`
	Delta        *ChatMessage `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason"`
	LogProbs     *LogProbs    `json:"logprobs,omitempty"`
}

// ChatMessage represents a message in a chat conversation.
type ChatMessage struct {
	Role         string         `json:"role"` // system, user, assistant, tool
	Content      string         `json:"content"`
	Name         string         `json:"name,omitempty"`
	ToolCalls    []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID   string         `json:"tool_call_id,omitempty"`
	FunctionCall *FunctionCall  `json:"function_call,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Tool represents a tool that can be called by the LLM.
type Tool struct {
	Type     string              `json:"type"` // function, code_interpreter, retrieval
	Function *FunctionDefinition `json:"function,omitempty"`
	Config   map[string]any      `json:"config,omitempty"`
}

// FunctionDefinition represents a function definition.
type FunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Required    []string       `json:"required,omitempty"`
	Examples    []any          `json:"examples,omitempty"`
}

// ToolCall represents a tool call made by the LLM.
type ToolCall struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function *FunctionCall `json:"function,omitempty"`
	Result   any           `json:"result,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// FunctionCall represents a function call.
type FunctionCall struct {
	Name      string         `json:"name"`
	Arguments string         `json:"arguments"`
	Parsed    map[string]any `json:"parsed,omitempty"`
}

// LogProbs represents log probabilities for tokens.
type LogProbs struct {
	Tokens        []string             `json:"tokens"`
	TokenLogProbs []float64            `json:"token_logprobs"`
	TopLogProbs   []map[string]float64 `json:"top_logprobs"`
}

// ChatStreamEvent represents a streaming chat event.
type ChatStreamEvent struct {
	Type      string         `json:"type"` // message, tool_call, function_call, error, done
	ID        string         `json:"id"`
	Object    string         `json:"object"`
	Created   int64          `json:"created"`
	Model     string         `json:"model"`
	Provider  string         `json:"provider"`
	Choices   []ChatChoice   `json:"choices"`
	Usage     *LLMUsage      `json:"usage,omitempty"`
	Error     string         `json:"error,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	RequestID string         `json:"request_id"`

	// Block-level streaming support (for providers like Anthropic)
	// BlockType indicates the type of content block (thinking, text, tool_use)
	BlockType string `json:"block_type,omitempty"`
	// BlockIndex is the index of the content block within the message
	BlockIndex int `json:"block_index,omitempty"`
	// BlockState indicates the state of the block (start, delta, stop)
	BlockState string `json:"block_state,omitempty"`
}

// ChatStreamHandler handles streaming chat events.
type ChatStreamHandler func(event ChatStreamEvent) error

// ChatSession represents a chat session with conversation history.
type ChatSession struct {
	ID        string         `json:"id"`
	Messages  []ChatMessage  `json:"messages"`
	Model     string         `json:"model"`
	Provider  string         `json:"provider"`
	Settings  ChatSettings   `json:"settings"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// ChatSettings represents settings for a chat session.
type ChatSettings struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	TopK             *int     `json:"top_k,omitempty"`
	Stop             []string `json:"stop,omitempty"`
	SystemPrompt     string   `json:"system_prompt,omitempty"`
	MaxHistoryLength int      `json:"max_history_length,omitempty"`
	ContextWindow    int      `json:"context_window,omitempty"`
	RetainSystemMsg  bool     `json:"retain_system_msg"`
	EnableTools      bool     `json:"enable_tools"`
	EnableFunctions  bool     `json:"enable_functions"`
}

// NewChatSession creates a new chat session.
func NewChatSession(id, model, provider string) *ChatSession {
	return &ChatSession{
		ID:        id,
		Messages:  make([]ChatMessage, 0),
		Model:     model,
		Provider:  provider,
		Settings:  ChatSettings{},
		Metadata:  make(map[string]any),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// AddMessage adds a message to the chat session.
func (s *ChatSession) AddMessage(message ChatMessage) {
	s.Messages = append(s.Messages, message)
	s.UpdatedAt = time.Now()

	// Trim history if necessary
	if s.Settings.MaxHistoryLength > 0 && len(s.Messages) > s.Settings.MaxHistoryLength {
		// Keep system message if configured
		if s.Settings.RetainSystemMsg && len(s.Messages) > 0 && s.Messages[0].Role == "system" {
			systemMsg := s.Messages[0]
			s.Messages = append([]ChatMessage{systemMsg}, s.Messages[len(s.Messages)-s.Settings.MaxHistoryLength+1:]...)
		} else {
			s.Messages = s.Messages[len(s.Messages)-s.Settings.MaxHistoryLength:]
		}
	}
}

// GetMessages returns all messages in the session.
func (s *ChatSession) GetMessages() []ChatMessage {
	return s.Messages
}

// GetLastMessage returns the last message in the session.
func (s *ChatSession) GetLastMessage() *ChatMessage {
	if len(s.Messages) == 0 {
		return nil
	}

	return &s.Messages[len(s.Messages)-1]
}

// GetMessageCount returns the number of messages in the session.
func (s *ChatSession) GetMessageCount() int {
	return len(s.Messages)
}

// EstimateTokens estimates the number of tokens in the session.
func (s *ChatSession) EstimateTokens() int {
	totalTokens := 0
	for _, msg := range s.Messages {
		// Simple estimation: ~4 characters per token
		totalTokens += len(msg.Content) / 4
		if msg.Name != "" {
			totalTokens += len(msg.Name) / 4
		}
		// Add overhead for message structure
		totalTokens += 10
	}

	return totalTokens
}

// IsWithinContextWindow checks if the session is within the context window.
func (s *ChatSession) IsWithinContextWindow() bool {
	if s.Settings.ContextWindow <= 0 {
		return true
	}

	return s.EstimateTokens() <= s.Settings.ContextWindow
}

// Clear clears all messages from the session (except system message if configured).
func (s *ChatSession) Clear() {
	if s.Settings.RetainSystemMsg && len(s.Messages) > 0 && s.Messages[0].Role == "system" {
		s.Messages = s.Messages[:1]
	} else {
		s.Messages = make([]ChatMessage, 0)
	}

	s.UpdatedAt = time.Now()
}

// SetSystemPrompt sets the system prompt for the session.
func (s *ChatSession) SetSystemPrompt(prompt string) {
	s.Settings.SystemPrompt = prompt

	// Update or add system message
	if len(s.Messages) > 0 && s.Messages[0].Role == "system" {
		s.Messages[0].Content = prompt
	} else {
		systemMsg := ChatMessage{
			Role:    "system",
			Content: prompt,
		}
		s.Messages = append([]ChatMessage{systemMsg}, s.Messages...)
	}

	s.UpdatedAt = time.Now()
}

// GetSystemPrompt returns the system prompt.
func (s *ChatSession) GetSystemPrompt() string {
	return s.Settings.SystemPrompt
}

// ToRequest converts the session to a chat request.
func (s *ChatSession) ToRequest() ChatRequest {
	return ChatRequest{
		Provider:    s.Provider,
		Model:       s.Model,
		Messages:    s.Messages,
		Temperature: s.Settings.Temperature,
		MaxTokens:   s.Settings.MaxTokens,
		TopP:        s.Settings.TopP,
		TopK:        s.Settings.TopK,
		Stop:        s.Settings.Stop,
		Context:     s.Metadata,
		RequestID:   s.ID,
	}
}

// ChatBuilder provides a fluent interface for building chat requests.
type ChatBuilder struct {
	request ChatRequest
}

// NewChatBuilder creates a new chat builder.
func NewChatBuilder() *ChatBuilder {
	return &ChatBuilder{
		request: ChatRequest{
			Messages: make([]ChatMessage, 0),
			Context:  make(map[string]any),
			Metadata: make(map[string]any),
		},
	}
}

// WithProvider sets the provider.
func (b *ChatBuilder) WithProvider(provider string) *ChatBuilder {
	b.request.Provider = provider

	return b
}

// WithModel sets the model.
func (b *ChatBuilder) WithModel(model string) *ChatBuilder {
	b.request.Model = model

	return b
}

// WithTemperature sets the temperature.
func (b *ChatBuilder) WithTemperature(temperature float64) *ChatBuilder {
	b.request.Temperature = &temperature

	return b
}

// WithMaxTokens sets the maximum tokens.
func (b *ChatBuilder) WithMaxTokens(maxTokens int) *ChatBuilder {
	b.request.MaxTokens = &maxTokens

	return b
}

// WithTopP sets the top-p parameter.
func (b *ChatBuilder) WithTopP(topP float64) *ChatBuilder {
	b.request.TopP = &topP

	return b
}

// WithTopK sets the top-k parameter.
func (b *ChatBuilder) WithTopK(topK int) *ChatBuilder {
	b.request.TopK = &topK

	return b
}

// WithStop sets the stop sequences.
func (b *ChatBuilder) WithStop(stop ...string) *ChatBuilder {
	b.request.Stop = stop

	return b
}

// WithStream enables or disables streaming.
func (b *ChatBuilder) WithStream(stream bool) *ChatBuilder {
	b.request.Stream = stream

	return b
}

// WithSystemMessage adds a system message.
func (b *ChatBuilder) WithSystemMessage(content string) *ChatBuilder {
	b.request.Messages = append(b.request.Messages, ChatMessage{
		Role:    "system",
		Content: content,
	})

	return b
}

// WithUserMessage adds a user message.
func (b *ChatBuilder) WithUserMessage(content string) *ChatBuilder {
	b.request.Messages = append(b.request.Messages, ChatMessage{
		Role:    "user",
		Content: content,
	})

	return b
}

// WithAssistantMessage adds an assistant message.
func (b *ChatBuilder) WithAssistantMessage(content string) *ChatBuilder {
	b.request.Messages = append(b.request.Messages, ChatMessage{
		Role:    "assistant",
		Content: content,
	})

	return b
}

// WithMessage adds a custom message.
func (b *ChatBuilder) WithMessage(message ChatMessage) *ChatBuilder {
	b.request.Messages = append(b.request.Messages, message)

	return b
}

// WithMessages adds multiple messages.
func (b *ChatBuilder) WithMessages(messages []ChatMessage) *ChatBuilder {
	b.request.Messages = append(b.request.Messages, messages...)

	return b
}

// WithTools adds tools.
func (b *ChatBuilder) WithTools(tools []Tool) *ChatBuilder {
	b.request.Tools = tools

	return b
}

// WithTool adds a single tool.
func (b *ChatBuilder) WithTool(tool Tool) *ChatBuilder {
	b.request.Tools = append(b.request.Tools, tool)

	return b
}

// WithToolChoice sets the tool choice.
func (b *ChatBuilder) WithToolChoice(choice string) *ChatBuilder {
	b.request.ToolChoice = choice

	return b
}

// WithContext adds context data.
func (b *ChatBuilder) WithContext(key string, value any) *ChatBuilder {
	b.request.Context[key] = value

	return b
}

// WithMetadata adds metadata.
func (b *ChatBuilder) WithMetadata(key string, value any) *ChatBuilder {
	b.request.Metadata[key] = value

	return b
}

// WithRequestID sets the request ID.
func (b *ChatBuilder) WithRequestID(id string) *ChatBuilder {
	b.request.RequestID = id

	return b
}

// Build returns the built chat request.
func (b *ChatBuilder) Build() ChatRequest {
	return b.request
}

// Execute executes the chat request using the provided LLM manager.
func (b *ChatBuilder) Execute(ctx context.Context, manager *LLMManager) (ChatResponse, error) {
	return manager.Chat(ctx, b.request)
}

// CreateFunction creates a function definition.
func CreateFunction(name, description string, parameters map[string]any) Tool {
	return Tool{
		Type: "function",
		Function: &FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}

// CreateFunctionWithRequired creates a function definition with required parameters.
func CreateFunctionWithRequired(name, description string, parameters map[string]any, required []string) Tool {
	return Tool{
		Type: "function",
		Function: &FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  parameters,
			Required:    required,
		},
	}
}
