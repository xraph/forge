package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	forge "github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
)

func TestCompress_WithGzipSupport(t *testing.T) {
	// Create a forge handler
	handler := Compress(gzip.DefaultCompression)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "compressed content")
	})

	// Convert to http.Handler for testing
	httpHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, nil)
		defer func() {
			if cleanup, ok := ctx.(interface{ Cleanup() }); ok {
				cleanup.Cleanup()
			}
		}()
		_ = handler(ctx)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	httpHandler(rec, req)

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
	// Create a forge handler
	handler := Compress(gzip.DefaultCompression)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "uncompressed content")
	})

	// Convert to http.Handler for testing
	httpHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, nil)
		defer func() {
			if cleanup, ok := ctx.(interface{ Cleanup() }); ok {
				cleanup.Cleanup()
			}
		}()
		_ = handler(ctx)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	httpHandler(rec, req)

	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "uncompressed content", rec.Body.String())
}

func TestCompressDefault(t *testing.T) {
	// Create a forge handler
	handler := CompressDefault()(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "content")
	})

	// Convert to http.Handler for testing
	httpHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, nil)
		defer func() {
			if cleanup, ok := ctx.(interface{ Cleanup() }); ok {
				cleanup.Cleanup()
			}
		}()
		_ = handler(ctx)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	httpHandler(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
}

func TestCompress_InvalidLevel(t *testing.T) {
	// Create a forge handler
	handler := Compress(999)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "content")
	})

	// Convert to http.Handler for testing
	httpHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, nil)
		defer func() {
			if cleanup, ok := ctx.(interface{ Cleanup() }); ok {
				cleanup.Cleanup()
			}
		}()
		_ = handler(ctx)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	httpHandler(rec, req)

	// Should fall back to no compression on error
	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "content", rec.Body.String())
}
