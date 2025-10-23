package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// OTelTracer provides OpenTelemetry-based distributed tracing
type OTelTracer struct {
	config         OTelTracingConfig
	tracer         trace.Tracer
	tracerProvider *sdktrace.TracerProvider
	propagator     propagation.TextMapPropagator
	logger         common.Logger
	shutdownC      chan struct{}
}

// OTelTracingConfig contains OpenTelemetry tracing configuration
type OTelTracingConfig struct {
	ServiceName    string        `yaml:"service_name" default:"forge-service"`
	ServiceVersion string        `yaml:"service_version" default:"1.0.0"`
	Environment    string        `yaml:"environment" default:"development"`
	SamplingRate   float64       `yaml:"sampling_rate" default:"1.0"`
	MaxSpans       int           `yaml:"max_spans" default:"10000"`
	FlushInterval  time.Duration `yaml:"flush_interval" default:"5s"`

	// Exporters
	EnableJaeger   bool   `yaml:"enable_jaeger" default:"false"`
	JaegerEndpoint string `yaml:"jaeger_endpoint" default:"http://localhost:14268/api/traces"`
	EnableOTLP     bool   `yaml:"enable_otlp" default:"true"`
	OTLPEndpoint   string `yaml:"otlp_endpoint" default:"http://localhost:4318/v1/traces"`
	EnableConsole  bool   `yaml:"enable_console" default:"false"`

	// Propagation
	EnableB3  bool `yaml:"enable_b3" default:"true"`
	EnableW3C bool `yaml:"enable_w3c" default:"true"`

	Logger common.Logger `yaml:"-"`
}

// contextKey is a typed context key to avoid collisions
type contextKey string

const (
	spanContextKey contextKey = "forge.span"
	traceIDKey     contextKey = "forge.trace_id"
	spanIDKey      contextKey = "forge.span_id"
)

// NewOTelTracer creates a new OpenTelemetry tracer
func NewOTelTracer(config OTelTracingConfig) (*OTelTracer, error) {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	tracer := &OTelTracer{
		config:    config,
		logger:    config.Logger,
		shutdownC: make(chan struct{}),
	}

	// Create resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.ServiceVersion),
			semconv.DeploymentEnvironment(config.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporters
	exporters := make([]sdktrace.SpanExporter, 0)

	if config.EnableJaeger {
		jaegerExporter, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(config.JaegerEndpoint)))
		if err != nil {
			return nil, fmt.Errorf("failed to create Jaeger exporter: %w", err)
		}
		exporters = append(exporters, jaegerExporter)
	}

	if config.EnableOTLP {
		otlpExporter, err := otlptracehttp.New(context.Background(),
			otlptracehttp.WithEndpoint(config.OTLPEndpoint),
			otlptracehttp.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
		}
		exporters = append(exporters, otlpExporter)
	}

	if config.EnableConsole {
		// Console exporter would be added here
		// For now, we'll skip it as it's not in the standard library
	}

	if len(exporters) == 0 {
		return nil, fmt.Errorf("no exporters configured")
	}

	// Create batch span processor
	bsp := sdktrace.NewBatchSpanProcessor(exporters[0])
	if len(exporters) > 1 {
		// Use multi-exporter if available
		bsp = sdktrace.NewBatchSpanProcessor(exporters[0])
	}

	// Create tracer provider
	tracer.tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(config.SamplingRate)),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tracer.tracerProvider)

	// Create tracer
	tracer.tracer = tracer.tracerProvider.Tracer(
		config.ServiceName,
		trace.WithInstrumentationVersion(config.ServiceVersion),
	)

	// Set up propagation
	propagators := make([]propagation.TextMapPropagator, 0)
	if config.EnableW3C {
		propagators = append(propagators, propagation.TraceContext{}, propagation.Baggage{})
	}
	if config.EnableB3 {
		// B3 propagation would be added here if needed
	}

	if len(propagators) > 0 {
		tracer.propagator = propagation.NewCompositeTextMapPropagator(propagators...)
		otel.SetTextMapPropagator(tracer.propagator)
	}

	return tracer, nil
}

// StartSpan starts a new span
func (t *OTelTracer) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, span := t.tracer.Start(ctx, name, opts...)

	// Store span in context for compatibility
	ctx = context.WithValue(ctx, spanContextKey, span)
	ctx = context.WithValue(ctx, traceIDKey, span.SpanContext().TraceID().String())
	ctx = context.WithValue(ctx, spanIDKey, span.SpanContext().SpanID().String())

	return ctx, span
}

// EndSpan ends a span
func (t *OTelTracer) EndSpan(span trace.Span) {
	if span != nil {
		span.End()
	}
}

