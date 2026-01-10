package security

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/xraph/forge"
)

// CORSConfig holds CORS configuration.
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
	// Use ["*"] to allow all headers (not recommended for production, and cannot be used with AllowCredentials)
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

// DefaultCORSConfig returns the default CORS configuration.
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

// SecureCORSConfig returns a more restrictive CORS configuration.
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

// CORSManager manages CORS policies.
type CORSManager struct {
	config CORSConfig
	logger forge.Logger
}

// NewCORSManager creates a new CORS manager.
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

// shouldSkipPath checks if the path should skip CORS handling.
func (m *CORSManager) shouldSkipPath(path string) bool {
	for _, skipPath := range m.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}

	return false
}

// isOriginAllowed checks if an origin is allowed.
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

// getAllowOrigin returns the appropriate Access-Control-Allow-Origin value.
func (m *CORSManager) getAllowOrigin(origin string) string {
	// If credentials are allowed, we must return the specific origin, not "*"
	if m.config.AllowCredentials && m.isOriginAllowed(origin) {
		return origin
	}

	// Check for wildcard
	if slices.Contains(m.config.AllowOrigins, "*") {
		return "*"
	}

	// Return the origin if it's allowed
	if m.isOriginAllowed(origin) {
		return origin
	}

	return ""
}

// CORSMiddleware returns a middleware function for CORS handling.
func CORSMiddleware(manager *CORSManager) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if !manager.config.Enabled {
				return next(ctx)
			}

			r := ctx.Request()
			w := ctx.Response()

			// Skip CORS for specified paths
			if manager.shouldSkipPath(r.URL.Path) {
				return next(ctx)
			}

			origin := r.Header.Get("Origin")

			// No origin header means it's not a CORS request
			if origin == "" {
				return next(ctx)
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

				return next(ctx)
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
			if r.Method == http.MethodOptions {
				manager.logger.Info("cors preflight request detected",
					forge.F("path", r.URL.Path),
					forge.F("origin", origin),
				)

				return manager.handlePreflight(ctx)
			}

			return next(ctx)
		}
	}
}

// normalizeHeader normalizes HTTP header names for case-insensitive comparison.
// HTTP headers are case-insensitive, so we normalize to a canonical form.
func normalizeHeader(header string) string {
	// Convert to lowercase for comparison, but preserve original casing for response
	// Actually, for CORS, we should return headers in the exact case they were configured
	// But for comparison, we use lowercase
	return strings.ToLower(strings.TrimSpace(header))
}

// isHeaderAllowed checks if a header is in the allowed list (case-insensitive).
func (m *CORSManager) isHeaderAllowed(requestedHeader string) bool {
	// Check for wildcard first
	if slices.Contains(m.config.AllowHeaders, "*") {
		// Wildcard is only valid if credentials are not allowed
		return !m.config.AllowCredentials
	}

	// Normalize requested header for comparison
	normalizedRequested := normalizeHeader(requestedHeader)

	// Check against allowed headers (case-insensitive)
	for _, allowedHeader := range m.config.AllowHeaders {
		if normalizeHeader(allowedHeader) == normalizedRequested {
			return true
		}
	}

	return false
}

