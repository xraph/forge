package sdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
	"github.com/xraph/forge/extensions/ai/sdk/testhelpers"
)

func TestNewStreamBuilder(t *testing.T) {
	llmManager := &testhelpers.MockLLMManager{}
	logger := &testhelpers.MockLogger{}
	metrics := testhelpers.NewMockMetrics()

	builder := NewStreamBuilder(context.Background(), llmManager, logger, metrics)

	if builder == nil {
		t.Fatal("expected builder to be created")
	}

	if builder.llmManager == nil {
		t.Error("expected llm manager to be set")
	}

	if builder.timeout != 60*time.Second {
		t.Errorf("expected default timeout 60s, got %v", builder.timeout)
	}

	if builder.bufferSize != 100 {
		t.Errorf("expected default buffer size 100, got %d", builder.bufferSize)
	}
}

func TestStreamBuilder_WithProvider(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithProvider("openai")

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.provider != "openai" {
		t.Errorf("expected provider 'openai', got '%s'", builder.provider)
	}
}

func TestStreamBuilder_WithModel(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithModel("gpt-4")

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", builder.model)
	}
}

func TestStreamBuilder_WithPrompt(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	prompt := "Explain quantum computing"
	result := builder.WithPrompt(prompt)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.prompt != prompt {
		t.Error("expected prompt to be set")
	}
}

func TestStreamBuilder_WithVars(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	vars := map[string]interface{}{
		"topic": "AI",
		"level": "beginner",
	}
	result := builder.WithVars(vars)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.vars["topic"] != "AI" {
		t.Error("expected vars to be set")
	}
}

func TestStreamBuilder_WithVar(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithVar("key", "value")

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.vars["key"] != "value" {
		t.Error("expected var to be set")
	}
}

func TestStreamBuilder_WithSystemPrompt(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	sysPrompt := "You are a helpful AI assistant"
	result := builder.WithSystemPrompt(sysPrompt)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.systemPrompt != sysPrompt {
		t.Error("expected system prompt to be set")
	}
}

func TestStreamBuilder_WithMessages(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	messages := []llm.ChatMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}
	result := builder.WithMessages(messages)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if len(builder.messages) != 2 {
		t.Error("expected messages to be set")
	}
}

func TestStreamBuilder_WithTemperature(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	temp := 0.8
	result := builder.WithTemperature(temp)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.temperature == nil || *builder.temperature != temp {
		t.Error("expected temperature to be set")
	}
}

func TestStreamBuilder_WithMaxTokens(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	tokens := 2000
	result := builder.WithMaxTokens(tokens)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.maxTokens == nil || *builder.maxTokens != tokens {
		t.Error("expected max tokens to be set")
	}
}

func TestStreamBuilder_WithTopP(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	topP := 0.95
	result := builder.WithTopP(topP)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.topP == nil || *builder.topP != topP {
		t.Error("expected top-p to be set")
	}
}

func TestStreamBuilder_WithTopK(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	topK := 50
	result := builder.WithTopK(topK)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.topK == nil || *builder.topK != topK {
		t.Error("expected top-k to be set")
	}
}

func TestStreamBuilder_WithStop(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	stop := []string{"END", "STOP"}
	result := builder.WithStop(stop...)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if len(builder.stop) != 2 {
		t.Error("expected stop sequences to be set")
	}
}

func TestStreamBuilder_WithTools(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	tool := llm.Tool{
		Type: "function",
		Function: &llm.FunctionDefinition{
			Name:        "get_weather",
			Description: "Get weather information",
		},
	}
	result := builder.WithTools(tool)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if len(builder.tools) != 1 {
		t.Error("expected tools to be set")
	}
}

func TestStreamBuilder_WithToolChoice(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	choice := "auto"
	result := builder.WithToolChoice(choice)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.toolChoice != choice {
		t.Error("expected tool choice to be set")
	}
}

func TestStreamBuilder_WithReasoning(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	result := builder.WithReasoning(true)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if !builder.includeReasoning {
		t.Error("expected reasoning to be enabled")
	}
}

func TestStreamBuilder_WithBufferSize(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	size := 200
	result := builder.WithBufferSize(size)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.bufferSize != size {
		t.Errorf("expected buffer size %d, got %d", size, builder.bufferSize)
	}
}

func TestStreamBuilder_WithTimeout(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	timeout := 90 * time.Second
	result := builder.WithTimeout(timeout)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.timeout != timeout {
		t.Error("expected timeout to be set")
	}
}

