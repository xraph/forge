# Router Adapters

This package provides router adapters for different routing backends.

## Available Adapters

### 1. BunRouter (Recommended)
**Default adapter** - High performance, modern API

```go
import (
    "github.com/xraph/forge/v2"
    "github.com/xraph/forge/v2/extras"
)

router := forge.NewRouter(
    forge.WithAdapter(extras.NewBunRouterAdapter()),
    forge.WithContainer(container),
)
```

**Features:**
- Fast route matching
- Modern API design
- Path parameters: `/users/:id`
- Wildcard routes: `/files/*`

### 2. Chi Router
Clean, composable router from go-chi

```go
router := forge.NewRouter(
    forge.WithAdapter(extras.NewChiAdapter()),
    forge.WithContainer(container),
)
```

**Features:**
- Middleware-first design
- Sub-router mounting
- Path parameters: `/users/{id}`

### 3. HTTPRouter
Extremely fast router from julienschmidt

```go
router := forge.NewRouter(
    forge.WithAdapter(extras.NewHTTPRouterAdapter()),
    forge.WithContainer(container),
)
```

**Features:**
- Fastest route matching
- Minimal allocations
- Path parameters: `/users/:id`

## Default Behavior

If no adapter is specified, the router uses a **simple in-memory adapter** for basic functionality. For production use, explicitly specify one of the above adapters.

## Choosing an Adapter

| Adapter | Performance | Features | Use Case |
|---------|-------------|----------|----------|
| **BunRouter** | ⚡⚡⚡ High | Modern, full-featured | **Recommended for most apps** |
| Chi | ⚡⚡ Good | Middleware-focused | Apps with complex middleware |
| HTTPRouter | ⚡⚡⚡ Highest | Minimal, fast | High-throughput APIs |

## Example

```go
package main

import (
    "net/http"
    "github.com/xraph/forge/v2"
    "github.com/xraph/forge/v2/extras"
)

func main() {
    // Create container
    container := forge.NewContainer()
    
    // Create router with BunRouter adapter
    router := forge.NewRouter(
        forge.WithAdapter(extras.NewBunRouterAdapter()),
        forge.WithContainer(container),
        forge.WithRecovery(),
    )
    
    // Register routes
    router.GET("/", func(ctx forge.Context) error {
        return ctx.String(200, "Hello from BunRouter!")
    })
    
    router.GET("/users/:id", func(ctx forge.Context) error {
        id := ctx.Param("id")
        return ctx.JSON(200, map[string]string{
            "id": id,
            "name": "User " + id,
        })
    })
    
    // Start server
    http.ListenAndServe(":8080", router)
}
```

