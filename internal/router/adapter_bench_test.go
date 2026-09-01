package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/internal/router/forgemux"
	"github.com/xraph/forge/internal/shared"
)

func benchAdapter(b *testing.B, a shared.RouterAdapter) shared.RouterAdapter {
	b.Helper()

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	a.Handle(http.MethodGet, "/test", h)
	a.Handle(http.MethodGet, "/users/:id/posts/:pid", h)
	a.Handle(http.MethodGet, "/files/*", h)

	return a
}

func benchServe(b *testing.B, a shared.RouterAdapter, path string) {
	b.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		a.ServeHTTP(rec, req)
	}
}

func BenchmarkAdapter_BunRouter_Static(b *testing.B) {
	benchServe(b, benchAdapter(b, NewBunRouterAdapter()), "/test")
}

func BenchmarkAdapter_ForgeMux_Static(b *testing.B) {
	benchServe(b, benchAdapter(b, forgemux.New()), "/test")
}

func BenchmarkAdapter_BunRouter_TwoParams(b *testing.B) {
	benchServe(b, benchAdapter(b, NewBunRouterAdapter()), "/users/42/posts/7")
}

func BenchmarkAdapter_ForgeMux_TwoParams(b *testing.B) {
	benchServe(b, benchAdapter(b, forgemux.New()), "/users/42/posts/7")
}

func BenchmarkAdapter_BunRouter_Wildcard(b *testing.B) {
	benchServe(b, benchAdapter(b, NewBunRouterAdapter()), "/files/a/b/c.txt")
}

func BenchmarkAdapter_ForgeMux_Wildcard(b *testing.B) {
	benchServe(b, benchAdapter(b, forgemux.New()), "/files/a/b/c.txt")
}
