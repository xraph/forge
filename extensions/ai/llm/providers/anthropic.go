package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/ai/llm"
)

// AnthropicProvider implements LLM provider for Anthropic.
type AnthropicProvider struct {
	name      string
	apiKey    string
	baseURL   string
	version   string
	client    *http.Client
	models    []string
	usage     llm.LLMUsage
	logger    forge.Logger
	metrics   forge.Metrics
	rateLimit *RateLimiter
}

// AnthropicConfig contains configuration for Anthropic provider.
type AnthropicConfig struct {
	APIKey     string        `env:"ANTHROPIC_API_KEY"             yaml:"api_key"`
	BaseURL    string        `default:"https://api.anthropic.com" yaml:"base_url"`
	Version    string        `default:"2023-06-01"                yaml:"version"`
	Timeout    time.Duration `default:"30s"                       yaml:"timeout"`
	MaxRetries int           `default:"3"                         yaml:"max_retries"`
	RateLimit  *RateLimit    `yaml:"rate_limit"`
}

// Anthropic API structures.
type anthropicChatRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	TopK        *int               `json:"top_k,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Stop        []string           `json:"stop_sequences,omitempty"`
	System      string             `json:"system,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	ToolChoice  any                `json:"tool_choice,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type       string               `json:"type"`
	Text       string               `json:"text,omitempty"`
	ToolUse    *anthropicToolUse    `json:"tool_use,omitempty"`
	ToolResult *anthropicToolResult `json:"tool_result,omitempty"`
}

