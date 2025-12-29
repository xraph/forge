package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// LMStudioProvider implements LLM provider for LMStudio.
// LMStudio provides an OpenAI-compatible API for local inference.
type LMStudioProvider struct {
	name    string
	apiKey  string // Optional, for auth if configured
	baseURL string
	client  *http.Client
	models  []string
	usage   llm.LLMUsage
	logger  forge.Logger
	metrics forge.Metrics
	mu      sync.RWMutex
	started bool
}

// LMStudioConfig contains configuration for LMStudio provider.
type LMStudioConfig struct {
	APIKey     string        `yaml:"api_key"` // Optional, for auth if configured
	BaseURL    string        `default:"http://localhost:1234/v1" yaml:"base_url"`
	Timeout    time.Duration `default:"60s" yaml:"timeout"` // Local inference can be slow
	MaxRetries int           `default:"2" yaml:"max_retries"`
	Models     []string      `yaml:"models"` // Pre-configured model list
	Logger     forge.Logger
	Metrics    forge.Metrics
}

// lmstudioRequest represents a request to LMStudio API (OpenAI-compatible).
type lmstudioRequest struct {
	Model            string             `json:"model"`
	Messages         []lmstudioMessage  `json:"messages,omitempty"`
	Prompt           string             `json:"prompt,omitempty"`
	MaxTokens        *int               `json:"max_tokens,omitempty"`
	Temperature      *float64           `json:"temperature,omitempty"`
	TopP             *float64           `json:"top_p,omitempty"`
	N                *int               `json:"n,omitempty"`
	Stream           bool               `json:"stream,omitempty"`
	Stop             []string           `json:"stop,omitempty"`
	PresencePenalty  *float64           `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64           `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]float64 `json:"logit_bias,omitempty"`
	User             string             `json:"user,omitempty"`
	Tools            []lmstudioTool     `json:"tools,omitempty"`
	ToolChoice       any                `json:"tool_choice,omitempty"`
	Input            any                `json:"input,omitempty"`
	EncodingFormat   string             `json:"encoding_format,omitempty"`
	Dimensions       *int               `json:"dimensions,omitempty"`
}

type lmstudioMessage struct {
	Role         string                `json:"role"`
	Content      string                `json:"content"`
	Name         string                `json:"name,omitempty"`
	ToolCalls    []lmstudioToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string                `json:"tool_call_id,omitempty"`
	FunctionCall *lmstudioFunctionCall `json:"function_call,omitempty"`
}

type lmstudioTool struct {
	Type     string                     `json:"type"`
	Function lmstudioFunctionDefinition `json:"function"`
}

type lmstudioFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type lmstudioToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function lmstudioFunctionCall `json:"function"`
}

type lmstudioFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type lmstudioResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []lmstudioChoice    `json:"choices"`
	Usage   *lmstudioUsage      `json:"usage,omitempty"`
	Data    []lmstudioEmbedding `json:"data,omitempty"`
}

type lmstudioChoice struct {
	Index        int               `json:"index"`
	Message      *lmstudioMessage  `json:"message,omitempty"`
	Text         string            `json:"text,omitempty"`
	Delta        *lmstudioMessage  `json:"delta,omitempty"`
	FinishReason string            `json:"finish_reason"`
	LogProbs     *lmstudioLogProbs `json:"logprobs,omitempty"`
}

type lmstudioLogProbs struct {
	Tokens        []string             `json:"tokens"`
	TokenLogProbs []float64            `json:"token_logprobs"`
	TopLogProbs   []map[string]float64 `json:"top_logprobs"`
}

type lmstudioUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type lmstudioEmbedding struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type lmstudioModelsResponse struct {
	Object string              `json:"object"`
	Data   []lmstudioModelInfo `json:"data"`
}

type lmstudioModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// NewLMStudioProvider creates a new LMStudio provider.
func NewLMStudioProvider(config LMStudioConfig, logger forge.Logger, metrics forge.Metrics) (*LMStudioProvider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:1234/v1"
	}

	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
	}

	provider := &LMStudioProvider{
		name:    "lmstudio",
		apiKey:  config.APIKey,
		baseURL: config.BaseURL,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		models:  config.Models,
		usage:   llm.LLMUsage{LastReset: time.Now()},
		logger:  logger,
		metrics: metrics,
		started: true,
	}

	// If no models configured, try to discover them
	if len(provider.models) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := provider.discoverModels(ctx); err != nil {
			// Log warning but don't fail initialization
			// Models can be discovered on first request
		}
	}

	return provider, nil
}

// Name returns the provider name.
func (p *LMStudioProvider) Name() string {
	return p.name
}

// Models returns the available models.
func (p *LMStudioProvider) Models() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.models
}

// Chat performs a chat completion request.
func (p *LMStudioProvider) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	start := time.Now()

	// Convert request to LMStudio format
	lmstudioReq := p.convertChatRequest(request)

	// Make API request
	response, err := p.makeRequest(ctx, "/chat/completions", lmstudioReq)
	if err != nil {
		p.updateUsageMetrics(0, time.Since(start), true)
		return llm.ChatResponse{}, p.wrapError(err, "chat completion")
	}

	// Update metrics
	tokens := 0
	if response.Usage != nil {
		tokens = response.Usage.TotalTokens
	}
	p.updateUsageMetrics(tokens, time.Since(start), false)

	// Convert response
	return p.convertChatResponse(response, request.RequestID)
}

// Complete performs a text completion request.
func (p *LMStudioProvider) Complete(ctx context.Context, request llm.CompletionRequest) (llm.CompletionResponse, error) {
	start := time.Now()

	// Convert request to LMStudio format
	lmstudioReq := p.convertCompletionRequest(request)

	// Make API request
	response, err := p.makeRequest(ctx, "/completions", lmstudioReq)
	if err != nil {
		p.updateUsageMetrics(0, time.Since(start), true)
		return llm.CompletionResponse{}, p.wrapError(err, "text completion")
	}

	// Update metrics
	tokens := 0
	if response.Usage != nil {
		tokens = response.Usage.TotalTokens
	}
	p.updateUsageMetrics(tokens, time.Since(start), false)

	// Convert response
	return p.convertCompletionResponse(response, request.RequestID)
}

// Embed performs an embedding request.
func (p *LMStudioProvider) Embed(ctx context.Context, request llm.EmbeddingRequest) (llm.EmbeddingResponse, error) {
	start := time.Now()

	// Convert request to LMStudio format
	lmstudioReq := p.convertEmbeddingRequest(request)

	// Make API request
	response, err := p.makeRequest(ctx, "/embeddings", lmstudioReq)
	if err != nil {
		p.updateUsageMetrics(0, time.Since(start), true)
		return llm.EmbeddingResponse{}, p.wrapError(err, "embedding")
	}

	// Update metrics
	tokens := 0
	if response.Usage != nil {
		tokens = response.Usage.TotalTokens
	}
	p.updateUsageMetrics(tokens, time.Since(start), false)

	// Convert response
	return p.convertEmbeddingResponse(response, request.RequestID)
}

// GetUsage returns current usage statistics.
func (p *LMStudioProvider) GetUsage() llm.LLMUsage {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.usage
}

