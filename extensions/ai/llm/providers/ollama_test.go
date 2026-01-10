package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

func TestNewOllamaProvider(t *testing.T) {
	tests := []struct {
		name    string
		config  OllamaConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: OllamaConfig{
				BaseURL: "http://localhost:11434",
				Timeout: 60 * time.Second,
			},
			wantErr: false,
		},
		{
			name:    "default values",
			config:  OllamaConfig{},
			wantErr: false,
		},
		{
			name: "with models",
			config: OllamaConfig{
				BaseURL: "http://localhost:11434",
				Models:  []string{"llama2", "mistral"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOllamaProvider(tt.config, nil, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewOllamaProvider() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr {
				if provider.Name() != "ollama" {
					t.Errorf("Name() = %v, want ollama", provider.Name())
				}
			}
		})
	}
}

func TestOllamaProvider_Name(t *testing.T) {
	provider, _ := NewOllamaProvider(OllamaConfig{}, nil, nil)
	if got := provider.Name(); got != "ollama" {
		t.Errorf("Name() = %v, want ollama", got)
	}
}

func TestOllamaProvider_Models(t *testing.T) {
	models := []string{"llama2", "mistral", "codellama"}
	provider, _ := NewOllamaProvider(OllamaConfig{Models: models}, nil, nil)

	got := provider.Models()
	if len(got) != len(models) {
		t.Errorf("Models() returned %d models, want %d", len(got), len(models))
	}
}

func TestOllamaProvider_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			// Return empty models list
			json.NewEncoder(w).Encode(ollamaModelsResponse{Models: []ollamaModelInfo{}})

			return
		case "/api/chat":
			// Verify request
			var req ollamaChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if req.Model != "llama2" {
				t.Errorf("unexpected model: %s", req.Model)
			}

			// Send response
			response := ollamaChatResponse{
				Model:     "llama2",
				CreatedAt: time.Now().Format(time.RFC3339),
				Message: ollamaMessage{
					Role:    "assistant",
					Content: "Hello! How can I help you today?",
				},
				Done:            true,
				PromptEvalCount: 10,
				EvalCount:       8,
			}

			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, _ := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
		Models:  []string{"llama2"}, // Pre-configure to avoid fetching
	}, nil, nil)

	request := llm.ChatRequest{
		Model: "llama2",
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
		RequestID: "test-123",
	}

	response, err := provider.Chat(context.Background(), request)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if response.Model != "llama2" {
		t.Errorf("Chat() model = %v, want llama2", response.Model)
	}

	if len(response.Choices) == 0 {
		t.Fatal("Chat() returned no choices")
	}

	if response.Choices[0].Message.Content != "Hello! How can I help you today?" {
		t.Errorf("Chat() content = %v, want 'Hello! How can I help you today?'", response.Choices[0].Message.Content)
	}

	if response.Usage == nil {
		t.Error("Chat() usage is nil")
	} else {
		if response.Usage.InputTokens != 10 {
			t.Errorf("Chat() input tokens = %d, want 10", response.Usage.InputTokens)
		}

		if response.Usage.OutputTokens != 8 {
			t.Errorf("Chat() output tokens = %d, want 8", response.Usage.OutputTokens)
		}
	}
}

func TestOllamaProvider_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(ollamaModelsResponse{Models: []ollamaModelInfo{}})

			return
		case "/api/generate":
			// Verify request
			var req ollamaGenerateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if req.Model != "llama2" {
				t.Errorf("unexpected model: %s", req.Model)
			}

			// Send response
			response := ollamaGenerateResponse{
				Model:           "llama2",
				CreatedAt:       time.Now().Format(time.RFC3339),
				Response:        "This is a test completion response.",
				Done:            true,
				PromptEvalCount: 5,
				EvalCount:       7,
			}

			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, _ := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
		Models:  []string{"llama2"},
	}, nil, nil)

	request := llm.CompletionRequest{
		Model:     "llama2",
		Prompt:    "Complete this: ",
		RequestID: "test-456",
	}

	response, err := provider.Complete(context.Background(), request)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if response.Model != "llama2" {
		t.Errorf("Complete() model = %v, want llama2", response.Model)
	}

	if len(response.Choices) == 0 {
		t.Fatal("Complete() returned no choices")
	}

	if response.Choices[0].Text != "This is a test completion response." {
		t.Errorf("Complete() text = %v, want 'This is a test completion response.'", response.Choices[0].Text)
	}
}

