package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// GenerateBuilder provides a fluent interface for text generation.
type GenerateBuilder struct {
	ctx      context.Context
	provider string
	model    string
	prompt   string
	vars     map[string]any

	// Generation parameters
	temperature *float64
	maxTokens   *int
	topP        *float64
	topK        *int
	stop        []string

	// Advanced options
	tools      []llm.Tool
	toolChoice string

	// System configuration
	systemPrompt string
	messages     []llm.ChatMessage

	// Execution options
	timeout  time.Duration
	cache    bool
	cacheTTL time.Duration

	// Callbacks
	onStart    func()
	onComplete func(Result)
	onError    func(error)

	// Internal
	llmManager LLMManager
	logger     forge.Logger
	metrics    forge.Metrics
}

// LLMManager interface for LLM operations.
type LLMManager interface {
	Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error)
}

// NewGenerateBuilder creates a new generate builder.
func NewGenerateBuilder(ctx context.Context, llmManager LLMManager, logger forge.Logger, metrics forge.Metrics) *GenerateBuilder {
	return &GenerateBuilder{
		ctx:        ctx,
		vars:       make(map[string]any),
		llmManager: llmManager,
		logger:     logger,
		metrics:    metrics,
		timeout:    30 * time.Second,
		cache:      false,
	}
}

// WithProvider sets the LLM provider.
func (b *GenerateBuilder) WithProvider(provider string) *GenerateBuilder {
	b.provider = provider

	return b
}

// WithModel sets the model to use.
func (b *GenerateBuilder) WithModel(model string) *GenerateBuilder {
	b.model = model

	return b
}

// WithPrompt sets the prompt template.
func (b *GenerateBuilder) WithPrompt(prompt string) *GenerateBuilder {
	b.prompt = prompt

	return b
}

// WithVars sets template variables.
func (b *GenerateBuilder) WithVars(vars map[string]any) *GenerateBuilder {
	b.vars = vars

	return b
}

// WithVar sets a single template variable.
func (b *GenerateBuilder) WithVar(key string, value any) *GenerateBuilder {
	b.vars[key] = value

	return b
}

// WithSystemPrompt sets the system prompt.
func (b *GenerateBuilder) WithSystemPrompt(prompt string) *GenerateBuilder {
	b.systemPrompt = prompt

	return b
}

// WithTemperature sets the temperature (0.0-2.0).
func (b *GenerateBuilder) WithTemperature(temp float64) *GenerateBuilder {
	b.temperature = &temp

	return b
}

// WithMaxTokens sets the maximum tokens to generate.
func (b *GenerateBuilder) WithMaxTokens(tokens int) *GenerateBuilder {
	b.maxTokens = &tokens

	return b
}

// WithTopP sets the top-p sampling parameter.
func (b *GenerateBuilder) WithTopP(topP float64) *GenerateBuilder {
	b.topP = &topP

	return b
}

// WithTopK sets the top-k sampling parameter.
func (b *GenerateBuilder) WithTopK(topK int) *GenerateBuilder {
	b.topK = &topK

	return b
}

// WithStop sets stop sequences.
func (b *GenerateBuilder) WithStop(stop ...string) *GenerateBuilder {
	b.stop = stop

	return b
}

// WithTools adds tools for function calling.
func (b *GenerateBuilder) WithTools(tools ...llm.Tool) *GenerateBuilder {
	b.tools = append(b.tools, tools...)

	return b
}

// WithToolChoice sets the tool choice strategy.
func (b *GenerateBuilder) WithToolChoice(choice string) *GenerateBuilder {
	b.toolChoice = choice

	return b
}

// WithTimeout sets the request timeout.
func (b *GenerateBuilder) WithTimeout(timeout time.Duration) *GenerateBuilder {
	b.timeout = timeout

	return b
}

// WithCache enables caching with TTL.
func (b *GenerateBuilder) WithCache(ttl time.Duration) *GenerateBuilder {
	b.cache = true
	b.cacheTTL = ttl

	return b
}

// WithMessages sets the full conversation history.
func (b *GenerateBuilder) WithMessages(messages []llm.ChatMessage) *GenerateBuilder {
	b.messages = messages

	return b
}

// OnStart sets a callback for when generation starts.
func (b *GenerateBuilder) OnStart(fn func()) *GenerateBuilder {
	b.onStart = fn

	return b
}

// OnComplete sets a callback for when generation completes.
func (b *GenerateBuilder) OnComplete(fn func(Result)) *GenerateBuilder {
	b.onComplete = fn

	return b
}

// OnError sets a callback for when an error occurs.
func (b *GenerateBuilder) OnError(fn func(error)) *GenerateBuilder {
	b.onError = fn

	return b
}

