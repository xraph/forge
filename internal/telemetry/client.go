package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

// InstrumentTransport wraps rt so outbound requests get a client span and
// carry the trace headers. It captures the global tracer provider and
// propagator current at the call, so in a process with several apps the
// transport stays with whichever provider was current then. Build it once and
// keep the result.
func InstrumentTransport(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}

	return otelhttp.NewTransport(rt,
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithPropagators(otel.GetTextMapPropagator()),
	)
}

// HTTPClient returns a client whose requests are traced. Every client shares
// http.DefaultTransport's connection pool.
func HTTPClient() *http.Client {
	return &http.Client{Transport: InstrumentTransport(nil)}
}
