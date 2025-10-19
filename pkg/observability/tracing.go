package observability

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Tracer provides distributed tracing functionality
type Tracer struct {
	config    TracingConfig
	spans     map[string]*Span
	mu        sync.RWMutex
	logger    common.Logger
	exporters []SpanExporter
}

// TracingConfig contains tracing configuration
type TracingConfig struct {
	ServiceName    string        `yaml:"service_name" default:"forge-service"`
	ServiceVersion string        `yaml:"service_version" default:"1.0.0"`
	Environment    string        `yaml:"environment" default:"development"`
	SamplingRate   float64       `yaml:"sampling_rate" default:"1.0"`
	MaxSpans       int           `yaml:"max_spans" default:"10000"`
	FlushInterval  time.Duration `yaml:"flush_interval" default:"5s"`
	EnableB3       bool          `yaml:"enable_b3" default:"true"`
	EnableW3C      bool          `yaml:"enable_w3c" default:"true"`
	EnableJaeger   bool          `yaml:"enable_jaeger" default:"false"`
	JaegerEndpoint string        `yaml:"jaeger_endpoint" default:"http://localhost:14268/api/traces"`
	EnableZipkin   bool          `yaml:"enable_zipkin" default:"false"`
	ZipkinEndpoint string        `yaml:"zipkin_endpoint" default:"http://localhost:9411/api/v2/spans"`
	Logger         common.Logger `yaml:"-"`
}

// Span represents a tracing span
type Span struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	Kind         SpanKind          `json:"kind"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time,omitempty"`
	Duration     time.Duration     `json:"duration,omitempty"`
	Status       SpanStatus        `json:"status"`
	Attributes   map[string]string `json:"attributes"`
	Events       []SpanEvent       `json:"events"`
	Links        []SpanLink        `json:"links"`
	Resource     map[string]string `json:"resource"`
	mu           sync.RWMutex
}

// SpanKind represents the kind of span
type SpanKind int

