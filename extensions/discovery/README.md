## Service Discovery Extension

Enterprise-grade service discovery and registry for distributed Forge applications.

## Features

- ✅ **Multiple Backends** - Consul, etcd, Kubernetes, Eureka, in-memory
- ✅ **Service Registration** - Automatic service registration on startup
- ✅ **Health Checks** - Continuous health monitoring
- ✅ **Service Discovery** - Find services by name, tags, health status
- ✅ **Load Balancing** - Round-robin, random, weighted strategies
- ✅ **Service Watching** - Real-time notifications of service changes
- ✅ **Auto-Deregistration** - Automatic cleanup on shutdown

## Installation

```bash
go get github.com/xraph/forge/extensions/discovery
```

## Quick Start

### Basic Usage

```go
package main

import (
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/discovery"
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Extensions: []forge.Extension{
            discovery.NewExtension(
                discovery.WithEnabled(true),
                discovery.WithBackend("memory"),
                discovery.WithServiceName("my-api"),
                discovery.WithServiceAddress("localhost", 8080),
                discovery.WithServiceTags("api", "v1"),
                discovery.WithHTTPHealthCheck("http://localhost:8080/_/health", 10*time.Second),
            ),
        },
    })

    // Get discovery service
    discoveryService := forge.Must[*discovery.Service](app.Container(), "discovery.Service")

    // Discover other services
    router := app.Router()
    router.GET("/call-user-service", func(ctx forge.Context) error {
        // Find a healthy user-service instance
        instance, err := discoveryService.SelectInstance(
            ctx.Context(),
            "user-service",
            discovery.LoadBalanceRoundRobin,
        )
        if err != nil {
            return err
        }

        // Make request to selected instance
        url := instance.URL("http") + "/users"
        resp, err := http.Get(url)
        // ...
    })

    app.Run()
}
```

## Configuration

### YAML Configuration

```yaml
extensions:
  discovery:
    enabled: true
    backend: "consul"

    # Service configuration
    service:
      name: "my-api"
      version: "1.0.0"
      address: "10.0.1.5"
      port: 8080
      tags:
        - "api"
        - "v1"
      metadata:
        region: "us-east-1"
        environment: "production"
      enable_auto_deregister: true

    # Health check configuration
    health_check:
      enabled: true
      interval: 10s
      timeout: 5s
      deregister_critical_service_after: 1m
      http: "http://localhost:8080/_/health"

    # Watch configuration
    watch:
      enabled: true
      services:
        - "user-service"
        - "payment-service"
      tags:
        - "production"

    # Consul backend
    consul:
      address: "consul.company.com:8500"
      token: "${CONSUL_TOKEN}"
      datacenter: "dc1"
      tls_enabled: true
```

### Programmatic Configuration

```go
discovery.NewExtension(
    discovery.WithEnabled(true),
    discovery.WithConsul("consul.company.com:8500", os.Getenv("CONSUL_TOKEN")),
    discovery.WithService(discovery.ServiceConfig{
        Name:     "my-api",
        Version:  "1.0.0",
        Address:  "10.0.1.5",
        Port:     8080,
        Tags:     []string{"api", "v1"},
        Metadata: map[string]string{
            "region": "us-east-1",
        },
    }),
    discovery.WithHTTPHealthCheck("http://localhost:8080/_/health", 10*time.Second),
)
```

## Usage Examples

### Service Registration

```go
// Automatic registration on app startup
// Service is automatically registered when extension starts

// Manual registration
instance := &discovery.ServiceInstance{
    ID:       "my-api-1",
    Name:     "my-api",
    Version:  "1.0.0",
    Address:  "10.0.1.5",
    Port:     8080,
    Tags:     []string{"api"},
    Metadata: map[string]string{"region": "us-east-1"},
}

err := service.Register(ctx, instance)
```

### Service Discovery

```go
// Discover all instances of a service
instances, err := service.Discover(ctx, "user-service")
if err != nil {
    return err
}

for _, instance := range instances {
    fmt.Printf("Found: %s at %s:%d\n", instance.ID, instance.Address, instance.Port)
}
```

### Discover with Tags

```go
// Discover only services with specific tags
instances, err := service.DiscoverWithTags(ctx, "user-service", []string{"production", "v2"})
```

