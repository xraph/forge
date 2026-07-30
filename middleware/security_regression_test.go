package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A wildcard entry like "*.example.com" was matched with strings.HasSuffix, so
// any domain merely ending in "example.com" was accepted. Since getAllowOrigin
// reflects the request origin back and this path is reachable with
// AllowCredentials, an attacker could register a matching domain and read
// authenticated responses.
func TestCORSWildcardMatchesOnLabelBoundaries(t *testing.T) {
	allowed := []string{"*.example.com", "https://exact.test:8443"}

	deny := map[string]string{
		"https://evil-example.com": "hyphen prefix is a different domain",
		"https://notexample.com":   "no label separator",
		"https://wwwexample.com":   "no label separator",
		"https://example.com":      "apex is not a subdomain",
		"https://exact.test":       "port must match an exact entry",
		"not-a-url":                "unparseable origin",
		"":                         "empty origin",
	}

	for origin, why := range deny {
		if isOriginAllowed(origin, allowed) {
			t.Errorf("origin %q should be denied (%s)", origin, why)
		}
	}

	permit := []string{
		"https://app.example.com",
		"http://deep.nested.example.com",
		"https://APP.EXAMPLE.COM",
		"https://exact.test:8443",
	}

	for _, origin := range permit {
		if !isOriginAllowed(origin, allowed) {
			t.Errorf("origin %q should be allowed", origin)
		}
	}
}

// The limiter keyed on RemoteAddr, which Go sets to "IP:ephemeral-port". A new
// port per connection meant a new bucket per connection: no effective limit, and
// unbounded map growth driven by the client.
func TestRateLimitKeyIgnoresEphemeralPort(t *testing.T) {
	for port := range 5 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = fmt.Sprintf("203.0.113.7:%d", 40000+port)

		if got := ClientIP(r); got != "203.0.113.7" {
			t.Fatalf("ClientIP(%q) = %q, want 203.0.113.7", r.RemoteAddr, got)
		}
	}

	limiter := NewRateLimiter(1, 3) // 1/s, burst 3
	defer limiter.Stop()

	allowed := 0

	for port := range 100 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = fmt.Sprintf("203.0.113.7:%d", 40000+port)

		if limiter.Allow(ClientIP(r)) {
			allowed++
		}
	}

	if allowed > 3 {
		t.Errorf("rate limit bypassed: %d/100 requests allowed from one IP, burst is 3", allowed)
	}

	if len(limiter.buckets) != 1 {
		t.Errorf("bucket per connection: %d buckets tracked for a single IP", len(limiter.buckets))
	}
}

// Bucket count must be bounded; the map was previously attacker-driven.
func TestRateLimiterBoundsTrackedKeys(t *testing.T) {
	limiter := NewRateLimiter(1, 5)
	defer limiter.Stop()

	limiter.SetMaxBuckets(10)

	for i := range 500 {
		limiter.Allow(fmt.Sprintf("10.0.0.%d", i))
	}

	if len(limiter.buckets) > 10 {
		t.Errorf("tracked %d keys, cap is 10", len(limiter.buckets))
	}
}

// Stop must be idempotent — it is a natural thing to call from a shutdown hook
// that may run more than once.
func TestRateLimiterStopIsIdempotent(t *testing.T) {
	limiter := NewRateLimiter(1, 1)

	limiter.Stop()
	limiter.Stop()
}

// The Timeout middleware buffers responses so it can substitute a 504. It must
// stop buffering when the handler flushes, or streaming responses stall until
// the handler returns.
func TestTimeoutMiddlewarePassesFlushThrough(t *testing.T) {
	var sawFlusher bool

	h := Timeout(time.Second, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		sawFlusher = ok

		if !ok {
			return
		}

		_, _ = w.Write([]byte("chunk"))

		f.Flush()
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))

	if !sawFlusher {
		t.Fatal("handler did not receive an http.Flusher; streaming is broken through Timeout")
	}

	if body := rec.Body.String(); body != "chunk" {
		t.Errorf("body = %q, want %q", body, "chunk")
	}
}

// A response larger than the buffer limit must not be held in memory.
func TestTimeoutMiddlewareBoundsItsBuffer(t *testing.T) {
	payload := make([]byte, DefaultTimeoutBufferLimit+4096)
	for i := range payload {
		payload[i] = 'x'
	}

	h := Timeout(5*time.Second, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/big", nil))

	if got := rec.Body.Len(); got != len(payload) {
		t.Errorf("wrote %d bytes, want %d", got, len(payload))
	}
}

// An inbound X-Request-ID is echoed into the response and every log line, so it
// must be bounded and free of characters that would break log parsing.
func TestRequestIDRejectsHostileInboundValues(t *testing.T) {
	reject := map[string]string{
		"":     "empty",
		"a b":  "contains a space",
		"a\tb": "contains a tab",
		"\x00": "contains a NUL",
	}

	for v, why := range reject {
		if got := sanitizeRequestID(v); got != "" {
			t.Errorf("sanitizeRequestID(%q) = %q, want rejected (%s)", v, got, why)
		}
	}

	long := make([]byte, maxRequestIDLength+1)
	for i := range long {
		long[i] = 'a'
	}

	if got := sanitizeRequestID(string(long)); got != "" {
		t.Errorf("over-length ID accepted (%d chars)", len(got))
	}

	const ok = "7f3a9c21-4b6e-11ee-be56-0242ac120002"
	if got := sanitizeRequestID(ok); got != ok {
		t.Errorf("sanitizeRequestID(%q) = %q, want it preserved", ok, got)
	}
}

// Sensitive headers must be redacted when header logging is on; the config field
// existed but was never read.
func TestLoggingRedactsSensitiveHeaders(t *testing.T) {
	sensitive := map[string]bool{"authorization": true, "cookie": true}

	header := http.Header{}
	header.Set("Authorization", "Bearer super-secret")
	header.Set("X-Trace-Id", "trace-1")

	// Assigned directly rather than via Set, which would canonicalize it. Header
	// names are case-insensitive, so redaction must match regardless of casing.
	header["cookie"] = []string{"session=abc123"}

	got := redactHeaders(header, sensitive)

	for _, name := range []string{"Authorization", "cookie"} {
		if v := got[name]; v != redactedValue {
			t.Errorf("header %s = %q, want %q", name, v, redactedValue)
		}
	}

	if got["X-Trace-Id"] != "trace-1" {
		t.Errorf("non-sensitive header was altered: %q", got["X-Trace-Id"])
	}
}