// AddEvent adds an event to a span
func (t *OTelTracer) AddEvent(span trace.Span, name string, attributes map[string]string) {
	if span == nil {
		return
	}

	attrs := make([]attribute.KeyValue, 0, len(attributes))
	for k, v := range attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttribute sets an attribute on a span
func (t *OTelTracer) SetAttribute(span trace.Span, key, value string) {
	if span == nil {
		return
	}

	span.SetAttributes(attribute.String(key, value))
}

// SetStatus sets the status of a span
func (t *OTelTracer) SetStatus(span trace.Span, code codes.Code, description string) {
	if span == nil {
		return
	}

	span.SetStatus(code, description)
}

// AddLink adds a link to another span
func (t *OTelTracer) AddLink(span trace.Span, traceID, spanID string, attributes map[string]string) {
	if span == nil {
		return
	}

	// Parse trace and span IDs
	tID, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		t.logger.Error("invalid trace ID", logger.String("trace_id", traceID), logger.String("error", err.Error()))
		return
	}

	sID, err := trace.SpanIDFromHex(spanID)
	if err != nil {
		t.logger.Error("invalid span ID", logger.String("span_id", spanID), logger.String("error", err.Error()))
		return
	}

	attrs := make([]attribute.KeyValue, 0, len(attributes))
	for k, v := range attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	link := trace.Link{
		SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: tID,
			SpanID:  sID,
		}),
		Attributes: attrs,
	}

	span.AddLink(link)
}

// GetSpanFromContext gets the current span from context
func (t *OTelTracer) GetSpanFromContext(ctx context.Context) trace.Span {
	span, ok := ctx.Value(spanContextKey).(trace.Span)
	if !ok {
		return trace.SpanFromContext(ctx)
	}
	return span
}

// GetTraceIDFromContext gets the trace ID from context
func (t *OTelTracer) GetTraceIDFromContext(ctx context.Context) string {
	if traceID, ok := ctx.Value(traceIDKey).(string); ok {
		return traceID
	}

	span := trace.SpanFromContext(ctx)
	return span.SpanContext().TraceID().String()
}

// GetSpanIDFromContext gets the span ID from context
func (t *OTelTracer) GetSpanIDFromContext(ctx context.Context) string {
	if spanID, ok := ctx.Value(spanIDKey).(string); ok {
		return spanID
	}

	span := trace.SpanFromContext(ctx)
	return span.SpanContext().SpanID().String()
}

// InjectTraceContext injects trace context into headers
func (t *OTelTracer) InjectTraceContext(ctx context.Context, headers map[string]string) {
	if t.propagator == nil {
		return
	}

	carrier := &headerCarrier{headers: headers}
	t.propagator.Inject(ctx, carrier)
}

// ExtractTraceContext extracts trace context from headers
func (t *OTelTracer) ExtractTraceContext(ctx context.Context, headers map[string]string) context.Context {
	if t.propagator == nil {
		return ctx
	}

	carrier := &headerCarrier{headers: headers}
	return t.propagator.Extract(ctx, carrier)
}

// GetStats returns tracing statistics
func (t *OTelTracer) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"service_name":    t.config.ServiceName,
		"service_version": t.config.ServiceVersion,
		"environment":     t.config.Environment,
		"sampling_rate":   t.config.SamplingRate,
		"exporters": map[string]bool{
			"jaeger":  t.config.EnableJaeger,
			"otlp":    t.config.EnableOTLP,
			"console": t.config.EnableConsole,
		},
	}
}

// Shutdown shuts down the tracer
func (t *OTelTracer) Shutdown(ctx context.Context) error {
	close(t.shutdownC)

	if t.tracerProvider != nil {
		return t.tracerProvider.Shutdown(ctx)
	}

	return nil
}

// headerCarrier implements the TextMapCarrier interface for HTTP headers
type headerCarrier struct {
	headers map[string]string
}

func (c *headerCarrier) Get(key string) string {
	return c.headers[key]
}

func (c *headerCarrier) Set(key, value string) {
	c.headers[key] = value
}

func (c *headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for k := range c.headers {
		keys = append(keys, k)
	}
	return keys
}

// Helper functions for span options

// WithSpanKind sets the span kind
func WithSpanKind(kind trace.SpanKind) trace.SpanStartOption {
	return trace.WithSpanKind(kind)
}

// WithAttributes sets span attributes
func WithAttributes(attributes map[string]string) trace.SpanStartOption {
	attrs := make([]attribute.KeyValue, 0, len(attributes))
	for k, v := range attributes {
		attrs = append(attrs, attribute.String(k, v))
	}
	return trace.WithAttributes(attrs...)
}

// WithResource sets span resource attributes
func WithResource(resource map[string]string) trace.SpanStartOption {
	attrs := make([]attribute.KeyValue, 0, len(resource))
	for k, v := range resource {
		attrs = append(attrs, attribute.String(k, v))
	}
	return trace.WithAttributes(attrs...)
}
