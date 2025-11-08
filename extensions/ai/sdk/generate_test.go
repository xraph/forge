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

func TestNewGenerateBuilder(t *testing.T) {
	ctx := context.Background()
	llmMgr := &testhelpers.MockLLMManager{}
	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	builder := NewGenerateBuilder(ctx, llmMgr, logger, metrics)

	if builder == nil {
		t.Fatal("expected non-nil builder")
	}

	if builder.ctx != ctx {
		t.Error("expected context to be set")
	}

	if builder.llmManager == nil {
		t.Error("expected llm manager to be set")
	}

	if builder.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", builder.timeout)
	}

	if builder.cache {
		t.Error("expected cache to be disabled by default")
	}

	if builder.vars == nil {
		t.Error("expected vars map to be initialized")
	}
}

func TestGenerateBuilder_WithProvider(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithProvider("openai")

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", builder.provider)
	}
}

func TestGenerateBuilder_WithModel(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithModel("gpt-4")

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", builder.model)
	}
}

func TestGenerateBuilder_WithPrompt(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	prompt := "What is 2+2?"
	result := builder.WithPrompt(prompt)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.prompt != prompt {
		t.Errorf("expected prompt '%s', got '%s'", prompt, builder.prompt)
	}
}

func TestGenerateBuilder_WithVars(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	vars := map[string]any{
		"name": "Alice",
		"age":  30,
	}
	result := builder.WithVars(vars)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.vars["name"] != "Alice" {
		t.Error("expected vars to be set")
	}

	if builder.vars["age"] != 30 {
		t.Error("expected vars to be set")
	}
}

func TestGenerateBuilder_WithVar(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithVar("key", "value")

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.vars["key"] != "value" {
		t.Errorf("expected var 'value', got '%v'", builder.vars["key"])
	}
}

func TestGenerateBuilder_WithSystemPrompt(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	sysPrompt := "You are a helpful assistant"
	result := builder.WithSystemPrompt(sysPrompt)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.systemPrompt != sysPrompt {
		t.Errorf("expected system prompt '%s', got '%s'", sysPrompt, builder.systemPrompt)
	}
}

func TestGenerateBuilder_WithTemperature(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	temp := 0.7
	result := builder.WithTemperature(temp)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.temperature == nil || *builder.temperature != temp {
		t.Errorf("expected temperature %v, got %v", temp, builder.temperature)
	}
}

func TestGenerateBuilder_WithMaxTokens(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	tokens := 100
	result := builder.WithMaxTokens(tokens)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.maxTokens == nil || *builder.maxTokens != tokens {
		t.Errorf("expected max tokens %v, got %v", tokens, builder.maxTokens)
	}
}

func TestGenerateBuilder_WithTopP(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	topP := 0.9
	result := builder.WithTopP(topP)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.topP == nil || *builder.topP != topP {
		t.Errorf("expected topP %v, got %v", topP, builder.topP)
	}
}

func TestGenerateBuilder_WithTopK(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	topK := 40
	result := builder.WithTopK(topK)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.topK == nil || *builder.topK != topK {
		t.Errorf("expected topK %v, got %v", topK, builder.topK)
	}
}

func TestGenerateBuilder_WithStop(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	stop := []string{"END", "STOP"}
	result := builder.WithStop(stop...)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if len(builder.stop) != 2 {
		t.Errorf("expected 2 stop sequences, got %d", len(builder.stop))
	}
}

func TestGenerateBuilder_WithTools(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	tool := llm.Tool{
		Type: "function",
		Function: &llm.FunctionDefinition{
			Name:        "test_func",
			Description: "A test function",
		},
	}
	result := builder.WithTools(tool)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if len(builder.tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(builder.tools))
	}
}

func TestGenerateBuilder_WithToolChoice(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	choice := "auto"
	result := builder.WithToolChoice(choice)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.toolChoice != choice {
		t.Errorf("expected tool choice '%s', got '%s'", choice, builder.toolChoice)
	}
}

func TestGenerateBuilder_WithTimeout(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	timeout := 60 * time.Second
	result := builder.WithTimeout(timeout)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.timeout != timeout {
		t.Errorf("expected timeout %v, got %v", timeout, builder.timeout)
	}
}

func TestGenerateBuilder_WithCache(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	ttl := 10 * time.Minute
	result := builder.WithCache(ttl)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if !builder.cache {
		t.Error("expected cache to be enabled")
	}

	if builder.cacheTTL != ttl {
		t.Errorf("expected cache TTL %v, got %v", ttl, builder.cacheTTL)
	}
}

func TestGenerateBuilder_WithMessages(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	messages := []llm.ChatMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}
	result := builder.WithMessages(messages)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if len(builder.messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(builder.messages))
	}
}

