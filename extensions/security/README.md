# Security Extension v2.0.0

The security extension provides comprehensive production-ready security features for Forge applications including session management, CSRF protection, rate limiting, password hashing, JWT authentication, CORS, API keys, audit logging, and security headers.

> **📘 New in v2.0.0**: Added CSRF protection, rate limiting, security headers, password hashing (Argon2id/bcrypt), JWT authentication, CORS, API key authentication, and audit logging. See [SECURITY_FEATURES.md](SECURITY_FEATURES.md) for details.

## Features

### Core Security Features (v1.x)

- **Session Management**
  - Cryptographically secure session ID generation (256-bit)
  - Multiple backend support (in-memory, Redis planned)
  - Automatic session expiration and cleanup
  - Session renewal and idle timeout
  - Multi-session management per user
  - Optional IP address and user agent tracking

- **Cookie Management**
  - Secure cookie handling with security flags
  - HttpOnly (XSS prevention)
  - Secure flag (HTTPS-only)
  - SameSite attribute (CSRF protection)
  - Domain and path scoping
  - Flexible expiration control

- **Middleware**
  - Automatic session loading from cookies
  - Session context injection
  - Protected route support
  - Path exclusion support
  - Metrics and logging integration

### New Security Features (v2.0.0)

- **🛡️ CSRF Protection** - Token-based CSRF protection with multiple token lookup methods
- **⏱️ Rate Limiting** - Token bucket rate limiting (per-IP, per-user, custom keys)
- **🔒 Security Headers** - Comprehensive HTTP security headers (CSP, HSTS, X-Frame-Options, etc.)
- **🔐 Password Hashing** - Argon2id and bcrypt with strength checking and validation
- **🎫 JWT Authentication** - Full JWT support with multiple signing algorithms
- **🌐 CORS** - Cross-Origin Resource Sharing with wildcard and preflight support
- **🔑 API Key Management** - Cryptographic API key generation, validation, and scope management
- **📝 Audit Logging** - Security event logging with sensitive data redaction

**See [SECURITY_FEATURES.md](SECURITY_FEATURES.md) for complete documentation of new features.**

## Installation

The security extension is included with Forge. No additional installation required.

## Quick Start

```go
package main

import (
    "context"
    "net/http"
    "time"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/security"
)

func main() {
    app := forge.New()
    
    // Register security extension
    securityExt := security.NewExtension(
        security.WithSessionStore("inmemory"),
        security.WithSessionTTL(24 * time.Hour),
        security.WithCookieSecure(true),
        security.WithCookieHttpOnly(true),
    )
    
    app.RegisterExtension(securityExt)
    app.Start(context.Background())
    
    // Get services from DI container
    var sessionStore security.SessionStore
    var cookieManager *security.CookieManager
    
    app.Container().Resolve("security.SessionStore", &sessionStore)
    app.Container().Resolve("security.CookieManager", &cookieManager)
    
    // Create session middleware
    sessionMw := security.SessionMiddleware(security.SessionMiddlewareOptions{
        Store:         sessionStore,
        CookieManager: cookieManager,
        Config: security.SessionConfig{
            CookieName: "forge_session",
            TTL:        24 * time.Hour,
            AutoRenew:  true,
        },
        Logger:  app.Logger(),
        Metrics: app.Metrics(),
    })
    
    // Apply middleware
    app.Router().Use(sessionMw)
    
    // Define routes
    app.Router().POST("/login", loginHandler(sessionStore, cookieManager))
    app.Router().GET("/profile", profileHandler)
    app.Router().POST("/logout", logoutHandler(sessionStore, cookieManager))
    
    app.ListenAndServe(":8080")
}

func loginHandler(store security.SessionStore, cm *security.CookieManager) forge.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        // Authenticate user...
        
        // Create session
        session, err := security.CreateSession(
            r.Context(),
            w,
            "user123",
            store,
            cm,
            security.SessionConfig{
                CookieName: "forge_session",
                TTL:        24 * time.Hour,
            },
            map[string]interface{}{
                "username": "admin",
                "role":     "admin",
            },
        )
        
        if err != nil {
            return err
        }
        
        return forge.JSON(w, 200, map[string]string{
            "session_id": session.ID,
        })
    }
}

func profileHandler(w http.ResponseWriter, r *http.Request) error {
    session, ok := security.GetSession(r.Context())
    if !ok {
        return forge.JSON(w, 401, map[string]string{"error": "unauthorized"})
    }
    
    return forge.JSON(w, 200, map[string]interface{}{
        "user_id": session.UserID,
        "data":    session.Data,
    })
}

func logoutHandler(store security.SessionStore, cm *security.CookieManager) forge.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        if err := security.DestroySession(r.Context(), w, store, cm, "forge_session"); err != nil {
            return err
        }
        
        return forge.JSON(w, 200, map[string]string{"message": "logged out"})
    }
}
```

