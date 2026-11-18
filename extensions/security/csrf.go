package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
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
	Enabled bool `yaml:"enabled"`

	// TokenLength is the length of the CSRF token in bytes (default: 32)
	TokenLength int `yaml:"token_length"`

	// TokenLookup defines where to find the token:
	// - "header:<name>" - look in header
	// - "form:<name>" - look in form field
	// - "query:<name>" - look in query parameter
	// Default: "header:X-CSRF-Token"
	TokenLookup string `yaml:"token_lookup"`

	// CookieName is the name of the CSRF cookie (default: "csrf_token")
	CookieName string `yaml:"cookie_name"`

	// CookiePath is the path for the CSRF cookie (default: "/")
	CookiePath string `yaml:"cookie_path"`

	// CookieDomain is the domain for the CSRF cookie
	CookieDomain string `yaml:"cookie_domain"`

	// CookieSecure indicates if the cookie should only be sent over HTTPS
	CookieSecure bool `yaml:"cookie_secure"`

	// CookieHTTPOnly indicates if the cookie should be inaccessible to JavaScript
	// Note: For CSRF tokens, this should be false so JavaScript can read it
	CookieHTTPOnly bool `yaml:"cookie_http_only"`

	// CookieSameSite specifies the SameSite attribute
	CookieSameSite string `yaml:"cookie_same_site"`

	// TTL is the token time-to-live (default: 12 hours)
	TTL time.Duration `yaml:"ttl"`

	// Secret is the HMAC key that signs CSRF tokens. Set it to the same value
	// on every replica so a token issued by one instance validates on
	// another, and so tokens survive a restart. When empty, a random key is
	// generated per process.
	Secret string `yaml:"secret"`

	// BindToSession ties each token to the current session ID when
	// SessionMiddleware runs before the CSRF middleware. A token issued
	// before login is then replaced on the next safe request after login,
	// and an unsafe request carrying it is rejected. Off by default.
	BindToSession bool `yaml:"bind_to_session"`

	// SafeMethods are HTTP methods that don't require CSRF protection
	// Default: GET, HEAD, OPTIONS, TRACE
	SafeMethods []string `yaml:"safe_methods"`

	// SkipPaths is a list of paths to skip CSRF protection
	SkipPaths []string `yaml:"skip_paths"`

	// AutoApplyMiddleware automatically applies CSRF middleware globally
	AutoApplyMiddleware bool `yaml:"auto_apply_middleware"`

	// ErrorHandler is called when CSRF validation fails
	ErrorHandler func(forge.Context, error) error `yaml:"-"`
}

// DefaultCSRFConfig returns the default CSRF configuration.
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{ //nolint:gosec // G101: "csrf_token" and the token lookup are cookie and header names, not credentials
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

// maxStoredCSRFTokens bounds the map behind SetToken. The middleware never
// writes to it, but an application that does should not be able to grow it
// without limit either.
const maxStoredCSRFTokens = 100_000

// Signed token layout, before base64url encoding:
//
//	version (1) | expiry unix seconds, big endian (8) | nonce (TokenLength) | HMAC-SHA256 (32)
const (
	csrfTokenVersion   = 1
	csrfTokenMACDomain = "forge-csrf-v1" //nolint:gosec // G101: a domain separation label for the MAC input, not a credential
)

// CSRFProtection manages CSRF token generation and validation.
//
// The middleware uses the signed double-submit cookie pattern. A token is a
// random nonce and an expiry, signed with an HMAC key that only the server
// knows. The same token goes into a cookie and must come back in a header,
// form field or query parameter on unsafe requests. Nothing is stored on the
// server, so a client cannot make the server hold state by sending cookies,
// and a cookie value the server did not sign is never trusted.
type CSRFProtection struct {
	config CSRFConfig
	logger forge.Logger
	secret []byte

	lookupSource string
	lookupKey    string

	// tokens backs the SetToken/GetToken/ValidateToken API for applications
	// that keep their own per-session tokens. It is bounded and swept.
	mu     sync.Mutex
	tokens map[string]csrfToken
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

	secret := []byte(config.Secret)
	if len(secret) == 0 {
		// A per-process key keeps the default config working. Tokens then do
		// not survive a restart or validate on another replica, which is what
		// the old in-memory token map did too. Set Secret to share them.
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			panic(fmt.Sprintf("security: failed to generate csrf secret: %v", err))
		}
	}

	c := &CSRFProtection{
		config: config,
		logger: logger,
		secret: secret,
		tokens: make(map[string]csrfToken),
	}

	// Parse the lookup once here instead of on every request.
	if source, key, ok := strings.Cut(config.TokenLookup, ":"); ok {
		c.lookupSource = strings.TrimSpace(source)
		c.lookupKey = strings.TrimSpace(key)
	}

	return c
}

