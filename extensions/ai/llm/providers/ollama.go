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

// OllamaProvider implements LLM provider for Ollama.
// Ollama is a local LLM inference engine with its own API.
type OllamaProvider struct {
	name    string
	baseURL string
	client  *http.Client
	models  []string
	usage   llm.LLMUsage
	logger  forge.Logger
	metrics forge.Metrics
	mu      sync.RWMutex
	started bool
}

// OllamaConfig contains configuration for Ollama provider.
type OllamaConfig struct {
	BaseURL    string        `default:"http://localhost:11434" yaml:"base_url"`
	Timeout    time.Duration `default:"120s" yaml:"timeout"` // Ollama can be slow on first load
	MaxRetries int           `default:"2" yaml:"max_retries"`
	Models     []string      `yaml:"models"` // Pre-configured model list
	Logger     forge.Logger
	Metrics    forge.Metrics
}

// ollamaChatRequest represents an Ollama chat request.
type ollamaChatRequest struct {
	Model     string          `json:"model"`
	Messages  []ollamaMessage `json:"messages"`
	Stream    bool            `json:"stream,omitempty"`
	Format    string          `json:"format,omitempty"` // "json" for JSON mode
	Options   *ollamaOptions  `json:"options,omitempty"`
	Tools     []ollamaTool    `json:"tools,omitempty"`
	KeepAlive string          `json:"keep_alive,omitempty"`
}

// ollamaGenerateRequest represents an Ollama generate (completion) request.
type ollamaGenerateRequest struct {
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`
	Stream    bool           `json:"stream,omitempty"`
	Format    string         `json:"format,omitempty"`
	Options   *ollamaOptions `json:"options,omitempty"`
	System    string         `json:"system,omitempty"`
	Template  string         `json:"template,omitempty"`
	Context   []int          `json:"context,omitempty"`
	Raw       bool           `json:"raw,omitempty"`
	KeepAlive string         `json:"keep_alive,omitempty"`
}

// ollamaEmbedRequest represents an Ollama embedding request.
type ollamaEmbedRequest struct {
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt,omitempty"`
	Input     any            `json:"input,omitempty"` // string or []string
	Options   *ollamaOptions `json:"options,omitempty"`
	KeepAlive string         `json:"keep_alive,omitempty"`
}

// ollamaMessage represents a message in Ollama format.
type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Images    []string         `json:"images,omitempty"` // Base64 encoded images
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

// ollamaTool represents a tool definition.
type ollamaTool struct {
	Type     string            `json:"type"`
	Function ollamaFunctionDef `json:"function"`
}

// ollamaFunctionDef represents a function definition.
type ollamaFunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ollamaToolCall represents a tool call.
type ollamaToolCall struct {
	Function ollamaFunctionCall `json:"function"`
}

// ollamaFunctionCall represents a function call.
type ollamaFunctionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ollamaOptions represents Ollama-specific options.
type ollamaOptions struct {
	NumPredict      *int     `json:"num_predict,omitempty"` // max tokens
	Temperature     *float64 `json:"temperature,omitempty"`
	TopK            *int     `json:"top_k,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
	RepeatPenalty   *float64 `json:"repeat_penalty,omitempty"`
	Seed            *int     `json:"seed,omitempty"`
	Stop            []string `json:"stop,omitempty"`
	TfsZ            *float64 `json:"tfs_z,omitempty"`
	NumCtx          *int     `json:"num_ctx,omitempty"` // context window
	NumGqa          *int     `json:"num_gqa,omitempty"`
	NumGpu          *int     `json:"num_gpu,omitempty"`
	NumThread       *int     `json:"num_thread,omitempty"`
	RepeatLastN     *int     `json:"repeat_last_n,omitempty"`
	Mirostat        *int     `json:"mirostat,omitempty"`
	MirostatTau     *float64 `json:"mirostat_tau,omitempty"`
	MirostatEta     *float64 `json:"mirostat_eta,omitempty"`
	PenalizeNewline *bool    `json:"penalize_newline,omitempty"`
	UseMLock        *bool    `json:"use_mlock,omitempty"`
	NumKeep         *int     `json:"num_keep,omitempty"`
}

// ollamaChatResponse represents an Ollama chat response.
type ollamaChatResponse struct {
	Model              string        `json:"model"`
	CreatedAt          string        `json:"created_at"`
	Message            ollamaMessage `json:"message"`
	Done               bool          `json:"done"`
	TotalDuration      int64         `json:"total_duration,omitempty"`
	LoadDuration       int64         `json:"load_duration,omitempty"`
	PromptEvalCount    int           `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64         `json:"prompt_eval_duration,omitempty"`
	EvalCount          int           `json:"eval_count,omitempty"`
	EvalDuration       int64         `json:"eval_duration,omitempty"`
	Context            []int         `json:"context,omitempty"`
	DoneReason         string        `json:"done_reason,omitempty"`
}

