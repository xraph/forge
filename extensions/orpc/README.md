# oRPC Extension for Forge

The oRPC (Open RPC) extension automatically exposes your Forge application's REST API as JSON-RPC 2.0 methods with OpenRPC schema support. This provides a unified RPC interface for your existing HTTP endpoints.

## Features

- ✅ **Automatic Method Generation**: Converts REST routes to JSON-RPC 2.0 methods
- ✅ **OpenRPC Schema**: Generates OpenRPC 1.3.2 compliant schema documentation
- ✅ **Route Execution**: Executes underlying HTTP endpoints via JSON-RPC calls
- ✅ **Batch Requests**: Supports JSON-RPC 2.0 batch request specification
- ✅ **Custom Method Names**: Override auto-generated method names
- ✅ **Schema Annotations**: Add custom param/result schemas via route options
- ✅ **Pattern Matching**: Include/exclude routes with glob patterns
- ✅ **Flexible Naming**: Multiple naming strategies (path-based, method-based, custom)
- ✅ **Observability**: Integrated metrics and logging
- ✅ **Type Safety**: Schema validation and type hints

## Installation

```bash
go get github.com/xraph/forge/extensions/orpc
```

## Quick Start

### Basic Setup (Auto-Exposure)

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/orpc"
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Name:    "my-api",
        Version: "1.0.0",
    })
    
    // Add REST routes
    app.Router().GET("/users/:id", getUserHandler)
    app.Router().POST("/users", createUserHandler)
    
    // Enable oRPC - automatically exposes routes as JSON-RPC methods
    app.RegisterExtension(orpc.NewExtension(
        orpc.WithEnabled(true),
        orpc.WithAutoExposeRoutes(true),
    ))
    
    app.Run()
}
```

This automatically creates JSON-RPC methods:
- `get.users.id` → `GET /users/:id`
- `create.users` → `POST /users`

### Access Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/rpc` | POST | Main JSON-RPC 2.0 endpoint |
| `/rpc/schema` | GET | OpenRPC schema document |
| `/rpc/methods` | GET | List available methods |

## Configuration

### Using ConfigManager (Recommended)

In `config.yaml`:

```yaml
extensions:
  orpc:
    enabled: true
    endpoint: "/rpc"
    openrpc_endpoint: "/rpc/schema"
    auto_expose_routes: true
    method_prefix: "api."
    exclude_patterns:
      - "/_/*"
      - "/internal/*"
    include_patterns: []
    enable_openrpc: true
    enable_discovery: true
    enable_batch: true
    batch_limit: 10
    naming_strategy: "path"
    max_request_size: 1048576
    request_timeout: 30
    schema_cache: true
    enable_metrics: true
```

Then in code:

```go
// Config loaded automatically from ConfigManager
app.RegisterExtension(orpc.NewExtension())
```

### Programmatic Configuration

```go
app.RegisterExtension(orpc.NewExtension(
    orpc.WithEnabled(true),
    orpc.WithEndpoint("/rpc"),
    orpc.WithAutoExposeRoutes(true),
    orpc.WithMethodPrefix("api."),
    orpc.WithExcludePatterns([]string{"/_/*", "/internal/*"}),
    orpc.WithIncludePatterns([]string{"/api/*"}), // Only expose /api/* routes
    orpc.WithOpenRPC(true),
    orpc.WithDiscovery(true),
    orpc.WithBatch(true),
    orpc.WithBatchLimit(10),
    orpc.WithNamingStrategy("path"),
    orpc.WithAuth("X-API-Key", []string{"secret-token"}),
    orpc.WithRateLimit(100), // 100 requests per minute
    orpc.WithMaxRequestSize(1024 * 1024), // 1MB
    orpc.WithRequestTimeout(30), // seconds
))
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `Enabled` | bool | `true` | Enable/disable oRPC server |
| `Endpoint` | string | `"/rpc"` | JSON-RPC endpoint path |
| `OpenRPCEndpoint` | string | `"/rpc/schema"` | OpenRPC schema endpoint |
| `AutoExposeRoutes` | bool | `true` | Auto-generate methods from routes |
| `MethodPrefix` | string | `""` | Prefix for method names |
| `ExcludePatterns` | []string | `["/_/*"]` | Patterns to exclude |
| `IncludePatterns` | []string | `[]` | Only expose matching patterns |
| `EnableOpenRPC` | bool | `true` | Generate OpenRPC schema |
| `EnableDiscovery` | bool | `true` | Enable method discovery endpoint |
| `EnableBatch` | bool | `true` | Support batch requests |
| `BatchLimit` | int | `10` | Max batch size |
| `NamingStrategy` | string | `"path"` | Method naming: "path", "method", "custom" |
| `MaxRequestSize` | int64 | `1048576` | Max request size in bytes |
| `RequestTimeout` | int | `30` | Request timeout in seconds |
| `SchemaCache` | bool | `true` | Cache generated schemas |
| `EnableMetrics` | bool | `true` | Enable metrics collection |

## Usage Examples

### Custom Method Names

```go
app.Router().GET("/users/:id", getUserHandler,
    forge.WithORPCMethod("user.get"),  // Custom RPC method name
    forge.WithSummary("Get user by ID"),
)

