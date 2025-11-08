package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/sdk/testhelpers"
)

// Test structs.
type Person struct {
	Name string `description:"Full name of the person" json:"name"`
	Age  int    `description:"Age in years"            json:"age"`
}

type Address struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	ZipCode string `json:"zip_code,omitempty"`
}

type NestedStruct struct {
	Person  Person  `json:"person"`
	Address Address `json:"address"`
}

type ComplexTypes struct {
	StringSlice []string          `json:"string_slice"`
	IntSlice    []int             `json:"int_slice"`
	FloatValue  float64           `json:"float_value"`
	BoolValue   bool              `json:"bool_value"`
	MapValue    map[string]string `json:"map_value"`
	PointerStr  *string           `json:"pointer_str,omitempty"`
}

func TestNewGenerateObjectBuilder(t *testing.T) {
	llmManager := &testhelpers.MockLLMManager{}
	logger := &testhelpers.MockLogger{}
	metrics := testhelpers.NewMockMetrics()

	builder := NewGenerateObjectBuilder[Person](context.Background(), llmManager, logger, metrics)

	if builder == nil {
		t.Fatal("expected builder to be created")
	}

	if builder.llmManager == nil {
		t.Error("expected llm manager to be set")
	}

	if builder.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", builder.timeout)
	}

	if builder.retries != 3 {
		t.Errorf("expected default retries 3, got %d", builder.retries)
	}

	if !builder.schemaStrict {
		t.Error("expected schema strict to be true by default")
	}
}

func TestGenerateObjectBuilder_WithProvider(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithProvider("openai")

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", builder.provider)
	}
}

func TestGenerateObjectBuilder_WithModel(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithModel("gpt-4")

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", builder.model)
	}
}

func TestGenerateObjectBuilder_WithPrompt(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	prompt := "Extract person info"
	result := builder.WithPrompt(prompt)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.prompt != prompt {
		t.Error("expected prompt to be set")
	}
}

func TestGenerateObjectBuilder_WithVars(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	vars := map[string]interface{}{
		"name": "Alice",
		"age":  30,
	}
	result := builder.WithVars(vars)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.vars["name"] != "Alice" {
		t.Error("expected vars to be set")
	}
}

func TestGenerateObjectBuilder_WithVar(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithVar("key", "value")

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.vars["key"] != "value" {
		t.Error("expected var to be set")
	}
}

func TestGenerateObjectBuilder_WithSystemPrompt(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	sysPrompt := "You are a data extractor"
	result := builder.WithSystemPrompt(sysPrompt)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.systemPrompt != sysPrompt {
		t.Error("expected system prompt to be set")
	}
}

func TestGenerateObjectBuilder_WithMessages(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	messages := []llm.ChatMessage{
		{Role: "user", Content: "Hello"},
	}
	result := builder.WithMessages(messages)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if len(builder.messages) != 1 {
		t.Error("expected messages to be set")
	}
}

func TestGenerateObjectBuilder_WithTemperature(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	temp := 0.7
	result := builder.WithTemperature(temp)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.temperature == nil || *builder.temperature != temp {
		t.Error("expected temperature to be set")
	}
}

func TestGenerateObjectBuilder_WithMaxTokens(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	tokens := 500
	result := builder.WithMaxTokens(tokens)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.maxTokens == nil || *builder.maxTokens != tokens {
		t.Error("expected max tokens to be set")
	}
}

func TestGenerateObjectBuilder_WithTopP(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	topP := 0.9
	result := builder.WithTopP(topP)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.topP == nil || *builder.topP != topP {
		t.Error("expected top-p to be set")
	}
}

func TestGenerateObjectBuilder_WithTopK(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	topK := 40
	result := builder.WithTopK(topK)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.topK == nil || *builder.topK != topK {
		t.Error("expected top-k to be set")
	}
}

func TestGenerateObjectBuilder_WithStop(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	stop := []string{"END", "STOP"}
	result := builder.WithStop(stop...)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if len(builder.stop) != 2 {
		t.Error("expected stop sequences to be set")
	}
}

func TestGenerateObjectBuilder_WithSchema(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	schema := map[string]interface{}{
		"type": "object",
	}
	result := builder.WithSchema(schema)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.schema == nil {
		t.Error("expected schema to be set")
	}
}

