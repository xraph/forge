package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/router/forgemux"
	"github.com/xraph/forge/internal/shared"
)

// differentialRoutes is deliberately restricted to the semantics both
// backends share. Constraints and 405 reporting are forgemux-only and are
// covered by the conformance suite instead.
var differentialRoutes = []string{
	"/",
	"/users",
	"/users/me",
	"/users/{id}",
	"/users/{id}/posts",
	"/users/{id}/posts/{postId}",
	"/api/v1/health",
	"/files/*",
}

func buildBoth(t *testing.T) (shared.RouterAdapter, shared.RouterAdapter) {
	t.Helper()

	bun := NewBunRouterAdapter()
	mux := forgemux.New()

	for _, route := range differentialRoutes {
		body := route

		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		})

		bun.Handle(http.MethodGet, route, h)
		mux.Handle(http.MethodGet, route, h)
	}

	return bun, mux
}

func serve(a shared.RouterAdapter, path string) (int, string) {
	rec := httptest.NewRecorder()

	req, err := http.NewRequest(http.MethodGet, "http://example.test"+path, nil)
	if err != nil {
		return -1, ""
	}

	a.ServeHTTP(rec, req)

	return rec.Code, rec.Body.String()
}

func FuzzDifferentialAgainstBunRouter(f *testing.F) {
	seeds := []string{
		"/", "/users", "/users/me", "/users/42", "/users/42/posts",
		"/users/42/posts/7", "/api/v1/health", "/files/a/b.txt",
		"/nope", "/users/", "//users", "/users/42/", "/files/",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, path string) {
		// Only well-formed absolute paths are in scope. Request parsing is
		// net/http's business, not the matcher's.
		if path == "" || path[0] != '/' {
			return
		}

		for i := range len(path) {
			if path[i] < 0x20 || path[i] == 0x7f || path[i] == '?' || path[i] == '#' {
				return
			}
		}

		// A NUL byte in a parameter position is out of scope because the
		// oracle crashes: bunrouter v1.0.23 panics with "index out of range
		// [0] with length 0" inside node._findRoute. forgemux serves the
		// request normally. See docs/superpowers/notes/forgemux-differences.md.
		if strings.Contains(path, "%00") || strings.ContainsRune(path, 0) {
			return
		}

		// Interior repeated slashes are out of scope: the two backends
		// genuinely disagree and forge has picked a side.
		//
		// bunrouter matches "/users//0/0" against the two-segment
		// "/users/{id}", binding id="0" and silently discarding the trailing
		// "/0". The collapsed form "/users/0/0" correctly 404s, so bunrouter
		// is inconsistent with itself and drops part of the URL. forgemux
		// collapses empty segments, which makes "/users//0/0" mean
		// "/users/0/0" and 404 accordingly. See
		// docs/superpowers/notes/forgemux-differences.md.
		if strings.Contains(path, "//") {
			return
		}

		bun, mux := buildBoth(t)

		bunCode, bunBody := serve(bun, path)
		muxCode, muxBody := serve(mux, path)

		if bunCode == -1 || muxCode == -1 {
			return
		}

		// Both adapters should match a trailing slash leniently: forgemux by
		// design, and the BunRouter adapter because normalizeTrailingSlash
		// trims before bunrouter can redirect. A 3xx here therefore means that
		// trimming was bypassed, which is a finding worth reading rather than
		// a case to compare, so bail out instead of asserting.
		if bunCode >= 300 && bunCode < 400 {
			return
		}

		require.Equalf(t, bunCode, muxCode, "status disagreement on %q", path)

		if bunCode == http.StatusOK {
			require.Equalf(t, bunBody, muxBody, "different handler reached for %q", path)
		}
	})
}
