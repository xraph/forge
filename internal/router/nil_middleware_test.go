package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func okHandler(ctx Context) error { return ctx.String(http.StatusOK, "ok") }

func passThrough(next Handler) Handler { return next }

// A nil middleware used to panic deep inside chain composition. Every way of
// attaching route middleware now turns it into a registration error that
// names the route.
func TestRegister_RejectsNilMiddleware(t *testing.T) {
	var unset Middleware

	cases := map[string]func(Router) error{
		"Use": func(r Router) error {
			r.Use(passThrough, unset)

			return r.GET("/x", okHandler)
		},
		"WithMiddleware": func(r Router) error {
			return r.GET("/x", okHandler, WithMiddleware(unset))
		},
		"WithGroupMiddleware": func(r Router) error {
			return r.Group("", WithGroupMiddleware(unset)).GET("/x", okHandler)
		},
	}

	for name, register := range cases {
		t.Run(name, func(t *testing.T) {
			var err error

			require.NotPanics(t, func() { err = register(NewRouter()) })
			require.Error(t, err)
			assert.Contains(t, err.Error(), "GET /x")
			assert.Contains(t, err.Error(), "is nil")
		})
	}
}

func TestRegister_NilMiddlewareLeavesRouterUsable(t *testing.T) {
	r := NewRouter()

	require.Error(t, r.GET("/bad", okHandler, WithMiddleware(nil)))
	require.NoError(t, r.GET("/good", okHandler, WithMiddleware(passThrough)))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/good", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// UseGlobal has no error return, so it panics at the call with a readable
// message. Before, the panic came from inside the adapter while the global
// chain's mutex was held, so whoever recovered it hung on the next UseGlobal.
func TestUseGlobal_NilMiddlewareFailsAtTheCall(t *testing.T) {
	r := NewRouter()

	assert.PanicsWithValue(t, "router: UseGlobal: middleware 1 of 1 is nil; make sure it is built before you register it", func() {
		r.UseGlobal(nil)
	})

	done := make(chan struct{})

	go func() {
		defer close(done)

		r.UseGlobal(passThrough)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("UseGlobal hung after a nil middleware was refused")
	}

	require.NoError(t, r.GET("/x", okHandler))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGlobalChain_PanicWhileComposingReleasesTheLock(t *testing.T) {
	gc := &globalChain{middleware: []Middleware{nil}}

	assert.Panics(t, func() { gc.install(http.NotFoundHandler()) })

	locked := gc.mu.TryLock()
	require.True(t, locked, "the chain must not stay locked after a panic")
	gc.mu.Unlock()
}