func TestGenerateObjectBuilder_WithSchemaStrict(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithSchemaStrict(false)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.schemaStrict {
		t.Error("expected schema strict to be false")
	}
}

func TestGenerateObjectBuilder_WithFallbackOnFail(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithFallbackOnFail(true)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if !builder.fallbackOnFail {
		t.Error("expected fallback on fail to be true")
	}
}

func TestGenerateObjectBuilder_WithTimeout(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	timeout := 60 * time.Second
	result := builder.WithTimeout(timeout)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.timeout != timeout {
		t.Error("expected timeout to be set")
	}
}

func TestGenerateObjectBuilder_WithRetries(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithRetries(5, 2*time.Second)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.retries != 5 {
		t.Errorf("expected retries 5, got %d", builder.retries)
	}

	if builder.retryDelay != 2*time.Second {
		t.Errorf("expected retry delay 2s, got %v", builder.retryDelay)
	}
}

func TestGenerateObjectBuilder_OnStart(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func() {}
	result := builder.OnStart(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onStart == nil {
		t.Error("expected onStart to be set")
	}
}

func TestGenerateObjectBuilder_OnComplete(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func(p Person) {}
	result := builder.OnComplete(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onComplete == nil {
		t.Error("expected onComplete to be set")
	}
}

func TestGenerateObjectBuilder_OnError(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func(err error) {}
	result := builder.OnError(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onError == nil {
		t.Error("expected onError to be set")
	}
}

func TestGenerateObjectBuilder_WithValidator(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	validator := func(p Person) error {
		if p.Age < 0 {
			return errors.New("age cannot be negative")
		}

		return nil
	}
	result := builder.WithValidator(validator)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if len(builder.validators) != 1 {
		t.Error("expected validator to be added")
	}
}

func TestGenerateObjectBuilder_Execute_Success(t *testing.T) {
	person := Person{Name: "Alice", Age: 30}
	personJSON, _ := json.Marshal(person)

	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				ID:    "test-123",
				Model: "gpt-4",
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: string(personJSON),
						},
						FinishReason: "stop",
					},
				},
				Usage: &llm.LLMUsage{
					InputTokens:  50,
					OutputTokens: 20,
					TotalTokens:  70,
				},
			}, nil
		},
	}

	logger := &testhelpers.MockLogger{}
	metrics := testhelpers.NewMockMetrics()

	startCalled := false

	var completedPerson Person

	errorCalled := false

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, logger, metrics)

	result, err := builder.
		WithProvider("openai").
		WithModel("gpt-4").
		WithPrompt("Extract person info").
		OnStart(func() { startCalled = true }).
		OnComplete(func(p Person) { completedPerson = p }).
		OnError(func(e error) { errorCalled = true }).
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Name != "Alice" {
		t.Errorf("expected name 'Alice', got '%s'", result.Name)
	}

	if result.Age != 30 {
		t.Errorf("expected age 30, got %d", result.Age)
	}

	if !startCalled {
		t.Error("expected onStart callback to be called")
	}

	if completedPerson.Name != "Alice" {
		t.Error("expected onComplete callback to be called with result")
	}

	if errorCalled {
		t.Error("expected onError not to be called")
	}

	if len(logger.DebugCalls) == 0 {
		t.Error("expected logger to be called")
	}
}

func TestGenerateObjectBuilder_Execute_WithTemplateVars(t *testing.T) {
	person := Person{Name: "Bob", Age: 25}
	personJSON, _ := json.Marshal(person)

	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			// Verify template was rendered
			if len(request.Messages) > 0 {
				lastMsg := request.Messages[len(request.Messages)-1]
				if lastMsg.Content != "Extract: Bob is 25 years old" {
					t.Errorf("template not rendered correctly: %s", lastMsg.Content)
				}
			}

			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: string(personJSON),
						},
					},
				},
			}, nil
		},
	}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("Extract: {{.name}} is {{.age}} years old").
		WithVar("name", "Bob").
		WithVar("age", 25).
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Name != "Bob" {
		t.Errorf("expected name 'Bob', got '%s'", result.Name)
	}
}

