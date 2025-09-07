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

	"github.com/xraph/forge/pkg/ai/llm"
	"github.com/xraph/forge/pkg/common"
)

// HuggingFaceProvider implements LLM provider for Hugging Face
type HuggingFaceProvider struct {
	name      string
	apiKey    string
	baseURL   string
	client    *http.Client
	models    []string
	usage     llm.LLMUsage
	logger    common.Logger
	metrics   common.Metrics
	rateLimit *RateLimiter
}

// HuggingFaceConfig contains configuration for Hugging Face provider
type HuggingFaceConfig struct {
	APIKey     string        `yaml:"api_key" env:"HUGGINGFACE_API_KEY"`
	BaseURL    string        `yaml:"base_url" default:"https://api-inference.huggingface.co"`
	Timeout    time.Duration `yaml:"timeout" default:"30s"`
	MaxRetries int           `yaml:"max_retries" default:"3"`
	RateLimit  *RateLimit    `yaml:"rate_limit"`
}

// Hugging Face API structures
type hfTextGenerationRequest struct {
	Inputs     string        `json:"inputs"`
	Parameters *hfParameters `json:"parameters,omitempty"`
	Options    *hfOptions    `json:"options,omitempty"`
}

type hfParameters struct {
	Temperature    *float64 `json:"temperature,omitempty"`
	MaxNewTokens   *int     `json:"max_new_tokens,omitempty"`
	TopP           *float64 `json:"top_p,omitempty"`
	TopK           *int     `json:"top_k,omitempty"`
	Stop           []string `json:"stop,omitempty"`
	DoSample       bool     `json:"do_sample,omitempty"`
	ReturnFullText bool     `json:"return_full_text,omitempty"`
}

type hfOptions struct {
	WaitForModel bool `json:"wait_for_model"`
	UseCache     bool `json:"use_cache"`
}

type hfTextGenerationResponse struct {
	GeneratedText string  `json:"generated_text"`
	Score         float64 `json:"score,omitempty"`
}

type hfEmbeddingRequest struct {
	Inputs  interface{} `json:"inputs"`
	Options *hfOptions  `json:"options,omitempty"`
}

type hfEmbeddingResponse [][]float64

type hfChatRequest struct {
	Model       string      `json:"model"`
	Messages    []hfMessage `json:"messages"`
	Temperature *float64    `json:"temperature,omitempty"`
	MaxTokens   *int        `json:"max_tokens,omitempty"`
	TopP        *float64    `json:"top_p,omitempty"`
	Stream      bool        `json:"stream,omitempty"`
	Stop        []string    `json:"stop,omitempty"`
}

type hfMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type hfChatResponse struct {
	ID      string     `json:"id"`
	Object  string     `json:"object"`
	Created int64      `json:"created"`
	Model   string     `json:"model"`
	Choices []hfChoice `json:"choices"`
	Usage   *hfUsage   `json:"usage,omitempty"`
}

type hfChoice struct {
	Index        int       `json:"index"`
	Message      hfMessage `json:"message"`
	FinishReason string    `json:"finish_reason"`
}

type hfUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// NewHuggingFaceProvider creates a new Hugging Face provider
func NewHuggingFaceProvider(config HuggingFaceConfig, logger common.Logger, metrics common.Metrics) (*HuggingFaceProvider, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("Hugging Face API key is required")
	}

	if config.BaseURL == "" {
		config.BaseURL = "https://api-inference.huggingface.co"
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

	return &HuggingFaceProvider{
		name:    "huggingface",
		apiKey:  config.APIKey,
		baseURL: config.BaseURL,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		models: []string{
			// Text generation models
			"microsoft/DialoGPT-medium",
			"microsoft/DialoGPT-large",
			"google/flan-t5-large",
			"google/flan-t5-xl",
			"google/flan-ul2",
			"bigscience/bloom",
			"bigscience/bloomz",
			"meta-llama/Llama-2-7b-chat-hf",
			"meta-llama/Llama-2-13b-chat-hf",
			"meta-llama/Llama-2-70b-chat-hf",
			"mistralai/Mistral-7B-Instruct-v0.1",
			"mistralai/Mistral-7B-Instruct-v0.2",
			"mistralai/Mixtral-8x7B-Instruct-v0.1",
			"codellama/CodeLlama-7b-Python-hf",
			"codellama/CodeLlama-13b-Python-hf",
			"Salesforce/codegen-350M-mono",
			"Salesforce/codegen-2B-mono",
			"Salesforce/codegen-6B-mono",
			// Embedding models
			"sentence-transformers/all-MiniLM-L6-v2",
			"sentence-transformers/all-mpnet-base-v2",
			"sentence-transformers/all-distilroberta-v1",
			"sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2",
			"sentence-transformers/msmarco-distilbert-base-tas-b",
			"thenlper/gte-large",
			"thenlper/gte-base",
			"BAAI/bge-large-en-v1.5",
			"BAAI/bge-base-en-v1.5",
			"BAAI/bge-small-en-v1.5",
		},
		usage:     llm.LLMUsage{LastReset: time.Now()},
		logger:    logger,
		metrics:   metrics,
		rateLimit: rateLimiter,
	}, nil
}

