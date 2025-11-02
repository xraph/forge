# FARP - Forge API Gateway Registration Protocol

**Version**: 1.0.0  
**Status**: Draft Specification  
**Authors**: Forge Core Team  
**Last Updated**: 2025-11-01

## Overview

FARP (Forge API Gateway Registration Protocol) is a protocol specification for enabling service instances to automatically register their API schemas, health information, and capabilities with API gateways and service meshes. It provides a standardized way for services to expose their contracts (OpenAPI, AsyncAPI, gRPC, GraphQL) and enable dynamic route configuration in API gateways.

## Key Features

- **Schema-Aware Service Discovery**: Services register with complete API contracts
- **Multi-Protocol Support**: OpenAPI, AsyncAPI, gRPC, GraphQL, and extensible for future protocols
- **Dynamic Gateway Configuration**: API gateways auto-configure routes based on registered schemas
- **Health & Telemetry Integration**: Built-in health checks and metrics endpoints
- **Backend Agnostic**: Works with Consul, etcd, Kubernetes, Eureka, and custom backends
- **Push & Pull Models**: Flexible schema distribution strategies
- **Zero-Downtime Updates**: Schema versioning and checksum validation

## Repository Structure

```
farp/
├── README.md                    # This file - project overview
├── docs/
│   ├── SPECIFICATION.md         # Complete protocol specification
│   ├── ARCHITECTURE.md          # Architecture and design decisions
│   ├── IMPLEMENTATION_GUIDE.md  # Guide for implementers
│   └── GATEWAY_INTEGRATION.md   # Gateway integration guide
├── examples/
│   ├── basic/                   # Basic usage examples
│   ├── multi-protocol/          # Multi-protocol service example
│   └── gateway-client/          # Reference gateway client
├── types.go                     # Core protocol types
├── manifest.go                  # Schema manifest types
├── provider.go                  # Schema provider interface
├── registry.go                  # Schema registry interface
├── storage.go                   # Storage abstraction
└── version.go                   # Protocol version constants
```

## Quick Start

### Service Registration

```go
import "github.com/xraph/forge/farp"

// Create schema manifest
manifest := &farp.SchemaManifest{
    Version:        farp.ProtocolVersion,
    ServiceName:    "user-service",
    ServiceVersion: "v1.2.3",
    InstanceID:     "user-service-abc123",
    Schemas: []farp.SchemaDescriptor{
        {
            Type:        farp.SchemaTypeOpenAPI,
            SpecVersion: "3.1.0",
            Location: farp.SchemaLocation{
                Type: farp.LocationTypeHTTP,
                URL:  "http://user-service:8080/openapi.json",
            },
        },
    },
    Capabilities: []string{"rest", "websocket"},
    Endpoints: farp.SchemaEndpoints{
        Health:   "/health",
        Metrics:  "/metrics",
        OpenAPI:  "/openapi.json",
    },
}

// Register with backend
registry.RegisterManifest(ctx, manifest)
```

### Gateway Integration

```go
import "github.com/xraph/forge/farp/gateway"

// Create gateway client
client := gateway.NewClient(backend)

// Watch for service schema changes
client.WatchServices(ctx, func(routes []gateway.ServiceRoute) {
    // Auto-configure gateway routes
    for _, route := range routes {
        gateway.AddRoute(route)
    }
})
```

## Use Cases

1. **API Gateway Auto-Configuration**: Gateways like Kong, Traefik, or Nginx automatically register routes based on service schemas
2. **Service Mesh Control Plane**: Enable contract-aware routing and validation in service meshes
3. **Developer Portals**: Auto-generate API documentation from registered schemas
4. **Multi-Protocol Systems**: Unified discovery for REST, gRPC, GraphQL, and async protocols
5. **Contract Testing**: Validate service contracts at deployment time
6. **Schema Versioning**: Support multiple API versions with zero-downtime migrations

## Protocol Status

- ✅ Core protocol specification complete
- ✅ Type definitions
- 🚧 Reference implementation (in progress)
- 🚧 Gateway client library (in progress)
- ⏳ Community feedback and refinement

## Documentation

- [Complete Specification](docs/SPECIFICATION.md) - Full protocol specification
- [Architecture Guide](docs/ARCHITECTURE.md) - Design decisions and architecture
- [Implementation Guide](docs/IMPLEMENTATION_GUIDE.md) - How to implement FARP
- [Gateway Integration](docs/GATEWAY_INTEGRATION.md) - Integrating with API gateways

## Contributing

FARP is part of the Forge framework. Contributions are welcome! Please see the main Forge repository for contribution guidelines.

## License

Apache License 2.0 - See LICENSE file in the Forge repository

