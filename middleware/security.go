package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/router"
)

// SecurityMiddleware provides security headers and CSRF protection
type SecurityMiddleware struct {
	*BaseMiddleware
	config SecurityConfig
}

// SecurityConfig represents security middleware configuration
type SecurityConfig struct {
	// Content Security Policy
	CSP             string `json:"csp"`
	CSPReportOnly   bool   `json:"csp_report_only"`
	CSPReportURI    string `json:"csp_report_uri"`
	CSPNonce        bool   `json:"csp_nonce"`
	CSPUnsafeInline bool   `json:"csp_unsafe_inline"`
	CSPUnsafeEval   bool   `json:"csp_unsafe_eval"`

	// HTTP Strict Transport Security
	HSTS                  bool `json:"hsts"`
	HSTSMaxAge            int  `json:"hsts_max_age"`
	HSTSIncludeSubdomains bool `json:"hsts_include_subdomains"`
	HSTSPreload           bool `json:"hsts_preload"`

	// X-Frame-Options
	FrameOptions string `json:"frame_options"`

	// X-Content-Type-Options
	ContentTypeOptions string `json:"content_type_options"`

	// X-XSS-Protection
	XSSProtection string `json:"xss_protection"`

	// Referrer Policy
	ReferrerPolicy string `json:"referrer_policy"`

	// Feature Policy / Permissions Policy
	FeaturePolicy     string `json:"feature_policy"`
	PermissionsPolicy string `json:"permissions_policy"`

	// CSRF Protection
	CSRF router.CSRFConfig `json:"csrf"`

	// Custom headers
	CustomHeaders map[string]string `json:"custom_headers"`

	// Security scanning
	HidePoweredBy    bool `json:"hide_powered_by"`
	HideServerHeader bool `json:"hide_server_header"`

	// Click jacking protection
	ClickjackingProtection bool `json:"clickjacking_protection"`

	// MIME type sniffing protection
	NoSniff bool `json:"no_sniff"`

	// Cross-domain policy
	CrossDomainPolicy string `json:"cross_domain_policy"`

	// Expect-CT
	ExpectCT          bool   `json:"expect_ct"`
	ExpectCTMaxAge    int    `json:"expect_ct_max_age"`
	ExpectCTEnforce   bool   `json:"expect_ct_enforce"`
	ExpectCTReportURI string `json:"expect_ct_report_uri"`
}

// NewSecurityMiddleware creates a new security middleware
func NewSecurityMiddleware(config SecurityConfig) Middleware {
	return &SecurityMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"security",
			PrioritySecurity,
			"Security headers and CSRF protection middleware",
		),
		config: config,
	}
}

// DefaultSecurityConfig returns default security configuration
func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		CSP:                   "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'",
		CSPReportOnly:         false,
		CSPNonce:              false,
		CSPUnsafeInline:       false,
		CSPUnsafeEval:         false,
		HSTS:                  true,
		HSTSMaxAge:            31536000, // 1 year
		HSTSIncludeSubdomains: true,
		HSTSPreload:           false,
		FrameOptions:          "DENY",
		ContentTypeOptions:    "nosniff",
		XSSProtection:         "1; mode=block",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		FeaturePolicy:         "",
		PermissionsPolicy:     "",
		CSRF: router.CSRFConfig{
			Enabled:        false,
			TokenLength:    32,
			TokenLookup:    "header:X-CSRF-Token",
			ContextKey:     "csrf_token",
			CookieName:     "csrf_token",
			CookiePath:     "/",
			CookieMaxAge:   3600,
			CookieSecure:   true,
			CookieHTTPOnly: true,
			CookieSameSite: http.SameSiteStrictMode,
		},
		CustomHeaders:          make(map[string]string),
		HidePoweredBy:          true,
		HideServerHeader:       true,
		ClickjackingProtection: true,
		NoSniff:                true,
		CrossDomainPolicy:      "none",
		ExpectCT:               false,
		ExpectCTMaxAge:         86400,
		ExpectCTEnforce:        false,
	}
}

// StrictSecurityConfig returns strict security configuration
func StrictSecurityConfig() SecurityConfig {
	config := DefaultSecurityConfig()
	config.CSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'"
	config.CSPNonce = true
	config.FrameOptions = "DENY"
	config.XSSProtection = "1; mode=block"
	config.ReferrerPolicy = "no-referrer"
	config.HSTSPreload = true
	config.ExpectCT = true
	config.ExpectCTEnforce = true
	config.CSRF.Enabled = true
	return config
}

