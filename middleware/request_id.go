package middleware

import (
	"context"

	"github.com/google/uuid"
	forge "github.com/xraph/forge"
)

// RequestIDContextKey is the context key for storing request ID
type RequestIDContextKey string

const requestIDKey RequestIDContextKey = "request_id"

// RequestID middleware adds a unique request ID to each request
// If X-Request-ID header is present, it uses that, otherwise generates a new UUID
func RequestID() forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			// Check for existing request ID in header
			requestID := ctx.Request().Header.Get("X-Request-ID")
			if requestID == "" {
				// Generate new UUID if not present
				requestID = uuid.New().String()
			}

			// Set response header
			ctx.Response().Header().Set("X-Request-ID", requestID)

			// Add to Forge context
			ctx.Set("request_id", requestID)

			return next(ctx)
		}
	}
}

// GetRequestID retrieves the request ID from standard context
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// GetRequestIDFromForgeContext retrieves the request ID from Forge context
func GetRequestIDFromForgeContext(ctx forge.Context) string {
	val := ctx.Get("request_id")
	if val == nil {
		return ""
	}
	if requestID, ok := val.(string); ok {
		return requestID
	}
	return ""
}
