package security

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/xraph/forge"
)

// SecurityHeadersConfig holds security headers configuration
type SecurityHeadersConfig struct {
	// Enabled determines if security headers are enabled
	Enabled bool

	// XFrameOptions prevents clickjacking attacks
	// Values: "DENY", "SAMEORIGIN", "ALLOW-FROM https://example.com"
	// Default: "SAMEORIGIN"
	XFrameOptions string

	// XContentTypeOptions prevents MIME-sniffing attacks
	// Value: "nosniff"
	// Default: "nosniff"
	XContentTypeOptions string

	// XSSProtection enables XSS filter in older browsers
	// Values: "0" (disable), "1" (enable), "1; mode=block" (enable and block)
	// Default: "1; mode=block"
	// Note: Modern browsers use CSP instead
	XSSProtection string

	// ContentSecurityPolicy defines CSP rules
	// Example: "default-src 'self'; script-src 'self' 'unsafe-inline'"
	// Default: "" (not set)
	ContentSecurityPolicy string

	// ContentSecurityPolicyReportOnly enables CSP in report-only mode
	// Useful for testing CSP without breaking the site
	ContentSecurityPolicyReportOnly string

	// StrictTransportSecurity enforces HTTPS
	// Example: "max-age=31536000; includeSubDomains; preload"
	// Default: "max-age=31536000; includeSubDomains"
	StrictTransportSecurity string

	// ReferrerPolicy controls referrer information
	// Values: "no-referrer", "no-referrer-when-downgrade", "origin",
	//         "origin-when-cross-origin", "same-origin", "strict-origin",
	//         "strict-origin-when-cross-origin", "unsafe-url"
	// Default: "strict-origin-when-cross-origin"
	ReferrerPolicy string

	// PermissionsPolicy controls browser features
	// Example: "geolocation=(self), microphone=()"
	// Default: ""
	PermissionsPolicy string

	// CrossOriginEmbedderPolicy controls document embedding
	// Values: "unsafe-none", "require-corp", "credentialless"
	// Default: "" (not set)
	CrossOriginEmbedderPolicy string

	// CrossOriginOpenerPolicy controls cross-origin window interaction
	// Values: "unsafe-none", "same-origin-allow-popups", "same-origin"
	// Default: "" (not set)
	CrossOriginOpenerPolicy string

	// CrossOriginResourcePolicy controls resource loading
	// Values: "same-site", "same-origin", "cross-origin"
	// Default: "" (not set)
	CrossOriginResourcePolicy string

	// RemoveServerHeader removes the Server header
	// Default: true
	RemoveServerHeader bool

	// RemovePoweredBy removes the X-Powered-By header
	// Default: true
	RemovePoweredBy bool

	// CustomHeaders allows setting custom security headers
	CustomHeaders map[string]string

	// SkipPaths is a list of paths to skip security headers
	SkipPaths []string

	// AutoApplyMiddleware automatically applies security headers middleware globally
	AutoApplyMiddleware bool
}

// DefaultSecurityHeadersConfig returns the default security headers configuration
func DefaultSecurityHeadersConfig() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		Enabled:                     true,
		XFrameOptions:               "SAMEORIGIN",
		XContentTypeOptions:         "nosniff",
		XSSProtection:               "1; mode=block",
		StrictTransportSecurity:     "max-age=31536000; includeSubDomains",
		ReferrerPolicy:              "strict-origin-when-cross-origin",
		RemoveServerHeader:          true,
		RemovePoweredBy:             true,
		CustomHeaders:               make(map[string]string),
		SkipPaths:                   []string{},
		AutoApplyMiddleware:         false, // Default to false for backwards compatibility
	}
}

