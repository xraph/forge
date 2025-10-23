package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	forge "github.com/xraph/forge"
)

// RequestIDContextKey is the context key for storing request ID
type RequestIDContextKey string

const requestIDKey RequestIDContextKey = "request_id"

// RequestID middleware adds a unique request ID to each request
// If X-Request-ID header is present, it uses that, otherwise generates a new UUID
func RequestID() forge.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for existing request ID in header
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				// Generate new UUID if not present
				requestID = uuid.New().String()
			}

			// Set response header
			w.Header().Set("X-Request-ID", requestID)

			// Add to context
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetRequestID retrieves the request ID from context
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}
