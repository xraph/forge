package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// CSRF errors.
var (
	ErrCSRFTokenMissing = errors.New("csrf token missing")
	ErrCSRFTokenInvalid = errors.New("csrf token invalid")
	ErrCSRFTokenExpired = errors.New("csrf token expired")
)

// CSRFConfig holds CSRF protection configuration.
type CSRFConfig struct {
	// Enabled determines if CSRF protection is enabled
	Enabled bool

	// TokenLength is the length of the CSRF token in bytes (default: 32)
	TokenLength int

	// TokenLookup defines where to find the token:
	// - "header:<name>" - look in header
	// - "form:<name>" - look in form field
	// - "query:<name>" - look in query parameter
	// Default: "header:X-CSRF-Token"
	TokenLookup string

	// CookieName is the name of the CSRF cookie (default: "csrf_token")
	CookieName string

	// CookiePath is the path for the CSRF cookie (default: "/")
	CookiePath string

	// CookieDomain is the domain for the CSRF cookie
	CookieDomain string

	// CookieSecure indicates if the cookie should only be sent over HTTPS
	CookieSecure bool

	// CookieHTTPOnly indicates if the cookie should be inaccessible to JavaScript
	// Note: For CSRF tokens, this should be false so JavaScript can read it
	CookieHTTPOnly bool

	// CookieSameSite specifies the SameSite attribute
	CookieSameSite string

	// TTL is the token time-to-live (default: 12 hours)
	TTL time.Duration

	// SafeMethods are HTTP methods that don't require CSRF protection
	// Default: GET, HEAD, OPTIONS, TRACE
	SafeMethods []string

	// SkipPaths is a list of paths to skip CSRF protection
	SkipPaths []string

	// AutoApplyMiddleware automatically applies CSRF middleware globally
	AutoApplyMiddleware bool

	// ErrorHandler is called when CSRF validation fails
	ErrorHandler func(forge.Context, error) error
}

// DefaultCSRFConfig returns the default CSRF configuration.
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		Enabled:             true,
		TokenLength:         32,
		TokenLookup:         "header:X-CSRF-Token",
		CookieName:          "csrf_token",
		CookiePath:          "/",
		CookieSecure:        true,
		CookieHTTPOnly:      false, // Must be false so JavaScript can read it
		CookieSameSite:      "strict",
		TTL:                 12 * time.Hour,
		SafeMethods:         []string{"GET", "HEAD", "OPTIONS", "TRACE"},
		SkipPaths:           []string{},
		AutoApplyMiddleware: false, // Default to false for backwards compatibility
		ErrorHandler: func(ctx forge.Context, err error) error {
			return ctx.JSON(http.StatusForbidden, map[string]string{
				"error": "CSRF token validation failed",
			})
		},
	}
}

// csrfToken represents a CSRF token with metadata.
type csrfToken struct {
	Value     string
	ExpiresAt time.Time
}

// CSRFProtection manages CSRF token generation and validation.
type CSRFProtection struct {
	config CSRFConfig
	tokens sync.Map // map[string]*csrfToken - session ID -> token
	logger forge.Logger
}

// NewCSRFProtection creates a new CSRF protection instance.
func NewCSRFProtection(config CSRFConfig, logger forge.Logger) *CSRFProtection {
	if config.TokenLength <= 0 {
		config.TokenLength = 32
	}

	if config.TokenLookup == "" {
		config.TokenLookup = "header:X-CSRF-Token"
	}

	if config.CookieName == "" {
		config.CookieName = "csrf_token"
	}

	if config.TTL <= 0 {
		config.TTL = 12 * time.Hour
	}

	if len(config.SafeMethods) == 0 {
		config.SafeMethods = []string{"GET", "HEAD", "OPTIONS", "TRACE"}
	}

	return &CSRFProtection{
		config: config,
		logger: logger,
	}
}

// GenerateToken generates a new CSRF token.
func (c *CSRFProtection) GenerateToken() (string, error) {
	bytes := make([]byte, c.config.TokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate csrf token: %w", err)
	}

	return base64.URLEncoding.EncodeToString(bytes), nil
}

// SetToken stores a CSRF token for a session.
func (c *CSRFProtection) SetToken(sessionID, token string) {
	c.tokens.Store(sessionID, &csrfToken{
		Value:     token,
		ExpiresAt: time.Now().Add(c.config.TTL),
	})
}

// GetToken retrieves the CSRF token for a session.
func (c *CSRFProtection) GetToken(sessionID string) (string, bool) {
	if val, ok := c.tokens.Load(sessionID); ok {
		token := val.(*csrfToken)
		if time.Now().After(token.ExpiresAt) {
			c.tokens.Delete(sessionID)

			return "", false
		}

		return token.Value, true
	}

	return "", false
}

// ValidateToken validates a CSRF token against the stored token.
func (c *CSRFProtection) ValidateToken(sessionID, providedToken string) error {
	if providedToken == "" {
		return ErrCSRFTokenMissing
	}

	storedToken, ok := c.GetToken(sessionID)
	if !ok {
		return ErrCSRFTokenExpired
	}

	// Use constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(storedToken), []byte(providedToken)) != 1 {
		return ErrCSRFTokenInvalid
	}

	return nil
}

