package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	forge "github.com/xraph/forge"
)

// TestCORS_PreflightWithoutExplicitOPTIONSHandler tests that CORS preflight
// works even when no explicit OPTIONS handler is registered for the route.
// This is the most common scenario and the one that was failing before.
func TestCORS_PreflightWithoutExplicitOPTIONSHandler(t *testing.T) {
	app := forge.New()
	router := app.Router()

	// Apply CORS middleware globally (required for preflight to work)
	cors := CORS(DefaultCORSConfig())
	router.UseGlobal(cors)

	// Register only a GET handler (no OPTIONS handler)
	err := router.GET("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"user": "john"})
	})
	require.NoError(t, err)

	// Test preflight OPTIONS request
	req := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Should return 204 No Content (successful preflight)
	assert.Equal(t, http.StatusNoContent, rec.Code, "Expected 204 No Content for preflight, got %d. Body: %s", rec.Code, rec.Body.String())

	// Should have CORS headers
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"), "Missing Access-Control-Allow-Origin header")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"), "Missing Access-Control-Allow-Methods header")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"), "Missing Access-Control-Allow-Headers header")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Max-Age"), "Missing Access-Control-Max-Age header")
}

// TestCORS_ActualRequestAfterPreflight tests that the actual request works after preflight.
func TestCORS_ActualRequestAfterPreflight(t *testing.T) {
	app := forge.New()
	router := app.Router()

	// Apply CORS middleware globally
	cors := CORS(DefaultCORSConfig())
	router.UseGlobal(cors)

	// Register GET handler
	err := router.GET("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"user": "john"})
	})
	require.NoError(t, err)

	// Test actual GET request (after preflight)
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Should return 200 OK
	assert.Equal(t, http.StatusOK, rec.Code)

	// Should have CORS headers
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"))
}

// TestCORS_PreflightWithSpecificOrigin tests preflight with a specific allowed origin.
func TestCORS_PreflightWithSpecificOrigin(t *testing.T) {
	app := forge.New()
	router := app.Router()

	// Configure CORS with specific origin
	corsConfig := CORSConfig{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           3600,
	}
	cors := CORS(corsConfig)
	router.UseGlobal(cors)

	// Register POST handler
	err := router.POST("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusCreated, map[string]string{"id": "123"})
	})
	require.NoError(t, err)

	// Test preflight OPTIONS request
	req := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Should return 204 No Content
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Should have CORS headers with specific origin
	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	assert.Contains(t, rec.Header().Get("Vary"), "Origin")
}

// TestCORS_PreflightWithDisallowedOrigin tests that preflight fails for disallowed origins.
func TestCORS_PreflightWithDisallowedOrigin(t *testing.T) {
	app := forge.New()
	router := app.Router()

	// Configure CORS with specific origin
	corsConfig := CORSConfig{
		AllowOrigins: []string{"https://example.com"},
		AllowMethods: []string{"GET"},
		AllowHeaders: []string{"Content-Type"},
		MaxAge:       3600,
	}
	cors := CORS(corsConfig)
	router.UseGlobal(cors)

	// Register GET handler
	err := router.GET("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"user": "john"})
	})
	require.NoError(t, err)

	// Test preflight OPTIONS request from disallowed origin
	req := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	req.Header.Set("Origin", "https://evil.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// The request should complete, but without CORS headers
	// The browser will block the response
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORS_PreflightMultipleRoutes tests that CORS works for multiple routes.
func TestCORS_PreflightMultipleRoutes(t *testing.T) {
	app := forge.New()
	router := app.Router()

	// Apply CORS middleware globally
	cors := CORS(DefaultCORSConfig())
	router.UseGlobal(cors)

	// Register multiple handlers
	err := router.GET("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"user": "john"})
	})
	require.NoError(t, err)

	err = router.POST("/api/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusCreated, map[string]string{"id": "123"})
	})
	require.NoError(t, err)

	err = router.GET("/api/posts", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, []string{"post1", "post2"})
	})
	require.NoError(t, err)

	// Test preflight for /api/users
	req1 := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	req1.Header.Set("Origin", "https://example.com")
	req1.Header.Set("Access-Control-Request-Method", "POST")

	rec1 := httptest.NewRecorder()

	router.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusNoContent, rec1.Code)
	assert.Equal(t, "*", rec1.Header().Get("Access-Control-Allow-Origin"))

	// Test preflight for /api/posts
	req2 := httptest.NewRequest(http.MethodOptions, "/api/posts", nil)
	req2.Header.Set("Origin", "https://example.com")
	req2.Header.Set("Access-Control-Request-Method", "GET")

	rec2 := httptest.NewRecorder()

	router.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusNoContent, rec2.Code)
	assert.Equal(t, "*", rec2.Header().Get("Access-Control-Allow-Origin"))
}

// TestCORS_PreflightWithRouteGroups tests CORS with route groups.
func TestCORS_PreflightWithRouteGroups(t *testing.T) {
	app := forge.New()
	router := app.Router()

	// Apply CORS middleware globally (required for preflight in groups)
	cors := CORS(DefaultCORSConfig())
	router.UseGlobal(cors)

	// Create a route group
	api := router.Group("/api")

	err := api.GET("/users", func(ctx forge.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"user": "john"})
	})
	require.NoError(t, err)

	// Test preflight for grouped route
	req := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}
