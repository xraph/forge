package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	forge "github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
)

type mockLogger struct {
	messages []string
}

func (m *mockLogger) Debug(msg string, fields ...forge.Field)      { m.messages = append(m.messages, msg) }
func (m *mockLogger) Info(msg string, fields ...forge.Field)       { m.messages = append(m.messages, msg) }
func (m *mockLogger) Warn(msg string, fields ...forge.Field)       { m.messages = append(m.messages, msg) }
func (m *mockLogger) Error(msg string, fields ...forge.Field)      { m.messages = append(m.messages, msg) }
func (m *mockLogger) Fatal(msg string, fields ...forge.Field)      { m.messages = append(m.messages, msg) }
func (m *mockLogger) Debugf(template string, args ...any)          {}
func (m *mockLogger) Infof(template string, args ...any)           {}
func (m *mockLogger) Warnf(template string, args ...any)           {}
func (m *mockLogger) Errorf(template string, args ...any)          {}
func (m *mockLogger) Fatalf(template string, args ...any)          {}
func (m *mockLogger) With(fields ...forge.Field) forge.Logger      { return m }
func (m *mockLogger) WithContext(ctx context.Context) forge.Logger { return m }
func (m *mockLogger) Named(name string) forge.Logger               { return m }
func (m *mockLogger) Sugar() forge.SugarLogger                     { return nil }
func (m *mockLogger) Sync() error                                  { return nil }

func TestRecovery_NoPanic(t *testing.T) {
	logger := &mockLogger{}
	handler := Recovery(logger)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	err := handler(ctx)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
	assert.Empty(t, logger.messages)
}

func TestRecovery_WithPanic(t *testing.T) {
	logger := &mockLogger{}
	handler := Recovery(logger)(func(ctx forge.Context) error {
		panic("something went wrong")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "Internal Server Error")
	assert.Len(t, logger.messages, 1)
	assert.Contains(t, logger.messages[0], "panic recovered")
	assert.Contains(t, logger.messages[0], "something went wrong")
}
