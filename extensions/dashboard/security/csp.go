package security

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// CSPConfig configures the Content-Security-Policy header.
type CSPConfig struct {
	// Nonce is a cryptographically secure random value included in script-src
	// directives. Scripts must include a matching nonce attribute to execute.
	Nonce string

	// BasePath is the base path for the dashboard. Currently reserved for
	// future use in connect-src or frame-ancestors directives.
	BasePath string

	// AllowInline controls whether 'unsafe-inline' is added to script-src.
	// When false (default), only nonced scripts are permitted.
	AllowInline bool
}

// GenerateNonce creates a cryptographically secure random nonce for CSP.
// The nonce is a 16-byte random value encoded as unpadded base64.
func GenerateNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback should never occur in practice; crypto/rand reads from
		// the OS CSPRNG which does not fail under normal conditions.
		panic(fmt.Sprintf("security: failed to generate CSP nonce: %v", err))
	}

	return base64.RawStdEncoding.EncodeToString(b)
}

// BuildCSPHeader generates a Content-Security-Policy header value.
//
// The returned policy includes the following directives:
//   - default-src 'self'
//   - script-src  'self' 'nonce-{nonce}' plus CDN origins for HTMX/Alpine
//   - style-src   'self' 'unsafe-inline' (required for Tailwind/inline styles)
//   - img-src     'self' data: https:
//   - connect-src 'self' (covers SSE and API fetch calls)
//   - font-src    'self' https:
//   - frame-src   'none'
//   - object-src  'none'
//
// If AllowInline is set, 'unsafe-inline' is appended to script-src. This is
// discouraged in production but can be useful during development.
func BuildCSPHeader(config CSPConfig) string {
	// Build script-src directive.
	scriptSrc := fmt.Sprintf("'self' 'nonce-%s' https://unpkg.com https://cdn.jsdelivr.net", config.Nonce)
	if config.AllowInline {
		scriptSrc += " 'unsafe-inline'"
	}

	return fmt.Sprintf(
		"default-src 'self'; "+
			"script-src %s; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data: https:; "+
			"connect-src 'self'; "+
			"font-src 'self' https:; "+
			"frame-src 'none'; "+
			"object-src 'none'",
		scriptSrc,
	)
}