// HealthCheck performs a health check.
func (p *LMStudioProvider) HealthCheck(ctx context.Context) error {
	// Try to list models to verify server is running
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Make request
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("LMStudio server not responding - ensure LMStudio is running and API server is enabled: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LMStudio API health check failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// discoverModels attempts to discover available models from LMStudio.
func (p *LMStudioProvider) discoverModels(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("failed to create models request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Make request
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to discover models: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read models response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var modelsResp lmstudioModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return fmt.Errorf("failed to parse models response: %w", err)
	}

	// Extract model IDs
	p.mu.Lock()
	p.models = make([]string, len(modelsResp.Data))
	for i, model := range modelsResp.Data {
		p.models[i] = model.ID
	}
	p.mu.Unlock()

	return nil
}

// makeRequest makes an HTTP request to the LMStudio API.
func (p *LMStudioProvider) makeRequest(ctx context.Context, endpoint string, payload any) (*lmstudioResponse, error) {
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
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Make request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LMStudio API request failed - ensure LMStudio is running and API server is enabled: %w", err)
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
	var response lmstudioResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Update usage statistics
	p.mu.Lock()
	if response.Usage != nil {
		p.usage.InputTokens += int64(response.Usage.PromptTokens)
		p.usage.OutputTokens += int64(response.Usage.CompletionTokens)
		p.usage.TotalTokens += int64(response.Usage.TotalTokens)
		p.usage.RequestCount++
	}
	p.mu.Unlock()

	return &response, nil
}

// convertChatRequest converts a chat request to LMStudio format.
func (p *LMStudioProvider) convertChatRequest(request llm.ChatRequest) *lmstudioRequest {
	lmstudioReq := &lmstudioRequest{
		Model:       request.Model,
		MaxTokens:   request.MaxTokens,
		Temperature: request.Temperature,
		TopP:        request.TopP,
		Stream:      request.Stream,
		Stop:        request.Stop,
	}

	// Convert messages
	lmstudioReq.Messages = make([]lmstudioMessage, len(request.Messages))
	for i, msg := range request.Messages {
		lmstudioReq.Messages[i] = lmstudioMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}

		// Convert tool calls
		if len(msg.ToolCalls) > 0 {
			lmstudioReq.Messages[i].ToolCalls = make([]lmstudioToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				lmstudioReq.Messages[i].ToolCalls[j] = lmstudioToolCall{
					ID:   tc.ID,
					Type: tc.Type,
				}
				if tc.Function != nil {
					lmstudioReq.Messages[i].ToolCalls[j].Function = lmstudioFunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					}
				}
			}
		}

		// Convert function call (legacy)
		if msg.FunctionCall != nil {
			lmstudioReq.Messages[i].FunctionCall = &lmstudioFunctionCall{
				Name:      msg.FunctionCall.Name,
				Arguments: msg.FunctionCall.Arguments,
			}
		}
	}

	// Convert tools
	if len(request.Tools) > 0 {
		lmstudioReq.Tools = make([]lmstudioTool, len(request.Tools))
		for i, tool := range request.Tools {
			lmstudioReq.Tools[i] = lmstudioTool{
				Type: tool.Type,
			}
			if tool.Function != nil {
				lmstudioReq.Tools[i].Function = lmstudioFunctionDefinition{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  tool.Function.Parameters,
				}
			}
		}
	}

	// Convert tool choice
	if request.ToolChoice != "" {
		if request.ToolChoice == "auto" || request.ToolChoice == "none" {
			lmstudioReq.ToolChoice = request.ToolChoice
		} else {
			lmstudioReq.ToolChoice = map[string]any{
				"type": "function",
				"function": map[string]string{
					"name": request.ToolChoice,
				},
			}
		}
	}

	return lmstudioReq
}

// convertCompletionRequest converts a completion request to LMStudio format.
func (p *LMStudioProvider) convertCompletionRequest(request llm.CompletionRequest) *lmstudioRequest {
	return &lmstudioRequest{
		Model:            request.Model,
		Prompt:           request.Prompt,
		MaxTokens:        request.MaxTokens,
		Temperature:      request.Temperature,
		TopP:             request.TopP,
		N:                request.N,
		Stream:           request.Stream,
		Stop:             request.Stop,
		PresencePenalty:  request.PresencePenalty,
		FrequencyPenalty: request.FrequencyPenalty,
	}
}

// convertEmbeddingRequest converts an embedding request to LMStudio format.
func (p *LMStudioProvider) convertEmbeddingRequest(request llm.EmbeddingRequest) *lmstudioRequest {
	return &lmstudioRequest{
		Model:          request.Model,
		Input:          request.Input,
		User:           request.User,
		EncodingFormat: request.EncodingFormat,
		Dimensions:     request.Dimensions,
	}
}

