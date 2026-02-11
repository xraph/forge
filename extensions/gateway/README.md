# Forge Gateway Extension

Production-grade API gateway extension for [Forge](https://github.com/xraph/forge) that turns any Forge application into a feature-complete reverse proxy with automatic service discovery, multi-protocol support, and an admin dashboard.

## Features

- **Multi-protocol proxying**: HTTP, WebSocket, SSE (Server-Sent Events), gRPC (unary + streaming)
- **Automatic service discovery**: FARP-based schema-driven route generation from OpenAPI, AsyncAPI, GraphQL descriptors
- **Manual route configuration**: Static routes via config file or admin API
- **Load balancing**: Round-robin, weighted round-robin, random, least-connections, consistent hash
- **Circuit breakers**: Per-target three-state (closed/open/half-open) circuit breakers
- **Health monitoring**: Active HTTP probes + passive failure tracking with configurable thresholds
- **Rate limiting**: Token-bucket algorithm (global, per-route, per-client)
- **Retry with backoff**: Exponential, linear, fixed backoff with jitter and retry budgets
- **Traffic splitting**: Canary, blue-green, A/B testing, shadow/mirror traffic
- **Authentication**: API key, Bearer token, forward auth -- integrates with Forge auth extension
- **Response caching**: In-memory or external cache store, per-route policies, Cache-Control respect
- **TLS/mTLS**: Upstream TLS with CA certs, client certs for mTLS, auto cert reloading
- **Request/response transformation**: Path rewriting, header manipulation, prefix stripping
- **CORS and IP filtering**: Gateway-level security policies
- **Observability**: Prometheus metrics, structured access logging, OpenTelemetry trace propagation
- **Admin dashboard**: Real-time ForgeUI-based dashboard with routes, upstreams, stats, and service discovery views
- **Admin REST API**: Full CRUD for routes, upstreams, stats, and configuration
- **Hot-reload**: Configuration changes applied without restart
- **OpenAPI aggregation**: Unified OpenAPI spec from all upstream services via FARP, Swagger UI, per-service specs
- **Hook system**: Extensible OnRequest/OnResponse/OnError/OnRouteChange/OnUpstreamHealth hooks

## Quick Start

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/gateway"
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Name:    "api-gateway",
        Version: "1.0.0",
        Extensions: []forge.Extension{
            gateway.NewExtension(
                gateway.WithRoute(gateway.RouteConfig{
                    Path:    "/users/*",
                    Targets: []gateway.TargetConfig{
                        {URL: "http://user-service:8080", Weight: 1},
                    },
                    StripPrefix: true,
                    Protocol:    gateway.ProtocolHTTP,
                    Enabled:     true,
                }),
                gateway.WithRoute(gateway.RouteConfig{
                    Path:    "/orders/*",
                    Targets: []gateway.TargetConfig{
                        {URL: "http://order-service:8080", Weight: 1},
                    },
                    StripPrefix: true,
                    Protocol:    gateway.ProtocolHTTP,
                    Enabled:     true,
                }),
            ),
        },
    })

    app.Run()
}
```

## With FARP Auto-Discovery

```go
app := forge.NewApp(forge.AppConfig{
    Name: "api-gateway",
    Extensions: []forge.Extension{
        // Discovery extension enables FARP
        discovery.NewExtension(
            discovery.WithEnabled(true),
            discovery.WithBackend("consul"),
        ),
        // Gateway auto-discovers services
        gateway.NewExtension(
            gateway.WithDiscoveryEnabled(true),
            gateway.WithDashboardEnabled(true),
        ),
    },
})
```

## Configuration

### Programmatic Configuration

```go
gateway.NewExtension(
    gateway.WithBasePath("/api"),
    gateway.WithLoadBalancing(gateway.LoadBalancingConfig{
        Strategy: gateway.LBWeightedRoundRobin,
    }),
    gateway.WithCircuitBreaker(gateway.CircuitBreakerConfig{
        Enabled:          true,
        FailureThreshold: 5,
        ResetTimeout:     30 * time.Second,
    }),
    gateway.WithRateLimiting(gateway.RateLimitConfig{
        Enabled:        true,
        RequestsPerSec: 1000,
        Burst:          100,
        PerClient:      true,
    }),
    gateway.WithHealthCheck(gateway.HealthCheckConfig{
        Enabled:  true,
        Interval: 10 * time.Second,
        Path:     "/health",
    }),
)
```

### File-based Configuration (YAML/JSON)

The gateway loads config from Forge's ConfigManager under the `gateway` key:

```yaml
gateway:
  enabled: true
  basePath: "/api"
  routes:
    - path: "/users/*"
      targets:
        - url: "http://user-service:8080"
          weight: 1
      stripPrefix: true
      protocol: http
      enabled: true
  circuitBreaker:
    enabled: true
    failureThreshold: 5
    resetTimeout: 30s
  rateLimiting:
    enabled: true
    requestsPerSec: 1000
    burst: 100
  healthCheck:
    enabled: true
    interval: 10s
    path: "/health"
  discovery:
    enabled: true
    watchMode: true
    autoPrefix: true
    prefixTemplate: "/{{.ServiceName}}"
  dashboard:
    enabled: true
    basePath: "/gateway"
    realtime: true
