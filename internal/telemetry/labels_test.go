package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/shared"
)

// labelLines returns the "# labels:" lines of a debug=1 goroutine profile, one
// per group of goroutines. The calling goroutine is included.
func labelLines(t *testing.T) []string {
	t.Helper()

	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		t.Fatal(err)
	}

	var out []string

	for line := range strings.Lines(buf.String()) {
		if strings.HasPrefix(line, "# labels: ") {
			out = append(out, strings.TrimSpace(line))
		}
	}

	return out
}

// labelsWithSpan returns the label line holding span_id=id, or "".
func labelsWithSpan(t *testing.T, id string) string {
	t.Helper()

	for _, l := range labelLines(t) {
		if strings.Contains(l, `"span_id":"`+id+`"`) {
			return l
		}
	}

	return ""
}

func spanIDOf(r *http.Request) string {
	return trace.SpanFromContext(r.Context()).SpanContext().SpanID().String()
}

func labelledSetup(t *testing.T, sampler sdktrace.Sampler, labels bool) (*tracetest.InMemoryExporter, func(http.Handler) http.Handler) {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sampler))

	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	return exp, ServerMiddleware(tp, Settings{ExcludePaths: []string{"/_/"}, SpanLabels: labels})
}

func serve(t *testing.T, h http.Handler, method, path string) {
	t.Helper()

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), method, path, nil))
}

func TestSpanLabelsOnSampledRequest(t *testing.T) {
	exp, mw := labelledSetup(t, sdktrace.AlwaysSample(), true)

	var beforeMatch, afterMatch, id string

	serve(t, mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id = spanIDOf(r)
		beforeMatch = labelsWithSpan(t, id)

		shared.RouteMatchFromContext(r.Context()).Match("/users/:id", shared.KindHTTP)

		afterMatch = labelsWithSpan(t, id)
	})), http.MethodGet, "/users/7")

	if beforeMatch == "" || strings.Contains(beforeMatch, "span_name") {
		t.Fatalf("before the match, labels = %q, want span_id only", beforeMatch)
	}

	if !strings.Contains(afterMatch, `"span_name":"GET /users/:id"`) {
		t.Fatalf("after the match, labels = %q, want span_name GET /users/:id", afterMatch)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}

	if v, _ := attrOf(spans[0].Attributes, "pyroscope.profile.id"); v.AsString() != id {
		t.Fatalf("pyroscope.profile.id = %q, want %q", v.AsString(), id)
	}

	if l := labelsWithSpan(t, id); l != "" {
		t.Fatalf("labels outlived the request: %q", l)
	}
}

func TestSpanLabelsUnknownMethodUsesHTTP(t *testing.T) {
	_, mw := labelledSetup(t, sdktrace.AlwaysSample(), true)

	var got string

	serve(t, mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.RouteMatchFromContext(r.Context()).Match("/x", shared.KindHTTP)
		got = labelsWithSpan(t, spanIDOf(r))
	})), "PURGE", "/x")

	if !strings.Contains(got, `"span_name":"HTTP"`) {
		t.Fatalf("labels = %q, want span_name HTTP", got)
	}
}

func TestSpanLabelsOffOrUnsampledAddNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		sampler sdktrace.Sampler
		labels  bool
	}{
		"labels off": {sdktrace.AlwaysSample(), false},
		"unsampled":  {sdktrace.NeverSample(), true},
	} {
		t.Run(name, func(t *testing.T) {
			exp, mw := labelledSetup(t, tc.sampler, tc.labels)

			var got string

			serve(t, mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				shared.RouteMatchFromContext(r.Context()).Match("/x", shared.KindHTTP)
				got = labelsWithSpan(t, spanIDOf(r))
			})), http.MethodGet, "/x")

			if got != "" {
				t.Fatalf("labels = %q, want none", got)
			}

			for _, s := range exp.GetSpans() {
				if _, ok := attrOf(s.Attributes, "pyroscope.profile.id"); ok {
					t.Fatal("span has pyroscope.profile.id")
				}
			}
		})
	}
}

func TestSpanLabelsDropSpanIDWhenStreamEstablished(t *testing.T) {
	_, mw := labelledSetup(t, sdktrace.AlwaysSample(), true)

	var after string

	serve(t, mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := shared.RouteMatchFromContext(r.Context())
		m.Match("/ws/chat", shared.KindWebSocket)
		m.Establish(http.StatusSwitchingProtocols)

		for _, l := range labelLines(t) {
			if strings.Contains(l, `"span_name":"GET /ws/chat"`) {
				after = l
			}
		}
	})), http.MethodGet, "/ws/chat")

	if after == "" {
		t.Fatal("span_name is gone after the stream was established, want it kept")
	}

	if strings.Contains(after, "span_id") {
		t.Fatalf("labels after establish = %q, want no span_id", after)
	}
}

func TestSpanLabelsRestoredAfterPanic(t *testing.T) {
	_, mw := labelledSetup(t, sdktrace.AlwaysSample(), true)

	var id string

	func() {
		defer func() { _ = recover() }()

		serve(t, mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id = spanIDOf(r)

			panic("boom")
		})), http.MethodGet, "/x")
	}()

	if l := labelsWithSpan(t, id); l != "" {
		t.Fatalf("labels survived the panic: %q", l)
	}
}

func TestSpanLabelsMergeWithHandlerLabels(t *testing.T) {
	_, mw := labelledSetup(t, sdktrace.AlwaysSample(), true)

	var inner, after string

	serve(t, mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := spanIDOf(r)

		pprof.Do(r.Context(), pprof.Labels("tenant", "acme"), func(context.Context) {
			inner = labelsWithSpan(t, id)
		})

		after = labelsWithSpan(t, id)
	})), http.MethodGet, "/x")

	if !strings.Contains(inner, `"tenant":"acme"`) {
		t.Fatalf("inside the handler's Do, labels = %q, want span_id and tenant", inner)
	}

	if after == "" || strings.Contains(after, "tenant") {
		t.Fatalf("after the handler's Do, labels = %q, want span_id without tenant", after)
	}
}

func TestSpanLabelsConcurrentRequestsKeepTheirOwn(t *testing.T) {
	_, mw := labelledSetup(t, sdktrace.AlwaysSample(), true)

	const n = 8

	var (
		ready sync.WaitGroup
		done  = make(chan struct{})
		mu    sync.Mutex
		wrong []string
		wg    sync.WaitGroup
	)

	ready.Add(n)

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := spanIDOf(r)

		ready.Done()
		<-done // hold every request open at once

		if l := labelsWithSpan(t, id); strings.Count(l, "span_id") != 1 {
			mu.Lock()

			wrong = append(wrong, l)

			mu.Unlock()
		}
	}))

	for range n {
		wg.Go(func() {
			serve(t, h, http.MethodGet, "/x")
		})
	}

	ready.Wait()
	close(done)
	wg.Wait()

	if len(wrong) > 0 {
		t.Fatalf("requests saw labels %v", wrong)
	}
}

// A request on an excluded path gets no span, so it gets no labels.
func TestSpanLabelsSkipExcludedPaths(t *testing.T) {
	_, mw := labelledSetup(t, sdktrace.AlwaysSample(), true)

	var lines []string

	serve(t, mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines = labelLines(t)
	})), http.MethodGet, "/_/health")

	for _, l := range lines {
		if strings.Contains(l, "span_id") {
			t.Fatalf("excluded path carries %q", l)
		}
	}
}
