package observability

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// tracer implements the Tracer interface using OpenTelemetry
type tracer struct {
	config        TracingConfig
	provider      *trace.TracerProvider
	tracer        oteltrace.Tracer
	propagator    propagation.TextMapPropagator
	shutdownFuncs []func(context.Context) error
}

// span implements the Span interface using OpenTelemetry
type span struct {
	span oteltrace.Span
	ctx  context.Context
}

// NewTracer creates a new tracer instance
func NewTracer(config TracingConfig) (Tracer, error) {
	if !config.Enabled {
		return &noopTracer{}, nil
	}

	t := &tracer{
		config:        config,
		shutdownFuncs: make([]func(context.Context) error, 0),
	}

	if err := t.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize tracer: %w", err)
	}

	return t, nil
}

// initialize sets up the OpenTelemetry tracer provider
func (t *tracer) initialize() error {
	// Create resource
	res, err := t.createResource()
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporters
	exporters, err := t.createExporters()
	if err != nil {
		return fmt.Errorf("failed to create exporters: %w", err)
	}

	// Create sampler
	sampler := t.createSampler()

	// Create trace provider
	opts := []trace.TracerProviderOption{
		trace.WithResource(res),
		trace.WithSampler(sampler),
	}

	// Add span processors with exporters
	for _, exporter := range exporters {
		processor := trace.NewBatchSpanProcessor(exporter, t.createBatchProcessorOptions()...)
		opts = append(opts, trace.WithSpanProcessor(processor))
	}

	t.provider = trace.NewTracerProvider(opts...)
	t.tracer = t.provider.Tracer(
		t.config.ServiceName,
		oteltrace.WithInstrumentationVersion(t.config.ServiceVersion),
	)

	// Set up propagation
	t.propagator = propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	return nil
}

// createResource creates an OpenTelemetry resource
func (t *tracer) createResource() (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(t.config.ServiceName),
		semconv.ServiceVersion(t.config.ServiceVersion),
		semconv.DeploymentEnvironment(t.config.Environment),
	}

	// Add custom resource attributes
	for key, value := range t.config.ResourceAttributes {
		attrs = append(attrs, attribute.String(key, value))
	}

	return resource.NewWithAttributes(
		semconv.SchemaURL,
		attrs...,
	), nil
}

// createExporters creates trace exporters based on configuration
func (t *tracer) createExporters() ([]trace.SpanExporter, error) {
	exporters := make([]trace.SpanExporter, 0, len(t.config.Exporters))

	for _, exporterConfig := range t.config.Exporters {
		exporter, err := t.createExporter(exporterConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s exporter: %w", exporterConfig.Type, err)
		}
		exporters = append(exporters, exporter)
	}

	// If no exporters configured, use stdout by default
	if len(exporters) == 0 {
		exporter, err := stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create stdout exporter: %w", err)
		}
		exporters = append(exporters, exporter)
	}

	return exporters, nil
}

// createExporter creates a specific trace exporter
func (t *tracer) createExporter(config ExporterConfig) (trace.SpanExporter, error) {
	switch config.Type {
	case "jaeger":
		return t.createJaegerExporter(config)
	case "otlp":
		return t.createOTLPExporter(config)
	case "zipkin":
		return t.createZipkinExporter(config)
	case "stdout":
		return t.createStdoutExporter(config)
	default:
		return nil, fmt.Errorf("unsupported exporter type: %s", config.Type)
	}
}

// createJaegerExporter creates a Jaeger exporter
func (t *tracer) createJaegerExporter(config ExporterConfig) (trace.SpanExporter, error) {
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("http://%s:%d/api/traces", config.AgentHost, config.AgentPort)
	}

	exporter, err := jaeger.New(
		jaeger.WithCollectorEndpoint(
			jaeger.WithEndpoint(endpoint),
		),
	)
	if err != nil {
		return nil, err
	}

	t.shutdownFuncs = append(t.shutdownFuncs, exporter.Shutdown)
	return exporter, nil
}

