package messaging

import (
	"context"
	"maps"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Inject writes ctx's trace context into carrier with the global propagator.
// It works with tracing off, so a pass-through hop keeps the caller's trace.
// Injecting into a nil map panics, as propagation.MapCarrier does; use
// WithTraceContext for maps.
func Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	if carrier == nil {
		return
	}

	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// Extract returns ctx with the remote span context found in carrier, or ctx
// unchanged when there is none.
func Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	if carrier == nil {
		return ctx
	}

	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// WithTraceContext returns headers plus ctx's trace context. It never modifies
// headers: when there is something to add it returns a copy, otherwise
// headers itself, so nil stays nil.
func WithTraceContext(ctx context.Context, headers map[string]string) map[string]string {
	added := propagation.MapCarrier{}
	Inject(ctx, added)

	if len(added) == 0 {
		return headers
	}

	out := make(map[string]string, len(headers)+len(added))
	maps.Copy(out, headers)
	maps.Copy(out, added)

	return out
}
