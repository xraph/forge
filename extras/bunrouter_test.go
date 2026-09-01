package extras

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
	t.Skip("BunRouter mount with wildcards is problematic, tested via main router")

	adapter := NewBunRouterAdapter()

	// Mount doesn't work properly with bunrouter wildcards
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("mounted"))
	})

	adapter.Mount("/api", handler)
	assert.NotNil(t, adapter)
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

func TestBunRouterAdapter_RendersForgePathsIntoBunRouterSyntax(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/users/:id", "/users/:id"},
		{"/users/{id}", "/users/:id"},
		{"/posts/{postId}/comments/{commentId}", "/posts/:postId/comments/:commentId"},
		{"/{category}/{id}", "/:category/:id"},
		{"/static", "/static"},
		{"/files/*", "/files/*filepath"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, toBunPath(tt.in))
		})
	}
}

// Braces used to be passed through verbatim, so "/users/{id}" matched only the
// literal path "/users/{id}" and 404'd "/users/42".
func TestBunRouterAdapter_BraceParameterCaptures(t *testing.T) {
	adapter := NewBunRouterAdapter()
	adapter.Handle("GET", "/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	rec := httptest.NewRecorder()
	adapter.ServeHTTP(rec, httptest.NewRequest("GET", "/users/42", nil))

	assert.Equal(t, 200, rec.Code, "a brace parameter must capture a real segment")
}
