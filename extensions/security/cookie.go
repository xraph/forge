package security

import (
	"errors"
	"net/http"
	"time"
)

var (
	// ErrCookieNotFound is returned when a cookie is not found
	ErrCookieNotFound = errors.New("cookie not found")
	// ErrInvalidCookie is returned when cookie data is invalid
	ErrInvalidCookie = errors.New("invalid cookie")
)

// SameSiteMode represents the SameSite attribute for cookies
type SameSiteMode string

const (
	// SameSiteDefault uses the browser's default behavior
	SameSiteDefault SameSiteMode = "default"
	// SameSiteLax allows cookies for top-level navigation
	SameSiteLax SameSiteMode = "lax"
	// SameSiteStrict only sends cookies for same-site requests
	SameSiteStrict SameSiteMode = "strict"
	// SameSiteNone allows cookies for cross-site requests (requires Secure)
	SameSiteNone SameSiteMode = "none"
)

// CookieOptions holds options for creating a cookie
type CookieOptions struct {
	// Path specifies the cookie path
	Path string

	// Domain specifies the cookie domain
	Domain string

	// MaxAge specifies the cookie lifetime in seconds
	MaxAge int

	// Expires specifies the cookie expiration time
	Expires time.Time

	// Secure indicates if the cookie should only be sent over HTTPS
	Secure bool

	// HttpOnly indicates if the cookie should be inaccessible to JavaScript
	HttpOnly bool

	// SameSite specifies the SameSite attribute
	SameSite SameSiteMode
}

// DefaultCookieOptions returns secure default cookie options
func DefaultCookieOptions() CookieOptions {
	return CookieOptions{
		Path:     "/",
		Secure:   true,  // Always use HTTPS in production
		HttpOnly: true,  // Prevent XSS attacks
		SameSite: SameSiteLax, // CSRF protection
	}
}

// CookieManager manages HTTP cookies with security best practices
type CookieManager struct {
	defaults CookieOptions
}

// NewCookieManager creates a new cookie manager with default options
func NewCookieManager(defaults CookieOptions) *CookieManager {
	return &CookieManager{
		defaults: defaults,
	}
}

// SetCookie sets a cookie on the response
func (cm *CookieManager) SetCookie(w http.ResponseWriter, name, value string, opts *CookieOptions) {
	if opts == nil {
		opts = &cm.defaults
	}

	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     cm.getOrDefault(opts.Path, cm.defaults.Path),
		Domain:   cm.getOrDefault(opts.Domain, cm.defaults.Domain),
		MaxAge:   cm.getIntOrDefault(opts.MaxAge, cm.defaults.MaxAge),
		Secure:   cm.getBoolOrDefault(opts.Secure, cm.defaults.Secure),
		HttpOnly: cm.getBoolOrDefault(opts.HttpOnly, cm.defaults.HttpOnly),
		SameSite: cm.convertSameSite(cm.getSameSiteOrDefault(opts.SameSite, cm.defaults.SameSite)),
	}

	if !opts.Expires.IsZero() {
		cookie.Expires = opts.Expires
	}

	http.SetCookie(w, cookie)
}

// GetCookie gets a cookie from the request
func (cm *CookieManager) GetCookie(r *http.Request, name string) (string, error) {
	cookie, err := r.Cookie(name)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", ErrCookieNotFound
		}
		return "", err
	}
	return cookie.Value, nil
}

// DeleteCookie deletes a cookie by setting MaxAge to -1
func (cm *CookieManager) DeleteCookie(w http.ResponseWriter, name string, opts *CookieOptions) {
	if opts == nil {
		opts = &cm.defaults
	}

	cookie := &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     cm.getOrDefault(opts.Path, cm.defaults.Path),
		Domain:   cm.getOrDefault(opts.Domain, cm.defaults.Domain),
		MaxAge:   -1, // Delete cookie
		Secure:   cm.defaults.Secure,
		HttpOnly: cm.defaults.HttpOnly,
		SameSite: cm.convertSameSite(cm.defaults.SameSite),
	}

	http.SetCookie(w, cookie)
}

// HasCookie checks if a cookie exists in the request
func (cm *CookieManager) HasCookie(r *http.Request, name string) bool {
	_, err := r.Cookie(name)
	return err == nil
}

// GetAllCookies returns all cookies from the request
func (cm *CookieManager) GetAllCookies(r *http.Request) []*http.Cookie {
	return r.Cookies()
}

// Helper methods

func (cm *CookieManager) getOrDefault(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}

func (cm *CookieManager) getIntOrDefault(value, defaultValue int) int {
	if value != 0 {
		return value
	}
	return defaultValue
}

func (cm *CookieManager) getBoolOrDefault(value, defaultValue bool) bool {
	// For boolean, we need to check if it was explicitly set
	// Since Go's zero value for bool is false, we treat false as "use default"
	// To explicitly set false, users should set it in defaults
	return value || defaultValue
}

func (cm *CookieManager) getSameSiteOrDefault(value, defaultValue SameSiteMode) SameSiteMode {
	if value != "" {
		return value
	}
	return defaultValue
}

func (cm *CookieManager) convertSameSite(mode SameSiteMode) http.SameSite {
	switch mode {
	case SameSiteStrict:
		return http.SameSiteStrictMode
	case SameSiteLax:
		return http.SameSiteLaxMode
	case SameSiteNone:
		return http.SameSiteNoneMode
	default:
		return http.SameSiteDefaultMode
	}
}

