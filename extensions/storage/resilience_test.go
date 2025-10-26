package storage

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics"
)

// Mock storage for testing
type mockStorage struct {
	failCount    int
	callCount    int
	shouldFail   bool
	slowResponse time.Duration
}

func (m *mockStorage) Upload(ctx context.Context, key string, data io.Reader, opts ...UploadOption) error {
	m.callCount++
	if m.slowResponse > 0 {
		select {
		case <-time.After(m.slowResponse):
			// Continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if m.shouldFail {
		return errors.New("mock upload error")
	}
	return nil
}

func (m *mockStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	m.callCount++
	if m.shouldFail {
		return nil, errors.New("mock download error")
	}
	return nil, nil
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	m.callCount++
	if m.shouldFail {
		return errors.New("mock delete error")
	}
	return nil
}

func (m *mockStorage) List(ctx context.Context, prefix string, opts ...ListOption) ([]Object, error) {
	m.callCount++
	if m.shouldFail {
		return nil, errors.New("mock list error")
	}
	return []Object{}, nil
}

func (m *mockStorage) Metadata(ctx context.Context, key string) (*ObjectMetadata, error) {
	m.callCount++
	if m.shouldFail {
		return nil, errors.New("mock metadata error")
	}
	return &ObjectMetadata{}, nil
}

func (m *mockStorage) Exists(ctx context.Context, key string) (bool, error) {
	m.callCount++
	if m.shouldFail {
		return false, errors.New("mock exists error")
	}
	return true, nil
}

func (m *mockStorage) Copy(ctx context.Context, srcKey, dstKey string) error {
	m.callCount++
	if m.shouldFail {
		return errors.New("mock copy error")
	}
	return nil
}

func (m *mockStorage) Move(ctx context.Context, srcKey, dstKey string) error {
	m.callCount++
	if m.shouldFail {
		return errors.New("mock move error")
	}
	return nil
}

func (m *mockStorage) PresignUpload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	m.callCount++
	if m.shouldFail {
		return "", errors.New("mock presign upload error")
	}
	return "https://example.com/upload", nil
}

func (m *mockStorage) PresignDownload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	m.callCount++
	if m.shouldFail {
		return "", errors.New("mock presign download error")
	}
	return "https://example.com/download", nil
}

// Helper functions to create test dependencies
func newTestLogger() forge.Logger {
	return logger.NewTestLogger()
}

func newTestMetrics() forge.Metrics {
	return metrics.NewMockMetricsCollector()
}

func TestCircuitBreaker(t *testing.T) {
	config := DefaultResilienceConfig()
	config.CircuitBreakerThreshold = 3
	config.CircuitBreakerTimeout = 100 * time.Millisecond

	testLogger := newTestLogger()
	testMetrics := newTestMetrics()

	cb := NewCircuitBreaker(config, testLogger, testMetrics)

	// Test: Initial state should be closed
	if cb.GetState() != CircuitClosed {
		t.Errorf("Expected circuit breaker to be closed initially, got %v", cb.GetState())
	}

	// Test: Should open after threshold failures
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.GetState() != CircuitOpen {
		t.Errorf("Expected circuit breaker to be open after failures, got %v", cb.GetState())
	}

	// Test: Should not allow attempts when open
	if cb.CanAttempt() {
		t.Error("Circuit breaker should not allow attempts when open")
	}

	// Test: Should transition to half-open after timeout
	time.Sleep(150 * time.Millisecond)

	if !cb.CanAttempt() {
		t.Error("Circuit breaker should allow attempts after timeout")
	}

	// Test: Successful attempt should close circuit
	cb.RecordSuccess()

	if cb.GetState() != CircuitClosed {
		t.Errorf("Expected circuit breaker to be closed after success, got %v", cb.GetState())
	}
}

func TestRateLimiter(t *testing.T) {
	config := DefaultResilienceConfig()
	config.RateLimitPerSec = 10
	config.RateLimitBurst = 5

	testMetrics := newTestMetrics()

	rl := NewRateLimiter(config, testMetrics)

	// Test: Should allow up to burst
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Errorf("Rate limiter should allow request %d", i+1)
		}
	}

	// Test: Should reject after burst
	if rl.Allow() {
		t.Error("Rate limiter should reject requests after burst")
	}

	// Test: Should refill over time
	time.Sleep(150 * time.Millisecond)

	if !rl.Allow() {
		t.Error("Rate limiter should allow requests after refill")
	}
}

