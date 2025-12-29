package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

// MockStreamingLLMManager for testing
type MockStreamingLLMManager struct {
	chatResponse llm.ChatResponse
	chatErr      error
	streamErr    error
}

func (m *MockStreamingLLMManager) Chat(ctx context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	if m.chatErr != nil {
		return llm.ChatResponse{}, m.chatErr
	}
	return m.chatResponse, nil
}

func (m *MockStreamingLLMManager) ChatStream(ctx context.Context, request llm.ChatRequest, handler func(llm.ChatStreamEvent) error) error {
	if m.streamErr != nil {
		return m.streamErr
	}
	// Simulate streaming events
	handler(llm.ChatStreamEvent{
		Choices: []llm.ChatChoice{
			{Delta: &llm.ChatMessage{Content: "Hello"}},
		},
	})
	handler(llm.ChatStreamEvent{
		Choices: []llm.ChatChoice{
			{Delta: &llm.ChatMessage{Content: " World"}},
		},
	})
	return nil
}

func TestAIGateway_Basic(t *testing.T) {
	gateway := NewAIGateway(nil, nil).
		AddProvider("openai", &MockStreamingLLMManager{
			chatResponse: llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{Message: llm.ChatMessage{Content: "Hello from OpenAI"}},
				},
			},
		}).
		Build()

	response, err := gateway.Chat(context.Background(), GatewayRequest{
		ChatRequest: llm.ChatRequest{
			Messages: []llm.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(response.Choices) == 0 {
		t.Fatal("Expected choices in response")
	}

	if response.Choices[0].Message.Content != "Hello from OpenAI" {
		t.Errorf("Unexpected content: %s", response.Choices[0].Message.Content)
	}
}

func TestAIGateway_WithFallback(t *testing.T) {
	gateway := NewAIGateway(nil, nil).
		AddProvider("openai", &MockStreamingLLMManager{
			chatErr: context.DeadlineExceeded,
		}).
		AddProvider("anthropic", &MockStreamingLLMManager{
			chatResponse: llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{Message: llm.ChatMessage{Content: "Hello from Anthropic"}},
				},
			},
		}).
		WithFallback("gpt-4", "anthropic:claude-3-opus").
		Build()

	response, err := gateway.Chat(context.Background(), GatewayRequest{
		ChatRequest: llm.ChatRequest{
			Model: "gpt-4",
			Messages: []llm.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if response.FinalProvider != "anthropic" {
		t.Errorf("Expected fallback to anthropic, got: %s", response.FinalProvider)
	}
}

func TestAIGateway_WithRouter(t *testing.T) {
	router := NewDefaultRouter()
	router.RegisterModel(ModelCapability{
		Provider:       "openai",
		Model:          "gpt-4",
		SupportsTools:  true,
		SupportsJSON:   true,
		CostPer1KInput: 0.03,
	})

	gateway := NewAIGateway(nil, nil).
		AddProvider("openai", &MockStreamingLLMManager{
			chatResponse: llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{Message: llm.ChatMessage{Content: "test"}},
				},
			},
		}).
		WithRouter(router).
		Build()

	response, err := gateway.Chat(context.Background(), GatewayRequest{
		ChatRequest: llm.ChatRequest{
			Model: "gpt-4",
			Messages: []llm.ChatMessage{
				{Role: "user", Content: "test"},
			},
		},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if response.RouteDecision.Provider != "openai" {
		t.Errorf("Expected openai provider, got: %s", response.RouteDecision.Provider)
	}
}

func TestDefaultRouter(t *testing.T) {
	router := NewDefaultRouter()
	router.RegisterModel(ModelCapability{
		Provider: "openai",
		Model:    "gpt-4",
	})

	decision, err := router.Route(context.Background(), &RouteRequest{
		ChatRequest: llm.ChatRequest{
			Model: "gpt-4",
		},
		AvailableProviders: []string{"openai"},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if decision == nil {
		t.Fatal("Expected decision")
	}

	if decision.Model != "gpt-4" {
		t.Errorf("Expected gpt-4, got: %s", decision.Model)
	}
}

func TestCostBasedRouter(t *testing.T) {
	optimizer := NewCostOptimizer(100)
	router := NewCostBasedRouter(optimizer)
	router.SetModelCost("openai:gpt-4", 0.03)
	router.SetModelCost("openai:gpt-3.5-turbo", 0.002)

	// Test with max cost constraint
	decision, err := router.Route(context.Background(), &RouteRequest{
		MaxCost:            0.01,
		AvailableProviders: []string{"openai"},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if decision != nil && decision.Model == "gpt-4" {
		t.Error("Should not route to expensive model when max cost is low")
	}
}

func TestCapabilityRouter(t *testing.T) {
	router := NewCapabilityRouter()
	router.RegisterCapability(ModelCapability{
		Provider:       "openai",
		Model:          "gpt-4-vision",
		SupportsVision: true,
		SupportsTools:  true,
	})
	router.RegisterCapability(ModelCapability{
		Provider:      "openai",
		Model:         "gpt-4",
		SupportsTools: true,
	})

	decision, err := router.Route(context.Background(), &RouteRequest{
		RequiredCaps:       []string{"vision"},
		AvailableProviders: []string{"openai"},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if decision == nil {
		t.Fatal("Expected decision")
	}

	if decision.Model != "gpt-4-vision" {
		t.Errorf("Expected gpt-4-vision for vision capability, got: %s", decision.Model)
	}
}

func TestLatencyBasedRouter(t *testing.T) {
	router := NewLatencyBasedRouter()
	// Record latency using the same model that will be used in routing
	router.RecordLatency("openai", "gpt-4-turbo", 500*time.Millisecond)
	router.RecordLatency("anthropic", "claude-3-opus-20240229", 200*time.Millisecond)

	decision, err := router.Route(context.Background(), &RouteRequest{
		AvailableProviders: []string{"openai", "anthropic"},
	})

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if decision == nil {
		t.Fatal("Expected decision")
	}

	// Should route to faster provider
	if decision.Provider != "anthropic" {
		t.Errorf("Expected anthropic (faster), got: %s", decision.Provider)
	}
}

func TestRoundRobinBalancer(t *testing.T) {
	balancer := NewRoundRobinBalancer()
	providers := []string{"a", "b", "c"}

	results := make(map[string]int)
	for i := 0; i < 9; i++ {
		selected := balancer.Select(providers)
		results[selected]++
	}

	// Each provider should be selected 3 times
	for _, p := range providers {
		if results[p] != 3 {
			t.Errorf("Expected %s to be selected 3 times, got %d", p, results[p])
		}
	}
}

func TestWeightedBalancer(t *testing.T) {
	balancer := NewWeightedBalancer()
	balancer.SetWeight("heavy", 10)
	balancer.SetWeight("light", 1)

	providers := []string{"heavy", "light"}
	results := make(map[string]int)

	for i := 0; i < 1000; i++ {
		selected := balancer.Select(providers)
		results[selected]++
	}

	// Heavy should be selected more often
	if results["heavy"] < results["light"]*5 {
		t.Errorf("Expected heavy to be selected much more often. Heavy: %d, Light: %d",
			results["heavy"], results["light"])
	}
}

func TestLeastConnectionsBalancer(t *testing.T) {
	balancer := NewLeastConnectionsBalancer()
	providers := []string{"a", "b", "c"}

	// Select provider multiple times
	balancer.Select(providers) // a gets connection
	balancer.Select(providers) // b gets connection

	// Next should go to c (least connections)
	selected := balancer.Select(providers)
	if selected != "c" {
		t.Errorf("Expected c (least connections), got: %s", selected)
	}

	// Release one connection from a
	balancer.Release("a")

	// Now a should have least connections (0)
	selected = balancer.Select(providers)
	if selected != "a" {
		t.Errorf("Expected a after release, got: %s", selected)
	}
}

func TestProviderHealthChecker(t *testing.T) {
	checker := NewProviderHealthChecker(nil, time.Second)
	checker.Start([]string{"openai", "anthropic"})
	defer checker.Stop()

	// Initially healthy
	if !checker.IsHealthy("openai") {
		t.Error("Expected openai to be healthy initially")
	}

	// Record failures
	for i := 0; i < 5; i++ {
		checker.RecordFailure("openai", context.DeadlineExceeded)
	}

	// Should be unhealthy after failures
	if checker.IsHealthy("openai") {
		t.Error("Expected openai to be unhealthy after failures")
	}

	// Other provider should still be healthy
	if !checker.IsHealthy("anthropic") {
		t.Error("Expected anthropic to still be healthy")
	}
}

func TestCostOptimizer(t *testing.T) {
	optimizer := NewCostOptimizer(100)
	optimizer.SetModelCost("gpt-4", 0.03)
	optimizer.SetModelCost("gpt-3.5-turbo", 0.002)

	// Test within budget
	if !optimizer.IsWithinBudget() {
		t.Error("Should be within budget initially")
	}

	// Record spending
	optimizer.RecordSpend(50)
	if optimizer.RemainingBudget() != 50 {
		t.Errorf("Expected remaining budget 50, got: %f", optimizer.RemainingBudget())
	}

	// Test model suggestion
	models := []string{"gpt-4", "gpt-3.5-turbo"}
	suggested := optimizer.SuggestModel(models, 0.01)
	if suggested != "gpt-3.5-turbo" {
		t.Errorf("Expected gpt-3.5-turbo (cheaper), got: %s", suggested)
	}

	// Exceed budget
	optimizer.RecordSpend(60)
	if optimizer.IsWithinBudget() {
		t.Error("Should not be within budget after exceeding")
	}

	// Reset
	optimizer.ResetSpend()
	if !optimizer.IsWithinBudget() {
		t.Error("Should be within budget after reset")
	}
}

func TestParseModelSpec(t *testing.T) {
	tests := []struct {
		spec             string
		expectedProvider string
		expectedModel    string
	}{
		{"openai:gpt-4", "openai", "gpt-4"},
		{"gpt-4", "", "gpt-4"},
		{"anthropic:claude-3-opus", "anthropic", "claude-3-opus"},
		{"", "", ""},
	}

	for _, tt := range tests {
		provider, model := parseModelSpec(tt.spec)
		if provider != tt.expectedProvider {
			t.Errorf("parseModelSpec(%s): expected provider %s, got %s",
				tt.spec, tt.expectedProvider, provider)
		}
		if model != tt.expectedModel {
			t.Errorf("parseModelSpec(%s): expected model %s, got %s",
				tt.spec, tt.expectedModel, model)
		}
	}
}

func TestGetDefaultModel(t *testing.T) {
	tests := []struct {
		provider string
		expected string
	}{
		{"openai", "gpt-4-turbo"},
		{"anthropic", "claude-3-opus-20240229"},
		{"unknown", "default"},
	}

	for _, tt := range tests {
		got := getDefaultModel(tt.provider)
		if got != tt.expected {
			t.Errorf("getDefaultModel(%s): expected %s, got %s",
				tt.provider, tt.expected, got)
		}
	}
}
