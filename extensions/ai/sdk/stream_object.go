package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// StreamObjectBuilder provides a fluent API for streaming structured outputs
// with partial updates as fields arrive. This enables real-time UI updates
// where you can render parts of the response before the full object is complete.
//
// Example:
//
//	type Person struct {
//	    Name    string   `json:"name" description:"Full name"`
//	    Age     int      `json:"age" description:"Age in years"`
//	    Hobbies []string `json:"hobbies" description:"List of hobbies"`
//	}
//
//	result, err := sdk.NewStreamObjectBuilder[Person](ctx, llm, logger, metrics).
//	    WithPrompt("Extract person info from: {{.text}}").
//	    WithVar("text", "John Doe is 30 years old and loves hiking").
//	    OnPartialObject(func(partial Person, paths [][]string) {
//	        // Update UI with partial data as it arrives
//	        fmt.Printf("Got fields: %v\n", paths)
//	    }).
//	    OnFieldComplete(func(path []string, value any) {
//	        fmt.Printf("Field %s = %v\n", strings.Join(path, "."), value)
//	    }).
//	    Stream()
type StreamObjectBuilder[T any] struct {
	ctx        context.Context
	llmManager StreamingLLMManager
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

	// Schema configuration
	schema       map[string]any
	schemaStrict bool

	// Execution configuration
	timeout time.Duration

	// Streaming callbacks
	onStart         func()
	onPartialObject func(partial T, completedPaths [][]string)
	onFieldComplete func(path []string, value any)
	onDelta         func(delta string)
	onComplete      func(result *StreamObjectResult[T])
	onError         func(error)

	// Validation
	validators []func(T) error
}

// StreamObjectResult contains the complete result of a streaming object generation.
type StreamObjectResult[T any] struct {
	// Object is the final parsed object
	Object T

	// RawJSON is the complete JSON string
	RawJSON string

	// Usage contains token usage information
	Usage *Usage

	// Duration is the total streaming time
	Duration time.Duration

	// FieldsReceived tracks which fields were streamed
	FieldsReceived [][]string

	// Model used for generation
	Model    string
	Provider string
}

// NewStreamObjectBuilder creates a new builder for streaming structured output.
func NewStreamObjectBuilder[T any](
	ctx context.Context,
	llmManager StreamingLLMManager,
	logger forge.Logger,
	metrics forge.Metrics,
) *StreamObjectBuilder[T] {
	return &StreamObjectBuilder[T]{
		ctx:        ctx,
		llmManager: llmManager,
		logger:     logger,
		metrics:    metrics,
		vars:       make(map[string]any),
		timeout:    60 * time.Second,
	}
}

// WithProvider sets the LLM provider.
func (b *StreamObjectBuilder[T]) WithProvider(provider string) *StreamObjectBuilder[T] {
	b.provider = provider

	return b
}

// WithModel sets the model to use.
func (b *StreamObjectBuilder[T]) WithModel(model string) *StreamObjectBuilder[T] {
	b.model = model

	return b
}

// WithPrompt sets the prompt template.
func (b *StreamObjectBuilder[T]) WithPrompt(prompt string) *StreamObjectBuilder[T] {
	b.prompt = prompt

	return b
}

// WithVars sets multiple template variables.
func (b *StreamObjectBuilder[T]) WithVars(vars map[string]any) *StreamObjectBuilder[T] {
	maps.Copy(b.vars, vars)

	return b
}

// WithVar sets a single template variable.
func (b *StreamObjectBuilder[T]) WithVar(key string, value any) *StreamObjectBuilder[T] {
	b.vars[key] = value

	return b
}

// WithSystemPrompt sets the system prompt.
func (b *StreamObjectBuilder[T]) WithSystemPrompt(prompt string) *StreamObjectBuilder[T] {
	b.systemPrompt = prompt

	return b
}

