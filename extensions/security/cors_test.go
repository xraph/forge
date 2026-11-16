package security

import (
	"net/http"
	"net/http/httptest"
	"testing"

	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

// TestIsOriginAllowed_WildcardDotBoundary verifies that a "*.example.com"
// allowlist entry matches only genuine subdomains (with a real label boundary),
// not lookalike domains that merely share the suffix string.
func TestIsOriginAllowed_WildcardDotBoundary(t *testing.T) {
	m := &CORSManager{config: CORSConfig{
		AllowOrigins: []string{"https://app.example.com", "*.example.com"},
	}}

	cases := []struct {
		origin string
		want   bool
	}{
		{"https://app.example.com", true},       // exact allowlist entry
		{"https://foo.example.com", true},       // real subdomain
		{"https://bar.example.com", true},       // real subdomain
		{"https://a.b.example.com", true},       // nested subdomain
		{"https://example.com", true},           // apex equals the base domain
		{"https://foo.example.com:8443", true},  // port is ignored (host match)
		{"https://evilexample.com", false},      // lookalike, no label boundary
		{"https://example.com.evil.com", false}, // base embedded elsewhere
		{"https://evil.com", false},             // unrelated
		{"", false},                             // empty origin
		{"://bad", false},                       // malformed
	}
	for _, c := range cases {
		if got := m.isOriginAllowed(c.origin); got != c.want {
			t.Errorf("isOriginAllowed(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}

// TestIsOriginAllowed_NoWildcard confirms exact-match semantics are unchanged
// when no wildcard entry is configured.
func TestIsOriginAllowed_NoWildcard(t *testing.T) {
	m := &CORSManager{config: CORSConfig{AllowOrigins: []string{"https://app.example.com"}}}
	if m.isOriginAllowed("https://foo.example.com") {
		t.Fatal("subdomain must not be allowed without a wildcard entry")
	}

	if !m.isOriginAllowed("https://app.example.com") {
		t.Fatal("exact origin must be allowed")
	}
}

func corsResponse(t *testing.T, m *CORSManager, method, origin string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, "/api/me", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	_ = CORSMiddleware(m)(okHandler)(forge_http.NewContext(w, req, nil))

	return w
}

// AllowOrigins ["*"] with AllowCredentials used to pass Validate and reflect
// any Origin together with Access-Control-Allow-Credentials: true, letting
// every site read credentialed responses.
func TestCORS_WildcardWithCredentialsRejectedByValidate(t *testing.T) {
	cfg := DefaultConfig()
	WithCORSAllowCredentials(true)(&cfg)

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted AllowOrigins [*] with AllowCredentials")
	}

	WithCORSOrigins([]string{"https://app.example"})(&cfg)

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected an explicit origin list: %v", err)
	}
}

func TestCORS_NeverReflectsBecauseOfWildcard(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowCredentials = true // as if Validate had been skipped
	m := NewCORSManager(cfg, logger.NewNoopLogger())

	w := corsResponse(t, m, http.MethodGet, "https://evil.example", nil)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("ACAO = %q, want *", got)
	}

	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials allowed for an unlisted origin: %q", got)
	}
}

func TestCORS_ReflectedOriginSetsVary(t *testing.T) {
	for _, creds := range []bool{true, false} {
		cfg := SecureCORSConfig([]string{"https://app.example"})
		cfg.AllowCredentials = creds
		m := NewCORSManager(cfg, logger.NewNoopLogger())

		w := corsResponse(t, m, http.MethodGet, "https://app.example", nil)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
			t.Fatalf("creds=%v: ACAO = %q", creds, got)
		}

		if got := w.Header().Get("Vary"); got != "Origin" {
			t.Fatalf("creds=%v: Vary = %q, want Origin", creds, got)
		}

		if creds != (w.Header().Get("Access-Control-Allow-Credentials") == "true") {
			t.Fatalf("creds=%v: wrong Access-Control-Allow-Credentials", creds)
		}

		// A refused origin still varies, so a cache cannot hand the refusal
		// to the allowed origin or the other way round.
		w = corsResponse(t, m, http.MethodGet, "https://evil.example", nil)
		if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "Origin" {
			t.Fatalf("creds=%v: refused origin got ACAO %q Vary %q", creds,
				w.Header().Get("Access-Control-Allow-Origin"), w.Header().Get("Vary"))
		}
	}

	// A pure "*" policy answers the same for everyone and needs no Vary.
	w := corsResponse(t, NewCORSManager(DefaultCORSConfig(), logger.NewNoopLogger()), http.MethodGet, "https://a.example", nil)
	if w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("Vary") != "" {
		t.Fatalf("wildcard policy: ACAO %q Vary %q", w.Header().Get("Access-Control-Allow-Origin"), w.Header().Get("Vary"))
	}
}

func TestCORS_PreflightFiltersRequestedHeaders(t *testing.T) {
	m := NewCORSManager(SecureCORSConfig([]string{"https://app.example"}), logger.NewNoopLogger())

	w := corsResponse(t, m, http.MethodOptions, "https://app.example", map[string]string{
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "content-type, X-Evil, Authorization, CONTENT-TYPE",
	})

	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", w.Code)
	}

	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "content-type, Authorization" {
		t.Fatalf("Allow-Headers = %q", got)
	}

	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, PUT, DELETE" {
		t.Fatalf("Allow-Methods = %q", got)
	}

	if got := w.Header().Values("Vary"); len(got) != 1 || got[0] != "Origin, Access-Control-Request-Method, Access-Control-Request-Headers" {
		t.Fatalf("Vary = %q", got)
	}

	w = corsResponse(t, m, http.MethodOptions, "https://app.example", map[string]string{
		"Access-Control-Request-Method": "PATCH",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("disallowed method: status %d, want 403", w.Code)
	}
}

func TestCORS_PreflightAllocations(t *testing.T) {
	m := NewCORSManager(SecureCORSConfig([]string{"https://app.example"}), logger.NewNoopLogger())
	mw := CORSMiddleware(m)(okHandler)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/api/x", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, authorization")

	// The recorder and context dominate; before this change the preflight
	// itself added about 40 allocations for debug log fields.
	allocs := testing.AllocsPerRun(200, func() {
		_ = mw(forge_http.NewContext(httptest.NewRecorder(), req, nil))
	})
	if allocs > 30 {
		t.Fatalf("preflight allocated %.0f times, want at most 30", allocs)
	}
}

func BenchmarkCORSPreflight(b *testing.B) {
	m := NewCORSManager(SecureCORSConfig([]string{"https://app.example"}), logger.NewNoopLogger())
	mw := CORSMiddleware(m)(okHandler)
	req := httptest.NewRequestWithContext(b.Context(), http.MethodOptions, "/api/x", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, authorization")
	b.ReportAllocs()

	for b.Loop() {
		_ = mw(forge_http.NewContext(httptest.NewRecorder(), req, nil))
	}
}