// RelaxedSecurityConfig returns relaxed security configuration for development
func RelaxedSecurityConfig() SecurityConfig {
	config := DefaultSecurityConfig()
	config.CSP = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; connect-src 'self' ws: wss:"
	config.CSPUnsafeInline = true
	config.CSPUnsafeEval = true
	config.HSTS = false
	config.FrameOptions = "SAMEORIGIN"
	config.CSRF.Enabled = false
	config.CSRF.CookieSecure = false
	return config
}

// Handle implements the Middleware interface
func (sm *SecurityMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set security headers
		sm.setSecurityHeaders(w, r)

		// Handle CSRF protection
		if sm.config.CSRF.Enabled {
			if !sm.handleCSRF(w, r) {
				sm.handleCSRFError(w, r)
				return
			}
		}

		// Continue with next handler
		next.ServeHTTP(w, r)
	})
}

// setSecurityHeaders sets various security headers
func (sm *SecurityMiddleware) setSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	// Content Security Policy
	if sm.config.CSP != "" {
		csp := sm.config.CSP

		// Add nonce if enabled
		if sm.config.CSPNonce {
			nonce := sm.generateNonce()
			csp = strings.ReplaceAll(csp, "'nonce-'", fmt.Sprintf("'nonce-%s'", nonce))
			// Store nonce in context for templates
			ctx := context.WithValue(r.Context(), "csp_nonce", nonce)
			*r = *r.WithContext(ctx)
		}

		if sm.config.CSPReportOnly {
			w.Header().Set("Content-Security-Policy-Report-Only", csp)
		} else {
			w.Header().Set("Content-Security-Policy", csp)
		}

		// CSP Report URI
		if sm.config.CSPReportURI != "" {
			if !strings.Contains(csp, "report-uri") {
				csp += fmt.Sprintf("; report-uri %s", sm.config.CSPReportURI)
				if sm.config.CSPReportOnly {
					w.Header().Set("Content-Security-Policy-Report-Only", csp)
				} else {
					w.Header().Set("Content-Security-Policy", csp)
				}
			}
		}
	}

	// HTTP Strict Transport Security
	if sm.config.HSTS && r.TLS != nil {
		hsts := fmt.Sprintf("max-age=%d", sm.config.HSTSMaxAge)
		if sm.config.HSTSIncludeSubdomains {
			hsts += "; includeSubDomains"
		}
		if sm.config.HSTSPreload {
			hsts += "; preload"
		}
		w.Header().Set("Strict-Transport-Security", hsts)
	}

	// X-Frame-Options
	if sm.config.FrameOptions != "" {
		w.Header().Set("X-Frame-Options", sm.config.FrameOptions)
	}

	// X-Content-Type-Options
	if sm.config.ContentTypeOptions != "" {
		w.Header().Set("X-Content-Type-Options", sm.config.ContentTypeOptions)
	}

	// X-XSS-Protection
	if sm.config.XSSProtection != "" {
		w.Header().Set("X-XSS-Protection", sm.config.XSSProtection)
	}

	// Referrer-Policy
	if sm.config.ReferrerPolicy != "" {
		w.Header().Set("Referrer-Policy", sm.config.ReferrerPolicy)
	}

	// Feature-Policy (deprecated, use Permissions-Policy)
	if sm.config.FeaturePolicy != "" {
		w.Header().Set("Feature-Policy", sm.config.FeaturePolicy)
	}

	// Permissions-Policy
	if sm.config.PermissionsPolicy != "" {
		w.Header().Set("Permissions-Policy", sm.config.PermissionsPolicy)
	}

	// Cross-Domain-Policy
	if sm.config.CrossDomainPolicy != "" {
		w.Header().Set("X-Permitted-Cross-Domain-Policies", sm.config.CrossDomainPolicy)
	}

	// Expect-CT
	if sm.config.ExpectCT {
		expectCT := fmt.Sprintf("max-age=%d", sm.config.ExpectCTMaxAge)
		if sm.config.ExpectCTEnforce {
			expectCT += ", enforce"
		}
		if sm.config.ExpectCTReportURI != "" {
			expectCT += fmt.Sprintf(", report-uri=\"%s\"", sm.config.ExpectCTReportURI)
		}
		w.Header().Set("Expect-CT", expectCT)
	}

	// Hide server information
	if sm.config.HidePoweredBy {
		w.Header().Del("X-Powered-By")
	}

	if sm.config.HideServerHeader {
		w.Header().Set("Server", "")
	}

	// Custom headers
	for key, value := range sm.config.CustomHeaders {
		w.Header().Set(key, value)
	}
}