// Name returns the provider name
func (p *HuggingFaceProvider) Name() string {
	return p.name
}

// Models returns the available models
func (p *HuggingFaceProvider) Models() []string {
	return p.models
}

// Chat performs a chat completion request
func (p *HuggingFaceProvider) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	if err := p.checkRateLimit(ctx, request.Model); err != nil {
		return llm.ChatResponse{}, err
	}

	// Check if model supports chat format
	if p.isChatModel(request.Model) {
		return p.chatWithChatModel(ctx, request)
	}

	// Convert to text generation for non-chat models
	return p.chatWithTextGeneration(ctx, request)
}

// Complete performs a text completion request
func (p *HuggingFaceProvider) Complete(ctx context.Context, request llm.CompletionRequest) (llm.CompletionResponse, error) {
	if err := p.checkRateLimit(ctx, request.Model); err != nil {
		return llm.CompletionResponse{}, err
	}

	// Convert request to HF format
	hfReq := p.convertCompletionRequest(request)

	// Make API request
	response, err := p.makeTextGenerationRequest(ctx, request.Model, hfReq)
	if err != nil {
		return llm.CompletionResponse{}, err
	}

	// Convert response
	return p.convertCompletionResponse(response, request.RequestID)
}

// Embed performs an embedding request
func (p *HuggingFaceProvider) Embed(ctx context.Context, request llm.EmbeddingRequest) (llm.EmbeddingResponse, error) {
	if err := p.checkRateLimit(ctx, request.Model); err != nil {
		return llm.EmbeddingResponse{}, err
	}

	// Convert request to HF format
	hfReq := p.convertEmbeddingRequest(request)

	// Make API request
	response, err := p.makeEmbeddingRequest(ctx, request.Model, hfReq)
	if err != nil {
		return llm.EmbeddingResponse{}, err
	}

	// Convert response
	return p.convertEmbeddingResponse(response, request.RequestID)
}

// GetUsage returns current usage statistics
func (p *HuggingFaceProvider) GetUsage() llm.LLMUsage {
	return p.usage
}

// HealthCheck performs a health check
func (p *HuggingFaceProvider) HealthCheck(ctx context.Context) error {
	// Simple health check - try a small text generation
	testRequest := llm.CompletionRequest{
		Model:     "microsoft/DialoGPT-medium",
		Prompt:    "Hello",
		MaxTokens: &[]int{5}[0],
	}

	_, err := p.Complete(ctx, testRequest)
	return err
}

// isChatModel checks if a model supports chat format
func (p *HuggingFaceProvider) isChatModel(model string) bool {
	chatModels := []string{
		"meta-llama/Llama-2-7b-chat-hf",
		"meta-llama/Llama-2-13b-chat-hf",
		"meta-llama/Llama-2-70b-chat-hf",
		"mistralai/Mistral-7B-Instruct-v0.1",
		"mistralai/Mistral-7B-Instruct-v0.2",
		"mistralai/Mixtral-8x7B-Instruct-v0.1",
	}

	for _, chatModel := range chatModels {
		if model == chatModel {
			return true
		}
	}
	return false
}

// chatWithChatModel handles chat with models that support chat format
func (p *HuggingFaceProvider) chatWithChatModel(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	// Convert request to HF chat format
	hfReq := p.convertChatRequest(request)

	// Make API request to chat endpoint
	response, err := p.makeChatRequest(ctx, request.Model, hfReq)
	if err != nil {
		return llm.ChatResponse{}, err
	}

	// Convert response
	return p.convertChatResponse(response, request.RequestID)
}

