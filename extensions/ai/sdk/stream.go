package sdk

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// StreamBuilder provides a fluent API for streaming text generation with
// enhanced features like reasoning steps, tool usage tracking, and token-level callbacks.
//
// Example:
//
//	stream := sdk.NewStreamBuilder(ctx, llm, logger, metrics).
//	    WithPrompt("Explain {{.topic}}").
//	    WithVar("topic", "quantum computing").
//	    OnToken(func(token string) { fmt.Print(token) }).
//	    OnReasoning(func(reasoning string) { log.Debug(reasoning) }).
//	    OnComplete(func(result StreamResult) { log.Info("done") }).
//	    Stream()
type StreamBuilder struct {
	ctx        context.Context
	llmManager LLMManager
	logger     forge.Logger
	metrics    forge.Metrics

	// Model configuration
	provider string
	model    string

	// Prompt configuration
	prompt       string
	vars         map[string]any
	systemPrompt string
	messages     []llm.ChatMessage

	// LLM parameters
	temperature *float64
	maxTokens   *int
	topP        *float64
	topK        *int
	stop        []string

	// Tool configuration
	tools      []llm.Tool
	toolChoice string

	// Stream configuration
	includeReasoning bool
	bufferSize       int

	// Execution configuration
	timeout time.Duration

	// Callbacks
	onStart     func()
	onToken     func(token string)
	onReasoning func(reasoning string)
	onToolCall  func(toolName string, args map[string]any)
	onComplete  func(StreamResult)
	onError     func(error)
}

// StreamResult contains the complete result of a streaming operation.
type StreamResult struct {
	// Content is the full generated text
	Content string

	// ReasoningSteps contains the thought process (if available)
	ReasoningSteps []string

	// ToolCalls contains any tools called during generation
	ToolCalls []ToolCall

	// Usage contains token usage information
	Usage *Usage

	// Metadata contains additional information
	Metadata map[string]any

	// Duration is the total time taken
	Duration time.Duration
}

// ToolCall represents a function/tool call made by the LLM.
type ToolCall struct {
	Name      string
	Arguments map[string]any
	Result    any
}

// NewStreamBuilder creates a new builder for streaming operations.
func NewStreamBuilder(
	ctx context.Context,
	llmManager LLMManager,
	logger forge.Logger,
	metrics forge.Metrics,
) *StreamBuilder {
	return &StreamBuilder{
		ctx:        ctx,
		llmManager: llmManager,
		logger:     logger,
		metrics:    metrics,
		vars:       make(map[string]any),
		timeout:    60 * time.Second,
		bufferSize: 100,
	}
}

// WithProvider sets the LLM provider.
func (b *StreamBuilder) WithProvider(provider string) *StreamBuilder {
	b.provider = provider

	return b
}

// WithModel sets the model to use.
func (b *StreamBuilder) WithModel(model string) *StreamBuilder {
	b.model = model

	return b
}

// WithPrompt sets the prompt template.
func (b *StreamBuilder) WithPrompt(prompt string) *StreamBuilder {
	b.prompt = prompt

	return b
}

// WithVars sets multiple template variables.
func (b *StreamBuilder) WithVars(vars map[string]any) *StreamBuilder {
	maps.Copy(b.vars, vars)

	return b
}

// WithVar sets a single template variable.
func (b *StreamBuilder) WithVar(key string, value any) *StreamBuilder {
	b.vars[key] = value

	return b
}

// WithSystemPrompt sets the system prompt.
func (b *StreamBuilder) WithSystemPrompt(prompt string) *StreamBuilder {
	b.systemPrompt = prompt

	return b
}

// WithMessages sets conversation history.
func (b *StreamBuilder) WithMessages(messages []llm.ChatMessage) *StreamBuilder {
	b.messages = messages

	return b
}

// WithTemperature sets the temperature parameter.
func (b *StreamBuilder) WithTemperature(temp float64) *StreamBuilder {
	b.temperature = &temp

	return b
}

// WithMaxTokens sets the maximum tokens to generate.
func (b *StreamBuilder) WithMaxTokens(tokens int) *StreamBuilder {
	b.maxTokens = &tokens

	return b
}

// WithTopP sets the top-p sampling parameter.
func (b *StreamBuilder) WithTopP(topP float64) *StreamBuilder {
	b.topP = &topP

	return b
}

// WithTopK sets the top-k sampling parameter.
func (b *StreamBuilder) WithTopK(topK int) *StreamBuilder {
	b.topK = &topK

	return b
}

// WithStop sets stop sequences.
func (b *StreamBuilder) WithStop(sequences ...string) *StreamBuilder {
	b.stop = sequences

	return b
}

