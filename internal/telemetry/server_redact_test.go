package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// spanLeaks returns every place on s that contains needle: the name, the
// status description, an attribute, an event or an event attribute.
func spanLeaks(s tracetest.SpanStub, needle string) []string {
	var found []string

	if strings.Contains(s.Name, needle) {
		found = append(found, "name")
	}

	if strings.Contains(s.Status.Description, needle) {
		found = append(found, "status")
	}

	for _, kv := range s.Attributes {
		if strings.Contains(kv.Value.String(), needle) {
			found = append(found, string(kv.Key))
		}
	}

	for _, e := range s.Events {
		if strings.Contains(e.Name, needle) {
			found = append(found, "event "+e.Name)
		}

		for _, kv := range e.Attributes {
			if strings.Contains(kv.Value.String(), needle) {
				found = append(found, "event "+e.Name+" "+string(kv.Key))
			}
		}
	}

	return found
}

// leakyRequest has a secret in a path parameter, in a query value and in the
// user agent, the way a session ID, a dashboard ticket and a browser string
// arrive.
func leakyRequest(t *testing.T) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/sessions/SECRET-SESSION?t=SECRET-TICKET&page=2", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (SECRET-UA)")

	return req
}

func TestServerSpanKeepsRequestSecretsOff(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		exp, mw := setup(t, Settings{})

		mw(routed("/v1/sessions/:id", status)).ServeHTTP(httptest.NewRecorder(), leakyRequest(t))

		s := exp.GetSpans()[0]

		if leaks := spanLeaks(s, "SECRET"); len(leaks) > 0 {
			t.Fatalf("status %d: the span leaks a secret in %v", status, leaks)
		}

		if s.Name != "GET /v1/sessions/:id" {
			t.Fatalf("name = %q", s.Name)
		}

		for key, want := range map[string]string{
			"http.route": "/v1/sessions/:id",
			"url.path":   "/v1/sessions/:id",
		} {
			if v, _ := attrOf(s.Attributes, key); v.AsString() != want {
				t.Fatalf("%s = %q, want %q", key, v.AsString(), want)
			}
		}

		if v, _ := attrOf(s.Attributes, "http.response.status_code"); v.AsInt64() != int64(status) {
			t.Fatalf("status code = %d, want %d", v.AsInt64(), status)
		}

		for _, key := range []string{"url.query", "url.full", "user_agent.original"} {
			if _, ok := attrOf(s.Attributes, key); ok {
				t.Fatalf("%s is recorded by default", key)
			}
		}
	}
}

func TestServerSpanUnmatchedOmitsPath(t *testing.T) {
	exp, mw := setup(t, Settings{})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/files/SECRET", nil)
	mw(routed("", http.StatusNotFound)).ServeHTTP(httptest.NewRecorder(), req)

	s := exp.GetSpans()[0]

	if _, ok := attrOf(s.Attributes, "url.path"); ok {
		t.Fatal("an unmatched request recorded url.path")
	}

	if v, _ := attrOf(s.Attributes, "http.response.status_code"); v.AsInt64() != http.StatusNotFound {
		t.Fatalf("status code = %d", v.AsInt64())
	}

	if leaks := spanLeaks(s, "SECRET"); len(leaks) > 0 {
		t.Fatalf("the span leaks the path in %v", leaks)
	}
}

func TestServerSpanOptInsRestoreRawValues(t *testing.T) {
	exp, mw := setup(t, Settings{RecordRawPath: true, RecordUserAgent: true, AllowedQueryKeys: []string{"page"}})

	mw(routed("/v1/sessions/:id", http.StatusOK)).ServeHTTP(httptest.NewRecorder(), leakyRequest(t))
	mw(routed("", http.StatusNotFound)).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope/42", nil))

	spans := exp.GetSpans()

	for key, want := range map[string]string{
		"url.path":            "/v1/sessions/SECRET-SESSION",
		"url.query":           "t=REDACTED&page=2",
		"user_agent.original": "Mozilla/5.0 (SECRET-UA)",
		"http.route":          "/v1/sessions/:id",
	} {
		if v, _ := attrOf(spans[0].Attributes, key); v.AsString() != want {
			t.Fatalf("%s = %q, want %q", key, v.AsString(), want)
		}
	}

	// The name stays the route even with the raw path on.
	if spans[0].Name != "GET /v1/sessions/:id" {
		t.Fatalf("name = %q", spans[0].Name)
	}

	if v, _ := attrOf(spans[1].Attributes, "url.path"); v.AsString() != "/nope/42" {
		t.Fatalf("unmatched url.path = %q, want the raw path", v.AsString())
	}

	if spans[1].Name != "GET" {
		t.Fatalf("unmatched name = %q", spans[1].Name)
	}
}

func TestServerSpanQueryWithoutAllowedKeysIsOmitted(t *testing.T) {
	exp, mw := setup(t, Settings{RecordRawPath: true})

	mw(routed("/v1/sessions/:id", http.StatusOK)).ServeHTTP(httptest.NewRecorder(), leakyRequest(t))

	if _, ok := attrOf(exp.GetSpans()[0].Attributes, "url.query"); ok {
		t.Fatal("url.query recorded without allowed_query_keys")
	}
}

func TestScrubQuery(t *testing.T) {
	allowed := keySet([]string{"page", "sort by"})

	for _, tc := range []struct{ raw, want string }{
		{"", ""},
		{"t=abc", "t=REDACTED"},
		{"page=2", "page=2"},
		{"page=2&t=abc&page=3", "page=2&t=REDACTED&page=3"},
		{"flag&t=abc", "REDACTED&t=REDACTED"},
		{"page&t=abc", "page&t=REDACTED"},
		{"a=1&&b=2", "a=REDACTED&&b=REDACTED"},
		{"t=&page=", "t=&page="},
		{"sort+by=name&sort%20by=date", "sort+by=name&sort%20by=date"},
		{"pa%67e=2", "pa%67e=2"},
		{"%zz=1", "%zz=REDACTED"},
		{"a=b=c", "a=REDACTED"},
	} {
		if got := scrubQuery(tc.raw, allowed); got != tc.want {
			t.Errorf("scrubQuery(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}

	if got := scrubQuery("page=2&t=abc&eyJhbGciOi", nil); got != "page=REDACTED&t=REDACTED&REDACTED" {
		t.Errorf("nil allowed: %q", got)
	}
}

// A panic that escapes the router reaches the span as "panic" with a stack
// trace, never the panic value.
func TestServerSpanPanicValueStaysOff(t *testing.T) {
	exp, mw := setup(t, Settings{})

	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("session SECRET expired") })

	func() {
		defer func() { _ = recover() }()

		mw(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil))
	}()

	s := exp.GetSpans()[0]

	if s.Status.Description != "panic" {
		t.Fatalf("status description = %q", s.Status.Description)
	}

	if leaks := spanLeaks(s, "SECRET"); len(leaks) > 0 {
		t.Fatalf("the span leaks the panic value in %v", leaks)
	}

	attrs := map[string]string{}
	for _, kv := range s.Events[0].Attributes {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}

	if attrs["exception.type"] != "panic" || attrs["exception.stacktrace"] == "" {
		t.Fatalf("exception event = %v, want type panic with a stack trace", attrs)
	}
}