// Execute performs the generation.
func (b *GenerateBuilder) Execute() (*Result, error) {
	startTime := time.Now()

	// Trigger start callback
	if b.onStart != nil {
		b.onStart()
	}

	// Apply timeout
	ctx := b.ctx
	if b.timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	// Render prompt template
	renderedPrompt, err := b.renderPrompt()
	if err != nil {
		if b.onError != nil {
			b.onError(err)
		}

		return nil, fmt.Errorf("failed to render prompt: %w", err)
	}

	// Build messages
	messages := b.buildMessages(renderedPrompt)

	// Create request
	request := llm.ChatRequest{
		Provider:    b.provider,
		Model:       b.model,
		Messages:    messages,
		Temperature: b.temperature,
		MaxTokens:   b.maxTokens,
		TopP:        b.topP,
		TopK:        b.topK,
		Stop:        b.stop,
		Tools:       b.tools,
		ToolChoice:  b.toolChoice,
	}

	// Log request
	if b.logger != nil {
		b.logger.Debug("executing generation",
			forge.String("provider", b.provider),
			forge.String("model", b.model),
			forge.Int("prompt_length", len(renderedPrompt)),
		)
	}

	// Execute request
	response, err := b.llmManager.Chat(ctx, request)
	if err != nil {
		if b.onError != nil {
			b.onError(err)
		}

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.generate.errors",
				"provider", b.provider,
				"model", b.model,
			).Inc()
		}

		return nil, fmt.Errorf("failed to generate: %w", err)
	}

	// Build result
	result := &Result{
		Metadata:     make(map[string]any),
		FinishReason: "unknown",
	}

	if len(response.Choices) > 0 {
		result.Content = response.Choices[0].Message.Content
		result.FinishReason = response.Choices[0].FinishReason
	}

	if response.Usage != nil {
		result.Usage = &Usage{
			Provider:     b.provider,
			Model:        b.model,
			InputTokens:  int(response.Usage.InputTokens),
			OutputTokens: int(response.Usage.OutputTokens),
			Cost:         response.Usage.Cost,
			Timestamp:    time.Now(),
		}
	}

	result.Metadata["duration"] = time.Since(startTime)
	result.Metadata["response_id"] = response.ID

	// Trigger complete callback
	if b.onComplete != nil {
		b.onComplete(*result)
	}

	// Record metrics
	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.generate.success",
			"provider", b.provider,
			"model", b.model,
		).Inc()

		b.metrics.Histogram("forge.ai.sdk.generate.duration",
			"provider", b.provider,
			"model", b.model,
		).Observe(time.Since(startTime).Seconds())

		if result.Usage != nil {
			b.metrics.Counter("forge.ai.sdk.generate.tokens",
				"provider", b.provider,
				"model", b.model,
				"type", "input",
			).Add(float64(result.Usage.InputTokens))

			b.metrics.Counter("forge.ai.sdk.generate.tokens",
				"provider", b.provider,
				"model", b.model,
				"type", "output",
			).Add(float64(result.Usage.OutputTokens))
		}
	}

	return result, nil
}

// renderPrompt renders the prompt template with variables.
func (b *GenerateBuilder) renderPrompt() (string, error) {
	if len(b.vars) == 0 {
		return b.prompt, nil
	}

	// Simple template rendering (can be enhanced with text/template)
	rendered := b.prompt
	for key, value := range b.vars {
		placeholder := fmt.Sprintf("{{.%s}}", key)
		rendered = replaceAll(rendered, placeholder, fmt.Sprintf("%v", value))
	}

	return rendered, nil
}

// buildMessages builds the chat messages.
func (b *GenerateBuilder) buildMessages(prompt string) []llm.ChatMessage {
	messages := make([]llm.ChatMessage, 0)

	// Add system prompt if provided
	if b.systemPrompt != "" {
		messages = append(messages, llm.ChatMessage{
			Role:    "system",
			Content: b.systemPrompt,
		})
	}

	// Add existing messages
	messages = append(messages, b.messages...)

	// Add user prompt
	if prompt != "" {
		messages = append(messages, llm.ChatMessage{
			Role:    "user",
			Content: prompt,
		})
	}

	return messages
}

// replaceAll replaces all occurrences of old with new in s.
func replaceAll(s, old, new string) string {
	result := s
	for {
		newResult := replaceFirst(result, old, new)
		if newResult == result {
			break
		}

		result = newResult
	}

	return result
}

// replaceFirst replaces the first occurrence of old with new in s.
func replaceFirst(s, old, new string) string {
	idx := indexOf(s, old)
	if idx == -1 {
		return s
	}

	return s[:idx] + new + s[idx+len(old):]
}

// indexOf returns the index of the first occurrence of substr in s, or -1.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}

	return -1
}

// String returns the generated content (convenience method).
func (r *Result) String() string {
	return r.Content
}

// JSON marshals the result to JSON.
func (r *Result) JSON() ([]byte, error) {
	return json.Marshal(r)
}
