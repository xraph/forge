package sdk

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test Circuit Breaker

func TestNewCircuitBreaker(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:         "test-cb",
		MaxFailures:  3,
		ResetTimeout: 30 * time.Second,
		HalfOpenMax:  2,
	}

	cb := NewCircuitBreaker(config, nil, nil)

	if cb.name != "test-cb" {
		t.Errorf("expected name 'test-cb', got '%s'", cb.name)
	}

	if cb.maxFailures != 3 {
		t.Errorf("expected max failures 3, got %d", cb.maxFailures)
	}

	if cb.state != CircuitStateClosed {
		t.Errorf("expected initial state closed, got %s", cb.state)
	}
}

func TestNewCircuitBreaker_Defaults(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{Name: "test"}, nil, nil)

	if cb.maxFailures != 5 {
		t.Errorf("expected default max failures 5, got %d", cb.maxFailures)
	}

	if cb.resetTimeout != 60*time.Second {
		t.Errorf("expected default reset timeout 60s, got %v", cb.resetTimeout)
	}

	if cb.halfOpenMax != 2 {
		t.Errorf("expected default half open max 2, got %d", cb.halfOpenMax)
	}
}

func TestCircuitBreaker_Success(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 3,
	}, nil, nil)

	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if cb.GetState() != CircuitStateClosed {
		t.Errorf("expected state closed, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_OpenAfterFailures(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 3,
	}, nil, nil)

	testErr := errors.New("test error")

	// Fail 3 times to open circuit
	for i := 0; i < 3; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.GetState() != CircuitStateOpen {
		t.Errorf("expected state open after 3 failures, got %s", cb.GetState())
	}

	// Next call should be rejected
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		t.Error("function should not be called when circuit is open")
		return nil
	})

	if err == nil {
		t.Error("expected error when circuit is open")
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  2,
		ResetTimeout: 100 * time.Millisecond,
		HalfOpenMax:  2,
	}, nil, nil)

	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return testErr
		})
	}

	if cb.GetState() != CircuitStateOpen {
		t.Fatal("circuit should be open")
	}

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Next call should transition to half-open
	cb.Execute(context.Background(), func(ctx context.Context) error {
		return nil
	})

	if cb.GetState() != CircuitStateHalfOpen {
		t.Errorf("expected state half-open, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:         "test",
		MaxFailures:  2,
		ResetTimeout: 100 * time.Millisecond,
		HalfOpenMax:  2,
	}, nil, nil)

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return errors.New("error")
		})
	}

	// Wait and transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Succeed twice to close
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return nil
		})
	}

	if cb.GetState() != CircuitStateClosed {
		t.Errorf("expected state closed after successful half-open, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 2,
	}, nil, nil)

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func(ctx context.Context) error {
			return errors.New("error")
		})
	}

	if cb.GetState() != CircuitStateOpen {
		t.Fatal("circuit should be open")
	}

	cb.Reset()

	if cb.GetState() != CircuitStateClosed {
		t.Errorf("expected state closed after reset, got %s", cb.GetState())
	}
}

// Test Retry

