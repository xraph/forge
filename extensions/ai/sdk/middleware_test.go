package sdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/ai/llm"
)

func TestMiddlewareChain_Execute(t *testing.T) {
	chain := NewMiddlewareChain(nil, nil)

	var order []string

	// Add middleware that records execution order
	chain.Use(&testMiddleware{
		name:   "first",
		before: func() { order = append(order, "first-before") },
		after:  func() { order = append(order, "first-after") },
	})
	chain.Use(&testMiddleware{
		name:   "second",
		before: func() { order = append(order, "second-before") },
		after:  func() { order = append(order, "second-after") },
	})

	resp, err := chain.Execute(context.Background(), &MiddlewareRequest{}, func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		order = append(order, "handler")

		return &MiddlewareResponse{}, nil
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response")
	}

	// Verify execution order
	expected := []string{"first-before", "second-before", "handler", "second-after", "first-after"}
	if len(order) != len(expected) {
		t.Fatalf("Expected order %v, got %v", expected, order)
	}

	for i, v := range expected {
		if order[i] != v {
			t.Errorf("Order[%d]: expected %s, got %s", i, v, order[i])
		}
	}
}

type testMiddleware struct {
	name   string
	before func()
	after  func()
}

func (m *testMiddleware) Name() string { return m.name }

func (m *testMiddleware) ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error) {
	if m.before != nil {
		m.before()
	}

	resp, err := next(ctx, req)

	if m.after != nil {
		m.after()
	}

	return resp, err
}

func TestLoggingMiddleware(t *testing.T) {
	middleware := NewLoggingMiddleware(nil, LoggingConfig{
		LogRequest:  true,
		LogResponse: true,
	})

	chain := NewMiddlewareChain(nil, nil)
	chain.Use(middleware)

	resp, err := chain.Execute(context.Background(), &MiddlewareRequest{
		ChatRequest: llm.ChatRequest{
			Provider: "openai",
			Model:    "gpt-4",
		},
	}, func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{}, nil
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify middleware executed (response has duration)
	if resp == nil {
		t.Error("Expected response")
	}
}

func TestCachingMiddleware(t *testing.T) {
	cache := NewInMemoryCache()

	middleware := NewCachingMiddleware(CachingConfig{
		Cache: cache,
		TTL:   time.Minute,
	})

	chain := NewMiddlewareChain(nil, nil)
	chain.Use(middleware)

	callCount := 0
	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		callCount++

		return &MiddlewareResponse{
			ChatResponse: llm.ChatResponse{
				Choices: []llm.ChatChoice{
					{Message: llm.ChatMessage{Content: "test"}},
				},
			},
		}, nil
	}

	req := &MiddlewareRequest{
		ChatRequest: llm.ChatRequest{
			Provider: "openai",
			Model:    "gpt-4",
			Messages: []llm.ChatMessage{{Role: "user", Content: "hello"}},
		},
	}

	// First call - should not be cached
	resp1, err := chain.Execute(context.Background(), req, handler)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if resp1.Cached {
		t.Error("First response should not be cached")
	}

	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	// Second call - should be cached
	resp2, err := chain.Execute(context.Background(), req, handler)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !resp2.Cached {
		t.Error("Second response should be cached")
	}

	if callCount != 1 {
		t.Errorf("Expected still 1 call (cached), got %d", callCount)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	limiter := NewTokenBucketLimiter(1, 2) // 1 token/sec, capacity 2

	middleware := NewRateLimitMiddleware(MiddlewareRateLimitConfig{
		Limiter: limiter,
	})

	chain := NewMiddlewareChain(nil, nil)
	chain.Use(middleware)

	req := &MiddlewareRequest{
		ChatRequest: llm.ChatRequest{
			Provider: "openai",
			Model:    "gpt-4",
		},
	}

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{}, nil
	}

	// First two requests should succeed (capacity 2)
	_, err := chain.Execute(context.Background(), req, handler)
	if err != nil {
		t.Fatalf("First request should succeed: %v", err)
	}

	_, err = chain.Execute(context.Background(), req, handler)
	if err != nil {
		t.Fatalf("Second request should succeed: %v", err)
	}

	// Third request should be rate limited
	_, err = chain.Execute(context.Background(), req, handler)
	if err == nil {
		t.Error("Third request should be rate limited")
	}
}

