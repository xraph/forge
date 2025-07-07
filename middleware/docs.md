# Forge Framework - Complete Middleware Package

## 🎯 Overview

The Forge middleware package provides a comprehensive, production-ready middleware system with advanced features for building robust HTTP APIs and web applications.

## 📦 Package Structure

```
middleware/
├── stack.go          # Middleware stack management with priorities
├── logging.go        # Structured request/response logging
├── recovery.go       # Panic recovery with detailed error handling
├── cors.go           # CORS with flexible origin matching
├── auth.go           # Multi-method authentication (JWT, API keys, etc.)
├── ratelimit.go      # Rate limiting with multiple strategies
├── security.go       # Security headers and CSRF protection
├── compression.go    # Request/response compression (gzip, deflate)
├── timeout.go        # Request timeout handling
├── requestid.go      # Request ID generation and tracking
├── validation.go     # Request validation and sanitization
├── bodylimit.go      # Request body size limiting
└── metrics.go        # Request metrics collection
```

## 🚀 Quick Start

### Basic Application Setup

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/middleware"
    "github.com/xraph/forge/logger"
    "github.com/xraph/forge/observability"
)

func main() {
    // Create logger
    logConfig := logger.LoggingConfig{
        Level:  "info",
        Format: "json",
    }
    appLogger := logger.NewLogger(logConfig)
    
    // Create metrics provider
    metricsConfig := observability.MetricsConfig{
        ServiceName: "my-api",
        Port:        9090,
    }
    metrics, _ := observability.NewMetrics(metricsConfig)
    
    // Create middleware stack
    stack := middleware.NewStack(middleware.DefaultStackConfig())
    
    // Add middleware in priority order
    stack.UseWithPriority(100, middleware.NewRequestIDMiddleware())
    stack.UseWithPriority(200, middleware.NewLoggingMiddleware(appLogger))
    stack.UseWithPriority(300, middleware.NewRecoveryMiddleware(appLogger))
    stack.UseWithPriority(400, middleware.NewCORSMiddleware(middleware.DefaultCORSConfig()))
    stack.UseWithPriority(500, middleware.NewSecurityMiddleware(middleware.DefaultSecurityConfig()))
    stack.UseWithPriority(600, middleware.NewCompressionMiddleware(middleware.DefaultCompressionConfig()))
    stack.UseWithPriority(700, middleware.NewAuthMiddleware(authConfig))
    stack.UseWithPriority(800, middleware.NewRateLimitMiddleware(rateLimitConfig, nil))
    stack.UseWithPriority(900, middleware.NewValidationMiddleware(middleware.DefaultValidationConfig()))
    stack.UseWithPriority(950, middleware.NewBodyLimitMiddleware(middleware.DefaultBodyLimitConfig()))
    stack.UseWithPriority(1000, middleware.NewMetricsMiddleware(metrics))
    
    // Create your handler
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"message": "Hello, World!"}`))
    })
    
    // Apply middleware stack
    finalHandler := stack.Handler(handler)
    
    // Start server
    server := &http.Server{
        Addr:    ":8080",
        Handler: finalHandler,
    }
    
    log.Println("Server starting on :8080")
    log.Fatal(server.ListenAndServe())
}
```

### Production Configuration

