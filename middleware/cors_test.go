package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	forge "github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
)

func TestCORS_DefaultConfig(t *testing.T) {
	config := DefaultCORSConfig()

	assert.Contains(t, config.AllowOrigins, "*")
	assert.Contains(t, config.AllowMethods, "GET")
	assert.Contains(t, config.AllowHeaders, "Content-Type")
	assert.Equal(t, 3600, config.MaxAge)
	assert.False(t, config.AllowCredentials)
}

func TestCORS_RegularRequest(t *testing.T) {
	config := DefaultCORSConfig()
	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"))
	// Should not have Vary header when using wildcard
	assert.Empty(t, rec.Header().Get("Vary"))
}

func TestCORS_NoOriginHeader(t *testing.T) {
	config := DefaultCORSConfig()
	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No Origin header - not a CORS request
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	// No CORS headers should be set
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_PreflightRequest(t *testing.T) {
	config := DefaultCORSConfig()
	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"))
}

func TestCORS_PreflightWithDisallowedMethod(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "DELETE") // Not allowed

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCORS_PreflightWithDisallowedHeader(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "X-Custom-Header") // Not allowed

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCORS_NonPreflightOptions(t *testing.T) {
	config := DefaultCORSConfig()
	handlerCalled := false
	handler := CORS(config)(func(ctx forge.Context) error {
		handlerCalled = true

		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	// No Access-Control-Request-Method header - not a CORS preflight
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	// Should call next handler since it's not a preflight
	assert.True(t, handlerCalled)
}

func TestCORS_WithCredentials(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           7200,
		ExposeHeaders:    []string{"X-Request-ID"},
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "7200", rec.Header().Get("Access-Control-Max-Age"))
	assert.Equal(t, "X-Request-ID", rec.Header().Get("Access-Control-Expose-Headers"))
	// Should have Vary header when using specific origin
	assert.Contains(t, rec.Header().Get("Vary"), "Origin")
}

func TestCORS_CredentialsWithWildcardPanics(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true, // Invalid combination
		MaxAge:           3600,
	}

	assert.Panics(t, func() {
		CORS(config)
	}, "Should panic when AllowCredentials is true with wildcard origin")
}

func TestCORS_SpecificOriginAllowed(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"https://example.com", "https://app.example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	// Test allowed origin
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Vary"), "Origin")
}

func TestCORS_SpecificOriginDenied(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	// Test disallowed origin
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://evil.com")

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	// Handler still executes, but no CORS headers
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_WildcardSubdomain(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"*.example.com"},
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	// Test matching subdomain
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://app.example.com")

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))

	// Test non-matching origin
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.Header.Set("Origin", "https://evil.com")

	rec2 := httptest.NewRecorder()
	ctx2 := di.NewContext(rec2, req2, nil)

	err2 := handler(ctx2)
	require.NoError(t, err2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Empty(t, rec2.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_VaryHeader(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	// Simulate another middleware that sets Vary header first
	otherMiddleware := func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			ctx.Response().Header().Set("Vary", "Accept-Encoding")

			return next(ctx)
		}
	}

	// Chain: otherMiddleware -> CORS -> handler
	handler := otherMiddleware(CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	vary := rec.Header().Get("Vary")
	assert.Contains(t, vary, "Accept-Encoding")
	assert.Contains(t, vary, "Origin")
}

func TestCORS_EmptyOriginsList(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{}, // Empty
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://example.com")

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	// Should not set CORS headers with empty origins list
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_CaseInsensitiveHeaders(t *testing.T) {
	config := CORSConfig{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"POST"},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	// Test preflight with different header casing
	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, AUTHORIZATION") // Different casing

	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)
	require.NoError(t, err)

	// Should accept despite different casing
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
