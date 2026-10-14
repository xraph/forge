package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

var benchHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

func benchServe(b *testing.B, h http.Handler) {
	b.Helper()
	b.ReportAllocs()

	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/users/42", nil)

	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkBaseline(b *testing.B) { benchServe(b, benchHandler) }

func BenchmarkServerMiddlewareNoExporter(b *testing.B) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	benchServe(b, ServerMiddleware(tp, Settings{ExcludePaths: []string{"/_/"}, ResponseHeader: "traceparent"})(benchHandler))
}

func BenchmarkServerMiddlewareNoop(b *testing.B) {
	benchServe(b, ServerMiddleware(noop.NewTracerProvider(), Settings{})(benchHandler))
}