// handleCSRF handles CSRF protection
func (sm *SecurityMiddleware) handleCSRF(w http.ResponseWriter, r *http.Request) bool {
	// Skip CSRF for safe methods
	if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
		// Generate and set CSRF token for safe methods
		token := sm.generateCSRFToken()
		sm.setCSRFToken(w, r, token)
		return true
	}

	// Get token from request
	token := sm.getCSRFTokenFromRequest(r)
	if token == "" {
		return false
	}

	// Get expected token from cookie/session
	expectedToken := sm.getCSRFTokenFromCookie(r)
	if expectedToken == "" {
		return false
	}

	// Compare tokens
	if !sm.validateCSRFToken(token, expectedToken) {
		return false
	}

	return true
}

// generateCSRFToken generates a new CSRF token
func (sm *SecurityMiddleware) generateCSRFToken() string {
	bytes := make([]byte, sm.config.CSRF.TokenLength)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based token
		return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return base64.URLEncoding.EncodeToString(bytes)
}

// setCSRFToken sets CSRF token in cookie and context
func (sm *SecurityMiddleware) setCSRFToken(w http.ResponseWriter, r *http.Request, token string) {
	// Set in cookie
	cookie := &http.Cookie{
		Name:     sm.config.CSRF.CookieName,
		Value:    token,
		Path:     sm.config.CSRF.CookiePath,
		Domain:   sm.config.CSRF.CookieDomain,
		MaxAge:   sm.config.CSRF.CookieMaxAge,
		Secure:   sm.config.CSRF.CookieSecure,
		HttpOnly: sm.config.CSRF.CookieHTTPOnly,
		SameSite: sm.config.CSRF.CookieSameSite,
	}
	http.SetCookie(w, cookie)

	// Set in context
	ctx := context.WithValue(r.Context(), sm.config.CSRF.ContextKey, token)
	*r = *r.WithContext(ctx)
}

// getCSRFTokenFromRequest extracts CSRF token from request
func (sm *SecurityMiddleware) getCSRFTokenFromRequest(r *http.Request) string {
	// Parse token lookup format
	parts := strings.Split(sm.config.CSRF.TokenLookup, ":")
	if len(parts) != 2 {
		return ""
	}

	source := parts[0]
	key := parts[1]

	switch source {
	case "header":
		return r.Header.Get(key)
	case "form":
		return r.FormValue(key)
	case "query":
		return r.URL.Query().Get(key)
	case "cookie":
		cookie, err := r.Cookie(key)
		if err != nil {
			return ""
		}
		return cookie.Value
	default:
		return ""
	}
}

// getCSRFTokenFromCookie gets CSRF token from cookie
func (sm *SecurityMiddleware) getCSRFTokenFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(sm.config.CSRF.CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// validateCSRFToken validates CSRF token using constant-time comparison
func (sm *SecurityMiddleware) validateCSRFToken(token, expectedToken string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) == 1
}

// handleCSRFError handles CSRF validation errors
func (sm *SecurityMiddleware) handleCSRFError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)

	response := map[string]interface{}{
		"error":   "CSRF token validation failed",
		"message": "Invalid or missing CSRF token",
		"code":    "CSRF_TOKEN_INVALID",
	}

	// Don't include token in response for security
	fmt.Fprintf(w, `{"error":"%s","message":"%s","code":"%s"}`,
		response["error"], response["message"], response["code"])
}

// generateNonce generates a nonce for CSP
func (sm *SecurityMiddleware) generateNonce() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.URLEncoding.EncodeToString(bytes)
}

