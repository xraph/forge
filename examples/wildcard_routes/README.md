# Wildcard Routes Example

This example demonstrates support for wildcard routes in bunrouter, specifically addressing the issue where unnamed wildcards like `/api/auth/dashboard/static/*` were causing panics.

## The Problem

Previously, using an unnamed wildcard in a route pattern would cause bunrouter to panic:

```go
router.GET("/api/auth/dashboard/static/*", handler) // ❌ panic: param must have a name
```

**Error:** `panic: param must have a name: "api/auth/dashboard/static/*"`

## The Solution

The bunrouter adapter now automatically converts unnamed wildcards to named wildcards. You can use either format:

### Option 1: Unnamed Wildcard (Auto-converted)
```go
router.GET("/static/*", handler)
// Automatically converted to: /static/*filepath
```

### Option 2: Named Wildcard (Explicit)
```go
router.GET("/static/*filepath", handler)
// Used as-is
```

## How It Works

The `convertPathToBunRouter` function in `/internal/router/bunrouter.go` performs the following conversions:

| Input Pattern | Converted To | Description |
|---------------|--------------|-------------|
| `/static/*` | `/static/*filepath` | Wildcard at end |
| `/api/*/assets` | `/api/*filepath/assets` | Wildcard in middle |
| `/files/*path` | `/files/*path` | Already named (no change) |
| `/*` | `/*filepath` | Root wildcard |

## Implementation Details

### Code Changes

**Before:**
```go
func convertPathToBunRouter(path string) string {
    // Just returned path as-is
    return path
}
```

**After:**
```go
func convertPathToBunRouter(path string) string {
    // Auto-name wildcards for bunrouter compatibility
    if strings.HasSuffix(path, "/*") {
        return path + "filepath"
    }
    
    // Handle middle wildcards
    path = strings.ReplaceAll(path, "/*/", "/*filepath/")
    
    return path
}
```

## Running the Example

```bash
cd examples/wildcard_routes
go run main.go
```

Then visit:
- http://localhost:8080 - Home page with test links
- http://localhost:8080/static/css/style.css
- http://localhost:8080/api/auth/dashboard/static/index.html
- http://localhost:8080/files/documents/report.pdf

## Test Coverage

The fix includes comprehensive tests in `/internal/router/bunrouter_test.go`:

- `TestConvertPathToBunRouter` - Unit tests for path conversion logic
- `TestBunRouterAdapter_WildcardRoute` - Integration test for simple wildcards
- `TestBunRouterAdapter_ComplexWildcardRoute` - Integration test for complex nested paths

All tests pass with race detection enabled.

## Key Takeaways

1. ✅ **Unnamed wildcards now work**: Use `/path/*` without naming the parameter
2. ✅ **Backward compatible**: Named wildcards like `/*filepath` still work
3. ✅ **Production ready**: Includes comprehensive tests and race detection
4. ✅ **Zero breaking changes**: Existing code continues to work

## Best Practices

### For Static File Serving
```go
// Good: Simple and readable
router.GET("/static/*", serveStaticFiles)

// Also good: Explicit parameter name if you need to access it
router.GET("/static/*filepath", serveStaticFiles)
```

### Accessing Wildcard Parameters
```go
router.GET("/files/*", func(w http.ResponseWriter, r *http.Request) {
    // Method 1: Extract from URL path
    path := strings.TrimPrefix(r.URL.Path, "/files/")
    
    // Method 2: Access from context (if available)
    // params := forge.PathParams(r.Context())
    // filepath := params.Get("filepath")
    
    fmt.Fprintf(w, "Requested file: %s", path)
})
```

## Troubleshooting

### Issue: Still getting "param must have a name" error

**Cause:** Path doesn't start with a slash  
**Fix:** Ensure your route starts with `/`:

```go
// ❌ Wrong
router.GET("api/static/*", handler)

// ✅ Correct
router.GET("/api/static/*", handler)
```

### Issue: Wildcard not matching nested paths

**Cause:** Incorrect wildcard placement  
**Solution:** Wildcards must be at the end or properly separated:

```go
// ✅ Matches /static/any/nested/path.txt
router.GET("/static/*", handler)

// ❌ Won't work as expected
router.GET("/static*/", handler)
```

## Related Documentation

- [Bunrouter Documentation](https://github.com/uptrace/bunrouter)
- [Forge Router Guide](/docs/router.md)
- [Path Parameters](/docs/routing/parameters.md)

