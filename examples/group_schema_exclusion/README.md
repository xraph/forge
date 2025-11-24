# Group Schema Exclusion Example

This example demonstrates the `forge.WithGroupSchemaExclude()` option, which allows you to exclude entire route groups from schema generation (OpenAPI, AsyncAPI, oRPC).

## What This Example Shows

1. **Public API Routes**: Routes that appear in OpenAPI schema
2. **Admin Group**: Internal admin routes excluded from schemas using `WithGroupSchemaExclude()`
3. **Debug Group**: Debug routes excluded from schemas
4. **Monitoring Group**: Internal monitoring routes excluded with additional group options

## Key Features

### WithGroupSchemaExclude()

Excludes all routes in a group from:
- OpenAPI schema
- AsyncAPI schema  
- oRPC schema

```go
adminGroup := router.Group("/admin", forge.WithGroupSchemaExclude())
adminGroup.GET("/users", handler)        // Excluded
adminGroup.DELETE("/cache", handler)     // Excluded
```

### Metadata Inheritance

Routes automatically inherit the group's exclusion metadata:
```go
debugGroup := router.Group("/debug", forge.WithGroupSchemaExclude())

// These routes automatically get:
// - openapi.exclude = true
// - asyncapi.exclude = true
// - orpc.exclude = true
debugGroup.GET("/status", handler)
debugGroup.GET("/metrics", handler)
```

### Combining Options

You can combine `WithGroupSchemaExclude()` with other group options:
```go
monitoringGroup := router.Group("/internal",
    forge.WithGroupSchemaExclude(),
    forge.WithGroupTags("monitoring", "internal"),
    forge.WithGroupMiddleware(authMiddleware),
)
```

## Running the Example

### Build and Run
```bash
go build
./group_schema_exclusion
```

### Test the Routes

#### Public Routes (appear in schema)
```bash
curl http://localhost:8080/api/users
curl http://localhost:8080/api/products
```

#### Internal Routes (excluded from schema)
```bash
# Admin routes
curl http://localhost:8080/admin/users
curl http://localhost:8080/admin/config
curl -X POST http://localhost:8080/admin/cache/flush

# Debug routes  
curl http://localhost:8080/debug/status
curl http://localhost:8080/debug/routes

# Monitoring routes
curl http://localhost:8080/internal/monitoring/health
```

### Check the OpenAPI Spec

```bash
# Get the OpenAPI schema
curl http://localhost:8080/openapi.json | jq '.paths | keys'
```

Expected output (only public routes):
```json
[
  "/api/products",
  "/api/users"
]
```

Notice that `/admin/*`, `/debug/*`, and `/internal/monitoring/*` are **NOT** listed!

### Verify Routes Are Still Accessible

Even though excluded from schemas, the routes still work:
```bash
# These all return 200 OK
curl -i http://localhost:8080/admin/users
curl -i http://localhost:8080/debug/status
curl -i http://localhost:8080/internal/monitoring/health
```

## Use Cases

### 1. Admin Interfaces
```go
adminGroup := router.Group("/admin", forge.WithGroupSchemaExclude())
adminGroup.GET("/users", listUsers)
adminGroup.DELETE("/users/:id", deleteUser)
adminGroup.POST("/system/restart", restart)
```

### 2. Debug Endpoints
```go
debugGroup := router.Group("/debug", forge.WithGroupSchemaExclude())
debugGroup.GET("/pprof/heap", pprofHeap)
debugGroup.GET("/routes", listRoutes)
debugGroup.GET("/config", dumpConfig)
```

### 3. Internal Monitoring
```go
internalGroup := router.Group("/internal", forge.WithGroupSchemaExclude())
internalGroup.GET("/metrics", prometheusMetrics)
internalGroup.GET("/health/detailed", detailedHealth)
internalGroup.WebSocket("/events", eventStream)
```

### 4. Development Tools
```go
devGroup := router.Group("/dev", forge.WithGroupSchemaExclude())
devGroup.POST("/test-data/seed", seedTestData)
devGroup.POST("/hot-reload", triggerReload)
devGroup.GET("/schema/inspect", inspectSchema)
```

## Benefits

### 1. Cleaner Public Documentation
- Internal routes don't clutter your public API docs
- Customers only see relevant endpoints
- Professional API documentation

### 2. Security
- Internal endpoints aren't advertised
- Reduces attack surface
- Separates public and private APIs

### 3. Organization
- Clear separation between public and internal routes
- Easy to identify route purposes
- Better code structure

### 4. Flexibility
- Apply to entire groups at once
- No need to flag individual routes
- Combine with other group options

## Comparison

### Before WithGroupSchemaExclude()
```go
// Manual exclusion for each route
router.GET("/admin/users", handler, forge.WithSchemaExclude())
router.GET("/admin/config", handler, forge.WithSchemaExclude())
router.GET("/admin/cache", handler, forge.WithSchemaExclude())
// Easy to forget on new routes!
```

### After WithGroupSchemaExclude()
```go
// Automatic exclusion for entire group
adminGroup := router.Group("/admin", forge.WithGroupSchemaExclude())
adminGroup.GET("/users", handler)
adminGroup.GET("/config", handler)
adminGroup.GET("/cache", handler)
// All routes automatically excluded!
```

## Related Features

- **Route-Level Exclusion**: `forge.WithSchemaExclude()`
- **Extension-Level Exclusion**: `InternalExtension` interface
- **Per-Schema Exclusion**: `WithOpenAPIExclude()`, `WithAsyncAPIExclude()`

## Documentation

- Full docs: `INTERNAL_EXTENSION_INTERFACE.md`
- Quick reference: `SCHEMA_EXCLUSION_QUICK_REF.md`
- Route exclusion: `SCHEMA_EXCLUSION_FEATURE.md`