const (
	SpanKindUnspecified SpanKind = iota
	SpanKindInternal
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

// SpanStatus represents the status of a span
type SpanStatus int

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

// SpanEvent represents an event within a span
type SpanEvent struct {
	Name       string            `json:"name"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}

// SpanLink represents a link to another span
type SpanLink struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	Attributes map[string]string `json:"attributes"`
}

// SpanExporter interface for exporting spans
type SpanExporter interface {
	ExportSpans(ctx context.Context, spans []*Span) error
	Shutdown(ctx context.Context) error
}

// TraceContext represents trace context information
type TraceContext struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
	Sampled bool   `json:"sampled"`
	Flags   string `json:"flags"`
}

// NewTracer creates a new tracer
func NewTracer(config TracingConfig) *Tracer {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	tracer := &Tracer{
		config:    config,
		spans:     make(map[string]*Span),
		logger:    config.Logger,
		exporters: make([]SpanExporter, 0),
	}

	// Initialize exporters
	tracer.initializeExporters()

	// Start flush goroutine
	go tracer.startFlush()

	return tracer
}

// StartSpan starts a new span
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span) {
	// Check sampling
	if !t.shouldSample() {
		return ctx, nil
	}

	// Generate trace and span IDs
	traceID := t.generateTraceID()
	spanID := t.generateSpanID()

	// Get parent span from context
	parentSpan := t.getParentSpan(ctx)

	// Create new span
	span := &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: "",
		Name:         name,
		Kind:         SpanKindInternal,
		StartTime:    time.Now(),
		Status:       SpanStatusUnset,
		Attributes:   make(map[string]string),
		Events:       make([]SpanEvent, 0),
		Links:        make([]SpanLink, 0),
		Resource:     make(map[string]string),
	}

	// Set parent span ID if parent exists
	if parentSpan != nil {
		span.ParentSpanID = parentSpan.SpanID
		span.TraceID = parentSpan.TraceID // Use parent's trace ID
	}

	// Apply options
	for _, opt := range opts {
		opt(span)
	}

	// Store span
	t.mu.Lock()
	t.spans[spanID] = span
	t.mu.Unlock()

	// Add span to context
	ctx = context.WithValue(ctx, "span", span)
	ctx = context.WithValue(ctx, "trace_id", traceID)
	ctx = context.WithValue(ctx, "span_id", spanID)

	return ctx, span
}

// EndSpan ends a span
func (t *Tracer) EndSpan(span *Span) {
	if span == nil {
		return
	}

	span.mu.Lock()
	span.EndTime = time.Now()
	span.Duration = span.EndTime.Sub(span.StartTime)
	span.mu.Unlock()

	// Remove from active spans
	t.mu.Lock()
	delete(t.spans, span.SpanID)
	t.mu.Unlock()
}

// AddEvent adds an event to a span
func (t *Tracer) AddEvent(span *Span, name string, attributes map[string]string) {
	if span == nil {
		return
	}

	span.mu.Lock()
	event := SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attributes,
	}
	span.Events = append(span.Events, event)
	span.mu.Unlock()
}

// SetAttribute sets an attribute on a span
func (t *Tracer) SetAttribute(span *Span, key, value string) {
	if span == nil {
		return
	}

	span.mu.Lock()
	span.Attributes[key] = value
	span.mu.Unlock()
}

// SetStatus sets the status of a span
func (t *Tracer) SetStatus(span *Span, status SpanStatus) {
	if span == nil {
		return
	}

	span.mu.Lock()
	span.Status = status
	span.mu.Unlock()
}

// AddLink adds a link to another span
func (t *Tracer) AddLink(span *Span, traceID, spanID string, attributes map[string]string) {
	if span == nil {
		return
	}

	span.mu.Lock()
	link := SpanLink{
		TraceID:    traceID,
		SpanID:     spanID,
		Attributes: attributes,
	}
	span.Links = append(span.Links, link)
	span.mu.Unlock()
}

// GetSpanFromContext gets the current span from context
func (t *Tracer) GetSpanFromContext(ctx context.Context) *Span {
	span, ok := ctx.Value("span").(*Span)
	if !ok {
		return nil
	}
	return span
}

// GetTraceIDFromContext gets the trace ID from context
func (t *Tracer) GetTraceIDFromContext(ctx context.Context) string {
	traceID, ok := ctx.Value("trace_id").(string)
	if !ok {
		return ""
	}
	return traceID
}

// GetSpanIDFromContext gets the span ID from context
func (t *Tracer) GetSpanIDFromContext(ctx context.Context) string {
	spanID, ok := ctx.Value("span_id").(string)
	if !ok {
		return ""
	}
	return spanID
}

// InjectTraceContext injects trace context into headers
func (t *Tracer) InjectTraceContext(ctx context.Context, headers map[string]string) {
	traceID := t.GetTraceIDFromContext(ctx)
	spanID := t.GetSpanIDFromContext(ctx)

	if traceID != "" && spanID != "" {
		// B3 format
		if t.config.EnableB3 {
			headers["X-B3-TraceId"] = traceID
			headers["X-B3-SpanId"] = spanID
			headers["X-B3-Sampled"] = "1"
		}

		// W3C format
		if t.config.EnableW3C {
			headers["traceparent"] = fmt.Sprintf("00-%s-%s-01", traceID, spanID)
		}
	}
}

// ExtractTraceContext extracts trace context from headers
func (t *Tracer) ExtractTraceContext(ctx context.Context, headers map[string]string) context.Context {
	var traceID, spanID string

	// Extract from B3 format
	if b3TraceID, exists := headers["X-B3-TraceId"]; exists {
		traceID = b3TraceID
	}
	if b3SpanID, exists := headers["X-B3-SpanId"]; exists {
		spanID = b3SpanID
	}

	// Extract from W3C format
	if traceparent, exists := headers["traceparent"]; exists {
		// Parse traceparent header: 00-{trace-id}-{span-id}-{trace-flags}
		parts := splitTraceparent(traceparent)
		if len(parts) >= 4 {
			traceID = parts[1] + "-" + parts[2] // trace-123
			spanID = parts[3] + "-" + parts[4]  // span-456
		}
	}

	if traceID != "" && spanID != "" {
		ctx = context.WithValue(ctx, "trace_id", traceID)
		ctx = context.WithValue(ctx, "span_id", spanID)
	}

	return ctx
}

// shouldSample determines if a span should be sampled
func (t *Tracer) shouldSample() bool {
	// Simple sampling based on rate
	// In a real implementation, this would be more sophisticated
	return true // For now, always sample
}

// generateTraceID generates a new trace ID
func (t *Tracer) generateTraceID() string {
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}

// generateSpanID generates a new span ID
func (t *Tracer) generateSpanID() string {
	return fmt.Sprintf("%08x", time.Now().UnixNano())
}

// getParentSpan gets the parent span from context
func (t *Tracer) getParentSpan(ctx context.Context) *Span {
	span, ok := ctx.Value("span").(*Span)
	if !ok {
		return nil
	}
	return span
}

// initializeExporters initializes span exporters
func (t *Tracer) initializeExporters() {
	// Initialize Jaeger exporter if enabled
	if t.config.EnableJaeger {
		exporter := NewJaegerExporter(t.config.JaegerEndpoint)
		t.exporters = append(t.exporters, exporter)
	}

	// Initialize Zipkin exporter if enabled
	if t.config.EnableZipkin {
		exporter := NewZipkinExporter(t.config.ZipkinEndpoint)
		t.exporters = append(t.exporters, exporter)
	}
}

// startFlush starts the flush goroutine
func (t *Tracer) startFlush() {
	ticker := time.NewTicker(t.config.FlushInterval)
	defer ticker.Stop()

	for range ticker.C {
		t.flushSpans()
	}
}

// flushSpans flushes completed spans to exporters
func (t *Tracer) flushSpans() {
	t.mu.RLock()
	spans := make([]*Span, 0, len(t.spans))
	for _, span := range t.spans {
		spans = append(spans, span)
	}
	t.mu.RUnlock()

	if len(spans) == 0 {
		return
	}

	// Export spans to all exporters
	for _, exporter := range t.exporters {
		if err := exporter.ExportSpans(context.Background(), spans); err != nil {
			t.logger.Error("failed to export spans", logger.String("error", err.Error()))
		}
	}
}

// GetStats returns tracing statistics
func (t *Tracer) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]interface{}{
		"active_spans":    len(t.spans),
		"service_name":    t.config.ServiceName,
		"service_version": t.config.ServiceVersion,
		"environment":     t.config.Environment,
		"sampling_rate":   t.config.SamplingRate,
		"exporters":       len(t.exporters),
	}
}

// Shutdown shuts down the tracer
func (t *Tracer) Shutdown(ctx context.Context) error {
	// Flush remaining spans
	t.flushSpans()

	// Shutdown exporters
	for _, exporter := range t.exporters {
		if err := exporter.Shutdown(ctx); err != nil {
			t.logger.Error("failed to shutdown exporter", logger.String("error", err.Error()))
		}
	}

	return nil
}

// SpanOption represents a span option
type SpanOption func(*Span)

// Helper functions

// splitTraceparent splits a traceparent header
func splitTraceparent(traceparent string) []string {
	// Parse traceparent header: 00-{trace-id}-{span-id}-{trace-flags}
	return strings.Split(traceparent, "-")
}

// NewJaegerExporter creates a new Jaeger exporter
func NewJaegerExporter(endpoint string) SpanExporter {
	return &JaegerExporter{endpoint: endpoint}
}

// NewZipkinExporter creates a new Zipkin exporter
func NewZipkinExporter(endpoint string) SpanExporter {
	return &ZipkinExporter{endpoint: endpoint}
}

// JaegerExporter implements SpanExporter for Jaeger
type JaegerExporter struct {
	endpoint string
}

func (e *JaegerExporter) ExportSpans(ctx context.Context, spans []*Span) error {
	// In a real implementation, this would send spans to Jaeger
	return nil
}

func (e *JaegerExporter) Shutdown(ctx context.Context) error {
	return nil
}

// ZipkinExporter implements SpanExporter for Zipkin
type ZipkinExporter struct {
	endpoint string
}

func (e *ZipkinExporter) ExportSpans(ctx context.Context, spans []*Span) error {
	// In a real implementation, this would send spans to Zipkin
	return nil
}

func (e *ZipkinExporter) Shutdown(ctx context.Context) error {
	return nil
}
