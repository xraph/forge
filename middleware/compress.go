package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"

	forge "github.com/xraph/forge"
)

// Compress middleware compresses HTTP responses using gzip
// Only compresses if client supports gzip and response is suitable for compression
func Compress(level int) forge.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if client accepts gzip
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			// Set content encoding header
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Add("Vary", "Accept-Encoding")

			// Create gzip writer
			gz, err := gzip.NewWriterLevel(w, level)
			if err != nil {
				// Fall back to no compression on error
				w.Header().Del("Content-Encoding")
				next.ServeHTTP(w, r)
				return
			}
			defer gz.Close()

			// Wrap response writer
			gzw := &gzipResponseWriter{
				Writer:         gz,
				ResponseWriter: w,
			}

			next.ServeHTTP(gzw, r)
		})
	}
}

// CompressDefault returns Compress middleware with default compression level
func CompressDefault() forge.Middleware {
	return Compress(gzip.DefaultCompression)
}