func TestResilientStorage_Success(t *testing.T) {
	config := DefaultResilienceConfig()
	config.MaxRetries = 3
	config.InitialBackoff = 10 * time.Millisecond

	testLogger := newTestLogger()
	testMetrics := newTestMetrics()

	backend := &mockStorage{}
	rs := NewResilientStorage(backend, config, testLogger, testMetrics)

	ctx := context.Background()

	// Test: Successful upload
	err := rs.Upload(ctx, "test.txt", nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if backend.callCount != 1 {
		t.Errorf("Expected 1 call, got %d", backend.callCount)
	}
}

func TestResilientStorage_Retry(t *testing.T) {
	config := DefaultResilienceConfig()
	config.MaxRetries = 3
	config.InitialBackoff = 10 * time.Millisecond
	config.CircuitBreakerEnabled = false // Disable circuit breaker for this test

	testLogger := newTestLogger()
	testMetrics := newTestMetrics()

	backend := &mockStorage{shouldFail: true}
	rs := NewResilientStorage(backend, config, testLogger, testMetrics)

	ctx := context.Background()

	// Test: Should retry on failure
	err := rs.Upload(ctx, "test.txt", nil)
	if err == nil {
		t.Error("Expected error after retries")
	}

	// Should have called 4 times (initial + 3 retries)
	if backend.callCount != 4 {
		t.Errorf("Expected 4 calls (1 + 3 retries), got %d", backend.callCount)
	}
}

func TestResilientStorage_CircuitBreaker(t *testing.T) {
	config := DefaultResilienceConfig()
	config.MaxRetries = 0
	config.CircuitBreakerThreshold = 2
	config.CircuitBreakerEnabled = true

	testLogger := newTestLogger()
	testMetrics := newTestMetrics()

	backend := &mockStorage{shouldFail: true}
	rs := NewResilientStorage(backend, config, testLogger, testMetrics)

	ctx := context.Background()

	// Test: First two failures should go through
	rs.Upload(ctx, "test1.txt", nil)
	rs.Upload(ctx, "test2.txt", nil)

	// Test: Third attempt should be rejected by circuit breaker
	err := rs.Upload(ctx, "test3.txt", nil)
	if !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Errorf("Expected circuit breaker open error, got %v", err)
	}
}

func TestResilientStorage_NonRetryableError(t *testing.T) {
	config := DefaultResilienceConfig()
	config.MaxRetries = 3
	config.InitialBackoff = 10 * time.Millisecond
	config.CircuitBreakerEnabled = false

	testLogger := newTestLogger()
	testMetrics := newTestMetrics()

	// Mock that returns non-retryable error
	backend := &mockStorage{}
	rs := NewResilientStorage(backend, config, testLogger, testMetrics)

	ctx := context.Background()

	// Test: Non-retryable errors should not be retried
	err := rs.Delete(ctx, "../invalid")
	if err == nil {
		t.Error("Expected error for invalid key")
	}

	// Should only call once (no retries for validation errors)
	if backend.callCount > 0 {
		t.Errorf("Expected 0 calls for validation error, got %d", backend.callCount)
	}
}

func TestResilientStorage_Timeout(t *testing.T) {
	config := DefaultResilienceConfig()
	config.OperationTimeout = 50 * time.Millisecond
	config.MaxRetries = 0

	testLogger := newTestLogger()
	testMetrics := newTestMetrics()

	backend := &mockStorage{slowResponse: 100 * time.Millisecond}
	rs := NewResilientStorage(backend, config, testLogger, testMetrics)

	ctx := context.Background()

	// Test: Should timeout
	err := rs.Upload(ctx, "test.txt", nil)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func BenchmarkResilientStorage_Upload(b *testing.B) {
	config := DefaultResilienceConfig()
	testLogger := newTestLogger()
	testMetrics := newTestMetrics()

	backend := &mockStorage{}
	rs := NewResilientStorage(backend, config, testLogger, testMetrics)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rs.Upload(ctx, "test.txt", nil)
	}
}
