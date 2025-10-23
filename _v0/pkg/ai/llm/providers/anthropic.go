package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/ai/llm"
	"github.com/xraph/forge/v0/pkg/common"
)

// AnthropicProvider implements LLM provider for Anthropic
type AnthropicProvider struct {
	name      string
	apiKey    string
	baseURL   string
	version   string
	client    *http.Client
	models    []string
	usage     llm.LLMUsage
	logger    common.Logger
	metrics   common.Metrics
	rateLimit *RateLimiter
}

// AnthropicConfig contains configuration for Anthropic provider
type AnthropicConfig struct {
	APIKey     string        `yaml:"api_key" env:"ANTHROPIC_API_KEY"`
	BaseURL    string        `yaml:"base_url" default:"https://api.anthropic.com"`
	Version    string        `yaml:"version" default:"2023-06-01"`
	Timeout    time.Duration `yaml:"timeout" default:"30s"`
	MaxRetries int           `yaml:"max_retries" default:"3"`
	RateLimit  *RateLimit    `yaml:"rate_limit"`
}

// Anthropic API structures
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
	ToolChoice  interface{}        `json:"tool_choice,omitempty"`
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
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

type anthropicToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
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

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(config AnthropicConfig, logger common.Logger, metrics common.Metrics) (*AnthropicProvider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("Anthropic API key is required")
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

// Name returns the provider name
func (p *AnthropicProvider) Name() string {
	return p.name
}

// Models returns the available models
func (p *AnthropicProvider) Models() []string {
	return p.models
}

// Chat performs a chat completion request
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

// Complete performs a text completion request (not supported by Anthropic)
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

// Embed performs an embedding request (not supported by Anthropic)
func (p *AnthropicProvider) Embed(ctx context.Context, request llm.EmbeddingRequest) (llm.EmbeddingResponse, error) {
	return llm.EmbeddingResponse{}, fmt.Errorf("embedding not supported by Anthropic provider")
}

// GetUsage returns current usage statistics
func (p *AnthropicProvider) GetUsage() llm.LLMUsage {
	return p.usage
}

// HealthCheck performs a health check
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

// makeRequest makes an HTTP request to the Anthropic API
func (p *AnthropicProvider) makeRequest(ctx context.Context, endpoint string, payload interface{}) (*anthropicResponse, error) {
	// Serialize payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", p.version)

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

// convertChatRequest converts a chat request to Anthropic format
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
							Input: map[string]interface{}{},
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

// convertChatResponse converts Anthropic chat response to standard format
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

// convertToCompletionResponse converts chat response to completion response
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

// checkRateLimit checks if the request is within rate limits
func (p *AnthropicProvider) checkRateLimit(ctx context.Context, model string) error {
	if p.rateLimit == nil {
		return nil
	}

	now := time.Now()

	// Clean up old entries
	p.cleanupRateLimit(now)

	// Check request rate limit
	if len(p.rateLimit.requestTokens) >= p.rateLimit.requestsPerMinute {
		return fmt.Errorf("rate limit exceeded: too many requests per minute")
	}

	// Add current request
	p.rateLimit.requestTokens = append(p.rateLimit.requestTokens, now)

	return nil
}

// cleanupRateLimit removes old entries from rate limit tracking
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

// IsModelSupported checks if a model is supported
func (p *AnthropicProvider) IsModelSupported(model string) bool {
	for _, supportedModel := range p.models {
		if supportedModel == model {
			return true
		}
	}
	return false
}

// GetModelInfo returns information about a model
func (p *AnthropicProvider) GetModelInfo(model string) (map[string]interface{}, error) {
	if !p.IsModelSupported(model) {
		return nil, fmt.Errorf("model %s is not supported", model)
	}

	info := map[string]interface{}{
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