### Discover Healthy Services

```go
// Discover only healthy instances
instances, err := service.DiscoverHealthy(ctx, "user-service")
```

### Load Balancing

```go
// Select a single instance using round-robin
instance, err := service.SelectInstance(
    ctx,
    "user-service",
    discovery.LoadBalanceRoundRobin,
)

// Get service URL
url, err := service.GetServiceURL(
    ctx,
    "user-service",
    "http",
    discovery.LoadBalanceRandom,
)
```

### Service Watching

```go
// Watch for service changes
err := service.Watch(ctx, "user-service", func(instances []*discovery.ServiceInstance) {
    logger.Info("user-service instances changed",
        forge.F("count", len(instances)),
    )

    // Update load balancer pool
    updatePool(instances)
})
```

## Backends

### Memory Backend (Development)

```go
discovery.NewExtension(
    discovery.WithBackend("memory"),
)
```

### Consul Backend

```go
discovery.NewExtension(
    discovery.WithConsul("consul.company.com:8500", os.Getenv("CONSUL_TOKEN")),
)
```

### etcd Backend

```go
discovery.NewExtension(
    discovery.WithEtcd([]string{
        "etcd1.company.com:2379",
        "etcd2.company.com:2379",
        "etcd3.company.com:2379",
    }),
)
```

### Kubernetes Backend

```go
discovery.NewExtension(
    discovery.WithKubernetes("default", true), // namespace, in-cluster
)
```

### Eureka Backend

```go
discovery.NewExtension(
    discovery.WithEureka([]string{
        "http://eureka1.company.com:8761/eureka",
        "http://eureka2.company.com:8761/eureka",
    }),
)
```

## Load Balancing Strategies

### Round Robin

```go
// Distribute requests evenly across instances
instance, err := service.SelectInstance(ctx, "user-service", discovery.LoadBalanceRoundRobin)
```

### Random

```go
// Random selection for simple load distribution
instance, err := service.SelectInstance(ctx, "user-service", discovery.LoadBalanceRandom)
```

### Least Connections

```go
// Select instance with fewest active connections
instance, err := service.SelectInstance(ctx, "user-service", discovery.LoadBalanceLeastConnections)
```

## Health Checks

### HTTP Health Check

```yaml
health_check:
  enabled: true
  interval: 10s
  timeout: 5s
  http: "http://localhost:8080/_/health"
```

### TCP Health Check

```yaml
health_check:
  enabled: true
  interval: 10s
  timeout: 5s
  tcp: "localhost:8080"
```

### gRPC Health Check

```yaml
health_check:
  enabled: true
  interval: 10s
  timeout: 5s
  grpc: "localhost:9090"
```

## Service Metadata

```go
// Register service with metadata
instance := &discovery.ServiceInstance{
    Name: "my-api",
    Metadata: map[string]string{
        "version":     "1.0.0",
        "region":      "us-east-1",
        "environment": "production",
        "datacenter":  "dc1",
    },
}

// Query metadata
if region, ok := instance.GetMetadata("region"); ok {
    fmt.Printf("Service is in region: %s\n", region)
}
```

## Client-Side Load Balancing

```go
// Create HTTP client with service discovery
type DiscoveryClient struct {
    service     *discovery.Service
    serviceName string
    strategy    discovery.LoadBalanceStrategy
}

func (c *DiscoveryClient) Get(ctx context.Context, path string) (*http.Response, error) {
    url, err := c.service.GetServiceURL(ctx, c.serviceName, "http", c.strategy)
    if err != nil {
        return nil, err
    }

    return http.Get(url + path)
}

// Usage
client := &DiscoveryClient{
    service:     discoveryService,
    serviceName: "user-service",
    strategy:    discovery.LoadBalanceRoundRobin,
}

resp, err := client.Get(ctx, "/users/123")
```

## Service Tags

```go
// Register with tags
service.Register(ctx, &discovery.ServiceInstance{
    Name: "api",
    Tags: []string{"production", "v2", "us-east"},
})

// Discover by tags
instances, err := service.DiscoverWithTags(ctx, "api", []string{"production", "v2"})
```

## Best Practices

### 1. Use Health Checks