// JSON-RPC method: "user.get" (instead of auto-generated "get.users.id")
```

### Schema Annotations

```go
app.Router().GET("/users/:id", getUserHandler,
    forge.WithORPCParams(&orpc.ParamsSchema{
        Type: "object",
        Properties: map[string]*orpc.PropertySchema{
            "id": {
                Type:        "string",
                Description: "User ID",
            },
        },
        Required: []string{"id"},
    }),
    forge.WithORPCResult(&orpc.ResultSchema{
        Type:        "object",
        Description: "User details",
        Properties: map[string]*orpc.PropertySchema{
            "id":    {Type: "string"},
            "name":  {Type: "string"},
            "email": {Type: "string"},
        },
    }),
)
```

### Exclude Specific Routes

```go
// Explicitly exclude from oRPC
app.Router().GET("/internal/debug", debugHandler,
    forge.WithORPCExclude(),
)
```

### Making JSON-RPC Calls

#### Single Request

```bash
curl -X POST http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "get.users.id",
    "params": {
      "id": "123",
      "query": {"include": "profile"}
    },
    "id": 1
  }'
```

Response:

```json
{
  "jsonrpc": "2.0",
  "result": {
    "id": "123",
    "name": "John Doe",
    "email": "john@example.com"
  },
  "id": 1
}
```

#### Batch Request

```bash
curl -X POST http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '[
    {
      "jsonrpc": "2.0",
      "method": "get.users.id",
      "params": {"id": "123"},
      "id": 1
    },
    {
      "jsonrpc": "2.0",
      "method": "get.users.id",
      "params": {"id": "456"},
      "id": 2
    }
  ]'
```

Response:

```json
[
  {
    "jsonrpc": "2.0",
    "result": {"id": "123", "name": "John"},
    "id": 1
  },
  {
    "jsonrpc": "2.0",
    "result": {"id": "456", "name": "Jane"},
    "id": 2
  }
]
```

#### Error Response

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32601,
    "message": "Method 'user.delete' not found"
  },
  "id": 1
}
```

### OpenRPC Schema

```bash
curl http://localhost:8080/rpc/schema
```

Returns OpenRPC 1.3.2 compliant schema:

```json
{
  "openrpc": "1.3.2",
  "info": {
    "title": "my-api",
    "version": "1.0.0",
    "description": "JSON-RPC 2.0 API"
  },
  "methods": [
    {
      "name": "get.users.id",
      "summary": "Get user by ID",
      "params": [
        {
          "name": "params",
          "required": true,
          "schema": {
            "type": "object",
            "properties": {
              "id": {"type": "string", "description": "User ID"}
            },
            "required": ["id"]
          }
        }
      ],
      "result": {
        "name": "result",
        "schema": {"type": "object"}
      }
    }
  ]
}
```

## Method Naming

Methods are named based on HTTP method and path:

