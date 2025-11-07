package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	forge "github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
)

func TestRequestID_Generate(t *testing.T) {
	handler := RequestID()(func(ctx forge.Context) error {
		requestID := GetRequestIDFromForgeContext(ctx)
		assert.NotEmpty(t, requestID)
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
}

func TestRequestID_UseExisting(t *testing.T) {
	existingID := "existing-request-id"

	handler := RequestID()(func(ctx forge.Context) error {
		requestID := GetRequestIDFromForgeContext(ctx)
		assert.Equal(t, existingID, requestID)
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, existingID, rec.Header().Get("X-Request-ID"))
}

func TestGetRequestID_NoContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	requestID := GetRequestID(req.Context())
	assert.Empty(t, requestID)
}