// chatWithTextGeneration handles chat using text generation models
func (p *HuggingFaceProvider) chatWithTextGeneration(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	// Convert chat to text format
	prompt := p.convertChatToPrompt(request.Messages)

	// Create text generation request
	hfReq := &hfTextGenerationRequest{
		Inputs: prompt,
		Parameters: &hfParameters{
			Temperature:    request.Temperature,
			MaxNewTokens:   request.MaxTokens,
			TopP:           request.TopP,
			TopK:           request.TopK,
			Stop:           request.Stop,
			DoSample:       true,
			ReturnFullText: false,
		},
		Options: &hfOptions{
			WaitForModel: true,
			UseCache:     false,
		},
	}

	// Make API request
	response, err := p.makeTextGenerationRequest(ctx, request.Model, hfReq)
	if err != nil {
		return llm.ChatResponse{}, err
	}

	// Convert to chat response
	return p.convertTextGenerationToChatResponse(response, request.RequestID)
}

// convertChatToPrompt converts chat messages to a single prompt
func (p *HuggingFaceProvider) convertChatToPrompt(messages []llm.ChatMessage) string {
	var prompt strings.Builder

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			prompt.WriteString(fmt.Sprintf("System: %s\n", msg.Content))
		case "user":
			prompt.WriteString(fmt.Sprintf("User: %s\n", msg.Content))
		case "assistant":
			prompt.WriteString(fmt.Sprintf("Assistant: %s\n", msg.Content))
		}
	}

	prompt.WriteString("Assistant:")
	return prompt.String()
}

// makeTextGenerationRequest makes a text generation request
func (p *HuggingFaceProvider) makeTextGenerationRequest(ctx context.Context, model string, payload *hfTextGenerationRequest) ([]hfTextGenerationResponse, error) {
	// Serialize payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	endpoint := fmt.Sprintf("/models/%s", model)
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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
	var response []hfTextGenerationResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Update usage statistics
	p.updateUsage(len(payload.Inputs), len(response))

	return response, nil
}

