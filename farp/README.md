# FARP - Forge API Gateway Registration Protocol

**Version**: 1.0.0  
**Status**: Draft Specification  
**Authors**: Forge Core Team  
**Last Updated**: 2025-11-01

## Overview

FARP (Forge API Gateway Registration Protocol) is a protocol specification for enabling service instances to automatically register their API schemas, health information, and capabilities with API gateways and service meshes. It provides a standardized way for services to expose their contracts (OpenAPI, AsyncAPI, gRPC, GraphQL) and enable dynamic route configuration in API gateways.

## Key Features

- **Schema-Aware Service Discovery**: Services register with complete API contracts
- **Multi-Protocol Support**: OpenAPI, AsyncAPI, gRPC, GraphQL, oRPC, Thrift, Avro, and extensible for future protocols
- **Production-Ready Providers**: Built-in schema providers for all major API protocols including RPC and data serialization
- **Dynamic Gateway Configuration**: API gateways auto-configure routes based on registered schemas
- **Health & Telemetry Integration**: Built-in health checks and metrics endpoints
- **Backend Agnostic**: Works with Consul, etcd, Kubernetes, mDNS/Bonjour, Eureka, and custom backends
- **Transport Agnostic**: Schema metadata propagates through KV stores, DNS TXT records, ConfigMaps, and more
- **Push & Pull Models**: Flexible schema distribution strategies
- **Zero-Downtime Updates**: Schema versioning and checksum validation
- **Zero Configuration**: Works with mDNS/Bonjour for local network discovery without infrastructure

## Repository Structure

```
farp/
├── README.md                    # This file - project overview
├── PROVIDERS_IMPLEMENTATION.md  # Schema providers implementation guide
├── docs/
│   ├── SPECIFICATION.md         # Complete protocol specification
│   ├── ARCHITECTURE.md          # Architecture and design decisions
│   ├── IMPLEMENTATION_GUIDE.md  # Guide for implementers
│   └── GATEWAY_INTEGRATION.md   # Gateway integration guide
├── providers/                   # Schema provider implementations
│   ├── openapi/                 # OpenAPI 3.x provider
│   ├── asyncapi/                # AsyncAPI 2.x/3.x provider
│   ├── grpc/                    # gRPC FileDescriptorSet provider
│   ├── graphql/                 # GraphQL SDL/introspection provider
│   ├── orpc/                    # oRPC (OpenAPI-based RPC) provider
│   ├── thrift/                  # Apache Thrift IDL provider
│   └── avro/                    # Apache Avro schema provider
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

### Using Schema Providers

```go
import (
    "github.com/xraph/forge/farp/providers/openapi"
    "github.com/xraph/forge/farp/providers/grpc"
    "github.com/xraph/forge/farp/providers/graphql"
    "github.com/xraph/forge/farp/providers/orpc"
    "github.com/xraph/forge/farp/providers/thrift"
    "github.com/xraph/forge/farp/providers/avro"
)

// OpenAPI provider
openapiProvider := openapi.NewProvider("3.1.0", "/openapi.json")
schema, err := openapiProvider.Generate(ctx, app)

// gRPC provider (with reflection)
grpcProvider := grpc.NewProvider("proto3", nil)
schema, err = grpcProvider.Generate(ctx, app)

// GraphQL provider (SDL format)
graphqlProvider := graphql.NewProvider("2021", "/graphql")
graphqlProvider.UseSDL()
schema, err = graphqlProvider.Generate(ctx, app)

// oRPC provider
orpcProvider := orpc.NewProvider("1.0.0", "/orpc.json")
schema, err = orpcProvider.Generate(ctx, app)

// Thrift provider (from IDL files)
thriftProvider := thrift.NewProvider("0.19.0", []string{"user.thrift"})
schema, err = thriftProvider.Generate(ctx, app)

// Avro provider (from schema files)
avroProvider := avro.NewProvider("1.11.1", []string{"user.avsc"})
schema, err = avroProvider.Generate(ctx, app)
```

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
        {
            Type:        farp.SchemaTypeGRPC,
            SpecVersion: "proto3",
            Location: farp.SchemaLocation{
                Type: farp.LocationTypeInline,
            },
        },
        {
            Type:        farp.SchemaTypeGraphQL,
            SpecVersion: "2021",
            Location: farp.SchemaLocation{
                Type: farp.LocationTypeHTTP,
                URL:  "http://user-service:8080/graphql",
            },
        },
    },
    Capabilities: []string{"rest", "grpc", "graphql", "websocket"},
    Endpoints: farp.SchemaEndpoints{
        Health:         "/health",
        Metrics:        "/metrics",
        OpenAPI:        "/openapi.json",
        GraphQL:        "/graphql",
        GRPCReflection: true,
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
- ✅ Schema providers (OpenAPI, AsyncAPI, gRPC, GraphQL, oRPC, Thrift, Avro)
- ✅ Discovery extension integration
- ✅ mDNS/Bonjour backend support
- 🚧 Gateway client library (in progress)
- ⏳ Community feedback and refinement

## Supported Schema Types

| Protocol | Status | Provider | Spec Version | Content Type |
|----------|--------|----------|--------------|--------------|
| **OpenAPI** | ✅ Complete | `providers/openapi` | 3.1.0 (default) | `application/json` |
| **AsyncAPI** | ✅ Complete | `providers/asyncapi` | 3.0.0 (default) | `application/json` |
| **gRPC** | ✅ Complete | `providers/grpc` | proto3 (default) | `application/json` |
| **GraphQL** | ✅ Complete | `providers/graphql` | 2021 (default) | `application/graphql` or `application/json` |
| **oRPC** | ✅ Complete | `providers/orpc` | 1.0.0 | `application/json` |
| **Thrift** | ✅ Complete | `providers/thrift` | 0.19.0 (default) | `application/json` |
| **Avro** | ✅ Complete | `providers/avro` | 1.11.1 (default) | `application/json` |

See [PROVIDERS_IMPLEMENTATION.md](PROVIDERS_IMPLEMENTATION.md) for detailed provider documentation.

## Documentation

- [Complete Specification](docs/SPECIFICATION.md) - Full protocol specification
- [Providers Implementation](PROVIDERS_IMPLEMENTATION.md) - Schema provider guide
- [mDNS Service Type Configuration](docs/MDNS_SERVICE_TYPE_GUIDE.md) - Service type filtering for gateways
- [Gateway Discovery Examples](docs/GATEWAY_DISCOVERY_EXAMPLES.md) - Practical gateway integration examples
- [Architecture Guide](docs/ARCHITECTURE.md) - Design decisions and architecture
- [Implementation Guide](docs/IMPLEMENTATION_GUIDE.md) - How to implement FARP
- [Gateway Integration](docs/GATEWAY_INTEGRATION.md) - Integrating with API gateways
- [mDNS/Bonjour Integration](../extensions/discovery/FARP_MDNS_INTEGRATION.md) - Zero-config discovery

## Contributing

FARP is part of the Forge framework. Contributions are welcome! Please see the main Forge repository for contribution guidelines.

## License

Apache License 2.0 - See LICENSE file in the Forge repository

