# Distributed Deployment Guide

## Overview

This guide provides instructions for deploying the Forge v2 streaming extension in a distributed, multi-node configuration with authentication, message filtering, rate limiting, load balancing, and cross-node coordination.

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Node 1     │     │   Node 2     │     │   Node 3     │
│              │     │              │     │              │
│  ┌────────┐  │     │  ┌────────┐  │     │  ┌────────┐  │
│  │Manager │  │     │  │Manager │  │     │  │Manager │  │
│  └───┬────┘  │     │  └───┬────┘  │     │  └───┬────┘  │
│      │       │     │      │       │     │      │       │
└──────┼───────┘     └──────┼───────┘     └──────┼───────┘
       │                    │                    │
       └────────────────────┼────────────────────┘
                            │
                   ┌────────▼────────┐
                   │   Coordinator   │
                   │  (Redis/NATS)   │
                   └─────────────────┘
```

## Prerequisites

### Infrastructure
- **Compute**: 3+ nodes (recommended minimum)
- **Load Balancer**: NGINX, HAProxy, or cloud load balancer
- **Redis Cluster** (for Redis coordinator) OR **NATS Cluster** (for NATS coordinator)
- **Network**: Low-latency network between nodes

### Software
- Go 1.21+
- Redis 7.0+ (if using Redis coordinator)
- NATS 2.10+ with JetStream (if using NATS coordinator)

## Setup

### 1. Redis Coordinator Setup

#### 1.1 Redis Cluster Deployment

**Using Docker Compose**:

```yaml
version: '3.8'

services:
  redis-node-1:
    image: redis:7-alpine
    command: redis-server --cluster-enabled yes --cluster-config-file nodes.conf --cluster-node-timeout 5000 --appendonly yes
    ports:
      - "7001:6379"
    volumes:
      - redis-1:/data

  redis-node-2:
    image: redis:7-alpine
    command: redis-server --cluster-enabled yes --cluster-config-file nodes.conf --cluster-node-timeout 5000 --appendonly yes
    ports:
      - "7002:6379"
    volumes:
      - redis-2:/data

  redis-node-3:
    image: redis:7-alpine
    command: redis-server --cluster-enabled yes --cluster-config-file nodes.conf --cluster-node-timeout 5000 --appendonly yes
    ports:
      - "7003:6379"
    volumes:
      - redis-3:/data

volumes:
  redis-1:
  redis-2:
  redis-3:
```

**Initialize Cluster**:

```bash
redis-cli --cluster create \
  127.0.0.1:7001 \
  127.0.0.1:7002 \
  127.0.0.1:7003 \
  --cluster-replicas 0
```

#### 1.2 Application Configuration

```go
package main

import (
    "github.com/redis/go-redis/v9"
    "github.com/xraph/forge/extensions/streaming"
    "github.com/xraph/forge/extensions/streaming/coordinator"
)

func main() {
    // Redis client
    redisClient := redis.NewClusterClient(&redis.ClusterOptions{
        Addrs: []string{
            "redis-node-1:6379",
            "redis-node-2:6379",
            "redis-node-3:6379",
        },
    })

    // Create coordinator
    coord := coordinator.NewRedisCoordinator(redisClient, "node-1")

    // Configure streaming extension
    config := streaming.Config{
        NodeID: "node-1",
        Coordination: streaming.CoordinationConfig{
            Enabled:       true,
            Backend:       "redis",
            URLs:          []string{"redis-node-1:6379", "redis-node-2:6379"},
            PresenceSync:  true,
            RoomStateSync: true,
            SyncInterval:  30 * time.Second,
        },
    }

    // Create extension with coordinator
    streamExt := streaming.NewExtension(config)
    // Set coordinator on manager after initialization
}
```

### 2. NATS Coordinator Setup

#### 2.1 NATS Cluster Deployment

**Using Docker Compose**:

```yaml
version: '3.8'

services:
  nats-1:
    image: nats:latest
    command: >
      -js
      -cluster nats://0.0.0.0:6222
      -routes nats://nats-2:6222,nats://nats-3:6222
    ports:
      - "4222:4222"
      - "6222:6222"
      - "8222:8222"
    volumes:
      - nats-1:/data

  nats-2:
    image: nats:latest
    command: >
      -js
      -cluster nats://0.0.0.0:6222
      -routes nats://nats-1:6222,nats://nats-3:6222
    ports:
      - "4223:4222"
      - "6223:6222"
      - "8223:8222"
    volumes:
      - nats-2:/data

  nats-3:
    image: nats:latest
    command: >
      -js
      -cluster nats://0.0.0.0:6222
      -routes nats://nats-1:6222,nats://nats-2:6222
    ports:
      - "4224:4222"
      - "6224:6222"
      - "8224:8222"
    volumes:
      - nats-3:/data

