package observability

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/v2/internal/logger"
)

func TestNewTracer(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)
	if tracer == nil {
		t.Error("NewTracer() returned nil")
	}

	if tracer.config.ServiceName != config.ServiceName {
		t.Error("Tracer service name not set correctly")
	}
}

func TestTracer_StartSpan(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start a span
	ctx, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Error("StartSpan() returned nil span")
	}

	if span.Name != "test-operation" {
		t.Errorf("StartSpan() span name = %v, want %v", span.Name, "test-operation")
	}

	if span.TraceID == "" {
		t.Error("StartSpan() returned empty trace ID")
	}

	if span.SpanID == "" {
		t.Error("StartSpan() returned empty span ID")
	}

	// Check context values
	traceID := tracer.GetTraceIDFromContext(ctx)
	if traceID == "" {
		t.Error("GetTraceIDFromContext() returned empty trace ID")
	}

	spanID := tracer.GetSpanIDFromContext(ctx)
	if spanID == "" {
		t.Error("GetSpanIDFromContext() returned empty span ID")
	}
}

func TestTracer_EndSpan(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start and end a span
	_, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	startTime := span.StartTime
	time.Sleep(1 * time.Millisecond) // Ensure some duration

	tracer.EndSpan(span)

	if span.EndTime.IsZero() {
		t.Error("EndSpan() did not set end time")
	}

	if span.Duration <= 0 {
		t.Error("EndSpan() did not set duration")
	}

	if span.EndTime.Before(startTime) {
		t.Error("EndSpan() end time is before start time")
	}
}

func TestTracer_AddEvent(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start a span
	_, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Add an event
	attributes := map[string]string{
		"event.type": "test",
		"event.data": "test-data",
	}
	tracer.AddEvent(span, "test-event", attributes)

	if len(span.Events) != 1 {
		t.Errorf("AddEvent() added %d events, want 1", len(span.Events))
	}

	event := span.Events[0]
	if event.Name != "test-event" {
		t.Errorf("AddEvent() event name = %v, want %v", event.Name, "test-event")
	}

	if event.Attributes["event.type"] != "test" {
		t.Errorf("AddEvent() event type = %v, want %v", event.Attributes["event.type"], "test")
	}
}

func TestTracer_SetAttribute(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start a span
	_, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Set attributes
	tracer.SetAttribute(span, "test.key", "test-value")
	tracer.SetAttribute(span, "test.number", "123")

	if span.Attributes["test.key"] != "test-value" {
		t.Errorf("SetAttribute() test.key = %v, want %v", span.Attributes["test.key"], "test-value")
	}

	if span.Attributes["test.number"] != "123" {
		t.Errorf("SetAttribute() test.number = %v, want %v", span.Attributes["test.number"], "123")
	}
}

func TestTracer_SetStatus(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start a span
	_, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Set status
	tracer.SetStatus(span, SpanStatusOK)

	if span.Status != SpanStatusOK {
		t.Errorf("SetStatus() status = %v, want %v", span.Status, SpanStatusOK)
	}
}

func TestTracer_AddLink(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start a span
	_, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Add a link
	attributes := map[string]string{
		"link.type": "test",
	}
	tracer.AddLink(span, "trace-123", "span-456", attributes)

	if len(span.Links) != 1 {
		t.Errorf("AddLink() added %d links, want 1", len(span.Links))
	}

	link := span.Links[0]
	if link.TraceID != "trace-123" {
		t.Errorf("AddLink() trace ID = %v, want %v", link.TraceID, "trace-123")
	}

	if link.SpanID != "span-456" {
		t.Errorf("AddLink() span ID = %v, want %v", link.SpanID, "span-456")
	}
}

func TestTracer_InjectTraceContext(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableB3:       true,
		EnableW3C:      true,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start a span
	ctx, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Inject trace context
	headers := make(map[string]string)
	tracer.InjectTraceContext(ctx, headers)

	// Check B3 headers
	if headers["X-B3-TraceId"] == "" {
		t.Error("InjectTraceContext() did not set X-B3-TraceId")
	}

	if headers["X-B3-SpanId"] == "" {
		t.Error("InjectTraceContext() did not set X-B3-SpanId")
	}

	if headers["X-B3-Sampled"] != "1" {
		t.Error("InjectTraceContext() did not set X-B3-Sampled")
	}

	// Check W3C headers
	if headers["traceparent"] == "" {
		t.Error("InjectTraceContext() did not set traceparent")
	}
}

func TestTracer_ExtractTraceContext(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		EnableB3:       true,
		EnableW3C:      true,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Create headers with trace context
	headers := map[string]string{
		"X-B3-TraceId": "trace-123",
		"X-B3-SpanId":  "span-456",
		"traceparent":  "00-trace-123-span-456-01",
	}

	// Extract trace context
	ctx := tracer.ExtractTraceContext(context.Background(), headers)

	// Check extracted values
	traceID := tracer.GetTraceIDFromContext(ctx)
	if traceID != "trace-123" {
		t.Errorf("ExtractTraceContext() trace ID = %v, want %v", traceID, "trace-123")
	}

	spanID := tracer.GetSpanIDFromContext(ctx)
	if spanID != "span-456" {
		t.Errorf("ExtractTraceContext() span ID = %v, want %v", spanID, "span-456")
	}
}

func TestTracer_GetSpanFromContext(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start a span
	ctx, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Get span from context
	retrievedSpan := tracer.GetSpanFromContext(ctx)
	if retrievedSpan == nil {
		t.Error("GetSpanFromContext() returned nil span")
	}

	if retrievedSpan != span {
		t.Error("GetSpanFromContext() returned different span")
	}
}

func TestTracer_GetStats(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Start a span
	_, span := tracer.StartSpan(context.Background(), "test-operation")
	if span == nil {
		t.Fatalf("StartSpan() returned nil span")
	}

	// Get stats
	stats := tracer.GetStats()
	if stats == nil {
		t.Error("GetStats() returned nil stats")
	}

	if stats["service_name"] != config.ServiceName {
		t.Errorf("GetStats() service_name = %v, want %v", stats["service_name"], config.ServiceName)
	}

	if stats["service_version"] != config.ServiceVersion {
		t.Errorf("GetStats() service_version = %v, want %v", stats["service_version"], config.ServiceVersion)
	}

	if stats["environment"] != config.Environment {
		t.Errorf("GetStats() environment = %v, want %v", stats["environment"], config.Environment)
	}
}

func TestTracer_ConcurrentAccess(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Test concurrent span creation
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			_, span := tracer.StartSpan(context.Background(), "test-operation")
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

func TestTracer_Shutdown(t *testing.T) {
	config := TracingConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		SamplingRate:   1.0,
		MaxSpans:       1000,
		FlushInterval:  5 * time.Second,
		Logger:         logger.NewLogger(logger.LoggingConfig{Level: "info"}),
	}

	tracer := NewTracer(config)

	// Shutdown the tracer
	ctx := context.Background()
	err := tracer.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}