type anthropicToolUse struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type anthropicToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []anthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence string             `json:"stop_sequence"`
	Usage        *anthropicUsage    `json:"usage,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(config AnthropicConfig, logger forge.Logger, metrics forge.Metrics) (*AnthropicProvider, error) {
	if config.APIKey == "" {
		return nil, errors.New("Anthropic API key is required")
	}

	if config.BaseURL == "" {
		config.BaseURL = "https://api.anthropic.com"
	}

	if config.Version == "" {
		config.Version = "2023-06-01"
	}

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	var rateLimiter *RateLimiter
	if config.RateLimit != nil {
		rateLimiter = &RateLimiter{
			requestsPerMinute: config.RateLimit.RequestsPerMinute,
			tokensPerMinute:   config.RateLimit.TokensPerMinute,
			requestTokens:     make([]time.Time, 0),
			tokenUsage:        make([]TokenUsage, 0),
		}
	}

	return &AnthropicProvider{
		name:    "anthropic",
		apiKey:  config.APIKey,
		baseURL: config.BaseURL,
		version: config.Version,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		models: []string{
			"claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022",
			"claude-3-opus-20240229", "claude-3-sonnet-20240229", "claude-3-haiku-20240307",
			"claude-2.1", "claude-2.0", "claude-instant-1.2",
		},
		usage:     llm.LLMUsage{LastReset: time.Now()},
		logger:    logger,
		metrics:   metrics,
		rateLimit: rateLimiter,
	}, nil
}

// Name returns the provider name.
func (p *AnthropicProvider) Name() string {
	return p.name
}

// Models returns the available models.
func (p *AnthropicProvider) Models() []string {
	return p.models
}

// Chat performs a chat completion request.
func (p *AnthropicProvider) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	if err := p.checkRateLimit(ctx, request.Model); err != nil {
		return llm.ChatResponse{}, err
	}

	// Convert request to Anthropic format
	anthropicReq := p.convertChatRequest(request)

	// Make API request
	response, err := p.makeRequest(ctx, "/v1/messages", anthropicReq)
	if err != nil {
		return llm.ChatResponse{}, err
	}

	// Convert response
	return p.convertChatResponse(response, request.RequestID)
}

// Complete performs a text completion request (not supported by Anthropic).
func (p *AnthropicProvider) Complete(ctx context.Context, request llm.CompletionRequest) (llm.CompletionResponse, error) {
	// Convert completion to chat format
	chatRequest := llm.ChatRequest{
		Provider: request.Provider,
		Model:    request.Model,
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: request.Prompt,
			},
		},
		Temperature: request.Temperature,
		MaxTokens:   request.MaxTokens,
		TopP:        request.TopP,
		TopK:        request.TopK,
		Stop:        request.Stop,
		Stream:      request.Stream,
		Context:     request.Context,
		Metadata:    request.Metadata,
		RequestID:   request.RequestID,
	}

	chatResponse, err := p.Chat(ctx, chatRequest)
	if err != nil {
		return llm.CompletionResponse{}, err
	}

	// Convert chat response to completion response
	return p.convertToCompletionResponse(chatResponse), nil
}

// Embed performs an embedding request (not supported by Anthropic).
func (p *AnthropicProvider) Embed(ctx context.Context, request llm.EmbeddingRequest) (llm.EmbeddingResponse, error) {
	return llm.EmbeddingResponse{}, errors.New("embedding not supported by Anthropic provider")
}

// GetUsage returns current usage statistics.
func (p *AnthropicProvider) GetUsage() llm.LLMUsage {
	return p.usage
}

// HealthCheck performs a health check.
func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	// Simple health check - try to make a minimal request
	testRequest := llm.ChatRequest{
		Model: "claude-3-haiku-20240307",
		Messages: []llm.ChatMessage{
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: &[]int{5}[0],
	}

	_, err := p.Chat(ctx, testRequest)

	return err
}

// makeRequest makes an HTTP request to the Anthropic API.
func (p *AnthropicProvider) makeRequest(ctx context.Context, endpoint string, payload any) (*anthropicResponse, error) {
	// Serialize payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", p.apiKey)
	req.Header.Set("Anthropic-Version", p.version)

	// Make request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var response anthropicResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Update usage statistics
	if response.Usage != nil {
		p.usage.InputTokens += int64(response.Usage.InputTokens)
		p.usage.OutputTokens += int64(response.Usage.OutputTokens)
		p.usage.TotalTokens += int64(response.Usage.InputTokens + response.Usage.OutputTokens)
		p.usage.RequestCount++
	}

	return &response, nil
}

// convertChatRequest converts a chat request to Anthropic format.
func (p *AnthropicProvider) convertChatRequest(request llm.ChatRequest) *anthropicChatRequest {
	anthropicReq := &anthropicChatRequest{
		Model:       request.Model,
		MaxTokens:   1000, // Default max tokens
		Temperature: request.Temperature,
		TopP:        request.TopP,
		TopK:        request.TopK,
		Stream:      request.Stream,
		Stop:        request.Stop,
	}

	if request.MaxTokens != nil {
		anthropicReq.MaxTokens = *request.MaxTokens
	}

	// Convert messages and extract system message
	anthropicReq.Messages = make([]anthropicMessage, 0)
	for _, msg := range request.Messages {
		if msg.Role == "system" {
			anthropicReq.System = msg.Content

			continue
		}

		anthropicMsg := anthropicMessage{
			Role:    msg.Role,
			Content: []anthropicContent{{Type: "text", Text: msg.Content}},
		}

		// Convert tool calls
		if len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				if toolCall.Function != nil {
					anthropicMsg.Content = append(anthropicMsg.Content, anthropicContent{
						Type: "tool_use",
						ToolUse: &anthropicToolUse{
							ID:    toolCall.ID,
							Name:  toolCall.Function.Name,
							Input: map[string]any{},
						},
					})
				}
			}
		}

		anthropicReq.Messages = append(anthropicReq.Messages, anthropicMsg)
	}

	// Convert tools
	if len(request.Tools) > 0 {
		anthropicReq.Tools = make([]anthropicTool, len(request.Tools))
		for i, tool := range request.Tools {
			if tool.Function != nil {
				anthropicReq.Tools[i] = anthropicTool{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					InputSchema: tool.Function.Parameters,
				}
			}
		}
	}

	return anthropicReq
}

// convertChatResponse converts Anthropic chat response to standard format.
func (p *AnthropicProvider) convertChatResponse(response *anthropicResponse, requestID string) (llm.ChatResponse, error) {
	chatResponse := llm.ChatResponse{
		ID:        response.ID,
		Object:    "chat.completion",
		Created:   time.Now().Unix(),
		Model:     response.Model,
		Provider:  p.name,
		RequestID: requestID,
		Choices:   []llm.ChatChoice{},
	}

	// Convert content
	if len(response.Content) > 0 {
		choice := llm.ChatChoice{
			Index:        0,
			FinishReason: response.StopReason,
			Message: llm.ChatMessage{
				Role:    "assistant",
				Content: "",
			},
		}

		// Combine text content
		for _, content := range response.Content {
			if content.Type == "text" {
				choice.Message.Content += content.Text
			} else if content.Type == "tool_use" && content.ToolUse != nil {
				// Convert tool use to tool call
				choice.Message.ToolCalls = append(choice.Message.ToolCalls, llm.ToolCall{
					ID:   content.ToolUse.ID,
					Type: "function",
					Function: &llm.FunctionCall{
						Name:      content.ToolUse.Name,
						Arguments: fmt.Sprintf("%v", content.ToolUse.Input),
					},
				})
			}
		}

		chatResponse.Choices = []llm.ChatChoice{choice}
	}

	// Convert usage
	if response.Usage != nil {
		chatResponse.Usage = &llm.LLMUsage{
			InputTokens:  int64(response.Usage.InputTokens),
			OutputTokens: int64(response.Usage.OutputTokens),
			TotalTokens:  int64(response.Usage.InputTokens + response.Usage.OutputTokens),
		}
	}

	return chatResponse, nil
}

// convertToCompletionResponse converts chat response to completion response.
func (p *AnthropicProvider) convertToCompletionResponse(chatResponse llm.ChatResponse) llm.CompletionResponse {
	completionResponse := llm.CompletionResponse{
		ID:        chatResponse.ID,
		Object:    "text_completion",
		Created:   chatResponse.Created,
		Model:     chatResponse.Model,
		Provider:  chatResponse.Provider,
		RequestID: chatResponse.RequestID,
		Choices:   []llm.CompletionChoice{},
		Usage:     chatResponse.Usage,
	}

	// Convert choices
	for _, choice := range chatResponse.Choices {
		completionChoice := llm.CompletionChoice{
			Index:        choice.Index,
			Text:         choice.Message.Content,
			FinishReason: choice.FinishReason,
		}
		completionResponse.Choices = append(completionResponse.Choices, completionChoice)
	}

	return completionResponse
}

// checkRateLimit checks if the request is within rate limits.
func (p *AnthropicProvider) checkRateLimit(ctx context.Context, model string) error {
	if p.rateLimit == nil {
		return nil
	}

	now := time.Now()

	// Clean up old entries
	p.cleanupRateLimit(now)

	// Check request rate limit
	if len(p.rateLimit.requestTokens) >= p.rateLimit.requestsPerMinute {
		return errors.New("rate limit exceeded: too many requests per minute")
	}

	// Add current request
	p.rateLimit.requestTokens = append(p.rateLimit.requestTokens, now)

	return nil
}

// cleanupRateLimit removes old entries from rate limit tracking.
func (p *AnthropicProvider) cleanupRateLimit(now time.Time) {
	cutoff := now.Add(-time.Minute)

	// Clean up request tokens
	var newRequestTokens []time.Time

	for _, timestamp := range p.rateLimit.requestTokens {
		if timestamp.After(cutoff) {
			newRequestTokens = append(newRequestTokens, timestamp)
		}
	}

	p.rateLimit.requestTokens = newRequestTokens

	// Clean up token usage
	var newTokenUsage []TokenUsage

	for _, usage := range p.rateLimit.tokenUsage {
		if usage.timestamp.After(cutoff) {
			newTokenUsage = append(newTokenUsage, usage)
		}
	}

	p.rateLimit.tokenUsage = newTokenUsage
}

// IsModelSupported checks if a model is supported.
func (p *AnthropicProvider) IsModelSupported(model string) bool {
	return slices.Contains(p.models, model)
}

// GetModelInfo returns information about a model.
func (p *AnthropicProvider) GetModelInfo(model string) (map[string]any, error) {
	if !p.IsModelSupported(model) {
		return nil, fmt.Errorf("model %s is not supported", model)
	}

	info := map[string]any{
		"name":     model,
		"provider": p.name,
		"type":     "chat",
	}

	// Add model-specific information
	if strings.Contains(model, "claude-3") {
		info["context_window"] = 200000
		if strings.Contains(model, "opus") {
			info["max_tokens"] = 4096
		} else if strings.Contains(model, "sonnet") {
			info["max_tokens"] = 4096
		} else if strings.Contains(model, "haiku") {
			info["max_tokens"] = 4096
		}
	} else if strings.Contains(model, "claude-2") {
		info["context_window"] = 100000
		info["max_tokens"] = 4096
	}

	return info, nil
}

// Anthropic streaming event types.
const (
	anthropicEventMessageStart      = "message_start"
	anthropicEventContentBlockStart = "content_block_start"
	anthropicEventContentBlockDelta = "content_block_delta"
	anthropicEventContentBlockStop  = "content_block_stop"
	anthropicEventMessageDelta      = "message_delta"
	anthropicEventMessageStop       = "message_stop"
	anthropicEventPing              = "ping"
	anthropicEventError             = "error"
)

// Anthropic content block types.
const (
	anthropicBlockTypeThinking = "thinking"
	anthropicBlockTypeText     = "text"
	anthropicBlockTypeToolUse  = "tool_use"
)

// anthropicStreamEvent represents a streaming event from Anthropic API.
type anthropicStreamEvent struct {
	Type         string          `json:"type"`
	Index        int             `json:"index,omitempty"`
	ContentBlock json.RawMessage `json:"content_block,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	Error        *anthropicError `json:"error,omitempty"`
	Usage        *anthropicUsage `json:"usage,omitempty"`
}