## Configuration

### Programmatic Configuration

```go
security.NewExtension(
    // General
    security.WithEnabled(true),
    
    // Session
    security.WithSessionEnabled(true),
    security.WithSessionStore("inmemory"),
    security.WithSessionCookieName("forge_session"),
    security.WithSessionTTL(24 * time.Hour),
    security.WithSessionIdleTimeout(30 * time.Minute),
    security.WithSessionAutoRenew(true),
    
    // Redis (when using Redis store)
    security.WithRedisAddress("redis://localhost:6379"),
    security.WithRedisPassword("secret"),
    security.WithRedisDB(0),
    
    // Cookie
    security.WithCookieEnabled(true),
    security.WithCookieSecure(true),
    security.WithCookieHttpOnly(true),
    security.WithCookieSameSite("lax"),
    security.WithCookiePath("/"),
    security.WithCookieDomain("example.com"),
)
```

### File-based Configuration

```yaml
# config.yaml
extensions:
  security:
    enabled: true
    
    session:
      enabled: true
      store: inmemory
      cookie_name: forge_session
      ttl: 24h
      idle_timeout: 30m
      auto_renew: true
      track_ip_address: false
      track_user_agent: false
      
      redis:
        address: redis://localhost:6379
        password: ""
        db: 0
        pool_size: 10
    
    cookie:
      enabled: true
      secure: true
      http_only: true
      same_site: lax
      path: /
      domain: ""
      max_age: 0
```

## Session Management

### Creating Sessions

```go
session, err := security.CreateSession(
    ctx,
    w,
    userID,
    sessionStore,
    cookieManager,
    security.SessionConfig{
        CookieName: "forge_session",
        TTL:        24 * time.Hour,
    },
    map[string]interface{}{
        "role": "admin",
        "email": "user@example.com",
    },
)
```

### Accessing Sessions

```go
// From middleware-injected context
session, ok := security.GetSession(r.Context())
if !ok {
    // No session found
    return
}

// Or panic if session is required
session := security.MustGetSession(r.Context())
```

### Updating Sessions

```go
session, _ := security.GetSession(r.Context())
session.Data["last_page"] = "/dashboard"

err := security.UpdateSession(r.Context(), sessionStore, 24 * time.Hour)
```

### Destroying Sessions

```go
// Destroy current session
err := security.DestroySession(ctx, w, sessionStore, cookieManager, "forge_session")

// Destroy all sessions for a user
err := sessionStore.DeleteByUserID(ctx, userID)
```

### Session Properties

```go
type Session struct {
    ID             string                 // Unique session identifier
    UserID         string                 // User identifier
    Data           map[string]interface{} // Custom session data
    CreatedAt      time.Time             // Creation timestamp
    ExpiresAt      time.Time             // Expiration timestamp
    LastAccessedAt time.Time             // Last access timestamp
    IPAddress      string                // Client IP (optional)
    UserAgent      string                // Client user agent (optional)
}
```

## Cookie Management

### Setting Cookies

```go
cookieManager.SetCookie(w, "theme", "dark", &security.CookieOptions{
    Path:     "/",
    MaxAge:   7 * 24 * 60 * 60, // 7 days
    Secure:   true,
    HttpOnly: true,
    SameSite: security.SameSiteLax,
})
```

### Getting Cookies

```go
value, err := cookieManager.GetCookie(r, "theme")
if err == security.ErrCookieNotFound {
    // Cookie doesn't exist
}
```

### Checking Cookie Existence

```go
if cookieManager.HasCookie(r, "theme") {
    // Cookie exists
}
```

### Deleting Cookies

```go
cookieManager.DeleteCookie(w, "theme", nil)
```

### Getting All Cookies

```go
cookies := cookieManager.GetAllCookies(r)
for _, cookie := range cookies {
    fmt.Println(cookie.Name, cookie.Value)
}
```

## Middleware

### Session Middleware

Automatically loads sessions from cookies and injects into request context:

```go
sessionMw := security.SessionMiddleware(security.SessionMiddlewareOptions{
    Store:         sessionStore,
    CookieManager: cookieManager,
    Config: security.SessionConfig{
        CookieName: "forge_session",
        TTL:        24 * time.Hour,
        AutoRenew:  true,
    },
    Logger:  app.Logger(),
    Metrics: app.Metrics(),
    
    // Optional callbacks
    OnSessionCreated: func(session *security.Session) {
        log.Printf("Session created: %s", session.ID)
    },
    OnSessionExpired: func(sessionID string) {
        log.Printf("Session expired: %s", sessionID)
    },
    
    // Skip paths
    SkipPaths: []string{"/health", "/public"},
})

app.Router().Use(sessionMw)
```

