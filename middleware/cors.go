package middleware

import (
	"net/http"
	"strconv"
	"strings"

	forge "github.com/xraph/forge"
)

// CORSConfig defines configuration for CORS middleware.
type CORSConfig struct {
	// AllowOrigin defines the allowed origin(s)
	// Use "*" for all origins, or specific origin like "https://example.com"
	AllowOrigin string

	// AllowMethods defines allowed HTTP methods
	AllowMethods []string

	// AllowHeaders defines allowed request headers
	AllowHeaders []string

	// ExposeHeaders defines headers exposed to the client
	ExposeHeaders []string

	// AllowCredentials indicates whether credentials are allowed
	AllowCredentials bool

	// MaxAge indicates how long preflight requests can be cached (seconds)
	MaxAge int
}

// DefaultCORSConfig returns a permissive CORS configuration suitable for development.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigin:      "*",
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH", "HEAD"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: false,
		MaxAge:           3600,
	}
}

// CORS returns middleware that handles Cross-Origin Resource Sharing.
func CORS(config CORSConfig) forge.Middleware {
	// Pre-compute header values
	allowMethods := strings.Join(config.AllowMethods, ", ")
	allowHeaders := strings.Join(config.AllowHeaders, ", ")
	exposeHeaders := strings.Join(config.ExposeHeaders, ", ")
	maxAge := strconv.Itoa(config.MaxAge)

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			w := ctx.Response()
			r := ctx.Request()

			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", config.AllowOrigin)
			w.Header().Set("Access-Control-Allow-Methods", allowMethods)
			w.Header().Set("Access-Control-Allow-Headers", allowHeaders)

			if len(config.ExposeHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", exposeHeaders)
			}

			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if config.MaxAge > 0 {
				w.Header().Set("Access-Control-Max-Age", maxAge)
			}

			// Handle preflight OPTIONS request
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)

				return nil
			}

			return next(ctx)
		}
	}
}