```go
func setupProductionMiddleware(logger logger.Logger, metrics observability.Metrics) middleware.Stack {
    stack := middleware.NewStack(middleware.ProductionStackConfig())
    
    // Request ID (always first)
    stack.UseWithPriority(100, middleware.NewRequestIDMiddleware())
    
    // Security middleware early
    securityConfig := middleware.StrictSecurityConfig()
    securityConfig.CSRF.Enabled = true
    stack.UseWithPriority(200, middleware.NewSecurityMiddleware(securityConfig))
    
    // Recovery with production settings
    recoveryConfig := middleware.ProductionRecoveryConfig()
    stack.UseWithPriority(300, middleware.NewRecoveryMiddleware(logger, recoveryConfig))
    
    // Logging with sampling
    loggingConfig := middleware.DefaultLoggingConfig()
    loggingConfig.OnlyLogErrors = false
    loggingConfig.SkipHealthChecks = true
    stack.UseWithPriority(400, middleware.NewLoggingMiddleware(logger, loggingConfig))
    
    // CORS with specific origins
    corsConfig := middleware.CORSConfig{
        AllowedOrigins:   []string{"https://yourdomain.com", "https://app.yourdomain.com"},
        AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
        AllowedHeaders:   []string{"Authorization", "Content-Type", "X-API-Key"},
        AllowCredentials: true,
        MaxAge:          3600,
    }
    stack.UseWithPriority(500, middleware.NewCORSMiddleware(corsConfig))
    
    // Compression
    compressionConfig := middleware.DefaultCompressionConfig()
    compressionConfig.GzipLevel = 6 // Balanced compression
    stack.UseWithPriority(600, middleware.NewCompressionMiddleware(compressionConfig))
    
    // Rate limiting with Redis
    rateLimitConfig := middleware.RateLimitConfig{
        RequestsPerSecond: 100,
        BurstSize:        200,
        Strategy:         middleware.RateLimitByAPIKey,
    }
    redisLimiter := middleware.NewRedisRateLimiter("redis:6379", "rate_limit:")
    stack.UseWithPriority(700, middleware.NewRateLimitMiddleware(rateLimitConfig, redisLimiter))
    
    // Authentication
    authConfig := middleware.AuthConfig{
        Methods: []middleware.AuthMethod{middleware.AuthMethodJWT, middleware.AuthMethodAPIKey},
        JWT: middleware.JWTConfig{
            Secret:         os.Getenv("JWT_SECRET"),
            ExpirationTime: 24 * time.Hour,
            ValidateExp:    true,
        },
        APIKey: middleware.APIKeyConfig{
            HeaderName: "X-API-Key",
            Validator:  &customAPIKeyValidator{},
        },
    }
    stack.UseWithPriority(800, middleware.NewAuthMiddleware(authConfig))
    
    // Request validation
    validationConfig := middleware.StrictValidationConfig()
    stack.UseWithPriority(900, middleware.NewValidationMiddleware(validationConfig))
    
    // Body limits
    bodyLimitConfig := middleware.DefaultBodyLimitConfig()
    bodyLimitConfig.DefaultLimit = 5 * 1024 * 1024 // 5MB
    stack.UseWithPriority(950, middleware.NewBodyLimitMiddleware(bodyLimitConfig))
    
    // Request timeout
    timeoutConfig := middleware.DefaultTimeoutConfig()
    timeoutConfig.Timeout = 30 * time.Second
    stack.UseWithPriority(980, middleware.NewTimeoutMiddleware(timeoutConfig))
    
    // Metrics (last)
    metricsConfig := middleware.ProductionMetricsConfig()
    stack.UseWithPriority(1000, middleware.NewMetricsMiddleware(metrics, metricsConfig))
    
    return stack
}
```

### Development Configuration

```go
func setupDevelopmentMiddleware(logger logger.Logger) middleware.Stack {
    stack := middleware.NewStack(middleware.DevelopmentStackConfig())
    
    // Request ID
    stack.UseWithPriority(100, middleware.NewRequestIDMiddleware())
    
    // Recovery with detailed output
    recoveryConfig := middleware.DevelopmentRecoveryConfig()
    stack.UseWithPriority(300, middleware.NewRecoveryMiddleware(logger, recoveryConfig))
    
    // Verbose logging
    loggingConfig := middleware.DefaultLoggingConfig()
    loggingConfig.LogRequests = true
    loggingConfig.LogResponses = true
    loggingConfig.LogHeaders = true
    stack.UseWithPriority(400, middleware.NewLoggingMiddleware(logger, loggingConfig))
    
    // Permissive CORS
    corsConfig := middleware.PermissiveCORSConfig()
    stack.UseWithPriority(500, middleware.NewCORSMiddleware(corsConfig))
    
    // Relaxed security
    securityConfig := middleware.RelaxedSecurityConfig()
    stack.UseWithPriority(600, middleware.NewSecurityMiddleware(securityConfig))
    
    // No rate limiting in development
    // Optional: light validation
    validationConfig := middleware.DefaultValidationConfig()
    stack.UseWithPriority(900, middleware.NewValidationMiddleware(validationConfig))
    
    return stack
}
```

