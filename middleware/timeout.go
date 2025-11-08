package middleware

import (
	"context"
	"maps"
	"net/http"
	"sync"
	"time"

	forge "github.com/xraph/forge"
)

// safeResponseWriter wraps http.ResponseWriter to prevent race conditions
// by completely buffering the response until flush is called.
type safeResponseWriter struct {
	http.ResponseWriter

	mu      sync.Mutex
	header  http.Header
	code    int
	body    []byte
	flushed bool
}

func newSafeResponseWriter(w http.ResponseWriter) *safeResponseWriter {
	return &safeResponseWriter{
		ResponseWriter: w,
		header:         make(http.Header),
		code:           http.StatusOK,
	}
}

func (w *safeResponseWriter) Header() http.Header {
	return w.header
}

func (w *safeResponseWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.flushed {
		w.code = code
	}
}

func (w *safeResponseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.flushed {
		w.body = append(w.body, data...)

		return len(data), nil
	}

	return w.ResponseWriter.Write(data)
}

func (w *safeResponseWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.flushed {
		// Copy headers to underlying writer
		maps.Copy(w.ResponseWriter.Header(), w.header)

		w.ResponseWriter.WriteHeader(w.code)

		if len(w.body) > 0 {
			_, _ = w.ResponseWriter.Write(w.body)
		}

		w.flushed = true
	}
}

// Timeout middleware enforces a timeout on request handling
// Returns http.StatusGatewayTimeout if request exceeds duration
// Note: This middleware uses http.Handler pattern due to goroutine requirements.
func Timeout(duration time.Duration, logger forge.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create context with timeout
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()

			// Wrap response writer to prevent race conditions
			safeW := newSafeResponseWriter(w)

			// Channel to signal completion
			done := make(chan struct{})

			// Run handler in goroutine
			go func() {
				defer close(done)

				next.ServeHTTP(safeW, r.WithContext(ctx))
			}()

			// Wait for completion or timeout
			select {
			case <-done:
				// Request completed successfully, flush any buffered data
				safeW.flush()

				return
			case <-ctx.Done():
				// Timeout occurred - write timeout response directly to underlying writer
				if logger != nil {
					logger.Warn("request timeout")
				}
				// Write timeout response directly to avoid race with buffered response
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte("Gateway Timeout"))
			}
		})
	}
}
