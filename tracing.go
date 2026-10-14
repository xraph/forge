package forge

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/forge/internal/telemetry"
)

// TracingConfig configures distributed tracing.
type TracingConfig = shared.TracingConfig

// ErrTracingDisabled is returned by RegisterSpanProcessor when tracing is off.
var ErrTracingDisabled = telemetry.ErrTracingDisabled

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
// caller's trace context. Build it once and reuse it.
func HTTPClient() *http.Client { return telemetry.HTTPClient() }

// InstrumentTransport wraps rt with tracing. A nil rt means http.DefaultTransport.
func InstrumentTransport(rt http.RoundTripper) http.RoundTripper {
	return telemetry.InstrumentTransport(rt)
}

// RegisterSpanProcessor adds a span processor to an app built by forge.New.
func RegisterSpanProcessor(a App, sp sdktrace.SpanProcessor) error {
	impl, ok := a.(*app)
	if !ok || impl.tracing == nil {
		return errors.New("forge: RegisterSpanProcessor needs an App built by forge.New")
	}

	return impl.tracing.RegisterSpanProcessor(sp)
}