func TestGenerateObjectBuilder_Execute_WithValidator(t *testing.T) {
	person := Person{Name: "Charlie", Age: -5} // Invalid age
	personJSON, _ := json.Marshal(person)

	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: string(personJSON),
						},
					},
				},
			}, nil
		},
	}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("Extract person").
		WithRetries(0, 0). // No retries for faster test
		WithValidator(func(p Person) error {
			if p.Age < 0 {
				return errors.New("age cannot be negative")
			}

			return nil
		}).
		Execute()
	if err == nil {
		t.Error("expected validation error")
	} else {
		// Got expected error - either timeout or validation failure
		t.Logf("Got expected error: %v", err)
	}
}

func TestGenerateObjectBuilder_Execute_LLMError(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{}, errors.New("API error")
		},
	}

	errorCalled := false

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("Extract person").
		WithRetries(0, 0). // No retries
		OnError(func(e error) { errorCalled = true }).
		Execute()
	if err == nil {
		t.Error("expected error")
	}

	if !errorCalled {
		t.Error("expected onError callback to be called")
	}
}

func TestGenerateObjectBuilder_Execute_JSONParseError(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "invalid json {{{",
						},
					},
				},
			}, nil
		},
	}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("Extract person").
		WithRetries(0, 0). // No retries
		Execute()
	if err == nil {
		t.Error("expected JSON parse error")
	}
}

func TestGenerateObjectBuilder_Execute_FallbackOnFail(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "invalid json",
						},
					},
				},
			}, nil
		},
	}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("Extract person").
		WithRetries(0, 0).
		WithFallbackOnFail(true).
		Execute()
	if err != nil {
		t.Errorf("expected no error with fallback, got %v", err)
	}

	// Should return zero value
	if result.Name != "" || result.Age != 0 {
		t.Error("expected zero value with fallback")
	}
}

func TestGenerateObjectBuilder_Execute_NoChoices(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{}, // No choices
			}, nil
		},
	}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("Extract person").
		WithRetries(0, 0).
		Execute()
	if err == nil {
		t.Error("expected error when no choices")
	}
}

func TestGenerateObjectBuilder_GenerateSchema_SimpleStruct(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), nil, nil, nil)

	schema, err := builder.generateSchema()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if schema["type"] != "object" {
		t.Error("expected type to be object")
	}

	props := schema["properties"].(map[string]interface{})

	if props["name"] == nil {
		t.Error("expected name property")
	}

	nameSchema := props["name"].(map[string]interface{})
	if nameSchema["type"] != "string" {
		t.Error("expected name type to be string")
	}

	if nameSchema["description"] != "Full name of the person" {
		t.Error("expected name description")
	}

	if props["age"] == nil {
		t.Error("expected age property")
	}

	ageSchema := props["age"].(map[string]interface{})
	if ageSchema["type"] != "integer" {
		t.Error("expected age type to be integer")
	}

	required := schema["required"].([]string)
	if len(required) != 2 {
		t.Errorf("expected 2 required fields, got %d", len(required))
	}
}

func TestGenerateObjectBuilder_GenerateSchema_NestedStruct(t *testing.T) {
	builder := NewGenerateObjectBuilder[NestedStruct](context.Background(), nil, nil, nil)

	schema, err := builder.generateSchema()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	props := schema["properties"].(map[string]interface{})

	if props["person"] == nil {
		t.Error("expected person property")
	}

	personSchema := props["person"].(map[string]interface{})
	if personSchema["type"] != "object" {
		t.Error("expected person type to be object")
	}

	personProps := personSchema["properties"].(map[string]interface{})
	if personProps["name"] == nil {
		t.Error("expected nested name property")
	}
}

func TestGenerateObjectBuilder_GenerateSchema_ComplexTypes(t *testing.T) {
	builder := NewGenerateObjectBuilder[ComplexTypes](context.Background(), nil, nil, nil)

	schema, err := builder.generateSchema()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	props := schema["properties"].(map[string]interface{})

	// Check array type
	if props["string_slice"] == nil {
		t.Error("expected string_slice property")
	}

	sliceSchema := props["string_slice"].(map[string]interface{})
	if sliceSchema["type"] != "array" {
		t.Error("expected string_slice type to be array")
	}

	// Check number type
	if props["float_value"] == nil {
		t.Error("expected float_value property")
	}

	floatSchema := props["float_value"].(map[string]interface{})
	if floatSchema["type"] != "number" {
		t.Error("expected float_value type to be number")
	}

	// Check bool type
	if props["bool_value"] == nil {
		t.Error("expected bool_value property")
	}

	boolSchema := props["bool_value"].(map[string]interface{})
	if boolSchema["type"] != "boolean" {
		t.Error("expected bool_value type to be boolean")
	}

	// Check map type
	if props["map_value"] == nil {
		t.Error("expected map_value property")
	}

	mapSchema := props["map_value"].(map[string]interface{})
	if mapSchema["type"] != "object" {
		t.Error("expected map_value type to be object")
	}
}

