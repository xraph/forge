package forge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/forge/internal/logger"
)

func clearProfilingEnv(t *testing.T) {
	t.Helper()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	for _, k := range []string{
		"PYROSCOPE_SERVER_ADDRESS", "PYROSCOPE_APPLICATION_NAME", "PYROSCOPE_BASIC_AUTH_USER",
		"PYROSCOPE_BASIC_AUTH_PASSWORD", "PYROSCOPE_TENANT_ID",
	} {
		t.Setenv(k, "")
	}
}

// profileIDOf serves one request through a and returns the span's
// pyroscope.profile.id, or "" when it has none.
func profileIDOf(t *testing.T, a App, exp *tracetest.InMemoryExporter) string {
	t.Helper()

	_ = a.Router().GET("/users/:id", func(c Context) error { return c.String(http.StatusOK, "ok") })
	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/7", nil))

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}

	for _, kv := range spans[0].Attributes {
		if kv.Key == "pyroscope.profile.id" {
			if want := spans[0].SpanContext.SpanID().String(); kv.Value.AsString() != want {
				t.Fatalf("pyroscope.profile.id = %q, want the span ID %q", kv.Value.AsString(), want)
			}

			return kv.Value.AsString()
		}
	}

	return ""
}

func withExporter(t *testing.T, a App) *tracetest.InMemoryExporter {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	if err := RegisterSpanProcessor(a, sdktrace.NewSimpleSpanProcessor(exp)); err != nil {
		t.Fatal(err)
	}

	return exp
}

func TestAppProfilingOffByDefault(t *testing.T) {
	clearProfilingEnv(t)

	a := New(WithAppName("plain"))

	if id := profileIDOf(t, a, withExporter(t, a)); id != "" {
		t.Fatalf("pyroscope.profile.id = %q with no profiling config", id)
	}
}

func TestAppSpanLabelsFromCode(t *testing.T) {
	clearProfilingEnv(t)

	on := true
	a := New(WithAppName("labels"), WithAppProfilingConfig(ProfilingConfig{SpanLabels: &on}))

	if id := profileIDOf(t, a, withExporter(t, a)); id == "" {
		t.Fatal("span_labels: true should put pyroscope.profile.id on the span")
	}
}

func TestAppSpanLabelsFromConfigManager(t *testing.T) {
	clearProfilingEnv(t)

	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("profiling", map[string]any{"span_labels": true})

	a := New(WithAppName("labels-file"), WithAppConfigManager(cm))

	if id := profileIDOf(t, a, withExporter(t, a)); id == "" {
		t.Fatal("profiling.span_labels in the config file should put pyroscope.profile.id on the span")
	}
}

func TestAppWarnsWhenSpanLabelsNeedTracing(t *testing.T) {
	clearProfilingEnv(t)

	log, ok := logger.NewTestLogger().(*logger.TestLogger)
	if !ok {
		t.Fatal("logger.NewTestLogger did not return a *logger.TestLogger")
	}

	on, off := true, false
	New(WithAppName("no-tracing"), WithAppLogger(log),
		WithAppTracingConfig(TracingConfig{Enabled: &off}),
		WithAppProfilingConfig(ProfilingConfig{SpanLabels: &on}))

	for _, e := range log.GetLogsByLevel("WARN") {
		if e.Message == "profiling: span labels need tracing, which is off, so they stay off" {
			return
		}
	}

	t.Fatal("want a warning that span labels need tracing")
}

func TestAppPushesProfilesUntilStop(t *testing.T) {
	clearProfilingEnv(t)

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		hits.Add(1)
	}))
	t.Cleanup(srv.Close)

	a := New(WithAppName("pushed"), WithAppProfilingConfig(ProfilingConfig{ServerAddress: srv.URL}))

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if !a.(*app).profiler.Running() {
		t.Fatal("profiler not running after Start")
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if a.(*app).profiler.Running() {
		t.Fatal("profiler still running after Stop")
	}

	if hits.Load() == 0 {
		t.Fatal("Stop returned before the final profiles were uploaded")
	}
}

func TestAppUnreachablePyroscopeDoesNotBlockBoot(t *testing.T) {
	clearProfilingEnv(t)

	a := New(WithAppName("unreachable"), WithAppProfilingConfig(ProfilingConfig{ServerAddress: "http://127.0.0.1:1"}))

	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("Start = %v, want nil with Pyroscope down", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	start := time.Now()

	if err := a.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	if time.Since(start) > 5*time.Second {
		t.Fatalf("Stop took %v", time.Since(start))
	}
}

func TestAppFailedStartStopsProfiler(t *testing.T) {
	clearProfilingEnv(t)

	a := New(WithAppName("failing"), WithAppProfilingConfig(ProfilingConfig{ServerAddress: "http://127.0.0.1:1"}))

	if err := a.RegisterHookFn(PhaseBeforeStart, "fail", func(context.Context, App) error {
		return errors.New("boom")
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.Start(t.Context()); err == nil {
		t.Fatal("Start should fail when a before-start hook fails")
	}

	if a.(*app).profiler.Running() {
		t.Fatal("a failed Start left the profiler running")
	}
}

// TestAppFailedStartCapsFinalUpload points push at a Pyroscope that accepts
// the upload and never answers. A failed Start must still return quickly,
// and the final upload proves the profiler ran before the hooks.
func TestAppFailedStartCapsFinalUpload(t *testing.T) {
	clearProfilingEnv(t)

	arrived := make(chan struct{}, 1)
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}

		<-release
	}))
	// Cleanups run last in, first out: release the handler, then close.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	a := New(WithAppName("hanging"), WithAppProfilingConfig(ProfilingConfig{ServerAddress: srv.URL}))

	var runningInHook atomic.Bool

	if err := a.RegisterHookFn(PhaseBeforeStart, "fail", func(context.Context, App) error {
		runningInHook.Store(a.(*app).profiler.Running())

		return errors.New("boom")
	}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err := a.Start(t.Context())
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Start = %v, want the hook's error", err)
	}

	if elapsed > 10*time.Second {
		t.Fatalf("Start took %v to return the hook error, want the final upload capped", elapsed)
	}

	if !runningInHook.Load() {
		t.Fatal("profiler was not running when the before-start hooks ran")
	}

	if a.(*app).profiler.Running() {
		t.Fatal("a failed Start left the profiler running")
	}

	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("the hanging Pyroscope never saw the final upload")
	}
}

func TestAppWarnsWhenPushHoldsPprofCPU(t *testing.T) {
	clearProfilingEnv(t)

	log, ok := logger.NewTestLogger().(*logger.TestLogger)
	if !ok {
		t.Fatal("logger.NewTestLogger did not return a *logger.TestLogger")
	}

	a := New(WithAppName("both"), WithAppLogger(log), WithPprof(),
		WithAppProfilingConfig(ProfilingConfig{ServerAddress: "http://127.0.0.1:1"}))

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	for _, e := range log.GetLogsByLevel("WARN") {
		if e.Message == pprofCPUConflictWarning {
			return
		}
	}

	t.Fatal("want the warning that push holds the CPU profiler")
}
