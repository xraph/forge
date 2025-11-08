package providers

import (
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
	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/internal/errors"
)

// OpenAIProvider implements LLM provider for OpenAI.
type OpenAIProvider struct {
	name      string
	apiKey    string
	baseURL   string
	orgID     string
	client    *http.Client
	models    []string
	usage     llm.LLMUsage
	logger    forge.Logger
	metrics   forge.Metrics
	rateLimit *RateLimiter
}

// OpenAIConfig contains configuration for OpenAI provider.
type OpenAIConfig struct {
	APIKey     string        `env:"OPENAI_API_KEY"                yaml:"api_key"`
	BaseURL    string        `default:"https://api.openai.com/v1" yaml:"base_url"`
	OrgID      string        `env:"OPENAI_ORG_ID"                 yaml:"org_id"`
	Timeout    time.Duration `default:"30s"                       yaml:"timeout"`
	MaxRetries int           `default:"3"                         yaml:"max_retries"`
	RateLimit  *RateLimit    `yaml:"rate_limit"`
}

// RateLimit defines rate limiting configuration.
type RateLimit struct {
	RequestsPerMinute int `default:"60"    yaml:"requests_per_minute"`
	TokensPerMinute   int `default:"10000" yaml:"tokens_per_minute"`
}

// RateLimiter handles rate limiting.
type RateLimiter struct {
	requestsPerMinute int
	tokensPerMinute   int
	requestTokens     []time.Time
	tokenUsage        []TokenUsage
}

// TokenUsage tracks token usage with timestamp.
type TokenUsage struct {
	tokens    int
	timestamp time.Time
}

// OpenAI API structures.
type openAIRequest struct {
	Model            string             `json:"model"`
	Messages         []openAIMessage    `json:"messages,omitempty"`
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
	Tools            []openAITool       `json:"tools,omitempty"`
	ToolChoice       any                `json:"tool_choice,omitempty"`
	Input            any                `json:"input,omitempty"`
	EncodingFormat   string             `json:"encoding_format,omitempty"`
	Dimensions       *int               `json:"dimensions,omitempty"`
}

type openAIMessage struct {
	Role         string              `json:"role"`
	Content      string              `json:"content"`
	Name         string              `json:"name,omitempty"`
	ToolCalls    []openAIToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string              `json:"tool_call_id,omitempty"`
	FunctionCall *openAIFunctionCall `json:"function_call,omitempty"`
}

type openAITool struct {
	Type     string                   `json:"type"`
	Function openAIFunctionDefinition `json:"function"`
}

type openAIFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponse struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []openAIChoice    `json:"choices"`
	Usage   *openAIUsage      `json:"usage,omitempty"`
	Data    []openAIEmbedding `json:"data,omitempty"`
}

type openAIChoice struct {
	Index        int             `json:"index"`
	Message      *openAIMessage  `json:"message,omitempty"`
	Text         string          `json:"text,omitempty"`
	Delta        *openAIMessage  `json:"delta,omitempty"`
	FinishReason string          `json:"finish_reason"`
	LogProbs     *openAILogProbs `json:"logprobs,omitempty"`
}