### Require Session Middleware

Protects routes by requiring a valid session:

```go
// With handler
unauthorizedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    forge.JSON(w, 401, map[string]string{"error": "unauthorized"})
})

protectedRouter := app.Router().Group("/api")
protectedRouter.Use(security.RequireSession(unauthorizedHandler))

// With handler function
protectedRouter.Use(security.RequireSessionFunc(func(w http.ResponseWriter, r *http.Request) {
    forge.JSON(w, 401, map[string]string{"error": "unauthorized"})
}))
```

## Security Best Practices

### Production Checklist

- [ ] Use HTTPS and set `Secure: true` for cookies
- [ ] Use Redis for session storage in distributed systems
- [ ] Set appropriate session TTL and idle timeout
- [ ] Enable `HttpOnly` to prevent XSS attacks
- [ ] Use `SameSite: Lax` or `Strict` for CSRF protection
- [ ] Implement rate limiting on login endpoints
- [ ] Rotate session IDs after privilege elevation
- [ ] Clear sessions on logout
- [ ] Monitor session metrics
- [ ] Set up session cleanup jobs for expired sessions

### Session Security

```go
security.NewExtension(
    // Enable IP tracking to detect session hijacking
    security.WithSessionTrackIPAddress(true),
    
    // Enable user agent tracking
    security.WithSessionTrackUserAgent(true),
    
    // Short TTL for sensitive operations
    security.WithSessionTTL(15 * time.Minute),
    
    // Idle timeout
    security.WithSessionIdleTimeout(5 * time.Minute),
    
    // Auto-renew on activity
    security.WithSessionAutoRenew(true),
)
```

### Cookie Security

```go
security.NewExtension(
    // Always use Secure in production (requires HTTPS)
    security.WithCookieSecure(true),
    
    // Prevent JavaScript access
    security.WithCookieHttpOnly(true),
    
    // CSRF protection (use 'strict' for sensitive apps)
    security.WithCookieSameSite("lax"),
    
    // Limit cookie scope
    security.WithCookieDomain("example.com"),
    security.WithCookiePath("/api"),
)
```

### Validation Example

```go
// Validate session security
func validateSession(r *http.Request, session *security.Session) error {
    // Check IP address if tracking is enabled
    if session.IPAddress != "" {
        clientIP := getClientIP(r)
        if session.IPAddress != clientIP {
            return errors.New("session hijacking detected: IP mismatch")
        }
    }
    
    // Check user agent if tracking is enabled
    if session.UserAgent != "" {
        clientUA := r.UserAgent()
        if session.UserAgent != clientUA {
            return errors.New("session hijacking detected: user agent mismatch")
        }
    }
    
    return nil
}
```

## Metrics

The extension provides comprehensive metrics:

- `security.sessions.created`: Total sessions created
- `security.sessions.retrieved`: Total sessions retrieved
- `security.sessions.updated`: Total sessions updated
- `security.sessions.deleted`: Total sessions deleted
- `security.sessions.touched`: Total sessions renewed
- `security.sessions.expired`: Total expired sessions
- `security.sessions.active`: Current active sessions (gauge)
- `security.sessions.cleaned_up`: Total sessions cleaned up
- `security.sessions.not_found`: Total session not found errors
- `security.sessions.accessed`: Total session accesses

## Error Handling

```go
_, err := sessionStore.Get(ctx, sessionID)
switch {
case errors.Is(err, security.ErrSessionNotFound):
    // Session doesn't exist
case errors.Is(err, security.ErrSessionExpired):
    // Session has expired
case errors.Is(err, security.ErrInvalidSession):
    // Session data is corrupted
default:
    // Other error
}

_, err = cookieManager.GetCookie(r, "name")
switch {
case errors.Is(err, security.ErrCookieNotFound):
    // Cookie doesn't exist
case errors.Is(err, security.ErrInvalidCookie):
    // Cookie data is invalid
default:
    // Other error
}
```

## Backend Implementations

### In-Memory Store

Best for development and single-instance deployments:

```go
security.NewExtension(
    security.WithSessionStore("inmemory"),
)
```

**Characteristics:**
- ✅ Fast access
- ✅ No external dependencies
- ✅ Automatic cleanup
- ❌ Not suitable for distributed systems
- ❌ Sessions lost on restart

### Redis Store (Planned)

Best for production and distributed systems:

