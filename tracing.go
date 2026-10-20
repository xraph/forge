package forge

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/forge/internal/telemetry"
)

// TracingConfig configures distributed tracing.
type TracingConfig = shared.TracingConfig

// ErrTracingDisabled is returned by RegisterSpanProcessor when tracing is off.
var ErrTracingDisabled = telemetry.ErrTracingDisabled

// UnsampledSpanProcessor is a span processor that also wants the spans the
// sampler would drop, for as long as WantsUnsampled returns true. While any
// registered processor wants them, those spans are recorded and reach every
// registered processor, including ones that never asked, with IsRecording
// true. None of them is exported, so your OTLP backend sees the same sample as
// before. A processor that wants only exported spans checks
// s.SpanContext().IsSampled() in OnEnd. WantsUnsampled runs for every dropped
// span; keep it cheap.
type UnsampledSpanProcessor interface {
	sdktrace.SpanProcessor
	WantsUnsampled() bool
}

const tracerName = "github.com/xraph/forge"

// StartSpan starts a span as a child of whatever span ctx carries. End it
// when the work is done:
//
//	ctx, span := forge.StartSpan(c.Context(), "charge-card")
//	defer span.End()
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, name, opts...) //nolint:spancheck // the caller ends the span
}

// SpanFromContext returns the current span, or a no-op span if there is none.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// HTTPClient returns an HTTP client whose requests are traced and carry the
// caller's trace context. Build it once and reuse it. The client span's
// url.full has no userinfo or fragment and reads REDACTED for every query
// value, and a transport error reaches the span as its class only; see
// InstrumentTransport.
func HTTPClient() *http.Client { return telemetry.HTTPClient() }

// InstrumentTransport wraps rt with tracing. A nil rt means
// http.DefaultTransport. The request rt sends and the errors you get back are
// untouched. Only the span is scrubbed: url.full keeps the scheme, host and
// path, drops userinfo and the fragment and reads REDACTED for every query
// value, and a transport or body read error shows up as its class (the Go
// type of the innermost error) in the status and error.type. A secret in the
// path stays visible, so give such URLs a transport of your own.
func InstrumentTransport(rt http.RoundTripper) http.RoundTripper {
	return telemetry.InstrumentTransport(rt)
}

// RegisterSpanProcessor adds a span processor to an app built by forge.New.
// A processor that implements UnsampledSpanProcessor also receives the spans
// the sampler drops while it asks for them.
func RegisterSpanProcessor(a App, sp sdktrace.SpanProcessor) error {
	impl, ok := a.(*app)
	if !ok || impl.tracing == nil {
		return errors.New("forge: RegisterSpanProcessor needs an App built by forge.New")
	}

	return impl.tracing.RegisterSpanProcessor(sp)
}

// InjectTraceContext writes ctx's trace context into carrier, such as a
// message's headers, with the global propagator. It works with tracing off, so
// a pass-through hop keeps the caller's trace. A nil carrier is ignored.
func InjectTraceContext(ctx context.Context, carrier map[string]string) {
	if carrier == nil {
		return
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(carrier))
}

// ExtractTraceContext returns ctx with the remote span context found in
// carrier, or ctx unchanged when there is none. Start the consumer's span from
// the returned context to join the publisher's trace.
func ExtractTraceContext(ctx context.Context, carrier map[string]string) context.Context {
	if len(carrier) == 0 {
		return ctx
	}

	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}
