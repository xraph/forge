package security

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/xraph/forge"
)

// CORSConfig holds CORS configuration
type CORSConfig struct {
	// Enabled determines if CORS is enabled
	Enabled bool

	// AllowOrigins is a list of allowed origins
	// Use ["*"] to allow all origins (not recommended for production)
	// Example: ["https://example.com", "https://app.example.com"]
	AllowOrigins []string

	// AllowMethods is a list of allowed HTTP methods
	// Default: ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]
	AllowMethods []string

	// AllowHeaders is a list of allowed request headers
	// Default: ["Origin", "Content-Type", "Accept", "Authorization"]
	AllowHeaders []string

	// ExposeHeaders is a list of headers exposed to the browser
	// Default: []
	ExposeHeaders []string

	// AllowCredentials indicates if credentials are allowed
	// Default: false
	// Note: Cannot be true when AllowOrigins is ["*"]
	AllowCredentials bool

	// MaxAge indicates how long (in seconds) the results of a preflight request can be cached
	// Default: 3600 (1 hour)
	MaxAge int

	// AllowPrivateNetwork allows requests from private networks
	// Default: false
	AllowPrivateNetwork bool

	// SkipPaths is a list of paths to skip CORS handling
	SkipPaths []string

	// AutoApplyMiddleware automatically applies CORS middleware globally
	AutoApplyMiddleware bool
}

// DefaultCORSConfig returns the default CORS configuration
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		Enabled:             true,
		AllowOrigins:        []string{"*"},
		AllowMethods:        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:        []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:       []string{},
		AllowCredentials:    false,
		MaxAge:              3600,
		AllowPrivateNetwork: false,
		SkipPaths:           []string{},
		AutoApplyMiddleware: false, // Default to false for backwards compatibility
	}
}

// SecureCORSConfig returns a more restrictive CORS configuration
func SecureCORSConfig(allowedOrigins []string) CORSConfig {
	return CORSConfig{
		Enabled:             true,
		AllowOrigins:        allowedOrigins,
		AllowMethods:        []string{"GET", "POST", "PUT", "DELETE"},
		AllowHeaders:        []string{"Content-Type", "Authorization"},
		ExposeHeaders:       []string{},
		AllowCredentials:    true,
		MaxAge:              600, // 10 minutes
		AllowPrivateNetwork: false,
		SkipPaths:           []string{},
	}
}

// CORSManager manages CORS policies
type CORSManager struct {
	config CORSConfig
	logger forge.Logger
}

// NewCORSManager creates a new CORS manager
func NewCORSManager(config CORSConfig, logger forge.Logger) *CORSManager {
	// Set defaults
	if len(config.AllowMethods) == 0 {
		config.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	}
	if len(config.AllowHeaders) == 0 {
		config.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	}
	if config.MaxAge == 0 {
		config.MaxAge = 3600
	}

	return &CORSManager{
		config: config,
		logger: logger,
	}
}

// shouldSkipPath checks if the path should skip CORS handling
func (m *CORSManager) shouldSkipPath(path string) bool {
	for _, skipPath := range m.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// isOriginAllowed checks if an origin is allowed
func (m *CORSManager) isOriginAllowed(origin string) bool {
	if len(m.config.AllowOrigins) == 0 {
		return false
	}

	// Check for wildcard
	for _, allowedOrigin := range m.config.AllowOrigins {
		if allowedOrigin == "*" {
			return true
		}
		if allowedOrigin == origin {
			return true
		}
		// Support wildcard subdomains like *.example.com
		if strings.HasPrefix(allowedOrigin, "*.") {
			domain := allowedOrigin[2:]
			if strings.HasSuffix(origin, domain) {
				return true
			}
		}
	}

	return false
}

// getAllowOrigin returns the appropriate Access-Control-Allow-Origin value
func (m *CORSManager) getAllowOrigin(origin string) string {
	// If credentials are allowed, we must return the specific origin, not "*"
	if m.config.AllowCredentials && m.isOriginAllowed(origin) {
		return origin
	}

	// Check for wildcard
	for _, allowedOrigin := range m.config.AllowOrigins {
		if allowedOrigin == "*" {
			return "*"
		}
	}

	// Return the origin if it's allowed
	if m.isOriginAllowed(origin) {
		return origin
	}

	return ""
}

// CORSMiddleware returns a middleware function for CORS handling
func CORSMiddleware(manager *CORSManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !manager.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip CORS for specified paths
			if manager.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")

		// No origin header means it's not a CORS request
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Check if origin is allowed
		allowOrigin := manager.getAllowOrigin(origin)
		if allowOrigin == "" {
			// Origin not allowed, but we still process the request
			// The browser will block the response
			manager.logger.Debug("cors origin not allowed",
				forge.F("origin", origin),
				forge.F("path", r.URL.Path),
			)
			next.ServeHTTP(w, r)
			return
		}

		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)

		// Handle credentials
		if manager.config.AllowCredentials {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		// Expose headers
		if len(manager.config.ExposeHeaders) > 0 {
			w.Header().Set("Access-Control-Expose-Headers", strings.Join(manager.config.ExposeHeaders, ", "))
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			manager.handlePreflight(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
	}
}

// handlePreflight handles CORS preflight requests
func (m *CORSManager) handlePreflight(w http.ResponseWriter, r *http.Request) {
	// Access-Control-Request-Method is required for preflight
	requestMethod := r.Header.Get("Access-Control-Request-Method")
	if requestMethod == "" {
		http.Error(w, "Access-Control-Request-Method header is required", http.StatusBadRequest)
		return
	}

	// Check if method is allowed
	methodAllowed := false
	for _, method := range m.config.AllowMethods {
		if method == requestMethod {
			methodAllowed = true
			break
		}
	}

	if !methodAllowed {
		m.logger.Debug("cors method not allowed",
			forge.F("method", requestMethod),
			forge.F("path", r.URL.Path),
		)
		http.Error(w, "Method not allowed by CORS policy", http.StatusForbidden)
		return
	}

	// Set preflight headers
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(m.config.AllowMethods, ", "))
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(m.config.AllowHeaders, ", "))
	w.Header().Set("Access-Control-Max-Age", strconv.Itoa(m.config.MaxAge))

	// Handle private network access
	if m.config.AllowPrivateNetwork {
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
	}

	// Return 204 No Content for successful preflight
	w.WriteHeader(http.StatusNoContent)
}

// Vary header management for CORS
// This is important for caching
func addVaryHeader(header http.Header, value string) {
	vary := header.Get("Vary")
	if vary == "" {
		header.Set("Vary", value)
		return
	}

	// Check if the value already exists
	values := strings.Split(vary, ",")
	for _, v := range values {
		if strings.TrimSpace(v) == value {
			return
		}
	}

	header.Set("Vary", vary+", "+value)
}

