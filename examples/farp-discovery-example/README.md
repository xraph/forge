# FARP + Discovery Integration Example

This example demonstrates the complete integration of FARP (Forge API Gateway Registration Protocol) with the Discovery extension.

## What This Example Shows

1. **Service Registration**: Service registers with discovery backend
2. **Automatic Schema Generation**: OpenAPI and AsyncAPI schemas auto-generated from routes
3. **Schema Publishing**: Schemas published to FARP registry
4. **Manifest Creation**: SchemaManifest created and registered
5. **Gateway Ready**: API gateways can discover and configure routes automatically

## Features Demonstrated

- ✅ Discovery extension with FARP enabled
- ✅ Automatic OpenAPI schema generation from HTTP routes
- ✅ Automatic AsyncAPI schema generation from streaming routes
- ✅ Schema publishing to registry (push strategy)
- ✅ Manifest registration with checksums
- ✅ Service capabilities advertisement
- ✅ Health and metrics endpoints

## Running the Example

```bash
cd /Users/rexraphael/Work/Web-Mobile/xraph/forge/examples/farp-discovery-example
go run main.go
```

## Expected Behavior

### On Startup

1. **Service Registration**:
   ```
   Service registered: user-service (localhost:8080)
   Tags: rest, production
   ```

2. **Schema Generation**:
   ```
   Generating OpenAPI schema from router...
   OpenAPI schema generated: 5 paths, 4 operations
   
   Generating AsyncAPI schema from router...
   AsyncAPI schema generated: 0 channels
   ```

3. **FARP Registration**:
   ```
   Publishing FARP schemas...
   - OpenAPI 3.1.0 published to /schemas/user-service/v1/openapi
   - AsyncAPI 3.0.0 published to /schemas/user-service/v1/asyncapi
   
   SchemaManifest registered:
   - Instance ID: user-service-abc123
   - Schemas: 2
   - Capabilities: rest, websocket
   - Checksum: a1b2c3d4...
   ```

4. **Server Started**:
   ```
   Server listening on :8080
   OpenAPI spec: http://localhost:8080/openapi.json
   AsyncAPI spec: http://localhost:8080/asyncapi.json
   ```

### Testing Endpoints

```bash
# List users
curl http://localhost:8080/users

# Create user
curl -X POST http://localhost:8080/users -H "Content-Type: application/json" -d '{"name":"Dave"}'

# Get user by ID
curl http://localhost:8080/users/1

# Delete user
curl -X DELETE http://localhost:8080/users/1

# Health check
curl http://localhost:8080/health

# OpenAPI spec
curl http://localhost:8080/openapi.json | jq

# AsyncAPI spec
curl http://localhost:8080/asyncapi.json | jq
```

## Configuration Explained

```go
discovery.FARPConfig{
    Enabled:      true,          // Enable FARP
    AutoRegister: true,          // Auto-register on startup
    Strategy:     "push",        // Push schemas to registry
    
    Schemas: []discovery.FARPSchemaConfig{
        {
            Type:        "openapi",         // Schema type
            SpecVersion: "3.1.0",          // OpenAPI version
            Location: {
                Type:         "registry",   // Store in registry
                RegistryPath: "/schemas/user-service/v1/openapi",
            },
            ContentType: "application/json",
        },
    },
    
    Endpoints: {
        Health:   "/health",
        Metrics:  "/metrics",
        OpenAPI:  "/openapi.json",
        AsyncAPI: "/asyncapi.json",
    },
    
    Capabilities: []string{"rest", "websocket"},
}
```

## API Gateway Integration

An API gateway can now discover this service:

```go
// Gateway side
import "github.com/xraph/forge/farp/gateway"

client := gateway.NewClient(registryBackend)

client.WatchServices(ctx, "", func(routes []gateway.ServiceRoute) {
    for _, route := range routes {
        // Auto-configure gateway routes
        fmt.Printf("Route: %s %s → %s\n", 
            route.Methods[0], route.Path, route.TargetURL)
        
        // Example output:
        // Route: GET /users → http://localhost:8080/users
        // Route: POST /users → http://localhost:8080/users
        // Route: GET /users/:id → http://localhost:8080/users/:id
        // Route: DELETE /users/:id → http://localhost:8080/users/:id
    }
})
```

## What Gets Registered

### Service Instance
```json
{
  "id": "user-service-abc123",
  "name": "user-service",
  "version": "v1.2.3",
  "address": "localhost",
  "port": 8080,
  "tags": ["rest", "production"],
  "status": "passing"
}
```

### FARP Schema Manifest
```json
{
  "version": "1.0.0",
  "service_name": "user-service",
  "service_version": "v1.2.3",
  "instance_id": "user-service-abc123",
  "schemas": [
    {
      "type": "openapi",
      "spec_version": "3.1.0",
      "location": {
        "type": "registry",
        "registry_path": "/schemas/user-service/v1/openapi"
      },
      "content_type": "application/json",
      "hash": "abc123...",
      "size": 45678
    },
    {
      "type": "asyncapi",
      "spec_version": "3.0.0",
      "location": {
        "type": "registry",
        "registry_path": "/schemas/user-service/v1/asyncapi"
      },
      "content_type": "application/json",
      "hash": "def456...",
      "size": 12345
    }
  ],
  "capabilities": ["rest", "websocket"],
  "endpoints": {
    "health": "/health",
    "metrics": "/metrics",
    "openapi": "/openapi.json",
    "asyncapi": "/asyncapi.json"
  },
  "updated_at": 1698768000,
  "checksum": "manifest-checksum..."
}
```

### OpenAPI Schema (Generated)
```json
{
  "openapi": "3.1.0",
  "info": {
    "title": "user-service",
    "version": "v1.2.3"
  },
  "paths": {
    "/users": {
      "get": {
        "summary": "List users",
        "tags": ["users"],
        "responses": { "200": { ... } }
      },
      "post": {
        "summary": "Create user",
        "tags": ["users"],
        "responses": { "201": { ... } }
      }
    },
    "/users/{id}": {
      "get": {
        "summary": "Get user by ID",
        "tags": ["users"],
        "parameters": [ ... ],
        "responses": { "200": { ... } }
      },
      "delete": {
        "summary": "Delete user",
        "tags": ["users"],
        "parameters": [ ... ],
        "responses": { "200": { ... } }
      }
    }
  }
}
```

## Benefits

### For This Service
- ✅ No manual API documentation needed
- ✅ Schemas stay in sync with code
- ✅ Automatic gateway registration
- ✅ Contract validation

### For API Gateways
- ✅ Auto-discover new services
- ✅ Auto-configure routes
- ✅ Real-time schema updates
- ✅ Health-aware routing

### For Organizations
- ✅ Centralized API catalog
- ✅ Consistent API governance
- ✅ Contract testing infrastructure
- ✅ API lifecycle management

## Next Steps

- Add more routes (PATCH, OPTIONS, etc.)
- Add WebSocket routes for AsyncAPI
- Configure different strategies (pull, hybrid)
- Use production backends (Consul, etcd)
- Integrate with actual API gateway (Kong, Traefik)