// WithMessages sets conversation history.
func (b *StreamObjectBuilder[T]) WithMessages(messages []llm.ChatMessage) *StreamObjectBuilder[T] {
	b.messages = messages

	return b
}

// WithTemperature sets the temperature parameter.
func (b *StreamObjectBuilder[T]) WithTemperature(temp float64) *StreamObjectBuilder[T] {
	b.temperature = &temp

	return b
}

// WithMaxTokens sets the maximum tokens to generate.
func (b *StreamObjectBuilder[T]) WithMaxTokens(tokens int) *StreamObjectBuilder[T] {
	b.maxTokens = &tokens

	return b
}

// WithTopP sets the top-p sampling parameter.
func (b *StreamObjectBuilder[T]) WithTopP(topP float64) *StreamObjectBuilder[T] {
	b.topP = &topP

	return b
}

// WithTopK sets the top-k sampling parameter.
func (b *StreamObjectBuilder[T]) WithTopK(topK int) *StreamObjectBuilder[T] {
	b.topK = &topK

	return b
}

// WithStop sets stop sequences.
func (b *StreamObjectBuilder[T]) WithStop(sequences ...string) *StreamObjectBuilder[T] {
	b.stop = sequences

	return b
}

// WithSchema sets a custom JSON schema (overrides auto-generation).
func (b *StreamObjectBuilder[T]) WithSchema(schema map[string]any) *StreamObjectBuilder[T] {
	b.schema = schema

	return b
}

// WithSchemaStrict enables/disables strict schema validation.
func (b *StreamObjectBuilder[T]) WithSchemaStrict(strict bool) *StreamObjectBuilder[T] {
	b.schemaStrict = strict

	return b
}

// WithTimeout sets the execution timeout.
func (b *StreamObjectBuilder[T]) WithTimeout(timeout time.Duration) *StreamObjectBuilder[T] {
	b.timeout = timeout

	return b
}

// OnStart registers a callback to run before streaming starts.
func (b *StreamObjectBuilder[T]) OnStart(fn func()) *StreamObjectBuilder[T] {
	b.onStart = fn

	return b
}

// OnPartialObject registers a callback for partial object updates.
// Called whenever new fields become available in the streaming response.
func (b *StreamObjectBuilder[T]) OnPartialObject(fn func(partial T, completedPaths [][]string)) *StreamObjectBuilder[T] {
	b.onPartialObject = fn

	return b
}

// OnFieldComplete registers a callback for when individual fields complete.
// Called with the field path and value when a field finishes streaming.
func (b *StreamObjectBuilder[T]) OnFieldComplete(fn func(path []string, value any)) *StreamObjectBuilder[T] {
	b.onFieldComplete = fn

	return b
}

// OnDelta registers a callback for raw token deltas.
func (b *StreamObjectBuilder[T]) OnDelta(fn func(delta string)) *StreamObjectBuilder[T] {
	b.onDelta = fn

	return b
}

// OnComplete registers a callback for when streaming completes.
func (b *StreamObjectBuilder[T]) OnComplete(fn func(result *StreamObjectResult[T])) *StreamObjectBuilder[T] {
	b.onComplete = fn

	return b
}

// OnError registers a callback for errors.
func (b *StreamObjectBuilder[T]) OnError(fn func(error)) *StreamObjectBuilder[T] {
	b.onError = fn

	return b
}

// WithValidator adds a custom validation function for the final output.
func (b *StreamObjectBuilder[T]) WithValidator(validator func(T) error) *StreamObjectBuilder[T] {
	b.validators = append(b.validators, validator)

	return b
}