func TestOllamaProvider_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(ollamaModelsResponse{Models: []ollamaModelInfo{}})

			return
		case "/api/embed", "/api/embeddings":
			// Send response
			response := ollamaEmbedResponse{
				Model:     "nomic-embed-text",
				Embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
			}

			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, _ := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
		Models:  []string{"nomic-embed-text"},
	}, nil, nil)

	request := llm.EmbeddingRequest{
		Model:     "nomic-embed-text",
		Input:     []string{"test text"},
		RequestID: "test-789",
	}

	response, err := provider.Embed(context.Background(), request)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if response.Model != "nomic-embed-text" {
		t.Errorf("Embed() model = %v, want nomic-embed-text", response.Model)
	}

	if len(response.Data) == 0 {
		t.Fatal("Embed() returned no embeddings")
	}

	if len(response.Data[0].Embedding) != 5 {
		t.Errorf("Embed() embedding length = %d, want 5", len(response.Data[0].Embedding))
	}
}

func TestOllamaProvider_ChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(ollamaModelsResponse{Models: []ollamaModelInfo{}})

			return
		case "/api/chat":
			// Verify request
			var req ollamaChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if !req.Stream {
				t.Error("expected stream=true")
			}

			// Send streaming responses
			encoder := json.NewEncoder(w)

			// First chunk
			chunk1 := ollamaChatResponse{
				Model:     "llama2",
				CreatedAt: time.Now().Format(time.RFC3339),
				Message: ollamaMessage{
					Role:    "assistant",
					Content: "Hello",
				},
				Done: false,
			}
			if err := encoder.Encode(chunk1); err != nil {
				t.Fatalf("failed to encode chunk1: %v", err)
			}

			// Second chunk
			chunk2 := ollamaChatResponse{
				Model:     "llama2",
				CreatedAt: time.Now().Format(time.RFC3339),
				Message: ollamaMessage{
					Role:    "assistant",
					Content: " there!",
				},
				Done: false,
			}
			if err := encoder.Encode(chunk2); err != nil {
				t.Fatalf("failed to encode chunk2: %v", err)
			}

			// Final chunk
			final := ollamaChatResponse{
				Model:           "llama2",
				CreatedAt:       time.Now().Format(time.RFC3339),
				Message:         ollamaMessage{Role: "assistant"},
				Done:            true,
				PromptEvalCount: 5,
				EvalCount:       3,
				DoneReason:      "stop",
			}
			if err := encoder.Encode(final); err != nil {
				t.Fatalf("failed to encode final: %v", err)
			}
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, _ := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
		Models:  []string{"llama2"},
	}, nil, nil)

	request := llm.ChatRequest{
		Model: "llama2",
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
		RequestID: "test-stream",
	}

	var events []llm.ChatStreamEvent

	handler := func(event llm.ChatStreamEvent) error {
		events = append(events, event)

		return nil
	}

	err := provider.ChatStream(context.Background(), request, handler)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if len(events) < 3 {
		t.Errorf("ChatStream() received %d events, want at least 3", len(events))
	}

	// Check for done event
	var foundDone bool

	for _, event := range events {
		if event.Type == "done" {
			foundDone = true

			if event.Usage == nil {
				t.Error("done event has no usage")
			}
		}
	}

	if !foundDone {
		t.Error("ChatStream() did not receive done event")
	}
}

