package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setValue is route or global middleware that stores one value the way the
// auth and security extensions publish theirs.
func setValue(key string, value any) Middleware {
	return func(next Handler) Handler {
		return func(ctx Context) error {
			ctx.Set(key, value)

			return next(ctx)
		}
	}
}

func getPath(t *testing.T, r Router, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

	return rec
}

// TestMiddlewareValuesReachHandler covers every hop a value written with
// ctx.Set has to survive. Each layer (global chain, route chain, handler
// adapter) builds its own pooled Context, and each one used to start with an
// empty values map, so a handler never saw what middleware stored.
func TestMiddlewareValuesReachHandler(t *testing.T) {
	t.Run("route middleware to context handler", func(t *testing.T) {
		r := NewRouter()

		var got any

		require.NoError(t, r.GET("/x", func(ctx Context) error {
			got = ctx.Get("k")

			return nil
		}, WithMiddleware(setValue("k", "v"))))

		assert.Equal(t, http.StatusOK, getPath(t, r, "/x").Code)
		assert.Equal(t, "v", got)
	})

	t.Run("router Use middleware to context handler", func(t *testing.T) {
		r := NewRouter()
		r.Use(setValue("k", "v"))

		var got any

		require.NoError(t, r.GET("/x", func(ctx Context) error {
			got = ctx.Get("k")

			return nil
		}))

		getPath(t, r, "/x")
		assert.Equal(t, "v", got)
	})

	t.Run("global middleware to route middleware and handler", func(t *testing.T) {
		r := NewRouter()
		r.UseGlobal(setValue("global", "g"))

		var inRoute, inHandler any

		require.NoError(t, r.GET("/x", func(ctx Context) error {
			inHandler = ctx.Get("global")

			return nil
		}, WithMiddleware(func(next Handler) Handler {
			return func(ctx Context) error {
				inRoute = ctx.Get("global")

				return next(ctx)
			}
		})))

		getPath(t, r, "/x")
		assert.Equal(t, "g", inRoute, "route middleware should see global values")
		assert.Equal(t, "g", inHandler, "handler should see global values")
	})

	t.Run("route middleware to opinionated handler", func(t *testing.T) {
		type req struct{}

		type resp struct {
			Value any `json:"value"`
		}

		r := NewRouter()

		require.NoError(t, r.GET("/x", func(ctx Context, _ *req) (*resp, error) {
			return &resp{Value: ctx.Get("k")}, nil
		}, WithMiddleware(setValue("k", "v"))))

		rec := getPath(t, r, "/x")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"value":"v"}`, rec.Body.String())
	})

	t.Run("handler writes are visible to middleware after next returns", func(t *testing.T) {
		r := NewRouter()

		var after any

		require.NoError(t, r.GET("/x", func(ctx Context) error {
			ctx.Set("from-handler", "h")

			return nil
		}, WithMiddleware(func(next Handler) Handler {
			return func(ctx Context) error {
				err := next(ctx)
				after = ctx.Get("from-handler")

				return err
			}
		})))

		getPath(t, r, "/x")
		assert.Equal(t, "h", after)
	})

	t.Run("route middleware to SSE handler", func(t *testing.T) {
		r := NewRouter()

		var got any

		require.NoError(t, r.SSE("/events", func(ctx Context) error {
			got = ctx.Get("k")

			return nil
		}, WithMiddleware(setValue("k", "v"))))

		getPath(t, r, "/events")
		assert.Equal(t, "v", got)
	})

	t.Run("values do not leak into the next request", func(t *testing.T) {
		r := NewRouter()

		var got []any

		require.NoError(t, r.GET("/set", func(ctx Context) error { return nil },
			WithMiddleware(setValue("k", "v"))))
		require.NoError(t, r.GET("/read", func(ctx Context) error {
			got = append(got, ctx.Get("k"))

			return nil
		}))

		for range 50 {
			getPath(t, r, "/set")
			getPath(t, r, "/read")
		}

		for i, v := range got {
			require.Nil(t, v, "request %d saw a value set by an earlier request", i)
		}
	})
}
