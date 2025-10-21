package middleware

import (
	"context"
	"net/http"
	"time"

	forge "github.com/xraph/forge/v2"
)

// Timeout middleware enforces a timeout on request handling
// Returns 504 Gateway Timeout if request exceeds duration
func Timeout(duration time.Duration, logger forge.Logger) forge.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create context with timeout
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()

			// Channel to signal completion
			done := make(chan struct{})

			// Run handler in goroutine
			go func() {
				next.ServeHTTP(w, r.WithContext(ctx))
				close(done)
			}()

			// Wait for completion or timeout
			select {
			case <-done:
				// Request completed successfully
				return
			case <-ctx.Done():
				// Timeout occurred
				if logger != nil {
					logger.Warn("request timeout")
				}
				http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
			}
		})
	}
}
