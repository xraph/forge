package observability

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// createTestConfig creates a test configuration with unique port
func createTestConfig(port string) ObservabilityConfig {
	config := CreateDefaultConfig()
	config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	config.Prometheus.ListenAddress = port
	return config
}

func TestNewObservability(t *testing.T) {
	config := createTestConfig(":9091")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	if obs == nil {
		t.Fatal("NewObservability() returned nil")
	}

	// Test shutdown
	ctx := context.Background()
	if err := obs.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestObservability_StartSpan(t *testing.T) {
	config := createTestConfig(":9092")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := obs.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Error("StartSpan() returned nil span")
	}

	// Test span context
	traceID := obs.tracer.GetTraceIDFromContext(ctx)
	if traceID == "" {
		t.Error("GetTraceIDFromContext() returned empty trace ID")
	}

	spanID := obs.tracer.GetSpanIDFromContext(ctx)
	if spanID == "" {
		t.Error("GetSpanIDFromContext() returned empty span ID")
	}
}

func TestObservability_RecordMetric(t *testing.T) {
	config := createTestConfig(":9093")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	// Test counter metric
	metric := &Metric{
		Name:        "test_counter",
		Type:        MetricTypeCounter,
		Value:       1.0,
		Unit:        "count",
		Labels:      map[string]string{"test": "true"},
		Description: "Test counter metric",
	}

	obs.RecordMetric(metric)

	// Test gauge metric
	gaugeMetric := &Metric{
		Name:        "test_gauge",
		Type:        MetricTypeGauge,
		Value:       42.0,
		Unit:        "units",
		Labels:      map[string]string{"test": "true"},
		Description: "Test gauge metric",
	}

	obs.RecordMetric(gaugeMetric)
}

func TestObservability_RecordError(t *testing.T) {
	config := createTestConfig(":9094")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := obs.StartSpan(ctx, "test-operation")
	defer obs.EndSpan(span)

	// Record error
	err = obs.RecordError(ctx, context.DeadlineExceeded, map[string]string{
		"operation": "test",
		"severity":  "warning",
	})
	if err != nil {
		t.Errorf("RecordError() error = %v", err)
	}
}

func TestObservability_ObserveRequest(t *testing.T) {
	config := createTestConfig(":9095")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	ctx := context.Background()

	// Test successful request
	err = obs.ObserveRequest(ctx, "test-request", func(ctx context.Context) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Errorf("ObserveRequest() error = %v", err)
	}

	// Test failed request
	err = obs.ObserveRequest(ctx, "test-request-fail", func(ctx context.Context) error {
		return context.DeadlineExceeded
	})
	if err == nil {
		t.Error("ObserveRequest() should have returned error")
	}
}

func TestObservability_WithSpan(t *testing.T) {
	config := createTestConfig(":9096")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	ctx := context.Background()

	// Test successful span
	err = obs.WithSpan(ctx, "test-span", func(ctx context.Context, span interface{}) error {
		if span == nil {
			t.Error("WithSpan() span should not be nil")
		}
		return nil
	})
	if err != nil {
		t.Errorf("WithSpan() error = %v", err)
	}

	// Test failed span
	err = obs.WithSpan(ctx, "test-span-fail", func(ctx context.Context, span interface{}) error {
		return context.DeadlineExceeded
	})
	if err == nil {
		t.Error("WithSpan() should have returned error")
	}
}

func TestObservability_WithMetrics(t *testing.T) {
	config := createTestConfig(":9097")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	// Test successful metrics
	err = obs.WithMetrics("test-operation", map[string]string{"test": "true"}, func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Errorf("WithMetrics() error = %v", err)
	}

	// Test failed metrics
	err = obs.WithMetrics("test-operation-fail", map[string]string{"test": "true"}, func() error {
		return context.DeadlineExceeded
	})
	if err == nil {
		t.Error("WithMetrics() should have returned error")
	}
}

func TestObservability_AddAlert(t *testing.T) {
	config := createTestConfig(":9098")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	// Test adding alert
	alert := &Alert{
		ID:          "test-alert",
		Name:        "Test Alert",
		Description: "A test alert",
		Severity:    AlertSeverityWarning,
		Condition:   "test_condition",
		Threshold:   50.0,
		Duration:    5 * time.Minute,
		Labels:      map[string]string{"test": "true"},
	}

	err = obs.AddAlert(alert)
	if err != nil {
		t.Errorf("AddAlert() error = %v", err)
	}
}

func TestObservability_GetHealthStatus(t *testing.T) {
	config := createTestConfig(":9099")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	// Test health status
	health := obs.GetHealthStatus()
	if health == nil {
		t.Error("GetHealthStatus() returned nil")
	}

	if health.Name == "" {
		t.Error("GetHealthStatus() returned empty name")
	}
}

func TestObservability_GetStats(t *testing.T) {
	config := createTestConfig(":9100")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	// Test stats
	stats := obs.GetStats()
	if stats == nil {
		t.Error("GetStats() returned nil")
	}

	// Check for expected components
	expectedComponents := []string{"monitor", "tracer", "prometheus"}
	for _, component := range expectedComponents {
		if _, exists := stats[component]; !exists {
			t.Errorf("GetStats() missing component: %s", component)
		}
	}
}

func TestObservability_GetPrometheusHandler(t *testing.T) {
	config := createTestConfig(":9101")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	// Test Prometheus handler
	handler := obs.GetPrometheusHandler()
	if handler == nil {
		t.Error("GetPrometheusHandler() returned nil")
	}
}

func TestObservability_RegisterAlertHandler(t *testing.T) {
	config := createTestConfig(":9102")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	// Test registering alert handler
	handler := &TestAlertHandler{}
	obs.RegisterAlertHandler(handler)

	// Test adding alert that should trigger handler
	alert := &Alert{
		ID:          "test-alert",
		Name:        "Test Alert",
		Description: "A test alert",
		Severity:    AlertSeverityCritical,
		Condition:   "test_condition",
		Threshold:   50.0,
		Duration:    5 * time.Minute,
		Labels:      map[string]string{"test": "true"},
	}

	err = obs.AddAlert(alert)
	if err != nil {
		t.Errorf("AddAlert() error = %v", err)
	}
}

func TestObservability_ConcurrentAccess(t *testing.T) {
	config := createTestConfig(":9103")

	obs, err := NewObservability(config)
	if err != nil {
		t.Fatalf("NewObservability() error = %v", err)
	}
	defer obs.Shutdown(context.Background())

	// Test concurrent access
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			ctx := context.Background()
			ctx, span := obs.StartSpan(ctx, "concurrent-operation")
			if span != nil {
				obs.EndSpan(span)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestCreateDefaultConfig(t *testing.T) {
	config := CreateDefaultConfig()
	if config.Tracer.ServiceName == "" {
		t.Error("CreateDefaultConfig() should set service name")
	}
	if config.Tracer.ServiceVersion == "" {
		t.Error("CreateDefaultConfig() should set service version")
	}
	if config.Tracer.Environment == "" {
		t.Error("CreateDefaultConfig() should set environment")
	}
}

// TestAlertHandler for testing
type TestAlertHandler struct{}

func (h *TestAlertHandler) HandleAlert(ctx context.Context, alert *Alert) error {
	return nil
}

func (h *TestAlertHandler) GetName() string {
	return "test"
}