## 🔧 Advanced Usage

### Conditional Middleware

```go
// Apply authentication only to API routes
stack.UseForPath("/api/*", middleware.NewAuthMiddleware(authConfig))

// Apply rate limiting only to POST requests
stack.UseForMethod("POST", middleware.NewRateLimitMiddleware(strictRateLimit, limiter))

// Apply body limits only for uploads
stack.UseIf(func(r *http.Request) bool {
    return strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data")
}, middleware.NewBodyLimitMiddleware(uploadBodyLimit))

// Custom condition for admin routes
stack.UseIf(func(r *http.Request) bool {
    return strings.HasPrefix(r.URL.Path, "/admin") && 
           r.Header.Get("X-Admin-Token") != ""
}, middleware.NewAuthMiddleware(adminAuthConfig))
```

### Middleware Groups

```go
// Define middleware groups
apiMiddleware := []middleware.Middleware{
    middleware.NewAuthMiddleware(authConfig),
    middleware.NewRateLimitMiddleware(apiRateLimit, limiter),
    middleware.NewValidationMiddleware(apiValidation),
}

adminMiddleware := []middleware.Middleware{
    middleware.NewAuthMiddleware(adminAuthConfig),
    middleware.RequireRole("admin"),
    middleware.NewRateLimitMiddleware(adminRateLimit, limiter),
}

// Register groups
stack.Group("api", apiMiddleware...)
stack.Group("admin", adminMiddleware...)

// Use groups
stack.UseGroup("api")
// or conditionally
stack.UseForPath("/admin/*", stack.UseGroup("admin"))
```

### Custom Middleware

```go
// Custom middleware using the Middleware interface
type CustomAuthMiddleware struct {
    *middleware.BaseMiddleware
    authService AuthService
}

func NewCustomAuthMiddleware(authService AuthService) middleware.Middleware {
    return &CustomAuthMiddleware{
        BaseMiddleware: middleware.NewBaseMiddleware(
            "custom_auth",
            middleware.PriorityAuth,
            "Custom authentication middleware",
        ),
        authService: authService,
    }
}

func (cam *CustomAuthMiddleware) Handle(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Custom authentication logic
        token := r.Header.Get("X-Custom-Token")
        user, err := cam.authService.ValidateToken(r.Context(), token)
        if err != nil {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        
        // Add user to context
        ctx := context.WithValue(r.Context(), "user", user)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// Register custom middleware
stack.Use(NewCustomAuthMiddleware(authService))
```

### Error Handling

```go
// Custom error handlers
recoveryConfig := middleware.RecoveryConfig{
    RecoveryHandler: func(w http.ResponseWriter, r *http.Request, recovered interface{}) {
        // Custom panic handling
        logger.Error("Panic occurred", "panic", recovered)
        
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]string{
            "error": "Internal server error",
            "id":    middleware.GetRequestIDFromRequest(r),
        })
    },
}

validationConfig := middleware.ValidationConfig{
    ErrorHandler: &customValidationErrorHandler{},
}

// Custom timeout handler
timeoutConfig := middleware.TimeoutConfig{
    TimeoutHandler: func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusRequestTimeout)
        json.NewEncoder(w).Encode(map[string]interface{}{
            "error": "Request timeout",
            "timeout": "30s",
            "request_id": middleware.GetRequestIDFromRequest(r),
        })
    },
}
```

