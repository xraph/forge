package middleware

import (
	"net/http"
	"strconv"
	"strings"

	forge "github.com/xraph/forge"
)

// CORSConfig defines configuration for CORS middleware.
type CORSConfig struct {
	// AllowOrigins defines the allowed origin(s)
	// Use []string{"*"} for all origins (not compatible with AllowCredentials=true)
	// Or specific origins like []string{"https://example.com", "https://app.example.com"}
	AllowOrigins []string

	// AllowMethods defines allowed HTTP methods
	AllowMethods []string

	// AllowHeaders defines allowed request headers
	AllowHeaders []string

	// ExposeHeaders defines headers exposed to the client
	ExposeHeaders []string

	// AllowCredentials indicates whether credentials are allowed
	// Cannot be true when AllowOrigins contains "*"
	AllowCredentials bool

	// MaxAge indicates how long preflight requests can be cached (seconds)
	MaxAge int
}

// DefaultCORSConfig returns a permissive CORS configuration suitable for development.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH", "HEAD"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: false,
		MaxAge:           3600,
	}
}

// isOriginAllowed checks if the request origin is in the allowed list.
func isOriginAllowed(requestOrigin string, allowedOrigins []string) bool {
	if len(allowedOrigins) == 0 {
		return false
	}

	for _, allowed := range allowedOrigins {
		if allowed == "*" {
			return true
		}
		if allowed == requestOrigin {
			return true
		}
		// Support wildcard subdomains like *.example.com
		if strings.HasPrefix(allowed, "*.") {
			domain := allowed[2:]
			if strings.HasSuffix(requestOrigin, domain) {
				return true
			}
		}
	}

	return false
}

// getAllowOrigin returns the origin value to set in response headers.
func getAllowOrigin(requestOrigin string, config CORSConfig) string {
	// If no origins configured, deny
	if len(config.AllowOrigins) == 0 {
		return ""
	}

	// If wildcard and no credentials, allow all
	for _, origin := range config.AllowOrigins {
		if origin == "*" {
			if config.AllowCredentials {
				// SECURITY: Cannot use "*" with credentials
				// Return empty to deny the request
				return ""
			}
			return "*"
		}
	}

	// Specific origin validation
	if isOriginAllowed(requestOrigin, config.AllowOrigins) {
		// Return the actual request origin for proper CORS
		return requestOrigin
	}

	return ""
}

// addVaryHeader adds a value to the Vary header if not already present.
func addVaryHeader(header http.Header, value string) {
	vary := header.Get("Vary")
	if vary == "" {
		header.Set("Vary", value)
		return
	}

	// Check if value already exists
	parts := strings.Split(vary, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == value {
			return
		}
	}

	header.Set("Vary", vary+", "+value)
}

// CORS returns middleware that handles Cross-Origin Resource Sharing.
func CORS(config CORSConfig) forge.Middleware {
	// Validate configuration at initialization
	hasWildcard := false
	for _, origin := range config.AllowOrigins {
		if origin == "*" {
			hasWildcard = true
			break
		}
	}

	if hasWildcard && config.AllowCredentials {
		panic("CORS misconfiguration: AllowCredentials cannot be true when AllowOrigins contains '*'")
	}

	// Pre-compute header values
	allowMethods := strings.Join(config.AllowMethods, ", ")
	allowHeaders := strings.Join(config.AllowHeaders, ", ")
	exposeHeaders := strings.Join(config.ExposeHeaders, ", ")
	maxAge := strconv.Itoa(config.MaxAge)

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			w := ctx.Response()
			r := ctx.Request()

			// Get origin from request
			origin := r.Header.Get("Origin")

			// No origin header means not a CORS request, skip CORS handling
			if origin == "" {
				return next(ctx)
			}

			// Determine allowed origin for this request
			allowOrigin := getAllowOrigin(origin, config)

			// If origin not allowed, continue but don't set CORS headers
			// The browser will block the response
			if allowOrigin == "" {
				return next(ctx)
			}

			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)

			// Add Vary header when not using wildcard for proper caching
			if allowOrigin != "*" {
				addVaryHeader(w.Header(), "Origin")
			}

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
				// Validate it's a valid CORS preflight request
				requestMethod := r.Header.Get("Access-Control-Request-Method")
				if requestMethod == "" {
					// Not a CORS preflight, just a regular OPTIONS request
					return next(ctx)
				}

				// Additional validation: check if requested method is allowed
				methodAllowed := false
				for _, method := range config.AllowMethods {
					if method == requestMethod {
						methodAllowed = true
						break
					}
				}

				if !methodAllowed {
					w.WriteHeader(http.StatusForbidden)
					return nil
				}

				// Validate requested headers if present
				requestHeaders := r.Header.Get("Access-Control-Request-Headers")
				if requestHeaders != "" {
					// Split and check each requested header
					headers := strings.Split(requestHeaders, ",")
					for _, header := range headers {
						header = strings.TrimSpace(header)
						headerAllowed := false
						headerLower := strings.ToLower(header)

						for _, allowedHeader := range config.AllowHeaders {
							if strings.ToLower(allowedHeader) == headerLower {
								headerAllowed = true
								break
							}
						}

						if !headerAllowed {
							w.WriteHeader(http.StatusForbidden)
							return nil
						}
					}
				}

				// Preflight successful
				w.WriteHeader(http.StatusNoContent)
				return nil
			}

			return next(ctx)
		}
	}
}