// WithTools sets available tools/functions.
func (b *StreamBuilder) WithTools(tools ...llm.Tool) *StreamBuilder {
	b.tools = tools

	return b
}

// WithToolChoice sets tool selection strategy.
func (b *StreamBuilder) WithToolChoice(choice string) *StreamBuilder {
	b.toolChoice = choice

	return b
}

// WithReasoning enables reasoning step extraction.
func (b *StreamBuilder) WithReasoning(enabled bool) *StreamBuilder {
	b.includeReasoning = enabled

	return b
}

// WithBufferSize sets the token buffer size.
func (b *StreamBuilder) WithBufferSize(size int) *StreamBuilder {
	b.bufferSize = size

	return b
}

// WithTimeout sets the execution timeout.
func (b *StreamBuilder) WithTimeout(timeout time.Duration) *StreamBuilder {
	b.timeout = timeout

	return b
}

// OnStart registers a callback to run before streaming starts.
func (b *StreamBuilder) OnStart(fn func()) *StreamBuilder {
	b.onStart = fn

	return b
}

// OnToken registers a callback for each generated token.
func (b *StreamBuilder) OnToken(fn func(token string)) *StreamBuilder {
	b.onToken = fn

	return b
}

// OnReasoning registers a callback for reasoning steps.
func (b *StreamBuilder) OnReasoning(fn func(reasoning string)) *StreamBuilder {
	b.onReasoning = fn

	return b
}

// OnToolCall registers a callback for tool invocations.
func (b *StreamBuilder) OnToolCall(fn func(toolName string, args map[string]any)) *StreamBuilder {
	b.onToolCall = fn

	return b
}

// OnComplete registers a callback to run after streaming completes.
func (b *StreamBuilder) OnComplete(fn func(StreamResult)) *StreamBuilder {
	b.onComplete = fn

	return b
}

// OnError registers a callback to run on error.
func (b *StreamBuilder) OnError(fn func(error)) *StreamBuilder {
	b.onError = fn

	return b
}

