package forge

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/logger"
)

func newTracedApp(t *testing.T, cfg TracingConfig) (App, *tracetest.InMemoryExporter) {
	t.Helper()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	a := New(WithAppName("traced"), WithAppTracingConfig(cfg))

	exp := tracetest.NewInMemoryExporter()
	if err := RegisterSpanProcessor(a, sdktrace.NewSimpleSpanProcessor(exp)); err != nil {
		t.Fatal(err)
	}

	return a, exp
}

func TestAppCreatesRouteNamedSpans(t *testing.T) {
	a, exp := newTracedApp(t, TracingConfig{})

	_ = a.Router().GET("/users/:id", func(c Context) error { return c.String(http.StatusOK, "ok") })

	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/7", nil))

	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Name != "GET /users/:id" {
		t.Fatalf("spans = %v", spans)
	}
}

func TestAppTracingDisabledByConfig(t *testing.T) {
	off := false
	a := New(WithAppName("off"), WithAppTracingConfig(TracingConfig{Enabled: &off}))

	if err := RegisterSpanProcessor(a, sdktrace.NewSimpleSpanProcessor(tracetest.NewInMemoryExporter())); err == nil {
		t.Fatal("want ErrTracingDisabled")
	}
}

func TestAppProvidesTracerProviderInDI(t *testing.T) {
	a, _ := newTracedApp(t, TracingConfig{})

	tp, err := Inject[trace.TracerProvider](a.Container())
	if err != nil || tp == nil {
		t.Fatalf("Inject[trace.TracerProvider] = %v, %v", tp, err)
	}
}

func TestAppBadEndpointStillBoots(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	log, ok := logger.NewTestLogger().(*logger.TestLogger)
	if !ok {
		t.Fatal("logger.NewTestLogger did not return a *logger.TestLogger")
	}

	a := New(WithAppName("bad-endpoint"), WithAppLogger(log), WithAppTracingConfig(TracingConfig{Endpoint: "localhost:4318"}))

	exp := tracetest.NewInMemoryExporter()
	if err := RegisterSpanProcessor(a, sdktrace.NewSimpleSpanProcessor(exp)); err != nil {
		t.Fatal(err)
	}

	_ = a.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })
	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))

	if len(exp.GetSpans()) != 1 {
		t.Fatal("spans should still be created when the exporter could not be built")
	}

	exportErrors := 0

	for _, e := range log.GetLogsByLevel("ERROR") {
		if e.Message == "telemetry: tracing export disabled" {
			exportErrors++
		}
	}

	if exportErrors != 1 {
		t.Fatalf("logged %d export-disabled errors, want 1; errors: %v", exportErrors, log.GetLogsByLevel("ERROR"))
	}

	if ep := a.(*app).tracing.Settings().Endpoint; ep != "" {
		t.Fatalf("fallback provider kept endpoint %q, want none", ep)
	}
}

func TestAppStopFlushesSpans(t *testing.T) {
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		hits.Add(1)
	}))
	t.Cleanup(srv.Close)

	a, _ := newTracedApp(t, TracingConfig{Endpoint: srv.URL + "/v1/traces"})
	_ = a.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if hits.Load() == 0 {
		t.Fatal("Stop returned before the queued span was exported")
	}
}

func TestAppConcurrentRequestsKeepTheirOwnIDs(t *testing.T) {
	a, exp := newTracedApp(t, TracingConfig{})

	var (
		mu     sync.Mutex
		byPath = map[string]string{} // request path -> log trace id seen inside the handler
	)

	_ = a.Router().GET("/r/:n", func(c Context) error {
		mu.Lock()
		byPath[c.Request().URL.Path] = logger.TraceIDFromContext(c.Context())
		mu.Unlock()

		return c.String(http.StatusOK, "ok")
	})
	_ = a.Router().GET("/other", func(c Context) error { return c.String(http.StatusOK, "ok") })

	var wg sync.WaitGroup

	for i := range 50 {
		wg.Go(func() {
			path := fmt.Sprintf("/r/%d", i)
			if i%2 == 1 {
				path = "/other"
			}

			a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		})
	}

	wg.Wait()

	for _, s := range exp.GetSpans() {
		path := ""

		for _, kv := range s.Attributes {
			if kv.Key == "url.path" {
				path = kv.Value.AsString()
			}
		}

		if s.Name == "GET /r/:n" && byPath[path] != s.SpanContext.TraceID().String() {
			t.Fatalf("request %s logged trace %s but its span is %s", path, byPath[path], s.SpanContext.TraceID())
		}

		if path == "/other" && s.Name != "GET /other" {
			t.Fatalf("/other got span name %q: route pattern leaked between requests", s.Name)
		}
	}
}

func TestAppTracingConfigFromConfigManager(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("tracing", map[string]any{"enabled": false})

	a := New(WithAppName("file-off"), WithAppConfigManager(cm))

	if err := RegisterSpanProcessor(a, sdktrace.NewSimpleSpanProcessor(tracetest.NewInMemoryExporter())); err == nil {
		t.Fatal("tracing.enabled: false in the config file should turn tracing off")
	}
}
