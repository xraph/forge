package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/router"
)

// CORSMiddleware handles Cross-Origin Resource Sharing
type CORSMiddleware struct {
	*BaseMiddleware
	config router.CORSConfig
}

// NewCORSMiddleware creates a new CORS middleware
func NewCORSMiddleware(config router.CORSConfig) Middleware {
	return &CORSMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"cors",
			PriorityCORS,
			"Cross-Origin Resource Sharing (CORS) middleware",
		),
		config: config,
	}
}

// DefaultCORSConfig returns default CORS configuration
func DefaultCORSConfig() router.CORSConfig {
	return router.CORSConfig{
		AllowedOrigins:     []string{"*"},
		AllowedMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowedHeaders:     []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Requested-With"},
		ExposedHeaders:     []string{},
		AllowCredentials:   false,
		MaxAge:             86400, // 24 hours
		OptionsPassthrough: false,
	}
}

// RestrictiveCORSConfig returns a restrictive CORS configuration
func RestrictiveCORSConfig() router.CORSConfig {
	return router.CORSConfig{
		AllowedOrigins:     []string{},
		AllowedMethods:     []string{"GET", "POST"},
		AllowedHeaders:     []string{"Accept", "Content-Type"},
		ExposedHeaders:     []string{},
		AllowCredentials:   false,
		MaxAge:             3600, // 1 hour
		OptionsPassthrough: false,
	}
}

// PermissiveCORSConfig returns a permissive CORS configuration (for development)
func PermissiveCORSConfig() router.CORSConfig {
	return router.CORSConfig{
		AllowedOrigins:     []string{"*"},
		AllowedMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowedHeaders:     []string{"*"},
		ExposedHeaders:     []string{"*"},
		AllowCredentials:   true,
		MaxAge:             86400, // 24 hours
		OptionsPassthrough: false,
	}
}

// Handle implements the Middleware interface
func (cm *CORSMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		if !cm.isOriginAllowed(origin) {
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Set CORS headers
		cm.setCORSHeaders(w, r, origin)

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			cm.handlePreflight(w, r)
			return
		}

		// Continue with actual request
		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed checks if the origin is allowed
func (cm *CORSMiddleware) isOriginAllowed(origin string) bool {
	// If no origin is provided, allow (same-origin requests)
	if origin == "" {
		return true
	}

	// Check if wildcard is allowed
	for _, allowedOrigin := range cm.config.AllowedOrigins {
		if allowedOrigin == "*" {
			return true
		}
		if cm.matchOrigin(origin, allowedOrigin) {
			return true
		}
	}

	return false
}

// matchOrigin checks if origin matches the allowed origin pattern
func (cm *CORSMiddleware) matchOrigin(origin, allowedOrigin string) bool {
	// Exact match
	if origin == allowedOrigin {
		return true
	}

	// Wildcard subdomain matching (e.g., *.example.com)
	if strings.HasPrefix(allowedOrigin, "*.") {
		domain := allowedOrigin[2:]
		return strings.HasSuffix(origin, domain) || origin == domain
	}

	return false
}

// setCORSHeaders sets the appropriate CORS headers
func (cm *CORSMiddleware) setCORSHeaders(w http.ResponseWriter, r *http.Request, origin string) {
	// Set Access-Control-Allow-Origin
	if origin != "" {
		if cm.hasWildcardOrigin() && !cm.config.AllowCredentials {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
	}

	// Set Access-Control-Allow-Credentials
	if cm.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	// Set Access-Control-Expose-Headers
	if len(cm.config.ExposedHeaders) > 0 {
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(cm.config.ExposedHeaders, ", "))
	}

	// Set Vary header for caching
	vary := w.Header().Get("Vary")
	if vary == "" {
		w.Header().Set("Vary", "Origin")
	} else if !strings.Contains(vary, "Origin") {
		w.Header().Set("Vary", vary+", Origin")
	}
}

// handlePreflight handles preflight OPTIONS requests
func (cm *CORSMiddleware) handlePreflight(w http.ResponseWriter, r *http.Request) {
	// Check if method is allowed
	requestMethod := r.Header.Get("Access-Control-Request-Method")
	if requestMethod != "" && !cm.isMethodAllowed(requestMethod) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check if headers are allowed
	requestHeaders := r.Header.Get("Access-Control-Request-Headers")
	if requestHeaders != "" && !cm.areHeadersAllowed(requestHeaders) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Set preflight response headers
	cm.setPreflightHeaders(w, r)

	// Set status code
	w.WriteHeader(http.StatusNoContent)
}

// setPreflightHeaders sets the preflight response headers
func (cm *CORSMiddleware) setPreflightHeaders(w http.ResponseWriter, r *http.Request) {
	// Set Access-Control-Allow-Methods
	if len(cm.config.AllowedMethods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(cm.config.AllowedMethods, ", "))
	}

	// Set Access-Control-Allow-Headers
	if len(cm.config.AllowedHeaders) > 0 {
		if cm.hasWildcardHeaders() {
			// If wildcard is allowed, echo the requested headers
			requestHeaders := r.Header.Get("Access-Control-Request-Headers")
			if requestHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
			}
		} else {
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(cm.config.AllowedHeaders, ", "))
		}
	}

	// Set Access-Control-Max-Age
	if cm.config.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", strconv.Itoa(cm.config.MaxAge))
	}
}