| Route | HTTP Method | Auto-Generated Name | With Prefix `api.` |
|-------|-------------|---------------------|-------------------|
| `/users` | GET | `get.users` | `api.get.users` |
| `/users` | POST | `create.users` | `api.create.users` |
| `/users/:id` | GET | `get.users.id` | `api.get.users.id` |
| `/users/:id` | PUT | `update.users.id` | `api.update.users.id` |
| `/users/:id` | DELETE | `delete.users.id` | `api.delete.users.id` |
| `/users/:id` | PATCH | `patch.users.id` | `api.patch.users.id` |
| `/api/v1/posts` | GET | `get.api.v1.posts` | `api.get.api.v1.posts` |

### Naming Strategies

1. **Path-based (default)**: Generates from HTTP method + path
2. **Method-based**: Uses route name if available
3. **Custom**: Uses `forge.WithORPCMethod()` annotation

## Pattern Matching

### Exclude Patterns

```go
orpc.NewExtension(
    orpc.WithExcludePatterns([]string{
        "/_/*",        // Exclude /_/health, /_/metrics
        "/internal/*", // Exclude /internal/admin
        "/debug/*",    // Exclude debug routes
    }),
)
```

### Include Patterns

```go
orpc.NewExtension(
    orpc.WithIncludePatterns([]string{
        "/api/*",      // Only expose /api/* routes
        "/public/*",   // And /public/* routes
    }),
)
```

**Note**: If `IncludePatterns` is set, only matching routes are exposed.

## Route Options Reference

### `forge.WithORPCMethod(name string)`

Set custom JSON-RPC method name.

```go
forge.WithORPCMethod("user.get")
```

### `forge.WithORPCParams(schema *orpc.ParamsSchema)`

Define parameter schema for OpenRPC.

```go
forge.WithORPCParams(&orpc.ParamsSchema{
    Type: "object",
    Properties: map[string]*orpc.PropertySchema{
        "id": {Type: "string"},
    },
})
```

### `forge.WithORPCResult(schema *orpc.ResultSchema)`

Define result schema for OpenRPC.

```go
forge.WithORPCResult(&orpc.ResultSchema{
    Type: "object",
    Description: "User object",
})
```

### `forge.WithORPCExclude()`

Exclude route from oRPC auto-exposure.

```go
forge.WithORPCExclude()
```

### `forge.WithORPCTags(tags ...string)`

Add custom tags for OpenRPC schema.

```go
forge.WithORPCTags("users", "admin")
```

## Observability

### Metrics

The extension automatically records:

- `orpc_methods_total` - Total registered methods (gauge)
- `orpc_requests_total{status,method}` - Total requests by status and method (counter)

### Logging

All operations are logged with structured fields:

```go
logger.Debug("orpc: method registered", forge.F("method", methodName))
logger.Warn("orpc: failed to generate method", forge.F("path", path), forge.F("error", err))
logger.Info("orpc: routes exposed as methods", forge.F("total_routes", count), forge.F("methods", methodCount))
```

## JSON-RPC 2.0 Error Codes

| Code | Message | Description |
|------|---------|-------------|
| -32700 | Parse error | Invalid JSON |
| -32600 | Invalid Request | Invalid Request object |
| -32601 | Method not found | Method does not exist |
| -32602 | Invalid params | Invalid method parameters |
| -32603 | Internal error | Internal JSON-RPC error |
| -32000 | Server error | Implementation-defined server error |

## Advanced Usage

### Manual Method Registration

Register custom methods without routes:

```go
ext := app.GetExtension("orpc").(*orpc.Extension)
server := ext.Server()

server.RegisterMethod(&orpc.Method{
    Name:        "custom.operation",
    Description: "Custom operation not tied to HTTP route",
    Handler: func(ctx context.Context, params interface{}) (interface{}, error) {
        // Custom logic
        return map[string]string{"result": "success"}, nil
    },
})
```

### Interceptors

Add middleware to JSON-RPC method execution:

```go
server.Use(func(ctx context.Context, req *orpc.Request, next orpc.MethodHandler) (interface{}, error) {
    // Pre-processing
    log.Printf("Calling method: %s", req.Method)
    
    // Execute method
    result, err := next(ctx, req.Params)
    
    // Post-processing
    if err != nil {
        log.Printf("Method failed: %s", err)
    }
    
    return result, err
})
```