// convertChatResponse converts LMStudio chat response to standard format.
func (p *LMStudioProvider) convertChatResponse(response *lmstudioResponse, requestID string) (llm.ChatResponse, error) {
	chatResponse := llm.ChatResponse{
		ID:        response.ID,
		Object:    response.Object,
		Created:   response.Created,
		Model:     response.Model,
		Provider:  p.name,
		RequestID: requestID,
		Choices:   make([]llm.ChatChoice, len(response.Choices)),
	}

	// Convert choices
	for i, choice := range response.Choices {
		chatResponse.Choices[i] = llm.ChatChoice{
			Index:        choice.Index,
			FinishReason: choice.FinishReason,
		}

		// Convert message
		if choice.Message != nil {
			chatResponse.Choices[i].Message = llm.ChatMessage{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
				Name:    choice.Message.Name,
			}

			// Convert tool calls
			if len(choice.Message.ToolCalls) > 0 {
				chatResponse.Choices[i].Message.ToolCalls = make([]llm.ToolCall, len(choice.Message.ToolCalls))
				for j, tc := range choice.Message.ToolCalls {
					chatResponse.Choices[i].Message.ToolCalls[j] = llm.ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
					}
					if tc.Function.Name != "" {
						chatResponse.Choices[i].Message.ToolCalls[j].Function = &llm.FunctionCall{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						}
					}
				}
			}

			// Convert function call (legacy)
			if choice.Message.FunctionCall != nil {
				chatResponse.Choices[i].Message.FunctionCall = &llm.FunctionCall{
					Name:      choice.Message.FunctionCall.Name,
					Arguments: choice.Message.FunctionCall.Arguments,
				}
			}
		}

		// Convert delta (for streaming)
		if choice.Delta != nil {
			chatResponse.Choices[i].Delta = &llm.ChatMessage{
				Role:    choice.Delta.Role,
				Content: choice.Delta.Content,
				Name:    choice.Delta.Name,
			}
		}

		// Convert log probabilities
		if choice.LogProbs != nil {
			chatResponse.Choices[i].LogProbs = &llm.LogProbs{
				Tokens:        choice.LogProbs.Tokens,
				TokenLogProbs: choice.LogProbs.TokenLogProbs,
				TopLogProbs:   choice.LogProbs.TopLogProbs,
			}
		}
	}

	// Convert usage
	if response.Usage != nil {
		chatResponse.Usage = &llm.LLMUsage{
			InputTokens:  int64(response.Usage.PromptTokens),
			OutputTokens: int64(response.Usage.CompletionTokens),
			TotalTokens:  int64(response.Usage.TotalTokens),
		}
	}

	return chatResponse, nil
}

// convertCompletionResponse converts LMStudio completion response to standard format.
func (p *LMStudioProvider) convertCompletionResponse(response *lmstudioResponse, requestID string) (llm.CompletionResponse, error) {
	completionResponse := llm.CompletionResponse{
		ID:        response.ID,
		Object:    response.Object,
		Created:   response.Created,
		Model:     response.Model,
		Provider:  p.name,
		RequestID: requestID,
		Choices:   make([]llm.CompletionChoice, len(response.Choices)),
	}

	// Convert choices
	for i, choice := range response.Choices {
		completionResponse.Choices[i] = llm.CompletionChoice{
			Index:        choice.Index,
			Text:         choice.Text,
			FinishReason: choice.FinishReason,
		}

		// Convert log probabilities
		if choice.LogProbs != nil {
			completionResponse.Choices[i].LogProbs = &llm.LogProbs{
				Tokens:        choice.LogProbs.Tokens,
				TokenLogProbs: choice.LogProbs.TokenLogProbs,
				TopLogProbs:   choice.LogProbs.TopLogProbs,
			}
		}
	}

	// Convert usage
	if response.Usage != nil {
		completionResponse.Usage = &llm.LLMUsage{
			InputTokens:  int64(response.Usage.PromptTokens),
			OutputTokens: int64(response.Usage.CompletionTokens),
			TotalTokens:  int64(response.Usage.TotalTokens),
		}
	}

	return completionResponse, nil
}