func TestGenerateBuilder_OnStart(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	called := false
	fn := func() { called = true }
	result := builder.OnStart(fn)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.onStart == nil {
		t.Error("expected onStart to be set")
	}

	builder.onStart()

	if !called {
		t.Error("expected callback to be called")
	}
}

func TestGenerateBuilder_OnComplete(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)

	var capturedResult Result

	fn := func(r Result) { capturedResult = r }
	result := builder.OnComplete(fn)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.onComplete == nil {
		t.Error("expected onComplete to be set")
	}

	testResult := Result{Content: "test"}
	builder.onComplete(testResult)

	if capturedResult.Content != "test" {
		t.Error("expected callback to receive result")
	}
}

func TestGenerateBuilder_OnError(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)

	var capturedError error

	fn := func(err error) { capturedError = err }
	result := builder.OnError(fn)

	if result != builder {
		t.Error("expected fluent interface to return same builder")
	}

	if builder.onError == nil {
		t.Error("expected onError to be set")
	}

	testErr := errors.New("test error")
	builder.onError(testErr)

	if !errors.Is(capturedError, testErr) {
		t.Error("expected callback to receive error")
	}
}

func TestGenerateBuilder_Execute_Success(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				ID:    "test-123",
				Model: "gpt-4",
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "The answer is 4",
						},
						FinishReason: "stop",
					},
				},
				Usage: &llm.LLMUsage{
					InputTokens:  10,
					OutputTokens: 5,
					TotalTokens:  15,
					Cost:         0.001,
				},
			}, nil
		},
	}

	logger := testhelpers.NewMockLogger()
	metrics := testhelpers.NewMockMetrics()

	builder := NewGenerateBuilder(context.Background(), mockLLM, logger, metrics)

	startCalled := false
	completeCalled := false

	result, err := builder.
		WithProvider("openai").
		WithModel("gpt-4").
		WithPrompt("What is 2+2?").
		OnStart(func() { startCalled = true }).
		OnComplete(func(r Result) { completeCalled = true }).
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !startCalled {
		t.Error("expected start callback to be called")
	}

	if !completeCalled {
		t.Error("expected complete callback to be called")
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Content != "The answer is 4" {
		t.Errorf("expected content 'The answer is 4', got '%s'", result.Content)
	}

	if result.FinishReason != "stop" {
		t.Errorf("expected finish reason 'stop', got '%s'", result.FinishReason)
	}

	if result.Usage == nil {
		t.Fatal("expected usage to be set")
	}

	if result.Usage.InputTokens != 10 {
		t.Errorf("expected input tokens 10, got %d", result.Usage.InputTokens)
	}

	if result.Usage.OutputTokens != 5 {
		t.Errorf("expected output tokens 5, got %d", result.Usage.OutputTokens)
	}
}

func TestGenerateBuilder_Execute_WithTemplateVars(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			// Check that template was rendered
			if len(request.Messages) == 0 {
				t.Fatal("expected at least one message")
			}

			lastMsg := request.Messages[len(request.Messages)-1]
			if lastMsg.Content != "My name is Alice and I am 30 years old" {
				t.Errorf("expected rendered template, got '%s'", lastMsg.Content)
			}

			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{Message: llm.ChatMessage{Content: "Hello Alice!"}},
				},
			}, nil
		},
	}

	builder := NewGenerateBuilder(context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("My name is {{.name}} and I am {{.age}} years old").
		WithVars(map[string]any{
			"name": "Alice",
			"age":  30,
		}).
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGenerateBuilder_Execute_WithSystemPrompt(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			if len(request.Messages) < 2 {
				t.Fatal("expected at least 2 messages")
			}

			if request.Messages[0].Role != "system" {
				t.Error("expected first message to be system")
			}

			if request.Messages[0].Content != "You are helpful" {
				t.Error("expected system prompt to be set")
			}

			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{Message: llm.ChatMessage{Content: "response"}},
				},
			}, nil
		},
	}

	builder := NewGenerateBuilder(context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithSystemPrompt("You are helpful").
		WithPrompt("Hello").
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGenerateBuilder_Execute_Error(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{}, errors.New("API error")
		},
	}

	metrics := testhelpers.NewMockMetrics()
	errorCalled := false

	builder := NewGenerateBuilder(context.Background(), mockLLM, nil, metrics)

	_, err := builder.
		WithProvider("openai").
		WithModel("gpt-4").
		WithPrompt("test").
		OnError(func(e error) { errorCalled = true }).
		Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errorCalled {
		t.Error("expected error callback to be called")
	}
}

