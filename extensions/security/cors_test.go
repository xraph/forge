package security

import "testing"

// TestIsOriginAllowed_WildcardDotBoundary verifies that a "*.example.com"
// allowlist entry matches only genuine subdomains (with a real label boundary),
// not lookalike domains that merely share the suffix string.
func TestIsOriginAllowed_WildcardDotBoundary(t *testing.T) {
	m := &CORSManager{config: CORSConfig{
		AllowOrigins: []string{"https://app.kineta.ai", "*.kineta.ai"},
	}}

	cases := []struct {
		origin string
		want   bool
	}{
		{"https://app.kineta.ai", true},        // exact allowlist entry
		{"https://wakflo.kineta.ai", true},     // real subdomain
		{"https://dental.kineta.ai", true},     // real subdomain
		{"https://a.b.kineta.ai", true},        // nested subdomain
		{"https://kineta.ai", true},            // apex equals the base domain
		{"https://wakflo.kineta.ai:8443", true}, // port is ignored (host match)
		{"https://evilkineta.ai", false},       // lookalike, no label boundary
		{"https://kineta.ai.evil.com", false},  // base embedded elsewhere
		{"https://evil.com", false},            // unrelated
		{"", false},                            // empty origin
		{"://bad", false},                      // malformed
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
	m := &CORSManager{config: CORSConfig{AllowOrigins: []string{"https://app.kineta.ai"}}}
	if m.isOriginAllowed("https://wakflo.kineta.ai") {
		t.Fatal("subdomain must not be allowed without a wildcard entry")
	}
	if !m.isOriginAllowed("https://app.kineta.ai") {
		t.Fatal("exact origin must be allowed")
	}
}
