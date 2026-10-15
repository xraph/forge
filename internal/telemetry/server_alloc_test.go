package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestServerSpanServerAddress(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"example.com", "example.com"},
		{"example.com:8080", "example.com"},
		{"10.0.0.1", "10.0.0.1"},
		{"10.0.0.1:9000", "10.0.0.1"},
		{"[::1]:443", "::1"},
		{"[::1]", "[::1]"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			exp, mw := setup(t, Settings{})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
			req.Host = tc.host
			mw(routed("/x", http.StatusOK)).ServeHTTP(httptest.NewRecorder(), req)

			got, ok := attrOf(exp.GetSpans()[0].Attributes, "server.address")
			if !ok || got.AsString() != tc.want {
				t.Fatalf("server.address = %q (set: %v), want %q", got.AsString(), ok, tc.want)
			}
		})
	}
}

// One middleware names spans for many routes and methods, and the names must
// not bleed between them.
func TestServerSpanNamesPerMethodAndRoute(t *testing.T) {
	exp, mw := setup(t, Settings{})

	cases := []struct{ method, pattern, want string }{
		{http.MethodGet, "/users/:id", "GET /users/:id"},
		{http.MethodPost, "/users/:id", "POST /users/:id"},
		{http.MethodGet, "/orders", "GET /orders"},
		{http.MethodGet, "/users/:id", "GET /users/:id"},
	}

	for _, c := range cases {
		mw(routed(c.pattern, http.StatusOK)).ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequestWithContext(t.Context(), c.method, "/x", nil))
	}

	spans := exp.GetSpans()
	for i, c := range cases {
		if spans[i].Name != c.want {
			t.Fatalf("span %d named %q, want %q", i, spans[i].Name, c.want)
		}
	}
}

type headerOnlyWriter struct{ h http.Header }

func (w headerOnlyWriter) Header() http.Header         { return w.h }
func (w headerOnlyWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w headerOnlyWriter) WriteHeader(int)             {}

// The tracing middleware runs on every request. Of its 15 allocations, 5 are
// its own: the per-request state, the traceparent string the IDs are sliced
// from, the header value slice, the establish callback and the attribute
// slice. The OpenTelemetry SDK makes 3 (the span, its attribute copy and the
// span on the context), and 7 are context and request copies: the
// route-match holder and its context, the request copy, and 4 for the log
// trace and span IDs, which go-utils v1.4.0 boxes separately. If the count
// climbs, the hot path regressed.
func TestServerMiddlewareAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates on its own; counts are only meaningful without -race")
	}

	// The propagator forge installs, so Extract costs what it costs in an app.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))

	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	h := ServerMiddleware(tp, Settings{ResponseHeader: "traceparent"})(routed("/users/:id", http.StatusOK))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/7", nil)
	w := headerOnlyWriter{h: http.Header{}}

	h.ServeHTTP(w, req)

	// AllocsPerRun counts every allocation in the process, so goroutines
	// other tests left running can add to it. They only ever add, so the
	// lowest of a few rounds is the middleware's own count.
	allocs := testing.AllocsPerRun(200, func() { h.ServeHTTP(w, req) })
	for range 4 {
		allocs = min(allocs, testing.AllocsPerRun(200, func() { h.ServeHTTP(w, req) }))
	}

	t.Logf("tracing middleware: %.0f allocs per request", allocs)

	if allocs > maxServerMiddlewareAllocs {
		t.Fatalf("tracing middleware allocated %.0f times per request, want at most %d", allocs, maxServerMiddlewareAllocs)
	}
}

const maxServerMiddlewareAllocs = 15

// The propagators read lowercase keys; the carrier must find the canonical
// header without allocating a canonicalized key per lookup.
func TestHeaderCarrierGet(t *testing.T) {
	h := http.Header{}
	h.Set("Traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	h.Set("Baggage", "k=v")
	h.Set("X-Custom", "yes")

	c := headerCarrier(h)

	for key, want := range map[string]string{
		"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		"baggage":     "k=v",
		"tracestate":  "",
		"x-custom":    "yes",
	} {
		if got := c.Get(key); got != want {
			t.Fatalf("Get(%q) = %q, want %q", key, got, want)
		}
	}

	if allocs := testing.AllocsPerRun(100, func() {
		_ = c.Get("traceparent")
		_ = c.Get("tracestate")
		_ = c.Get("baggage")
	}); allocs != 0 {
		t.Fatalf("Get on the propagator keys allocated %.0f times, want 0", allocs)
	}
}

// The request attributes are set when the span ends. Every one of them must
// still be on the exported span, in the order it had when they were set at
// start.
func TestServerSpanCarriesRequestAttributes(t *testing.T) {
	exp, mw := setup(t, Settings{})

	req := httptest.NewRequestWithContext(t.Context(), "PURGE", "/users/7", nil)
	req.Host = "api.example.com:8443"
	req.Header.Set("User-Agent", "probe/1.0")

	mw(routed("/users/:id", http.StatusOK)).ServeHTTP(httptest.NewRecorder(), req)

	want := []string{
		"http.request.method", "url.scheme", "url.path", "network.protocol.version",
		"http.request.method_original", "server.address", "user_agent.original",
		"http.route", "http.response.status_code",
	}

	attrs := exp.GetSpans()[0].Attributes
	if len(attrs) != len(want) {
		t.Fatalf("span has %d attributes, want %d: %v", len(attrs), len(want), attrs)
	}

	for i, key := range want {
		if string(attrs[i].Key) != key {
			t.Fatalf("attribute %d is %q, want %q", i, attrs[i].Key, key)
		}
	}

	for key, value := range map[string]string{
		"http.request.method":          "_OTHER",
		"http.request.method_original": "PURGE",
		"url.path":                     "/users/7",
		"server.address":               "api.example.com",
		"user_agent.original":          "probe/1.0",
	} {
		if got, _ := attrOf(attrs, key); got.AsString() != value {
			t.Fatalf("%s = %q, want %q", key, got.AsString(), value)
		}
	}
}

// A panicking handler never reaches finish; its span still carries the
// request attributes.
func TestServerSpanPanicKeepsRequestAttributes(t *testing.T) {
	exp, mw := setup(t, Settings{})

	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") })

	func() {
		defer func() { _ = recover() }()

		mw(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/boom", nil))
	}()

	attrs := exp.GetSpans()[0].Attributes
	for _, key := range []string{"http.request.method", "url.scheme", "url.path", "server.address"} {
		if _, ok := attrOf(attrs, key); !ok {
			t.Fatalf("panicked span is missing %s: %v", key, attrs)
		}
	}
}