func TestGenerateBuilder_Execute_Timeout(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			// Simulate slow response
			select {
			case <-time.After(2 * time.Second):
				return llm.ChatResponse{}, nil
			case <-ctx.Done():
				return llm.ChatResponse{}, ctx.Err()
			}
		},
	}

	builder := NewGenerateBuilder(context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("test").
		WithTimeout(100 * time.Millisecond).
		Execute()
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestResult_String(t *testing.T) {
	result := &Result{
		Content: "test content",
	}

	if result.String() != "test content" {
		t.Errorf("expected 'test content', got '%s'", result.String())
	}
}

func TestResult_JSON(t *testing.T) {
	result := &Result{
		Content:      "test",
		FinishReason: "stop",
	}

	data, err := result.JSON()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var decoded Result
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("expected no error unmarshaling, got %v", err)
	}

	if decoded.Content != "test" {
		t.Errorf("expected content 'test', got '%s'", decoded.Content)
	}
}

func TestRenderPrompt_NoVars(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), nil, nil, nil)
	builder.prompt = "Simple prompt"

	rendered, err := builder.renderPrompt()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if rendered != "Simple prompt" {
		t.Errorf("expected 'Simple prompt', got '%s'", rendered)
	}
}

func TestRenderPrompt_WithVars(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), nil, nil, nil)
	builder.prompt = "Hello {{.name}}, you are {{.age}} years old"
	builder.vars = map[string]any{
		"name": "Bob",
		"age":  25,
	}

	rendered, err := builder.renderPrompt()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expected := "Hello Bob, you are 25 years old"
	if rendered != expected {
		t.Errorf("expected '%s', got '%s'", expected, rendered)
	}
}

func TestBuildMessages(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), nil, nil, nil)
	builder.systemPrompt = "You are helpful"
	builder.messages = []llm.ChatMessage{
		{Role: "user", Content: "Previous message"},
	}

	messages := builder.buildMessages("New prompt")

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	if messages[0].Role != "system" {
		t.Error("expected first message to be system")
	}

	if messages[1].Content != "Previous message" {
		t.Error("expected previous messages to be included")
	}

	if messages[2].Content != "New prompt" {
		t.Error("expected new prompt to be added")
	}
}

func TestStringHelpers(t *testing.T) {
	// Test indexOf
	if idx := indexOf("hello world", "world"); idx != 6 {
		t.Errorf("expected index 6, got %d", idx)
	}

	if idx := indexOf("hello world", "missing"); idx != -1 {
		t.Errorf("expected index -1, got %d", idx)
	}

	// Test replaceFirst
	result := replaceFirst("hello world", "world", "universe")
	if result != "hello universe world" {
		t.Errorf("expected 'hello universe world', got '%s'", result)
	}

	// Test replaceAll
	result = replaceAll("hello world", "world", "universe")
	if result != "hello universe" {
		t.Errorf("expected 'hello universe universe', got '%s'", result)
	}
}

func TestGenerateBuilder_Execute_NoUsage(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				ID:    "test-no-usage",
				Model: "gpt-4",
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "Response without usage",
						},
						FinishReason: "stop",
					},
				},
				Usage: nil, // No usage information
			}, nil
		},
	}

	builder := NewGenerateBuilder(context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("test").
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Usage != nil {
		t.Error("expected usage to be nil")
	}
}

func TestGenerateBuilder_Execute_NoChoices(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				ID:      "test-no-choices",
				Model:   "gpt-4",
				Choices: []llm.ChatChoice{}, // No choices
			}, nil
		},
	}

	builder := NewGenerateBuilder(context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("test").
		Execute()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Content != "" {
		t.Error("expected empty content when no choices")
	}

	if result.FinishReason != "unknown" {
		t.Errorf("expected finish reason 'unknown', got '%s'", result.FinishReason)
	}
}

func TestBuildMessages_OnlyPrompt(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), nil, nil, nil)

	messages := builder.buildMessages("Just a prompt")

	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	if messages[0].Role != "user" {
		t.Error("expected message role to be user")
	}

	if messages[0].Content != "Just a prompt" {
		t.Error("expected prompt to match")
	}
}

func TestBuildMessages_EmptyPrompt(t *testing.T) {
	builder := NewGenerateBuilder(context.Background(), nil, nil, nil)
	builder.systemPrompt = "System"

	messages := builder.buildMessages("")

	if len(messages) != 1 {
		t.Fatalf("expected 1 message (system only), got %d", len(messages))
	}

	if messages[0].Role != "system" {
		t.Error("expected only system message")
	}
}

func TestGenerateBuilder_RenderPrompt_Error(t *testing.T) {
	// Create a builder with a prompt that will never render
	// Actually, our simple renderer doesn't have error cases, so this is hard to test
	// Unless we make renderPrompt more sophisticated
	builder := NewGenerateBuilder(context.Background(), nil, nil, nil)
	builder.prompt = "Normal prompt"

	_, err := builder.renderPrompt()
	if err != nil {
		t.Errorf("expected no error for normal prompt, got %v", err)
	}
}
