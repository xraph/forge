package security

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension and forge.MiddlewareExtension for comprehensive security features.
type Extension struct {
	*forge.BaseExtension

	config          Config
	sessionStore    SessionStore
	cookieManager   *CookieManager
	csrfProtection  *CSRFProtection
	rateLimiter     *MemoryRateLimiter
	securityHeaders *SecurityHeadersManager
	passwordHasher  *PasswordHasher
	jwtManager      *JWTManager
	corsManager     *CORSManager
	apiKeyManager   *APIKeyManager
	auditLogger     *AuditLogger
	middlewares     []forge.Middleware
}

// Compile-time interface enforcement.
var (
	_ forge.Extension           = (*Extension)(nil)
	_ forge.MiddlewareExtension = (*Extension)(nil)
)

// NewExtension creates a new security extension with functional options.
// Config is loaded from ConfigManager by default, with options providing overrides.
//
// Example:
//
//	// Load from ConfigManager (tries "extensions.security", then "security")
//	security.NewExtension()
//
//	// Override specific fields
//	security.NewExtension(
//	    security.WithSessionStore("redis"),
//	    security.WithRedisAddress("redis://localhost:6379"),
//	)
//
//	// Require config from ConfigManager
//	security.NewExtension(security.WithRequireConfig(true))
func NewExtension(opts ...ConfigOption) *Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("security", "2.0.0", "Comprehensive Security Features: Sessions, CSRF, Rate Limiting, JWT, CORS, Password Hashing, API Keys, Audit Logging, Security Headers")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new security extension with a complete config.
// This is for backward compatibility or when config is fully known at initialization.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the security extension with the app.
func (e *Extension) Register(app forge.App) error {
	// Call base registration (sets logger, metrics)
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	if !e.config.Enabled {
		e.Logger().Info("security extension disabled")

		return nil
	}

	// Load config from ConfigManager with dual-key support
	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("security", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("security: failed to load required config: %w", err)
		}

		e.Logger().Warn("security: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("security config validation failed: %w", err)
	}

	// Initialize session store if enabled
	if e.config.Session.Enabled {
		if err := e.initSessionStore(); err != nil {
			return fmt.Errorf("failed to initialize session store: %w", err)
		}

		// Register session store with DI container
		sessionStore := e.sessionStore
		if err := vessel.ProvideConstructor(app.Container(), func() SessionStore {
			return sessionStore
		}, vessel.WithAliases(SessionStoreKey, SessionStoreKeyLegacy)); err != nil {
			return fmt.Errorf("failed to register session store: %w", err)
		}

		// Prepare session middleware if auto-apply is enabled
		if e.config.Session.AutoApplyMiddleware {
			e.prepareSessionMiddleware()
		}
	}

	// Initialize cookie manager if enabled
	if e.config.Cookie.Enabled {
		e.initCookieManager()

		// Register cookie manager with DI container
		cookieManager := e.cookieManager
		if err := vessel.ProvideConstructor(app.Container(), func() *CookieManager {
			return cookieManager
		}, vessel.WithAliases(CookieManagerKey, CookieManagerKeyLegacy)); err != nil {
			return fmt.Errorf("failed to register cookie manager: %w", err)
		}
	}

	// Initialize all other security managers
	if err := e.initSecurityManagers(); err != nil {
		return fmt.Errorf("failed to initialize security managers: %w", err)
	}

	// Register all managers with DI container
	if err := e.registerWithDI(app); err != nil {
		return fmt.Errorf("failed to register with DI container: %w", err)
	}

	// Prepare auto-apply middlewares
	e.prepareAutoApplyMiddlewares()

	e.Logger().Info("security extension registered",
		forge.F("session_enabled", e.config.Session.Enabled),
		forge.F("session_store", e.config.Session.Store),
		forge.F("cookie_enabled", e.config.Cookie.Enabled),
		forge.F("csrf_enabled", e.config.CSRF.Enabled),
		forge.F("rate_limit_enabled", e.config.RateLimit.Enabled),
		forge.F("security_headers_enabled", e.config.SecurityHeaders.Enabled),
		forge.F("jwt_enabled", e.config.JWT.Enabled),
		forge.F("cors_enabled", e.config.CORS.Enabled),
		forge.F("api_key_enabled", e.config.APIKey.Enabled),
		forge.F("audit_enabled", e.config.Audit.Enabled),
	)

	return nil
}

