package forge_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge"
)

func TestInjectExtractTraceContextRoundTrip(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())

		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	ctx, span := forge.StartSpan(t.Context(), "publish")
	defer span.End()

	headers := map[string]string{}
	forge.InjectTraceContext(ctx, headers)

	got := trace.SpanContextFromContext(forge.ExtractTraceContext(context.Background(), headers))
	if got.TraceID() != span.SpanContext().TraceID() || !got.IsRemote() {
		t.Fatalf("extracted %v", got)
	}

	forge.InjectTraceContext(ctx, nil) // must not panic

	if forge.ExtractTraceContext(ctx, nil) != ctx {
		t.Fatal("nil carrier changed ctx")
	}
}