volumes:
  nats-1:
  nats-2:
  nats-3:
```

#### 2.2 Application Configuration

```go
package main

import (
    "github.com/nats-io/nats.go"
    "github.com/xraph/forge/extensions/streaming"
    "github.com/xraph/forge/extensions/streaming/coordinator"
)

func main() {
    // NATS connection
    nc, err := nats.Connect(
        nats.Name("streaming-node-1"),
        nats.Servers("nats://nats-1:4222", "nats://nats-2:4222", "nats://nats-3:4222"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create coordinator
    coord, err := coordinator.NewNATSCoordinator(nc, "node-1")
    if err != nil {
        log.Fatal(err)
    }

    // Configure streaming extension
    config := streaming.Config{
        NodeID: "node-1",
        Coordination: streaming.CoordinationConfig{
            Enabled:       true,
            Backend:       "nats",
            URLs:          []string{"nats://nats-1:4222"},
            PresenceSync:  true,
            RoomStateSync: true,
            SyncInterval:  30 * time.Second,
        },
    }

    streamExt := streaming.NewExtension(config)
}
```

### 3. Authentication Setup

#### 3.1 Enable Authentication

```go
import (
    "github.com/xraph/forge/extensions/auth"
    "github.com/xraph/forge/extensions/streaming"
    streamAuth "github.com/xraph/forge/extensions/streaming/auth"
)

// Setup auth extension
authExt := auth.NewExtension(auth.Config{
    Providers: []auth.ProviderConfig{
        {
            Name: "jwt",
            Type: "jwt",
            Config: map[string]any{
                "secret": "your-secret-key",
            },
        },
        {
            Name: "api_key",
            Type: "api_key",
        },
    },
})

// Register auth extension
app.RegisterExtension(authExt)

// Configure streaming with auth
streamConfig := streaming.Config{
    RequireAuth:        true,
    AuthProviders:      []string{"jwt", "api_key"},
    RoomAuthEnabled:    true,
    MessageAuthEnabled: true,
}
```

### 4. Load Balancing Configuration

#### 4.1 Sticky Sessions

```go
import (
    "github.com/xraph/forge/extensions/streaming/lb"
)

// Create sticky session store (using Redis)
sessionStore := // Create Redis-backed session store

// Create base load balancer
baseBalancer := lb.NewConsistentHashBalancer(150, nodeStore)

// Wrap with sticky sessions
loadBalancer := lb.NewStickyLoadBalancer(
    time.Hour,        // Session TTL
    baseBalancer,     // Fallback strategy
    sessionStore,
)

// Set on manager
manager.SetLoadBalancer(loadBalancer)
```

#### 4.2 Geographic Proximity

```go
import (
    "github.com/xraph/forge/extensions/streaming/lb"
)

// Create geo locator
geoLocator := lb.NewSimpleGeoLocator()

// Create geo-aware load balancer
loadBalancer := lb.NewGeoProximityBalancer(
    geoLocator,
    fallbackBalancer,
    nodeStore,
)

// Register nodes with geo info
loadBalancer.RegisterNode(ctx, &lb.NodeInfo{
    ID:        "node-us-east",
    Address:   "10.0.1.10",
    Port:      8080,
    Latitude:  40.7128,
    Longitude: -74.0060,
    Region:    "us-east-1",
})
```

### 5. Rate Limiting Configuration

#### 5.1 Distributed Rate Limiting with Redis

```go
import (
    "github.com/xraph/forge/extensions/streaming/ratelimit"
    "github.com/redis/go-redis/v9"
)

// Create Redis store for rate limiting
rateLimitStore := // Create Redis-backed store

// Create rate limiter
rateLimiter := ratelimit.NewTokenBucket(
    ratelimit.RateLimitConfig{
        MessagesPerSecond:     10,
        MessagesPerMinute:     100,
        BurstSize:            20,
        RoomMessagesPerSecond: 100,
        ConnectionsPerUser:    5,
        ConnectionsPerIP:      20,
    },
    rateLimitStore,
)

// Set on manager
manager.SetRateLimiter(rateLimiter)
```

### 6. Message Filtering and Validation

#### 6.1 Setup Filters

```go
import (
    "github.com/xraph/forge/extensions/streaming/filters"
)

// Content filter
contentFilter := filters.NewContentFilter(filters.ContentFilterConfig{
    EnableProfanity:     true,
    ProfanityList:       []string{"badword1", "badword2"},
    ProfanityAction:     "replace",
    ReplaceWith:         "***",
    EnableURLFilter:     true,
    BlockedDomains:      []string{"spam.com"},
    EnableSpamDetection: true,
    MaxURLs:             3,
    MaxMentions:         5,
})

// Permission filter
permissionFilter := filters.NewPermissionFilter(
    filters.NewDefaultPermissionChecker(roomStore),
)

// Size filter
sizeFilter := filters.NewSizeFilter(filters.SizeFilterConfig{
    MaxMessageSize:         10240, // 10KB
    EnableTruncation:       true,
    TruncateLength:         1024,
    EnableCompression:      true,
    CompressionThreshold:   2048,
    CompressionMinSavings:  0.2,
})

// Add to manager
manager.AddMessageFilter(permissionFilter)
manager.AddMessageFilter(contentFilter)
manager.AddMessageFilter(sizeFilter)
```

#### 6.2 Setup Validators

```go
import (
    "github.com/xraph/forge/extensions/streaming/validation"
)

// Composite validator
validator := validation.NewCompositeValidator(
    validation.NewContentValidator(validation.ContentValidatorConfig{
        MaxContentLength: 10000,
        MinContentLength: 1,
        RequireUserID:    true,
        RequireType:      true,
        ValidateURLs:     true,
    }),
    validation.NewSecurityValidator(validation.SecurityValidatorConfig{
        EnableXSSPrevention:         true,
        EnableSQLInjectionCheck:     true,
        EnableScriptInjectionCheck:  true,
        EnableLinkSafety:            true,
        AllowedProtocols:            []string{"http", "https"},
        BlockedDomains:              []string{"malicious.com"},
    }),
)

// Set on manager
manager.SetMessageValidator(validator)
```

### 7. Health Checking

#### 7.1 Enable Health Checks

```go
import (
    "github.com/xraph/forge/extensions/streaming/lb"
)

// Create health checker
healthChecker := lb.NewHealthChecker(
    lb.HealthCheckConfig{
        Enabled:       true,
        Interval:      10 * time.Second,
        Timeout:       5 * time.Second,
        FailThreshold: 3,
        PassThreshold: 2,
    },
    loadBalancer,
)

// Register nodes
healthChecker.RegisterNode(&lb.NodeInfo{
    ID:      "node-1",
    Address: "10.0.1.10",
    Port:    8080,
    Healthy: true,
})

// Start health checking
healthChecker.Start(ctx)
```

#### 7.2 Health Check Endpoint

```go
// Add health check endpoint on each node
router.GET("/health", func(ctx forge.Context) error {
    return ctx.JSON(200, map[string]any{
        "status": "healthy",
        "node":   "node-1",
    })
})
```

### 8. Monitoring and Observability

#### 8.1 Metrics Export

```go
// Enable metrics collection
app := forge.NewApp(forge.AppConfig{
    Name:    "Streaming Service",
    Version: "1.0.0",
    MetricsConfig: &forge.MetricsConfig{
        Enabled: true,
        Type:    "prometheus",
    },
})

// Expose metrics endpoint
router.GET("/metrics", func(ctx forge.Context) error {
    // Export Prometheus metrics
    return ctx.Text(200, metrics.Export())
})
```

#### 8.2 Key Metrics to Monitor

- **Connections**:
  - `streaming.connections.active` - Current active connections
  - `streaming.connections.total` - Total connections since start
  - `streaming.connections.per_user` - Connections per user

- **Messages**:
  - `streaming.messages.sent` - Messages sent
  - `streaming.messages.received` - Messages received
  - `streaming.messages.filtered` - Messages blocked by filters

- **Rooms**:
  - `streaming.rooms.active` - Active rooms
  - `streaming.rooms.members` - Total members across all rooms

- **Rate Limiting**:
  - `streaming.ratelimit.exceeded` - Rate limit violations
  - `streaming.ratelimit.allowed` - Allowed requests

- **Coordination**:
  - `streaming.coordinator.broadcasts` - Cross-node broadcasts
  - `streaming.coordinator.sync_lag` - Synchronization lag

### 9. Production Deployment Checklist

#### 9.1 Security
- [ ] Enable TLS for all connections
- [ ] Configure authentication providers
- [ ] Set up firewall rules
- [ ] Enable rate limiting
- [ ] Configure message filtering
- [ ] Enable security validation

#### 9.2 High Availability
- [ ] Deploy minimum 3 nodes
- [ ] Configure load balancer
- [ ] Set up health checking
- [ ] Enable sticky sessions
- [ ] Configure failover

#### 9.3 Performance
- [ ] Tune connection limits
- [ ] Configure message size limits
- [ ] Enable compression
- [ ] Optimize Redis/NATS
- [ ] Set appropriate rate limits

#### 9.4 Monitoring
- [ ] Set up metrics collection
- [ ] Configure alerts
- [ ] Enable logging
- [ ] Set up tracing (optional)
- [ ] Configure dashboards

#### 9.5 Scalability
- [ ] Configure horizontal scaling
- [ ] Set up auto-scaling (if cloud)
- [ ] Tune coordination parameters
- [ ] Configure proper TTLs
- [ ] Optimize synchronization intervals

### 10. Scaling Strategies

#### 10.1 Horizontal Scaling

**Add Nodes**:
```bash
# Deploy new node with same configuration
# Load balancer will automatically route traffic
# Coordinator will sync state
```

**Remove Nodes**:
```bash
# Gracefully shutdown node
# Load balancer will detect unhealthy node
# Connections will fail over to healthy nodes
```

#### 10.2 Vertical Scaling

- Increase node resources (CPU, memory)
- Tune connection limits
- Optimize backend configuration

#### 10.3 Geographic Distribution

```go
// Deploy nodes in multiple regions
nodes := []lb.NodeInfo{
    {
        ID:        "node-us-east",
        Region:    "us-east-1",
        Latitude:  40.7128,
        Longitude: -74.0060,
    },
    {
        ID:        "node-eu-west",
        Region:    "eu-west-1",
        Latitude:  51.5074,
        Longitude: -0.1278,
    },
    {
        ID:        "node-ap-south",
        Region:    "ap-south-1",
        Latitude:  19.0760,
        Longitude: 72.8777,
    },
}

// Use geo-proximity load balancing
loadBalancer := lb.NewGeoProximityBalancer(geoLocator, fallback, store)
for _, node := range nodes {
    loadBalancer.RegisterNode(ctx, &node)
}
```

### 11. Troubleshooting

#### 11.1 Connection Issues

**Problem**: Clients can't connect
- Check load balancer health
- Verify authentication configuration
- Check firewall rules
- Review rate limiting settings

#### 11.2 Synchronization Issues

**Problem**: State not syncing across nodes
- Check coordinator connectivity (Redis/NATS)
- Verify network connectivity
- Review synchronization intervals
- Check for clock skew

#### 11.3 Performance Issues

**Problem**: High latency or slow message delivery
- Check coordinator performance
- Review rate limiting configuration
- Optimize message filtering
- Check network latency between nodes
- Review backend (Redis/NATS) performance

#### 11.4 Memory Issues

**Problem**: High memory usage
- Check connection limits
- Review message store retention
- Optimize presence tracking
- Check for connection leaks

### 12. Best Practices

1. **Always use TLS** in production
2. **Enable authentication** for all connections
3. **Configure rate limiting** to prevent abuse
4. **Monitor metrics** continuously
5. **Set up alerts** for critical issues
6. **Use sticky sessions** for better user experience
7. **Enable health checking** for automatic failover
8. **Configure proper timeouts** to prevent resource leaks
9. **Test failover scenarios** before production
10. **Document your configuration** for operations team

### 13. Example Production Configuration

```go
package main

import (
    "time"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/streaming"
    "github.com/xraph/forge/extensions/streaming/lb"
    "github.com/xraph/forge/extensions/streaming/ratelimit"
)

func main() {
    // Production configuration
    config := streaming.Config{
        NodeID: "node-prod-1",
        
        // Authentication
        RequireAuth:        true,
        AuthProviders:      []string{"jwt"},
        RoomAuthEnabled:    true,
        MessageAuthEnabled: true,
        
        // Filtering
        EnableFilters: true,
        
        // Validation
        ValidationEnabled: true,
        
        // Rate Limiting
        RateLimitEnabled: true,
        RateLimit: ratelimit.RateLimitConfig{
            MessagesPerSecond:     10,
            MessagesPerMinute:     100,
            BurstSize:            20,
            RoomMessagesPerSecond: 100,
        },
        
        // Load Balancing
        LoadBalancing: lb.LoadBalancerConfig{
            Strategy: lb.StrategySticky,
            HealthCheck: lb.HealthCheckConfig{
                Enabled:       true,
                Interval:      10 * time.Second,
                Timeout:       5 * time.Second,
                FailThreshold: 3,
                PassThreshold: 2,
            },
            StickySession: lb.StickySessionConfig{
                Enabled: true,
                TTL:     time.Hour,
            },
        },
        
        // Coordination
        Coordination: streaming.CoordinationConfig{
            Enabled:       true,
            Backend:       "redis",
            URLs:          []string{"redis-1:6379", "redis-2:6379", "redis-3:6379"},
            PresenceSync:  true,
            RoomStateSync: true,
            SyncInterval:  30 * time.Second,
        },
        
        // Connection limits
        MaxConnectionsPerUser: 5,
        MaxRoomsPerUser:       100,
    }
    
    // Create and register extension
    streamExt := streaming.NewExtension(config)
    app.RegisterExtension(streamExt)
    
    // Start server
    app.Listen(":8080")
}
```

## Conclusion

This deployment guide covers all aspects of setting up a distributed, production-ready streaming system with authentication, filtering, rate limiting, load balancing, and cross-node coordination. Follow the checklist and best practices for a reliable deployment.

For support and updates, refer to the main documentation and the implementation summary in `AUTH_AND_COORDINATION_IMPLEMENTATION.md`.

