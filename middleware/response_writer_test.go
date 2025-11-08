package middleware

import (
	"bufio"
	"compress/gzip"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewResponseWriter(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	assert.NotNil(t, rw)
	assert.Equal(t, http.StatusOK, rw.Status())
	assert.Equal(t, 0, rw.Size())
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	rw.WriteHeader(http.StatusNotFound)

	assert.Equal(t, http.StatusNotFound, rw.Status())
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestResponseWriter_WriteHeader_MultipleCall(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	rw.WriteHeader(http.StatusNotFound)
	rw.WriteHeader(http.StatusInternalServerError) // Should be ignored

	assert.Equal(t, http.StatusNotFound, rw.Status())
}

func TestResponseWriter_Write(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	n, err := rw.Write([]byte("hello world"))

	assert.NoError(t, err)
	assert.Equal(t, 11, n)
	assert.Equal(t, 11, rw.Size())
	assert.Equal(t, http.StatusOK, rw.Status())
	assert.Equal(t, "hello world", w.Body.String())
}

func TestResponseWriter_MultipleWrites(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	rw.Write([]byte("hello "))
	rw.Write([]byte("world"))

	assert.Equal(t, 11, rw.Size())
	assert.Equal(t, "hello world", w.Body.String())
}

func TestResponseWriter_Flush(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	// Should not panic
	rw.Flush()
}

func TestResponseWriter_Hijack_NotSupported(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	conn, rw2, err := rw.Hijack()

	assert.Error(t, err)
	assert.Equal(t, http.ErrNotSupported, err)
	assert.Nil(t, conn)
	assert.Nil(t, rw2)
}

func TestResponseWriter_Push_NotSupported(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	err := rw.Push("/test", nil)

	assert.Error(t, err)
	assert.Equal(t, http.ErrNotSupported, err)
}

func TestGzipResponseWriter_Write(t *testing.T) {
	rec := httptest.NewRecorder()

	gz := gzip.NewWriter(rec)
	defer gz.Close()

	gzw := &gzipResponseWriter{
		Writer:         gz,
		ResponseWriter: rec,
	}

	n, err := gzw.Write([]byte("test"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n)
}

func TestGzipResponseWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Length", "123")

	gz := gzip.NewWriter(rec)
	gzw := &gzipResponseWriter{
		Writer:         gz,
		ResponseWriter: rec,
	}

	gzw.WriteHeader(http.StatusOK)

	// Content-Length should be removed
	assert.Empty(t, rec.Header().Get("Content-Length"))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGzipResponseWriter_Flush(t *testing.T) {
	rec := httptest.NewRecorder()

	gz := gzip.NewWriter(rec)
	defer gz.Close()

	gzw := &gzipResponseWriter{
		Writer:         gz,
		ResponseWriter: rec,
	}

	// Should not panic
	gzw.Flush()
}

// Hijackable recorder for testing.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack test")
}

func TestResponseWriter_Hijack_Supported(t *testing.T) {
	w := &hijackableRecorder{httptest.NewRecorder()}
	rw := NewResponseWriter(w)

	_, _, err := rw.Hijack()
	assert.Error(t, err)
	assert.Equal(t, "hijack test", err.Error())
}
