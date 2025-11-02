# Security Extension - Auto-Apply Middleware Example

This example demonstrates how to use the security extension with **automatic global middleware application**. The session middleware is automatically applied to all routes without needing to manually add it to each route or group.

## Key Features

- ✅ **Automatic middleware application** - No manual middleware setup required
- ✅ **Path exclusion** - Skip session handling for specific paths (e.g., `/health`, `/public`)
- ✅ **Zero boilerplate** - Session automatically available in all handlers
- ✅ **Production-ready** - Secure defaults with full configurability

## Difference from Manual Middleware

### Traditional Approach (Manual)
```go
// Manual middleware application - verbose and error-prone
sessionMw := security.SessionMiddleware(...)
app.Router().Use(sessionMw)
// OR
protectedRouter := app.Router().Group("/api")
protectedRouter.Use(sessionMw)
```

### Auto-Apply Approach (This Example)
```go
// Automatic middleware application - clean and simple
securityExt := security.NewExtension(
    security.WithAutoApplyMiddleware(true),
    security.WithSkipPaths([]string{"/health", "/public"}),
)
app.RegisterExtension(securityExt)
// Done! Middleware is automatically applied to all routes
```

## Running the Example

```bash
cd examples/security-auto-middleware
go run main.go
```

The server will start on `http://localhost:8080`.

## How It Works

1. **Extension Registration**: The security extension implements `forge.MiddlewareExtension` interface
2. **Middleware Preparation**: During `Register()`, the extension prepares middleware if `AutoApplyMiddleware` is enabled
3. **Automatic Application**: During `app.Start()`, Forge automatically applies middleware from all extensions that implement `MiddlewareExtension`
4. **Execution**: All routes automatically have session middleware applied (except skip paths)

## API Endpoints

### Public Endpoints (No Session Required)

#### GET /public
Public endpoint that doesn't require authentication. Session middleware is skipped.

```bash
curl http://localhost:8080/public
```

Response:
```json
{
  "message": "This is a public endpoint",
  "has_session": false
}
```

#### GET /health
Health check endpoint. Session middleware is skipped.

```bash
curl http://localhost:8080/health
```

### Authentication

#### POST /login
Create a session by logging in.

```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "password"}' \
  -c cookies.txt
```

### Automatic Session-Protected Endpoints

These endpoints automatically have session middleware applied. If no session exists, they'll return 401.

#### GET /profile
Get user profile from session.

```bash
# With session
curl http://localhost:8080/profile -b cookies.txt

# Without session (will fail)
curl http://localhost:8080/profile
```

#### GET /dashboard
Access dashboard (requires session).

```bash
curl http://localhost:8080/dashboard -b cookies.txt
```

#### POST /profile
Update profile data.

```bash
curl -X POST http://localhost:8080/profile \
  -H "Content-Type: application/json" \
  -d '{"bio": "Software Engineer", "location": "San Francisco"}' \
  -b cookies.txt
```

#### GET /settings
Access settings (requires session).

```bash
curl http://localhost:8080/settings -b cookies.txt
```

#### POST /logout
Logout and destroy session.

```bash
curl -X POST http://localhost:8080/logout -b cookies.txt
```

## Complete Testing Flow

```bash
# 1. Check public endpoint (no login required)
curl http://localhost:8080/public

# 2. Try to access protected endpoint without login (should fail)
curl http://localhost:8080/profile
# Response: {"error":"no session - please login first"}

# 3. Login and save cookies
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "password"}' \
  -c cookies.txt -v

# 4. Access protected endpoints with session
curl http://localhost:8080/profile -b cookies.txt
curl http://localhost:8080/dashboard -b cookies.txt
curl http://localhost:8080/settings -b cookies.txt

# 5. Update profile
curl -X POST http://localhost:8080/profile \
  -H "Content-Type: application/json" \
  -d '{"bio": "DevOps Engineer"}' \
  -b cookies.txt

# 6. Verify update
curl http://localhost:8080/profile -b cookies.txt

# 7. Logout
curl -X POST http://localhost:8080/logout -b cookies.txt

# 8. Try to access protected endpoint after logout (should fail)
curl http://localhost:8080/profile -b cookies.txt
```

## Configuration

### Auto-Apply Middleware Configuration