// DeleteToken removes a CSRF token for a session.
func (c *CSRFProtection) DeleteToken(sessionID string) {
	c.tokens.Delete(sessionID)
}

// extractTokenFromRequest extracts the token from request based on TokenLookup config.
func (c *CSRFProtection) extractTokenFromRequest(r *http.Request) string {
	parts := strings.Split(c.config.TokenLookup, ":")
	if len(parts) != 2 {
		return ""
	}

	extractor := parts[0]
	key := parts[1]

	switch extractor {
	case "header":
		return r.Header.Get(key)
	case "form":
		return r.FormValue(key)
	case "query":
		return r.URL.Query().Get(key)
	default:
		return ""
	}
}

// isSafeMethod checks if the HTTP method is safe (doesn't require CSRF protection).
func (c *CSRFProtection) isSafeMethod(method string) bool {

	return slices.Contains(c.config.SafeMethods, method)
}

// shouldSkipPath checks if the path should skip CSRF protection.
func (c *CSRFProtection) shouldSkipPath(path string) bool {
	for _, skipPath := range c.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}

	return false
}

// CSRFMiddleware returns a middleware function for CSRF protection.
func CSRFMiddleware(csrf *CSRFProtection, cookieManager *CookieManager) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if !csrf.config.Enabled {
				return next(ctx)
			}

			r := ctx.Request()

			// Skip CSRF protection for specified paths
			if csrf.shouldSkipPath(r.URL.Path) {
				return next(ctx)
			}

			// Skip CSRF protection for safe methods
			if csrf.isSafeMethod(r.Method) {
				// For safe methods, generate and set a new token if one doesn't exist
				return csrf.handleSafeMethod(ctx, cookieManager, next)
			}

			// For unsafe methods (POST, PUT, DELETE, etc.), validate the CSRF token
			return csrf.handleUnsafeMethod(ctx, cookieManager, next)
		}
	}
}

// handleSafeMethod handles safe HTTP methods (GET, HEAD, etc.)
func (c *CSRFProtection) handleSafeMethod(ctx forge.Context, cookieManager *CookieManager, next forge.Handler) error {
	r := ctx.Request()
	w := ctx.Response()

	// Get or create session ID
	sessionID, _ := cookieManager.GetCookie(r, c.config.CookieName)

	// Check if we already have a valid token
	if sessionID != "" {
		if _, ok := c.GetToken(sessionID); ok {
			// Token exists and is valid
			return next(ctx)
		}
	}

	// Generate new token
	token, err := c.GenerateToken()
	if err != nil {
		c.logger.Error("failed to generate csrf token", forge.F("error", err))

		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}

	// Generate new session ID if needed
	if sessionID == "" {
		sessionID = token // Use the token as session ID for simplicity
	}

	// Store token
	c.SetToken(sessionID, token)

	// Set cookie
	cookieManager.SetCookie(w, c.config.CookieName, sessionID, &CookieOptions{
		Path:     c.config.CookiePath,
		Domain:   c.config.CookieDomain,
		Secure:   c.config.CookieSecure,
		HttpOnly: c.config.CookieHTTPOnly,
		SameSite: SameSiteMode(c.config.CookieSameSite),
		MaxAge:   int(c.config.TTL.Seconds()),
	})

	// Set token in response header so client can read it
	w.Header().Set("X-Csrf-Token", token)

	return next(ctx)
}

// handleUnsafeMethod handles unsafe HTTP methods (POST, PUT, DELETE, etc.)
func (c *CSRFProtection) handleUnsafeMethod(ctx forge.Context, cookieManager *CookieManager, next forge.Handler) error {
	r := ctx.Request()

	// Get session ID from cookie
	sessionID, _ := cookieManager.GetCookie(r, c.config.CookieName)
	if sessionID == "" {
		c.logger.Warn("csrf validation failed: no session cookie")

		return ctx.String(http.StatusForbidden, "CSRF token validation failed")
	}

	// Extract token from request
	token := c.extractTokenFromRequest(r)
	if token == "" {
		c.logger.Warn("csrf validation failed: no token provided",
			forge.F("method", r.Method),
			forge.F("path", r.URL.Path),
		)

		return ctx.String(http.StatusForbidden, "CSRF token validation failed")
	}

	// Validate token
	if err := c.ValidateToken(sessionID, token); err != nil {
		c.logger.Warn("csrf validation failed",
			forge.F("error", err),
			forge.F("method", r.Method),
			forge.F("path", r.URL.Path),
		)

		return ctx.String(http.StatusForbidden, "CSRF token validation failed")
	}

	// Token is valid, proceed
	return next(ctx)
}

// GetCSRFToken retrieves the CSRF token from response header (helper for templates/responses).
func GetCSRFToken(w http.ResponseWriter) (string, bool) {
	token := w.Header().Get("X-Csrf-Token")
	if token != "" {
		return token, true
	}

	return "", false
}