// Stream executes the streaming generation and returns the final result.
func (b *StreamObjectBuilder[T]) Stream() (*StreamObjectResult[T], error) {
	startTime := time.Now()

	// Call onStart callback
	if b.onStart != nil {
		b.onStart()
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(b.ctx, b.timeout)
	defer cancel()

	// Generate JSON schema if not provided
	schema := b.schema
	if schema == nil {
		var err error

		schema, err = b.generateSchema()
		if err != nil {
			b.handleError(err)

			return nil, fmt.Errorf("schema generation failed: %w", err)
		}
	}

	// Render prompt with variables
	renderedPrompt, err := b.renderPrompt()
	if err != nil {
		b.handleError(err)

		return nil, fmt.Errorf("prompt rendering failed: %w", err)
	}

	// Build messages
	messages := b.buildMessages(renderedPrompt, schema)

	// Log execution
	if b.logger != nil {
		var zero T
		b.logger.Debug("Starting streaming object generation",
			F("provider", b.provider),
			F("model", b.model),
			F("schema_type", reflect.TypeOf(zero).Name()),
		)
	}

	// Build streaming request
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

	// Create partial object parser
	parser := NewStreamObjectParser[T]()
	if b.onFieldComplete != nil {
		parser.OnFieldComplete(b.onFieldComplete)
	}

	// Track all received fields
	var (
		allFieldPaths [][]string
		usage         *Usage
		streamErr     error
	)

	// Start streaming with handler
	err = b.llmManager.ChatStream(ctx, request, func(event llm.ChatStreamEvent) error {
		if event.Error != "" {
			streamErr = fmt.Errorf("%s", event.Error)

			return streamErr
		}

		// Extract content delta
		var delta string

		if len(event.Choices) > 0 {
			if event.Choices[0].Delta != nil {
				delta = event.Choices[0].Delta.Content
			} else if event.Choices[0].Message.Content != "" {
				delta = event.Choices[0].Message.Content
			}
		}

		if delta == "" {
			// Check for usage in final event
			if event.Usage != nil {
				usage = &Usage{
					Provider:     b.provider,
					Model:        b.model,
					InputTokens:  int(event.Usage.InputTokens),
					OutputTokens: int(event.Usage.OutputTokens),
					TotalTokens:  int(event.Usage.TotalTokens),
				}
			}

			return nil
		}

		// Call raw delta callback
		if b.onDelta != nil {
			b.onDelta(delta)
		}

		// Parse partial object
		state, _ := parser.Append(delta)

		// Call partial object callback if we have new fields
		if state != nil && b.onPartialObject != nil {
			b.onPartialObject(state.Value, state.CompletedPaths)
			allFieldPaths = state.CompletedPaths
		}

		return nil
	})
	if err != nil {
		b.handleError(err)

		return nil, fmt.Errorf("failed to stream: %w", err)
	}

	if streamErr != nil {
		b.handleError(streamErr)

		return nil, fmt.Errorf("stream error: %w", streamErr)
	}

	// Get final state
	finalState := parser.GetCurrentState()
	if finalState == nil {
		err := errors.New("no valid JSON received in stream")
		b.handleError(err)

		return nil, err
	}

	// Run validators on final object
	for i, validator := range b.validators {
		if err := validator(finalState.Value); err != nil {
			validationErr := fmt.Errorf("validation %d failed: %w", i, err)
			b.handleError(validationErr)

			return nil, validationErr
		}
	}

	// Build result
	result := &StreamObjectResult[T]{
		Object:         finalState.Value,
		RawJSON:        finalState.RawJSON,
		Usage:          usage,
		Duration:       time.Since(startTime),
		FieldsReceived: allFieldPaths,
		Model:          b.model,
		Provider:       b.provider,
	}

	// Log completion
	if b.logger != nil {
		b.logger.Info("Streaming object generation completed",
			F("duration", result.Duration),
			F("fields", len(result.FieldsReceived)),
		)
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.stream_object.success").Inc()
		b.metrics.Histogram("forge.ai.sdk.stream_object.duration").Observe(result.Duration.Seconds())
	}

	// Call complete callback
	if b.onComplete != nil {
		b.onComplete(result)
	}

	return result, nil
}

// handleError calls error callback and logs.
func (b *StreamObjectBuilder[T]) handleError(err error) {
	if b.onError != nil {
		b.onError(err)
	}

	if b.logger != nil {
		b.logger.Error("Stream object error", F("error", err.Error()))
	}

	if b.metrics != nil {
		b.metrics.Counter("forge.ai.sdk.stream_object.errors").Inc()
	}
}

// renderPrompt renders the prompt template with variables.
func (b *StreamObjectBuilder[T]) renderPrompt() (string, error) {
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
func (b *StreamObjectBuilder[T]) buildMessages(prompt string, schema map[string]any) []llm.ChatMessage {
	messages := make([]llm.ChatMessage, 0)

	// Add custom messages first
	if len(b.messages) > 0 {
		messages = append(messages, b.messages...)
	}

	// Add system prompt with schema instructions
	systemPrompt := b.systemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant that returns structured data in JSON format."
	}

	// Enhance system prompt with schema
	schemaJSON, _ := json.MarshalIndent(schema, "", "  ")
	systemPrompt += fmt.Sprintf(`

You must return a valid JSON object that matches this schema:
%s

IMPORTANT: Return ONLY the JSON object, no markdown code blocks, no explanations.
Start your response with { and end with }.`, string(schemaJSON))

	messages = append(messages, llm.ChatMessage{
		Role:    "system",
		Content: systemPrompt,
	})

	// Add user prompt
	if prompt != "" {
		messages = append(messages, llm.ChatMessage{
			Role:    "user",
			Content: prompt,
		})
	}

	return messages
}

// generateSchema generates a JSON schema from the Go type T.
func (b *StreamObjectBuilder[T]) generateSchema() (map[string]any, error) {
	var zero T

	t := reflect.TypeOf(zero)

	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Only support structs
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("type %s is not a struct", t.Name())
	}

	return generateSchemaFromType(t), nil
}