```go
security.NewExtension(
    security.WithSessionStore("redis"),
    security.WithRedisAddress("redis://localhost:6379"),
    security.WithRedisPassword("secret"),
    security.WithRedisDB(0),
)
```

**Characteristics:**
- ✅ Distributed session sharing
- ✅ Persistent across restarts
- ✅ Automatic expiration (TTL)
- ✅ High performance
- ✅ Scalable

## Advanced Usage

### Custom Session Store

Implement the `SessionStore` interface:

```go
type CustomStore struct {
    // Your implementation
}

func (s *CustomStore) Create(ctx context.Context, session *Session, ttl time.Duration) error {
    // Implementation
}

func (s *CustomStore) Get(ctx context.Context, sessionID string) (*Session, error) {
    // Implementation
}

// ... implement other methods
```

### Session Events

```go
sessionMw := security.SessionMiddleware(security.SessionMiddlewareOptions{
    // ... other options
    
    OnSessionCreated: func(session *security.Session) {
        // Log to audit trail
        auditLog.Record("session.created", session.UserID, session.ID)
        
        // Send analytics event
        analytics.Track("session_start", session.UserID)
    },
    
    OnSessionExpired: func(sessionID string) {
        // Clean up resources
        cleanup.SessionExpired(sessionID)
        
        // Update metrics
        metrics.Increment("sessions.expired.total")
    },
})
```

### Multi-Tenancy

```go
// Store tenant ID in session
session.Data["tenant_id"] = tenantID

// Access in middleware
func tenantMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        session, ok := security.GetSession(r.Context())
        if !ok {
            http.Error(w, "Unauthorized", 401)
            return
        }
        
        tenantID, ok := session.Data["tenant_id"].(string)
        if !ok {
            http.Error(w, "Invalid session", 400)
            return
        }
        
        // Inject tenant into context
        ctx := context.WithValue(r.Context(), "tenant_id", tenantID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

## Testing

```go
func TestSessionMiddleware(t *testing.T) {
    // Create test components
    logger := logger.NewNoOpLogger()
    metrics := metrics.NewNoOpMetrics()
    store := security.NewInMemorySessionStore(logger, metrics)
    cookieManager := security.NewCookieManager(security.DefaultCookieOptions())
    
    // Create session
    session, _ := security.NewSession("user123", 1*time.Hour)
    store.Create(context.Background(), session, 1*time.Hour)
    
    // Create test request with session cookie
    req := httptest.NewRequest("GET", "/", nil)
    req.AddCookie(&http.Cookie{
        Name:  "forge_session",
        Value: session.ID,
    })
    
    // Create middleware
    mw := security.SessionMiddleware(security.SessionMiddlewareOptions{
        Store:         store,
        CookieManager: cookieManager,
        Config: security.SessionConfig{
            CookieName: "forge_session",
            TTL:        1 * time.Hour,
        },
        Logger:  logger,
        Metrics: metrics,
    })
    
    // Test handler
    handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        loadedSession, ok := security.GetSession(r.Context())
        assert.True(t, ok)
        assert.Equal(t, session.ID, loadedSession.ID)
    }))
    
    // Execute
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)
}
```

## Examples

See the [examples/security-example](../../examples/security-example) directory for a complete working example.

## FAQ

**Q: Should I use in-memory or Redis for sessions?**

A: Use in-memory for development and single-instance deployments. Use Redis for production multi-instance deployments.

**Q: How do I handle session fixation attacks?**

A: Regenerate session IDs after authentication or privilege escalation:
```go
// Delete old session
sessionStore.Delete(ctx, oldSessionID)

// Create new session
newSession, _ := security.CreateSession(ctx, w, userID, ...)
```

**Q: How do I implement "Remember Me" functionality?**

A: Use two cookies - a short-lived session cookie and a long-lived remember-me token:
```go
// Short-lived session
security.CreateSession(ctx, w, userID, store, cm, 
    SessionConfig{TTL: 2 * time.Hour}, nil)

// Long-lived remember token
cookieManager.SetCookie(w, "remember_token", token, &CookieOptions{
    MaxAge: 30 * 24 * 60 * 60, // 30 days
    Secure: true,
    HttpOnly: true,
})
```

**Q: How do I handle concurrent logins?**

A: Store multiple sessions per user or implement single-session enforcement:
```go
// Single session enforcement
sessionStore.DeleteByUserID(ctx, userID) // Delete existing sessions
security.CreateSession(ctx, w, userID, ...) // Create new session
```

## References

- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
- [MDN: Using HTTP cookies](https://developer.mozilla.org/en-US/docs/Web/HTTP/Cookies)
- [RFC 6265: HTTP State Management Mechanism](https://tools.ietf.org/html/rfc6265)