// convertEmbeddingResponse converts LMStudio embedding response to standard format.
func (p *LMStudioProvider) convertEmbeddingResponse(response *lmstudioResponse, requestID string) (llm.EmbeddingResponse, error) {
	embeddingResponse := llm.EmbeddingResponse{
		Object:    response.Object,
		Model:     response.Model,
		Provider:  p.name,
		RequestID: requestID,
		Data:      make([]llm.EmbeddingData, len(response.Data)),
	}

	// Convert embeddings
	for i, embedding := range response.Data {
		embeddingResponse.Data[i] = llm.EmbeddingData{
			Object:    embedding.Object,
			Index:     embedding.Index,
			Embedding: embedding.Embedding,
		}
	}

	// Convert usage
	if response.Usage != nil {
		embeddingResponse.Usage = &llm.LLMUsage{
			InputTokens:  int64(response.Usage.PromptTokens),
			OutputTokens: int64(response.Usage.CompletionTokens),
			TotalTokens:  int64(response.Usage.TotalTokens),
		}
	}

	return embeddingResponse, nil
}

// updateUsageMetrics updates usage metrics.
func (p *LMStudioProvider) updateUsageMetrics(tokens int, latency time.Duration, isError bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if isError {
		p.usage.ErrorCount++
	}

	// Update average latency
	if p.usage.RequestCount > 0 {
		totalLatency := int64(p.usage.AverageLatency) * p.usage.RequestCount
		p.usage.AverageLatency = time.Duration((totalLatency + int64(latency)) / (p.usage.RequestCount + 1))
	} else {
		p.usage.AverageLatency = latency
	}

	// Update metrics
	if p.metrics != nil {
		p.metrics.Counter("forge.ai.llm.provider.requests_total", "provider", p.name).Inc()
		if tokens > 0 {
			p.metrics.Counter("forge.ai.llm.provider.tokens_total", "provider", p.name).Add(float64(tokens))
		}
		p.metrics.Histogram("forge.ai.llm.provider.request_duration", "provider", p.name).Observe(latency.Seconds())

		if isError {
			p.metrics.Counter("forge.ai.llm.provider.errors_total", "provider", p.name).Inc()
		}
	}
}

// wrapError wraps an error with context about LMStudio.
func (p *LMStudioProvider) wrapError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Add helpful context for common errors
	msg := fmt.Sprintf("LMStudio %s failed", operation)

	// Add context about ensuring LMStudio is running
	msg += " - ensure LMStudio is running and API server is enabled at " + p.baseURL

	return fmt.Errorf("%s: %w", msg, err)
}

// Stop stops the provider.
func (p *LMStudioProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.started = false

	return nil
}

// SetAPIKey updates the API key.
func (p *LMStudioProvider) SetAPIKey(apiKey string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.apiKey = apiKey
}

// SetBaseURL updates the base URL.
func (p *LMStudioProvider) SetBaseURL(baseURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.baseURL = baseURL
}

// RefreshModels refreshes the list of available models.
func (p *LMStudioProvider) RefreshModels(ctx context.Context) error {
	return p.discoverModels(ctx)
}