// createOTLPExporter creates an OTLP exporter
func (t *tracer) createOTLPExporter(config ExporterConfig) (trace.SpanExporter, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("OTLP endpoint is required")
	}

	// Check if it's HTTP or gRPC based on endpoint
	if isHTTPEndpoint(config.Endpoint) {
		return t.createOTLPHTTPExporter(config)
	}
	return t.createOTLPGRPCExporter(config)
}

// createOTLPHTTPExporter creates an OTLP HTTP exporter
func (t *tracer) createOTLPHTTPExporter(config ExporterConfig) (trace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(config.Endpoint),
	}

	if config.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	if len(config.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(config.Headers))
	}

	if config.Compression != "" {
		switch config.Compression {
		case "gzip":
			opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
		}
	}

	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}

	t.shutdownFuncs = append(t.shutdownFuncs, exporter.Shutdown)
	return exporter, nil
}

// createOTLPGRPCExporter creates an OTLP gRPC exporter
func (t *tracer) createOTLPGRPCExporter(config ExporterConfig) (trace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(config.Endpoint),
	}

	if config.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	if len(config.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(config.Headers))
	}

	if config.Compression != "" {
		switch config.Compression {
		case "gzip":
			opts = append(opts, otlptracegrpc.WithCompressor(config.Compression))
		}
	}

	exporter, err := otlptracegrpc.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}

	t.shutdownFuncs = append(t.shutdownFuncs, exporter.Shutdown)
	return exporter, nil
}

// createZipkinExporter creates a Zipkin exporter
func (t *tracer) createZipkinExporter(config ExporterConfig) (trace.SpanExporter, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("Zipkin endpoint is required")
	}

	exporter, err := zipkin.New(config.Endpoint)
	if err != nil {
		return nil, err
	}

	t.shutdownFuncs = append(t.shutdownFuncs, exporter.Shutdown)
	return exporter, nil
}

// createStdoutExporter creates a stdout exporter
func (t *tracer) createStdoutExporter(config ExporterConfig) (trace.SpanExporter, error) {
	opts := []stdouttrace.Option{
		stdouttrace.WithPrettyPrint(),
	}

	if config.FilePath != "" {
		// Write to file instead of stdout
		opts = append(opts, stdouttrace.WithWriter(createFileWriter(config.FilePath)))
	}

	exporter, err := stdouttrace.New(opts...)
	if err != nil {
		return nil, err
	}

	t.shutdownFuncs = append(t.shutdownFuncs, exporter.Shutdown)
	return exporter, nil
}

// createSampler creates a trace sampler
func (t *tracer) createSampler() trace.Sampler {
	if t.config.NeverSample {
		return trace.NeverSample()
	}

	if t.config.AlwaysSample {
		return trace.AlwaysSample()
	}

	if t.config.SampleRate > 0 {
		return trace.TraceIDRatioBased(t.config.SampleRate)
	}

	// Default to 10% sampling
	return trace.TraceIDRatioBased(0.1)
}

// createBatchProcessorOptions creates batch processor options
func (t *tracer) createBatchProcessorOptions() []trace.BatchSpanProcessorOption {
	opts := []trace.BatchSpanProcessorOption{}

	if t.config.BatchTimeout > 0 {
		opts = append(opts, trace.WithBatchTimeout(t.config.BatchTimeout))
	}

	if t.config.ExportTimeout > 0 {
		opts = append(opts, trace.WithExportTimeout(t.config.ExportTimeout))
	}

	if t.config.MaxExportBatchSize > 0 {
		opts = append(opts, trace.WithMaxExportBatchSize(t.config.MaxExportBatchSize))
	}

	if t.config.MaxQueueSize > 0 {
		opts = append(opts, trace.WithMaxQueueSize(t.config.MaxQueueSize))
	}

	return opts
}

// StartSpan starts a new span
func (t *tracer) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, Span) {
	config := &SpanConfig{}
	for _, opt := range opts {
		opt.Apply(config)
	}

	spanOpts := []oteltrace.SpanStartOption{
		oteltrace.WithSpanKind(t.convertSpanKind(config.Kind)),
	}

	if len(config.Attributes) > 0 {
		attrs := make([]attribute.KeyValue, len(config.Attributes))
		for i, attr := range config.Attributes {
			attrs[i] = attribute.String(attr.Key, fmt.Sprintf("%v", attr.Value))
		}
		spanOpts = append(spanOpts, oteltrace.WithAttributes(attrs...))
	}

	if !config.Timestamp.IsZero() {
		spanOpts = append(spanOpts, oteltrace.WithTimestamp(config.Timestamp))
	}

	newCtx, otelSpan := t.tracer.Start(ctx, name, spanOpts...)

	return newCtx, &span{
		span: otelSpan,
		ctx:  newCtx,
	}
}