```go
securityExt := security.NewExtension(
    // Enable automatic middleware application
    security.WithAutoApplyMiddleware(true),
    
    // Paths to skip (no session handling)
    security.WithSkipPaths([]string{
        "/health",
        "/metrics", 
        "/public",
        "/docs",
    }),
    
    // Session configuration
    security.WithSessionStore("inmemory"),
    security.WithSessionTTL(24 * time.Hour),
    security.WithSessionAutoRenew(true),
    
    // Cookie security
    security.WithCookieSecure(true),  // Requires HTTPS
    security.WithCookieHttpOnly(true),
    security.WithCookieSameSite("lax"),
)
```

### File-Based Configuration

```yaml
# config.yaml
extensions:
  security:
    enabled: true
    
    session:
      enabled: true
      store: inmemory
      cookie_name: auto_session
      ttl: 24h
      idle_timeout: 30m
      auto_renew: true
      
      # Enable automatic middleware
      auto_apply_middleware: true
      
      # Skip these paths
      skip_paths:
        - /health
        - /metrics
        - /public
    
    cookie:
      enabled: true
      secure: true
      http_only: true
      same_site: lax
      path: /
```

## Benefits of Auto-Apply

### 1. **Less Boilerplate**
No need to manually register middleware on every route group:
```go
// Before
router.Use(sessionMw)
apiRouter := router.Group("/api")
apiRouter.Use(sessionMw)
adminRouter := router.Group("/admin")
adminRouter.Use(sessionMw)

// After  
// Just enable auto-apply - done!
```

### 2. **Consistency**
All routes automatically have the same middleware applied, reducing the risk of forgetting to protect a route.

### 3. **Centralized Configuration**
Session behavior is configured in one place (extension config), not scattered across route definitions.

### 4. **Easy Testing**
Skip paths make it easy to exclude health checks and test endpoints from session requirements.

### 5. **Production Ready**
Follows security best practices by defaulting to protected routes with explicit public path exclusions.

## When to Use Auto-Apply vs Manual

### Use Auto-Apply When:
- ✅ Most routes require session handling
- ✅ You want consistent security across all routes
- ✅ You prefer configuration over code
- ✅ You have a few public/health endpoints to exclude

### Use Manual Middleware When:
- ✅ Only a few routes need sessions
- ✅ You have complex middleware chains
- ✅ You need fine-grained control per route
- ✅ You want explicit visibility in route definitions

## Combining with Other Extensions

Auto-apply middleware works seamlessly with other extensions:

```go
// Multiple extensions can provide middleware
authExt := auth.NewExtension(
    auth.WithAutoApplyMiddleware(true),
)

securityExt := security.NewExtension(
    security.WithAutoApplyMiddleware(true),
)

rateLimitExt := ratelimit.NewExtension(
    ratelimit.WithAutoApplyMiddleware(true),
)

// All middleware is applied in registration order
app.RegisterExtension(authExt)
app.RegisterExtension(securityExt)
app.RegisterExtension(rateLimitExt)
```

## Custom Extension with Auto-Apply

You can create your own extensions with auto-apply middleware:

```go
type MyExtension struct {
    *forge.BaseExtension
    middlewares []forge.Middleware
}

// Implement MiddlewareExtension interface
func (e *MyExtension) Middlewares() []forge.Middleware {
    return e.middlewares
}

func (e *MyExtension) Register(app forge.App) error {
    // Prepare middleware
    mw := func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Your middleware logic
            next.ServeHTTP(w, r)
        })
    }
    
    e.middlewares = append(e.middlewares, mw)
    return e.BaseExtension.Register(app)
}
```

## Security Considerations

### 1. **Skip Paths Carefully**
Only exclude paths that genuinely don't need session protection:
```go
security.WithSkipPaths([]string{
    "/health",      // ✅ Health check
    "/metrics",     // ✅ Metrics endpoint
    "/public",      // ✅ Public content
    "/docs",        // ✅ API documentation
    // ❌ Don't skip /admin or /api without good reason
})
```

### 2. **HTTPS in Production**
Always use secure cookies in production:
```go
security.WithCookieSecure(true), // Requires HTTPS
```

### 3. **Session Expiration**
Set appropriate TTL and idle timeout:
```go
security.WithSessionTTL(24 * time.Hour),
security.WithSessionIdleTimeout(30 * time.Minute),
```

### 4. **Monitor Sessions**
Watch session metrics for suspicious activity:
- `security.sessions.created`
- `security.sessions.expired`
- `security.sessions.active`

## Further Reading

- [Security Extension Documentation](../../extensions/security/README.md)
- [MiddlewareExtension Interface](../../extension.go)
- [Session Management Best Practices](../../extensions/security/README.md#security-best-practices)
- [OWASP Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)