func TestStreamBuilder_OnStart(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func() {}
	result := builder.OnStart(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onStart == nil {
		t.Error("expected onStart to be set")
	}
}

func TestStreamBuilder_OnToken(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func(token string) {}
	result := builder.OnToken(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onToken == nil {
		t.Error("expected onToken to be set")
	}
}

func TestStreamBuilder_OnReasoning(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func(reasoning string) {}
	result := builder.OnReasoning(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onReasoning == nil {
		t.Error("expected onReasoning to be set")
	}
}

func TestStreamBuilder_OnToolCall(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func(toolName string, args map[string]interface{}) {}
	result := builder.OnToolCall(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onToolCall == nil {
		t.Error("expected onToolCall to be set")
	}
}

func TestStreamBuilder_OnComplete(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func(result StreamResult) {}
	result := builder.OnComplete(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onComplete == nil {
		t.Error("expected onComplete to be set")
	}
}

func TestStreamBuilder_OnError(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), &testhelpers.MockLLMManager{}, nil, nil)
	fn := func(err error) {}
	result := builder.OnError(fn)

	if result != builder {
		t.Error("expected fluent interface")
	}

	if builder.onError == nil {
		t.Error("expected onError to be set")
	}
}

func TestStreamBuilder_Stream_Success(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			if !request.Stream {
				t.Error("expected stream mode to be enabled")
			}

			return llm.ChatResponse{
				ID:    "test-stream",
				Model: "gpt-4",
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "This is a streaming response",
						},
						FinishReason: "stop",
					},
				},
				Usage: &llm.LLMUsage{
					InputTokens:  20,
					OutputTokens: 10,
					TotalTokens:  30,
				},
			}, nil
		},
	}

	logger := &testhelpers.MockLogger{}
	metrics := testhelpers.NewMockMetrics()

	startCalled := false
	var tokens []string
	var completedResult StreamResult
	errorCalled := false

	builder := NewStreamBuilder(context.Background(), mockLLM, logger, metrics)

	result, err := builder.
		WithProvider("openai").
		WithModel("gpt-4").
		WithPrompt("Explain AI").
		OnStart(func() { startCalled = true }).
		OnToken(func(token string) { tokens = append(tokens, token) }).
		OnComplete(func(r StreamResult) { completedResult = r }).
		OnError(func(e error) { errorCalled = true }).
		Stream()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be returned")
	}

	if result.Content != "This is a streaming response" {
		t.Errorf("unexpected content: %s", result.Content)
	}

	if !startCalled {
		t.Error("expected onStart callback to be called")
	}

	if len(tokens) == 0 {
		t.Error("expected onToken callback to be called")
	}

	if completedResult.Content == "" {
		t.Error("expected onComplete callback to be called with result")
	}

	if errorCalled {
		t.Error("expected onError not to be called")
	}

	if len(logger.DebugCalls) == 0 {
		t.Error("expected logger to be called")
	}
}

func TestStreamBuilder_Stream_WithTemplateVars(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			// Verify template was rendered
			if len(request.Messages) > 0 {
				lastMsg := request.Messages[len(request.Messages)-1]
				if lastMsg.Content != "Explain quantum computing at beginner level" {
					t.Errorf("template not rendered correctly: %s", lastMsg.Content)
				}
			}

			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "Quantum computing explanation",
						},
					},
				},
			}, nil
		},
	}

	builder := NewStreamBuilder(context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("Explain {{.topic}} at {{.level}} level").
		WithVar("topic", "quantum computing").
		WithVar("level", "beginner").
		Stream()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Content != "Quantum computing explanation" {
		t.Error("expected content to match")
	}
}

func TestStreamBuilder_Stream_WithSystemPrompt(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			if len(request.Messages) < 2 {
				t.Error("expected system and user messages")
			}

			if request.Messages[0].Role != "system" {
				t.Error("expected first message to be system")
			}

			if request.Messages[0].Content != "You are an expert" {
				t.Error("expected system prompt")
			}

			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "Expert response",
						},
					},
				},
			}, nil
		},
	}

	builder := NewStreamBuilder(context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithSystemPrompt("You are an expert").
		WithPrompt("Help me").
		Stream()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestStreamBuilder_Stream_LLMError(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{}, errors.New("API error")
		},
	}

	errorCalled := false

	builder := NewStreamBuilder(context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("Test").
		OnError(func(e error) { errorCalled = true }).
		Stream()

	if err == nil {
		t.Error("expected error")
	}

	if !errorCalled {
		t.Error("expected onError callback to be called")
	}
}

func TestStreamBuilder_Stream_NoChoices(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{}, // No choices
			}, nil
		},
	}

	builder := NewStreamBuilder(context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("Test").
		Stream()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Content != "" {
		t.Error("expected empty content when no choices")
	}
}

