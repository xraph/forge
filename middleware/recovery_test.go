package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	forge "github.com/xraph/forge"
)

type mockLogger struct {
	messages []string
}

func (m *mockLogger) Debug(msg string, fields ...forge.Field)      { m.messages = append(m.messages, msg) }
func (m *mockLogger) Info(msg string, fields ...forge.Field)       { m.messages = append(m.messages, msg) }
func (m *mockLogger) Warn(msg string, fields ...forge.Field)       { m.messages = append(m.messages, msg) }
func (m *mockLogger) Error(msg string, fields ...forge.Field)      { m.messages = append(m.messages, msg) }
func (m *mockLogger) Fatal(msg string, fields ...forge.Field)      { m.messages = append(m.messages, msg) }
func (m *mockLogger) Debugf(template string, args ...interface{})  {}
func (m *mockLogger) Infof(template string, args ...interface{})   {}
func (m *mockLogger) Warnf(template string, args ...interface{})   {}
func (m *mockLogger) Errorf(template string, args ...interface{})  {}
func (m *mockLogger) Fatalf(template string, args ...interface{})  {}
func (m *mockLogger) With(fields ...forge.Field) forge.Logger      { return m }
func (m *mockLogger) WithContext(ctx context.Context) forge.Logger { return m }
func (m *mockLogger) Named(name string) forge.Logger               { return m }
func (m *mockLogger) Sugar() forge.SugarLogger                     { return nil }
func (m *mockLogger) Sync() error                                  { return nil }

func TestRecovery_NoPanic(t *testing.T) {
	logger := &mockLogger{}
	handler := Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
	assert.Empty(t, logger.messages)
}

func TestRecovery_WithPanic(t *testing.T) {
	logger := &mockLogger{}
	handler := Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 500, rec.Code)
	assert.Contains(t, rec.Body.String(), "Internal Server Error")
	assert.Len(t, logger.messages, 1)
	assert.Contains(t, logger.messages[0], "panic recovered")
	assert.Contains(t, logger.messages[0], "something went wrong")
}
