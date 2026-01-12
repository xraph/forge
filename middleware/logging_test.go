package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	forge "github.com/xraph/forge"
	forge_http "github.com/xraph/forge/internal/http"
)

func TestLogging_Success(t *testing.T) {
	logger := &mockLogger{}
	handler := Logging(logger)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	ctx := forge_http.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, logger.messages, 2) // started + completed
	assert.Equal(t, "request started", logger.messages[0])
	assert.Equal(t, "request completed", logger.messages[1])
}

func TestLogging_ExcludePath(t *testing.T) {
	logger := &mockLogger{}
	config := DefaultLoggingConfig()
	handler := LoggingWithConfig(logger, config)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	ctx := forge_http.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, logger.messages) // Health path excluded
}

func TestDefaultLoggingConfig(t *testing.T) {
	config := DefaultLoggingConfig()

	assert.False(t, config.IncludeHeaders)
	assert.Contains(t, config.ExcludePaths, "/health")
	assert.Contains(t, config.SensitiveHeaders, "Authorization")
}
