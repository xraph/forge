package forge

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/telemetry"
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

// A handler that dispatches a second request through the router with its own
// context (the way the orpc extension does) must not rename the outer span.
func TestAppNestedDispatchKeepsOuterRoute(t *testing.T) {
	a, exp := newTracedApp(t, TracingConfig{})

	_ = a.Router().GET("/users/:id", func(c Context) error { return c.String(http.StatusOK, "user") })
	_ = a.Router().POST("/rpc", func(c Context) error {
		inner := httptest.NewRequestWithContext(c.Context(), http.MethodGet, "/users/7", nil)
		a.Router().ServeHTTP(httptest.NewRecorder(), inner)

		return c.String(http.StatusOK, "rpc")
	})

	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc", nil))

	names := map[string]string{}

	for _, s := range exp.GetSpans() {
		route := ""

		for _, kv := range s.Attributes {
			if kv.Key == "http.route" {
				route = kv.Value.AsString()
			}
		}

		names[s.Name] = route
	}

	if len(names) != 2 || names["POST /rpc"] != "/rpc" || names["GET /users/:id"] != "/users/:id" {
		t.Fatalf("span name -> http.route = %v, want POST /rpc -> /rpc and GET /users/:id -> /users/:id", names)
	}
}

func TestAppTracingDisabledStillPropagates(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	var downstream atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstream.Store(r.Header.Get("Traceparent"))
	}))
	t.Cleanup(srv.Close)

	off := false
	a := New(WithAppName("propagate-only"), WithAppTracingConfig(TracingConfig{Enabled: &off}))

	client := HTTPClient()

	_ = a.Router().GET("/call", func(c Context) error {
		req, err := http.NewRequestWithContext(c.Context(), http.MethodGet, srv.URL, nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}

		_ = resp.Body.Close()

		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/call", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	if rec.Header().Get("Traceparent") != "" {
		t.Fatal("disabled tracing wrote a traceparent response header")
	}

	got, _ := downstream.Load().(string)
	if !strings.HasPrefix(got, "00-4bf92f3577b34da6a3ce929d0e0e4736-") {
		t.Fatalf("downstream traceparent = %q, want trace id 4bf92f3577b34da6a3ce929d0e0e4736", got)
	}
}

func TestAppWarnsOnUnreadableTracingConfig(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	const msg = "telemetry: could not read tracing config, using defaults"

	warned := func(cm ConfigManager) bool {
		log, ok := logger.NewTestLogger().(*logger.TestLogger)
		if !ok {
			t.Fatal("logger.NewTestLogger did not return a *logger.TestLogger")
		}

		New(WithAppName("tracing-config"), WithAppLogger(log), WithAppConfigManager(cm))

		for _, e := range log.GetLogsByLevel("WARN") {
			if e.Message == msg {
				return true
			}
		}

		return false
	}

	bad := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	bad.Set("tracing", true) // "tracing: true" in YAML, which is not a block

	if !warned(bad) {
		t.Fatal("a tracing block that fails to bind should log a warning")
	}

	if warned(NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)) {
		t.Fatal("a config with no tracing block should not warn")
	}
}

func TestTracingIgnoresInvalidSampleRatio(t *testing.T) {
	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("tracing", map[string]any{"sample_ratio": "lots", "endpoint": "http://collector:4318"})

	log := captureLogger(t)
	file := readTracingFile(cm, log)

	if file.SampleRatio != nil {
		t.Fatalf("SampleRatio = %v, want nil so the default applies", *file.SampleRatio)
	}

	if file.Endpoint != "http://collector:4318" {
		t.Fatalf("Endpoint = %q, the good key in the same block should still apply", file.Endpoint)
	}

	if got := telemetry.Resolve(telemetry.Inputs{File: file}).SampleRatio; got != 1.0 {
		t.Fatalf("resolved ratio = %v, want the development default 1.0, not 0", got)
	}

	if keys := warnedKeys(log); len(keys) != 1 || keys[0] != "tracing.sample_ratio" {
		t.Fatalf("warned for %v, want [tracing.sample_ratio]", keys)
	}
}

func TestAppWarnsOnInvalidSampleRatio(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("tracing", map[string]any{"sample_ratio": "lots"})

	log := captureLogger(t)
	New(WithAppName("bad-ratio"), WithAppLogger(log), WithAppConfigManager(cm))

	found := false

	for _, e := range log.GetLogsByLevel("WARN") {
		if v, _ := e.Field("value"); e.Message == invalidValueMsg && v == "lots" {
			found = true
		}
	}

	if !found {
		t.Fatal("New should warn about tracing.sample_ratio: lots")
	}
}