// makeChatRequest makes a chat request
func (p *HuggingFaceProvider) makeChatRequest(ctx context.Context, model string, payload *hfChatRequest) (*hfChatResponse, error) {
	// Serialize payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	endpoint := fmt.Sprintf("/models/%s/chat/completions", model)
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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
	var response hfChatResponse
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

// makeEmbeddingRequest makes an embedding request
func (p *HuggingFaceProvider) makeEmbeddingRequest(ctx context.Context, model string, payload *hfEmbeddingRequest) (hfEmbeddingResponse, error) {
	// Serialize payload
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	endpoint := fmt.Sprintf("/models/%s", model)
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

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
	var response hfEmbeddingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return response, nil
}

// convertChatRequest converts a chat request to HF format
func (p *HuggingFaceProvider) convertChatRequest(request llm.ChatRequest) *hfChatRequest {
	hfReq := &hfChatRequest{
		Model:       request.Model,
		Temperature: request.Temperature,
		MaxTokens:   request.MaxTokens,
		TopP:        request.TopP,
		Stream:      request.Stream,
		Stop:        request.Stop,
	}

	// Convert messages
	hfReq.Messages = make([]hfMessage, len(request.Messages))
	for i, msg := range request.Messages {
		hfReq.Messages[i] = hfMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	return hfReq
}

// convertCompletionRequest converts a completion request to HF format
func (p *HuggingFaceProvider) convertCompletionRequest(request llm.CompletionRequest) *hfTextGenerationRequest {
	return &hfTextGenerationRequest{
		Inputs: request.Prompt,
		Parameters: &hfParameters{
			Temperature:    request.Temperature,
			MaxNewTokens:   request.MaxTokens,
			TopP:           request.TopP,
			TopK:           request.TopK,
			Stop:           request.Stop,
			DoSample:       true,
			ReturnFullText: false,
		},
		Options: &hfOptions{
			WaitForModel: true,
			UseCache:     false,
		},
	}
}

// convertEmbeddingRequest converts an embedding request to HF format
func (p *HuggingFaceProvider) convertEmbeddingRequest(request llm.EmbeddingRequest) *hfEmbeddingRequest {
	return &hfEmbeddingRequest{
		Inputs: request.Input,
		Options: &hfOptions{
			WaitForModel: true,
			UseCache:     false,
		},
	}
}

// convertChatResponse converts HF chat response to standard format
func (p *HuggingFaceProvider) convertChatResponse(response *hfChatResponse, requestID string) (llm.ChatResponse, error) {
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
			Message: llm.ChatMessage{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
			},
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

// convertTextGenerationToChatResponse converts text generation to chat response
func (p *HuggingFaceProvider) convertTextGenerationToChatResponse(response []hfTextGenerationResponse, requestID string) (llm.ChatResponse, error) {
	chatResponse := llm.ChatResponse{
		ID:        fmt.Sprintf("hf-%d", time.Now().Unix()),
		Object:    "chat.completion",
		Created:   time.Now().Unix(),
		Model:     "huggingface",
		Provider:  p.name,
		RequestID: requestID,
		Choices:   make([]llm.ChatChoice, len(response)),
	}

	// Convert choices
	for i, choice := range response {
		chatResponse.Choices[i] = llm.ChatChoice{
			Index:        i,
			FinishReason: "stop",
			Message: llm.ChatMessage{
				Role:    "assistant",
				Content: choice.GeneratedText,
			},
		}
	}

	return chatResponse, nil
}

// convertCompletionResponse converts HF response to completion response
func (p *HuggingFaceProvider) convertCompletionResponse(response []hfTextGenerationResponse, requestID string) (llm.CompletionResponse, error) {
	completionResponse := llm.CompletionResponse{
		ID:        fmt.Sprintf("hf-%d", time.Now().Unix()),
		Object:    "text_completion",
		Created:   time.Now().Unix(),
		Model:     "huggingface",
		Provider:  p.name,
		RequestID: requestID,
		Choices:   make([]llm.CompletionChoice, len(response)),
	}

	// Convert choices
	for i, choice := range response {
		completionResponse.Choices[i] = llm.CompletionChoice{
			Index:        i,
			Text:         choice.GeneratedText,
			FinishReason: "stop",
		}
	}

	return completionResponse, nil
}

// convertEmbeddingResponse converts HF embedding response to standard format
func (p *HuggingFaceProvider) convertEmbeddingResponse(response hfEmbeddingResponse, requestID string) (llm.EmbeddingResponse, error) {
	embeddingResponse := llm.EmbeddingResponse{
		Object:    "list",
		Model:     "huggingface",
		Provider:  p.name,
		RequestID: requestID,
		Data:      make([]llm.EmbeddingData, len(response)),
	}

	// Convert embeddings
	for i, embedding := range response {
		embeddingResponse.Data[i] = llm.EmbeddingData{
			Object:    "embedding",
			Index:     i,
			Embedding: embedding,
		}
	}

	return embeddingResponse, nil
}

// updateUsage updates usage statistics
func (p *HuggingFaceProvider) updateUsage(inputTokens, outputTokens int) {
	p.usage.InputTokens += int64(inputTokens)
	p.usage.OutputTokens += int64(outputTokens)
	p.usage.TotalTokens += int64(inputTokens + outputTokens)
	p.usage.RequestCount++
}

// checkRateLimit checks if the request is within rate limits
func (p *HuggingFaceProvider) checkRateLimit(ctx context.Context, model string) error {
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
func (p *HuggingFaceProvider) cleanupRateLimit(now time.Time) {
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
func (p *HuggingFaceProvider) IsModelSupported(model string) bool {
	for _, supportedModel := range p.models {
		if supportedModel == model {
			return true
		}
	}
	return false
}

// GetModelInfo returns information about a model
func (p *HuggingFaceProvider) GetModelInfo(model string) (map[string]interface{}, error) {
	if !p.IsModelSupported(model) {
		return nil, fmt.Errorf("model %s is not supported", model)
	}

	info := map[string]interface{}{
		"name":     model,
		"provider": p.name,
	}

	// Add model-specific information
	if strings.Contains(model, "sentence-transformers") || strings.Contains(model, "gte-") || strings.Contains(model, "bge-") {
		info["type"] = "embedding"
	} else if strings.Contains(model, "codegen") || strings.Contains(model, "CodeLlama") {
		info["type"] = "code"
	} else {
		info["type"] = "text"
	}

	return info, nil
}
