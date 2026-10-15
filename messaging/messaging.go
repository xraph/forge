// Package messaging traces and times message publishing and processing the
// way the OpenTelemetry messaging semantic conventions describe. Forge's
// queue, events, conduit and kafka extensions use it, and you can use it in
// producers and consumers you write yourself.
//
//	mt := messaging.New("kafka", messaging.WithMetrics(app.Metrics()))
//	err := mt.Process(ctx, carrier, "orders", "process", func(ctx context.Context) error {
//		return handle(ctx, msg)
//	})
package messaging

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/go-utils/log"
	gometrics "github.com/xraph/go-utils/metrics"

	"github.com/xraph/forge/internal/metrics/vec"
)

const instrumentationName = "github.com/xraph/forge/messaging"

// Telemetry instruments one messaging system. A nil *Telemetry is valid: it
// starts no spans and records nothing, but StartProcess and Process still
// extract the caller's trace context.
type Telemetry struct {
	system  string
	tracing bool
	send    *vec.Vec[gometrics.Histogram]
	process *vec.Vec[gometrics.Histogram]
}

type config struct {
	tracing bool
	metrics gometrics.Metrics
}

// Option configures New.
type Option func(*config)

// WithTracing turns spans on or off. It defaults to true. With tracing off,
// trace context still passes through.
func WithTracing(enabled bool) Option { return func(c *config) { c.tracing = enabled } }

// WithMetrics records messaging.client.operation.duration and
// messaging.process.duration into m. Without it, or with a nil m, nothing is
// recorded. forge.Metrics is this type.
func WithMetrics(m gometrics.Metrics) Option { return func(c *config) { c.metrics = m } }

// New builds the instrumenter for one messaging system, such as "kafka",
// "rabbitmq", "redis", "nats" or "memory". The value becomes messaging.system.
func New(system string, opts ...Option) *Telemetry {
	cfg := config{tracing: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	t := &Telemetry{system: system, tracing: cfg.tracing}
	if cfg.metrics != nil {
		t.send = durationVec(cfg.metrics, clientOperationDuration, clientOperationHelp)
		t.process = durationVec(cfg.metrics, processDuration, processHelp)
	}

	return t
}

type startConfig struct {
	kind  trace.SpanKind
	links []trace.Link
	batch int
}

// StartOption adjusts one operation.
type StartOption func(*startConfig)

// WithLinks adds span links, such as a batch send linking each message's
// create span.
func WithLinks(links ...trace.Link) StartOption {
	return func(c *startConfig) { c.links = append(c.links, links...) }
}

// WithBatchSize sets messaging.batch.message_count. An n of zero or less is
// ignored and leaves the attribute off.
func WithBatchSize(n int) StartOption { return func(c *startConfig) { c.batch = n } }

// WithSpanKind overrides the span kind. Request and reply use client and
// server.
func WithSpanKind(kind trace.SpanKind) StartOption { return func(c *startConfig) { c.kind = kind } }

// StartCreate starts a producer span for creating one message, to inject into
// that message. It records no metric.
func (t *Telemetry) StartCreate(ctx context.Context, destination string, opts ...StartOption) (context.Context, *Operation) {
	if t == nil {
		return ctx, nil
	}

	return t.start(ctx, destination, "create", "create", trace.SpanKindProducer, nil, opts)
}

// StartSend starts a producer span for sending and times
// messaging.client.operation.duration.
func (t *Telemetry) StartSend(ctx context.Context, destination, operation string, opts ...StartOption) (context.Context, *Operation) {
	if t == nil {
		return ctx, nil
	}

	return t.start(ctx, destination, operation, "send", trace.SpanKindProducer, t.send, opts)
}

// StartProcess extracts the trace context from carrier, starts a consumer
// span as its child, puts the trace and span IDs on ctx for log correlation,
// and times messaging.process.duration.
func (t *Telemetry) StartProcess(ctx context.Context, carrier propagation.TextMapCarrier, destination, operation string, opts ...StartOption) (context.Context, *Operation) {
	ctx = Extract(ctx, carrier)
	if t == nil {
		return withLogIDs(ctx), nil
	}

	ctx, op := t.start(ctx, destination, operation, "process", trace.SpanKindConsumer, t.process, opts)

	return withLogIDs(ctx), op
}

// PanicError is the error End records when Process recovers a panic, before
// re-raising it.
type PanicError struct{ Value any }

func (e *PanicError) Error() string { return fmt.Sprintf("messaging: handler panicked: %v", e.Value) }

// Unwrap returns the panic value when it is an error, so errors.Is and
// errors.As can reach it through the error End records. It returns nil for any
// other value.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)

	return err
}

// Process runs fn inside a process operation and returns fn's error. A panic
// in fn ends the operation with a *PanicError and is re-raised.
func (t *Telemetry) Process(ctx context.Context, carrier propagation.TextMapCarrier, destination, operation string, fn func(context.Context) error, opts ...StartOption) (err error) {
	ctx, op := t.StartProcess(ctx, carrier, destination, operation, opts...)

	defer func() {
		if r := recover(); r != nil {
			op.End(&PanicError{Value: r})
			panic(r)
		}

		op.End(err)
	}()

	return fn(ctx)
}

func (t *Telemetry) start(ctx context.Context, destination, operation, opType string, kind trace.SpanKind, hist *vec.Vec[gometrics.Histogram], opts []StartOption) (context.Context, *Operation) {
	if !t.tracing && hist == nil {
		return ctx, nil
	}

	cfg := startConfig{kind: kind}
	for _, opt := range opts {
		opt(&cfg)
	}

	op := &Operation{hist: hist, labels: [4]string{t.system, destination, operation, opType}, start: time.Now()}
	if !t.tracing {
		return ctx, op
	}

	attrs := make([]attribute.KeyValue, 0, 5)
	attrs = append(attrs,
		attribute.String("messaging.system", t.system),
		attribute.String("messaging.operation.name", operation),
		attribute.String("messaging.operation.type", opType),
	)

	name := operation

	if destination != "" {
		attrs = append(attrs, attribute.String("messaging.destination.name", destination))
		name += " " + destination
	}

	if cfg.batch > 0 {
		attrs = append(attrs, attribute.Int("messaging.batch.message_count", cfg.batch))
	}

	ctx, op.span = otel.Tracer(instrumentationName).Start(ctx, name, //nolint:spancheck // Operation.End ends it
		trace.WithSpanKind(cfg.kind), trace.WithAttributes(attrs...), trace.WithLinks(cfg.links...))

	return ctx, op
}

// withLogIDs puts the active span's IDs on ctx so a request-scoped logger
// carries trace_id and span_id, as forge's HTTP and gRPC layers do.
func withLogIDs(ctx context.Context) context.Context {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ctx
	}

	ctx = log.WithTraceID(ctx, sc.TraceID().String())

	return log.WithSpanID(ctx, sc.SpanID().String())
}