func TestStreamBuilder_RenderPrompt_NoVars(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), nil, nil, nil)
	builder.prompt = "Simple prompt"

	rendered, err := builder.renderPrompt()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if rendered != "Simple prompt" {
		t.Error("expected prompt to remain unchanged")
	}
}

func TestStreamBuilder_RenderPrompt_WithVars(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), nil, nil, nil)
	builder.prompt = "Hello {{.name}}, you are {{.age}} years old"
	builder.vars = map[string]interface{}{
		"name": "Alice",
		"age":  30,
	}

	rendered, err := builder.renderPrompt()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if rendered != "Hello Alice, you are 30 years old" {
		t.Errorf("unexpected rendered prompt: %s", rendered)
	}
}

func TestStreamBuilder_BuildMessages_OnlyPrompt(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), nil, nil, nil)

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

func TestStreamBuilder_BuildMessages_WithSystemAndUser(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), nil, nil, nil)
	builder.systemPrompt = "You are helpful"

	messages := builder.buildMessages("Help me")

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if messages[0].Role != "system" {
		t.Error("expected first message to be system")
	}

	if messages[1].Role != "user" {
		t.Error("expected second message to be user")
	}
}

func TestStreamBuilder_BuildMessages_WithCustomMessages(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), nil, nil, nil)
	builder.messages = []llm.ChatMessage{
		{Role: "user", Content: "Previous message"},
		{Role: "assistant", Content: "Previous response"},
	}
	builder.systemPrompt = "System"

	messages := builder.buildMessages("New prompt")

	// Should have: custom messages + system + user prompt
	if len(messages) < 4 {
		t.Fatalf("expected at least 4 messages, got %d", len(messages))
	}

	if messages[0].Role != "user" || messages[0].Content != "Previous message" {
		t.Error("expected custom messages first")
	}
}

func TestStreamBuilder_BuildMessages_EmptyPrompt(t *testing.T) {
	builder := NewStreamBuilder(context.Background(), nil, nil, nil)
	builder.systemPrompt = "System only"

	messages := builder.buildMessages("")

	// Should have only system message when prompt is empty
	if len(messages) != 1 {
		t.Fatalf("expected 1 message (system only), got %d", len(messages))
	}

	if messages[0].Role != "system" {
		t.Error("expected only system message")
	}
}

func TestStreamBuilder_Stream_WithAllParameters(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			// Verify all parameters were set
			if request.Temperature == nil || *request.Temperature != 0.7 {
				t.Error("expected temperature to be set")
			}
			if request.MaxTokens == nil || *request.MaxTokens != 1000 {
				t.Error("expected max tokens to be set")
			}
			if request.TopP == nil || *request.TopP != 0.9 {
				t.Error("expected top-p to be set")
			}
			if request.TopK == nil || *request.TopK != 40 {
				t.Error("expected top-k to be set")
			}
			if len(request.Stop) != 1 || request.Stop[0] != "DONE" {
				t.Error("expected stop sequences to be set")
			}
			if len(request.Tools) != 1 {
				t.Error("expected tools to be set")
			}
			if request.ToolChoice != "auto" {
				t.Error("expected tool choice to be set")
			}

			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "Response",
						},
					},
				},
			}, nil
		},
	}

	tool := llm.Tool{
		Type: "function",
		Function: &llm.FunctionDefinition{
			Name: "test_func",
		},
	}

	builder := NewStreamBuilder(context.Background(), mockLLM, nil, nil)

	_, err := builder.
		WithPrompt("Test").
		WithTemperature(0.7).
		WithMaxTokens(1000).
		WithTopP(0.9).
		WithTopK(40).
		WithStop("DONE").
		WithTools(tool).
		WithToolChoice("auto").
		Stream()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestStreamBuilder_Stream_NilLogger(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "Response",
						},
					},
				},
			}, nil
		},
	}

	builder := NewStreamBuilder(context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("Test").
		Stream()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Content != "Response" {
		t.Error("expected result to be parsed correctly")
	}
}

func TestStreamBuilder_Stream_NilMetrics(t *testing.T) {
	mockLLM := &testhelpers.MockLLMManager{
		ChatFunc: func(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{
						Message: llm.ChatMessage{
							Role:    "assistant",
							Content: "Response",
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

	builder := NewStreamBuilder(context.Background(), mockLLM, nil, nil)

	result, err := builder.
		WithPrompt("Test").
		Stream()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	totalTokens := result.Usage.InputTokens + result.Usage.OutputTokens
	if totalTokens != 15 {
		t.Error("expected usage to be parsed correctly")
	}
}

