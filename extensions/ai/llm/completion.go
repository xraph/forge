package llm

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// CompletionRequest represents a text completion request.
type CompletionRequest struct {
	Provider         string         `json:"provider"`
	Model            string         `json:"model"`
	Prompt           string         `json:"prompt"`
	Temperature      *float64       `json:"temperature,omitempty"`
	MaxTokens        *int           `json:"max_tokens,omitempty"`
	TopP             *float64       `json:"top_p,omitempty"`
	TopK             *int           `json:"top_k,omitempty"`
	Stop             []string       `json:"stop,omitempty"`
	Stream           bool           `json:"stream"`
	N                *int           `json:"n,omitempty"`
	LogProbs         *int           `json:"logprobs,omitempty"`
	Echo             bool           `json:"echo"`
	PresencePenalty  *float64       `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64       `json:"frequency_penalty,omitempty"`
	BestOf           *int           `json:"best_of,omitempty"`
	Suffix           string         `json:"suffix,omitempty"`
	Context          map[string]any `json:"context"`
	Metadata         map[string]any `json:"metadata"`
	RequestID        string         `json:"request_id"`
}

// CompletionResponse represents a text completion response.
type CompletionResponse struct {
	ID        string             `json:"id"`
	Object    string             `json:"object"`
	Created   int64              `json:"created"`
	Model     string             `json:"model"`
	Provider  string             `json:"provider"`
	Choices   []CompletionChoice `json:"choices"`
	Usage     *LLMUsage          `json:"usage,omitempty"`
	Metadata  map[string]any     `json:"metadata"`
	RequestID string             `json:"request_id"`
}

// CompletionChoice represents a completion choice.
type CompletionChoice struct {
	Index        int        `json:"index"`
	Text         string     `json:"text"`
	LogProbs     *LogProbs  `json:"logprobs,omitempty"`
	FinishReason string     `json:"finish_reason"`
	Delta        *TextDelta `json:"delta,omitempty"`
}

// TextDelta represents a text delta for streaming.
type TextDelta struct {
	Text string `json:"text"`
}

// CompletionStreamEvent represents a streaming completion event.
type CompletionStreamEvent struct {
	Type      string             `json:"type"` // text, error, done
	ID        string             `json:"id"`
	Object    string             `json:"object"`
	Created   int64              `json:"created"`
	Model     string             `json:"model"`
	Provider  string             `json:"provider"`
	Choices   []CompletionChoice `json:"choices"`
	Usage     *LLMUsage          `json:"usage,omitempty"`
	Error     string             `json:"error,omitempty"`
	Metadata  map[string]any     `json:"metadata,omitempty"`
	RequestID string             `json:"request_id"`
}

// CompletionStreamHandler handles streaming completion events.
type CompletionStreamHandler func(event CompletionStreamEvent) error

// CompletionTemplate represents a completion template.
type CompletionTemplate struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Template    string              `json:"template"`
	Variables   []TemplateVariable  `json:"variables"`
	Settings    CompletionSettings  `json:"settings"`
	Examples    []CompletionExample `json:"examples"`
	Metadata    map[string]any      `json:"metadata"`
}

// TemplateVariable represents a template variable.
type TemplateVariable struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"` // string, number, boolean, array, object
	Description string         `json:"description"`
	Required    bool           `json:"required"`
	Default     any            `json:"default,omitempty"`
	Validation  string         `json:"validation,omitempty"`
	Examples    []string       `json:"examples,omitempty"`
	Constraints map[string]any `json:"constraints,omitempty"`
}