// SecureHeadersPreset returns a preset configuration for secure headers
func SecureHeadersPreset() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		Enabled:                     true,
		XFrameOptions:               "DENY",
		XContentTypeOptions:         "nosniff",
		XSSProtection:               "1; mode=block",
		ContentSecurityPolicy:       "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'",
		StrictTransportSecurity:     "max-age=63072000; includeSubDomains; preload",
		ReferrerPolicy:              "no-referrer",
		PermissionsPolicy:           "geolocation=(), microphone=(), camera=(), payment=(), usb=(), magnetometer=(), gyroscope=(), accelerometer=(), ambient-light-sensor=()",
		CrossOriginEmbedderPolicy:   "require-corp",
		CrossOriginOpenerPolicy:     "same-origin",
		CrossOriginResourcePolicy:   "same-origin",
		RemoveServerHeader:          true,
		RemovePoweredBy:             true,
		CustomHeaders:               make(map[string]string),
		SkipPaths:                   []string{},
	}
}

// SecurityHeadersManager manages security headers
type SecurityHeadersManager struct {
	config SecurityHeadersConfig
	logger forge.Logger
}

// NewSecurityHeadersManager creates a new security headers manager
func NewSecurityHeadersManager(config SecurityHeadersConfig, logger forge.Logger) *SecurityHeadersManager {
	return &SecurityHeadersManager{
		config: config,
		logger: logger,
	}
}

// shouldSkipPath checks if the path should skip security headers
func (m *SecurityHeadersManager) shouldSkipPath(path string) bool {
	for _, skipPath := range m.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// SecurityHeadersMiddleware returns a middleware function for setting security headers
func SecurityHeadersMiddleware(manager *SecurityHeadersManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !manager.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip security headers for specified paths
			if manager.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			headers := w.Header()

			// X-Frame-Options
			if manager.config.XFrameOptions != "" {
				headers.Set("X-Frame-Options", manager.config.XFrameOptions)
			}

			// X-Content-Type-Options
			if manager.config.XContentTypeOptions != "" {
				headers.Set("X-Content-Type-Options", manager.config.XContentTypeOptions)
			}

			// X-XSS-Protection
			if manager.config.XSSProtection != "" {
				headers.Set("X-XSS-Protection", manager.config.XSSProtection)
			}

			// Content-Security-Policy
			if manager.config.ContentSecurityPolicy != "" {
				headers.Set("Content-Security-Policy", manager.config.ContentSecurityPolicy)
			}

			// Content-Security-Policy-Report-Only
			if manager.config.ContentSecurityPolicyReportOnly != "" {
				headers.Set("Content-Security-Policy-Report-Only", manager.config.ContentSecurityPolicyReportOnly)
			}

		// Strict-Transport-Security (only set on HTTPS)
		if manager.config.StrictTransportSecurity != "" && r.TLS != nil {
			headers.Set("Strict-Transport-Security", manager.config.StrictTransportSecurity)
		}

		// Referrer-Policy
		if manager.config.ReferrerPolicy != "" {
			headers.Set("Referrer-Policy", manager.config.ReferrerPolicy)
		}

		// Permissions-Policy
		if manager.config.PermissionsPolicy != "" {
			headers.Set("Permissions-Policy", manager.config.PermissionsPolicy)
		}

		// Cross-Origin-Embedder-Policy
		if manager.config.CrossOriginEmbedderPolicy != "" {
			headers.Set("Cross-Origin-Embedder-Policy", manager.config.CrossOriginEmbedderPolicy)
		}

		// Cross-Origin-Opener-Policy
		if manager.config.CrossOriginOpenerPolicy != "" {
			headers.Set("Cross-Origin-Opener-Policy", manager.config.CrossOriginOpenerPolicy)
		}

		// Cross-Origin-Resource-Policy
		if manager.config.CrossOriginResourcePolicy != "" {
			headers.Set("Cross-Origin-Resource-Policy", manager.config.CrossOriginResourcePolicy)
		}

		// Remove Server header
		if manager.config.RemoveServerHeader {
			headers.Del("Server")
		}

		// Remove X-Powered-By header
		if manager.config.RemovePoweredBy {
			headers.Del("X-Powered-By")
		}

		// Custom headers
		for key, value := range manager.config.CustomHeaders {
			headers.Set(key, value)
		}

		next.ServeHTTP(w, r)
	})
	}
}

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

