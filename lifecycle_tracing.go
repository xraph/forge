package forge

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/xraph/forge/internal/telemetry"
	"github.com/xraph/forge/internal/telemetry/spanerror"
)

// lifecycleTracer is the app's own tracer, not the global one, so two apps in
// one process don't trace each other's boot.
func (a *app) lifecycleTracer() trace.Tracer {
	if a.tracing == nil {
		return noop.NewTracerProvider().Tracer(tracerName)
	}

	return a.tracing.TracerProvider().Tracer(tracerName)
}

// startLifecycleSpan opens the root span of a boot or a shutdown. The
// forge.lifecycle attribute makes the sampler keep it at any ratio above 0.
func (a *app) startLifecycleSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return a.lifecycleTracer().Start(ctx, name, //nolint:spancheck // ended by endLifecycleSpan
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(telemetry.LifecycleKey.Bool(true)),
	)
}

// endLifecycleSpan records err, when there is one, and ends the span. Only
// err's class reaches the span, as the status description and the exception
// event's exception.type, never its message: an extension's Register or Start
// error can quote a DSN or a credential, and the full error is logged anyway.
func endLifecycleSpan(span trace.Span, err error) {
	spanerror.RecordError(span, err)
	span.End()
}

// traceExtension runs fn inside a "<verb> <extension>" span. A panic is
// recorded on the span as spanerror.PanicClass, without its value, and
// re-raised, so the caller's recovery (or lack of it) is unchanged.
func (a *app) traceExtension(ctx context.Context, verb string, ext Extension, fn func(context.Context) error) (err error) {
	ctx, span := a.lifecycleTracer().Start(ctx, verb+" "+ext.Name(), //nolint:spancheck // ended by the deferred endLifecycleSpan
		trace.WithAttributes(
			attribute.String("forge.extension.name", ext.Name()),
			attribute.String("forge.extension.version", ext.Version()),
		),
	)

	defer func() {
		if r := recover(); r != nil {
			spanerror.RecordPanic(span, false)
			span.End()
			panic(r)
		}

		endLifecycleSpan(span, err)
	}()

	return fn(ctx) //nolint:spancheck // the deferred func ends the span
}
