package extras

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
)

// TestAdapters_NestedGroupInheritsGroupMiddleware runs the nested group
// middleware rules through forge's router on each extras backend: a rejecting
// middleware on a parent group guards routes on nested groups, the parent's
// group middleware runs before the child's, and it runs exactly once.
func TestAdapters_NestedGroupInheritsGroupMiddleware(t *testing.T) {
	adapters := map[string]func() forge.RouterAdapter{
		"chi":        NewChiAdapter,
		"httprouter": NewHTTPRouterAdapter,
		"bunrouter":  NewBunRouterAdapter,
	}

	for name, newAdapter := range adapters {
		t.Run(name, func(t *testing.T) {
			r := forge.NewRouter(forge.WithAdapter(newAdapter()))

			var calls []string

			record := func(label string) forge.Middleware {
				return func(next forge.Handler) forge.Handler {
					return func(ctx forge.Context) error {
						calls = append(calls, label)

						return next(ctx)
					}
				}
			}
			reject := func(next forge.Handler) forge.Handler {
				return func(ctx forge.Context) error {
					return ctx.String(http.StatusUnauthorized, "denied")
				}
			}
			ok := func(ctx forge.Context) error {
				calls = append(calls, "handler")

				return ctx.String(http.StatusOK, "ok")
			}

			admin := r.Group("/admin", forge.WithGroupMiddleware(reject))
			require.NoError(t, admin.Group("/v1").GET("/users", ok))
			require.NoError(t, admin.Group("/v1").Group("/deep").GET("/users", ok))

			open := r.Group("/open", forge.WithGroupMiddleware(record("parent")))
			require.NoError(t, open.GET("/x", ok))
			require.NoError(t, open.Group("/c", forge.WithGroupMiddleware(record("child"))).GET("/x", ok))

			get := func(path string) *httptest.ResponseRecorder {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)

				return rec
			}

			for _, path := range []string{"/admin/v1/users", "/admin/v1/deep/users"} {
				assert.Equal(t, http.StatusUnauthorized, get(path).Code, "GET %s", path)
			}

			assert.Empty(t, calls, "a handler behind a rejecting group middleware ran")

			require.Equal(t, http.StatusOK, get("/open/x").Code)
			assert.Equal(t, []string{"parent", "handler"}, calls)

			calls = nil

			require.Equal(t, http.StatusOK, get("/open/c/x").Code)
			assert.Equal(t, []string{"parent", "child", "handler"}, calls)
		})
	}
}
