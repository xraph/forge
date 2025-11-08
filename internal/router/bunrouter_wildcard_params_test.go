package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/di"
)

// TestBunRouterAdapter_WildcardParameters tests that wildcard params are extracted correctly.
func TestBunRouterAdapter_WildcardParameters(t *testing.T) {
	adapter := NewBunRouterAdapter()

	// Handler that extracts params from the request context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a forge context to access params
		ctx := di.NewContext(w, r, nil)

		// Test both "*" and "filepath" access
		wildcardParam := ctx.Param("*")
		filepathParam := ctx.Param("filepath")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(wildcardParam + "|" + filepathParam))
	})

	// Register wildcard route
	adapter.Handle("GET", "/static/*", handler)

	tests := []struct {
		name         string
		path         string
		expectStatus int
		expectBody   string
	}{
		{
			name:         "simple file",
			path:         "/static/style.css",
			expectStatus: 200,
			expectBody:   "style.css|style.css",
		},
		{
			name:         "nested path",
			path:         "/static/css/main.css",
			expectStatus: 200,
			expectBody:   "css/main.css|css/main.css",
		},
		{
			name:         "deeply nested",
			path:         "/static/assets/images/logo.png",
			expectStatus: 200,
			expectBody:   "assets/images/logo.png|assets/images/logo.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			adapter.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Equal(t, tt.expectBody, rec.Body.String())
		})
	}
}

// TestBunRouterAdapter_WildcardWithGroups tests wildcard params work correctly with route groups.
func TestBunRouterAdapter_WildcardWithGroups(t *testing.T) {
	router := NewRouter()

	// Create a group (simulating /api/auth prefix)
	group := router.Group("/api/auth")

	// Register wildcard route in the group
	err := group.GET("/dashboard/static/*", func(ctx Context) error {
		// Test both "*" and "filepath" param access
		wildcardParam := ctx.Param("*")
		filepathParam := ctx.Param("filepath")

		if wildcardParam == "" {
			return ctx.JSON(400, map[string]string{"error": "wildcard param is empty"})
		}

		return ctx.JSON(200, map[string]string{
			"wildcard": wildcardParam,
			"filepath": filepathParam,
		})
	})
	require.NoError(t, err)

	tests := []struct {
		name         string
		path         string
		expectStatus int
		expectParam  string
	}{
		{
			name:         "css file",
			path:         "/api/auth/dashboard/static/css/custom.css",
			expectStatus: 200,
			expectParam:  "css/custom.css",
		},
		{
			name:         "js file",
			path:         "/api/auth/dashboard/static/js/app.js",
			expectStatus: 200,
			expectParam:  "js/app.js",
		},
		{
			name:         "nested assets",
			path:         "/api/auth/dashboard/static/assets/images/logo.png",
			expectStatus: 200,
			expectParam:  "assets/images/logo.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code, "Response: %s", rec.Body.String())

			if tt.expectStatus == 200 {
				// Verify both wildcard and filepath params are set
				body := rec.Body.String()
				assert.Contains(t, body, tt.expectParam)
				assert.Contains(t, body, `"wildcard":"`+tt.expectParam+`"`)
				assert.Contains(t, body, `"filepath":"`+tt.expectParam+`"`)
			}
		})
	}
}

// TestBunRouterAdapter_NamedParameters tests that regular named parameters still work.
func TestBunRouterAdapter_NamedParameters(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, nil)

		userID := ctx.Param("id")
		postID := ctx.Param("postId")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(userID + "|" + postID))
	})

	// Register route with named parameters
	adapter.Handle("GET", "/users/:id/posts/:postId", handler)

	req := httptest.NewRequest(http.MethodGet, "/users/123/posts/456", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "123|456", rec.Body.String())
}

// TestBunRouterAdapter_MixedParameters tests routes with both named and wildcard params.
func TestBunRouterAdapter_MixedParameters(t *testing.T) {
	adapter := NewBunRouterAdapter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, nil)

		userID := ctx.Param("userId")
		filepath := ctx.Param("*")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(userID + "|" + filepath))
	})

	// Register route with both named param and wildcard
	adapter.Handle("GET", "/users/:userId/files/*", handler)

	req := httptest.NewRequest(http.MethodGet, "/users/john/files/docs/report.pdf", nil)
	rec := httptest.NewRecorder()

	adapter.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "john|docs/report.pdf", rec.Body.String())
}