// SpanFromContext returns a span from context
func (t *tracer) SpanFromContext(ctx context.Context) Span {
	otelSpan := oteltrace.SpanFromContext(ctx)
	if otelSpan == nil {
		return &noopSpan{}
	}

	return &span{
		span: otelSpan,
		ctx:  ctx,
	}
}

// Extract extracts span context from carriers
func (t *tracer) Extract(ctx context.Context, carrier interface{}) context.Context {
	if headers, ok := carrier.(http.Header); ok {
		return t.propagator.Extract(ctx, propagation.HeaderCarrier(headers))
	}
	return ctx
}

// Inject injects span context into carriers
func (t *tracer) Inject(ctx context.Context, carrier interface{}) error {
	if headers, ok := carrier.(http.Header); ok {
		t.propagator.Inject(ctx, propagation.HeaderCarrier(headers))
		return nil
	}
	return fmt.Errorf("unsupported carrier type: %T", carrier)
}

// SetGlobalTracer sets this tracer as the global tracer
func (t *tracer) SetGlobalTracer() {
	otel.SetTracerProvider(t.provider)
	otel.SetTextMapPropagator(t.propagator)
}

// Shutdown shuts down the tracer
func (t *tracer) Shutdown(ctx context.Context) error {
	var errs []error

	// Shutdown all exporters
	for _, shutdownFunc := range t.shutdownFuncs {
		if err := shutdownFunc(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	// Shutdown provider
	if t.provider != nil {
		if err := t.provider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}

// convertSpanKind converts our SpanKind to OpenTelemetry SpanKind
func (t *tracer) convertSpanKind(kind SpanKind) oteltrace.SpanKind {
	switch kind {
	case SpanKindInternal:
		return oteltrace.SpanKindInternal
	case SpanKindServer:
		return oteltrace.SpanKindServer
	case SpanKindClient:
		return oteltrace.SpanKindClient
	case SpanKindProducer:
		return oteltrace.SpanKindProducer
	case SpanKindConsumer:
		return oteltrace.SpanKindConsumer
	default:
		return oteltrace.SpanKindInternal
	}
}

// span implementation

// End ends the span
func (s *span) End() {
	s.span.End()
}

// Finish is an alias for End for compatibility
func (s *span) Finish() {
	s.End()
}

// SetName sets the span name
func (s *span) SetName(name string) {
	s.span.SetName(name)
}

// SetStatus sets the span status
func (s *span) SetStatus(code StatusCode, description string) {
	var status trace.Status
	switch code {
	case StatusCodeOK:
		status = trace.Status{Code: codes.Ok}
	case StatusCodeError:
		status = trace.Status{Code: codes.Error, Description: description}
	default:
		status = trace.Status{Code: codes.Unset}
	}
	s.span.SetStatus(status.Code, status.Description)
}

// AddEvent adds an event to the span
func (s *span) AddEvent(name string, attrs ...Attribute) {
	otelAttrs := make([]attribute.KeyValue, len(attrs))
	for i, attr := range attrs {
		otelAttrs[i] = attribute.String(attr.Key, fmt.Sprintf("%v", attr.Value))
	}
	s.span.AddEvent(name, oteltrace.WithAttributes(otelAttrs...))
}

// RecordError records an error on the span
func (s *span) RecordError(err error, attrs ...Attribute) {
	otelAttrs := make([]attribute.KeyValue, len(attrs))
	for i, attr := range attrs {
		otelAttrs[i] = attribute.String(attr.Key, fmt.Sprintf("%v", attr.Value))
	}
	s.span.RecordError(err, oteltrace.WithAttributes(otelAttrs...))
}

// SetAttribute sets a single attribute
func (s *span) SetAttribute(key string, value interface{}) {
	s.span.SetAttributes(attribute.String(key, fmt.Sprintf("%v", value)))
}

// SetAttributes sets multiple attributes
func (s *span) SetAttributes(attrs ...Attribute) {
	otelAttrs := make([]attribute.KeyValue, len(attrs))
	for i, attr := range attrs {
		otelAttrs[i] = attribute.String(attr.Key, fmt.Sprintf("%v", attr.Value))
	}
	s.span.SetAttributes(otelAttrs...)
}

// Context returns the span context
func (s *span) Context() oteltrace.SpanContext {
	return s.span.SpanContext()
}

// IsRecording returns whether the span is recording
func (s *span) IsRecording() bool {
	return s.span.IsRecording()
}

// SetBaggageItem sets a baggage item
func (s *span) SetBaggageItem(key, value string) {
	member, _ := baggage.NewMember(key, value)
	bag := baggage.FromContext(s.ctx)
	bag, _ = bag.SetMember(member)
	s.ctx = baggage.ContextWithBaggage(s.ctx, bag)
}

// GetBaggageItem gets a baggage item
func (s *span) GetBaggageItem(key string) string {
	bag := baggage.FromContext(s.ctx)
	member := bag.Member(key)
	return member.Value()
}

// noopTracer is a no-op implementation for when tracing is disabled
type noopTracer struct{}

func (n *noopTracer) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, Span) {
	return ctx, &noopSpan{}
}

func (n *noopTracer) SpanFromContext(ctx context.Context) Span {
	return &noopSpan{}
}

func (n *noopTracer) Extract(ctx context.Context, carrier interface{}) context.Context {
	return ctx
}

func (n *noopTracer) Inject(ctx context.Context, carrier interface{}) error {
	return nil
}

func (n *noopTracer) SetGlobalTracer() {}

func (n *noopTracer) Shutdown(ctx context.Context) error {
	return nil
}

// noopSpan is a no-op implementation for when tracing is disabled
type noopSpan struct{}

func (n *noopSpan) End()                                          {}
func (n *noopSpan) Finish()                                       {}
func (n *noopSpan) SetName(name string)                           {}
func (n *noopSpan) SetStatus(code StatusCode, description string) {}
func (n *noopSpan) AddEvent(name string, attrs ...Attribute)      {}
func (n *noopSpan) RecordError(err error, attrs ...Attribute)     {}
func (n *noopSpan) SetAttribute(key string, value interface{})    {}
func (n *noopSpan) SetAttributes(attrs ...Attribute)              {}
func (n *noopSpan) Context() oteltrace.SpanContext                { return oteltrace.SpanContext{} }
func (n *noopSpan) IsRecording() bool                             { return false }
func (n *noopSpan) SetBaggageItem(key, value string)              {}
func (n *noopSpan) GetBaggageItem(key string) string              { return "" }

// Span option implementations

type spanKindOption struct {
	kind SpanKind
}

func (o *spanKindOption) Apply(config *SpanConfig) {
	config.Kind = o.kind
}

func WithSpanKind(kind SpanKind) SpanOption {
	return &spanKindOption{kind: kind}
}

type spanAttributesOption struct {
	attributes []Attribute
}

func (o *spanAttributesOption) Apply(config *SpanConfig) {
	config.Attributes = o.attributes
}

func WithAttributes(attrs ...Attribute) SpanOption {
	return &spanAttributesOption{attributes: attrs}
}

type spanTimestampOption struct {
	timestamp time.Time
}

func (o *spanTimestampOption) Apply(config *SpanConfig) {
	config.Timestamp = o.timestamp
}

func WithTimestamp(timestamp time.Time) SpanOption {
	return &spanTimestampOption{timestamp: timestamp}
}

// Helper functions

// isHTTPEndpoint checks if the endpoint is HTTP-based
func isHTTPEndpoint(endpoint string) bool {
	return len(endpoint) > 4 && (endpoint[:4] == "http")
}

// createFileWriter creates a file writer for the given path
func createFileWriter(path string) *os.File {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(fmt.Sprintf("failed to create file writer: %v", err))
	}
	return file
}