// handlePreflight handles CORS preflight requests.
func (m *CORSManager) handlePreflight(ctx forge.Context) error {
	r := ctx.Request()
	w := ctx.Response()

	// Access-Control-Request-Method is required for preflight
	requestMethod := r.Header.Get("Access-Control-Request-Method")
	if requestMethod == "" {
		return ctx.String(http.StatusBadRequest, "Access-Control-Request-Method header is required")
	}

	// Check if method is allowed
	methodAllowed := slices.Contains(m.config.AllowMethods, requestMethod)

	if !methodAllowed {
		m.logger.Debug("cors method not allowed",
			forge.F("method", requestMethod),
			forge.F("path", r.URL.Path),
		)

		return ctx.String(http.StatusForbidden, "Method not allowed by CORS policy")
	}

	// Validate requested headers
	requestHeadersStr := r.Header.Get("Access-Control-Request-Headers")

	var allowedHeadersList []string

	if requestHeadersStr != "" {
		m.logger.Info("cors preflight requested headers",
			forge.F("requested_headers_raw", requestHeadersStr),
			forge.F("allowed_headers_config", m.config.AllowHeaders),
			forge.F("path", r.URL.Path),
		)
		// Parse requested headers (comma-separated, may have spaces)
		// Handle both "header1,header2" and "header1, header2" formats
		requestedHeaders := strings.Split(requestHeadersStr, ",")
		m.logger.Info("cors parsed requested headers",
			forge.F("parsed_headers", requestedHeaders),
			forge.F("count", len(requestedHeaders)),
		)

		allowedHeadersSet := make(map[string]string) // map[lowercase] = original

		// Check for wildcard in config
		hasWildcard := slices.Contains(m.config.AllowHeaders, "*")
		canUseWildcard := hasWildcard && !m.config.AllowCredentials

		for i, reqHeader := range requestedHeaders {
			reqHeader = strings.TrimSpace(reqHeader)
			if reqHeader == "" {
				m.logger.Debug("cors skipping empty header",
					forge.F("index", i),
					forge.F("raw", requestedHeaders[i]),
				)

				continue
			}

			normalizedReq := normalizeHeader(reqHeader)
			headerFound := false

			m.logger.Info("cors checking header",
				forge.F("requested", reqHeader),
				forge.F("normalized", normalizedReq),
				forge.F("index", i),
			)

			// If wildcard is allowed, all headers are allowed
			if canUseWildcard {
				// Find the original casing from config if it exists, otherwise use requested casing
				for _, allowedHeader := range m.config.AllowHeaders {
					if normalizeHeader(allowedHeader) == normalizedReq {
						allowedHeadersSet[normalizedReq] = allowedHeader
						headerFound = true

						break
					}
				}

				if !headerFound {
					// Use the requested header casing
					allowedHeadersSet[normalizedReq] = reqHeader
					headerFound = true
				}
			} else {
				// Check if this specific header is allowed
				for _, allowedHeader := range m.config.AllowHeaders {
					if normalizeHeader(allowedHeader) == normalizedReq {
						// Use the requested header casing (browsers expect this)
						allowedHeadersSet[normalizedReq] = reqHeader
						headerFound = true

						break
					}
				}
			}

			// Log if header was not found (for debugging)
			if !headerFound {
				m.logger.Warn("cors requested header not in allowed list",
					forge.F("requested_header", reqHeader),
					forge.F("normalized", normalizedReq),
					forge.F("allowed_headers", m.config.AllowHeaders),
					forge.F("path", r.URL.Path),
				)
			}
		}

		// Convert set to list, preserving requested casing
		for normalized, originalHeader := range allowedHeadersSet {
			allowedHeadersList = append(allowedHeadersList, originalHeader)
			m.logger.Info("cors adding header to response",
				forge.F("normalized", normalized),
				forge.F("header", originalHeader),
			)
		}

		m.logger.Info("cors preflight allowed headers list",
			forge.F("allowed_headers_response", allowedHeadersList),
			forge.F("count", len(allowedHeadersList)),
			forge.F("path", r.URL.Path),
		)
	} else {
		// No headers requested, but we should still return allowed headers
		// (or wildcard if applicable)
		if slices.Contains(m.config.AllowHeaders, "*") && !m.config.AllowCredentials {
			allowedHeadersList = []string{"*"}
		} else {
			allowedHeadersList = m.config.AllowHeaders
		}
	}

	// Set preflight headers
	// IMPORTANT: Check if headers were already set (another middleware might have set them)
	existingAllowHeaders := w.Header().Get("Access-Control-Allow-Headers")
	if existingAllowHeaders != "" {
		m.logger.Warn("cors headers already set by another middleware, overwriting",
			forge.F("existing_headers", existingAllowHeaders),
			forge.F("our_headers", allowedHeadersList),
			forge.F("path", r.URL.Path),
		)
	}

	w.Header().Set("Access-Control-Allow-Methods", strings.Join(m.config.AllowMethods, ", "))

	// Set allowed headers - use the validated list or wildcard
	var allowHeadersValue string
	if len(allowedHeadersList) == 1 && allowedHeadersList[0] == "*" {
		allowHeadersValue = "*"
	} else if len(allowedHeadersList) == 0 {
		// No headers matched - this shouldn't happen, but log it
		m.logger.Warn("cors no headers matched, returning empty list",
			forge.F("requested", requestHeadersStr),
			forge.F("allowed_config", m.config.AllowHeaders),
			forge.F("path", r.URL.Path),
		)

		allowHeadersValue = ""
	} else {
		allowHeadersValue = strings.Join(allowedHeadersList, ", ")
	}

	w.Header().Set("Access-Control-Allow-Headers", allowHeadersValue)
	w.Header().Set("Access-Control-Max-Age", strconv.Itoa(m.config.MaxAge))

	// Log what we're actually sending back
	m.logger.Info("cors preflight response headers SET",
		forge.F("allow_headers", allowHeadersValue),
		forge.F("allow_methods", strings.Join(m.config.AllowMethods, ", ")),
		forge.F("path", r.URL.Path),
	)

	// Double-check what's actually in the response
	finalHeaders := w.Header().Get("Access-Control-Allow-Headers")
	m.logger.Info("cors preflight final response headers",
		forge.F("final_allow_headers", finalHeaders),
		forge.F("path", r.URL.Path),
	)

	// Handle private network access
	if m.config.AllowPrivateNetwork {
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
	}

	// Return 204 No Content for successful preflight
	w.WriteHeader(http.StatusNoContent)

	return nil
}

// Vary header management for CORS
// This is important for caching.
func addVaryHeader(header http.Header, value string) {
	vary := header.Get("Vary")
	if vary == "" {
		header.Set("Vary", value)

		return
	}

	// Check if the value already exists
	values := strings.SplitSeq(vary, ",")
	for v := range values {
		if strings.TrimSpace(v) == value {
			return
		}
	}

	header.Set("Vary", vary+", "+value)
}
