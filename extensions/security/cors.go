package security

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/xraph/forge"
)

// CORSConfig holds CORS configuration.
type CORSConfig struct {
	// Enabled determines if CORS is enabled
	Enabled bool `yaml:"enabled"`

	// AllowOrigins is a list of allowed origins
	// Use ["*"] to allow all origins (not recommended for production)
	// Example: ["https://example.com", "https://app.example.com"]
	AllowOrigins []string `yaml:"allow_origins"`

	// AllowMethods is a list of allowed HTTP methods
	// Default: ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]
	AllowMethods []string `yaml:"allow_methods"`

	// AllowHeaders is a list of allowed request headers
	// Use ["*"] to allow all headers (not recommended for production, and cannot be used with AllowCredentials)
	// Default: ["Origin", "Content-Type", "Accept", "Authorization"]
	AllowHeaders []string `yaml:"allow_headers"`

	// ExposeHeaders is a list of headers exposed to the browser
	// Default: []
	ExposeHeaders []string `yaml:"expose_headers"`

	// AllowCredentials indicates if credentials are allowed
	// Default: false
	// Note: Cannot be true when AllowOrigins is ["*"]
	AllowCredentials bool `yaml:"allow_credentials"`

	// MaxAge indicates how long (in seconds) the results of a preflight request can be cached
	// Default: 3600 (1 hour)
	MaxAge int `yaml:"max_age"`

	// AllowPrivateNetwork allows requests from private networks
	// Default: false
	AllowPrivateNetwork bool `yaml:"allow_private_network"`

	// SkipPaths is a list of paths to skip CORS handling
	SkipPaths []string `yaml:"skip_paths"`

	// AutoApplyMiddleware automatically applies CORS middleware globally
	AutoApplyMiddleware bool `yaml:"auto_apply_middleware"`
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

	// Derived once from config so preflights do not rebuild them.
	hasWildcardOrigin bool
	hasWildcardHeader bool
	allowedHeaders    map[string]struct{} // lowercased
	allowMethods      string
	allowHeaders      string
	maxAge            string
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

	m := &CORSManager{
		config:            config,
		logger:            logger,
		hasWildcardOrigin: slices.Contains(config.AllowOrigins, "*"),
		hasWildcardHeader: slices.Contains(config.AllowHeaders, "*"),
		allowedHeaders:    make(map[string]struct{}, len(config.AllowHeaders)),
		allowMethods:      strings.Join(config.AllowMethods, ", "),
		allowHeaders:      strings.Join(config.AllowHeaders, ", "),
		maxAge:            strconv.Itoa(config.MaxAge),
	}

	for _, h := range config.AllowHeaders {
		m.allowedHeaders[normalizeHeader(h)] = struct{}{}
	}

	if m.hasWildcardOrigin && config.AllowCredentials && logger != nil {
		logger.Warn("cors: AllowOrigins [\"*\"] with AllowCredentials only answers listed origins with credentials; every other origin gets \"*\" without credentials")
	}

	return m
}

// shouldSkipPath checks if the path should skip CORS handling.
func (m *CORSManager) shouldSkipPath(path string) bool {
	return shouldSkipPath(path, m.config.SkipPaths)
}

// isOriginAllowed checks if an origin is allowed, counting a "*" entry.
func (m *CORSManager) isOriginAllowed(origin string) bool {
	if len(m.config.AllowOrigins) == 0 {
		return false
	}

	return slices.Contains(m.config.AllowOrigins, "*") || m.isOriginListed(origin)
}

// isOriginListed checks whether an origin matches an explicit entry or a
// "*.domain" pattern. A bare "*" does not count: it may answer any origin
// with "*", but it must never be turned into a reflected origin.
func (m *CORSManager) isOriginListed(origin string) bool {
	for _, allowedOrigin := range m.config.AllowOrigins {
		if allowedOrigin == "*" {
			continue
		}

		if allowedOrigin == origin {
			return true
		}
		// Support wildcard subdomains like *.example.com. Match on the origin's
		// host with a proper label boundary, NOT a bare string suffix. A bare
		// suffix (strings.HasSuffix(origin, "example.com")) would also match an
		// attacker-controlled "https://evilexample.com", which with credentials
		// is a cross-origin read vector. The host must equal the base domain or
		// be a real subdomain of it ("<label>.example.com").
		if strings.HasPrefix(allowedOrigin, "*.") {
			domain := strings.ToLower(allowedOrigin[2:])
			if host := originHost(origin); host != "" &&
				(host == domain || strings.HasSuffix(host, "."+domain)) {
				return true
			}
		}
	}

	return false
}

