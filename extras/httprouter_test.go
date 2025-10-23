package extras

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPRouterAdapter_BasicRoute(t *testing.T) {
	adapter := NewHTTPRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	adapter.Handle("GET", "/test", handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestHTTPRouterAdapter_PathParams(t *testing.T) {
	adapter := NewHTTPRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	// HTTPRouter uses :param format
	adapter.Handle("GET", "/users/:id", handler)

	req := httptest.NewRequest("GET", "/users/123", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
}

func TestHTTPRouterAdapter_Mount(t *testing.T) {
	adapter := NewHTTPRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("mounted"))
	})

	adapter.Mount("/api", handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "mounted", rec.Body.String())
}

func TestHTTPRouterAdapter_NotFound(t *testing.T) {
	adapter := NewHTTPRouterAdapter()

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 404, rec.Code)
}

func TestHTTPRouterAdapter_Close(t *testing.T) {
	adapter := NewHTTPRouterAdapter()

	err := adapter.Close()
	assert.NoError(t, err)
}

// Note: convertPath is a private function so we test it indirectly through Handle
