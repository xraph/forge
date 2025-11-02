// Package farp implements the Forge API Gateway Registration Protocol (FARP).
//
// FARP is a protocol specification for enabling service instances to automatically
// register their API schemas, health information, and capabilities with API gateways
// and service meshes.
//
// # Overview
//
// FARP provides:
//   - Schema-aware service discovery (OpenAPI, AsyncAPI, gRPC, GraphQL)
//   - Dynamic gateway configuration based on registered schemas
//   - Multi-protocol support with extensibility
//   - Health and telemetry integration
//   - Backend-agnostic storage (Consul, etcd, Kubernetes, Redis, Memory)
//   - Push and pull models for schema distribution
//   - Zero-downtime schema updates with versioning
//
// # Basic Usage
//
// Creating a schema manifest:
//
//	manifest := farp.NewManifest("user-service", "v1.2.3", "instance-abc123")
//	manifest.AddSchema(farp.SchemaDescriptor{
//	    Type:        farp.SchemaTypeOpenAPI,
//	    SpecVersion: "3.1.0",
//	    Location: farp.SchemaLocation{
//	        Type: farp.LocationTypeHTTP,
//	        URL:  "http://user-service:8080/openapi.json",
//	    },
//	    ContentType: "application/json",
//	})
//	manifest.AddCapability(farp.CapabilityREST.String())
//	manifest.Endpoints.Health = "/health"
//	manifest.UpdateChecksum()
//
// Registering with a registry:
//
//	registry := memory.NewRegistry()
//	err := registry.RegisterManifest(ctx, manifest)
//
// # Schema Providers
//
// Schema providers generate schemas from application code:
//
//	type MyProvider struct {
//	    farp.BaseSchemaProvider
//	}
//
//	func (p *MyProvider) Generate(ctx context.Context, app farp.Application) (interface{}, error) {
//	    // Generate schema from app
//	    return schema, nil
//	}
//
//	farp.RegisterProvider(&MyProvider{})
//
// # Gateway Integration
//
// Gateways watch for schema changes:
//
//	registry.WatchManifests(ctx, "user-service", func(event *farp.ManifestEvent) {
//	    if event.Type == farp.EventTypeAdded || event.Type == farp.EventTypeUpdated {
//	        // Fetch schemas and reconfigure gateway routes
//	        configureGatewayFromManifest(event.Manifest)
//	    }
//	})
//
// # Protocol Version
//
// Current protocol version: 1.0.0
//
// For full documentation, see: https://github.com/xraph/forge/tree/main/farp/docs
package farp
