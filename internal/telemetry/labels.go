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
// handshake while the goroutine keeps running. pprof.Do puts the previous
// labels back when serve returns, panics included.
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

	return method + " " + pattern
}
