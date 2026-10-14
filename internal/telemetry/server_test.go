package telemetry

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

func setup(t *testing.T, s Settings) (*tracetest.InMemoryExporter, func(http.Handler) http.Handler) {
	t.Helper()

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.AlwaysSample()))

	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	if s.ExcludePaths == nil {
		s.ExcludePaths = []string{"/_/"}
	}

	return exp, ServerMiddleware(tp, s)
}

// routed simulates the router: it stamps a pattern and writes a status.
func routed(pattern string, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m := shared.RouteMatchFromContext(r.Context()); m != nil && pattern != "" {
			m.Pattern = pattern
		}

		w.WriteHeader(status)
	})
}

func attrOf(attrs []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value, true
		}
	}

	return attribute.Value{}, false
}

func TestServerSpanRenamedAfterRouting(t *testing.T) {
	exp, mw := setup(t, Settings{ResponseHeader: "traceparent"})

	rec := httptest.NewRecorder()
	mw(routed("/users/:id", http.StatusOK)).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/42", nil))

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}

	s := spans[0]
	if s.Name != "GET /users/:id" {
		t.Fatalf("name = %q", s.Name)
	}

	if v, _ := attrOf(s.Attributes, "http.route"); v.AsString() != "/users/:id" {
		t.Fatalf("http.route = %q", v.AsString())
	}

	if v, _ := attrOf(s.Attributes, "http.response.status_code"); v.AsInt64() != 200 {
		t.Fatalf("status attr = %d", v.AsInt64())
	}

	if s.Status.Code == codes.Error {
		t.Fatal("200 must not be an error")
	}

	if rec.Header().Get("Traceparent") == "" {
		t.Fatal("response is missing traceparent")
	}
}

func TestServerSpanUnmatchedKeepsMethodName(t *testing.T) {
	exp, mw := setup(t, Settings{})
	mw(routed("", http.StatusNotFound)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))

	s := exp.GetSpans()[0]
	if s.Name != "GET" {
		t.Fatalf("name = %q", s.Name)
	}

	if _, ok := attrOf(s.Attributes, "http.route"); ok {
		t.Fatal("unmatched request must not carry http.route")
	}
}

func TestServerSpanErrorOn5xxOnly(t *testing.T) {
	exp, mw := setup(t, Settings{})
	mw(routed("/a", http.StatusBadRequest)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil))
	mw(routed("/b", http.StatusBadGateway)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/b", nil))

	spans := exp.GetSpans()
	if spans[0].Status.Code == codes.Error {
		t.Fatal("400 must not set Error status")
	}

	if spans[1].Status.Code != codes.Error {
		t.Fatal("502 must set Error status")
	}

	if v, _ := attrOf(spans[1].Attributes, "error.type"); v.AsString() != "502" {
		t.Fatalf("error.type = %q", v.AsString())
	}
}

func TestServerSpanUnknownMethod(t *testing.T) {
	exp, mw := setup(t, Settings{})
	mw(routed("", http.StatusMethodNotAllowed)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), "FOOBAR", "/", nil))

	s := exp.GetSpans()[0]
	if v, _ := attrOf(s.Attributes, "http.request.method"); v.AsString() != "_OTHER" {
		t.Fatalf("method = %q", v.AsString())
	}

	if v, _ := attrOf(s.Attributes, "http.request.method_original"); v.AsString() != "FOOBAR" {
		t.Fatalf("method_original = %q", v.AsString())
	}

	if s.Name != "HTTP" {
		t.Fatalf("name = %q, semconv uses HTTP for _OTHER", s.Name)
	}
}

func TestServerSpanExcludedPath(t *testing.T) {
	exp, mw := setup(t, Settings{})
	mw(routed("/_/health", http.StatusOK)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_/health", nil))

	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("excluded path produced %d spans", n)
	}
}

func TestServerSpanJoinsIncomingTrace(t *testing.T) {
	exp, mw := setup(t, Settings{})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	mw(routed("/a", http.StatusOK)).ServeHTTP(httptest.NewRecorder(), req)

	s := exp.GetSpans()[0]
	if s.SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %s", s.SpanContext.TraceID())
	}

	if s.Parent.SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("parent = %s", s.Parent.SpanID())
	}
}

func TestServerSpanMalformedTraceparentStartsNewTrace(t *testing.T) {
	exp, mw := setup(t, Settings{})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil)
	req.Header.Set("Traceparent", "00-zzzz-not-hex-01")

	rec := httptest.NewRecorder()
	mw(routed("/a", http.StatusOK)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	s := exp.GetSpans()[0]
	if !s.SpanContext.TraceID().IsValid() || s.Parent.IsValid() {
		t.Fatal("want a fresh root trace")
	}
}