// Start starts the security extension.
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.Logger().Info("starting security extension")

	// Start session store if enabled
	if e.config.Session.Enabled && e.sessionStore != nil {
		if err := e.sessionStore.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect session store: %w", err)
		}

		e.Logger().Info("session store connected",
			forge.F("store", e.config.Session.Store),
		)
	}

	e.MarkStarted()
	e.Logger().Info("security extension started")

	return nil
}

// Stop stops the security extension.
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.Logger().Info("stopping security extension")

	// Stop session store if enabled
	if e.config.Session.Enabled && e.sessionStore != nil {
		if err := e.sessionStore.Disconnect(ctx); err != nil {
			e.Logger().Error("failed to disconnect session store",
				forge.F("error", err),
			)
		} else {
			e.Logger().Info("session store disconnected")
		}
	}

	e.MarkStopped()
	e.Logger().Info("security extension stopped")

	return nil
}

// Health checks if the security extension is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Check session store health if enabled
	if e.config.Session.Enabled {
		if e.sessionStore == nil {
			return errors.New("session store not initialized")
		}

		if err := e.sessionStore.Ping(ctx); err != nil {
			return fmt.Errorf("session store health check failed: %w", err)
		}
	}

	return nil
}

// initSessionStore initializes the session store based on configuration.
func (e *Extension) initSessionStore() error {
	switch e.config.Session.Store {
	case "inmemory":
		e.sessionStore = NewInMemorySessionStore(e.Logger(), e.Metrics())
		e.Logger().Debug("initialized in-memory session store")

	case "redis":
		// TODO: Implement Redis session store
		return errors.New("redis session store not yet implemented")

	default:
		return fmt.Errorf("unknown session store: %s", e.config.Session.Store)
	}

	return nil
}

// initCookieManager initializes the cookie manager.
func (e *Extension) initCookieManager() {
	opts := CookieOptions{
		Path:     e.config.Cookie.Path,
		Domain:   e.config.Cookie.Domain,
		MaxAge:   e.config.Cookie.MaxAge,
		Secure:   e.config.Cookie.Secure,
		HttpOnly: e.config.Cookie.HttpOnly,
		SameSite: SameSiteMode(e.config.Cookie.SameSite),
	}

	e.cookieManager = NewCookieManager(opts)
	e.Logger().Debug("initialized cookie manager",
		forge.F("secure", opts.Secure),
		forge.F("http_only", opts.HttpOnly),
		forge.F("same_site", opts.SameSite),
	)
}

// SessionStore returns the session store (for advanced usage).
func (e *Extension) SessionStore() SessionStore {
	return e.sessionStore
}

// CookieManager returns the cookie manager (for advanced usage).
func (e *Extension) CookieManager() *CookieManager {
	return e.cookieManager
}

// Middlewares returns global middlewares for session management
// This implements the forge.MiddlewareExtension interface.
func (e *Extension) Middlewares() []forge.Middleware {
	return e.middlewares
}

// prepareSessionMiddleware prepares the session middleware for global application.
func (e *Extension) prepareSessionMiddleware() {
	if !e.config.Cookie.Enabled {
		e.Logger().Warn("cookie management is disabled, session middleware will not be applied")

		return
	}

	sessionMw := SessionMiddleware(SessionMiddlewareOptions{
		Store:         e.sessionStore,
		CookieManager: e.cookieManager,
		Config:        e.config.Session,
		Logger:        e.Logger(),
		Metrics:       e.Metrics(),
		SkipPaths:     e.config.Session.SkipPaths,
	})

	e.middlewares = append(e.middlewares, sessionMw)

	e.Logger().Debug("session middleware prepared for global application",
		forge.F("cookie_name", e.config.Session.CookieName),
		forge.F("skip_paths", e.config.Session.SkipPaths),
	)
}

