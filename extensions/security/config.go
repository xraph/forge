package security

import (
	"errors"
	"time"
)

// Config holds the security extension configuration.
type Config struct {
	// Enabled determines if the security extension is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Session configuration
	Session SessionConfig `json:"session" yaml:"session"`

	// Cookie configuration
	Cookie CookieConfig `json:"cookie" yaml:"cookie"`

	// CSRF protection configuration
	CSRF CSRFConfig `json:"csrf" yaml:"csrf"`

	// Rate limiting configuration
	RateLimit RateLimitConfig `json:"rate_limit" yaml:"rate_limit"`

	// Security headers configuration
	SecurityHeaders SecurityHeadersConfig `json:"security_headers" yaml:"security_headers"`

	// Password hashing configuration
	PasswordHasher PasswordHasherConfig `json:"password_hasher" yaml:"password_hasher"`

	// JWT configuration
	JWT JWTConfig `json:"jwt" yaml:"jwt"`

	// CORS configuration
	CORS CORSConfig `json:"cors" yaml:"cors"`

	// API Key configuration
	APIKey APIKeyConfig `json:"api_key" yaml:"api_key"`

	// Audit logging configuration
	Audit AuditConfig `json:"audit" yaml:"audit"`

	// RequireConfig requires config from ConfigManager
	RequireConfig bool `json:"-" yaml:"-"`
}

// SessionConfig holds session-specific configuration.
type SessionConfig struct {
	// Enabled determines if session management is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Store specifies the session store backend ("inmemory", "redis")
	Store string `json:"store" yaml:"store"`

	// CookieName is the name of the session cookie
	CookieName string `json:"cookie_name" yaml:"cookie_name"`

	// TTL is the session time-to-live
	TTL time.Duration `json:"ttl" yaml:"ttl"`

	// IdleTimeout is the session idle timeout
	IdleTimeout time.Duration `json:"idle_timeout" yaml:"idle_timeout"`

	// AutoRenew automatically extends session TTL on access
	AutoRenew bool `json:"auto_renew" yaml:"auto_renew"`

	// AutoApplyMiddleware automatically applies session middleware globally
	AutoApplyMiddleware bool `json:"auto_apply_middleware" yaml:"auto_apply_middleware"`

	// SkipPaths is a list of paths to skip session handling (when auto-applied)
	SkipPaths []string `json:"skip_paths" yaml:"skip_paths"`

	// TrackIPAddress stores the client IP address in session
	TrackIPAddress bool `json:"track_ip_address" yaml:"track_ip_address"`

	// TrackUserAgent stores the client user agent in session
	TrackUserAgent bool `json:"track_user_agent" yaml:"track_user_agent"`

	// Redis configuration (used when Store is "redis")
	Redis RedisConfig `json:"redis" yaml:"redis"`
}

// RedisConfig holds Redis-specific configuration.
type RedisConfig struct {
	// Address is the Redis server address
	Address string `json:"address" yaml:"address"`

	// Password is the Redis password
	Password string `json:"password" yaml:"password"`

	// DB is the Redis database number
	DB int `json:"db" yaml:"db"`

	// PoolSize is the connection pool size
	PoolSize int `json:"pool_size" yaml:"pool_size"`
}

// CookieConfig holds cookie-specific configuration.
type CookieConfig struct {
	// Enabled determines if cookie management is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Secure indicates if cookies should only be sent over HTTPS
	Secure bool `json:"secure" yaml:"secure"`

	// HttpOnly indicates if cookies should be inaccessible to JavaScript
	HttpOnly bool `json:"http_only" yaml:"http_only"`

	// SameSite specifies the SameSite attribute
	SameSite string `json:"same_site" yaml:"same_site"`

	// Path specifies the cookie path
	Path string `json:"path" yaml:"path"`

	// Domain specifies the cookie domain
	Domain string `json:"domain" yaml:"domain"`

	// MaxAge specifies the cookie lifetime in seconds
	MaxAge int `json:"max_age" yaml:"max_age"`
}