// DefaultSrc sets the default source directive
func (b *CSPBuilder) DefaultSrc(sources ...string) *CSPBuilder {
	b.directives["default-src"] = sources
	return b
}

// ScriptSrc sets the script source directive
func (b *CSPBuilder) ScriptSrc(sources ...string) *CSPBuilder {
	b.directives["script-src"] = sources
	return b
}

// StyleSrc sets the style source directive
func (b *CSPBuilder) StyleSrc(sources ...string) *CSPBuilder {
	b.directives["style-src"] = sources
	return b
}

// ImgSrc sets the image source directive
func (b *CSPBuilder) ImgSrc(sources ...string) *CSPBuilder {
	b.directives["img-src"] = sources
	return b
}

// FontSrc sets the font source directive
func (b *CSPBuilder) FontSrc(sources ...string) *CSPBuilder {
	b.directives["font-src"] = sources
	return b
}

// ConnectSrc sets the connect source directive
func (b *CSPBuilder) ConnectSrc(sources ...string) *CSPBuilder {
	b.directives["connect-src"] = sources
	return b
}

// FrameSrc sets the frame source directive
func (b *CSPBuilder) FrameSrc(sources ...string) *CSPBuilder {
	b.directives["frame-src"] = sources
	return b
}

// FrameAncestors sets the frame-ancestors directive
func (b *CSPBuilder) FrameAncestors(sources ...string) *CSPBuilder {
	b.directives["frame-ancestors"] = sources
	return b
}

// ObjectSrc sets the object source directive
func (b *CSPBuilder) ObjectSrc(sources ...string) *CSPBuilder {
	b.directives["object-src"] = sources
	return b
}

// MediaSrc sets the media source directive
func (b *CSPBuilder) MediaSrc(sources ...string) *CSPBuilder {
	b.directives["media-src"] = sources
	return b
}

// WorkerSrc sets the worker source directive
func (b *CSPBuilder) WorkerSrc(sources ...string) *CSPBuilder {
	b.directives["worker-src"] = sources
	return b
}

// ChildSrc sets the child source directive
func (b *CSPBuilder) ChildSrc(sources ...string) *CSPBuilder {
	b.directives["child-src"] = sources
	return b
}

// FormAction sets the form-action directive
func (b *CSPBuilder) FormAction(sources ...string) *CSPBuilder {
	b.directives["form-action"] = sources
	return b
}

// BaseURI sets the base-uri directive
func (b *CSPBuilder) BaseURI(sources ...string) *CSPBuilder {
	b.directives["base-uri"] = sources
	return b
}

// UpgradeInsecureRequests adds the upgrade-insecure-requests directive
func (b *CSPBuilder) UpgradeInsecureRequests() *CSPBuilder {
	b.directives["upgrade-insecure-requests"] = nil
	return b
}

// BlockAllMixedContent adds the block-all-mixed-content directive
func (b *CSPBuilder) BlockAllMixedContent() *CSPBuilder {
	b.directives["block-all-mixed-content"] = nil
	return b
}

// ReportURI sets the report-uri directive
func (b *CSPBuilder) ReportURI(uri string) *CSPBuilder {
	b.directives["report-uri"] = []string{uri}
	return b
}

// ReportTo sets the report-to directive
func (b *CSPBuilder) ReportTo(group string) *CSPBuilder {
	b.directives["report-to"] = []string{group}
	return b
}

// Build constructs the CSP header value
func (b *CSPBuilder) Build() string {
	var parts []string
	for directive, sources := range b.directives {
		if sources == nil {
			parts = append(parts, directive)
		} else {
			parts = append(parts, fmt.Sprintf("%s %s", directive, strings.Join(sources, " ")))
		}
	}
	return strings.Join(parts, "; ")
}

// Common CSP source values
const (
	CSPSelf         = "'self'"
	CSPNone         = "'none'"
	CSPUnsafeInline = "'unsafe-inline'"
	CSPUnsafeEval   = "'unsafe-eval'"
	CSPStrictDynamic = "'strict-dynamic'"
	CSPData         = "data:"
	CSPBlob         = "blob:"
	CSPHTTPS        = "https:"
)

