package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestTokenBucket_Allow(t *testing.T) {
	config := RateLimitConfig{
		ActionLimits: map[string]RateLimit{
			"test": {
				Requests: 10,
				Window:   time.Second,
				Burst:    5,
			},
		},
	}

	limiter := NewTokenBucket(config, nil)
	ctx := context.Background()

	// Should allow up to burst size immediately
	for i := 0; i < 5; i++ {
		allowed, err := limiter.Allow(ctx, "user1", "test")
		if err != nil {
			t.Fatalf("Allow() error = %v", err)
		}
		if !allowed {
			t.Errorf("request %d should be allowed", i)
		}
	}

	// Next request should be denied (burst exhausted)
	allowed, err := limiter.Allow(ctx, "user1", "test")
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if allowed {
		t.Error("request should be denied after burst exhausted")
	}
}

func TestTokenBucket_AllowN(t *testing.T) {
	config := RateLimitConfig{
		ActionLimits: map[string]RateLimit{
			"bulk": {
				Requests: 10,
				Window:   time.Second,
				Burst:    20,
			},
		},
	}

	limiter := NewTokenBucket(config, nil)
	ctx := context.Background()

	// Allow batch of 10
	allowed, err := limiter.AllowN(ctx, "user1", "bulk", 10)
	if err != nil {
		t.Fatalf("AllowN() error = %v", err)
	}
	if !allowed {
		t.Error("batch should be allowed")
	}

	// Try another batch of 15 (should fail, only 10 tokens left)
	allowed, err = limiter.AllowN(ctx, "user1", "bulk", 15)
	if err != nil {
		t.Fatalf("AllowN() error = %v", err)
	}
	if allowed {
		t.Error("batch should be denied")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	config := RateLimitConfig{
		ActionLimits: map[string]RateLimit{
			"refill": {
				Requests: 10,
				Window:   time.Second,
				Burst:    5,
			},
		},
	}

	limiter := NewTokenBucket(config, nil)
	ctx := context.Background()

	// Exhaust tokens
	for i := 0; i < 5; i++ {
		limiter.Allow(ctx, "user1", "refill")
	}

	// Should be denied
	allowed, err := limiter.Allow(ctx, "user1", "refill")
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if allowed {
		t.Error("should be denied")
	}

	// Wait for refill (10 req/sec = 1 token per 100ms)
	time.Sleep(200 * time.Millisecond)

	// Should be allowed now (2 tokens refilled)
	allowed, err = limiter.Allow(ctx, "user1", "refill")
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !allowed {
		t.Error("should be allowed after refill")
	}
}

func TestTokenBucket_GetStatus(t *testing.T) {
	config := RateLimitConfig{
		ActionLimits: map[string]RateLimit{
			"status": {
				Requests: 10,
				Window:   time.Second,
				Burst:    5,
			},
		},
	}

	limiter := NewTokenBucket(config, nil)
	ctx := context.Background()

	// Check initial status
	status, err := limiter.GetStatus(ctx, "user1", "status")
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}

	if status.Remaining != 5 {
		t.Errorf("expected 5 remaining, got %d", status.Remaining)
	}

	if status.Limit != 5 {
		t.Errorf("expected limit 5, got %d", status.Limit)
	}

	if !status.Allowed {
		t.Error("should be allowed")
	}

	// Consume a token
	limiter.Allow(ctx, "user1", "status")

	// Check status again
	status, err = limiter.GetStatus(ctx, "user1", "status")
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}

	if status.Remaining != 4 {
		t.Errorf("expected 4 remaining, got %d", status.Remaining)
	}
}

func TestTokenBucket_Reset(t *testing.T) {
	config := RateLimitConfig{
		ActionLimits: map[string]RateLimit{
			"reset": {
				Requests: 10,
				Window:   time.Second,
				Burst:    5,
			},
		},
	}

	limiter := NewTokenBucket(config, nil)
	ctx := context.Background()

	// Exhaust tokens
	for i := 0; i < 5; i++ {
		limiter.Allow(ctx, "user1", "reset")
	}

	// Reset
	err := limiter.Reset(ctx, "user1", "reset")
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}

	// Should have full burst again
	status, err := limiter.GetStatus(ctx, "user1", "reset")
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}

	if status.Remaining != 5 {
		t.Errorf("expected 5 remaining after reset, got %d", status.Remaining)
	}
}

func BenchmarkTokenBucket_Allow(b *testing.B) {
	config := RateLimitConfig{
		ActionLimits: map[string]RateLimit{
			"bench": {
				Requests: 1000000, // High limit for benchmarking
				Window:   time.Second,
				Burst:    1000000,
			},
		},
	}

	limiter := NewTokenBucket(config, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = limiter.Allow(ctx, "user1", "bench")
	}
}

func BenchmarkTokenBucket_AllowN(b *testing.B) {
	config := RateLimitConfig{
		ActionLimits: map[string]RateLimit{
			"benchN": {
				Requests: 1000000,
				Window:   time.Second,
				Burst:    1000000,
			},
		},
	}

	limiter := NewTokenBucket(config, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = limiter.AllowN(ctx, "user1", "benchN", 10)
	}
}