// DefaultConfig returns the default security configuration.
func DefaultConfig() Config {
	return Config{
		Enabled: true,
		Session: SessionConfig{
			Enabled:             true,
			Store:               "inmemory",
			CookieName:          "forge_session",
			TTL:                 24 * time.Hour,
			IdleTimeout:         30 * time.Minute,
			AutoRenew:           true,
			AutoApplyMiddleware: false, // Default to false for backwards compatibility
			SkipPaths:           []string{"/health", "/metrics"},
			TrackIPAddress:      false,
			TrackUserAgent:      false,
			Redis: RedisConfig{
				Address:  "localhost:6379",
				DB:       0,
				PoolSize: 10,
			},
		},
		Cookie: CookieConfig{
			Enabled:  true,
			Secure:   true,
			HttpOnly: true,
			SameSite: "lax",
			Path:     "/",
		},
		CSRF:            DefaultCSRFConfig(),
		RateLimit:       DefaultRateLimitConfig(),
		SecurityHeaders: DefaultSecurityHeadersConfig(),
		PasswordHasher:  DefaultPasswordHasherConfig(),
		JWT:             DefaultJWTConfig(),
		CORS:            DefaultCORSConfig(),
		APIKey:          DefaultAPIKeyConfig(),
		Audit:           DefaultAuditConfig(),
	}
}

// Validate validates the security configuration.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Session.Enabled {
		if c.Session.Store == "" {
			return errors.New("session store is required")
		}

		if c.Session.CookieName == "" {
			return errors.New("session cookie name is required")
		}

		if c.Session.TTL <= 0 {
			return errors.New("session TTL must be positive")
		}

		validStores := map[string]bool{"inmemory": true, "redis": true}
		if !validStores[c.Session.Store] {
			return errors.New("invalid session store: must be 'inmemory' or 'redis'")
		}

		if c.Session.Store == "redis" && c.Session.Redis.Address == "" {
			return errors.New("redis address is required when using redis store")
		}
	}

	if c.Cookie.Enabled {
		validSameSite := map[string]bool{"default": true, "lax": true, "strict": true, "none": true}
		if !validSameSite[c.Cookie.SameSite] {
			return errors.New("invalid cookie SameSite value: must be 'default', 'lax', 'strict', or 'none'")
		}

		if c.Cookie.SameSite == "none" && !c.Cookie.Secure {
			return errors.New("cookie SameSite=none requires Secure=true")
		}
	}

	return nil
}

// ConfigOption configures the security extension.
type ConfigOption func(*Config)

// WithEnabled sets whether the security extension is enabled.
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithSessionEnabled sets whether session management is enabled.
func WithSessionEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Session.Enabled = enabled
	}
}

// WithSessionStore sets the session store backend.
func WithSessionStore(store string) ConfigOption {
	return func(c *Config) {
		c.Session.Store = store
	}
}

// WithSessionCookieName sets the session cookie name.
func WithSessionCookieName(name string) ConfigOption {
	return func(c *Config) {
		c.Session.CookieName = name
	}
}

// WithSessionTTL sets the session TTL.
func WithSessionTTL(ttl time.Duration) ConfigOption {
	return func(c *Config) {
		c.Session.TTL = ttl
	}
}

// WithSessionIdleTimeout sets the session idle timeout.
func WithSessionIdleTimeout(timeout time.Duration) ConfigOption {
	return func(c *Config) {
		c.Session.IdleTimeout = timeout
	}
}

// WithSessionAutoRenew enables or disables automatic session renewal.
func WithSessionAutoRenew(autoRenew bool) ConfigOption {
	return func(c *Config) {
		c.Session.AutoRenew = autoRenew
	}
}

// WithAutoApplyMiddleware enables or disables automatic global middleware application.
func WithAutoApplyMiddleware(autoApply bool) ConfigOption {
	return func(c *Config) {
		c.Session.AutoApplyMiddleware = autoApply
	}
}

// WithSkipPaths sets the paths to skip session handling.
func WithSkipPaths(paths []string) ConfigOption {
	return func(c *Config) {
		c.Session.SkipPaths = paths
	}
}

// WithRedisAddress sets the Redis server address.
func WithRedisAddress(address string) ConfigOption {
	return func(c *Config) {
		c.Session.Redis.Address = address
	}
}

// WithRedisPassword sets the Redis password.
func WithRedisPassword(password string) ConfigOption {
	return func(c *Config) {
		c.Session.Redis.Password = password
	}
}

