# Forge Framework - Complete Middleware Package

## Package Structure

The middleware package provides a comprehensive set of HTTP middleware components for the Forge framework:

```
middleware/
├── stack.go          # Middleware stack management (already implemented)
├── logging.go        # Request/response logging middleware
├── recovery.go       # Panic recovery middleware
├── cors.go           # CORS handling middleware
├── auth.go           # Authentication middleware
└── ratelimit.go      # Rate limiting middleware
```

## Key Features

### 1. Middleware Stack Management (stack.go)
- **Flexible middleware composition** with priority-based ordering
- **Conditional middleware** execution based on request properties
- **Middleware grouping** and named middleware sets
- **Performance monitoring** with built-in statistics
- **Health checks** for individual middleware components
- **Configuration management** with hot-reload support

### 2. Request Logging (logging.go)
- **Structured logging** with configurable fields
- **Request/response tracking** with timing information
- **Header and body logging** with size limits
- **Path-based filtering** (skip health checks, static files)
- **Multiple output formats** (JSON, console, access log formats)
- **Custom formatters** for specialized logging needs

### 3. Panic Recovery (recovery.go)
- **Graceful panic recovery** with stack trace capture
- **Multiple response formats** (JSON, HTML, plain text)
- **Development vs production** configurations
- **Stack trace filtering** to remove noise
- **Notification system** for panic alerts
- **Request context capture** for debugging

### 4. CORS Support (cors.go)
- **Flexible origin matching** including wildcard support
- **Preflight request handling** with proper validation
- **Configurable headers and methods**
- **Credential support** with security considerations
- **Cache control** with max-age settings
- **Validation utilities** for configuration

### 5. Authentication (auth.go)
- **Multiple authentication methods**: JWT, API keys, Basic Auth, Sessions
- **User loading** from various sources
- **Role-based access control** (RBAC)
- **Permission-based authorization**
- **Session management** with configurable storage
- **Token validation** and refresh mechanisms

### 6. Rate Limiting (ratelimit.go)
- **Multiple limiting strategies**: IP, User, API Key, Global
- **Token bucket algorithm** with configurable refill rates
- **Distributed rate limiting** with Redis support
- **Flexible key generation** for custom strategies
- **Proper HTTP headers** for client feedback
- **Burst handling** for traffic spikes

## Usage Examples

### Basic Middleware Stack
```go
// Create a new middleware stack
stack := middleware.NewStack(middleware.DefaultStackConfig())

// Add middleware with priorities
stack.UseWithPriority(100, middleware.NewRequestIDMiddleware())
stack.UseWithPriority(200, middleware.NewLoggingMiddleware(logger))
stack.UseWithPriority(300, middleware.NewRecoveryMiddleware(logger))
stack.UseWithPriority(400, middleware.NewCORSMiddleware(middleware.DefaultCORSConfig()))

// Create HTTP handler
handler := stack.Handler(http.HandlerFunc(myHandler))
```

### Development Stack
```go
// Create development-optimized stack
stack := middleware.DefaultStack(logger)

// Add development-specific middleware
stack.UseWithPriority(250, middleware.NewRecoveryMiddleware(logger, middleware.DevelopmentRecoveryConfig()))
stack.UseWithPriority(350, middleware.NewLoggingMiddleware(logger, middleware.DefaultLoggingConfig()))
```

### Production Stack
```go
// Create production-optimized stack
config := middleware.ProductionStackConfig()
stack := middleware.NewStack(config)

// Add production middleware
stack.UseWithPriority(100, middleware.NewRequestIDMiddleware())
stack.UseWithPriority(200, middleware.NewLoggingMiddleware(logger))
stack.UseWithPriority(300, middleware.NewRecoveryMiddleware(logger, middleware.ProductionRecoveryConfig()))
stack.UseWithPriority(400, middleware.NewCORSMiddleware(middleware.RestrictiveCORSConfig()))
stack.UseWithPriority(500, middleware.NewAuthMiddleware(authConfig))
stack.UseWithPriority(600, middleware.NewRateLimitMiddleware(rateLimitConfig, rateLimiter))
```

### Conditional Middleware
```go
// Add middleware only for API paths
stack.UseForPath("/api/*", middleware.NewAuthMiddleware(authConfig))

// Add middleware only for specific methods
stack.UseForMethod("POST", middleware.NewRateLimitMiddleware(strictConfig, limiter))

// Add middleware with custom conditions
stack.UseIf(func(r *http.Request) bool {
    return strings.HasPrefix(r.URL.Path, "/admin")
}, middleware.NewAuthMiddleware(adminAuthConfig))
```

### Authentication Examples
```go
// JWT Authentication
jwtConfig := middleware.AuthConfig{
    Methods: []middleware.AuthMethod{middleware.AuthMethodJWT},
    JWT: middleware.JWTConfig{
        Secret:         "your-secret-key",
        ExpirationTime: 24 * time.Hour,
        HeaderName:     "Authorization",
    },
}

// API Key Authentication
apiKeyConfig := middleware.AuthConfig{
    Methods: []middleware.AuthMethod{middleware.AuthMethodAPIKey},
    APIKey: middleware.APIKeyConfig{
        HeaderName: "X-API-Key",
        Keys: map[string]string{
            "key1": "user1",
            "key2": "user2",
        },
    },
}

// Multi-method Authentication
multiConfig := middleware.AuthConfig{
    Methods: []middleware.AuthMethod{
        middleware.AuthMethodJWT,
        middleware.AuthMethodAPIKey,
        middleware.AuthMethodBasicAuth,
    },
    // Configure each method...
}
```