// isMethodAllowed checks if the HTTP method is allowed
func (cm *CORSMiddleware) isMethodAllowed(method string) bool {
	for _, allowedMethod := range cm.config.AllowedMethods {
		if allowedMethod == "*" || strings.EqualFold(allowedMethod, method) {
			return true
		}
	}
	return false
}

// areHeadersAllowed checks if the requested headers are allowed
func (cm *CORSMiddleware) areHeadersAllowed(requestHeaders string) bool {
	if cm.hasWildcardHeaders() {
		return true
	}

	headers := strings.Split(requestHeaders, ",")
	for _, header := range headers {
		header = strings.TrimSpace(header)
		if !cm.isHeaderAllowed(header) {
			return false
		}
	}
	return true
}

// isHeaderAllowed checks if a specific header is allowed
func (cm *CORSMiddleware) isHeaderAllowed(header string) bool {
	// Simple headers that are always allowed
	simpleHeaders := []string{
		"Accept",
		"Accept-Language",
		"Content-Language",
		"Content-Type",
		"DPR",
		"Downlink",
		"Save-Data",
		"Viewport-Width",
		"Width",
	}

	for _, simpleHeader := range simpleHeaders {
		if strings.EqualFold(header, simpleHeader) {
			return true
		}
	}

	// Check configured allowed headers
	for _, allowedHeader := range cm.config.AllowedHeaders {
		if allowedHeader == "*" || strings.EqualFold(allowedHeader, header) {
			return true
		}
	}

	return false
}

// hasWildcardOrigin checks if wildcard origin is allowed
func (cm *CORSMiddleware) hasWildcardOrigin() bool {
	for _, origin := range cm.config.AllowedOrigins {
		if origin == "*" {
			return true
		}
	}
	return false
}

// hasWildcardHeaders checks if wildcard headers are allowed
func (cm *CORSMiddleware) hasWildcardHeaders() bool {
	for _, header := range cm.config.AllowedHeaders {
		if header == "*" {
			return true
		}
	}
	return false
}

// Configure implements the Middleware interface
func (cm *CORSMiddleware) Configure(config map[string]interface{}) error {
	if allowedOrigins, ok := config["allowed_origins"].([]string); ok {
		cm.config.AllowedOrigins = allowedOrigins
	}
	if allowedMethods, ok := config["allowed_methods"].([]string); ok {
		cm.config.AllowedMethods = allowedMethods
	}
	if allowedHeaders, ok := config["allowed_headers"].([]string); ok {
		cm.config.AllowedHeaders = allowedHeaders
	}
	if exposedHeaders, ok := config["exposed_headers"].([]string); ok {
		cm.config.ExposedHeaders = exposedHeaders
	}
	if allowCredentials, ok := config["allow_credentials"].(bool); ok {
		cm.config.AllowCredentials = allowCredentials
	}
	if maxAge, ok := config["max_age"].(int); ok {
		cm.config.MaxAge = maxAge
	}
	if optionsPassthrough, ok := config["options_passthrough"].(bool); ok {
		cm.config.OptionsPassthrough = optionsPassthrough
	}

	return nil
}

// Health implements the Middleware interface
func (cm *CORSMiddleware) Health(ctx context.Context) error {
	// Basic validation
	if len(cm.config.AllowedOrigins) == 0 {
		return fmt.Errorf("no allowed origins configured")
	}
	if len(cm.config.AllowedMethods) == 0 {
		return fmt.Errorf("no allowed methods configured")
	}
	return nil
}

// Utility functions

// CORSMiddlewareFunc creates a simple CORS middleware function
func CORSMiddlewareFunc(config router.CORSConfig) func(http.Handler) http.Handler {
	middleware := NewCORSMiddleware(config)
	return middleware.Handle
}

// WithDefaultCORS creates CORS middleware with default configuration
func WithDefaultCORS() func(http.Handler) http.Handler {
	return CORSMiddlewareFunc(DefaultCORSConfig())
}

// WithRestrictiveCORS creates CORS middleware with restrictive configuration
func WithRestrictiveCORS() func(http.Handler) http.Handler {
	return CORSMiddlewareFunc(RestrictiveCORSConfig())
}

// WithPermissiveCORS creates CORS middleware with permissive configuration
func WithPermissiveCORS() func(http.Handler) http.Handler {
	return CORSMiddlewareFunc(PermissiveCORSConfig())
}

// WithAPIsCORS creates CORS middleware optimized for APIs
func WithAPIsCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	config := router.CORSConfig{
		AllowedOrigins:     allowedOrigins,
		AllowedMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowedHeaders:     []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-Requested-With"},
		ExposedHeaders:     []string{"X-Total-Count", "X-Page-Count", "Link"},
		AllowCredentials:   true,
		MaxAge:             3600,
		OptionsPassthrough: false,
	}
	return CORSMiddlewareFunc(config)
}