func TestRetry_Success(t *testing.T) {
	called := 0
	err := Retry(context.Background(), DefaultRetryConfig(), nil, func(ctx context.Context) error {
		called++
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if called != 1 {
		t.Errorf("expected function to be called once, got %d", called)
	}
}

func TestRetry_EventualSuccess(t *testing.T) {
	called := 0
	err := Retry(context.Background(), DefaultRetryConfig(), nil, func(ctx context.Context) error {
		called++
		if called < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected no error after retries, got %v", err)
	}

	if called != 3 {
		t.Errorf("expected function to be called 3 times, got %d", called)
	}
}

func TestRetry_MaxAttemptsExceeded(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	called := 0
	testErr := errors.New("persistent error")

	err := Retry(context.Background(), config, nil, func(ctx context.Context) error {
		called++
		return testErr
	})

	if err == nil {
		t.Error("expected error after max retries")
	}

	if called != 3 {
		t.Errorf("expected function to be called 3 times, got %d", called)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	
	config := RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
		Multiplier:   2.0,
	}

	called := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, config, nil, func(ctx context.Context) error {
		called++
		return errors.New("error")
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if called > 2 {
		t.Errorf("expected at most 2 calls before cancellation, got %d", called)
	}
}

func TestRetry_RetryableErrors(t *testing.T) {
	retryableErr := errors.New("retryable")
	nonRetryableErr := errors.New("non-retryable")

	config := RetryConfig{
		MaxAttempts:     3,
		InitialDelay:    10 * time.Millisecond,
		RetryableErrors: []error{retryableErr},
	}

	// Non-retryable error should fail immediately
	called := 0
	err := Retry(context.Background(), config, nil, func(ctx context.Context) error {
		called++
		return nonRetryableErr
	})

	if !errors.Is(err, nonRetryableErr) {
		t.Errorf("expected non-retryable error, got %v", err)
	}

	if called != 1 {
		t.Errorf("expected 1 call for non-retryable error, got %d", called)
	}
}

// Test Rate Limiter

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter("test", 10, 20, nil, nil)

	if rl.name != "test" {
		t.Errorf("expected name 'test', got '%s'", rl.name)
	}

	if rl.rate != 10 {
		t.Errorf("expected rate 10, got %d", rl.rate)
	}

	if rl.burst != 20 {
		t.Errorf("expected burst 20, got %d", rl.burst)
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter("test", 10, 5, nil, nil)

	// Should allow up to burst size
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Errorf("expected request %d to be allowed", i+1)
		}
	}

	// Next should be rejected (no time for refill)
	if rl.Allow() {
		t.Error("expected request to be rejected after burst exhausted")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter("test", 10, 2, nil, nil)

	// Exhaust tokens
	rl.Allow()
	rl.Allow()

	// Wait for refill (100ms at 10/s = 1 token)
	time.Sleep(150 * time.Millisecond)

	if !rl.Allow() {
		t.Error("expected request to be allowed after refill")
	}
}

func TestRateLimiter_Wait(t *testing.T) {
	rl := NewRateLimiter("test", 10, 1, nil, nil)

	// Exhaust token
	rl.Allow()

	// Wait should succeed eventually
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := rl.Wait(ctx)
	if err != nil {
		t.Errorf("expected wait to succeed, got %v", err)
	}
}

func TestRateLimiter_WaitTimeout(t *testing.T) {
	rl := NewRateLimiter("test", 1, 0, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := rl.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected deadline exceeded, got %v", err)
	}
}

// Test Fallback Chain

func TestFallbackChain_FirstSucceeds(t *testing.T) {
	fc := NewFallbackChain("test", nil, nil)

	called := []int{}
	err := fc.Execute(context.Background(),
		func(ctx context.Context) error {
			called = append(called, 1)
			return nil
		},
		func(ctx context.Context) error {
			called = append(called, 2)
			return nil
		},
	)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(called) != 1 || called[0] != 1 {
		t.Errorf("expected only first function to be called, got %v", called)
	}
}

func TestFallbackChain_SecondSucceeds(t *testing.T) {
	fc := NewFallbackChain("test", nil, nil)

	called := []int{}
	err := fc.Execute(context.Background(),
		func(ctx context.Context) error {
			called = append(called, 1)
			return errors.New("error 1")
		},
		func(ctx context.Context) error {
			called = append(called, 2)
			return nil
		},
	)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(called) != 2 {
		t.Errorf("expected both functions to be called, got %v", called)
	}
}

func TestFallbackChain_AllFail(t *testing.T) {
	fc := NewFallbackChain("test", nil, nil)

	err := fc.Execute(context.Background(),
		func(ctx context.Context) error {
			return errors.New("error 1")
		},
		func(ctx context.Context) error {
			return errors.New("error 2")
		},
	)

	if err == nil {
		t.Error("expected error when all fallbacks fail")
	}
}

func TestFallbackChain_NoFunctions(t *testing.T) {
	fc := NewFallbackChain("test", nil, nil)

	err := fc.Execute(context.Background())

	if err == nil {
		t.Error("expected error when no functions provided")
	}
}

// Test Bulkhead

func TestNewBulkhead(t *testing.T) {
	bh := NewBulkhead("test", 5, nil, nil)

	if bh.name != "test" {
		t.Errorf("expected name 'test', got '%s'", bh.name)
	}

	if bh.maxConcurrent != 5 {
		t.Errorf("expected max concurrent 5, got %d", bh.maxConcurrent)
	}
}

func TestBulkhead_ConcurrencyLimit(t *testing.T) {
	bh := NewBulkhead("test", 2, nil, nil)

	var wg sync.WaitGroup
	var maxConcurrent int32

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			bh.Execute(context.Background(), func(ctx context.Context) error {
				current := atomic.AddInt32(&maxConcurrent, 1)
				defer atomic.AddInt32(&maxConcurrent, -1)
				
				if current > 2 {
					t.Errorf("exceeded max concurrent: %d", current)
				}
				
				time.Sleep(10 * time.Millisecond)
				return nil
			})
		}()
	}

	wg.Wait()
}

func TestBulkhead_GetActiveCount(t *testing.T) {
	bh := NewBulkhead("test", 3, nil, nil)

	if bh.GetActiveCount() != 0 {
		t.Errorf("expected 0 active, got %d", bh.GetActiveCount())
	}

	done := make(chan bool)
	go func() {
		bh.Execute(context.Background(), func(ctx context.Context) error {
			<-done
			return nil
		})
	}()

	time.Sleep(10 * time.Millisecond)

	if bh.GetActiveCount() != 1 {
		t.Errorf("expected 1 active, got %d", bh.GetActiveCount())
	}

	close(done)
	time.Sleep(10 * time.Millisecond)

	if bh.GetActiveCount() != 0 {
		t.Errorf("expected 0 active after completion, got %d", bh.GetActiveCount())
	}
}

func TestBulkhead_Timeout(t *testing.T) {
	bh := NewBulkhead("test", 1, nil, nil)

	// Fill the bulkhead
	done := make(chan bool)
	go func() {
		bh.Execute(context.Background(), func(ctx context.Context) error {
			<-done
			return nil
		})
	}()

	time.Sleep(10 * time.Millisecond)

	// Try with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := bh.Execute(ctx, func(ctx context.Context) error {
		return nil
	})

	if err != context.DeadlineExceeded {
		t.Errorf("expected deadline exceeded, got %v", err)
	}

	close(done)
}