```go
// ✅ Always enable health checks
discovery.WithHTTPHealthCheck("http://localhost:8080/_/health", 10*time.Second)

// ❌ Don't disable health checks in production
```

### 2. Include Meaningful Tags

```go
// ✅ Good tags
Tags: []string{"production", "v2", "us-east-1", "canary"}

// ❌ Bad tags
Tags: []string{"server", "app"}
```

### 3. Auto-Deregister on Shutdown

```go
// ✅ Always enable auto-deregister
service:
  enable_auto_deregister: true
```

### 4. Use Metadata for Configuration

```go
// ✅ Store configuration in metadata
Metadata: map[string]string{
    "max_connections": "1000",
    "timeout": "30s",
    "protocol": "http2",
}
```

### 5. Watch Critical Services

```go
// ✅ Watch dependencies for changes
watch:
  enabled: true
  services:
    - "database"
    - "cache"
    - "payment-gateway"
```

## Patterns

### Circuit Breaker Integration

```go
// Combine with circuit breaker for resilience
circuitBreaker := NewCircuitBreaker(options)

instance, err := discoveryService.SelectInstance(ctx, "user-service", strategy)
if err != nil {
    return err
}

return circuitBreaker.Call(func() error {
    return callService(instance)
})
```

### Retry with Different Instance

```go
func callWithRetry(ctx context.Context, serviceName string, maxRetries int) error {
    for i := 0; i < maxRetries; i++ {
        instance, err := service.SelectInstance(ctx, serviceName, discovery.LoadBalanceRandom)
        if err != nil {
            return err
        }

        if err := callService(instance); err == nil {
            return nil
        }

        logger.Warn("call failed, retrying with different instance",
            forge.F("attempt", i+1),
        )
    }
    return fmt.Errorf("all retries failed")
}
```

### Dynamic Service Pool

```go
type ServicePool struct {
    instances []*discovery.ServiceInstance
    mu        sync.RWMutex
}

// Watch and update pool
service.Watch(ctx, "user-service", func(instances []*discovery.ServiceInstance) {
    pool.mu.Lock()
    defer pool.mu.Unlock()
    pool.instances = instances
})
```

## Testing

```go
func TestServiceDiscovery(t *testing.T) {
    // Use memory backend for testing
    backend := discovery.NewMemoryBackend()
    service := discovery.NewService(backend, logger)

    // Register test service
    instance := &discovery.ServiceInstance{
        ID:      "test-1",
        Name:    "test-service",
        Address: "localhost",
        Port:    8080,
        Status:  discovery.HealthStatusPassing,
    }
    service.Register(context.Background(), instance)

    // Discover
    instances, err := service.Discover(context.Background(), "test-service")
    assert.NoError(t, err)
    assert.Len(t, instances, 1)
}
```

## Monitoring

```go
// Expose service discovery metrics
router.GET("/_/discovery/services", func(ctx forge.Context) error {
    services, err := discoveryService.ListServices(ctx.Context())
    if err != nil {
        return err
    }

    return ctx.JSON(200, map[string]interface{}{
        "services": services,
    })
})

router.GET("/_/discovery/instances/:service", func(ctx forge.Context) error {
    serviceName := ctx.Param("service")
    instances, err := discoveryService.Discover(ctx.Context(), serviceName)
    if err != nil {
        return err
    }

    return ctx.JSON(200, map[string]interface{}{
        "service":   serviceName,
        "instances": instances,
    })
})
```

## Performance Considerations

- **Caching**: Service discovery results are cached by backends
- **Health Checks**: Balance frequency vs. load (10s recommended)
- **Watch Updates**: Debounce rapid changes to avoid thrashing
- **Connection Pooling**: Reuse connections to backend (Consul, etcd)

## Migration Guide

### From Hardcoded URLs

```go
// Before
userServiceURL := "http://user-service:8080"

// After
url, err := discoveryService.GetServiceURL(ctx, "user-service", "http", strategy)
```

### From Environment Variables

```go
// Before
services := map[string]string{
    "user-service": os.Getenv("USER_SERVICE_URL"),
    "payment":      os.Getenv("PAYMENT_SERVICE_URL"),
}

// After
// Services discovered automatically via service discovery
```

## License

MIT License - see LICENSE file for details