## 📊 Monitoring and Observability

### Metrics Integration

```go
// Set up custom metrics extractors
databaseExtractor := &middleware.DatabaseMetricsExtractor{}
cacheExtractor := &middleware.CacheMetricsExtractor{}

metricsConfig := middleware.MetricsConfig{
    IncludeMethod: true,
    IncludePath:   true,
    IncludeStatus: true,
    NormalizePaths: true,
    CustomMetrics: []middleware.CustomMetricExtractor{
        databaseExtractor,
        cacheExtractor,
    },
}

// In your handlers, add context data for metrics
func databaseHandler(w http.ResponseWriter, r *http.Request) {
    start := time.Now()
    
    // Simulate database operations
    ctx := middleware.IncrementDBQueryCount(r.Context())
    ctx = middleware.IncrementDBQueryCount(ctx)
    
    // Set database duration
    ctx = middleware.SetDBDuration(ctx, time.Since(start))
    
    // Update request context
    *r = *r.WithContext(ctx)
    
    w.WriteHeader(http.StatusOK)
}
```

### Health Checks

```go
// Monitor middleware health
func healthHandler(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    // Check middleware health
    middlewareHealth := make(map[string]string)
    for _, info := range stack.List() {
        if middleware, ok := stack.GetMiddleware(info.Name); ok {
            if err := middleware.Health(ctx); err != nil {
                middlewareHealth[info.Name] = "unhealthy: " + err.Error()
            } else {
                middlewareHealth[info.Name] = "healthy"
            }
        }
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "status": "ok",
        "middleware": middlewareHealth,
        "timestamp": time.Now().UTC(),
    })
}
```

## 🧪 Testing

### Unit Testing Middleware

```go
func TestLoggingMiddleware(t *testing.T) {
    // Create test logger
    logger := logger.NewTestLogger()
    
    // Create middleware
    middleware := middleware.NewLoggingMiddleware(logger)
    
    // Create test handler
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("test response"))
    })
    
    // Create test request
    req := httptest.NewRequest("GET", "/test", nil)
    rec := httptest.NewRecorder()
    
    // Execute middleware
    middleware.Handle(handler).ServeHTTP(rec, req)
    
    // Assert results
    assert.Equal(t, http.StatusOK, rec.Code)
    assert.True(t, logger.HasLoggedLevel(logger.InfoLevel))
}

func TestRateLimitMiddleware(t *testing.T) {
    // Create test rate limiter
    limiter := middleware.NewTestRateLimiter()
    
    config := middleware.RateLimitConfig{
        RequestsPerSecond: 10,
        BurstSize:        20,
        Strategy:         middleware.RateLimitByIP,
    }
    
    middleware := middleware.NewRateLimitMiddleware(config, limiter)
    
    // Test allowed request
    limiter.SetAllowNext(true)
    req := httptest.NewRequest("GET", "/test", nil)
    rec := httptest.NewRecorder()
    
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })
    
    middleware.Handle(handler).ServeHTTP(rec, req)
    assert.Equal(t, http.StatusOK, rec.Code)
    
    // Test blocked request
    limiter.SetAllowNext(false)
    rec = httptest.NewRecorder()
    middleware.Handle(handler).ServeHTTP(rec, req)
    assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}
```

### Integration Testing