type openAILogProbs struct {
	Tokens        []string             `json:"tokens"`
	TokenLogProbs []float64            `json:"token_logprobs"`
	TopLogProbs   []map[string]float64 `json:"top_logprobs"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIEmbedding struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// NewOpenAIProvider creates a new OpenAI provider.
func NewOpenAIProvider(config OpenAIConfig, logger forge.Logger, metrics forge.Metrics) (*OpenAIProvider, error) {
	if config.APIKey == "" {
		return nil, errors.New("OpenAI API key is required")
	}

	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
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

	return &OpenAIProvider{
		name:    "openai",
		apiKey:  config.APIKey,
		baseURL: config.BaseURL,
		orgID:   config.OrgID,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		models: []string{
			"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-4",
			"gpt-3.5-turbo", "gpt-3.5-turbo-16k",
			"text-davinci-003", "text-curie-001", "text-babbage-001", "text-ada-001",
			"text-embedding-3-large", "text-embedding-3-small", "text-embedding-ada-002",
		},
		usage:     llm.LLMUsage{LastReset: time.Now()},
		logger:    logger,
		metrics:   metrics,
		rateLimit: rateLimiter,
	}, nil
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return p.name
}

// Models returns the available models.
func (p *OpenAIProvider) Models() []string {
	return p.models
}

// Chat performs a chat completion request.
func (p *OpenAIProvider) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	if err := p.checkRateLimit(ctx, request.Model); err != nil {
		return llm.ChatResponse{}, err
	}

	// Convert request to OpenAI format
	openAIReq := p.convertChatRequest(request)

	// Make API request
	response, err := p.makeRequest(ctx, "/chat/completions", openAIReq)
	if err != nil {
		return llm.ChatResponse{}, err
	}

	// Convert response
	return p.convertChatResponse(response, request.RequestID)
}

// Complete performs a text completion request.
func (p *OpenAIProvider) Complete(ctx context.Context, request llm.CompletionRequest) (llm.CompletionResponse, error) {
	if err := p.checkRateLimit(ctx, request.Model); err != nil {
		return llm.CompletionResponse{}, err
	}

	// Convert request to OpenAI format
	openAIReq := p.convertCompletionRequest(request)

	// Make API request
	response, err := p.makeRequest(ctx, "/completions", openAIReq)
	if err != nil {
		return llm.CompletionResponse{}, err
	}

	// Convert response
	return p.convertCompletionResponse(response, request.RequestID)
}

// Embed performs an embedding request.
func (p *OpenAIProvider) Embed(ctx context.Context, request llm.EmbeddingRequest) (llm.EmbeddingResponse, error) {
	if err := p.checkRateLimit(ctx, request.Model); err != nil {
		return llm.EmbeddingResponse{}, err
	}

	// Convert request to OpenAI format
	openAIReq := p.convertEmbeddingRequest(request)

	// Make API request
	response, err := p.makeRequest(ctx, "/embeddings", openAIReq)
	if err != nil {
		return llm.EmbeddingResponse{}, err
	}

	// Convert response
	return p.convertEmbeddingResponse(response, request.RequestID)
}

// GetUsage returns current usage statistics.
func (p *OpenAIProvider) GetUsage() llm.LLMUsage {
	return p.usage
}

// HealthCheck performs a health check.
func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	// Simple health check - try to list models
	req := &openAIRequest{}
	_, err := p.makeRequest(ctx, "/models", req)

	return err
}

// makeRequest makes an HTTP request to the OpenAI API.
func (p *OpenAIProvider) makeRequest(ctx context.Context, endpoint string, payload any) (*openAIResponse, error) {
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
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	if p.orgID != "" {
		req.Header.Set("Openai-Organization", p.orgID)
	}

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
	var response openAIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Update usage statistics
	if response.Usage != nil {
		p.usage.InputTokens += int64(response.Usage.PromptTokens)
		p.usage.OutputTokens += int64(response.Usage.CompletionTokens)
		p.usage.TotalTokens += int64(response.Usage.TotalTokens)
		p.usage.RequestCount++
	}

	return &response, nil
}

// convertChatRequest converts a chat request to OpenAI format.
func (p *OpenAIProvider) convertChatRequest(request llm.ChatRequest) *openAIRequest {
	openAIReq := &openAIRequest{
		Model:       request.Model,
		MaxTokens:   request.MaxTokens,
		Temperature: request.Temperature,
		TopP:        request.TopP,
		// N:                request.N,
		Stream: request.Stream,
		Stop:   request.Stop,
		// PresencePenalty:  request.PresencePenalty,
		// FrequencyPenalty: request.FrequencyPenalty,
		// User:             request.User,
	}

	// Convert messages
	openAIReq.Messages = make([]openAIMessage, len(request.Messages))
	for i, msg := range request.Messages {
		openAIReq.Messages[i] = openAIMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}

		// Convert tool calls
		if len(msg.ToolCalls) > 0 {
			openAIReq.Messages[i].ToolCalls = make([]openAIToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				openAIReq.Messages[i].ToolCalls[j] = openAIToolCall{
					ID:   tc.ID,
					Type: tc.Type,
				}
				if tc.Function != nil {
					openAIReq.Messages[i].ToolCalls[j].Function = openAIFunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					}
				}
			}
		}

		// Convert function call (legacy)
		if msg.FunctionCall != nil {
			openAIReq.Messages[i].FunctionCall = &openAIFunctionCall{
				Name:      msg.FunctionCall.Name,
				Arguments: msg.FunctionCall.Arguments,
			}
		}
	}

	// Convert tools
	if len(request.Tools) > 0 {
		openAIReq.Tools = make([]openAITool, len(request.Tools))
		for i, tool := range request.Tools {
			openAIReq.Tools[i] = openAITool{
				Type: tool.Type,
			}
			if tool.Function != nil {
				openAIReq.Tools[i].Function = openAIFunctionDefinition{
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
			openAIReq.ToolChoice = request.ToolChoice
		} else {
			openAIReq.ToolChoice = map[string]any{
				"type": "function",
				"function": map[string]string{
					"name": request.ToolChoice,
				},
			}
		}
	}

	return openAIReq
}

// convertCompletionRequest converts a completion request to OpenAI format.
func (p *OpenAIProvider) convertCompletionRequest(request llm.CompletionRequest) *openAIRequest {
	return &openAIRequest{
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
		// User:             request.User,
	}
}

// convertEmbeddingRequest converts an embedding request to OpenAI format.
func (p *OpenAIProvider) convertEmbeddingRequest(request llm.EmbeddingRequest) *openAIRequest {
	return &openAIRequest{
		Model:          request.Model,
		Input:          request.Input,
		User:           request.User,
		EncodingFormat: request.EncodingFormat,
		Dimensions:     request.Dimensions,
	}
}

// convertChatResponse converts OpenAI chat response to standard format.
func (p *OpenAIProvider) convertChatResponse(response *openAIResponse, requestID string) (llm.ChatResponse, error) {
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

// convertCompletionResponse converts OpenAI completion response to standard format.
func (p *OpenAIProvider) convertCompletionResponse(response *openAIResponse, requestID string) (llm.CompletionResponse, error) {
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

// convertEmbeddingResponse converts OpenAI embedding response to standard format.
func (p *OpenAIProvider) convertEmbeddingResponse(response *openAIResponse, requestID string) (llm.EmbeddingResponse, error) {
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

// checkRateLimit checks if the request is within rate limits.
func (p *OpenAIProvider) checkRateLimit(ctx context.Context, model string) error {
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
func (p *OpenAIProvider) cleanupRateLimit(now time.Time) {
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

// Stop stops the provider.
func (p *OpenAIProvider) Stop(ctx context.Context) error {
	// Clean up resources if needed
	return nil
}

// SetAPIKey updates the API key.
func (p *OpenAIProvider) SetAPIKey(apiKey string) {
	p.apiKey = apiKey
}

// SetBaseURL updates the base URL.
func (p *OpenAIProvider) SetBaseURL(baseURL string) {
	p.baseURL = baseURL
}

// SetOrganization updates the organization ID.
func (p *OpenAIProvider) SetOrganization(orgID string) {
	p.orgID = orgID
}

// GetModels returns the list of available models.
func (p *OpenAIProvider) GetModels() []string {
	return p.models
}

// IsModelSupported checks if a model is supported.
func (p *OpenAIProvider) IsModelSupported(model string) bool {

	return slices.Contains(p.models, model)
}

// GetModelInfo returns information about a model.
func (p *OpenAIProvider) GetModelInfo(model string) (map[string]any, error) {
	if !p.IsModelSupported(model) {
		return nil, fmt.Errorf("model %s is not supported", model)
	}

	info := map[string]any{
		"name":     model,
		"provider": p.name,
	}

	// Add model-specific information
	if strings.HasPrefix(model, "gpt-4") {
		info["type"] = "chat"

		info["max_tokens"] = 8192
		if strings.Contains(model, "turbo") {
			info["max_tokens"] = 4096
		}
	} else if strings.HasPrefix(model, "gpt-3.5") {
		info["type"] = "chat"

		info["max_tokens"] = 4096
		if strings.Contains(model, "16k") {
			info["max_tokens"] = 16384
		}
	} else if strings.HasPrefix(model, "text-davinci") {
		info["type"] = "completion"
		info["max_tokens"] = 4096
	} else if strings.HasPrefix(model, "text-embedding") {
		info["type"] = "embedding"
		if strings.Contains(model, "3-large") {
			info["dimensions"] = 3072
		} else if strings.Contains(model, "3-small") {
			info["dimensions"] = 1536
		} else if strings.Contains(model, "ada-002") {
			info["dimensions"] = 1536
		}
	}

	return info, nil
}

// // updateUsage updates provider usage statistics
// func (p *OpenAIProvider) updateUsage(tokens int, latency time.Duration, isError bool) {
// 	p.usage.TotalRequests++
// 	p.usage.TotalTokens += int64(tokens)
// 	p.usage.LastRequestTime = time.Now()
//
// 	if isError {
// 		p.usage.ErrorCount++
// 	}
//
// 	// Update average latency
// 	if p.usage.TotalRequests > 0 {
// 		p.usage.AverageLatency = time.Duration(
// 			(int64(p.usage.AverageLatency)*(p.usage.TotalRequests-1) + int64(latency)) / p.usage.TotalRequests,
// 		)
// 	}
//
// 	// Update rates (simplified calculation)
// 	if p.usage.TotalRequests > 0 {
// 		duration := time.Since(p.usage.LastRequestTime)
// 		if duration > 0 {
// 			p.usage.RequestsPerSecond = float64(p.usage.TotalRequests) / duration.Seconds()
// 			p.usage.TokensPerSecond = float64(p.usage.TotalTokens) / duration.Seconds()
// 		}
// 	}
//
// 	// Update metrics
// 	if p.metrics != nil {
// 		p.metrics.Counter("forge.ai.llm.provider.requests_total", "provider", p.name).Inc()
// 		p.metrics.Counter("forge.ai.llm.provider.tokens_total", "provider", p.name).Add(float64(tokens))
// 		p.metrics.Histogram("forge.ai.llm.provider.request_duration", "provider", p.name).Observe(latency.Seconds())
//
// 		if isError {
// 			p.metrics.Counter("forge.ai.llm.provider.errors_total", "provider", p.name).Inc()
// 		}
// 	}
// }