// ollamaGenerateResponse represents an Ollama generate response.
type ollamaGenerateResponse struct {
	Model              string `json:"model"`
	CreatedAt          string `json:"created_at"`
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	Context            []int  `json:"context,omitempty"`
	TotalDuration      int64  `json:"total_duration,omitempty"`
	LoadDuration       int64  `json:"load_duration,omitempty"`
	PromptEvalCount    int    `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64  `json:"prompt_eval_duration,omitempty"`
	EvalCount          int    `json:"eval_count,omitempty"`
	EvalDuration       int64  `json:"eval_duration,omitempty"`
	DoneReason         string `json:"done_reason,omitempty"`
}

// ollamaEmbedResponse represents an Ollama embedding response.
type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embedding  []float64   `json:"embedding,omitempty"`
	Embeddings [][]float64 `json:"embeddings,omitempty"` // For multiple inputs
}

// ollamaModelInfo represents information about an Ollama model.
type ollamaModelInfo struct {
	Name       string             `json:"name"`
	Model      string             `json:"model"`
	ModifiedAt string             `json:"modified_at"`
	Size       int64              `json:"size"`
	Digest     string             `json:"digest"`
	Details    ollamaModelDetails `json:"details"`
}

// ollamaModelDetails represents detailed model information.
type ollamaModelDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// ollamaModelsResponse represents the response from /api/tags.
type ollamaModelsResponse struct {
	Models []ollamaModelInfo `json:"models"`
}

// ollamaVersionResponse represents the version response.
type ollamaVersionResponse struct {
	Version string `json:"version"`
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(config OllamaConfig, logger forge.Logger, metrics forge.Metrics) (*OllamaProvider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:11434"
	}

	if config.Timeout == 0 {
		config.Timeout = 120 * time.Second
	}

	provider := &OllamaProvider{
		name:    "ollama",
		baseURL: strings.TrimSuffix(config.BaseURL, "/"),
		client: &http.Client{
			Timeout: config.Timeout,
		},
		models:  config.Models,
		logger:  logger,
		metrics: metrics,
		usage: llm.LLMUsage{
			LastReset: time.Now(),
		},
		started: true,
	}

	// Fetch available models if not pre-configured
	if len(provider.models) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if models, err := provider.fetchModels(ctx); err == nil {
			provider.models = models
		} else if logger != nil {
			logger.Warn("failed to fetch ollama models", forge.F("error", err.Error()))
		}
	}

	if logger != nil {
		logger.Info("ollama provider initialized",
			forge.F("base_url", provider.baseURL),
			forge.F("models", provider.models))
	}

	return provider, nil
}

// Name returns the provider name.
func (p *OllamaProvider) Name() string {
	return p.name
}

// Models returns the available models.
func (p *OllamaProvider) Models() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.models
}

// Chat performs a chat completion request.
func (p *OllamaProvider) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	startTime := time.Now()

	// Convert to Ollama format
	ollamaReq := p.convertChatRequest(request)

	// Make request
	response, err := p.makeChatRequest(ctx, ollamaReq)
	if err != nil {
		p.recordError()
		return llm.ChatResponse{}, err
	}

	// Convert response
	chatResp := p.convertChatResponse(response, request.RequestID)

	// Update usage
	p.recordUsage(response.PromptEvalCount, response.EvalCount, time.Since(startTime))

	return chatResp, nil
}

// Complete performs a text completion request.
func (p *OllamaProvider) Complete(ctx context.Context, request llm.CompletionRequest) (llm.CompletionResponse, error) {
	startTime := time.Now()

	// Convert to Ollama format
	ollamaReq := p.convertCompletionRequest(request)

	// Make request
	response, err := p.makeGenerateRequest(ctx, ollamaReq)
	if err != nil {
		p.recordError()
		return llm.CompletionResponse{}, err
	}

	// Convert response
	compResp := p.convertCompletionResponse(response, request.RequestID)

	// Update usage
	p.recordUsage(response.PromptEvalCount, response.EvalCount, time.Since(startTime))

	return compResp, nil
}

// Embed performs an embedding request.
func (p *OllamaProvider) Embed(ctx context.Context, request llm.EmbeddingRequest) (llm.EmbeddingResponse, error) {
	startTime := time.Now()

	// Convert to Ollama format
	ollamaReq := p.convertEmbeddingRequest(request)

	// Make request
	response, err := p.makeEmbedRequest(ctx, ollamaReq)
	if err != nil {
		p.recordError()
		return llm.EmbeddingResponse{}, err
	}

	// Convert response
	embedResp := p.convertEmbeddingResponse(response, request.RequestID)

	// Update usage
	p.recordUsage(0, 0, time.Since(startTime))

	return embedResp, nil
}

// ChatStream performs a streaming chat completion request.
func (p *OllamaProvider) ChatStream(ctx context.Context, request llm.ChatRequest, handler func(llm.ChatStreamEvent) error) error {
	startTime := time.Now()

	// Convert to Ollama format
	ollamaReq := p.convertChatRequest(request)
	ollamaReq.Stream = true

	// Prepare request
	jsonData, err := json.Marshal(ollamaReq)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := p.client.Do(req)
	if err != nil {
		p.recordError()
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		p.recordError()
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama API error: %d - %s", resp.StatusCode, string(body))
	}

	// Process stream
	scanner := bufio.NewScanner(resp.Body)
	var fullContent strings.Builder
	var totalPromptTokens, totalCompletionTokens int

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var chunk ollamaChatResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			if p.logger != nil {
				p.logger.Warn("failed to parse streaming chunk", forge.F("error", err.Error()))
			}
			continue
		}

		// Build event
		event := llm.ChatStreamEvent{
			Type:      "message",
			ID:        request.RequestID,
			Model:     chunk.Model,
			Provider:  p.name,
			RequestID: request.RequestID,
		}

		if chunk.Message.Content != "" {
			fullContent.WriteString(chunk.Message.Content)
			event.Choices = []llm.ChatChoice{
				{
					Index: 0,
					Delta: &llm.ChatMessage{
						Role:    chunk.Message.Role,
						Content: chunk.Message.Content,
					},
				},
			}
		}

		// Handle tool calls
		if len(chunk.Message.ToolCalls) > 0 {
			event.Type = "tool_call"
			toolCalls := make([]llm.ToolCall, len(chunk.Message.ToolCalls))
			for i, tc := range chunk.Message.ToolCalls {
				args, _ := json.Marshal(tc.Function.Arguments)
				toolCalls[i] = llm.ToolCall{
					ID:   fmt.Sprintf("call_%d", i),
					Type: "function",
					Function: &llm.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: string(args),
					},
				}
			}
			event.Choices = []llm.ChatChoice{
				{
					Index: 0,
					Delta: &llm.ChatMessage{
						Role:      "assistant",
						ToolCalls: toolCalls,
					},
				},
			}
		}

		if chunk.Done {
			totalPromptTokens = chunk.PromptEvalCount
			totalCompletionTokens = chunk.EvalCount

			event.Type = "done"
			event.Usage = &llm.LLMUsage{
				InputTokens:  int64(totalPromptTokens),
				OutputTokens: int64(totalCompletionTokens),
				TotalTokens:  int64(totalPromptTokens + totalCompletionTokens),
			}
			event.Choices = []llm.ChatChoice{
				{
					Index: 0,
					Message: llm.ChatMessage{
						Role:    "assistant",
						Content: fullContent.String(),
					},
					FinishReason: p.convertDoneReason(chunk.DoneReason),
				},
			}
		}

		// Send event to handler
		if err := handler(event); err != nil {
			return err
		}

		if chunk.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		p.recordError()
		return fmt.Errorf("stream scan error: %w", err)
	}

	// Update usage
	p.recordUsage(totalPromptTokens, totalCompletionTokens, time.Since(startTime))

	return nil
}

// GetUsage returns current usage statistics.
func (p *OllamaProvider) GetUsage() llm.LLMUsage {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.usage
}

// HealthCheck performs a health check.
func (p *OllamaProvider) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/version", nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("health check failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Stop stops the provider.
func (p *OllamaProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = false
	return nil
}

// Helper methods

func (p *OllamaProvider) convertChatRequest(request llm.ChatRequest) *ollamaChatRequest {
	ollamaReq := &ollamaChatRequest{
		Model:    request.Model,
		Messages: make([]ollamaMessage, len(request.Messages)),
		Options:  &ollamaOptions{},
	}

	// Convert messages
	for i, msg := range request.Messages {
		ollamaReq.Messages[i] = ollamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Convert options
	if request.Temperature != nil {
		ollamaReq.Options.Temperature = request.Temperature
	}
	if request.MaxTokens != nil {
		ollamaReq.Options.NumPredict = request.MaxTokens
	}
	if request.TopP != nil {
		ollamaReq.Options.TopP = request.TopP
	}
	if request.TopK != nil {
		ollamaReq.Options.TopK = request.TopK
	}
	if len(request.Stop) > 0 {
		ollamaReq.Options.Stop = request.Stop
	}

	// Convert tools
	if len(request.Tools) > 0 {
		ollamaReq.Tools = make([]ollamaTool, len(request.Tools))
		for i, tool := range request.Tools {
			if tool.Function != nil {
				ollamaReq.Tools[i] = ollamaTool{
					Type: "function",
					Function: ollamaFunctionDef{
						Name:        tool.Function.Name,
						Description: tool.Function.Description,
						Parameters:  tool.Function.Parameters,
					},
				}
			}
		}
	}

	return ollamaReq
}

func (p *OllamaProvider) convertCompletionRequest(request llm.CompletionRequest) *ollamaGenerateRequest {
	ollamaReq := &ollamaGenerateRequest{
		Model:   request.Model,
		Prompt:  request.Prompt,
		Options: &ollamaOptions{},
	}

	// Convert options
	if request.Temperature != nil {
		ollamaReq.Options.Temperature = request.Temperature
	}
	if request.MaxTokens != nil {
		ollamaReq.Options.NumPredict = request.MaxTokens
	}
	if request.TopP != nil {
		ollamaReq.Options.TopP = request.TopP
	}
	if request.TopK != nil {
		ollamaReq.Options.TopK = request.TopK
	}
	if len(request.Stop) > 0 {
		ollamaReq.Options.Stop = request.Stop
	}

	return ollamaReq
}

func (p *OllamaProvider) convertEmbeddingRequest(request llm.EmbeddingRequest) *ollamaEmbedRequest {
	ollamaReq := &ollamaEmbedRequest{
		Model: request.Model,
	}

	// Handle single input or multiple inputs
	if len(request.Input) == 1 {
		ollamaReq.Prompt = request.Input[0]
	} else {
		ollamaReq.Input = request.Input
	}

	return ollamaReq
}

func (p *OllamaProvider) convertChatResponse(response *ollamaChatResponse, requestID string) llm.ChatResponse {
	return llm.ChatResponse{
		ID:        requestID,
		Model:     response.Model,
		Provider:  p.name,
		RequestID: requestID,
		Choices: []llm.ChatChoice{
			{
				Index: 0,
				Message: llm.ChatMessage{
					Role:    response.Message.Role,
					Content: response.Message.Content,
				},
				FinishReason: p.convertDoneReason(response.DoneReason),
			},
		},
		Usage: &llm.LLMUsage{
			InputTokens:  int64(response.PromptEvalCount),
			OutputTokens: int64(response.EvalCount),
			TotalTokens:  int64(response.PromptEvalCount + response.EvalCount),
		},
	}
}

func (p *OllamaProvider) convertCompletionResponse(response *ollamaGenerateResponse, requestID string) llm.CompletionResponse {
	return llm.CompletionResponse{
		ID:        requestID,
		Model:     response.Model,
		Provider:  p.name,
		RequestID: requestID,
		Choices: []llm.CompletionChoice{
			{
				Index:        0,
				Text:         response.Response,
				FinishReason: p.convertDoneReason(response.DoneReason),
			},
		},
		Usage: &llm.LLMUsage{
			InputTokens:  int64(response.PromptEvalCount),
			OutputTokens: int64(response.EvalCount),
			TotalTokens:  int64(response.PromptEvalCount + response.EvalCount),
		},
	}
}

func (p *OllamaProvider) convertEmbeddingResponse(response *ollamaEmbedResponse, requestID string) llm.EmbeddingResponse {
	embedResp := llm.EmbeddingResponse{
		Object:    "list",
		Model:     response.Model,
		Provider:  p.name,
		RequestID: requestID,
	}

	// Handle single or multiple embeddings
	if len(response.Embedding) > 0 {
		embedResp.Data = []llm.EmbeddingData{
			{
				Object:    "embedding",
				Index:     0,
				Embedding: response.Embedding,
			},
		}
	} else if len(response.Embeddings) > 0 {
		embedResp.Data = make([]llm.EmbeddingData, len(response.Embeddings))
		for i, emb := range response.Embeddings {
			embedResp.Data[i] = llm.EmbeddingData{
				Object:    "embedding",
				Index:     i,
				Embedding: emb,
			}
		}
	}

	return embedResp
}

func (p *OllamaProvider) convertDoneReason(reason string) string {
	switch reason {
	case "stop":
		return "stop"
	case "length":
		return "length"
	default:
		return "stop"
	}
}

func (p *OllamaProvider) makeChatRequest(ctx context.Context, request *ollamaChatRequest) (*ollamaChatResponse, error) {
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error: %d - %s", resp.StatusCode, string(body))
	}

	// For non-streaming, read the final response
	var result ollamaChatResponse
	decoder := json.NewDecoder(resp.Body)

	for {
		var chunk ollamaChatResponse
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode response: %w", err)
		}

		if chunk.Done {
			result = chunk
			break
		}

		// Accumulate content for non-final chunks
		result.Message.Content += chunk.Message.Content
	}

	return &result, nil
}

func (p *OllamaProvider) makeGenerateRequest(ctx context.Context, request *ollamaGenerateRequest) (*ollamaGenerateResponse, error) {
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/generate", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error: %d - %s", resp.StatusCode, string(body))
	}

	// For non-streaming, read the final response
	var result ollamaGenerateResponse
	decoder := json.NewDecoder(resp.Body)

	for {
		var chunk ollamaGenerateResponse
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode response: %w", err)
		}

		if chunk.Done {
			result = chunk
			break
		}

		// Accumulate response for non-final chunks
		result.Response += chunk.Response
	}

	return &result, nil
}

func (p *OllamaProvider) makeEmbedRequest(ctx context.Context, request *ollamaEmbedRequest) (*ollamaEmbedResponse, error) {
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Ollama uses /api/embeddings for batch or /api/embed for single
	endpoint := "/api/embed"
	if request.Input != nil {
		endpoint = "/api/embeddings"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error: %d - %s", resp.StatusCode, string(body))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

func (p *OllamaProvider) fetchModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch models failed: %d - %s", resp.StatusCode, string(body))
	}

	var modelsResp ollamaModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]string, len(modelsResp.Models))
	for i, model := range modelsResp.Models {
		models[i] = model.Name
	}

	return models, nil
}

func (p *OllamaProvider) recordUsage(promptTokens, completionTokens int, latency time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.usage.InputTokens += int64(promptTokens)
	p.usage.OutputTokens += int64(completionTokens)
	p.usage.TotalTokens += int64(promptTokens + completionTokens)
	p.usage.RequestCount++

	// Update average latency
	if p.usage.RequestCount == 1 {
		p.usage.AverageLatency = latency
	} else {
		p.usage.AverageLatency = (p.usage.AverageLatency + latency) / 2
	}
}

func (p *OllamaProvider) recordError() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usage.ErrorCount++
}

// Ensure OllamaProvider implements StreamingProvider interface.
var _ llm.StreamingProvider = (*OllamaProvider)(nil)
