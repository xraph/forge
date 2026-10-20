package forge

import (
	"errors"
	"fmt"
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
	stacks := map[string]bool{}

	for _, s := range exp.GetSpans() {
		events[s.Name] = len(s.Events)

		for _, e := range s.Events {
			for _, kv := range e.Attributes {
				if kv.Key == "exception.stacktrace" && kv.Value.AsString() != "" {
					stacks[s.Name] = true
				}
			}
		}
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

	if !stacks["GET /panic"] {
		t.Error("recovered panic was recorded without a stack trace")
	}

	if stacks["GET /boom"] {
		t.Error("a 5xx error should be recorded without a stack trace")
	}
}

type queryError struct{ args string }

func (e *queryError) Error() string { return "query failed with args " + e.args }

// A handler's 5xx error and a recovered panic reach the span as their class.
// The message and the panic value stay in the logs.
func TestRouteErrorTextStaysOffTheSpan(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))

	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	r := NewRouter(WithRecovery())
	_ = r.GET("/boom", func(Context) error {
		return InternalError(fmt.Errorf("load user: %w", &queryError{args: "[SECRET]"}))
	})
	_ = r.GET("/panic", func(Context) error { panic("token SECRET") })

	h := telemetry.ServerMiddleware(tp, telemetry.Settings{ExcludePaths: []string{"/_/"}})(r)

	for _, p := range []string{"/boom", "/panic"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))
	}

	types := map[string]string{}

	for _, s := range exp.GetSpans() {
		if leaks := spanLeaks(s, "SECRET"); len(leaks) > 0 {
			t.Errorf("%s leaks error text in %v", s.Name, leaks)
		}

		if v, ok := eventAttr(&s, "exception", "exception.type"); ok {
			types[s.Name] = v
		}

		if _, ok := eventAttr(&s, "exception", "exception.message"); ok {
			t.Errorf("%s carries exception.message", s.Name)
		}
	}

	if types["GET /boom"] != "*forge.queryError" {
		t.Errorf("GET /boom exception.type = %q, want *forge.queryError", types["GET /boom"])
	}

	if types["GET /panic"] != "panic" {
		t.Errorf("GET /panic exception.type = %q, want panic", types["GET /panic"])
	}
}