// CompletionSettings represents settings for completion.
type CompletionSettings struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	TopK             *int     `json:"top_k,omitempty"`
	Stop             []string `json:"stop,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	BestOf           *int     `json:"best_of,omitempty"`
	LogProbs         *int     `json:"logprobs,omitempty"`
	Echo             bool     `json:"echo"`
}

// CompletionExample represents an example for the template.
type CompletionExample struct {
	Input       map[string]any `json:"input"`
	Output      string         `json:"output"`
	Description string         `json:"description"`
}

// CompletionBuilder provides a fluent interface for building completion requests.
type CompletionBuilder struct {
	request CompletionRequest
}

// NewCompletionBuilder creates a new completion builder.
func NewCompletionBuilder() *CompletionBuilder {
	return &CompletionBuilder{
		request: CompletionRequest{
			Context:  make(map[string]any),
			Metadata: make(map[string]any),
		},
	}
}

// WithProvider sets the provider.
func (b *CompletionBuilder) WithProvider(provider string) *CompletionBuilder {
	b.request.Provider = provider

	return b
}

// WithModel sets the model.
func (b *CompletionBuilder) WithModel(model string) *CompletionBuilder {
	b.request.Model = model

	return b
}

// WithPrompt sets the prompt.
func (b *CompletionBuilder) WithPrompt(prompt string) *CompletionBuilder {
	b.request.Prompt = prompt

	return b
}

// WithTemperature sets the temperature.
func (b *CompletionBuilder) WithTemperature(temperature float64) *CompletionBuilder {
	b.request.Temperature = &temperature

	return b
}

// WithMaxTokens sets the maximum tokens.
func (b *CompletionBuilder) WithMaxTokens(maxTokens int) *CompletionBuilder {
	b.request.MaxTokens = &maxTokens

	return b
}

// WithTopP sets the top-p parameter.
func (b *CompletionBuilder) WithTopP(topP float64) *CompletionBuilder {
	b.request.TopP = &topP

	return b
}

// WithTopK sets the top-k parameter.
func (b *CompletionBuilder) WithTopK(topK int) *CompletionBuilder {
	b.request.TopK = &topK

	return b
}

// WithStop sets the stop sequences.
func (b *CompletionBuilder) WithStop(stop ...string) *CompletionBuilder {
	b.request.Stop = stop

	return b
}

// WithStream enables or disables streaming.
func (b *CompletionBuilder) WithStream(stream bool) *CompletionBuilder {
	b.request.Stream = stream

	return b
}

// WithN sets the number of completions to generate.
func (b *CompletionBuilder) WithN(n int) *CompletionBuilder {
	b.request.N = &n

	return b
}

// WithLogProbs sets the log probabilities.
func (b *CompletionBuilder) WithLogProbs(logProbs int) *CompletionBuilder {
	b.request.LogProbs = &logProbs

	return b
}

// WithEcho enables or disables echo.
func (b *CompletionBuilder) WithEcho(echo bool) *CompletionBuilder {
	b.request.Echo = echo

	return b
}

// WithPresencePenalty sets the presence penalty.
func (b *CompletionBuilder) WithPresencePenalty(penalty float64) *CompletionBuilder {
	b.request.PresencePenalty = &penalty

	return b
}

// WithFrequencyPenalty sets the frequency penalty.
func (b *CompletionBuilder) WithFrequencyPenalty(penalty float64) *CompletionBuilder {
	b.request.FrequencyPenalty = &penalty

	return b
}

// WithBestOf sets the best of parameter.
func (b *CompletionBuilder) WithBestOf(bestOf int) *CompletionBuilder {
	b.request.BestOf = &bestOf

	return b
}

// WithSuffix sets the suffix.
func (b *CompletionBuilder) WithSuffix(suffix string) *CompletionBuilder {
	b.request.Suffix = suffix

	return b
}

// WithContext adds context data.
func (b *CompletionBuilder) WithContext(key string, value any) *CompletionBuilder {
	b.request.Context[key] = value

	return b
}

// WithMetadata adds metadata.
func (b *CompletionBuilder) WithMetadata(key string, value any) *CompletionBuilder {
	b.request.Metadata[key] = value

	return b
}

// WithRequestID sets the request ID.
func (b *CompletionBuilder) WithRequestID(id string) *CompletionBuilder {
	b.request.RequestID = id

	return b
}

// Build returns the built completion request.
func (b *CompletionBuilder) Build() CompletionRequest {
	return b.request
}

// Execute executes the completion request using the provided LLM manager.
func (b *CompletionBuilder) Execute(ctx context.Context, manager *LLMManager) (CompletionResponse, error) {
	return manager.Complete(ctx, b.request)
}

// NewCompletionTemplate creates a new completion template.
func NewCompletionTemplate(name, description, template string) *CompletionTemplate {
	return &CompletionTemplate{
		Name:        name,
		Description: description,
		Template:    template,
		Variables:   make([]TemplateVariable, 0),
		Settings:    CompletionSettings{},
		Examples:    make([]CompletionExample, 0),
		Metadata:    make(map[string]any),
	}
}

// AddVariable adds a variable to the template.
func (t *CompletionTemplate) AddVariable(variable TemplateVariable) {
	t.Variables = append(t.Variables, variable)
}

// AddExample adds an example to the template.
func (t *CompletionTemplate) AddExample(example CompletionExample) {
	t.Examples = append(t.Examples, example)
}

// Render renders the template with the provided variables.
func (t *CompletionTemplate) Render(variables map[string]any) (string, error) {
	result := t.Template

	// Simple template rendering - replace {{variable}} with values
	for _, variable := range t.Variables {
		placeholder := "{{" + variable.Name + "}}"

		if value, exists := variables[variable.Name]; exists {
			result = strings.ReplaceAll(result, placeholder, toString(value))
		} else if variable.Required {
			return "", fmt.Errorf("required variable '%s' not provided", variable.Name)
		} else if variable.Default != nil {
			result = strings.ReplaceAll(result, placeholder, toString(variable.Default))
		}
	}

	return result, nil
}

// CreateCompletionRequest creates a completion request from the template.
func (t *CompletionTemplate) CreateCompletionRequest(variables map[string]any) (CompletionRequest, error) {
	prompt, err := t.Render(variables)
	if err != nil {
		return CompletionRequest{}, err
	}

	return CompletionRequest{
		Prompt:           prompt,
		Temperature:      t.Settings.Temperature,
		MaxTokens:        t.Settings.MaxTokens,
		TopP:             t.Settings.TopP,
		TopK:             t.Settings.TopK,
		Stop:             t.Settings.Stop,
		PresencePenalty:  t.Settings.PresencePenalty,
		FrequencyPenalty: t.Settings.FrequencyPenalty,
		BestOf:           t.Settings.BestOf,
		LogProbs:         t.Settings.LogProbs,
		Echo:             t.Settings.Echo,
		Context:          variables,
		Metadata:         t.Metadata,
	}, nil
}

// ValidateVariables validates the provided variables against the template.
func (t *CompletionTemplate) ValidateVariables(variables map[string]any) error {
	for _, variable := range t.Variables {
		value, exists := variables[variable.Name]

		if !exists {
			if variable.Required {
				return fmt.Errorf("required variable '%s' not provided", variable.Name)
			}

			continue
		}

		// Type validation
		if err := validateVariableType(variable, value); err != nil {
			return fmt.Errorf("variable '%s': %w", variable.Name, err)
		}

		// Constraint validation
		if err := validateVariableConstraints(variable, value); err != nil {
			return fmt.Errorf("variable '%s': %w", variable.Name, err)
		}
	}

	return nil
}

// validateVariableType validates the type of a variable.
func validateVariableType(variable TemplateVariable, value any) error {
	switch variable.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "number":
		switch value.(type) {
		case int, int32, int64, float32, float64:
			// Valid number types
		default:
			return fmt.Errorf("expected number, got %T", value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("expected array, got %T", value)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
	}

	return nil
}

// validateVariableConstraints validates the constraints of a variable.
func validateVariableConstraints(variable TemplateVariable, value any) error {
	if variable.Constraints == nil {
		return nil
	}

	// Length constraints for strings
	if strValue, ok := value.(string); ok {
		if minLen, exists := variable.Constraints["min_length"]; exists {
			if min, ok := minLen.(int); ok && len(strValue) < min {
				return fmt.Errorf("string length %d is less than minimum %d", len(strValue), min)
			}
		}

		if maxLen, exists := variable.Constraints["max_length"]; exists {
			if max, ok := maxLen.(int); ok && len(strValue) > max {
				return fmt.Errorf("string length %d is greater than maximum %d", len(strValue), max)
			}
		}
	}

	// Range constraints for numbers
	if numValue, ok := value.(float64); ok {
		if minVal, exists := variable.Constraints["min"]; exists {
			if min, ok := minVal.(float64); ok && numValue < min {
				return fmt.Errorf("value %f is less than minimum %f", numValue, min)
			}
		}

		if maxVal, exists := variable.Constraints["max"]; exists {
			if max, ok := maxVal.(float64); ok && numValue > max {
				return fmt.Errorf("value %f is greater than maximum %f", numValue, max)
			}
		}
	}

	// Enum constraints
	if enumValues, exists := variable.Constraints["enum"]; exists {
		if enum, ok := enumValues.([]any); ok {
			found := slices.Contains(enum, value)

			if !found {
				return fmt.Errorf("value %v is not in allowed enum values %v", value, enum)
			}
		}
	}

	return nil
}

// toString converts an interface{} to string.
func toString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int32:
		return strconv.Itoa(int(v))
	case int64:
		return strconv.FormatInt(v, 10)
	case float32:
		return fmt.Sprintf("%f", v)
	case float64:
		return fmt.Sprintf("%f", v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// CreateSimpleTemplate creates a simple completion template.
func CreateSimpleTemplate(name, template string, variables ...string) *CompletionTemplate {
	tmpl := NewCompletionTemplate(name, "", template)

	for _, varName := range variables {
		tmpl.AddVariable(TemplateVariable{
			Name:     varName,
			Type:     "string",
			Required: true,
		})
	}

	return tmpl
}

// CreateCodeTemplate creates a template for code generation.
func CreateCodeTemplate(name, language, description string) *CompletionTemplate {
	template := fmt.Sprintf(`Generate %s code for the following task:

