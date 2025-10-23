package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompress_WithGzipSupport(t *testing.T) {
	handler := Compress(gzip.DefaultCompression)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("compressed content"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Contains(t, rec.Header().Get("Vary"), "Accept-Encoding")

	// Decompress to verify content
	gz, err := gzip.NewReader(rec.Body)
	assert.NoError(t, err)
	defer gz.Close()

	content, err := io.ReadAll(gz)
	assert.NoError(t, err)
	assert.Equal(t, "compressed content", string(content))
}

func TestCompress_WithoutGzipSupport(t *testing.T) {
	handler := Compress(gzip.DefaultCompression)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("uncompressed content"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "uncompressed content", rec.Body.String())
}

func TestCompressDefault(t *testing.T) {
	handler := CompressDefault()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("content"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
}

func TestCompress_InvalidLevel(t *testing.T) {
	handler := Compress(999)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("content"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should fall back to no compression on error
	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "content", rec.Body.String())
}