```go
func TestMiddlewareStack(t *testing.T) {
    // Create test stack
    stack := middleware.NewStack(middleware.DefaultStackConfig())
    
    // Add test middleware
    testLogger := logger.NewTestLogger()
    stack.Use(middleware.NewRequestIDMiddleware())
    stack.Use(middleware.NewLoggingMiddleware(testLogger))
    stack.Use(middleware.NewRecoveryMiddleware(testLogger))
    
    // Create test server
    handler := stack.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify request ID was set
        requestID := middleware.GetRequestIDFromRequest(r)
        assert.NotEmpty(t, requestID)
        
        w.Header().Set("X-Request-ID", requestID)
        w.WriteHeader(http.StatusOK)
    }))
    
    server := httptest.NewServer(handler)
    defer server.Close()
    
    // Test request
    resp, err := http.Get(server.URL + "/test")
    assert.NoError(t, err)
    assert.Equal(t, http.StatusOK, resp.StatusCode)
    
    // Verify request ID in response
    requestID := resp.Header.Get("X-Request-ID")
    assert.NotEmpty(t, requestID)
    
    // Verify logging occurred
    assert.True(t, testLogger.HasLoggedLevel(logger.InfoLevel))
}
```

## 🚀 Performance Optimization

### Middleware Ordering

The order of middleware execution is crucial for performance:

1. **Request ID** (100) - Fast, no dependencies
2. **Logging** (200) - Early to capture all requests
3. **Recovery** (300) - Must catch all panics
4. **Metrics** (350) - Early to measure everything
5. **Tracing** (375) - Start spans early
6. **CORS** (400) - Before authentication
7. **Security** (500) - Security headers
8. **Compression** (600) - Before content generation
9. **Authentication** (700) - Verify identity
10. **Rate Limiting** (800) - After auth for better keys
11. **Validation** (900) - Validate authenticated requests
12. **Body Limit** (950) - Before reading body
13. **Timeout** (980) - Wrap request execution
14. **Application** (1000) - Your handlers

### Performance Tips

```go
// Use goroutines for non-critical middleware
metricsConfig.UseGoroutine = true
loggingConfig.UseAsync = true

// Optimize path normalization
metricsConfig.PathNormalizers = map[string]string{
    "/api/users/123":     "/api/users/{id}",
    "/api/posts/456":     "/api/posts/{id}",
    "/api/comments/789":  "/api/comments/{id}",
}

// Use connection pooling for external services
rateLimitConfig.ConnectionPool = &redis.ConnectionPool{
    MaxActive: 100,
    MaxIdle:   10,
}

// Enable compression for appropriate content
compressionConfig.ContentTypes = []string{
    "application/json",
    "text/html",
    "text/css",
    "application/javascript",
}
```

## 🔒 Security Best Practices

### Production Security Stack

```go
func secureMiddlewareStack() middleware.Stack {
    stack := middleware.NewStack(middleware.ProductionStackConfig())
    
    // Strict security headers
    securityConfig := middleware.SecurityConfig{
        CSP: "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'",
        HSTS: true,
        HSTSMaxAge: 31536000,
        HSTSIncludeSubdomains: true,
        HSTSPreload: true,
        FrameOptions: "DENY",
        ContentTypeOptions: "nosniff",
        XSSProtection: "1; mode=block",
        ReferrerPolicy: "strict-origin-when-cross-origin",
        CSRF: middleware.CSRFConfig{
            Enabled: true,
            TokenLength: 32,
            CookieSecure: true,
            CookieHTTPOnly: true,
            CookieSameSite: http.SameSiteStrictMode,
        },
    }
    
    // Rate limiting with multiple strategies
    rateLimitConfig := middleware.RateLimitConfig{
        RequestsPerSecond: 50,
        BurstSize: 100,
        Strategy: middleware.RateLimitByAPIKey,
    }
    
    // Strict validation
    validationConfig := middleware.ValidationConfig{
        MaxBodySize: 1024 * 1024, // 1MB
        ValidateJSON: true,
        JSONMaxDepth: 5,
        JSONMaxKeys: 50,
        SanitizeInput: true,
        StopOnFirstError: true,
    }
    
    // Add to stack...
    
    return stack
}
```

This completes the comprehensive middleware package for the Forge framework. The package provides production-ready middleware with extensive configuration options, monitoring capabilities, and security features.