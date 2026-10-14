package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

// InstrumentTransport wraps rt so outbound requests get a client span and
// carry the trace headers. It reads the OTel globals now, so call it after
// the app is built and keep the result.
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