func TestTokenBucketLimiter(t *testing.T) {
	limiter := NewTokenBucketLimiter(10, 5) // 10 tokens/sec, capacity 5

	// Should allow up to capacity
	for i := range 5 {
		if !limiter.Allow("test") {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	// Next should be rejected
	if limiter.Allow("test") {
		t.Error("Request should be rejected (over capacity)")
	}

	// Wait for refill
	time.Sleep(200 * time.Millisecond)

	// Should allow again
	if !limiter.Allow("test") {
		t.Error("Request should be allowed after refill")
	}
}

func TestSlidingWindowLimiter(t *testing.T) {
	limiter := NewSlidingWindowLimiter(3, 100*time.Millisecond)

	// Should allow up to limit
	for i := range 3 {
		if !limiter.Allow("test") {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	// Next should be rejected
	if limiter.Allow("test") {
		t.Error("Request should be rejected (over limit)")
	}

	// Wait for new window
	time.Sleep(150 * time.Millisecond)

	// Should allow again
	if !limiter.Allow("test") {
		t.Error("Request should be allowed in new window")
	}
}

func TestCostTrackingMiddleware(t *testing.T) {
	var recordedCost float64

	middleware := NewCostTrackingMiddleware(CostTrackingConfig{
		Costs: map[string]float64{
			"gpt-4": 0.03,
		},
		OnCostEvent: func(model string, cost float64, totalCost float64) {
			recordedCost = cost
		},
	})

	chain := NewMiddlewareChain(nil, nil)
	chain.Use(middleware)

	req := &MiddlewareRequest{
		ChatRequest: llm.ChatRequest{
			Model: "gpt-4",
		},
	}

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		return &MiddlewareResponse{
			ChatResponse: llm.ChatResponse{
				Usage: &llm.LLMUsage{
					TotalTokens: 1000, // 1K tokens
				},
			},
		}, nil
	}

	_, err := chain.Execute(context.Background(), req, handler)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// 1K tokens at $0.03/1K = $0.03
	if recordedCost != 0.03 {
		t.Errorf("Expected cost $0.03, got %f", recordedCost)
	}

	if middleware.GetTotalCost() != 0.03 {
		t.Errorf("Expected total cost $0.03, got %f", middleware.GetTotalCost())
	}
}

func TestRetryMiddleware(t *testing.T) {
	attempts := 0
	middleware := NewRetryMiddleware(MiddlewareRetryConfig{
		MaxRetries:   3,
		InitialDelay: 10 * time.Millisecond,
		RetryOn:      func(err error) bool { return true },
	})

	chain := NewMiddlewareChain(nil, nil)
	chain.Use(middleware)

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("temporary error")
		}

		return &MiddlewareResponse{}, nil
	}

	_, err := chain.Execute(context.Background(), &MiddlewareRequest{}, handler)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	middleware := NewTimeoutMiddleware(50 * time.Millisecond)

	chain := NewMiddlewareChain(nil, nil)
	chain.Use(middleware)

	handler := func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
		// Wait for context to be cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return &MiddlewareResponse{}, nil
		}
	}

	_, err := chain.Execute(context.Background(), &MiddlewareRequest{}, handler)
	if err == nil {
		t.Error("Expected timeout error")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected DeadlineExceeded, got %v", err)
	}
}

func TestInMemoryCache(t *testing.T) {
	cache := NewInMemoryCache()
	ctx := context.Background()

	// Test Set and Get
	err := cache.Set(ctx, "key1", []byte("value1"), time.Hour)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	value, ok := cache.Get(ctx, "key1")
	if !ok {
		t.Error("Expected value to exist")
	}

	if string(value) != "value1" {
		t.Errorf("Expected 'value1', got '%s'", value)
	}

	// Test Delete
	err = cache.Delete(ctx, "key1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, ok = cache.Get(ctx, "key1")
	if ok {
		t.Error("Expected value to be deleted")
	}

	// Test expiration
	err = cache.Set(ctx, "expiring", []byte("value"), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_, ok = cache.Get(ctx, "expiring")
	if ok {
		t.Error("Expected value to be expired")
	}
}

func TestDefaultCacheKeyFunc(t *testing.T) {
	req1 := &MiddlewareRequest{
		ChatRequest: llm.ChatRequest{
			Provider: "openai",
			Model:    "gpt-4",
			Messages: []llm.ChatMessage{{Role: "user", Content: "hello"}},
		},
	}

	req2 := &MiddlewareRequest{
		ChatRequest: llm.ChatRequest{
			Provider: "openai",
			Model:    "gpt-4",
			Messages: []llm.ChatMessage{{Role: "user", Content: "hello"}},
		},
	}

	req3 := &MiddlewareRequest{
		ChatRequest: llm.ChatRequest{
			Provider: "openai",
			Model:    "gpt-4",
			Messages: []llm.ChatMessage{{Role: "user", Content: "different"}},
		},
	}

	key1 := DefaultCacheKeyFunc(req1)
	key2 := DefaultCacheKeyFunc(req2)
	key3 := DefaultCacheKeyFunc(req3)

	// Same request should produce same key
	if key1 != key2 {
		t.Error("Same requests should have same cache key")
	}

	// Different request should produce different key
	if key1 == key3 {
		t.Error("Different requests should have different cache keys")
	}
}