// WithRedisDB sets the Redis database number.
func WithRedisDB(db int) ConfigOption {
	return func(c *Config) {
		c.Session.Redis.DB = db
	}
}

// WithCookieEnabled sets whether cookie management is enabled.
func WithCookieEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Cookie.Enabled = enabled
	}
}

// WithCookieSecure sets whether cookies should only be sent over HTTPS.
func WithCookieSecure(secure bool) ConfigOption {
	return func(c *Config) {
		c.Cookie.Secure = secure
	}
}

// WithCookieHttpOnly sets whether cookies should be inaccessible to JavaScript.
func WithCookieHttpOnly(httpOnly bool) ConfigOption {
	return func(c *Config) {
		c.Cookie.HttpOnly = httpOnly
	}
}

// WithCookieSameSite sets the SameSite attribute for cookies.
func WithCookieSameSite(sameSite string) ConfigOption {
	return func(c *Config) {
		c.Cookie.SameSite = sameSite
	}
}

// WithCookiePath sets the cookie path.
func WithCookiePath(path string) ConfigOption {
	return func(c *Config) {
		c.Cookie.Path = path
	}
}

// WithCookieDomain sets the cookie domain.
func WithCookieDomain(domain string) ConfigOption {
	return func(c *Config) {
		c.Cookie.Domain = domain
	}
}

// WithRequireConfig requires config from ConfigManager.
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) {
		c.RequireConfig = require
	}
}

// WithConfig sets the complete config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}

// WithCSRFEnabled enables or disables CSRF protection.
func WithCSRFEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.CSRF.Enabled = enabled
	}
}

// WithCSRFConfig sets the complete CSRF config.
func WithCSRFConfig(config CSRFConfig) ConfigOption {
	return func(c *Config) {
		c.CSRF = config
	}
}

// WithRateLimitEnabled enables or disables rate limiting.
func WithRateLimitEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.RateLimit.Enabled = enabled
	}
}

// WithRateLimitConfig sets the complete rate limit config.
func WithRateLimitConfig(config RateLimitConfig) ConfigOption {
	return func(c *Config) {
		c.RateLimit = config
	}
}

// WithSecurityHeadersEnabled enables or disables security headers.
func WithSecurityHeadersEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.SecurityHeaders.Enabled = enabled
	}
}

// WithSecurityHeadersConfig sets the complete security headers config.
func WithSecurityHeadersConfig(config SecurityHeadersConfig) ConfigOption {
	return func(c *Config) {
		c.SecurityHeaders = config
	}
}

// WithSecureHeaders applies secure header presets.
func WithSecureHeaders() ConfigOption {
	return func(c *Config) {
		c.SecurityHeaders = SecureHeadersPreset()
	}
}

// WithPasswordHasherConfig sets the complete password hasher config.
func WithPasswordHasherConfig(config PasswordHasherConfig) ConfigOption {
	return func(c *Config) {
		c.PasswordHasher = config
	}
}

// WithSecurePasswordHasher applies secure password hashing settings.
func WithSecurePasswordHasher() ConfigOption {
	return func(c *Config) {
		c.PasswordHasher = SecurePasswordHasherConfig()
	}
}

// WithJWTEnabled enables or disables JWT authentication.
func WithJWTEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.JWT.Enabled = enabled
	}
}

// WithJWTConfig sets the complete JWT config.
func WithJWTConfig(config JWTConfig) ConfigOption {
	return func(c *Config) {
		c.JWT = config
	}
}

// WithJWTSigningKey sets the JWT signing key.
func WithJWTSigningKey(key string) ConfigOption {
	return func(c *Config) {
		c.JWT.SigningKey = key
	}
}

// WithCORSEnabled enables or disables CORS.
func WithCORSEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.CORS.Enabled = enabled
	}
}

// WithCORSConfig sets the complete CORS config.
func WithCORSConfig(config CORSConfig) ConfigOption {
	return func(c *Config) {
		c.CORS = config
	}
}

// WithCORSOrigins sets the allowed CORS origins.
func WithCORSOrigins(origins []string) ConfigOption {
	return func(c *Config) {
		c.CORS.AllowOrigins = origins
	}
}

// WithCORSMethods sets the allowed CORS HTTP methods.
func WithCORSMethods(methods []string) ConfigOption {
	return func(c *Config) {
		c.CORS.AllowMethods = methods
	}
}

