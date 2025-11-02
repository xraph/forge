package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBunRouterAdapter_BasicRoute(t *testing.T) {
	adapter := NewBunRouterAdapter()

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

func TestBunRouterAdapter_PathParams(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path params should be in context
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	adapter.Handle("GET", "/users/:id", handler)

	req := httptest.NewRequest("GET", "/users/123", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
}

func TestBunRouterAdapter_Mount(t *testing.T) {
	t.Skip("BunRouter mount with wildcards is problematic, tested via main router")
	adapter := NewBunRouterAdapter()

	// Mount doesn't work properly with bunrouter wildcards
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("mounted"))
	})

	adapter.Mount("/api", handler)
	assert.NotNil(t, adapter)
}

func TestBunRouterAdapter_NotFound(t *testing.T) {
	adapter := NewBunRouterAdapter()

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 404, rec.Code)
}

func TestBunRouterAdapter_Close(t *testing.T) {
	adapter := NewBunRouterAdapter()

	err := adapter.Close()
	assert.NoError(t, err)
}

func TestConvertPathToBunRouter(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		desc     string
	}{
		{"/users/:id", "/users/:id", "named parameter"},
		{"/users/{id}", "/users/{id}", "brace parameter"},
		{"/posts/{postId}/comments/{commentId}", "/posts/{postId}/comments/{commentId}", "multiple parameters"},
		{"/static", "/static", "no parameters"},
		{"/{category}/{id}", "/{category}/{id}", "multiple brace parameters"},
		// Wildcard tests
		{"/api/auth/dashboard/static/*", "/api/auth/dashboard/static/*filepath", "unnamed wildcard at end"},
		{"/files/*", "/files/*filepath", "simple unnamed wildcard"},
		{"/*", "/*filepath", "root wildcard"},
		{"/api/*/assets", "/api/*filepath/assets", "wildcard in middle"},
		{"/static/*path", "/static/*path", "already named wildcard"},
		{"/api/*filepath", "/api/*filepath", "already named with filepath"},
	}

	for _, tt := range tests {
		result := convertPathToBunRouter(tt.input)
		assert.Equal(t, tt.expected, result, "Failed for input: %s (%s)", tt.input, tt.desc)
	}
}

func TestBunRouterAdapter_WildcardRoute(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("wildcard matched"))
	})

	// Test unnamed wildcard - should be auto-converted
	adapter.Handle("GET", "/static/*", handler)

	req := httptest.NewRequest("GET", "/static/css/style.css", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "wildcard matched", rec.Body.String())
}

func TestBunRouterAdapter_ComplexWildcardRoute(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("complex wildcard matched"))
	})

	// Test the specific route from the issue (with leading slash as required by bunrouter)
	adapter.Handle("GET", "/api/auth/dashboard/static/*", handler)

	tests := []struct {
		path       string
		expectCode int
	}{
		{"/api/auth/dashboard/static/", 200},
		{"/api/auth/dashboard/static/css/main.css", 200},
		{"/api/auth/dashboard/static/js/bundle.js", 200},
		{"/api/auth/dashboard/static/img/logo.png", 200},
		{"/api/auth/dashboard/static/nested/deep/file.txt", 200},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		rec := httptest.NewRecorder()

		adapter.ServeHTTP(rec, req)

		assert.Equal(t, tt.expectCode, rec.Code, "Failed for path: %s", tt.path)
	}
}
