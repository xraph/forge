package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	forge "github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
)

func TestCORS_DefaultConfig(t *testing.T) {
	config := DefaultCORSConfig()

	assert.Equal(t, "*", config.AllowOrigin)
	assert.Contains(t, config.AllowMethods, "GET")
	assert.Contains(t, config.AllowHeaders, "Content-Type")
	assert.Equal(t, 3600, config.MaxAge)
}

func TestCORS_RegularRequest(t *testing.T) {
	config := DefaultCORSConfig()
	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Headers"))
}

func TestCORS_PreflightRequest(t *testing.T) {
	config := DefaultCORSConfig()
	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_WithCredentials(t *testing.T) {
	config := CORSConfig{
		AllowOrigin:      "https://example.com",
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           7200,
		ExposeHeaders:    []string{"X-Request-ID"},
	}

	handler := CORS(config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "7200", rec.Header().Get("Access-Control-Max-Age"))
	assert.Equal(t, "X-Request-ID", rec.Header().Get("Access-Control-Expose-Headers"))
}