// WithCORSHeaders sets the allowed CORS request headers.
func WithCORSHeaders(headers []string) ConfigOption {
	return func(c *Config) {
		c.CORS.AllowHeaders = headers
	}
}

// WithCORSExposeHeaders sets the CORS headers exposed to the browser.
func WithCORSExposeHeaders(headers []string) ConfigOption {
	return func(c *Config) {
		c.CORS.ExposeHeaders = headers
	}
}

// WithCORSAllowCredentials sets whether credentials are allowed in CORS requests.
func WithCORSAllowCredentials(allow bool) ConfigOption {
	return func(c *Config) {
		c.CORS.AllowCredentials = allow
	}
}

// WithCORSMaxAge sets the CORS preflight cache duration in seconds.
func WithCORSMaxAge(maxAge int) ConfigOption {
	return func(c *Config) {
		c.CORS.MaxAge = maxAge
	}
}

// WithCORSMaxAgeDuration sets the CORS preflight cache duration from a time.Duration.
func WithCORSMaxAgeDuration(duration time.Duration) ConfigOption {
	return func(c *Config) {
		c.CORS.MaxAge = int(duration.Seconds())
	}
}

// WithCORSAllowPrivateNetwork sets whether private network access is allowed.
func WithCORSAllowPrivateNetwork(allow bool) ConfigOption {
	return func(c *Config) {
		c.CORS.AllowPrivateNetwork = allow
	}
}

// WithCORSSkipPaths sets paths to skip CORS handling.
func WithCORSSkipPaths(paths []string) ConfigOption {
	return func(c *Config) {
		c.CORS.SkipPaths = paths
	}
}

// WithAPIKeyEnabled enables or disables API key authentication.
func WithAPIKeyEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.APIKey.Enabled = enabled
	}
}

// WithAPIKeyConfig sets the complete API key config.
func WithAPIKeyConfig(config APIKeyConfig) ConfigOption {
	return func(c *Config) {
		c.APIKey = config
	}
}

// WithAuditEnabled enables or disables audit logging.
func WithAuditEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Audit.Enabled = enabled
	}
}

// WithAuditConfig sets the complete audit config.
func WithAuditConfig(config AuditConfig) ConfigOption {
	return func(c *Config) {
		c.Audit = config
	}
}

// WithAuditLevel sets the audit logging level.
func WithAuditLevel(level string) ConfigOption {
	return func(c *Config) {
		c.Audit.Level = level
	}
}

// Auto-Apply Middleware Options

// WithAutoApplyCSRF enables or disables automatic global CSRF middleware application.
func WithAutoApplyCSRF(autoApply bool) ConfigOption {
	return func(c *Config) {
		c.CSRF.AutoApplyMiddleware = autoApply
	}
}

// WithAutoApplyRateLimit enables or disables automatic global rate limiting middleware application.
func WithAutoApplyRateLimit(autoApply bool) ConfigOption {
	return func(c *Config) {
		c.RateLimit.AutoApplyMiddleware = autoApply
	}
}

// WithAutoApplySecurityHeaders enables or disables automatic global security headers middleware application.
func WithAutoApplySecurityHeaders(autoApply bool) ConfigOption {
	return func(c *Config) {
		c.SecurityHeaders.AutoApplyMiddleware = autoApply
	}
}

// WithAutoApplyJWT enables or disables automatic global JWT middleware application.
func WithAutoApplyJWT(autoApply bool) ConfigOption {
	return func(c *Config) {
		c.JWT.AutoApplyMiddleware = autoApply
	}
}

// WithAutoApplyCORS enables or disables automatic global CORS middleware application.
func WithAutoApplyCORS(autoApply bool) ConfigOption {
	return func(c *Config) {
		c.CORS.AutoApplyMiddleware = autoApply
	}
}

// WithAutoApplyAPIKey enables or disables automatic global API key middleware application.
func WithAutoApplyAPIKey(autoApply bool) ConfigOption {
	return func(c *Config) {
		c.APIKey.AutoApplyMiddleware = autoApply
	}
}

// WithAutoApplyAudit enables or disables automatic global audit middleware application.
func WithAutoApplyAudit(autoApply bool) ConfigOption {
	return func(c *Config) {
		c.Audit.AutoApplyMiddleware = autoApply
	}
}
