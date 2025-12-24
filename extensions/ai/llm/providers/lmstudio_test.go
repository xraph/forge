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

// TestNewLMStudioProvider tests provider creation.
func TestNewLMStudioProvider(t *testing.T) {
	tests := []struct {
		name    string
		config  LMStudioConfig
		wantErr bool
	}{
		{
			name: "default config",
			config: LMStudioConfig{
				Models: []string{"test-model"},
			},
			wantErr: false,
		},
		{
			name: "custom base URL",
			config: LMStudioConfig{
				BaseURL: "http://custom:8080/v1",
				Models:  []string{"model1", "model2"},
			},
			wantErr: false,
		},
		{
			name: "with API key",
			config: LMStudioConfig{
				APIKey: "test-key",
				Models: []string{"test-model"},
			},
			wantErr: false,
		},
		{
			name: "custom timeout",
			config: LMStudioConfig{
				Timeout: 120 * time.Second,
				Models:  []string{"test-model"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewLMStudioProvider(tt.config, nil, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLMStudioProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if provider == nil {
					t.Error("NewLMStudioProvider() returned nil provider")
				}
				if provider.Name() != "lmstudio" {
					t.Errorf("Name() = %v, want lmstudio", provider.Name())
				}
			}
		})
	}
}

// TestLMStudioProvider_Name tests the Name method.
func TestLMStudioProvider_Name(t *testing.T) {
	provider, err := NewLMStudioProvider(LMStudioConfig{
		Models: []string{"test-model"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	if got := provider.Name(); got != "lmstudio" {
		t.Errorf("Name() = %v, want lmstudio", got)
	}
}

// TestLMStudioProvider_Models tests the Models method.
func TestLMStudioProvider_Models(t *testing.T) {
	expectedModels := []string{"model1", "model2", "model3"}
	provider, err := NewLMStudioProvider(LMStudioConfig{
		Models: expectedModels,
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	models := provider.Models()
	if len(models) != len(expectedModels) {
		t.Errorf("Models() returned %d models, want %d", len(models), len(expectedModels))
	}

	for i, model := range models {
		if model != expectedModels[i] {
			t.Errorf("Models()[%d] = %v, want %v", i, model, expectedModels[i])
		}
	}
}

// TestLMStudioProvider_Chat tests the Chat method.
func TestLMStudioProvider_Chat(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Parse request
		var req lmstudioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Verify request
		if req.Model != "test-model" {
			t.Errorf("unexpected model: %s", req.Model)
		}

		// Send response
		response := lmstudioResponse{
			ID:      "test-id",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []lmstudioChoice{
				{
					Index: 0,
					Message: &lmstudioMessage{
						Role:    "assistant",
						Content: "Test response",
					},
					FinishReason: "stop",
				},
			},
			Usage: &lmstudioUsage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider
	provider, err := NewLMStudioProvider(LMStudioConfig{
		BaseURL: server.URL + "/v1",
		Models:  []string{"test-model"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	// Test chat
	ctx := context.Background()
	request := llm.ChatRequest{
		Model: "test-model",
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
		RequestID: "test-request",
	}

	response, err := provider.Chat(ctx, request)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if response.ID != "test-id" {
		t.Errorf("Chat() ID = %v, want test-id", response.ID)
	}

	if len(response.Choices) != 1 {
		t.Errorf("Chat() returned %d choices, want 1", len(response.Choices))
	}

	if response.Choices[0].Message.Content != "Test response" {
		t.Errorf("Chat() message content = %v, want Test response", response.Choices[0].Message.Content)
	}

	if response.Usage == nil {
		t.Error("Chat() usage is nil")
	} else {
		if response.Usage.TotalTokens != 30 {
			t.Errorf("Chat() total tokens = %v, want 30", response.Usage.TotalTokens)
		}
	}
}

// TestLMStudioProvider_Complete tests the Complete method.
func TestLMStudioProvider_Complete(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Parse request
		var req lmstudioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Send response
		response := lmstudioResponse{
			ID:      "completion-id",
			Object:  "text_completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []lmstudioChoice{
				{
					Index:        0,
					Text:         "Completed text",
					FinishReason: "stop",
				},
			},
			Usage: &lmstudioUsage{
				PromptTokens:     5,
				CompletionTokens: 15,
				TotalTokens:      20,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider
	provider, err := NewLMStudioProvider(LMStudioConfig{
		BaseURL: server.URL + "/v1",
		Models:  []string{"test-model"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	// Test completion
	ctx := context.Background()
	request := llm.CompletionRequest{
		Model:     "test-model",
		Prompt:    "Once upon a time",
		RequestID: "test-request",
	}

	response, err := provider.Complete(ctx, request)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if response.ID != "completion-id" {
		t.Errorf("Complete() ID = %v, want completion-id", response.ID)
	}

	if len(response.Choices) != 1 {
		t.Errorf("Complete() returned %d choices, want 1", len(response.Choices))
	}

	if response.Choices[0].Text != "Completed text" {
		t.Errorf("Complete() text = %v, want Completed text", response.Choices[0].Text)
	}
}

// TestLMStudioProvider_Embed tests the Embed method.
func TestLMStudioProvider_Embed(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Parse request
		var req lmstudioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Send response
		response := lmstudioResponse{
			Object: "list",
			Model:  req.Model,
			Data: []lmstudioEmbedding{
				{
					Object:    "embedding",
					Index:     0,
					Embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
				},
			},
			Usage: &lmstudioUsage{
				PromptTokens: 8,
				TotalTokens:  8,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider
	provider, err := NewLMStudioProvider(LMStudioConfig{
		BaseURL: server.URL + "/v1",
		Models:  []string{"embedding-model"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	// Test embedding
	ctx := context.Background()
	request := llm.EmbeddingRequest{
		Model:     "embedding-model",
		Input:     "Test text",
		RequestID: "test-request",
	}

	response, err := provider.Embed(ctx, request)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if len(response.Data) != 1 {
		t.Errorf("Embed() returned %d embeddings, want 1", len(response.Data))
	}

	if len(response.Data[0].Embedding) != 5 {
		t.Errorf("Embed() embedding length = %d, want 5", len(response.Data[0].Embedding))
	}

	if response.Data[0].Embedding[0] != 0.1 {
		t.Errorf("Embed() first value = %v, want 0.1", response.Data[0].Embedding[0])
	}
}

// TestLMStudioProvider_HealthCheck tests the HealthCheck method.
func TestLMStudioProvider_HealthCheck(t *testing.T) {
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
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					response := lmstudioModelsResponse{
						Object: "list",
						Data: []lmstudioModelInfo{
							{
								ID:     "model1",
								Object: "model",
							},
						},
					}
					json.NewEncoder(w).Encode(response)
				}
			}))
			defer server.Close()

			// Create provider
			provider, err := NewLMStudioProvider(LMStudioConfig{
				BaseURL: server.URL + "/v1",
				Models:  []string{"test-model"},
			}, nil, nil)
			if err != nil {
				t.Fatalf("NewLMStudioProvider() error = %v", err)
			}

			// Test health check
			ctx := context.Background()
			err = provider.HealthCheck(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("HealthCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestLMStudioProvider_GetUsage tests the GetUsage method.
func TestLMStudioProvider_GetUsage(t *testing.T) {
	provider, err := NewLMStudioProvider(LMStudioConfig{
		Models: []string{"test-model"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	usage := provider.GetUsage()
	if usage.RequestCount != 0 {
		t.Errorf("GetUsage() RequestCount = %v, want 0", usage.RequestCount)
	}

	if usage.TotalTokens != 0 {
		t.Errorf("GetUsage() TotalTokens = %v, want 0", usage.TotalTokens)
	}
}

// TestLMStudioProvider_ErrorHandling tests error handling.
func TestLMStudioProvider_ErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
			wantErr:    true,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			wantErr:    true,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(`{"error": "test error"}`))
			}))
			defer server.Close()

			// Create provider
			provider, err := NewLMStudioProvider(LMStudioConfig{
				BaseURL: server.URL + "/v1",
				Models:  []string{"test-model"},
			}, nil, nil)
			if err != nil {
				t.Fatalf("NewLMStudioProvider() error = %v", err)
			}

			// Test chat with error
			ctx := context.Background()
			request := llm.ChatRequest{
				Model: "test-model",
				Messages: []llm.ChatMessage{
					{
						Role:    "user",
						Content: "Hello",
					},
				},
			}

			_, err = provider.Chat(ctx, request)
			if (err != nil) != tt.wantErr {
				t.Errorf("Chat() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestLMStudioProvider_ContextCancellation tests context cancellation.
func TestLMStudioProvider_ContextCancellation(t *testing.T) {
	// Create mock server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create provider
	provider, err := NewLMStudioProvider(LMStudioConfig{
		BaseURL: server.URL + "/v1",
		Models:  []string{"test-model"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Test chat with cancelled context
	request := llm.ChatRequest{
		Model: "test-model",
		Messages: []llm.ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
	}

	_, err = provider.Chat(ctx, request)
	if err == nil {
		t.Error("Chat() expected error with cancelled context, got nil")
	}
}

// TestLMStudioProvider_SetMethods tests setter methods.
func TestLMStudioProvider_SetMethods(t *testing.T) {
	provider, err := NewLMStudioProvider(LMStudioConfig{
		Models: []string{"test-model"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	// Test SetAPIKey
	provider.SetAPIKey("new-key")
	if provider.apiKey != "new-key" {
		t.Errorf("SetAPIKey() failed, got %v", provider.apiKey)
	}

	// Test SetBaseURL
	provider.SetBaseURL("http://new-url:1234/v1")
	if provider.baseURL != "http://new-url:1234/v1" {
		t.Errorf("SetBaseURL() failed, got %v", provider.baseURL)
	}
}

// TestLMStudioProvider_Stop tests the Stop method.
func TestLMStudioProvider_Stop(t *testing.T) {
	provider, err := NewLMStudioProvider(LMStudioConfig{
		Models: []string{"test-model"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	ctx := context.Background()
	err = provider.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	if provider.started {
		t.Error("Stop() did not set started to false")
	}
}

// TestLMStudioProvider_DiscoverModels tests model discovery.
func TestLMStudioProvider_DiscoverModels(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		response := lmstudioModelsResponse{
			Object: "list",
			Data: []lmstudioModelInfo{
				{
					ID:      "model1",
					Object:  "model",
					Created: time.Now().Unix(),
					OwnedBy: "lmstudio",
				},
				{
					ID:      "model2",
					Object:  "model",
					Created: time.Now().Unix(),
					OwnedBy: "lmstudio",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider without models
	provider, err := NewLMStudioProvider(LMStudioConfig{
		BaseURL: server.URL + "/v1",
	}, nil, nil)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	// Refresh models
	ctx := context.Background()
	err = provider.RefreshModels(ctx)
	if err != nil {
		t.Fatalf("RefreshModels() error = %v", err)
	}

	// Check models
	models := provider.Models()
	if len(models) != 2 {
		t.Errorf("RefreshModels() discovered %d models, want 2", len(models))
	}

	expectedModels := []string{"model1", "model2"}
	for i, model := range models {
		if model != expectedModels[i] {
			t.Errorf("RefreshModels() model[%d] = %v, want %v", i, model, expectedModels[i])
		}
	}
}
