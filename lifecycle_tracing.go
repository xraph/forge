package forge

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/xraph/forge/internal/telemetry"
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

// endLifecycleSpan records err, when there is one, and ends the span.
func endLifecycleSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	span.End()
}

// traceExtension runs fn inside a "<verb> <extension>" span. A panic is
// recorded on the span and re-raised, so the caller's recovery (or lack of
// it) is unchanged.
func (a *app) traceExtension(ctx context.Context, verb string, ext Extension, fn func(context.Context) error) (err error) {
	ctx, span := a.lifecycleTracer().Start(ctx, verb+" "+ext.Name(), //nolint:spancheck // ended by the deferred endLifecycleSpan
		trace.WithAttributes(
			attribute.String("forge.extension.name", ext.Name()),
			attribute.String("forge.extension.version", ext.Version()),
		),
	)

	defer func() {
		if r := recover(); r != nil {
			endLifecycleSpan(span, fmt.Errorf("panic: %v", r))
			panic(r)
		}

		endLifecycleSpan(span, err)
	}()

	return fn(ctx) //nolint:spancheck // the deferred func ends the span
}