// WithSinglePageAppCORS creates CORS middleware optimized for SPAs
func WithSinglePageAppCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	config := router.CORSConfig{
		AllowedOrigins:     allowedOrigins,
		AllowedMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowedHeaders:     []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Requested-With"},
		ExposedHeaders:     []string{"X-CSRF-Token"},
		AllowCredentials:   true,
		MaxAge:             86400,
		OptionsPassthrough: false,
	}
	return CORSMiddlewareFunc(config)
}

// CORS validation helpers

// ValidateOrigin validates if an origin is properly formatted
func ValidateOrigin(origin string) error {
	if origin == "" {
		return fmt.Errorf("origin cannot be empty")
	}
	if origin == "*" {
		return nil
	}

	// Basic URL validation
	if !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
		return fmt.Errorf("origin must start with http:// or https://")
	}

	return nil
}

// ValidateMethod validates if a method is valid
func ValidateMethod(method string) error {
	if method == "" {
		return fmt.Errorf("method cannot be empty")
	}
	if method == "*" {
		return nil
	}

	validMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	for _, validMethod := range validMethods {
		if strings.EqualFold(method, validMethod) {
			return nil
		}
	}

	return fmt.Errorf("invalid HTTP method: %s", method)
}

// ValidateHeader validates if a header name is valid
func ValidateHeader(header string) error {
	if header == "" {
		return fmt.Errorf("header cannot be empty")
	}
	if header == "*" {
		return nil
	}

	// Basic header name validation
	for _, char := range header {
		if char < 33 || char > 126 || char == ':' {
			return fmt.Errorf("invalid header name: %s", header)
		}
	}

	return nil
}

// ValidateCORSConfig validates a CORS configuration
func ValidateCORSConfig(config router.CORSConfig) error {
	// Validate origins
	for _, origin := range config.AllowedOrigins {
		if err := ValidateOrigin(origin); err != nil {
			return fmt.Errorf("invalid origin %s: %w", origin, err)
		}
	}

	// Validate methods
	for _, method := range config.AllowedMethods {
		if err := ValidateMethod(method); err != nil {
			return fmt.Errorf("invalid method %s: %w", method, err)
		}
	}

	// Validate headers
	for _, header := range config.AllowedHeaders {
		if err := ValidateHeader(header); err != nil {
			return fmt.Errorf("invalid header %s: %w", header, err)
		}
	}

	for _, header := range config.ExposedHeaders {
		if err := ValidateHeader(header); err != nil {
			return fmt.Errorf("invalid exposed header %s: %w", header, err)
		}
	}

	// Validate MaxAge
	if config.MaxAge < 0 {
		return fmt.Errorf("max_age cannot be negative")
	}

	// Validate credentials with wildcard origin
	if config.AllowCredentials {
		for _, origin := range config.AllowedOrigins {
			if origin == "*" {
				return fmt.Errorf("cannot use wildcard origin with credentials")
			}
		}
	}

	return nil
}

// CORS debugging helpers

// LogCORSRequest logs CORS request information for debugging
func LogCORSRequest(logger logger.Logger, r *http.Request, allowed bool) {
	origin := r.Header.Get("Origin")
	method := r.Header.Get("Access-Control-Request-Method")
	headers := r.Header.Get("Access-Control-Request-Headers")

	fields := []logger.Field{
		logger.String("origin", origin),
		logger.String("method", r.Method),
		logger.Bool("allowed", allowed),
	}

	if method != "" {
		fields = append(fields, logger.String("request_method", method))
	}
	if headers != "" {
		fields = append(fields, logger.String("request_headers", headers))
	}

	if allowed {
		logger.Debug("CORS request allowed", fields...)
	} else {
		logger.Warn("CORS request denied", fields...)
	}
}

// GetCORSHeaders returns the CORS headers that would be set for a request
func GetCORSHeaders(config router.CORSConfig, r *http.Request) map[string]string {
	headers := make(map[string]string)
	origin := r.Header.Get("Origin")

	// Access-Control-Allow-Origin
	if origin != "" {
		headers["Access-Control-Allow-Origin"] = origin
	}

	// Access-Control-Allow-Credentials
	if config.AllowCredentials {
		headers["Access-Control-Allow-Credentials"] = "true"
	}

	// Access-Control-Expose-Headers
	if len(config.ExposedHeaders) > 0 {
		headers["Access-Control-Expose-Headers"] = strings.Join(config.ExposedHeaders, ", ")
	}

	// Preflight headers
	if r.Method == "OPTIONS" {
		if len(config.AllowedMethods) > 0 {
			headers["Access-Control-Allow-Methods"] = strings.Join(config.AllowedMethods, ", ")
		}
		if len(config.AllowedHeaders) > 0 {
			headers["Access-Control-Allow-Headers"] = strings.Join(config.AllowedHeaders, ", ")
		}
		if config.MaxAge > 0 {
			headers["Access-Control-Max-Age"] = strconv.Itoa(config.MaxAge)
		}
	}

	return headers
}