// anthropicContentBlock represents a content block in Anthropic streaming.
type anthropicContentBlock struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
}

// anthropicDelta represents a delta in Anthropic streaming.
type anthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

// anthropicError represents an error in Anthropic streaming.
type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// anthropicStreamState tracks state during streaming.
type anthropicStreamState struct {
	currentBlockType  string
	currentBlockIndex int
	currentToolID     string
	currentToolName   string
	messageID         string
	model             string
}

// ChatStream performs a streaming chat completion request.
// This implements the StreamingProvider interface with support for Anthropic's
// extended thinking feature and proper content block handling.
func (p *AnthropicProvider) ChatStream(ctx context.Context, request llm.ChatRequest, handler func(llm.ChatStreamEvent) error) error {
	start := time.Now()

	// Convert request and set stream: true
	anthropicReq := p.convertChatRequest(request)
	anthropicReq.Stream = true

	// Serialize payload
	jsonData, err := json.Marshal(anthropicReq)
	if err != nil {
		return fmt.Errorf("failed to marshal streaming request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create streaming request: %w", err)
	}

	// Set headers for SSE
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("X-Api-Key", p.apiKey)
	req.Header.Set("Anthropic-Version", p.version)

	// Execute request
	resp, err := p.client.Do(req)
	if err != nil {
		p.updateStreamMetrics(0, time.Since(start), true)
		return fmt.Errorf("streaming request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.updateStreamMetrics(0, time.Since(start), true)
		return fmt.Errorf("streaming request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Process SSE stream
	var totalTokens int
	state := &anthropicStreamState{}
	scanner := bufio.NewScanner(resp.Body)

	// Increase buffer size for larger chunks
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var eventType string
	var eventData string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Skip empty lines (event separator)
		if line == "" {
			// Process accumulated event
			if eventType != "" && eventData != "" {
				event, tokens, err := p.parseAnthropicStreamEvent(eventType, eventData, request.RequestID, state)
				if err != nil {
					if p.logger != nil {
						p.logger.Warn("Failed to parse stream event",
							forge.F("error", err.Error()),
							forge.F("event_type", eventType),
						)
					}
				} else if event != nil {
					totalTokens += tokens
					if err := handler(*event); err != nil {
						return err
					}
				}

				// Check if this was the final message
				if eventType == anthropicEventMessageStop {
					// Send done event
					doneEvent := llm.ChatStreamEvent{
						Type:      "done",
						RequestID: request.RequestID,
						Provider:  p.name,
						Model:     state.model,
					}
					if err := handler(doneEvent); err != nil {
						return err
					}
				}
			}
			eventType = ""
			eventData = ""
			continue
		}

		// Parse SSE lines
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			eventData = strings.TrimPrefix(line, "data: ")
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		p.updateStreamMetrics(totalTokens, time.Since(start), true)
		return fmt.Errorf("stream read error: %w", err)
	}

	// Update metrics
	p.updateStreamMetrics(totalTokens, time.Since(start), false)

	return nil
}

// parseAnthropicStreamEvent parses an Anthropic streaming event into a ChatStreamEvent.
func (p *AnthropicProvider) parseAnthropicStreamEvent(eventType, data, requestID string, state *anthropicStreamState) (*llm.ChatStreamEvent, int, error) {
	var streamEvent anthropicStreamEvent
	if err := json.Unmarshal([]byte(data), &streamEvent); err != nil {
		return nil, 0, fmt.Errorf("failed to parse event: %w", err)
	}

	tokens := 0

	switch eventType {
	case anthropicEventPing:
		// Ignore ping events
		return nil, 0, nil

	case anthropicEventMessageStart:
		// Extract message info
		if streamEvent.Message != nil {
			var msg struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			}
			if err := json.Unmarshal(streamEvent.Message, &msg); err == nil {
				state.messageID = msg.ID
				state.model = msg.Model
			}
		}
		return nil, 0, nil

	case anthropicEventContentBlockStart:
		// Parse content block
		var block anthropicContentBlock
		if err := json.Unmarshal(streamEvent.ContentBlock, &block); err != nil {
			return nil, 0, fmt.Errorf("failed to parse content block: %w", err)
		}

		state.currentBlockType = block.Type
		state.currentBlockIndex = streamEvent.Index

		// Build event with block start info
		event := &llm.ChatStreamEvent{
			Type:       "message",
			ID:         state.messageID,
			Model:      state.model,
			Provider:   p.name,
			RequestID:  requestID,
			BlockType:  block.Type,
			BlockIndex: streamEvent.Index,
			BlockState: string(llm.BlockStateStart),
			Choices: []llm.ChatChoice{{
				Index: 0,
				Delta: &llm.ChatMessage{
					Role: "assistant",
				},
			}},
		}

		// Handle tool use start
		if block.Type == anthropicBlockTypeToolUse {
			state.currentToolID = block.ID
			state.currentToolName = block.Name
			event.Type = "tool_call"
			event.Choices[0].Delta.ToolCalls = []llm.ToolCall{{
				ID:   block.ID,
				Type: "function",
				Function: &llm.FunctionCall{
					Name: block.Name,
				},
			}}
		}

		return event, 0, nil

	case anthropicEventContentBlockDelta:
		// Parse delta
		var delta anthropicDelta
		if err := json.Unmarshal(streamEvent.Delta, &delta); err != nil {
			return nil, 0, fmt.Errorf("failed to parse delta: %w", err)
		}

		event := &llm.ChatStreamEvent{
			Type:       "message",
			ID:         state.messageID,
			Model:      state.model,
			Provider:   p.name,
			RequestID:  requestID,
			BlockType:  state.currentBlockType,
			BlockIndex: state.currentBlockIndex,
			BlockState: string(llm.BlockStateDelta),
			Choices: []llm.ChatChoice{{
				Index: 0,
				Delta: &llm.ChatMessage{
					Role: "assistant",
				},
			}},
		}

		// Handle different delta types
		switch delta.Type {
		case "thinking_delta":
			// Extended thinking content
			event.Choices[0].Delta.Content = delta.Thinking
			event.BlockType = anthropicBlockTypeThinking

		case "text_delta":
			// Regular text content
			event.Choices[0].Delta.Content = delta.Text

		case "input_json_delta":
			// Tool use arguments (partial JSON)
			event.Type = "tool_call"
			event.Choices[0].Delta.ToolCalls = []llm.ToolCall{{
				ID:   state.currentToolID,
				Type: "function",
				Function: &llm.FunctionCall{
					Name:      state.currentToolName,
					Arguments: delta.PartialJSON,
				},
			}}
		}

		return event, 0, nil

	case anthropicEventContentBlockStop:
		// Build block stop event
		event := &llm.ChatStreamEvent{
			Type:       "message",
			ID:         state.messageID,
			Model:      state.model,
			Provider:   p.name,
			RequestID:  requestID,
			BlockType:  state.currentBlockType,
			BlockIndex: state.currentBlockIndex,
			BlockState: string(llm.BlockStateStop),
			Choices: []llm.ChatChoice{{
				Index: 0,
				Delta: &llm.ChatMessage{
					Role: "assistant",
				},
			}},
		}

		// Reset block state
		state.currentBlockType = ""
		state.currentToolID = ""
		state.currentToolName = ""

		return event, 0, nil

	case anthropicEventMessageDelta:
		// Parse message delta for usage info
		var delta anthropicDelta
		if err := json.Unmarshal(streamEvent.Delta, &delta); err != nil {
			return nil, 0, fmt.Errorf("failed to parse message delta: %w", err)
		}

		event := &llm.ChatStreamEvent{
			Type:      "message",
			ID:        state.messageID,
			Model:     state.model,
			Provider:  p.name,
			RequestID: requestID,
			Choices: []llm.ChatChoice{{
				Index:        0,
				FinishReason: delta.StopReason,
			}},
		}

		// Add usage if available
		if streamEvent.Usage != nil {
			tokens = streamEvent.Usage.InputTokens + streamEvent.Usage.OutputTokens
			event.Usage = &llm.LLMUsage{
				InputTokens:  int64(streamEvent.Usage.InputTokens),
				OutputTokens: int64(streamEvent.Usage.OutputTokens),
				TotalTokens:  int64(tokens),
			}
		}

		return event, tokens, nil

	case anthropicEventMessageStop:
		// Message complete - the caller will send the done event
		return nil, 0, nil

	case anthropicEventError:
		if streamEvent.Error != nil {
			return &llm.ChatStreamEvent{
				Type:      "error",
				Error:     streamEvent.Error.Message,
				Provider:  p.name,
				RequestID: requestID,
			}, 0, nil
		}
		return nil, 0, nil
	}

	return nil, 0, nil
}

// updateStreamMetrics updates metrics for streaming requests.
func (p *AnthropicProvider) updateStreamMetrics(tokens int, latency time.Duration, isError bool) {
	p.usage.RequestCount++

	if tokens > 0 {
		p.usage.TotalTokens += int64(tokens)
	}

	if isError {
		p.usage.ErrorCount++
	}

	// Update metrics if available
	if p.metrics != nil {
		p.metrics.Counter("forge.ai.llm.provider.stream_requests_total", "provider", p.name).Inc()
		p.metrics.Histogram("forge.ai.llm.provider.stream_duration", "provider", p.name).Observe(latency.Seconds())

		if isError {
			p.metrics.Counter("forge.ai.llm.provider.stream_errors_total", "provider", p.name).Inc()
		}
	}
}

// Ensure AnthropicProvider implements StreamingProvider interface.
var _ llm.StreamingProvider = (*AnthropicProvider)(nil)
