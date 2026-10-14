package forge

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/forge/internal/telemetry"
)

func TestRouteErrorsRecordedOnSpan(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))

	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	r := NewRouter(WithRecovery())
	_ = r.GET("/boom", func(Context) error { return InternalError(errors.New("db down")) })
	_ = r.GET("/bad", func(Context) error { return BadRequest("nope") })
	_ = r.GET("/panic", func(Context) error { panic("kaboom") })

	h := telemetry.ServerMiddleware(tp, telemetry.Settings{ExcludePaths: []string{"/_/"}})(r)

	for _, p := range []string{"/boom", "/bad", "/panic"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))
	}

	events := map[string]int{}
	for _, s := range exp.GetSpans() {
		events[s.Name] = len(s.Events)
	}

	if events["GET /boom"] == 0 {
		t.Error("500 from a handler was not recorded on the span")
	}

	if events["GET /bad"] != 0 {
		t.Error("400 must not record an exception event")
	}

	if events["GET /panic"] == 0 {
		t.Error("recovered panic was not recorded on the span")
	}
}
