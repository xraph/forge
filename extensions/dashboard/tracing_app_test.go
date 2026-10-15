package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// newDashboardTracedApp wires the trace store the way the extension does:
// the processor on the app's provider, the middleware inside the server span.
func newDashboardTracedApp(t *testing.T, capture bool) (forge.App, *collector.TraceStore, *collector.SpanProcessor) {
	t.Helper()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	ratio := 0.0 // nothing sampled: the dashboard must still see every request
	a := forge.New(forge.WithAppName("dash-traced"), forge.WithAppTracingConfig(forge.TracingConfig{SampleRatio: &ratio}))

	store := collector.NewTraceStore(50, time.Hour)
	p := collector.NewSpanProcessor(store, "/dashboard")

	if err := forge.RegisterSpanProcessor(a, p); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = p.Shutdown(t.Context()) })

	a.Router().UseGlobal(TracingMiddleware(store, "/dashboard", capture))

	return a, store, p
}

func TestDashboardTraces_RealRequestUnsampled(t *testing.T) {
	a, store, p := newDashboardTracedApp(t, true)

	_ = a.Router().POST("/orders/:id", func(c forge.Context) error {
		_, span := forge.StartSpan(c.Context(), "charge-card")
		span.End()

		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders/9", strings.NewReader(`{"card":"x","password":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "r-9")

	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if err := p.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}

	summaries, _ := store.ListTraces(collector.TraceFilter{})
	if len(summaries) != 1 {
		t.Fatalf("got %d traces, want 1", len(summaries))
	}

	d := store.GetTrace(summaries[0].TraceID)

	// The traceparent response header carries the same trace ID, even though
	// the request was not sampled for export.
	if tp := rec.Header().Get("Traceparent"); !strings.Contains(tp, d.TraceID) {
		t.Fatalf("traceparent %q does not carry trace %s", tp, d.TraceID)
	}

	if d.RootSpan.Name != "POST /orders/:id" || d.SpanCount != 2 {
		t.Fatalf("root %q, %d spans", d.RootSpan.Name, d.SpanCount)
	}

	if d.RootSpan.HTTP == nil || d.RootSpan.HTTP.Request.Headers["X-Request-Id"] != "r-9" {
		t.Fatalf("exchange missing: %+v", d.RootSpan.HTTP)
	}

	if !strings.Contains(d.RootSpan.HTTP.Request.Body, `"[redacted]"`) {
		t.Fatalf("body not captured and redacted: %q", d.RootSpan.HTTP.Request.Body)
	}
}

func TestDashboardTraces_ClosedGateRecordsNothing(t *testing.T) {
	a, store, p := newDashboardTracedApp(t, false)
	store.SetIngestGate(func() bool { return false })

	_ = a.Router().GET("/x", func(c forge.Context) error {
		if forge.SpanFromContext(c.Context()).IsRecording() {
			t.Error("span recorded at ratio 0 with nobody watching")
		}

		return c.String(http.StatusOK, "ok")
	})

	a.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	_ = p.ForceFlush(t.Context())

	if summaries, _ := store.ListTraces(collector.TraceFilter{}); len(summaries) != 0 {
		t.Fatalf("closed gate stored %d traces", len(summaries))
	}
}

func TestDashboardRegister_TracingDisabled(t *testing.T) {
	off := false
	a := forge.New(forge.WithAppName("dash-off"), forge.WithAppTracingConfig(forge.TracingConfig{Enabled: &off}))

	ext, ok := NewExtension().(*Extension)
	if !ok {
		t.Fatal("NewExtension did not return *Extension")
	}

	if err := ext.Register(a); err != nil {
		t.Fatalf("Register with tracing off: %v", err)
	}

	if ext.spanProcessor != nil {
		t.Fatal("span processor kept although tracing is off")
	}
}

func TestDashboardRegister_TracingOnRegistersProcessor(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "")

	a := forge.New(forge.WithAppName("dash-on"))

	ext, ok := NewExtension().(*Extension)
	if !ok {
		t.Fatal("NewExtension did not return *Extension")
	}

	if err := ext.Register(a); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ext.Stop(t.Context()) })

	if ext.spanProcessor == nil {
		t.Fatal("no span processor registered")
	}
}

// Spans end on other goroutines from the moment the processor is registered,
// and every one of them runs the ingest gate. Register must not write anything
// the gate reads without synchronization. Run under -race.
func TestDashboardRegister_SpansEndingDuringRegisterDoNotRace(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "")

	a := forge.New(forge.WithAppName("dash-race"))

	ext, ok := NewExtension().(*Extension)
	if !ok {
		t.Fatal("NewExtension did not return *Extension")
	}

	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)

		for {
			select {
			case <-stop:
				return
			default:
			}

			_, span := forge.StartSpan(context.Background(), "background-work")
			span.End()
		}
	}()

	err := ext.Register(a)

	// A second Register replaces the broker while the first processor's gate
	// may still be reading it. The pilot refuses to register twice, so this
	// one returns an error; only the race matters here.
	_ = ext.Register(a)

	close(stop)
	<-done

	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ext.Stop(context.WithoutCancel(t.Context())) })
}

// A second Register builds a new trace store. The processor has to follow it,
// or spans keep landing in a store nobody reads any more.
func TestDashboardRegister_TwiceRetiresTheStaleProcessor(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "")

	a := forge.New(forge.WithAppName("dash-twice"))

	ext, ok := NewExtension(WithTraceIdleTTL(0)).(*Extension)
	if !ok {
		t.Fatal("NewExtension did not return *Extension")
	}

	if err := ext.Register(a); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ext.Stop(context.WithoutCancel(t.Context())) })

	first, firstStore := ext.spanProcessor, ext.traceStore

	// The contract pilot refuses a second registration today, so this Register
	// fails part way. Whatever it returns, the first processor must be gone.
	err := ext.Register(a)

	if first.WantsUnsampled() || ext.spanProcessor == first {
		t.Fatal("second Register kept the first span processor running")
	}

	if ext.traceStore == firstStore {
		t.Fatal("second Register kept the first trace store")
	}

	if err != nil {
		// Finish what a successful Register does last.
		ext.registerSpanProcessor(a)
	}

	_, span := forge.StartSpan(t.Context(), "after-second-register")
	span.End()

	if err := ext.spanProcessor.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got, _ := ext.traceStore.ListTraces(collector.TraceFilter{}); len(got) != 1 {
		t.Fatalf("new store holds %d traces, want 1", len(got))
	}

	if got, _ := firstStore.ListTraces(collector.TraceFilter{}); len(got) != 0 {
		t.Fatalf("old store still receives spans: %d traces", len(got))
	}
}

// BenchmarkTraceIngestGate measures the gate every dropped and every ended span
// pays when nobody watches: a real broker with no streams and a TTL that has
// long run out.
func BenchmarkTraceIngestGate(b *testing.B) {
	store := collector.NewTraceStore(10, time.Hour)
	b.Cleanup(store.Close)

	var broker atomic.Pointer[transport.StreamBroker]
	broker.Store(transport.NewStreamBroker(contract.NewRegistry(), contract.NewWardenRegistry(), nil))

	store.SetIngestGate(traceIngestGate(store, &broker, time.Minute))

	p := collector.NewSpanProcessor(store, "/dashboard")

	b.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	b.Run("accepting", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if store.Accepting() {
				b.Fatal("gate open with nobody watching")
			}
		}
	})

	b.Run("wants-unsampled", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if p.WantsUnsampled() {
				b.Fatal("processor wants spans with nobody watching")
			}
		}
	})
}
