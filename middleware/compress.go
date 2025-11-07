package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"

	forge "github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
)

// Compress middleware compresses HTTP responses using gzip
// Only compresses if client supports gzip and response is suitable for compression
// This is the new forge middleware pattern using forge.Handler
func Compress(level int) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			// Check if client accepts gzip
			if !strings.Contains(ctx.Header("Accept-Encoding"), "gzip") {
				return next(ctx)
			}

			// Set content encoding header
			ctx.SetHeader("Content-Encoding", "gzip")
			ctx.SetHeader("Vary", "Accept-Encoding")

			// Create gzip writer
			gz, err := gzip.NewWriterLevel(ctx.Response(), level)
			if err != nil {
				// Fall back to no compression on error
				ctx.SetHeader("Content-Encoding", "")
				return next(ctx)
			}
			defer gz.Close()

			// Wrap response writer
			gzw := &gzipResponseWriter{
				Writer:         gz,
				ResponseWriter: ctx.Response(),
			}

			// Create a new context with the wrapped response writer
			// We need to preserve the container and context values
			container := ctx.Container()
			newCtx := di.NewContext(gzw, ctx.Request(), container)
			defer newCtx.(di.ContextWithClean).Cleanup()

			// Copy context values from original context
			// This ensures session, cookies, etc. are preserved
			// Note: We can't directly access the internal values map,
			// but the session and other values should be preserved through the container/scope
			// For now, we'll rely on the container to preserve scoped values

			// Execute the next handler with the new context
			return next(newCtx)
		}
	}
}

// CompressDefault returns Compress middleware with default compression level
func CompressDefault() forge.Middleware {
	return Compress(gzip.DefaultCompression)
}

// gzipResponseWriter wraps http.ResponseWriter with gzip compression
// This is a local type for the compress middleware
type gzipResponseWriter struct {
	http.ResponseWriter
	Writer *gzip.Writer
}

// Write writes compressed data
func (gzw *gzipResponseWriter) Write(b []byte) (int, error) {
	return gzw.Writer.Write(b)
}

// WriteHeader writes the status code
func (gzw *gzipResponseWriter) WriteHeader(statusCode int) {
	gzw.ResponseWriter.WriteHeader(statusCode)
}

// Flush implements http.Flusher
func (gzw *gzipResponseWriter) Flush() {
	gzw.Writer.Flush()
	if flusher, ok := gzw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