```

## Admin API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/gateway/api/routes` | List all routes |
| GET | `/gateway/api/routes/:id` | Get route details |
| POST | `/gateway/api/routes` | Create manual route |
| PUT | `/gateway/api/routes/:id` | Update route |
| DELETE | `/gateway/api/routes/:id` | Delete manual route |
| POST | `/gateway/api/routes/:id/enable` | Enable route |
| POST | `/gateway/api/routes/:id/disable` | Disable route |
| GET | `/gateway/api/upstreams` | List all targets |
| GET | `/gateway/api/stats` | Gateway statistics |
| GET | `/gateway/api/stats/routes` | Per-route statistics |
| GET | `/gateway/api/config` | Current configuration |
| GET | `/gateway/api/discovery/services` | Discovered services |
| POST | `/gateway/api/discovery/refresh` | Force FARP re-scan |
| GET | `/gateway/openapi.json` | Aggregated OpenAPI spec |
| GET | `/gateway/swagger` | Swagger UI |
| GET | `/gateway/api/openapi/services` | OpenAPI service listing |
| GET | `/gateway/api/openapi/services/:service` | Per-service OpenAPI spec |
| POST | `/gateway/api/openapi/refresh` | Force OpenAPI re-fetch |
| WS | `/gateway/ws` | Real-time event stream |

## Hook System

```go
ext := gateway.NewExtension()

// Access hooks after extension start
app.AfterStart(func() {
    gw := forge.Must[*gateway.Extension](app.Container(), "gateway")

    gw.Hooks().OnRequest(func(r *http.Request, route *gateway.Route) error {
        // Add custom auth header
        r.Header.Set("X-Gateway-Auth", "validated")
        return nil
    })

    gw.Hooks().OnResponse(func(resp *http.Response, route *gateway.Route) {
        // Add custom response header
        resp.Header.Set("X-Served-By", "forge-gateway")
    })

    gw.Hooks().OnError(func(err error, route *gateway.Route, w http.ResponseWriter) {
        // Custom error page
        log.Printf("Gateway error on route %s: %v", route.Path, err)
    })
})
```

## Architecture

```
Client Request
    │
    ├── IP Filter
    ├── CORS
    ├── Rate Limiter (global)
    ├── Route Matcher
    ├── Rate Limiter (per-route)
    ├── Authentication (API key/Bearer/forward auth)
    ├── Response Cache (check)
    ├── Request Hooks
    ├── Traffic Splitter
    ├── Load Balancer
    ├── Circuit Breaker Check
    ├── TLS/mTLS (per-target)
    ├── Protocol Proxy (HTTP/WS/SSE/gRPC)
    │   ├── Path Rewriting
    │   ├── Header Manipulation
    │   └── Upstream Connection
    ├── Response Cache (store)
    ├── Response Hooks
    ├── Access Logger
    └── Client Response
```

## OpenAPI Aggregation

The gateway automatically discovers and aggregates OpenAPI specifications from all upstream services that publish their schemas via FARP. This provides a single, unified API documentation view for all services behind the gateway.

### How It Works

1. **Discovery**: Services register with FARP and expose their OpenAPI spec via metadata (`farp.openapi` or `farp.openapi.path`)
2. **Fetching**: The gateway periodically fetches OpenAPI specs from all discovered services
3. **Merging**: Specs are merged into a unified OpenAPI 3.1.0 document with service-level tags and namespaced schemas
4. **Serving**: The aggregated spec is available at `/gateway/openapi.json` and browsable via Swagger UI at `/gateway/swagger`

### Configuration

```go
gateway.NewExtension(
    gateway.WithOpenAPI(gateway.OpenAPIConfig{
        Enabled:         true,
        Path:            "/openapi.json",
        UIPath:          "/swagger",
        Title:           "My API Gateway",
        Description:     "All services behind the gateway",
        Version:         "1.0.0",
        RefreshInterval: 30 * time.Second,
        FetchTimeout:    10 * time.Second,
        MergeStrategy:   "prefix",           // "prefix" or "flat"
        IncludeGatewayRoutes: true,           // Include gateway admin API
        ExcludeServices: []string{"internal"},// Services to skip
    }),
)
```

### YAML Configuration

```yaml
gateway:
  openapi:
    enabled: true
    path: "/openapi.json"
    uiPath: "/swagger"
    title: "My API Gateway"
    refreshInterval: 30s
    mergeStrategy: prefix
    includeGatewayRoutes: true
    excludeServices:
      - internal-admin
```

### Per-Service Specs

Individual service specs are available at `/gateway/api/openapi/services/:serviceName`, allowing you to access each service's original spec independently.

## ForgeUI Dashboard

The gateway includes a built-in admin dashboard that can run standalone or be mounted into the `dashboard` extension:

- **Overview**: Real-time stats (requests, errors, latency, health)
- **Routes**: Sortable table with path, protocol, source, targets, status
- **Upstreams**: Health matrix, circuit breaker states, connection counts
- **Services**: Discovered FARP services with schema types

The dashboard uses ForgeUI (gomponents + Alpine.js + Tailwind CSS) for a fully server-rendered, interactive UI with WebSocket real-time updates.

## License

Same license as the Forge framework.
