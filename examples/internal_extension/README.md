# Internal Extension Example

This example demonstrates the `InternalExtension` interface, which allows extensions to automatically exclude all their routes from schema generation (OpenAPI, AsyncAPI, oRPC).

## What This Example Shows

1. **InternalExtension Interface**: How to implement `ExcludeFromSchemas()` to mark an extension as internal
2. **Automatic Exclusion**: Routes are automatically excluded without manual flags
3. **Three Approaches**: Different ways to register routes with auto-exclusion
4. **Mixed Endpoints**: Public API endpoints vs internal endpoints in the same app

## Structure

```
.
├── main.go         # Application entry point
├── go.mod          # Module definition
└── README.md       # This file
```

## Key Components

### DebugExtension

An internal extension that provides debug/monitoring endpoints:
- `/internal/debug/status` - System status
- `/internal/debug/metrics` - Metrics data
- `/internal/debug/config` - Configuration dump
- `/internal/debug/routes` - Route listing

All these routes are **automatically excluded** from OpenAPI/AsyncAPI/oRPC schemas.

### Public API

The application also has a public API endpoint:
- `/api/users` - Public endpoint (appears in schemas)

## How It Works

### 1. Implement InternalExtension Interface

```go
type DebugExtension struct {
    app forge.App
}

// This marks the extension as internal
func (e *DebugExtension) ExcludeFromSchemas() bool {
    return true
}
```

### 2. Register Routes with Auto-Exclusion

```go
func (e *DebugExtension) Start(ctx context.Context) error {
    router := e.app.Router()
    
    // Approach 1: Single route
    opt := forge.WithExtensionExclusion(e)
    router.GET("/internal/debug/status", e.handleStatus, opt)
    
    // Approach 2: Multiple routes (recommended)
    opts := forge.ExtensionRoutes(e)
    router.GET("/internal/debug/metrics", e.handleMetrics, opts...)
    router.GET("/internal/debug/config", e.handleConfig, opts...)
    
    // Approach 3: With additional options
    opts = forge.ExtensionRoutes(e,
        forge.WithTags("debug", "internal"),
    )
    router.GET("/internal/debug/routes", e.handleRoutes, opts...)
    
    return nil
}
```

## Running the Example

### Build and Run
```bash
go build -o bin/internal_extension .
./bin/internal_extension
```

### Test the Endpoints

```bash
# Public endpoint (appears in schema)
curl http://localhost:8080/api/users

# Internal endpoints (excluded from schema)
curl http://localhost:8080/internal/debug/status
curl http://localhost:8080/internal/debug/metrics
curl http://localhost:8080/internal/debug/config
curl http://localhost:8080/internal/debug/routes
```

### Check the OpenAPI Spec

```bash
# Notice that /internal/debug/* endpoints are NOT listed
curl http://localhost:8080/openapi.json | jq '.paths | keys'
```

Expected output:
```json
[
  "/api/users"
]
```

The internal debug endpoints are **not** in the schema but are still accessible!

## Key Benefits

### 1. No Manual Exclusion
```go
// ❌ Before: Manual exclusion for every route
router.GET("/debug/status", handler, forge.WithSchemaExclude())
router.GET("/debug/metrics", handler, forge.WithSchemaExclude())

// ✅ After: Automatic exclusion
opts := forge.ExtensionRoutes(e)
router.GET("/debug/status", handler, opts...)
router.GET("/debug/metrics", handler, opts...)
```

### 2. Self-Documenting
Implementing `InternalExtension` clearly marks the extension as internal/debug.

### 3. Consistent Behavior
All extension routes automatically follow the same exclusion policy.

### 4. Flexible
Can be combined with other route options like auth, tags, timeouts.

## Use Cases

This pattern is ideal for:

- **Debug Extensions**: System diagnostics, route inspection
- **Admin Extensions**: User management, cache control
- **Metrics Extensions**: Prometheus, internal monitoring
- **Internal Tools**: Configuration reload, health details

## Related Documentation

- `/INTERNAL_EXTENSION_INTERFACE.md` - Full documentation
- `/INTERNAL_EXTENSION_QUICK_REF.md` - Quick reference
- `/SCHEMA_EXCLUSION_FEATURE.md` - Route-level exclusion

## Testing

```bash
# Run tests
go test -v

# Build
go build

# Run
./bin/internal_extension
```

