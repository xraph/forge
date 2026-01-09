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
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	adapter.Handle("GET", "/test", handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestBunRouterAdapter_PathParams(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path params should be in context
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	adapter.Handle("GET", "/users/:id", handler)

	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
}

func TestBunRouterAdapter_Mount(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("mounted: " + r.URL.Path))
	})

	adapter.Mount("/api", handler)

	// Test exact path
	req1 := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec1 := httptest.NewRecorder()
	adapter.ServeHTTP(rec1, req1)
	assert.Equal(t, 200, rec1.Code)
	assert.Contains(t, rec1.Body.String(), "mounted")

	// Test sub-path
	req2 := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec2 := httptest.NewRecorder()
	adapter.ServeHTTP(rec2, req2)
	assert.Equal(t, 200, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "mounted")

	// Test with POST method
	req3 := httptest.NewRequest(http.MethodPost, "/api/data", nil)
	rec3 := httptest.NewRecorder()
	adapter.ServeHTTP(rec3, req3)
	assert.Equal(t, 200, rec3.Code)
}

func TestBunRouterAdapter_MountWithWildcard(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("wildcard mounted"))
	})

	adapter.Mount("/files/*", handler)

	// Test wildcard path
	req := httptest.NewRequest(http.MethodGet, "/files/test.txt", nil)
	rec := httptest.NewRecorder()
	adapter.ServeHTTP(rec, req)
	assert.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), "wildcard mounted")
}

func TestBunRouterAdapter_NotFound(t *testing.T) {
	adapter := NewBunRouterAdapter()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
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
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("wildcard matched"))
	})

	// Test unnamed wildcard - should be auto-converted
	adapter.Handle("GET", "/static/*", handler)

	req := httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "wildcard matched", rec.Body.String())
}

func TestBunRouterAdapter_ComplexWildcardRoute(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		rec := httptest.NewRecorder()

		adapter.ServeHTTP(rec, req)

		assert.Equal(t, tt.expectCode, rec.Code, "Failed for path: %s", tt.path)
	}
}
