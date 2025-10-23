package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/metrics"
)

func TestNewCircuitBreaker(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)
	if breaker == nil {
		t.Error("NewCircuitBreaker() returned nil")
	}

	if breaker.config.Name != config.Name {
		t.Error("CircuitBreaker name not set correctly")
	}
}

func TestCircuitBreaker_Execute_Success(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Execute successful function
	result, err := breaker.Execute(context.Background(), func() (interface{}, error) {
		return "success", nil
	})

	if err != nil {
		t.Errorf("Execute() error = %v", err)
	}

	if result != "success" {
		t.Errorf("Execute() result = %v, want %v", result, "success")
	}

	// Check state
	if breaker.GetState() != CircuitStateClosed {
		t.Errorf("GetState() = %v, want %v", breaker.GetState(), CircuitStateClosed)
	}
}

func TestCircuitBreaker_Execute_Failure(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Execute failing function
	result, err := breaker.Execute(context.Background(), func() (interface{}, error) {
		return nil, errors.New("test error")
	})

	if err == nil {
		t.Error("Execute() should return error")
	}

	if result != nil {
		t.Errorf("Execute() result = %v, want nil", result)
	}

	// Check state
	if breaker.GetState() != CircuitStateClosed {
		t.Errorf("GetState() = %v, want %v", breaker.GetState(), CircuitStateClosed)
	}
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      5,
		Timeout:          60 * time.Second,
		MaxFailures:      3,
		FailureThreshold: 0.6,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  1 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Initially closed
	if breaker.GetState() != CircuitStateClosed {
		t.Errorf("Initial state = %v, want %v", breaker.GetState(), CircuitStateClosed)
	}

	// Fail multiple times to open circuit
	for i := 0; i < 5; i++ {
		breaker.Execute(context.Background(), func() (interface{}, error) {
			return nil, errors.New("test error")
		})
	}

	// Circuit should be open
	if breaker.GetState() != CircuitStateOpen {
		t.Errorf("State after failures = %v, want %v", breaker.GetState(), CircuitStateOpen)
	}

	// Wait for recovery timeout
	time.Sleep(2 * time.Second)

	// Try to execute - should move to half-open
	breaker.Execute(context.Background(), func() (interface{}, error) {
		return "success", nil
	})

	// Should be half-open or closed
	state := breaker.GetState()
	if state != CircuitStateHalfOpen && state != CircuitStateClosed {
		t.Errorf("State after recovery = %v, want %v or %v", state, CircuitStateHalfOpen, CircuitStateClosed)
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Execute some functions
	breaker.Execute(context.Background(), func() (interface{}, error) {
		return "success", nil
	})

	breaker.Execute(context.Background(), func() (interface{}, error) {
		return nil, errors.New("test error")
	})

	// Check stats
	stats := breaker.GetStats()
	if stats.TotalRequests != 2 {
		t.Errorf("GetStats() TotalRequests = %v, want 2", stats.TotalRequests)
	}

	if stats.SuccessfulRequests != 1 {
		t.Errorf("GetStats() SuccessfulRequests = %v, want 1", stats.SuccessfulRequests)
	}

	if stats.FailedRequests != 1 {
		t.Errorf("GetStats() FailedRequests = %v, want 1", stats.FailedRequests)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Execute some functions
	breaker.Execute(context.Background(), func() (interface{}, error) {
		return "success", nil
	})

	// Reset
	breaker.Reset()

	// Check stats are reset
	stats := breaker.GetStats()
	if stats.TotalRequests != 0 {
		t.Errorf("GetStats() after reset TotalRequests = %v, want 0", stats.TotalRequests)
	}

	// Check state is closed
	if breaker.GetState() != CircuitStateClosed {
		t.Errorf("GetState() after reset = %v, want %v", breaker.GetState(), CircuitStateClosed)
	}
}

func TestCircuitBreaker_StateMethods(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Initially closed
	if !breaker.IsClosed() {
		t.Error("IsClosed() should return true initially")
	}

	if breaker.IsOpen() {
		t.Error("IsOpen() should return false initially")
	}

	if breaker.IsHalfOpen() {
		t.Error("IsHalfOpen() should return false initially")
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Test concurrent access
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			breaker.Execute(context.Background(), func() (interface{}, error) {
				return "success", nil
			})
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Check stats
	stats := breaker.GetStats()
	if stats.TotalRequests != 10 {
		t.Errorf("GetStats() TotalRequests = %v, want 10", stats.TotalRequests)
	}
}

func TestCircuitBreaker_UpdateConfig(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Update config
	newConfig := config
	newConfig.MaxRequests = 20
	breaker.UpdateConfig(newConfig)

	// Check config is updated
	updatedConfig := breaker.GetConfig()
	if updatedConfig.MaxRequests != 20 {
		t.Errorf("UpdateConfig() MaxRequests = %v, want 20", updatedConfig.MaxRequests)
	}
}

func TestCircuitBreaker_ErrorTypes(t *testing.T) {
	config := CircuitBreakerConfig{
		Name:             "test-breaker",
		MaxRequests:      10,
		Timeout:          60 * time.Second,
		MaxFailures:      5,
		FailureThreshold: 0.5,
		SuccessThreshold: 0.8,
		RecoveryTimeout:  30 * time.Second,
		Logger:           logger.NewLogger(logger.LoggingConfig{Level: "info"}),
		Metrics:          metrics.NewMockMetricsCollector(),
	}

	breaker := NewCircuitBreaker(config)

	// Test circuit breaker error
	_, err := breaker.Execute(context.Background(), func() (interface{}, error) {
		return nil, errors.New("test error")
	})

	if err == nil {
		t.Error("Execute() should return error")
	}

	// Check error type
	if _, ok := err.(*CircuitBreakerError); ok {
		t.Error("Execute() should not return CircuitBreakerError for normal errors")
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitStateClosed, "closed"},
		{CircuitStateOpen, "open"},
		{CircuitStateHalfOpen, "half-open"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState.String() = %v, want %v", got, tt.want)
		}
	}
}