func TestGenerateObjectBuilder_GenerateSchema_WithOmitempty(t *testing.T) {
	builder := NewGenerateObjectBuilder[Address](context.Background(), nil, nil, nil)

	schema, err := builder.generateSchema()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	required := schema["required"].([]string)

	// zip_code has omitempty, so it should not be in required
	hasZipCode := false

	for _, field := range required {
		if field == "zip_code" {
			hasZipCode = true

			break
		}
	}

	if hasZipCode {
		t.Error("expected zip_code not to be required (has omitempty)")
	}

	// street and city should be required
	hasStreet := false
	hasCity := false

	for _, field := range required {
		if field == "street" {
			hasStreet = true
		}

		if field == "city" {
			hasCity = true
		}
	}

	if !hasStreet || !hasCity {
		t.Error("expected street and city to be required")
	}
}

func TestGenerateObjectBuilder_Execute_WithCustomSchema(t *testing.T) {
	customSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"custom_name": map[string]interface{}{
				"type": "string",
			},
		},
	}

	person := Person{Name: "Custom", Age: 99}
	personJSON, _ := json.Marshal(person)

	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			// Verify custom schema is in system prompt
			if len(request.Messages) > 0 {
				systemMsg := request.Messages[0]
				if !containsString(systemMsg.Content, "custom_name") {
					t.Error("expected custom schema in system prompt")
				}
			}

			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: string(personJSON),
						},
					},
				},
			}, nil
		},
	}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("Extract").
		WithSchema(customSchema).
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGenerateObjectBuilder_Execute_WithRetries(t *testing.T) {
	attempts := 0

	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			attempts++
			if attempts < 3 {
				return llm.ChatResponse{}, errors.New("temporary error")
			}

			person := Person{Name: "Success", Age: 42}
			personJSON, _ := json.Marshal(person)

			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: string(personJSON),
						},
					},
				},
			}, nil
		},
	}

	logger := &testhelpers.MockLogger{}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, logger, nil)

	result, err := builder.
		WithPrompt("Extract").
		WithRetries(5, 10*time.Millisecond).
		Execute()
	if err != nil {
		t.Fatalf("expected no error after retries, got %v", err)
	}

	if result.Name != "Success" {
		t.Error("expected successful result after retries")
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestGenerateObjectBuilder_BuildMessages(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), nil, nil, nil)
	builder.systemPrompt = "Custom system"

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
		},
	}

	messages := builder.buildMessages("User prompt", schema)

	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(messages))
	}

	// First message should be system
	if messages[0].Role != "system" {
		t.Error("expected first message to be system")
	}

	if !containsString(messages[0].Content, "Custom system") {
		t.Error("expected custom system prompt in message")
	}

	if !containsString(messages[0].Content, "schema") {
		t.Error("expected schema in system message")
	}

	// Last message should be user
	lastMsg := messages[len(messages)-1]
	if lastMsg.Role != "user" {
		t.Error("expected last message to be user")
	}

	if lastMsg.Content != "User prompt" {
		t.Error("expected user prompt in last message")
	}
}

func TestGenerateObjectBuilder_BuildMessages_WithCustomMessages(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), nil, nil, nil)
	builder.messages = []llm.ChatMessage{
		{Role: "user", Content: "Previous message"},
	}

	schema := map[string]interface{}{"type": "object"}

	messages := builder.buildMessages("New prompt", schema)

	// Should have: custom messages + system + user prompt
	if len(messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(messages))
	}

	if messages[0].Role != "user" || messages[0].Content != "Previous message" {
		t.Error("expected custom messages first")
	}
}

