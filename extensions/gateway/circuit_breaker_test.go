package gateway

import (
	"testing"
	"time"
)

// Note: Circuit breaker tests are simplified since the implementation
// doesn't expose a Stats() method. Tests focus on state transitions
// and Allow() behavior.

func TestNewCircuitBreaker(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)
	if cb == nil {
		t.Fatal("expected circuit breaker, got nil")
	}

	if cb.State() != CircuitClosed {
		t.Errorf("expected initial state closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_Allow_Closed(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Closed circuit should allow requests
	if !cb.Allow() {
		t.Error("closed circuit should allow requests")
	}
}

func TestCircuitBreaker_TransitionToOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     1 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Record failures to trigger open state
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Errorf("expected state open after %d failures, got %s", 3, cb.State())
	}

	// Open circuit should not allow requests
	if cb.Allow() {
		t.Error("open circuit should not allow requests")
	}
}

func TestCircuitBreaker_TransitionToHalfOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     100 * time.Millisecond,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Trigger open state
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected state open, got %s", cb.State())
	}

	// Wait for timeout to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Next request should transition to half-open
	if !cb.Allow() {
		t.Error("expected half-open circuit to allow probe request")
	}

	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected state half-open after timeout, got %s", cb.State())
	}
}

func TestCircuitBreaker_RecoverFromHalfOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     100 * time.Millisecond,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Trigger open state
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Transition to half-open
	_ = cb.Allow()

	// Record successful requests to close circuit
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Errorf("expected state closed after %d successes, got %s", 2, cb.State())
	}
}

func TestCircuitBreaker_FailInHalfOpen(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     100 * time.Millisecond,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Trigger open state
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Transition to half-open
	_ = cb.Allow()

	// Record failure in half-open state
	cb.RecordFailure()

	// Should transition back to open
	if cb.State() != CircuitOpen {
		t.Errorf("expected state open after failure in half-open, got %s", cb.State())
	}
}

func TestCircuitBreaker_SuccessInClosed(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Record successes in closed state
	cb.RecordSuccess()
	cb.RecordSuccess()

	// Should remain closed
	if cb.State() != CircuitClosed {
		t.Errorf("expected state closed after successes, got %s", cb.State())
	}

	// Record a failure
	cb.RecordFailure()

	// Should still be closed (below threshold)
	if cb.State() != CircuitClosed {
		t.Errorf("expected state closed after single failure, got %s", cb.State())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Trigger open state
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected state open, got %s", cb.State())
	}

	// Reset circuit
	cb.Reset()

	if cb.State() != CircuitClosed {
		t.Errorf("expected state closed after reset, got %s", cb.State())
	}
}

func TestCircuitBreaker_Disabled(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          false,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Record many failures
	for i := 0; i < 10; i++ {
		cb.RecordFailure()
	}

	// Should still allow requests when disabled
	if !cb.Allow() {
		t.Error("disabled circuit breaker should always allow requests")
	}
}

func TestCircuitBreakerManager(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	mgr := NewCircuitBreakerManager(config)

	// Get circuit breaker for target
	cb1 := mgr.Get("target-1")
	if cb1 == nil {
		t.Fatal("expected circuit breaker, got nil")
	}

	// Getting same target should return same circuit breaker
	cb2 := mgr.Get("target-1")
	if cb1 != cb2 {
		t.Error("expected same circuit breaker instance for same target")
	}

	// Different target should return different circuit breaker
	cb3 := mgr.Get("target-2")
	if cb1 == cb3 {
		t.Error("expected different circuit breaker instance for different target")
	}
}

func TestCircuitBreakerManager_MultipleTargets(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 3,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	mgr := NewCircuitBreakerManager(config)

	// Create multiple circuit breakers and trigger open state
	targetIDs := []string{"target-1", "target-2", "target-3"}

	for _, targetID := range targetIDs {
		cb := mgr.Get(targetID)
		for j := 0; j < 3; j++ {
			cb.RecordFailure()
		}

		if cb.State() != CircuitOpen {
			t.Errorf("expected circuit %s to be open", targetID)
		}
	}

	// Verify each target has its own circuit breaker
	for i, targetID := range targetIDs {
		cb := mgr.Get(targetID)
		if cb.State() != CircuitOpen {
			t.Errorf("target %d: expected state open, got %s", i, cb.State())
		}
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 100,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     10 * time.Second,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	done := make(chan bool)

	// Concurrent operations
	for i := 0; i < 50; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = cb.Allow()
				if j%2 == 0 {
					cb.RecordSuccess()
				} else {
					cb.RecordFailure()
				}
				_ = cb.State()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 50; i++ {
		<-done
	}

	// Should not panic or race
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	config := CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 2,
		FailureWindow:    10 * time.Second,
		ResetTimeout:     50 * time.Millisecond,
		HalfOpenMax:      2,
	}

	cb := NewCircuitBreaker("test-target", config)

	// Start: Closed
	if cb.State() != CircuitClosed {
		t.Errorf("expected initial state closed, got %s", cb.State())
	}

	// Closed -> Open (after failures)
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected state open after failures, got %s", cb.State())
	}

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Open -> HalfOpen (after timeout + request)
	if !cb.Allow() {
		t.Error("expected half-open to allow request")
	}

	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected state half-open, got %s", cb.State())
	}

	// HalfOpen -> Closed (after successes)
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Errorf("expected state closed after successes, got %s", cb.State())
	}
}
