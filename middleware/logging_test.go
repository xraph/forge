package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogging_Success(t *testing.T) {
	logger := &mockLogger{}
	handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Len(t, logger.messages, 2) // started + completed
	assert.Equal(t, "request started", logger.messages[0])
	assert.Equal(t, "request completed", logger.messages[1])
}

func TestLogging_ExcludePath(t *testing.T) {
	logger := &mockLogger{}
	config := DefaultLoggingConfig()
	handler := LoggingWithConfig(logger, config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Empty(t, logger.messages) // Health path excluded
}

func TestDefaultLoggingConfig(t *testing.T) {
	config := DefaultLoggingConfig()

	assert.False(t, config.IncludeHeaders)
	assert.Contains(t, config.ExcludePaths, "/health")
	assert.Contains(t, config.SensitiveHeaders, "Authorization")
}