func TestGenerateObjectBuilder_GenerateSchema_NonStructType(t *testing.T) {
	builder := NewGenerateObjectBuilder[string](context.Background(), nil, nil, nil)

	_, err := builder.generateSchema()
	if err == nil {
		t.Error("expected error for non-struct type")
	}
}

func TestGenerateObjectBuilder_GenerateSchema_PointerType(t *testing.T) {
	type TestStruct struct {
		Field string `json:"field"`
	}

	builder := NewGenerateObjectBuilder[*TestStruct](context.Background(), nil, nil, nil)

	schema, err := builder.generateSchema()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if schema["type"] != "object" {
		t.Error("expected type to be object")
	}
}

func TestGenerateObjectBuilder_Execute_SchemaGenerationError(t *testing.T) {
	builder := NewGenerateObjectBuilder[string](context.Background(), nil, nil, nil)

	_, err := builder.WithPrompt("test").Execute()
	if err == nil {
		t.Error("expected schema generation error")
	}
}

func TestGenerateObjectBuilder_GeneratePropertySchema_AllTypes(t *testing.T) {
	type AllTypes struct {
		String  string         `json:"string"`
		Int8    int8           `json:"int8"`
		Int16   int16          `json:"int16"`
		Int32   int32          `json:"int32"`
		Int64   int64          `json:"int64"`
		Uint    uint           `json:"uint"`
		Uint8   uint8          `json:"uint8"`
		Uint16  uint16         `json:"uint16"`
		Uint32  uint32         `json:"uint32"`
		Uint64  uint64         `json:"uint64"`
		Float32 float32        `json:"float32"`
		Float64 float64        `json:"float64"`
		Bool    bool           `json:"bool"`
		Slice   []string       `json:"slice"`
		Map     map[string]int `json:"map"`
	}

	builder := NewGenerateObjectBuilder[AllTypes](context.Background(), nil, nil, nil)

	schema, err := builder.generateSchema()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	props := schema["properties"].(map[string]interface{})

	// Check all types are handled correctly
	if props["string"].(map[string]interface{})["type"] != "string" {
		t.Error("expected string type")
	}

	if props["int8"].(map[string]interface{})["type"] != "integer" {
		t.Error("expected integer type for int8")
	}

	if props["uint"].(map[string]interface{})["type"] != "integer" {
		t.Error("expected integer type for uint")
	}

	if props["float32"].(map[string]interface{})["type"] != "number" {
		t.Error("expected number type for float32")
	}

	if props["bool"].(map[string]interface{})["type"] != "boolean" {
		t.Error("expected boolean type")
	}

	if props["slice"].(map[string]interface{})["type"] != "array" {
		t.Error("expected array type for slice")
	}

	if props["map"].(map[string]interface{})["type"] != "object" {
		t.Error("expected object type for map")
	}
}

func TestGenerateObjectBuilder_Execute_NilLogger(t *testing.T) {
	person := Person{Name: "Test", Age: 30}
	personJSON, _ := json.Marshal(person)

	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: string(personJSON),
						},
					},
				},
			}, nil
		},
	}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("Extract person").
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Name != "Test" {
		t.Error("expected result to be parsed correctly")
	}
}

func TestGenerateObjectBuilder_Execute_NilMetrics(t *testing.T) {
	person := Person{Name: "Test", Age: 30}
	personJSON, _ := json.Marshal(person)

	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: string(personJSON),
						},
					},
				},
				Usage: &llm.LLMUsage{
					InputTokens:  10,
					OutputTokens: 5,
					TotalTokens:  15,
				},
			}, nil
		},
	}

	builder := NewGenerateObjectBuilder[Person](context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("Extract person").
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Name != "Test" {
		t.Error("expected result to be parsed correctly")
	}
}

func TestGenerateObjectBuilder_BuildMessages_EmptyPrompt(t *testing.T) {
	builder := NewGenerateObjectBuilder[Person](context.Background(), nil, nil, nil)
	builder.systemPrompt = "System"

	schema := map[string]interface{}{"type": "object"}

	messages := builder.buildMessages("", schema)

	// Should have only system message when prompt is empty
	if len(messages) != 1 {
		t.Fatalf("expected 1 message (system only), got %d", len(messages))
	}

	if messages[0].Role != "system" {
		t.Error("expected only system message")
	}
}

// Helper function.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
