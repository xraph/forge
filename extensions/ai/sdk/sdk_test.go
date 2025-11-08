package sdk

import (
	"testing"
)

func TestVersion(t *testing.T) {
	if Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", Version)
	}
}

func TestFieldCreation(t *testing.T) {
	field := F("key", "value")
	if field.Key() != "key" {
		t.Errorf("expected key 'key', got %s", field.Key())
	}

	if field.Value() != "value" {
		t.Errorf("expected value 'value', got %v", field.Value())
	}
}

func TestOptions(t *testing.T) {
	opts := Options{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4",
		APIKey:          map[string]string{"openai": "test-key"},
	}

	if opts.DefaultProvider != "openai" {
		t.Errorf("expected provider 'openai', got %s", opts.DefaultProvider)
	}

	if opts.DefaultModel != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %s", opts.DefaultModel)
	}

	if opts.APIKey["openai"] != "test-key" {
		t.Errorf("expected API key 'test-key', got %s", opts.APIKey["openai"])
	}
}

func TestVector(t *testing.T) {
	vec := Vector{
		ID:       "vec-1",
		Values:   []float64{0.1, 0.2, 0.3},
		Metadata: map[string]any{"type": "test"},
	}

	if vec.ID != "vec-1" {
		t.Errorf("expected ID 'vec-1', got %s", vec.ID)
	}

	if len(vec.Values) != 3 {
		t.Errorf("expected 3 values, got %d", len(vec.Values))
	}

	if vec.Metadata["type"] != "test" {
		t.Errorf("expected metadata type 'test', got %v", vec.Metadata["type"])
	}
}

func TestVectorMatch(t *testing.T) {
	match := VectorMatch{
		ID:       "match-1",
		Score:    0.95,
		Metadata: map[string]any{"source": "doc"},
	}

	if match.ID != "match-1" {
		t.Errorf("expected ID 'match-1', got %s", match.ID)
	}

	if match.Score != 0.95 {
		t.Errorf("expected score 0.95, got %f", match.Score)
	}
}

func TestUsage(t *testing.T) {
	usage := Usage{
		Provider:     "openai",
		Model:        "gpt-4",
		InputTokens:  100,
		OutputTokens: 50,
		Cost:         0.005,
	}

	if usage.Provider != "openai" {
		t.Errorf("expected provider 'openai', got %s", usage.Provider)
	}

	if usage.InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %d", usage.InputTokens)
	}

	if usage.OutputTokens != 50 {
		t.Errorf("expected output tokens 50, got %d", usage.OutputTokens)
	}
}

func TestResult(t *testing.T) {
	result := Result{
		Content:      "test response",
		Metadata:     map[string]any{"model": "gpt-4"},
		FinishReason: "stop",
	}

	if result.Content != "test response" {
		t.Errorf("expected content 'test response', got %s", result.Content)
	}

	if result.FinishReason != "stop" {
		t.Errorf("expected finish reason 'stop', got %s", result.FinishReason)
	}

	if result.Error != nil {
		t.Errorf("expected no error, got %v", result.Error)
	}
}

func TestCostInsights(t *testing.T) {
	insights := CostInsights{
		CostToday:        10.5,
		CostThisMonth:    150.75,
		ProjectedMonthly: 450.0,
		CacheHitRate:     0.85,
		PotentialSavings: 50.0,
	}

	if insights.CostToday != 10.5 {
		t.Errorf("expected cost today 10.5, got %f", insights.CostToday)
	}

	if insights.CacheHitRate != 0.85 {
		t.Errorf("expected cache hit rate 0.85, got %f", insights.CacheHitRate)
	}
}

func TestModelCost(t *testing.T) {
	modelCost := ModelCost{
		Model: "gpt-4",
		Cost:  100.0,
		Calls: 1000,
	}

	if modelCost.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %s", modelCost.Model)
	}

	if modelCost.Cost != 100.0 {
		t.Errorf("expected cost 100.0, got %f", modelCost.Cost)
	}

	if modelCost.Calls != 1000 {
		t.Errorf("expected calls 1000, got %d", modelCost.Calls)
	}
}

func TestRateLimitConfig(t *testing.T) {
	config := RateLimitConfig{
		RequestsPerMinute: 100,
		TokensPerMinute:   10000,
		BurstSize:         10,
	}

	if config.RequestsPerMinute != 100 {
		t.Errorf("expected 100 requests per minute, got %d", config.RequestsPerMinute)
	}

	if config.TokensPerMinute != 10000 {
		t.Errorf("expected 10000 tokens per minute, got %d", config.TokensPerMinute)
	}

	if config.BurstSize != 10 {
		t.Errorf("expected burst size 10, got %d", config.BurstSize)
	}
}