// Test Timeout

func TestTimeout_Success(t *testing.T) {
	err := Timeout(context.Background(), 100*time.Millisecond, func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestTimeout_Exceeded(t *testing.T) {
	err := Timeout(context.Background(), 50*time.Millisecond, func(ctx context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	if err != context.DeadlineExceeded {
		t.Errorf("expected deadline exceeded, got %v", err)
	}
}

func TestTimeout_FunctionError(t *testing.T) {
	testErr := errors.New("test error")
	err := Timeout(context.Background(), 100*time.Millisecond, func(ctx context.Context) error {
		return testErr
	})

	if !errors.Is(err, testErr) {
		t.Errorf("expected test error, got %v", err)
	}
}

// Integration Tests

func TestCircuitBreakerWithRetry(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name:        "test",
		MaxFailures: 5, // Allow enough failures for retry to work
	}, nil, nil)

	config := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		Multiplier:   2.0,
	}

	attempts := 0
	err := Retry(context.Background(), config, nil, func(ctx context.Context) error {
		attempts++
		return cb.Execute(ctx, func(ctx context.Context) error {
			if attempts < 2 {
				return errors.New("temporary error")
			}
			return nil
		})
	})

	if err != nil {
		t.Errorf("expected success with retry, got %v", err)
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRateLimiterWithBulkhead(t *testing.T) {
	rl := NewRateLimiter("test", 5, 2, nil, nil)
	bh := NewBulkhead("test", 1, nil, nil)

	var wg sync.WaitGroup
	successCount := int32(0)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			if rl.Allow() {
				bh.Execute(context.Background(), func(ctx context.Context) error {
					atomic.AddInt32(&successCount, 1)
					time.Sleep(10 * time.Millisecond)
					return nil
				})
			}
		}()
	}

	wg.Wait()

	// Only 2 should succeed (burst size)
	if successCount != 2 {
		t.Errorf("expected 2 successful executions, got %d", successCount)
	}
}

