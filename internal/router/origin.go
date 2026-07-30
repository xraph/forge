package router

import (
	"net/http"
	"net/url"
	"strings"
)

// Origin validation for connection upgrades (WebSocket, WebTransport).
//
// Browsers do not apply CORS to WebSocket or WebTransport upgrades, but they do
// send cookies with them. An upgrade endpoint that accepts any Origin is
// therefore hijackable: any site the user visits can open an authenticated
// socket as that user (cross-site WebSocket hijacking). CORS middleware does
// not help — it governs XHR/fetch, not upgrades.
//
// The default here is same-origin: an upgrade carrying an Origin that does not
// match the request's own host is refused. Requests with no Origin header are
// allowed, because non-browser clients (CLIs, servers, mobile SDKs) do not send
// one and browsers always do. Cross-origin browser clients must be allowed
// explicitly via configuration.

// requestOriginAllowed reports whether an upgrade request's Origin is
// acceptable given the configured allow-list.
//
// allowedOrigins entries may be:
//   - "*"                      allow any origin (explicit opt-in)
//   - "https://app.example.com" exact scheme://host[:port] match
//   - "app.example.com"        bare host match, any scheme
//   - "*.example.com"          any strict subdomain of example.com
//
// An empty list means same-origin only.
func requestOriginAllowed(r *http.Request, allowedOrigins []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser client: no Origin to forge, nothing to check.
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return false
	}

	originScheme := strings.ToLower(parsed.Scheme)
	originHostPort := strings.ToLower(parsed.Host)

	if len(allowedOrigins) == 0 {
		return sameOrigin(r, originHostPort)
	}

	for _, allowed := range allowedOrigins {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}

		if allowed == "*" {
			return true
		}

		// Wildcard subdomain. Matched on label boundaries — a plain suffix
		// test would accept "evil-example.com" for "*.example.com".
		if rest, ok := strings.CutPrefix(allowed, "*."); ok {
			if strings.HasSuffix(stripPort(originHostPort), "."+rest) {
				return true
			}

			continue
		}

		// Exact scheme://host[:port].
		if allowed == originScheme+"://"+originHostPort {
			return true
		}

		// Bare host[:port], any scheme.
		if allowed == originHostPort {
			return true
		}
	}

	return false
}

// sameOrigin reports whether the origin's host matches the host the request was
// addressed to. Scheme is not compared: a TLS-terminating proxy makes the
// server see http for an https origin, and rejecting that would break every
// deployment behind a load balancer.
func sameOrigin(r *http.Request, originHostPort string) bool {
	host := strings.ToLower(r.Host)
	if host == "" {
		// Nothing to compare against; refuse rather than guess.
		return false
	}

	if host == originHostPort {
		return true
	}

	// Tolerate a default port on exactly one side (":443" vs bare host).
	return stripDefaultPort(host) == stripDefaultPort(originHostPort)
}

// stripPort removes a trailing ":port" if present.
func stripPort(hostPort string) string {
	if i := strings.LastIndexByte(hostPort, ':'); i != -1 && !strings.Contains(hostPort[i:], "]") {
		return hostPort[:i]
	}

	return hostPort
}

// stripDefaultPort removes an explicit :80 or :443 so that "example.com" and
// "example.com:443" compare equal.
func stripDefaultPort(hostPort string) string {
	switch {
	case strings.HasSuffix(hostPort, ":443"):
		return strings.TrimSuffix(hostPort, ":443")
	case strings.HasSuffix(hostPort, ":80"):
		return strings.TrimSuffix(hostPort, ":80")
	default:
		return hostPort
	}
}
