package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func originRequest(host, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Host = host

	if origin != "" {
		r.Header.Set("Origin", origin)
	}

	return r
}

// Connection upgrades are not covered by CORS but do carry cookies, so the
// Origin check is the only thing standing between a logged-in user and any site
// they visit opening an authenticated socket.
func TestRequestOriginAllowed(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		origin  string
		allowed []string
		want    bool
	}{
		// Default policy: same-origin.
		{"no origin header is a non-browser client", "api.example.com", "", nil, true},
		{"same origin", "api.example.com", "https://api.example.com", nil, true},
		{"same origin with explicit default port", "api.example.com", "https://api.example.com:443", nil, true},
		{"cross origin denied by default", "api.example.com", "https://evil.com", nil, false},
		{"empty host cannot establish same-origin", "", "https://evil.com", nil, false},

		// Explicit allow-list forms.
		{"exact origin", "api.example.com", "https://app.example.com", []string{"https://app.example.com"}, true},
		{"bare host, any scheme", "api.example.com", "http://app.example.com", []string{"app.example.com"}, true},
		{"wildcard subdomain", "api.example.com", "https://app.example.com", []string{"*.example.com"}, true},
		{"wildcard nested subdomain", "api.example.com", "https://a.b.example.com", []string{"*.example.com"}, true},
		{"explicit star allows anything", "api.example.com", "https://evil.com", []string{"*"}, true},

		// Regressions: a plain suffix test accepted all of these.
		{"suffix bypass with hyphen", "api.example.com", "https://evil-example.com", []string{"*.example.com"}, false},
		{"suffix bypass without separator", "api.example.com", "https://notexample.com", []string{"*.example.com"}, false},
		{"wildcard does not cover the apex", "api.example.com", "https://example.com", []string{"*.example.com"}, false},
		{"port mismatch on exact entry", "api.example.com", "https://exact.test", []string{"https://exact.test:8443"}, false},

		// Malformed origins must never match.
		{"non-absolute origin", "api.example.com", "not-a-url", []string{"*.example.com"}, false},
		{"scheme-only origin", "api.example.com", "javascript:alert(1)", []string{"*.example.com"}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := requestOriginAllowed(originRequest(c.host, c.origin), c.allowed)
			if got != c.want {
				t.Errorf("requestOriginAllowed(host=%q, origin=%q, allowed=%v) = %v, want %v",
					c.host, c.origin, c.allowed, got, c.want)
			}
		})
	}
}

// Origin matching is case-insensitive on host, per the URL spec.
func TestRequestOriginAllowedIsCaseInsensitive(t *testing.T) {
	r := originRequest("api.example.com", "https://APP.EXAMPLE.COM")

	for _, allowed := range [][]string{
		{"*.example.com"},
		{"https://app.example.com"},
		{"app.example.com"},
	} {
		if !requestOriginAllowed(r, allowed) {
			t.Errorf("uppercase origin rejected by allow-list %v", allowed)
		}
	}
}

// WebTransport and WebSocket must enforce identical rules; an empty list used to
// mean "allow everything" on the WebTransport side.
func TestCheckWebTransportOriginDefaultsToSameOrigin(t *testing.T) {
	check := checkWebTransportOrigin(nil)

	if check(originRequest("api.example.com", "https://evil.com")) {
		t.Error("empty allow-list permitted a cross-origin WebTransport upgrade")
	}

	if !check(originRequest("api.example.com", "https://api.example.com")) {
		t.Error("empty allow-list rejected a same-origin WebTransport upgrade")
	}
}
