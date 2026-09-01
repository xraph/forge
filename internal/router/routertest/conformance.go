// Package routertest holds the behavioral suite every router adapter must
// pass.
//
// Floor assertions run against every adapter. Wide assertions are gated on
// Capabilities, so an adapter that cannot express a behavior skips it with a
// logged reason instead of quietly passing.
package routertest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	forge_http "github.com/xraph/go-utils/http"

	"github.com/xraph/forge/internal/pathspec"
	"github.com/xraph/forge/internal/shared"
)

func handler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
}

func parsePattern(raw string) (pathspec.Pattern, error) { return pathspec.Parse(raw) }

// wildcardParam reads the "*" alias the way a forge handler would, through
// whichever carrier the adapter published.
func wildcardParam(r *http.Request) string {
	if rp, ok := r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams); ok {
		if v, found := rp.Get("*"); found {
			return v
		}
	}

	if m, ok := r.Context().Value("forge:params").(map[string]string); ok { //nolint:staticcheck // legacy contract
		return m["*"]
	}

	return ""
}

// RunConformance exercises one adapter. factory must return a fresh adapter
// on every call, because most subtests register conflicting routes.
func RunConformance(t *testing.T, name string, factory func() shared.RouterAdapter) {
	t.Run(name+"/floor", func(t *testing.T) { runFloor(t, factory) })

	probe := factory()

	ext, ok := probe.(shared.ExtendedAdapter)
	if !ok {
		t.Logf("%s: not an ExtendedAdapter, skipping every wide assertion", name)

		return
	}

	caps := ext.Capabilities()

	t.Run(name+"/wide", func(t *testing.T) { runWide(t, name, caps, factory) })
}

func runFloor(t *testing.T, factory func() shared.RouterAdapter) {
	t.Run("static route", func(t *testing.T) {
		a := factory()
		a.Handle(http.MethodGet, "/users", handler("users"))

		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users", nil))

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "users", rec.Body.String())
	})

	t.Run("colon parameter", func(t *testing.T) {
		a := factory()
		a.Handle(http.MethodGet, "/users/:id", handler("byID"))

		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("brace parameter", func(t *testing.T) {
		a := factory()
		a.Handle(http.MethodGet, "/users/{id}", handler("byID"))

		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("static beats parameter", func(t *testing.T) {
		a := factory()
		a.Handle(http.MethodGet, "/users/{id}", handler("byID"))
		a.Handle(http.MethodGet, "/users/me", handler("me"))

		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/me", nil))

		assert.Equal(t, "me", rec.Body.String(), "a static segment must win regardless of registration order")
	})

	t.Run("wildcard", func(t *testing.T) {
		a := factory()
		a.Handle(http.MethodGet, "/files/*", handler("files"))

		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/a/b/c.txt", nil))

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("wildcard is reachable as star", func(t *testing.T) {
		a := factory()

		var star string

		a.Handle(http.MethodGet, "/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Both adapters publish params through the request context; read
			// through the same accessor a forge handler would.
			star = wildcardParam(r)
			w.WriteHeader(http.StatusOK)
		}))

		a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil))

		assert.Equal(t, "css/app.css", star, `every adapter must expose the wildcard as "*"`)
	})

	t.Run("both trailing-slash spellings reach the same handler", func(t *testing.T) {
		a := factory()
		a.Handle(http.MethodGet, "/dashboard", handler("dash"))

		for _, path := range []string{"/dashboard", "/dashboard/"} {
			rec := httptest.NewRecorder()
			a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			assert.Equalf(t, http.StatusOK, rec.Code, "path %s", path)
			assert.Equalf(t, "dash", rec.Body.String(), "path %s", path)
		}
	})

	t.Run("unknown path is 404", func(t *testing.T) {
		a := factory()
		a.Handle(http.MethodGet, "/users", handler("users"))

		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("global middleware runs before routing", func(t *testing.T) {
		a := factory()

		ran := false
		a.UseGlobal(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ran = true
				next.ServeHTTP(w, r)
			})
		})

		a.Handle(http.MethodGet, "/users", handler("users"))
		a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

		assert.True(t, ran)
	})

	t.Run("global middleware runs for an unmatched path", func(t *testing.T) {
		a := factory()

		ran := false
		a.UseGlobal(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ran = true
				next.ServeHTTP(w, r)
			})
		})

		a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodOptions, "/nope", nil))

		assert.True(t, ran, "CORS preflight depends on this")
	})

	t.Run("close", func(t *testing.T) {
		assert.NoError(t, factory().Close())
	})
}

func runWide(t *testing.T, name string, caps shared.Capabilities, factory func() shared.RouterAdapter) {
	wide := func(t *testing.T) shared.ExtendedAdapter {
		t.Helper()

		return factory().(shared.ExtendedAdapter)
	}

	t.Run("method not allowed", func(t *testing.T) {
		if !caps.MethodNotAllowed {
			t.Skipf("%s does not report MethodNotAllowed", name)
		}

		a := wide(t)
		a.Handle(http.MethodGet, "/users", handler("get"))
		a.Handle(http.MethodPost, "/users", handler("post"))

		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/users", nil))

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		assert.Equal(t, "GET, POST", rec.Header().Get("Allow"))
	})

	t.Run("constraints fall through", func(t *testing.T) {
		if !caps.Constraints {
			t.Skipf("%s does not report Constraints", name)
		}

		a := wide(t)
		a.Handle(http.MethodGet, "/x/{n:int}", handler("int"))
		a.Handle(http.MethodGet, "/x/{s:alpha}", handler("alpha"))

		for path, want := range map[string]string{"/x/42": "int", "/x/abc": "alpha"} {
			rec := httptest.NewRecorder()
			a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			assert.Equalf(t, want, rec.Body.String(), "path %s", path)
		}
	})

	t.Run("conflict detection", func(t *testing.T) {
		if !caps.ConflictDetection {
			t.Skipf("%s does not report ConflictDetection", name)
		}

		a := wide(t)

		first, err := parsePattern("/users/{id}")
		require.NoError(t, err)
		require.NoError(t, a.HandleRoute(shared.RouteSpec{
			Method: http.MethodGet, Pattern: first, Handler: handler("a"),
		}))

		second, err := parsePattern("/users/{name}")
		require.NoError(t, err)

		assert.Error(t, a.HandleRoute(shared.RouteSpec{
			Method: http.MethodGet, Pattern: second, Handler: handler("b"),
		}), "an identical shape on the same method must be rejected")
	})

	t.Run("any method", func(t *testing.T) {
		if !caps.AnyMethod {
			t.Skipf("%s does not report AnyMethod", name)
		}

		a := wide(t)

		pattern, err := parsePattern("/mounted/*")
		require.NoError(t, err)
		require.NoError(t, a.HandleRoute(shared.RouteSpec{Pattern: pattern, Handler: handler("mount")}))

		for _, method := range []string{http.MethodGet, "PROPFIND"} {
			rec := httptest.NewRecorder()
			a.ServeHTTP(rec, httptest.NewRequest(method, "/mounted/x", nil))

			assert.Equalf(t, "mount", rec.Body.String(), "method %s", method)
		}
	})

	t.Run("configure is construction only", func(t *testing.T) {
		a := wide(t)
		require.NoError(t, a.Configure(shared.MatcherConfig{NotFound: http.NotFoundHandler()}))

		a.Handle(http.MethodGet, "/dashboard", handler("dash"))

		assert.Error(t, a.Configure(shared.MatcherConfig{}),
			"reconfiguring after a route exists would reinterpret it silently")
	})
}