// Configure implements the Middleware interface
func (sm *SecurityMiddleware) Configure(config map[string]interface{}) error {
	if csp, ok := config["csp"].(string); ok {
		sm.config.CSP = csp
	}
	if hsts, ok := config["hsts"].(bool); ok {
		sm.config.HSTS = hsts
	}
	if frameOptions, ok := config["frame_options"].(string); ok {
		sm.config.FrameOptions = frameOptions
	}
	if contentTypeOptions, ok := config["content_type_options"].(string); ok {
		sm.config.ContentTypeOptions = contentTypeOptions
	}
	if xssProtection, ok := config["xss_protection"].(string); ok {
		sm.config.XSSProtection = xssProtection
	}
	if referrerPolicy, ok := config["referrer_policy"].(string); ok {
		sm.config.ReferrerPolicy = referrerPolicy
	}
	if hidePoweredBy, ok := config["hide_powered_by"].(bool); ok {
		sm.config.HidePoweredBy = hidePoweredBy
	}
	if hideServerHeader, ok := config["hide_server_header"].(bool); ok {
		sm.config.HideServerHeader = hideServerHeader
	}
	if csrfEnabled, ok := config["csrf_enabled"].(bool); ok {
		sm.config.CSRF.Enabled = csrfEnabled
	}
	if customHeaders, ok := config["custom_headers"].(map[string]string); ok {
		sm.config.CustomHeaders = customHeaders
	}

	return nil
}

// Health implements the Middleware interface
func (sm *SecurityMiddleware) Health(ctx context.Context) error {
	// Basic configuration validation
	if sm.config.CSRF.Enabled && sm.config.CSRF.TokenLength < 16 {
		return fmt.Errorf("CSRF token length too short")
	}

	return nil
}

// Utility functions

// SecurityMiddlewareFunc creates a simple security middleware function
func SecurityMiddlewareFunc(config SecurityConfig) func(http.Handler) http.Handler {
	middleware := NewSecurityMiddleware(config)
	return middleware.Handle
}

// WithDefaultSecurity creates security middleware with default configuration
func WithDefaultSecurity() func(http.Handler) http.Handler {
	return SecurityMiddlewareFunc(DefaultSecurityConfig())
}

// WithStrictSecurity creates security middleware with strict configuration
func WithStrictSecurity() func(http.Handler) http.Handler {
	return SecurityMiddlewareFunc(StrictSecurityConfig())
}

// WithRelaxedSecurity creates security middleware with relaxed configuration
func WithRelaxedSecurity() func(http.Handler) http.Handler {
	return SecurityMiddlewareFunc(RelaxedSecurityConfig())
}

// WithCSRFProtection creates security middleware with CSRF protection enabled
func WithCSRFProtection(config router.CSRFConfig) func(http.Handler) http.Handler {
	securityConfig := DefaultSecurityConfig()
	securityConfig.CSRF = config
	securityConfig.CSRF.Enabled = true
	return SecurityMiddlewareFunc(securityConfig)
}

// GetCSRFToken extracts CSRF token from request context
func GetCSRFToken(r *http.Request) string {
	if token, ok := r.Context().Value("csrf_token").(string); ok {
		return token
	}
	return ""
}

// GetCSPNonce extracts CSP nonce from request context
func GetCSPNonce(r *http.Request) string {
	if nonce, ok := r.Context().Value("csp_nonce").(string); ok {
		return nonce
	}
	return ""
}

// CSP directive builders

// CSPBuilder helps build Content Security Policy directives
type CSPBuilder struct {
	directives map[string][]string
}

// NewCSPBuilder creates a new CSP builder
func NewCSPBuilder() *CSPBuilder {
	return &CSPBuilder{
		directives: make(map[string][]string),
	}
}

// DefaultSrc sets the default-src directive
func (b *CSPBuilder) DefaultSrc(sources ...string) *CSPBuilder {
	b.directives["default-src"] = sources
	return b
}

// ScriptSrc sets the script-src directive
func (b *CSPBuilder) ScriptSrc(sources ...string) *CSPBuilder {
	b.directives["script-src"] = sources
	return b
}

// StyleSrc sets the style-src directive
func (b *CSPBuilder) StyleSrc(sources ...string) *CSPBuilder {
	b.directives["style-src"] = sources
	return b
}

// ImgSrc sets the img-src directive
func (b *CSPBuilder) ImgSrc(sources ...string) *CSPBuilder {
	b.directives["img-src"] = sources
	return b
}

// FontSrc sets the font-src directive
func (b *CSPBuilder) FontSrc(sources ...string) *CSPBuilder {
	b.directives["font-src"] = sources
	return b
}

// ConnectSrc sets the connect-src directive
func (b *CSPBuilder) ConnectSrc(sources ...string) *CSPBuilder {
	b.directives["connect-src"] = sources
	return b
}

// FrameSrc sets the frame-src directive
func (b *CSPBuilder) FrameSrc(sources ...string) *CSPBuilder {
	b.directives["frame-src"] = sources
	return b
}

// ObjectSrc sets the object-src directive
func (b *CSPBuilder) ObjectSrc(sources ...string) *CSPBuilder {
	b.directives["object-src"] = sources
	return b
}