// originHost extracts the lowercased host (without port) from an Origin URL.
// Returns "" when the origin is empty or unparseable, so callers never match on
// a malformed origin.
func originHost(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}

	return strings.ToLower(u.Hostname())
}

// getAllowOrigin returns the appropriate Access-Control-Allow-Origin value.
//
// An origin is only ever reflected when it is listed explicitly or matches a
// "*.domain" pattern. A bare "*" entry answers with "*", which browsers never
// combine with credentials. Reflecting any origin because the list said "*"
// would let every site read credentialed responses.
func (m *CORSManager) getAllowOrigin(origin string) string {
	if m.config.AllowCredentials {
		if m.isOriginListed(origin) {
			return origin
		}

		if m.hasWildcardOrigin {
			return "*"
		}

		return ""
	}

	if m.hasWildcardOrigin {
		return "*"
	}

	if m.isOriginListed(origin) {
		return origin
	}

	return ""
}

// variesByOrigin reports whether the response depends on the Origin header.
// Only a policy that always answers "*" does not.
func (m *CORSManager) variesByOrigin() bool {
	return !m.hasWildcardOrigin || m.config.AllowCredentials
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

			// Caches must key on Origin whenever the answer depends on it,
			// including when there is no Origin or it is refused. Otherwise a
			// shared cache can serve one origin's response to another.
			if manager.variesByOrigin() {
				addVaryHeader(w.Header(), "Origin")
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

			// Credentials are only ever allowed for a specific origin.
			if manager.config.AllowCredentials && allowOrigin != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Expose headers
			if len(manager.config.ExposeHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(manager.config.ExposeHeaders, ", "))
			}

			// Handle preflight requests
			if r.Method == http.MethodOptions {
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
	if m.hasWildcardHeader {
		// Wildcard is only valid if credentials are not allowed
		return !m.config.AllowCredentials
	}

	_, ok := m.allowedHeaders[normalizeHeader(requestedHeader)]

	return ok
}

// handlePreflight handles CORS preflight requests.
//
// This runs for every preflight, so it builds no log fields on the success
// path and uses the method, header and max-age strings prepared in
// NewCORSManager.
func (m *CORSManager) handlePreflight(ctx forge.Context) error {
	r := ctx.Request()
	w := ctx.Response()

	// The answer below depends on these request headers too.
	addVaryHeader(w.Header(), "Access-Control-Request-Method")
	addVaryHeader(w.Header(), "Access-Control-Request-Headers")

	// Access-Control-Request-Method is required for preflight
	requestMethod := r.Header.Get("Access-Control-Request-Method")
	if requestMethod == "" {
		return ctx.String(http.StatusBadRequest, "Access-Control-Request-Method header is required")
	}

	if !slices.Contains(m.config.AllowMethods, requestMethod) {
		m.logger.Debug("cors method not allowed",
			forge.F("method", requestMethod),
			forge.F("path", r.URL.Path),
		)

		return ctx.String(http.StatusForbidden, "Method not allowed by CORS policy")
	}

	canUseWildcard := m.hasWildcardHeader && !m.config.AllowCredentials

	var allowHeadersValue string

	if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
		// Answer with the requested headers that are allowed, in the casing
		// the browser sent and without duplicates.
		var (
			b    strings.Builder
			seen []string
		)

		for reqHeader := range strings.SplitSeq(requested, ",") {
			reqHeader = strings.TrimSpace(reqHeader)
			if reqHeader == "" {
				continue
			}

			normalized := strings.ToLower(reqHeader)
			if slices.Contains(seen, normalized) {
				continue
			}

			if !canUseWildcard {
				if _, ok := m.allowedHeaders[normalized]; !ok {
					continue
				}
			}

			seen = append(seen, normalized)

			if b.Len() > 0 {
				b.WriteString(", ")
			}

			b.WriteString(reqHeader)
		}

		allowHeadersValue = b.String()
	} else if canUseWildcard {
		allowHeadersValue = "*"
	} else {
		allowHeadersValue = m.allowHeaders
	}

	w.Header().Set("Access-Control-Allow-Methods", m.allowMethods)
	w.Header().Set("Access-Control-Allow-Headers", allowHeadersValue)
	w.Header().Set("Access-Control-Max-Age", m.maxAge)

	// Handle private network access
	if m.config.AllowPrivateNetwork && r.Header.Get("Access-Control-Request-Private-Network") == "true" {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
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