// ChatStream performs a streaming chat completion request.
// This implements the StreamingProvider interface.
func (p *LMStudioProvider) ChatStream(ctx context.Context, request llm.ChatRequest, handler func(llm.ChatStreamEvent) error) error {
	start := time.Now()

	// Convert request and set stream: true
	lmstudioReq := p.convertChatRequest(request)
	lmstudioReq.Stream = true

	// Serialize payload
	jsonData, err := json.Marshal(lmstudioReq)
	if err != nil {
		return fmt.Errorf("failed to marshal streaming request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create streaming request: %w", err)
	}

	// Set headers for SSE
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// Execute request
	resp, err := p.client.Do(req)
	if err != nil {
		p.updateUsageMetrics(0, time.Since(start), true)
		return p.wrapError(err, "streaming chat")
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.updateUsageMetrics(0, time.Since(start), true)
		return fmt.Errorf("streaming request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Process SSE stream
	var totalTokens int
	scanner := bufio.NewScanner(resp.Body)

	// Increase buffer size for larger chunks
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// Handle SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Check for stream completion
			if data == "[DONE]" {
				// Send done event
				doneEvent := llm.ChatStreamEvent{
					Type:      "done",
					RequestID: request.RequestID,
					Provider:  p.name,
				}
				if err := handler(doneEvent); err != nil {
					return err
				}
				break
			}

			// Parse and send event
			event, tokens, err := p.parseStreamChunk(data, request.RequestID)
			if err != nil {
				// Log error but continue processing
				if p.logger != nil {
					p.logger.Warn("Failed to parse stream chunk",
						forge.F("error", err.Error()),
						forge.F("data", data),
					)
				}
				continue
			}

			totalTokens += tokens

			// Call handler with event
			if err := handler(event); err != nil {
				return err
			}
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		p.updateUsageMetrics(totalTokens, time.Since(start), true)
		return fmt.Errorf("stream read error: %w", err)
	}

	// Update metrics
	p.updateUsageMetrics(totalTokens, time.Since(start), false)

	return nil
}

// parseStreamChunk parses a streaming chunk into a ChatStreamEvent.
func (p *LMStudioProvider) parseStreamChunk(data, requestID string) (llm.ChatStreamEvent, int, error) {
	// Parse the JSON chunk
	var chunk lmstudioResponse
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return llm.ChatStreamEvent{}, 0, fmt.Errorf("failed to parse chunk: %w", err)
	}

	// Build the event
	event := llm.ChatStreamEvent{
		ID:        chunk.ID,
		Object:    chunk.Object,
		Created:   chunk.Created,
		Model:     chunk.Model,
		Provider:  p.name,
		RequestID: requestID,
		Choices:   make([]llm.ChatChoice, len(chunk.Choices)),
	}

	// Convert choices
	for i, choice := range chunk.Choices {
		event.Choices[i] = llm.ChatChoice{
			Index:        choice.Index,
			FinishReason: choice.FinishReason,
		}

		// Handle delta (streaming content)
		if choice.Delta != nil {
			event.Type = "message"
			event.Choices[i].Delta = &llm.ChatMessage{
				Role:    choice.Delta.Role,
				Content: choice.Delta.Content,
				Name:    choice.Delta.Name,
			}

			// Handle tool calls in delta
			if len(choice.Delta.ToolCalls) > 0 {
				event.Type = "tool_call"
				event.Choices[i].Delta.ToolCalls = make([]llm.ToolCall, len(choice.Delta.ToolCalls))
				for j, tc := range choice.Delta.ToolCalls {
					event.Choices[i].Delta.ToolCalls[j] = llm.ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
					}
					if tc.Function.Name != "" || tc.Function.Arguments != "" {
						event.Choices[i].Delta.ToolCalls[j].Function = &llm.FunctionCall{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						}
					}
				}
			}
		}

		// Handle finish reason
		if choice.FinishReason != "" {
			event.Type = "message"
		}
	}

	// Extract token count if available
	tokens := 0
	if chunk.Usage != nil {
		tokens = chunk.Usage.TotalTokens
		event.Usage = &llm.LLMUsage{
			InputTokens:  int64(chunk.Usage.PromptTokens),
			OutputTokens: int64(chunk.Usage.CompletionTokens),
			TotalTokens:  int64(chunk.Usage.TotalTokens),
		}
	}

	return event, tokens, nil
}

// Ensure LMStudioProvider implements StreamingProvider interface.
var _ llm.StreamingProvider = (*LMStudioProvider)(nil)