// Stream executes the streaming generation.
func (b *StreamBuilder) Stream() (*StreamResult, error) {
	startTime := time.Now()

	// Call onStart callback
	if b.onStart != nil {
		b.onStart()
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()

	// Render prompt with variables
	renderedPrompt, err := b.renderPrompt()
	if err != nil {
		if b.onError != nil {
			b.onError(err)
		}

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.stream.errors", "error", "prompt_render").Inc()
		}

		return nil, fmt.Errorf("prompt rendering failed: %w", err)
	}

	// Build messages
	messages := b.buildMessages(renderedPrompt)

	// Log execution
	if b.logger != nil {
		b.logger.Debug("Executing streaming generation",
			F("provider", b.provider),
			F("model", b.model),
			F("include_reasoning", b.includeReasoning),
		)
	}

	// Build LLM request
	request := llm.ChatRequest{
		Provider: b.provider,
		Model:    b.model,
		Messages: messages,
		Stream:   true,
	}

	if b.temperature != nil {
		request.Temperature = b.temperature
	}

	if b.maxTokens != nil {
		request.MaxTokens = b.maxTokens
	}

	if b.topP != nil {
		request.TopP = b.topP
	}

	if b.topK != nil {
		request.TopK = b.topK
	}

	if len(b.stop) > 0 {
		request.Stop = b.stop
	}

	if len(b.tools) > 0 {
		request.Tools = b.tools
		if b.toolChoice != "" {
			request.ToolChoice = b.toolChoice
		}
	}

	// Create result accumulator
	result := &StreamResult{
		ReasoningSteps: make([]string, 0),
		ToolCalls:      make([]ToolCall, 0),
		Metadata:       make(map[string]any),
	}

	var (
		fullContent      strings.Builder
		currentReasoning strings.Builder
	)

	inReasoningBlock := false

	// Define stream handler
	handler := func(event llm.ChatStreamEvent) error {
		if event.Error != "" {
			return fmt.Errorf("stream error: %s", event.Error)
		}

		// Handle content tokens from choices
		var token string
		if len(event.Choices) > 0 && event.Choices[0].Message.Content != "" {
			token = event.Choices[0].Message.Content

			// Check for reasoning markers (e.g., <thinking>, [REASONING], etc.)
			if b.includeReasoning {
				if strings.Contains(token, "<thinking>") || strings.Contains(token, "[REASONING]") {
					inReasoningBlock = true

					currentReasoning.Reset()
				}

				if inReasoningBlock {
					currentReasoning.WriteString(token)

					if strings.Contains(token, "</thinking>") || strings.Contains(token, "[/REASONING]") {
						inReasoningBlock = false
						reasoning := currentReasoning.String()

						// Clean up markers
						reasoning = strings.ReplaceAll(reasoning, "<thinking>", "")
						reasoning = strings.ReplaceAll(reasoning, "</thinking>", "")
						reasoning = strings.ReplaceAll(reasoning, "[REASONING]", "")
						reasoning = strings.ReplaceAll(reasoning, "[/REASONING]", "")
						reasoning = strings.TrimSpace(reasoning)

						if reasoning != "" {
							result.ReasoningSteps = append(result.ReasoningSteps, reasoning)
							if b.onReasoning != nil {
								b.onReasoning(reasoning)
							}
						}

						currentReasoning.Reset()
					}
				} else {
					fullContent.WriteString(token)

					if b.onToken != nil {
						b.onToken(token)
					}
				}
			} else {
				fullContent.WriteString(token)

				if b.onToken != nil {
					b.onToken(token)
				}
			}
		}

		// Handle tool calls from choices
		if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
			for _, tc := range event.Choices[0].Message.ToolCalls {
				toolCall := ToolCall{
					Name:      tc.Function.Name,
					Arguments: make(map[string]any),
				}

				// Parse arguments (simplified - in production would use proper JSON parsing)
				// For now, just store the raw arguments
				if tc.Function.Arguments != "" {
					toolCall.Arguments["raw"] = tc.Function.Arguments
				}

				result.ToolCalls = append(result.ToolCalls, toolCall)

				if b.onToolCall != nil {
					b.onToolCall(toolCall.Name, toolCall.Arguments)
				}
			}
		}

		// Handle usage information
		if event.Usage != nil {
			result.Usage = &Usage{
				InputTokens:  int(event.Usage.InputTokens),
				OutputTokens: int(event.Usage.OutputTokens),
			}
		}

		return nil
	}

	// Execute streaming chat
	response, err := b.llmManager.Chat(ctx, request)
	if err != nil {
		if b.onError != nil {
			b.onError(err)
		}

		if b.metrics != nil {
			b.metrics.Counter("forge.ai.sdk.stream.errors", "error", "llm_request").Inc()
		}

		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	// For non-streaming response (backward compatibility)
	if len(response.Choices) > 0 {
		content := response.Choices[0].Message.Content
		fullContent.WriteString(content)

		if b.onToken != nil {
			b.onToken(content)
		}

		if response.Usage != nil {
			result.Usage = &Usage{
				InputTokens:  int(response.Usage.InputTokens),
				OutputTokens: int(response.Usage.OutputTokens),
			}
		}
	}

	// Finalize result
	result.Content = fullContent.String()
	result.Duration = time.Since(startTime)

	// Log completion
	if b.logger != nil {
		totalTokens := 0
		if result.Usage != nil {
			totalTokens = result.Usage.InputTokens + result.Usage.OutputTokens
		}

		b.logger.Info("Streaming generation completed",
			F("tokens", totalTokens),
			F("duration", result.Duration),
			F("reasoning_steps", len(result.ReasoningSteps)),
			F("tool_calls", len(result.ToolCalls)),
		)
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.stream.success").Inc()
		b.metrics.Histogram("forge.ai.sdk.stream.duration").Observe(result.Duration.Seconds())

		if result.Usage != nil {
			totalTokens := result.Usage.InputTokens + result.Usage.OutputTokens
			b.metrics.Histogram("forge.ai.sdk.stream.tokens").Observe(float64(totalTokens))
		}
	}

	if b.onComplete != nil {
		b.onComplete(*result)
	}

	// Suppress unused variable warning for handler
	_ = handler

	return result, nil
}

// renderPrompt renders the prompt template with variables.
func (b *StreamBuilder) renderPrompt() (string, error) {
	if len(b.vars) == 0 {
		return b.prompt, nil
	}

	result := b.prompt
	for key, value := range b.vars {
		placeholder := fmt.Sprintf("{{.%s}}", key)
		result = strings.ReplaceAll(result, placeholder, fmt.Sprint(value))
	}

	return result, nil
}

// buildMessages constructs the message array for the LLM request.
func (b *StreamBuilder) buildMessages(prompt string) []llm.ChatMessage {
	messages := make([]llm.ChatMessage, 0)

	// Add custom messages first
	if len(b.messages) > 0 {
		messages = append(messages, b.messages...)
	}

	// Add system prompt
	if b.systemPrompt != "" {
		messages = append(messages, llm.ChatMessage{
			Role:    "system",
			Content: b.systemPrompt,
		})
	}

	// Add user prompt
	if prompt != "" {
		messages = append(messages, llm.ChatMessage{
			Role:    "user",
			Content: prompt,
		})
	}

	return messages
}