// prepareAutoApplyMiddlewares prepares all auto-apply middlewares for global application.
func (e *Extension) prepareAutoApplyMiddlewares() {
	// CSRF middleware
	if e.config.CSRF.Enabled && e.config.CSRF.AutoApplyMiddleware && e.csrfProtection != nil {
		e.middlewares = append(e.middlewares, CSRFMiddleware(e.csrfProtection, e.cookieManager))
		e.Logger().Debug("CSRF middleware prepared for global application",
			forge.F("skip_paths", e.config.CSRF.SkipPaths),
		)
	}

	// Rate limiting middleware
	if e.config.RateLimit.Enabled && e.config.RateLimit.AutoApplyMiddleware && e.rateLimiter != nil {
		e.middlewares = append(e.middlewares, RateLimitMiddleware(e.rateLimiter))
		e.Logger().Debug("rate limiting middleware prepared for global application",
			forge.F("skip_paths", e.config.RateLimit.SkipPaths),
		)
	}

	// Security headers middleware
	if e.config.SecurityHeaders.Enabled && e.config.SecurityHeaders.AutoApplyMiddleware && e.securityHeaders != nil {
		e.middlewares = append(e.middlewares, SecurityHeadersMiddleware(e.securityHeaders))
		e.Logger().Debug("security headers middleware prepared for global application",
			forge.F("skip_paths", e.config.SecurityHeaders.SkipPaths),
		)
	}

	// JWT middleware
	if e.config.JWT.Enabled && e.config.JWT.AutoApplyMiddleware && e.jwtManager != nil {
		e.middlewares = append(e.middlewares, JWTMiddleware(e.jwtManager))
		e.Logger().Debug("JWT middleware prepared for global application",
			forge.F("skip_paths", e.config.JWT.SkipPaths),
		)
	}

	// CORS middleware
	if e.config.CORS.Enabled && e.config.CORS.AutoApplyMiddleware && e.corsManager != nil {
		e.middlewares = append(e.middlewares, CORSMiddleware(e.corsManager))
		e.Logger().Debug("CORS middleware prepared for global application",
			forge.F("skip_paths", e.config.CORS.SkipPaths),
		)
	}

	// API Key middleware
	if e.config.APIKey.Enabled && e.config.APIKey.AutoApplyMiddleware && e.apiKeyManager != nil {
		e.middlewares = append(e.middlewares, APIKeyMiddleware(e.apiKeyManager))
		e.Logger().Debug("API Key middleware prepared for global application",
			forge.F("skip_paths", e.config.APIKey.SkipPaths),
		)
	}

	// Audit middleware
	if e.config.Audit.Enabled && e.config.Audit.AutoApplyMiddleware && e.auditLogger != nil {
		e.middlewares = append(e.middlewares, AuditMiddleware(e.auditLogger))
		e.Logger().Debug("audit middleware prepared for global application",
			forge.F("exclude_paths", e.config.Audit.ExcludePaths),
		)
	}
}

// initSecurityManagers initializes all security managers.
func (e *Extension) initSecurityManagers() error {
	// Initialize CSRF protection
	if e.config.CSRF.Enabled {
		e.csrfProtection = NewCSRFProtection(e.config.CSRF, e.Logger())
		e.Logger().Debug("initialized CSRF protection")
	}

	// Initialize rate limiter
	if e.config.RateLimit.Enabled {
		e.rateLimiter = NewMemoryRateLimiter(e.config.RateLimit, e.Logger(), e.Metrics())
		e.Logger().Debug("initialized rate limiter",
			forge.F("requests_per_window", e.config.RateLimit.RequestsPerWindow),
			forge.F("window", e.config.RateLimit.Window),
		)
	}

	// Initialize security headers
	if e.config.SecurityHeaders.Enabled {
		e.securityHeaders = NewSecurityHeadersManager(e.config.SecurityHeaders, e.Logger())
		e.Logger().Debug("initialized security headers")
	}

	// Initialize password hasher
	e.passwordHasher = NewPasswordHasher(e.config.PasswordHasher)
	e.Logger().Debug("initialized password hasher",
		forge.F("algorithm", e.config.PasswordHasher.Algorithm),
	)

	// Initialize JWT manager
	if e.config.JWT.Enabled {
		jwtManager, err := NewJWTManager(e.config.JWT, e.Logger())
		if err != nil {
			return fmt.Errorf("failed to initialize JWT manager: %w", err)
		}

		e.jwtManager = jwtManager
		e.Logger().Debug("initialized JWT manager",
			forge.F("signing_method", e.config.JWT.SigningMethod),
		)
	}

	// Initialize CORS manager
	if e.config.CORS.Enabled {
		e.corsManager = NewCORSManager(e.config.CORS, e.Logger())
		e.Logger().Debug("initialized CORS manager",
			forge.F("allow_origins", e.config.CORS.AllowOrigins),
		)
	}

	// Initialize API key manager
	if e.config.APIKey.Enabled {
		e.apiKeyManager = NewAPIKeyManager(e.config.APIKey, e.Logger())
		e.Logger().Debug("initialized API key manager")
	}

	// Initialize audit logger
	if e.config.Audit.Enabled {
		e.auditLogger = NewAuditLogger(e.config.Audit, e.Logger())
		e.Logger().Debug("initialized audit logger",
			forge.F("level", e.config.Audit.Level),
		)
	}

	return nil
}

