# FARP Basic Example

This example demonstrates the basic usage of the FARP (Forge API Gateway Registration Protocol).

## What This Example Shows

1. **Creating a Schema Manifest**: How to create and configure a service manifest
2. **Publishing Schemas**: Storing schemas in a registry
3. **Registering Services**: Registering manifests with the registry
4. **Fetching Manifests**: Retrieving manifests from the registry
5. **Schema Providers**: Using schema providers for OpenAPI generation
6. **Manifest Diffing**: Comparing two manifests to detect changes
7. **Watching for Changes**: Subscribing to manifest update events

## Running the Example

```bash
cd /Users/rexraphael/Work/Web-Mobile/xraph/forge/farp/examples/basic
go run main.go
```

## Expected Output

```
=== FARP Basic Example ===

✓ Created schema manifest
  Service: user-service
  Version: v1.2.3
  Instance: user-service-instance-abc123
  Schemas: 1
  Checksum: 1234567890abcdef...

✓ Published OpenAPI schema to registry
✓ Registered manifest with registry

✓ Fetched manifest from registry
  Retrieved checksum: 1234567890abcdef...

✓ Listed manifests: found 1 manifest(s)

=== Schema Provider Example ===

✓ Created OpenAPI provider
  Type: openapi
  Spec Version: 3.1.0
  Endpoint: /openapi.json
  Content Type: application/json

=== Manifest Diff Example ===

✓ Calculated manifest diff
  Has changes: true
  Schemas added: 1
  Capabilities added: 1

  New schemas:
    - asyncapi (3.0.0)
  New capabilities:
    - sse

=== Watch Example ===

✓ Starting manifest watcher...

  Updating manifest...

  → Manifest event: updated
    Service: user-service
    Instance: user-service-instance-abc123
    Timestamp: 1698768000

=== Example Complete ===
```

## Key Concepts

### Schema Manifest

A schema manifest describes all API contracts for a service instance:

```go
manifest := farp.NewManifest("service-name", "version", "instance-id")
manifest.AddSchema(schemaDescriptor)
manifest.AddCapability("rest")
manifest.UpdateChecksum()
```

### Schema Registration

Publish schemas to the registry and register the manifest:

```go
registry.PublishSchema(ctx, "/path/to/schema", schemaData)
registry.RegisterManifest(ctx, manifest)
```

### Watching for Changes

Subscribe to manifest changes:

```go
registry.WatchManifests(ctx, "service-name", func(event *farp.ManifestEvent) {
    // Handle manifest changes
})
```

## Next Steps

- See the `gateway-client` example for API gateway integration
- See the `multi-protocol` example for services exposing multiple protocols
- Read the full specification in `docs/SPECIFICATION.md`