// GenerateToken generates a new random CSRF token. It is not signed; the
// middleware uses IssueToken instead.
func (c *CSRFProtection) GenerateToken() (string, error) {
	bytes := make([]byte, c.config.TokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate csrf token: %w", err)
	}

	return base64.URLEncoding.EncodeToString(bytes), nil
}

// IssueToken returns a new signed CSRF token that expires after the
// configured TTL. binding ties the token to a value such as a session ID;
// VerifyToken must then be given the same binding. Pass "" for none.
func (c *CSRFProtection) IssueToken(binding string) (string, error) {
	raw := make([]byte, 1+8+c.config.TokenLength, 1+8+c.config.TokenLength+sha256.Size)
	raw[0] = csrfTokenVersion
	binary.BigEndian.PutUint64(raw[1:9], uint64(time.Now().Add(c.config.TTL).Unix())) //nolint:gosec // G115: a Unix time after 1970 is never negative

	if _, err := rand.Read(raw[9:]); err != nil {
		return "", fmt.Errorf("failed to generate csrf token: %w", err)
	}

	raw = append(raw, c.tokenMAC(raw, binding)...)

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// VerifyToken checks that token was issued by this server for binding and has
// not expired. The signature is compared in constant time.
func (c *CSRFProtection) VerifyToken(token, binding string) error {
	if token == "" {
		return ErrCSRFTokenMissing
	}

	// Reject anything of the wrong size before decoding, so an oversized
	// cookie costs nothing beyond a length check.
	rawLen := 1 + 8 + c.config.TokenLength + sha256.Size
	if len(token) != base64.RawURLEncoding.EncodedLen(rawLen) {
		return ErrCSRFTokenInvalid
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != rawLen || raw[0] != csrfTokenVersion {
		return ErrCSRFTokenInvalid
	}

	body, mac := raw[:rawLen-sha256.Size], raw[rawLen-sha256.Size:]
	if !hmac.Equal(mac, c.tokenMAC(body, binding)) {
		return ErrCSRFTokenInvalid
	}

	expiry := int64(binary.BigEndian.Uint64(body[1:9])) //nolint:gosec // G115: the value was written from a non-negative int64 and is covered by the MAC
	if time.Now().Unix() > expiry {
		return ErrCSRFTokenExpired
	}

	return nil
}

// tokenMAC signs a token body together with its binding.
func (c *CSRFProtection) tokenMAC(body []byte, binding string) []byte {
	h := hmac.New(sha256.New, c.secret)
	h.Write([]byte(csrfTokenMACDomain))
	h.Write(body)
	h.Write([]byte(binding))

	return h.Sum(nil)
}

// SetToken stores a CSRF token for a session ID chosen by the application.
// The middleware does not use this store. The store is bounded: expired
// entries are swept when it fills up, and if it is still full an arbitrary
// entry is dropped.
func (c *CSRFProtection) SetToken(sessionID, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.tokens[sessionID]; !exists && len(c.tokens) >= maxStoredCSRFTokens {
		c.sweepLocked(time.Now())

		if len(c.tokens) >= maxStoredCSRFTokens {
			for k := range c.tokens {
				delete(c.tokens, k)

				break
			}
		}
	}

	c.tokens[sessionID] = csrfToken{
		Value:     token,
		ExpiresAt: time.Now().Add(c.config.TTL),
	}
}

// sweepLocked removes expired entries. c.mu must be held.
func (c *CSRFProtection) sweepLocked(now time.Time) {
	for k, t := range c.tokens {
		if now.After(t.ExpiresAt) {
			delete(c.tokens, k)
		}
	}
}

// GetToken retrieves the CSRF token stored for a session with SetToken.
func (c *CSRFProtection) GetToken(sessionID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, ok := c.tokens[sessionID]
	if !ok {
		return "", false
	}

	if time.Now().After(token.ExpiresAt) {
		delete(c.tokens, sessionID)

		return "", false
	}

	return token.Value, true
}

// ValidateToken validates a CSRF token against the one stored with SetToken.
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

// DeleteToken removes a CSRF token stored with SetToken.
func (c *CSRFProtection) DeleteToken(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.tokens, sessionID)
}

// extractTokenFromRequest extracts the token from request based on TokenLookup config.
func (c *CSRFProtection) extractTokenFromRequest(r *http.Request) string {
	switch c.lookupSource {
	case "header":
		return r.Header.Get(c.lookupKey)
	case "form":
		return r.FormValue(c.lookupKey)
	case "query":
		return r.URL.Query().Get(c.lookupKey)
	default:
		return ""
	}
}

// binding returns the value a token is tied to for this request: the session
// ID when BindToSession is on and SessionMiddleware found a session, else "".
func (c *CSRFProtection) binding(ctx forge.Context) string {
	if !c.config.BindToSession {
		return ""
	}

	if session, ok := GetSessionFromForgeContext(ctx); ok && session != nil {
		return session.ID
	}

	return ""
}

// isSafeMethod checks if the HTTP method is safe (doesn't require CSRF protection).
func (c *CSRFProtection) isSafeMethod(method string) bool {
	return slices.Contains(c.config.SafeMethods, method)
}

// shouldSkipPath checks if the path should skip CSRF protection.
func (c *CSRFProtection) shouldSkipPath(path string) bool {
	return shouldSkipPath(path, c.config.SkipPaths)
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
	binding := c.binding(ctx)

	// Keep the client's token only if this server signed it for this binding
	// and it has not expired. Anything else, including an arbitrary value an
	// attacker planted, is replaced and never stored.
	token, _ := cookieManager.GetCookie(r, c.config.CookieName)
	if token == "" || c.VerifyToken(token, binding) != nil {
		var err error

		token, err = c.IssueToken(binding)
		if err != nil {
			c.logger.Error("failed to generate csrf token", forge.F("error", err))

			return ctx.String(http.StatusInternalServerError, "Internal Server Error")
		}

		cookieManager.SetCookie(w, c.config.CookieName, token, &CookieOptions{
			Path:     c.config.CookiePath,
			Domain:   c.config.CookieDomain,
			Secure:   c.config.CookieSecure,
			HttpOnly: c.config.CookieHTTPOnly,
			SameSite: SameSiteMode(c.config.CookieSameSite),
			MaxAge:   int(c.config.TTL.Seconds()),
		})
	}

	// Expose the token so a client or template can send it back.
	w.Header().Set("X-Csrf-Token", token)

	return next(ctx)
}

// handleUnsafeMethod handles unsafe HTTP methods (POST, PUT, DELETE, etc.)
func (c *CSRFProtection) handleUnsafeMethod(ctx forge.Context, cookieManager *CookieManager, next forge.Handler) error {
	r := ctx.Request()

	cookieToken, _ := cookieManager.GetCookie(r, c.config.CookieName)
	if cookieToken == "" {
		c.logger.Warn("csrf validation failed: no csrf cookie")

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

	// The cookie must be a token this server signed, and the request must
	// echo exactly that token.
	err := c.VerifyToken(cookieToken, c.binding(ctx))
	if err == nil && subtle.ConstantTimeCompare([]byte(cookieToken), []byte(token)) != 1 {
		err = ErrCSRFTokenInvalid
	}

	if err != nil {
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
