# Group Middleware Scoping Example

This example demonstrates the difference between scoped middleware (`Use()`) and global middleware (`UseGlobal()`) in Forge.

## Two Types of Middleware

### 1. UseGlobal() - Global Middleware

Global middleware applies to **ALL routes** in the entire application, regardless of where they're registered.

```go
router.UseGlobal(corsMiddleware())        // Applies to ALL routes
group.UseGlobal(metricsMiddleware())      // Still applies to ALL routes (not just group)
```

**Use cases:**
- CORS handling
- Request logging
- Metrics collection
- Security headers
- Extension middleware (automatically uses UseGlobal)

### 2. Use() - Scoped Middleware

Scoped middleware applies only to routes registered through that specific router/group instance (and inherited by child groups).

```go
router.Use(authMiddleware())              // Only routes on router + children
group.Use(rateLimitMiddleware())          // Only routes in group + children
```

**Use cases:**
- Authentication for protected routes
- Rate limiting for specific endpoints
- API versioning middleware
- Group-specific validation

## The Fix (Historical Context)

Previously, `Use()` behaved like `UseGlobal()` - applying middleware to all routes regardless of scope. This made it impossible to scope middleware to specific groups.

The fix:
1. **Added explicit `UseGlobal()` method** - For truly global middleware
2. **Made `Use()` properly scoped** - Middleware only applies to routes registered through that router/group
3. **Apply middleware during route registration** - Ensures proper scoping

This ensures that:
- Middleware added via `Use()` is scoped to that router/group
- Middleware added via `UseGlobal()` applies globally to all routes
- Child groups inherit parent's scoped middleware (as expected)

## Running the Example

```bash
go run main.go
```

Then test the endpoints:

```bash
# Public route - global + root scoped middleware
curl http://localhost:8080/public

# Protected route - global + root scoped + auth middleware
curl http://localhost:8080/protected

# API status - global + root scoped (no auth)
curl http://localhost:8080/api/status
```

## Expected Output

When you access `/public`:
```
✓ GLOBAL middleware executed for: /public
✓ ROOT-SCOPED middleware executed for: /public
```

When you access `/protected`:
```
✓ GLOBAL middleware executed for: /protected
✓ ROOT-SCOPED middleware executed for: /protected
🔒 Auth middleware executed for: /protected
```

When you access `/api/status`:
```
✓ GLOBAL middleware executed for: /api/status
✓ ROOT-SCOPED middleware executed for: /api/status
```

Note: No auth middleware on `/api/status` because it's in a different group that doesn't have auth middleware.

## Use Cases

This enables flexible middleware patterns:

```go
router := app.Router()

// Global middleware - applies to ALL routes
router.UseGlobal(corsMiddleware())
router.UseGlobal(loggingMiddleware())

// Root scoped middleware - only routes on root + children
router.Use(requestIDMiddleware())

// Protected routes with authentication
protectedRoutes := router.Group("/api")
protectedRoutes.Use(authMiddleware())       // Only /api routes
protectedRoutes.Use(rateLimitMiddleware())  // Only /api routes

protectedRoutes.GET("/users", listUsers)    // CORS + logging + requestID + auth + rate limit
protectedRoutes.POST("/users", createUser)  // CORS + logging + requestID + auth + rate limit

// Public routes without authentication
router.GET("/health", healthCheck)          // CORS + logging + requestID (no auth)
router.GET("/", homepage)                   // CORS + logging + requestID (no auth)

// Admin routes with different middleware
adminRoutes := router.Group("/admin")
adminRoutes.Use(adminAuthMiddleware())      // Only /admin routes
adminRoutes.GET("/dashboard", dashboard)    // CORS + logging + requestID + adminAuth
```

## Technical Details

### Two Methods for Middleware

**1. Use() - Scoped Middleware:**
```go
func (r *router) Use(middleware ...Middleware) {
    r.middleware = append(r.middleware, middleware...)
    // Middleware stored locally and applied during route registration
    // Only affects routes registered through this router/group instance
}
```

**2. UseGlobal() - Global Middleware:**
```go
func (r *router) UseGlobal(middleware ...Middleware) {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    // Register directly with adapter for ALL routes
    if r.adapter != nil {
        for _, mw := range middleware {
            httpMiddleware := convertForgeMiddlewareToHTTP(mw, ...)
            r.adapter.UseGlobal(httpMiddleware)
        }
    }
}
```

**Route Registration:**
```go
func (r *router) register(method, path string, handler any, opts ...RouteOption) error {
    // Combine router's scoped middleware + route-specific middleware
    combinedMiddleware := append([]Middleware{}, r.middleware...)
    combinedMiddleware = append(combinedMiddleware, cfg.Middleware...)
    
    // Apply combined middleware to the handler
    finalHandler := applyMiddleware(converted, combinedMiddleware, ...)
    
    // Register with adapter (global middleware already applied by adapter)
    r.adapter.Handle(method, fullPath, finalHandler)
}
```

This dual approach ensures:
- Global middleware (`UseGlobal`) runs for every route via the adapter
- Scoped middleware (`Use`) only applies to routes registered through that router/group
- Child groups inherit parent's scoped middleware
- Extensions use `UseGlobal` for truly global behavior

