# GraphQL Extension for Forge v2

Production-ready GraphQL server extension powered by [gqlgen](https://gqlgen.com/), providing type-safe GraphQL APIs with automatic code generation, schema management, and full observability support.

## Features

- ✅ **Type-safe GraphQL** - Automatic code generation from GraphQL schemas
- ✅ **Multiple transports** - HTTP (GET/POST), WebSocket subscriptions
- ✅ **Built-in optimization** - Query complexity limits, automatic persisted queries (APQ)
- ✅ **DataLoader support** - N+1 query optimization with batching and caching
- ✅ **Custom directives** - Authentication, authorization, and custom logic
- ✅ **Apollo Federation v2** - Microservices composition ready
- ✅ **File uploads** - Multipart form data support
- ✅ **GraphQL Playground** - Interactive query exploration
- ✅ **Full observability** - Logging, metrics, and tracing integration
- ✅ **DI integration** - Access to Forge services via dependency injection

## Installation

```bash
go get github.com/xraph/forge/extensions/graphql
go get github.com/99designs/gqlgen@v0.17.45
```

## Quick Start

### 1. Define Your GraphQL Schema

Create `schema/schema.graphql`:

```graphql
directive @auth(requires: String) on FIELD_DEFINITION

type Query {
  hello(name: String!): String!
  version: String!
}

type Mutation {
  echo(message: String!): String!
}
```

### 2. Generate Code

```bash
go run github.com/99designs/gqlgen generate
```

### 3. Implement Resolvers

Edit `schema.resolvers.go`:

```go
func (r *queryResolver) Hello(ctx context.Context, name string) (string, error) {
    r.logger.Debug("hello query called", forge.F("name", name))
    return fmt.Sprintf("Hello, %s!", name), nil
}
```

### 4. Register Extension

```go
package main

import (
    "context"
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/graphql"
)

func main() {
    // Create Forge app
    app := forge.NewApp(forge.DefaultAppConfig())

    // Register GraphQL extension
    gqlExt := graphql.NewExtension(
        graphql.WithEndpoint("/graphql"),
        graphql.WithPlayground(true),
        graphql.WithMaxComplexity(1000),
        graphql.WithQueryCache(true, 5*time.Minute),
    )
    
    if err := app.RegisterExtension(gqlExt); err != nil {
        log.Fatal(err)
    }

    // Start the app
    if err := app.Start(context.Background()); err != nil {
        log.Fatal(err)
    }

    // Run server
    if err := app.Run(context.Background(), ":8080"); err != nil {
        log.Fatal(err)
    }
}
```

### 5. Query Your API

```bash
# Query
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ hello(name: \"World\") }"}'

# Or visit the playground
open http://localhost:8080/playground
```

## Configuration

### Full Configuration Example

```go
config := graphql.DefaultConfig()
config.Endpoint = "/api/graphql"
config.PlaygroundEndpoint = "/api/playground"
config.EnablePlayground = true
config.EnableIntrospection = true

// Performance
config.MaxComplexity = 1000
config.MaxDepth = 15
config.QueryTimeout = 30 * time.Second

// DataLoader
config.EnableDataLoader = true
config.DataLoaderBatchSize = 100
config.DataLoaderWait = 10 * time.Millisecond

// Caching
config.EnableQueryCache = true
config.QueryCacheTTL = 5 * time.Minute
config.MaxCacheSize = 1000

// Security
config.EnableCORS = true
config.AllowedOrigins = []string{"https://example.com"}
config.MaxUploadSize = 10 * 1024 * 1024 // 10MB

// Observability
config.EnableMetrics = true
config.EnableTracing = true
config.EnableLogging = true
config.LogSlowQueries = true
config.SlowQueryThreshold = 1 * time.Second

ext := graphql.NewExtensionWithConfig(config)
```

### Environment Variables/Config File

```yaml
# config.yaml
extensions:
  graphql:
    endpoint: "/graphql"
    playground_endpoint: "/playground"
    enable_playground: true
    enable_introspection: true
    max_complexity: 1000
    max_depth: 15
    query_timeout: 30s
    enable_dataloader: true
    dataloader_batch_size: 100
    dataloader_wait: 10ms
    enable_query_cache: true
    query_cache_ttl: 5m
    max_cache_size: 1000
    enable_cors: true
    allowed_origins:
      - "https://example.com"
    max_upload_size: 10485760
    enable_metrics: true
    enable_tracing: true
    enable_logging: true
    log_slow_queries: true
    slow_query_threshold: 1s
```

## Advanced Features

### DataLoader for N+1 Query Optimization

```go
import "github.com/xraph/forge/extensions/graphql/dataloader"

// Create a loader
config := dataloader.DefaultLoaderConfig()
loader := dataloader.NewLoader(config, func(ctx context.Context, keys []interface{}) ([]interface{}, []error) {
    // Batch load users by IDs
    ids := make([]int, len(keys))
    for i, key := range keys {
        ids[i] = key.(int)
    }
    
    users, err := db.GetUsersByIDs(ctx, ids)
    if err != nil {
        errs := make([]error, len(keys))
        for i := range errs {
            errs[i] = err
        }
        return nil, errs
    }
    
    // Return in same order as keys
    results := make([]interface{}, len(keys))
    for i, user := range users {
        results[i] = user
    }
    return results, make([]error, len(keys))
})

// Use in resolver
func (r *userResolver) Friends(ctx context.Context, obj *User) ([]*User, error) {
    friends, err := loader.LoadMany(ctx, obj.FriendIDs)
    if err != nil {
        return nil, err
    }
    return friends.([]*User), nil
}
```

### Custom Directives

Define in schema:

```graphql
directive @auth(requires: String) on FIELD_DEFINITION

type Query {
  adminOnly: String! @auth(requires: "admin")
  user: String! @auth
}
```

Implement directive:

```go
import "github.com/xraph/forge/extensions/graphql/directives"

// Configure in gqlgen.yml
// directives:
//   auth:
//     skip_runtime: false

// Use built-in auth directive or implement custom
func (r *Resolver) Directive() generated.DirectiveRoot {
    return generated.DirectiveRoot{
        Auth: directives.Auth,
    }
}
```

### Apollo Federation v2

Configure in `gqlgen.yml`:

```yaml
federation:
  filename: generated/federation.go
  package: generated
  version: 2
```

Define federated schema:

```graphql
extend schema @link(url: "https://specs.apollo.dev/federation/v2.0", import: ["@key", "@external"])

type User @key(fields: "id") {
  id: ID!
  name: String!
}

extend type Product @key(fields: "id") {
  id: ID! @external
  reviews: [Review!]!
}
```

### File Uploads

```graphql
scalar Upload

type Mutation {
  uploadFile(file: Upload!): String!
}
```

```go
func (r *mutationResolver) UploadFile(ctx context.Context, file graphql.Upload) (string, error) {
    content, err := io.ReadAll(file.File)
    if err != nil {
        return "", err
    }
    
    // Process file...
    return fmt.Sprintf("Uploaded %s (%d bytes)", file.Filename, len(content)), nil
}
```

## Dependency Injection

Access Forge services in resolvers:

```go
type Resolver struct {
    container forge.Container
    logger    forge.Logger
    metrics   forge.Metrics
    config    Config
}

func (r *queryResolver) GetData(ctx context.Context) (*Data, error) {
    // Resolve database from DI using helper function
    db, err := database.GetDatabase(r.container)
    if err != nil {
        return nil, err
    }
    
    // Use services
    r.logger.Info("fetching data")
    r.metrics.Counter("data_requests").Inc()
    
    return db.QueryData(ctx)
}
```

## Observability

### Logging

```go
// Automatic operation logging
[DEBUG] graphql operation start {operation: "GetUser", type: "query"}
[DEBUG] graphql operation complete {operation: "GetUser", duration: "15ms"}

// Slow query logging
[WARN] slow query detected {operation: "ComplexQuery", duration: "1.5s", query: "..."}

// Error logging
[ERROR] graphql error {operation: "UpdateUser", error: "validation failed", path: ["updateUser"]}
```

### Metrics

Automatically collected:
- `graphql_operation_duration_seconds` - Operation latency histogram
- `graphql_operation_total` - Total operations counter

Both tagged with:
- `operation` - Operation name
- `type` - query, mutation, or subscription

### Tracing

Full distributed tracing support via OpenTelemetry integration.

## Testing

```go
func TestGraphQLQuery(t *testing.T) {
    container := forge.NewContainer()
    logger := forge.NewNoopLogger()
    metrics := forge.NewNoOpMetrics()
    config := graphql.DefaultConfig()

    server, err := graphql.NewGraphQLServer(config, logger, metrics, container)
    if err != nil {
        t.Fatal(err)
    }

    handler := server.HTTPHandler()

    query := `{"query":"{ hello(name: \"Test\") }"}`
    req := httptest.NewRequest("POST", "/graphql", strings.NewReader(query))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    
    handler.ServeHTTP(w, req)

    if w.Code != 200 {
        t.Errorf("expected 200, got %d", w.Code)
    }
}
```

## Performance Best Practices

1. **Enable Query Caching** - Use APQ for frequently executed queries
2. **Set Complexity Limits** - Prevent resource exhaustion
3. **Use DataLoader** - Batch and cache data loading
4. **Enable Compression** - Reduce bandwidth
5. **Monitor Slow Queries** - Optimize with logging and tracing

## Troubleshooting

### Schema Changes Not Reflected

Regenerate code after schema changes:

```bash
cd v2/extensions/graphql
go run github.com/99designs/gqlgen generate
```

### Type Conflicts

Rename conflicting types in `graphql.go` or use autobind in `gqlgen.yml`:

```yaml
autobind:
  - github.com/xraph/forge/extensions/graphql/models
```

### High Memory Usage

- Reduce `MaxCacheSize`
- Lower `DataLoaderBatchSize`
- Set reasonable `MaxComplexity`

## Examples

See `v2/examples/graphql-*/` for complete examples:
- `graphql-basic/` - Simple query and mutation
- `graphql-auth/` - Authentication and authorization
- `graphql-dataloader/` - DataLoader optimization
- `graphql-federation/` - Microservices composition
- `graphql-subscriptions/` - Real-time subscriptions

## API Reference

### Extension Methods

- `NewExtension(opts ...ConfigOption) forge.Extension`
- `NewExtensionWithConfig(config Config) forge.Extension`

### Config Options

- `WithEndpoint(endpoint string)`
- `WithPlayground(enable bool)`
- `WithIntrospection(enable bool)`
- `WithMaxComplexity(max int)`
- `WithMaxDepth(max int)`
- `WithTimeout(timeout time.Duration)`
- `WithDataLoader(enable bool)`
- `WithQueryCache(enable bool, ttl time.Duration)`
- `WithCORS(origins ...string)`
- `WithMetrics(enable bool)`
- `WithTracing(enable bool)`
- `WithRequireConfig(require bool)`
- `WithConfig(config Config)`

## License

Part of the Forge framework - see main repository for license.

