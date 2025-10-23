package observability

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
)

func TestNewOTelTracer(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	if tracer == nil {
		t.Fatal("NewOTelTracer() returned nil")
	}

	// Test shutdown
	ctx := context.Background()
	if err := tracer.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestOTelTracer_StartSpan(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Error("StartSpan() returned nil span")
	}

	// Test span context
	traceID := tracer.GetTraceIDFromContext(ctx)
	if traceID == "" {
		t.Error("GetTraceIDFromContext() returned empty trace ID")
	}

	spanID := tracer.GetSpanIDFromContext(ctx)
	if spanID == "" {
		t.Error("GetSpanIDFromContext() returned empty span ID")
	}
}

func TestOTelTracer_EndSpan(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// End span
	tracer.EndSpan(span)
}

func TestOTelTracer_AddEvent(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Add event
	attributes := map[string]string{
		"event.type": "test",
		"event.data": "test-data",
	}
	tracer.AddEvent(span, "test-event", attributes)
}

func TestOTelTracer_SetAttribute(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Set attributes
	tracer.SetAttribute(span, "test.key", "test-value")
	tracer.SetAttribute(span, "test.number", "123")
}

func TestOTelTracer_SetStatus(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Set status
	tracer.SetStatus(span, 1, "test error") // 1 = error code
}

func TestOTelTracer_AddLink(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Add link
	attributes := map[string]string{
		"link.type": "test",
	}
	tracer.AddLink(span, "12345678901234567890123456789012", "1234567890123456", attributes)
}

func TestOTelTracer_InjectTraceContext(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		EnableW3C:      true,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Inject trace context
	headers := make(map[string]string)
	tracer.InjectTraceContext(ctx, headers)

	// Check headers
	if headers["traceparent"] == "" {
		t.Error("InjectTraceContext() did not set traceparent")
	}
}

func TestOTelTracer_ExtractTraceContext(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		EnableW3C:      true,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	// Create headers with trace context
	headers := map[string]string{
		"traceparent": "00-12345678901234567890123456789012-1234567890123456-01",
	}

	// Extract trace context
	ctx := tracer.ExtractTraceContext(context.Background(), headers)

	// Check extracted values
	traceID := tracer.GetTraceIDFromContext(ctx)
	if traceID == "" {
		t.Error("ExtractTraceContext() did not extract trace ID")
	}
}

func TestOTelTracer_GetSpanFromContext(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Get span from context
	retrievedSpan := tracer.GetSpanFromContext(ctx)
	if retrievedSpan == nil {
		t.Error("GetSpanFromContext() returned nil span")
	}
}

func TestOTelTracer_GetStats(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	// Test stats
	stats := tracer.GetStats()
	if stats == nil {
		t.Error("GetStats() returned nil stats")
	}

	// Check for expected fields
	expectedFields := []string{"service_name", "service_version", "environment", "sampling_rate"}
	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("GetStats() missing field: %s", field)
		}
	}
}

func TestOTelTracer_ConcurrentAccess(t *testing.T) {
	config := OTelTracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableOTLP:     true,
		OTLPEndpoint:   "http://localhost:4318/v1/traces",
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer, err := NewOTelTracer(config)
	if err != nil {
		t.Fatalf("NewOTelTracer() error = %v", err)
	}
	defer tracer.Shutdown(context.Background())

	// Test concurrent span creation
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			ctx := context.Background()
			ctx, span := tracer.StartSpan(ctx, "concurrent-operation")
			if span != nil {
				tracer.EndSpan(span)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}
