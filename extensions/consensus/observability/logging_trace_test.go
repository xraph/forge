package observability

import (
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/xraph/forge"
)

func TestLogManagerUsesOTelSpanIDs(t *testing.T) {
	tp := sdktrace.NewTracerProvider()

	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	ctx, span := tp.Tracer("t").Start(t.Context(), "raft-append")
	defer span.End()

	lm := NewLogManager("node-1", LogConfig{EnableTracing: true}, forge.NewNoopLogger())

	traceID, spanID := traceFields(ctx, lm.config.EnableTracing)
	if traceID != span.SpanContext().TraceID().String() || spanID != span.SpanContext().SpanID().String() {
		t.Fatalf("got %q/%q, want the OTel span's IDs", traceID, spanID)
	}

	if tid, sid := traceFields(t.Context(), true); tid != "" || sid != "" {
		t.Fatalf("no span should give empty IDs, got %q/%q", tid, sid)
	}
}
