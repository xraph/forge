package telemetry

import (
	"context"
	"runtime/pprof"

	"github.com/xraph/forge/internal/shared"
)

// Pyroscope's names, the ones grafana/otel-profiling-go uses, so Grafana's
// Tempo "Profiles for this span" link works without that wrapper.
const (
	profileIDAttribute = "pyroscope.profile.id"
	spanIDLabel        = "span_id"
	spanNameLabel      = "span_name"
)

// serveLabelled runs serve with the goroutine labelled span_id for the CPU
// profiler. It adds span_name once the router reports the match, and drops
// span_id when a stream is established, because the span ends at the
// handshake while the goroutine keeps running. When serve returns, panics
// included, pprof.Do sets the goroutine's labels to the ones on ctx. Those
// aren't always what the goroutine had before: a label set on the goroutine
// and never put on ctx is gone.
//
// span_name lives only on the goroutine, not on the request context. A handler
// that adds its own labels with pprof.Do therefore loses span_name inside that
// Do and for the rest of the request after it returns. span_id, the trace
// link, survives because the context carries it.
func serveLabelled(ctx context.Context, spanID, method string, match *shared.RouteMatch, serve func(context.Context)) {
	pprof.Do(ctx, pprof.Labels(spanIDLabel, spanID), func(labelled context.Context) {
		match.OnMatched(func(pattern string) {
			pprof.SetGoroutineLabels(pprof.WithLabels(labelled, pprof.Labels(spanNameLabel, spanName(method, pattern))))
		})

		match.OnEstablished(func(int) {
			if match.Pattern == "" {
				pprof.SetGoroutineLabels(ctx)

				return
			}

			pprof.SetGoroutineLabels(pprof.WithLabels(ctx, pprof.Labels(spanNameLabel, spanName(method, match.Pattern))))
		})

		serve(labelled)
	})
}

// spanName is the server span's final name: the method and route, or "HTTP"
// for a method outside the standard nine, whose span is never renamed.
func spanName(method, pattern string) string {
	if method == "_OTHER" {
		return "HTTP"
	}

	return spanNames.get(method, pattern)
}