// registerWithDI registers all managers with the DI container using vessel.ProvideConstructor.
func (e *Extension) registerWithDI(app forge.App) error {
	container := app.Container()

	// Register CSRF protection
	if e.csrfProtection != nil {
		csrf := e.csrfProtection
		if err := vessel.ProvideConstructor(container, func() *CSRFProtection {
			return csrf
		}, vessel.WithAliases(CSRFProtectionKey)); err != nil {
			return fmt.Errorf("failed to register CSRF protection: %w", err)
		}
	}

	// Register rate limiter
	if e.rateLimiter != nil {
		limiter := e.rateLimiter
		if err := vessel.ProvideConstructor(container, func() *MemoryRateLimiter {
			return limiter
		}, vessel.WithAliases(RateLimiterKey)); err != nil {
			return fmt.Errorf("failed to register rate limiter: %w", err)
		}
	}

	// Register security headers manager
	if e.securityHeaders != nil {
		headers := e.securityHeaders
		if err := vessel.ProvideConstructor(container, func() *SecurityHeadersManager {
			return headers
		}, vessel.WithAliases(SecurityHeadersKey)); err != nil {
			return fmt.Errorf("failed to register security headers manager: %w", err)
		}
	}

	// Register password hasher
	if e.passwordHasher != nil {
		hasher := e.passwordHasher
		if err := vessel.ProvideConstructor(container, func() *PasswordHasher {
			return hasher
		}, vessel.WithAliases(PasswordHasherKey)); err != nil {
			return fmt.Errorf("failed to register password hasher: %w", err)
		}
	}

	// Register JWT manager
	if e.jwtManager != nil {
		jwt := e.jwtManager
		if err := vessel.ProvideConstructor(container, func() *JWTManager {
			return jwt
		}, vessel.WithAliases(JWTManagerKey)); err != nil {
			return fmt.Errorf("failed to register JWT manager: %w", err)
		}
	}

	// Register CORS manager
	if e.corsManager != nil {
		cors := e.corsManager
		if err := vessel.ProvideConstructor(container, func() *CORSManager {
			return cors
		}, vessel.WithAliases(CORSManagerKey)); err != nil {
			return fmt.Errorf("failed to register CORS manager: %w", err)
		}
	}

	// Register API key manager
	if e.apiKeyManager != nil {
		apiKey := e.apiKeyManager
		if err := vessel.ProvideConstructor(container, func() *APIKeyManager {
			return apiKey
		}, vessel.WithAliases(APIKeyManagerKey)); err != nil {
			return fmt.Errorf("failed to register API key manager: %w", err)
		}
	}

	// Register audit logger
	if e.auditLogger != nil {
		audit := e.auditLogger
		if err := vessel.ProvideConstructor(container, func() *AuditLogger {
			return audit
		}, vessel.WithAliases(AuditLoggerKey)); err != nil {
			return fmt.Errorf("failed to register audit logger: %w", err)
		}
	}

	return nil
}

// Accessor methods for all managers

// CSRFProtection returns the CSRF protection instance.
func (e *Extension) CSRFProtection() *CSRFProtection {
	return e.csrfProtection
}

// RateLimiter returns the rate limiter instance.
func (e *Extension) RateLimiter() *MemoryRateLimiter {
	return e.rateLimiter
}

// SecurityHeadersManager returns the security headers manager.
func (e *Extension) SecurityHeadersManager() *SecurityHeadersManager {
	return e.securityHeaders
}

// PasswordHasher returns the password hasher instance.
func (e *Extension) PasswordHasher() *PasswordHasher {
	return e.passwordHasher
}

// JWTManager returns the JWT manager instance.
func (e *Extension) JWTManager() *JWTManager {
	return e.jwtManager
}

// CORSManager returns the CORS manager instance.
func (e *Extension) CORSManager() *CORSManager {
	return e.corsManager
}

// APIKeyManager returns the API key manager instance.
func (e *Extension) APIKeyManager() *APIKeyManager {
	return e.apiKeyManager
}

// AuditLogger returns the audit logger instance.
func (e *Extension) AuditLogger() *AuditLogger {
	return e.auditLogger
}
