package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestHTTPClientPropagatesTrace(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	var got string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Traceparent")
	}))
	t.Cleanup(srv.Close)

	ctx, span := tp.Tracer("t").Start(t.Context(), "caller")
	defer span.End()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	resp, err := HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if got == "" || got[3:35] != span.SpanContext().TraceID().String() {
		t.Fatalf("traceparent %q does not carry trace %s", got, span.SpanContext().TraceID())
	}
}