Task: {{task}}
Requirements: {{requirements}}
Style: {{style}}

Code:`, language)

	tmpl := NewCompletionTemplate(name, description, template)

	tmpl.AddVariable(TemplateVariable{
		Name:        "task",
		Type:        "string",
		Description: "The task to generate code for",
		Required:    true,
	})

	tmpl.AddVariable(TemplateVariable{
		Name:        "requirements",
		Type:        "string",
		Description: "Specific requirements for the code",
		Required:    false,
		Default:     "None",
	})

	tmpl.AddVariable(TemplateVariable{
		Name:        "style",
		Type:        "string",
		Description: "Code style preferences",
		Required:    false,
		Default:     "Clean and readable",
	})

	return tmpl
}

// CreateSummaryTemplate creates a template for text summarization.
func CreateSummaryTemplate(name string) *CompletionTemplate {
	template := `Summarize the following text:

Text: {{text}}
Length: {{length}}
Style: {{style}}

Summary:`

	tmpl := NewCompletionTemplate(name, "Text summarization template", template)

	tmpl.AddVariable(TemplateVariable{
		Name:        "text",
		Type:        "string",
		Description: "The text to summarize",
		Required:    true,
	})

	tmpl.AddVariable(TemplateVariable{
		Name:        "length",
		Type:        "string",
		Description: "Desired summary length",
		Required:    false,
		Default:     "Brief",
		Constraints: map[string]any{
			"enum": []any{"Brief", "Medium", "Detailed"},
		},
	})

	tmpl.AddVariable(TemplateVariable{
		Name:        "style",
		Type:        "string",
		Description: "Summary style",
		Required:    false,
		Default:     "Neutral",
		Constraints: map[string]any{
			"enum": []any{"Neutral", "Formal", "Casual", "Technical"},
		},
	})

	return tmpl
}