func TestServerSpanPutsIDsOnLogContext(t *testing.T) {
	exp, mw := setup(t, Settings{})

	var gotTrace, gotSpan string

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTrace = logger.TraceIDFromContext(r.Context())
		gotSpan = logger.SpanIDFromContext(r.Context())
	})

	mw(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil))

	sc := exp.GetSpans()[0].SpanContext
	if gotTrace != sc.TraceID().String() || gotSpan != sc.SpanID().String() {
		t.Fatalf("log ids %s/%s, span %s/%s", gotTrace, gotSpan, sc.TraceID(), sc.SpanID())
	}
}

func TestServerSpanResponseHeaderDisabled(t *testing.T) {
	_, mw := setup(t, Settings{ResponseHeader: ""})

	rec := httptest.NewRecorder()
	mw(routed("/a", http.StatusOK)).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil))

	if rec.Header().Get("Traceparent") != "" {
		t.Fatal("traceparent set although response_header is empty")
	}
}

func TestServerSpanPanicRecordedAndRethrown(t *testing.T) {
	exp, mw := setup(t, Settings{})

	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") })

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic was swallowed")
			}
		}()

		mw(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil))
	}()

	s := exp.GetSpans()[0]
	if s.Status.Code != codes.Error || len(s.Events) == 0 {
		t.Fatalf("status %v events %v", s.Status, s.Events)
	}
}

func TestServerSpanEmptyHandlerIs200(t *testing.T) {
	exp, mw := setup(t, Settings{})
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil))

	if v, _ := attrOf(exp.GetSpans()[0].Attributes, "http.response.status_code"); v.AsInt64() != 200 {
		t.Fatalf("status = %d", v.AsInt64())
	}
}

func TestServerMiddlewareKeepsFlusher(t *testing.T) {
	_, mw := setup(t, Settings{})

	flushed := false
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter lost http.Flusher")
		}

		_, _ = w.Write([]byte("data: hi\n\n"))

		f.Flush()

		flushed = true
	})

	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/events", nil))

	if !flushed || !rec.Flushed {
		t.Fatal("flush did not reach the underlying writer")
	}
}

func TestServerMiddlewareKeepsHijacker(t *testing.T) {
	exp, mw := setup(t, Settings{})

	srv := httptest.NewServer(mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("ResponseWriter lost http.Hijacker")

			return
		}

		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Error(err)

			return
		}
		defer conn.Close()

		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\n")
		_ = buf.Flush()
	})))
	t.Cleanup(srv.Close)

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, _ = conn.Write([]byte("GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))

	line, _ := bufio.NewReader(conn).ReadString('\n')
	if line != "HTTP/1.1 101 Switching Protocols\r\n" {
		t.Fatalf("got %q", line)
	}

	srv.Close() // wait for the handler, so the span has ended

	if v, _ := attrOf(exp.GetSpans()[0].Attributes, "http.response.status_code"); v.AsInt64() != 101 {
		t.Fatalf("status = %d, want 101 for a hijacked connection", v.AsInt64())
	}
}

func TestPropagationMiddlewareExtractsOnly(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var got trace.SpanContext

	h := PropagationMiddleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = trace.SpanContextFromContext(r.Context())
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || !got.IsRemote() {
		t.Fatalf("span context = %v, want the remote caller's", got)
	}

	if rec.Header().Get("Traceparent") != "" {
		t.Fatal("PropagationMiddleware wrote a response header")
	}
}

// The response header is built by hand for speed, so check it against what
// the TraceContext propagator writes for the same span context.
func TestServerResponseHeaderMatchesTraceContext(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sampler sdktrace.Sampler
	}{
		{"sampled", sdktrace.AlwaysSample()},
		{"unsampled", sdktrace.NeverSample()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(tc.sampler))

			t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

			var sc trace.SpanContext

			h := ServerMiddleware(tp, Settings{ResponseHeader: "traceparent"})(http.HandlerFunc(
				func(_ http.ResponseWriter, r *http.Request) { sc = trace.SpanContextFromContext(r.Context()) }))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil))

			carrier := propagation.MapCarrier{}
			propagation.TraceContext{}.Inject(trace.ContextWithSpanContext(t.Context(), sc), carrier)

			want := carrier.Get("traceparent")
			if want == "" || rec.Header().Get("Traceparent") != want {
				t.Fatalf("traceparent = %q, propagator writes %q", rec.Header().Get("Traceparent"), want)
			}

			if sc.IsSampled() != (tc.name == "sampled") {
				t.Fatalf("sampled = %v", sc.IsSampled())
			}
		})
	}
}
