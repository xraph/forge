package security

import "testing"

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