// generateSchemaFromType generates a JSON schema from a reflect.Type.
func generateSchemaFromType(t reflect.Type) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": make(map[string]any),
		"required":   make([]string, 0),
	}

	properties := schema["properties"].(map[string]any)
	required := make([]string, 0)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" {
			jsonName = field.Name
		}

		description := field.Tag.Get("description")
		propSchema := generatePropertySchemaFromType(field.Type, description)
		properties[jsonName] = propSchema

		if !strings.Contains(jsonTag, "omitempty") {
			required = append(required, jsonName)
		}
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// generatePropertySchemaFromType generates a schema for a single property.
func generatePropertySchemaFromType(t reflect.Type, description string) map[string]any {
	schema := make(map[string]any)

	if description != "" {
		schema["description"] = description
	}

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		schema["type"] = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema["type"] = "integer"
	case reflect.Float32, reflect.Float64:
		schema["type"] = "number"
	case reflect.Bool:
		schema["type"] = "boolean"
	case reflect.Slice, reflect.Array:
		schema["type"] = "array"
		schema["items"] = generatePropertySchemaFromType(t.Elem(), "")
	case reflect.Map:
		schema["type"] = "object"
		schema["additionalProperties"] = generatePropertySchemaFromType(t.Elem(), "")
	case reflect.Struct:
		nested := generateSchemaFromType(t)
		maps.Copy(schema, nested)
	default:
		schema["type"] = "string"
	}

	return schema
}

// StreamObjectChannel returns a channel that emits partial objects as they stream.
// This is an alternative to callbacks for channel-based consumption.
func (b *StreamObjectBuilder[T]) StreamObjectChannel() (<-chan *PartialObjectState[T], <-chan error) {
	objectChan := make(chan *PartialObjectState[T], 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(objectChan)
		defer close(errChan)

		b.OnPartialObject(func(partial T, paths [][]string) {
			state := &PartialObjectState[T]{
				Value:          partial,
				CompletedPaths: paths,
			}
			select {
			case objectChan <- state:
			default:
				// Channel full, skip
			}
		})

		_, err := b.Stream()
		if err != nil {
			errChan <- err
		}
	}()

	return objectChan, errChan
}
