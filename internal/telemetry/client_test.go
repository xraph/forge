package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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

// quotingError stands in for an error whose text quotes a secret.
type quotingError struct{ dsn string }

func (e *quotingError) Error() string { return "bad dsn " + e.dsn }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// clientTracing installs a recording provider as the global, which
// InstrumentTransport captures when it is called.
func clientTracing(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	otel.SetTextMapPropagator(propagation.TraceContext{})

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	return exp
}

func TestHTTPClientScrubsTheURL(t *testing.T) {
	exp := clientTracing(t)

	var gotURL, gotTraceparent string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.RequestURI()
		gotTraceparent = r.Header.Get("Traceparent")
	}))
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	u.User = url.UserPassword("svc", "SECRET-PASS")
	u.Path = "/v1/items"
	u.RawQuery = "token=SECRET-TOKEN&page=2"
	u.Fragment = "SECRET-FRAGMENT"

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, u.String(), nil)

	resp, err := HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if gotURL != "/v1/items?token=SECRET-TOKEN&page=2" {
		t.Fatalf("the server got %q, want the real path and query", gotURL)
	}

	if gotTraceparent == "" {
		t.Fatal("the request lost its traceparent")
	}

	if resp.Request.URL.RawQuery != "token=SECRET-TOKEN&page=2" {
		t.Fatalf("resp.Request.URL = %s, want the real URL", resp.Request.URL)
	}

	s := exp.GetSpans()[0]

	if leaks := spanLeaks(s, "SECRET"); len(leaks) > 0 {
		t.Fatalf("the client span leaks a secret in %v", leaks)
	}

	want := srv.URL + "/v1/items?token=REDACTED&page=REDACTED"
	if v, _ := attrOf(s.Attributes, "url.full"); v.AsString() != want {
		t.Fatalf("url.full = %q, want %q", v.AsString(), want)
	}
}

// Each part of a URL that needs masking triggers it on its own. A URL with
// only userinfo, only a fragment or only a query must not reach url.full as
// sent.
func TestHTTPClientScrubsEachURLPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		name string
		edit func(*url.URL)
		want string
	}{
		{"userinfo", func(u *url.URL) { u.User = url.UserPassword("svc", "SECRET-PASS") }, "/v1/items"},
		{"fragment", func(u *url.URL) { u.Fragment = "SECRET-FRAGMENT" }, "/v1/items"},
		{"bare query key", func(u *url.URL) { u.RawQuery = "SECRET-TOKEN" }, "/v1/items?REDACTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exp := clientTracing(t)

			u, _ := url.Parse(srv.URL)
			u.Path = "/v1/items"
			tc.edit(u)

			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, u.String(), nil)

			resp, err := HTTPClient().Do(req)
			if err != nil {
				t.Fatal(err)
			}

			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()

			s := exp.GetSpans()[0]

			if leaks := spanLeaks(s, "SECRET"); len(leaks) > 0 {
				t.Fatalf("the client span leaks a secret in %v", leaks)
			}

			if v, _ := attrOf(s.Attributes, "url.full"); v.AsString() != srv.URL+tc.want {
				t.Fatalf("url.full = %q, want %q", v.AsString(), srv.URL+tc.want)
			}
		})
	}
}

func TestInstrumentTransportMasksTransportErrors(t *testing.T) {
	exp := clientTracing(t)

	sent := fmt.Errorf("dial postgres://u:SECRET@db:5432: %w", &quotingError{dsn: "SECRET"})
	rt := InstrumentTransport(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, sent }))

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://db.internal/x?k=v", nil)

	_, err := rt.RoundTrip(req) //nolint:bodyclose // the request fails, there is no body
	if !errors.Is(err, sent) {
		t.Fatalf("the caller got %v, want the original error", err)
	}

	s := exp.GetSpans()[0]

	if s.Status.Code != codes.Error || s.Status.Description != "*telemetry.quotingError" {
		t.Fatalf("status = %v %q", s.Status.Code, s.Status.Description)
	}

	if v, _ := attrOf(s.Attributes, "error.type"); v.AsString() != "*telemetry.quotingError" {
		t.Fatalf("error.type = %q", v.AsString())
	}

	if leaks := spanLeaks(s, "SECRET"); len(leaks) > 0 {
		t.Fatalf("the client span leaks the error text in %v", leaks)
	}
}

type failingBody struct{ err error }

func (b failingBody) Read([]byte) (int, error) { return 0, b.err }
func (failingBody) Close() error               { return nil }

func TestInstrumentTransportMasksBodyErrors(t *testing.T) {
	exp := clientTracing(t)

	sent := fmt.Errorf("read tcp 10.0.0.1:443: %w", &quotingError{dsn: "SECRET"})
	rt := InstrumentTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: failingBody{err: sent}, Request: r}, nil
	}))

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://api.internal/x", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := io.ReadAll(resp.Body); !errors.Is(err, sent) {
		t.Fatalf("the caller read %v, want the original error", err)
	}

	_ = resp.Body.Close()

	s := exp.GetSpans()[0]

	if s.Status.Code != codes.Error || s.Status.Description != "*telemetry.quotingError" {
		t.Fatalf("status = %v %q", s.Status.Code, s.Status.Description)
	}

	if leaks := spanLeaks(s, "SECRET"); len(leaks) > 0 {
		t.Fatalf("the client span leaks the body error in %v", leaks)
	}
}

type rwBody struct {
	io.Reader

	written []byte
}

func (b *rwBody) Write(p []byte) (int, error) {
	b.written = append(b.written, p...)

	return len(p), nil
}

func (*rwBody) Close() error { return nil }

// A 101 response body is an io.ReadWriteCloser, and WebSocket clients built
// on http.Client write to it. The masking must not hide the Write.
func TestInstrumentTransportKeepsReadWriteBodies(t *testing.T) {
	clientTracing(t)

	body := &rwBody{Reader: strings.NewReader("")}
	rt := InstrumentTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusSwitchingProtocols, Body: body, Request: r}, nil
	}))

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://ws.internal/socket?ticket=abc", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	w, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		t.Fatalf("body %T is no longer an io.ReadWriteCloser", resp.Body)
	}

	if _, err := w.Write([]byte("hi")); err != nil || string(body.written) != "hi" {
		t.Fatalf("write: %v, body got %q", err, body.written)
	}
}

// A request made with the context of one that was masked further up must
// keep its own URL.
func TestInstrumentTransportIgnoresAnInheritedURL(t *testing.T) {
	clientTracing(t)

	var got string

	rt := InstrumentTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.URL.String()

		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: r}, nil
	}))

	outer, _ := url.Parse("http://outer.internal/a?token=outer")
	ctx := context.WithValue(t.Context(), realURLKey{}, outer)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://inner.internal/b", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if got != "http://inner.internal/b" {
		t.Fatalf("base sent %q, want the request's own URL", got)
	}
}