// MediaSrc sets the media-src directive
func (b *CSPBuilder) MediaSrc(sources ...string) *CSPBuilder {
	b.directives["media-src"] = sources
	return b
}

// ReportURI sets the report-uri directive
func (b *CSPBuilder) ReportURI(uri string) *CSPBuilder {
	b.directives["report-uri"] = []string{uri}
	return b
}

// Build builds the CSP header value
func (b *CSPBuilder) Build() string {
	var parts []string

	// Ensure default-src is first
	if sources, exists := b.directives["default-src"]; exists {
		parts = append(parts, fmt.Sprintf("default-src %s", strings.Join(sources, " ")))
		delete(b.directives, "default-src")
	}

	// Add other directives
	for directive, sources := range b.directives {
		parts = append(parts, fmt.Sprintf("%s %s", directive, strings.Join(sources, " ")))
	}

	return strings.Join(parts, "; ")
}

// Common CSP source values
const (
	CSPSelf          = "'self'"
	CSPUnsafeInline  = "'unsafe-inline'"
	CSPUnsafeEval    = "'unsafe-eval'"
	CSPNone          = "'none'"
	CSPStrictDynamic = "'strict-dynamic'"
	CSPData          = "data:"
	CSPBlob          = "blob:"
	CSPFilesystem    = "filesystem:"
	CSPMediastream   = "mediastream:"
)

// Common CSP configurations

// CSPForSPA returns CSP configuration for Single Page Applications
func CSPForSPA() string {
	return NewCSPBuilder().
		DefaultSrc(CSPSelf).
		ScriptSrc(CSPSelf, CSPUnsafeInline).
		StyleSrc(CSPSelf, CSPUnsafeInline).
		ImgSrc(CSPSelf, CSPData).
		FontSrc(CSPSelf).
		ConnectSrc(CSPSelf).
		Build()
}

// CSPForAPI returns CSP configuration for API endpoints
func CSPForAPI() string {
	return NewCSPBuilder().
		DefaultSrc(CSPNone).
		Build()
}

// CSPForStaticSite returns CSP configuration for static sites
func CSPForStaticSite() string {
	return NewCSPBuilder().
		DefaultSrc(CSPSelf).
		ScriptSrc(CSPSelf).
		StyleSrc(CSPSelf).
		ImgSrc(CSPSelf, CSPData).
		FontSrc(CSPSelf).
		Build()
}

// Feature/Permissions Policy builders

// PermissionsPolicyBuilder helps build Permissions Policy directives
type PermissionsPolicyBuilder struct {
	directives map[string][]string
}

// NewPermissionsPolicyBuilder creates a new permissions policy builder
func NewPermissionsPolicyBuilder() *PermissionsPolicyBuilder {
	return &PermissionsPolicyBuilder{
		directives: make(map[string][]string),
	}
}

// Camera sets the camera directive
func (b *PermissionsPolicyBuilder) Camera(origins ...string) *PermissionsPolicyBuilder {
	b.directives["camera"] = origins
	return b
}

// Microphone sets the microphone directive
func (b *PermissionsPolicyBuilder) Microphone(origins ...string) *PermissionsPolicyBuilder {
	b.directives["microphone"] = origins
	return b
}

// Geolocation sets the geolocation directive
func (b *PermissionsPolicyBuilder) Geolocation(origins ...string) *PermissionsPolicyBuilder {
	b.directives["geolocation"] = origins
	return b
}

// Payment sets the payment directive
func (b *PermissionsPolicyBuilder) Payment(origins ...string) *PermissionsPolicyBuilder {
	b.directives["payment"] = origins
	return b
}

// Build builds the Permissions Policy header value
func (b *PermissionsPolicyBuilder) Build() string {
	var parts []string

	for directive, origins := range b.directives {
		if len(origins) == 0 {
			parts = append(parts, fmt.Sprintf("%s=()", directive))
		} else {
			parts = append(parts, fmt.Sprintf("%s=(%s)", directive, strings.Join(origins, " ")))
		}
	}

	return strings.Join(parts, ", ")
}

// Common Permissions Policy values
const (
	PPSelf = "self"
	PPNone = "()"
)

// RestrictivePermissionsPolicy returns a restrictive permissions policy
func RestrictivePermissionsPolicy() string {
	return NewPermissionsPolicyBuilder().
		Camera().
		Microphone().
		Geolocation().
		Payment().
		Build()
}
