package middleware

import (
	"context"
	"strings"

	"github.com/google/uuid"
	forge "github.com/xraph/forge"
)

// RequestIDContextKey is the context key for storing request ID.
type RequestIDContextKey string

const requestIDKey RequestIDContextKey = "request_id"

// RequestIDForgeKey is the forge-context key under which the request ID is
// stored. Exported so callers can read it without guessing the string.
const RequestIDForgeKey = "request_id"

// maxRequestIDLength bounds an inbound X-Request-ID. The value is echoed into
// the response and into every log line for the request, so an unbounded one lets
// a client inflate log volume at will.
const maxRequestIDLength = 128

// RequestID middleware adds a unique request ID to each request
// If X-Request-ID header is present and well-formed, it is used; otherwise a new
// UUID is generated.
func RequestID() forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			// Check for existing request ID in header
			requestID := sanitizeRequestID(ctx.Request().Header.Get("X-Request-ID"))
			if requestID == "" {
				// Generate new UUID if not present or not usable
				requestID = uuid.NewString()
			}

			// Set response header
			ctx.Response().Header().Set("X-Request-ID", requestID)

			// Add to Forge context
			ctx.Set(RequestIDForgeKey, requestID)

			// Also store on the stdlib request context so GetRequestID works for
			// code that only has a context.Context — background jobs, database
			// hooks, anything below the handler. Previously nothing wrote this
			// key, so GetRequestID always returned "".
			ctx.WithContext(context.WithValue(ctx.Context(), requestIDKey, requestID))

			return next(ctx)
		}
	}
}

// sanitizeRequestID accepts a client-supplied correlation ID only if it is safe
// to echo and to log: bounded in length and limited to printable, non-space
// characters. Returns "" when the value should be replaced with a fresh UUID.
//
// Rejecting rather than escaping keeps the ID usable as a log-correlation token;
// a value carrying spaces or control characters would break naive log parsers
// even though net/http itself strips newlines from header values.
func sanitizeRequestID(v string) string {
	if v == "" || len(v) > maxRequestIDLength {
		return ""
	}

	if strings.ContainsFunc(v, func(r rune) bool {
		return r < '!' || r > '~'
	}) {
		return ""
	}

	return v
}

// GetRequestID retrieves the request ID from standard context.
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}

	return ""
}

// GetRequestIDFromForgeContext retrieves the request ID from Forge context.
func GetRequestIDFromForgeContext(ctx forge.Context) string {
	val := ctx.Get(RequestIDForgeKey)
	if val == nil {
		return ""
	}

	if requestID, ok := val.(string); ok {
		return requestID
	}

	return ""
}