### Rate Limiting Examples
```go
// IP-based rate limiting
ipConfig := middleware.RateLimitConfig{
    RequestsPerSecond: 100,
    BurstSize:         200,
    Strategy:          middleware.RateLimitByIP,
}

// API Key-based rate limiting
apiConfig := middleware.RateLimitConfig{
    RequestsPerSecond: 1000,
    BurstSize:         2000,
    Strategy:          middleware.RateLimitByAPIKey,
}

// Custom key generation
customConfig := middleware.RateLimitConfig{
    RequestsPerSecond: 50,
    BurstSize:         100,
    Strategy:          middleware.RateLimitByHeader,
    KeyGenerator: func(r *http.Request) string {
        return "tenant:" + r.Header.Get("X-Tenant-ID")
    },
}
```

## Configuration

### Environment Variables
```bash
# Logging configuration
FORGE_LOG_LEVEL=info
FORGE_LOG_FORMAT=json
FORGE_LOG_SKIP_PATHS=/health,/metrics

# CORS configuration
FORGE_CORS_ALLOWED_ORIGINS=*
FORGE_CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE
FORGE_CORS_ALLOW_CREDENTIALS=true

# Rate limiting
FORGE_RATE_LIMIT_REQUESTS_PER_SECOND=100
FORGE_RATE_LIMIT_BURST_SIZE=200
FORGE_RATE_LIMIT_STRATEGY=ip

# Authentication
FORGE_AUTH_JWT_SECRET=your-secret-key
FORGE_AUTH_JWT_EXPIRATION=24h
FORGE_AUTH_METHODS=jwt,api_key
```

### Configuration Files
```yaml
# config.yaml
middleware:
  logging:
    level: info
    format: json
    skip_paths:
      - /health
      - /metrics
  cors:
    allowed_origins:
      - "https://example.com"
      - "https://app.example.com"
    allowed_methods:
      - GET
      - POST
      - PUT
      - DELETE
    allow_credentials: true
  rate_limit:
    requests_per_second: 100
    burst_size: 200
    strategy: ip
  auth:
    methods:
      - jwt
      - api_key
    jwt:
      secret: your-secret-key
      expiration: 24h
```

## Testing

### Middleware Testing
```go
// Test middleware in isolation
func TestLoggingMiddleware(t *testing.T) {
    logger := logger.NewTestLogger()
    middleware := middleware.NewLoggingMiddleware(logger)
    
    // Test request handling
    req := httptest.NewRequest("GET", "/test", nil)
    rec := httptest.NewRecorder()
    
    handler := middleware.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    
    handler.ServeHTTP(rec, req)
    
    assert.Equal(t, http.StatusOK, rec.Code)
    assert.True(t, logger.HasLoggedLevel(logger.InfoLevel))
}
```

### Integration Testing
```go
// Test middleware stack
func TestMiddlewareStack(t *testing.T) {
    stack := middleware.NewStack(middleware.DefaultStackConfig())
    stack.Use(middleware.NewLoggingMiddleware(logger))
    stack.Use(middleware.NewRecoveryMiddleware(logger))
    
    handler := stack.Handler(http.HandlerFunc(testHandler))
    
    // Test normal request
    req := httptest.NewRequest("GET", "/test", nil)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    
    assert.Equal(t, http.StatusOK, rec.Code)
}
```

## Performance Considerations

### Middleware Ordering
- **Request ID** generation should be first (priority 100)
- **Logging** should be early but after request ID (priority 200)
- **Recovery** should be early to catch all panics (priority 300)
- **CORS** should be before authentication (priority 400)
- **Authentication** should be after CORS (priority 700)
- **Rate limiting** should be after authentication (priority 800)

### Memory Usage
- **Connection pooling** for database-backed middleware
- **Cache expiration** for user data and session storage
- **Cleanup routines** for rate limiting buckets
- **Configurable limits** for request/response body logging

### Concurrency
- **Thread-safe implementations** for all middleware components
- **Efficient locking** with RWMutex where appropriate
- **Lock-free operations** for hot path code
- **Goroutine pools** for intensive operations

## Security Considerations

### Input Validation
- **Header validation** for all input sources
- **Size limits** for request bodies and headers
- **Sanitization** of logged data
- **Rate limiting** to prevent abuse

### Authentication Security
- **Secure token storage** with proper encryption
- **Session timeout** and rotation
- **Brute force protection** with rate limiting
- **CSRF protection** for session-based auth

### CORS Security
- **Origin validation** with exact matching
- **Credential restrictions** with wildcard origins
- **Header whitelisting** for security
- **Method restrictions** based on endpoints

## Best Practices

1. **Always use Request ID** middleware for request tracing
2. **Configure logging** appropriately for each environment
3. **Use recovery middleware** to prevent application crashes
4. **Implement proper CORS** policies for web applications
5. **Apply rate limiting** to prevent abuse
6. **Use authentication** for protected endpoints
7. **Monitor middleware performance** with metrics
8. **Test middleware** in isolation and integration
9. **Configure timeouts** appropriately
10. **Use environment-specific** configurations

## Integration with Forge Framework

The middleware package integrates seamlessly with the Forge framework:

```go
// In application builder
app, err := forge.New("my-app").
    WithMiddleware(
        middleware.NewLoggingMiddleware(logger),
        middleware.NewRecoveryMiddleware(logger),
        middleware.NewCORSMiddleware(corsConfig),
    ).
    WithAuth(authConfig).
    WithRateLimit(rateLimitConfig).
    Build()
```

The middleware stack is automatically configured based on the application's configuration and environment settings.