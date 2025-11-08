package extras

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChiAdapter_BasicRoute(t *testing.T) {
	adapter := NewChiAdapter()

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

func TestChiAdapter_PathParams(t *testing.T) {
	adapter := NewChiAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	adapter.Handle("GET", "/users/:id", handler)

	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
}

func TestChiAdapter_Mount(t *testing.T) {
	adapter := NewChiAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("mounted"))
	})

	adapter.Mount("/api", handler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "mounted", rec.Body.String())
}

func TestChiAdapter_Close(t *testing.T) {
	adapter := NewChiAdapter()

	err := adapter.Close()
	assert.NoError(t, err)
}

func TestConvertPathToChi(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/users/:id", "/users/{id}"},
		{"/users/{id}", "/users/{id}"},
		{"/posts/:postId/comments/:commentId", "/posts/{postId}/comments/{commentId}"},
		{"/static", "/static"},
		{"/:category/:id", "/{category}/{id}"},
	}

	for _, tt := range tests {
		result := convertPathToChi(tt.input)
		assert.Equal(t, tt.expected, result, "Failed for input: %s", tt.input)
	}
}