func TestOllamaProvider_HealthCheck(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "healthy",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "unhealthy",
			statusCode: http.StatusServiceUnavailable,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/tags":
					json.NewEncoder(w).Encode(ollamaModelsResponse{Models: []ollamaModelInfo{}})

					return
				case "/api/version":
					w.WriteHeader(tt.statusCode)

					if tt.statusCode == http.StatusOK {
						response := ollamaVersionResponse{Version: "0.1.0"}
						json.NewEncoder(w).Encode(response)
					}
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()

			provider, _ := NewOllamaProvider(OllamaConfig{
				BaseURL: server.URL,
				Timeout: 10 * time.Second,
				Models:  []string{"llama2"},
			}, nil, nil)

			err := provider.HealthCheck(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("HealthCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOllamaProvider_GetUsage(t *testing.T) {
	provider, _ := NewOllamaProvider(OllamaConfig{}, nil, nil)

	usage := provider.GetUsage()
	if usage.RequestCount != 0 {
		t.Errorf("GetUsage() request count = %d, want 0", usage.RequestCount)
	}

	if usage.TotalTokens != 0 {
		t.Errorf("GetUsage() total tokens = %d, want 0", usage.TotalTokens)
	}
}

func TestOllamaProvider_Stop(t *testing.T) {
	provider, _ := NewOllamaProvider(OllamaConfig{}, nil, nil)

	err := provider.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}
}

func TestOllamaProvider_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(ollamaModelsResponse{Models: []ollamaModelInfo{}})

			return
		case "/api/chat":
			// Verify request has tools
			var req ollamaChatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if len(req.Tools) == 0 {
				t.Error("expected tools in request")
			}

			if req.Tools[0].Function.Name != "get_weather" {
				t.Errorf("unexpected tool name: %s", req.Tools[0].Function.Name)
			}

			// Send response with tool call
			response := ollamaChatResponse{
				Model:     "llama2",
				CreatedAt: time.Now().Format(time.RFC3339),
				Message: ollamaMessage{
					Role: "assistant",
					ToolCalls: []ollamaToolCall{
						{
							Function: ollamaFunctionCall{
								Name: "get_weather",
								Arguments: map[string]any{
									"location": "San Francisco",
								},
							},
						},
					},
				},
				Done:            true,
				PromptEvalCount: 10,
				EvalCount:       5,
			}

			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, _ := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
		Models:  []string{"llama2"},
	}, nil, nil)

	temp := 0.7
	request := llm.ChatRequest{
		Model: "llama2",
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "What's the weather in San Francisco?",
			},
		},
		Tools: []llm.Tool{
			{
				Type: "function",
				Function: &llm.FunctionDefinition{
					Name:        "get_weather",
					Description: "Get the current weather",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{
								"type":        "string",
								"description": "The city name",
							},
						},
						"required": []string{"location"},
					},
				},
			},
		},
		Temperature: &temp,
		RequestID:   "test-tools",
	}

	response, err := provider.Chat(context.Background(), request)
	if err != nil {
		t.Fatalf("Chat() with tools error = %v", err)
	}

	if response.Model != "llama2" {
		t.Errorf("Chat() model = %v, want llama2", response.Model)
	}
}

func TestOllamaProvider_MultipleEmbeddings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send response with multiple embeddings
		response := ollamaEmbedResponse{
			Model: "nomic-embed-text",
			Embeddings: [][]float64{
				{0.1, 0.2, 0.3},
				{0.4, 0.5, 0.6},
				{0.7, 0.8, 0.9},
			},
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider, _ := NewOllamaProvider(OllamaConfig{
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	}, nil, nil)

	request := llm.EmbeddingRequest{
		Model:     "nomic-embed-text",
		Input:     []string{"text1", "text2", "text3"},
		RequestID: "test-multi-embed",
	}

	response, err := provider.Embed(context.Background(), request)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if len(response.Data) != 3 {
		t.Errorf("Embed() returned %d embeddings, want 3", len(response.Data))
	}

	for i, data := range response.Data {
		if data.Index != i {
			t.Errorf("Embedding %d has index %d", i, data.Index)
		}

		if len(data.Embedding) != 3 {
			t.Errorf("Embedding %d has length %d, want 3", i, len(data.Embedding))
		}
	}
}