## Testing

```go
func TestORPCExtension(t *testing.T) {
    app := forge.NewApp(forge.AppConfig{
        Name: "test-app",
    })
    
    app.RegisterExtension(orpc.NewExtension(
        orpc.WithEnabled(true),
    ))
    
    app.Router().GET("/test", func(ctx forge.Context) error {
        return ctx.JSON(200, map[string]string{"status": "ok"})
    })
    
    app.Start(context.Background())
    defer app.Stop(context.Background())
    
    // Test JSON-RPC request
    reqBody := map[string]interface{}{
        "jsonrpc": "2.0",
        "method":  "get.test",
        "id":      1,
    }
    body, _ := json.Marshal(reqBody)
    
    req := httptest.NewRequest("POST", "/rpc", bytes.NewReader(body))
    rec := httptest.NewRecorder()
    
    app.Router().ServeHTTP(rec, req)
    
    assert.Equal(t, 200, rec.Code)
}
```

## Architecture

```
┌─────────────────────────────────────────────┐
│           Forge Application                 │
│  ┌──────────────────────────────────────┐  │
│  │     Router (REST API)                │  │
│  │  GET /users/:id                      │  │
│  │  POST /users                         │  │
│  │  PUT /users/:id                      │  │
│  └──────────────────────────────────────┘  │
│                    │                        │
│                    ▼                        │
│  ┌──────────────────────────────────────┐  │
│  │     oRPC Extension                   │  │
│  │  • Auto-generate JSON-RPC methods    │  │
│  │  • Generate OpenRPC schema           │  │
│  │  • Execute via internal HTTP calls   │  │
│  └──────────────────────────────────────┘  │
│                    │                        │
│                    ▼                        │
│  ┌──────────────────────────────────────┐  │
│  │     JSON-RPC Endpoints               │  │
│  │  POST /rpc                           │  │
│  │  GET /rpc/schema (OpenRPC)           │  │
│  │  GET /rpc/methods                    │  │
│  └──────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
                    │
                    ▼
        ┌───────────────────────┐
        │   JSON-RPC Clients    │
        │   (Any language)      │
        └───────────────────────┘
```

## Comparison with Other Protocols

| Feature | REST | oRPC (JSON-RPC) | gRPC | GraphQL |
|---------|------|-----------------|------|---------|
| Transport | HTTP | HTTP | HTTP/2 | HTTP |
| Format | JSON/XML | JSON | Protobuf | JSON |
| Schema | OpenAPI | OpenRPC | .proto | SDL |
| Batching | ❌ | ✅ | ✅ (streaming) | ✅ |
| Type Safety | Via OpenAPI | Via OpenRPC | ✅ Native | ✅ Native |
| Browser Support | ✅ | ✅ | ⚠️ Limited | ✅ |
| Debugging | Easy | Easy | Harder | Easy |
| Auto-generation | ⚠️ | ✅ (Forge) | ✅ | ✅ |

## Best Practices

1. **Use Custom Method Names**: For public APIs, use semantic names via `WithORPCMethod()`
2. **Add Schema Annotations**: Document params/results with `WithORPCParams()` and `WithORPCResult()`
3. **Exclude Internal Routes**: Use patterns to exclude `/_/*` and `/internal/*`
4. **Enable OpenRPC**: Always enable schema generation for documentation
5. **Add Descriptions**: Use `forge.WithSummary()` and `forge.WithDescription()` on routes
6. **Use Method Prefix**: Namespace your methods with `WithMethodPrefix("api.")`
7. **Monitor Metrics**: Track method usage and error rates
8. **Version Your API**: Include version in prefix or method names

## License

MIT

## Contributing

Contributions welcome! Please submit PRs to the main Forge repository.

## Support

- [Documentation](https://forge.xraph.dev)
- [GitHub Issues](https://github.com/xraph/forge/issues)
- [Examples](../../examples/orpc_example)

---

Built with ❤️ by Dr. Ruby  
Part of the Forge framework

