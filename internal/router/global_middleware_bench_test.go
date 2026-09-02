package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	logger "github.com/xraph/go-utils/log"
)

// Global middleware is the path every extension takes: app.applyExtensionMiddlewares
// hands each extension's middleware to router.UseGlobal. Its per-request cost is
// therefore multiplied by the number of extensions an app installs, so these
// benchmarks exist to keep it flat.
//
// It was not flat. Each middleware used to be installed as its own adapter-level
// wrapper that built its own forge Context, and the adapter recomposed the whole
// chain on every request. Ten extension middlewares cost 37 allocs/op against the
// 6 that the same ten cost as route-scoped middleware. If the Global numbers below
// start climbing with N again, that regressed. See globalChain.
func benchGlobalMiddleware(b *testing.B, n int, global bool) {
	r := NewRouter(WithLogger(logger.NewTestLogger()))

	mw := func(next Handler) Handler {
		return func(ctx Context) error { return next(ctx) }
	}

	for range n {
		if global {
			r.UseGlobal(mw)
		} else {
			r.Use(mw)
		}
	}

	_ = r.GET("/test", func(ctx Context) error { return ctx.String(200, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_ScopedMiddleware_1(b *testing.B)  { benchGlobalMiddleware(b, 1, false) }
func BenchmarkRouter_ScopedMiddleware_10(b *testing.B) { benchGlobalMiddleware(b, 10, false) }
func BenchmarkRouter_GlobalMiddleware_1(b *testing.B)  { benchGlobalMiddleware(b, 1, true) }
func BenchmarkRouter_GlobalMiddleware_5(b *testing.B)  { benchGlobalMiddleware(b, 5, true) }
func BenchmarkRouter_GlobalMiddleware_10(b *testing.B) { benchGlobalMiddleware(b, 10, true) }
