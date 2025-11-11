package main

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/farp"
	"github.com/xraph/farp/providers/openapi"
	"github.com/xraph/farp/registry/memory"
)

func main() {
	fmt.Println("=== FARP Basic Example ===")
	fmt.Println()

	// Create a memory registry (for demo purposes)
	registry := memory.NewRegistry()
	defer registry.Close()

	// Create a schema manifest
	manifest := farp.NewManifest("user-service", "v1.2.3", "user-service-instance-abc123")

	// Add OpenAPI schema descriptor
	manifest.AddSchema(farp.SchemaDescriptor{
		Type:        farp.SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location: farp.SchemaLocation{
			Type:         farp.LocationTypeRegistry,
			RegistryPath: "/schemas/user-service/v1/openapi",
		},
		ContentType: "application/json",
		Hash:        "dummy-hash-12345", // In production, calculate actual hash
		Size:        1024,
	})

	// Add capabilities
	manifest.AddCapability(farp.CapabilityREST.String())
	manifest.AddCapability(farp.CapabilityWebSocket.String())

	// Set endpoints
	manifest.Endpoints = farp.SchemaEndpoints{
		Health:   "/health",
		Metrics:  "/metrics",
		OpenAPI:  "/openapi.json",
		AsyncAPI: "/asyncapi.json",
	}

	// Calculate checksum
	if err := manifest.UpdateChecksum(); err != nil {
		panic(err)
	}

	fmt.Println("✓ Created schema manifest")
	fmt.Printf("  Service: %s\n", manifest.ServiceName)
	fmt.Printf("  Version: %s\n", manifest.ServiceVersion)
	fmt.Printf("  Instance: %s\n", manifest.InstanceID)
	fmt.Printf("  Schemas: %d\n", len(manifest.Schemas))
	fmt.Printf("  Checksum: %s\n\n", manifest.Checksum[:16]+"...")

	// Publish OpenAPI schema to registry
	ctx := context.Background()
	openAPISchema := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":   "User Service API",
			"version": "v1.2.3",
		},
		"paths": map[string]interface{}{
			"/users": map[string]interface{}{
				"get": map[string]interface{}{
					"summary": "List users",
				},
			},
		},
	}

	if err := registry.PublishSchema(ctx, "/schemas/user-service/v1/openapi", openAPISchema); err != nil {
		panic(err)
	}

	fmt.Println("✓ Published OpenAPI schema to registry")

	// Register manifest
	if err := registry.RegisterManifest(ctx, manifest); err != nil {
		panic(err)
	}

	fmt.Println("✓ Registered manifest with registry")

	// Fetch manifest back
	fetchedManifest, err := registry.GetManifest(ctx, manifest.InstanceID)
	if err != nil {
		panic(err)
	}

	fmt.Println("✓ Fetched manifest from registry")
	fmt.Printf("  Retrieved checksum: %s...\n", fetchedManifest.Checksum[:16])
	fmt.Println()

	// List all manifests
	manifests, err := registry.ListManifests(ctx, "user-service")
	if err != nil {
		panic(err)
	}

	fmt.Printf("✓ Listed manifests: found %d manifest(s)\n\n", len(manifests))

	// Demonstrate schema provider
	fmt.Println()
	fmt.Println("=== Schema Provider Example ===")
	fmt.Println()

	provider := openapi.NewProvider("3.1.0", "/openapi.json")
	fmt.Printf("✓ Created OpenAPI provider\n")
	fmt.Printf("  Type: %s\n", provider.Type())
	fmt.Printf("  Spec Version: %s\n", provider.SpecVersion())
	fmt.Printf("  Endpoint: %s\n", provider.Endpoint())
	fmt.Printf("  Content Type: %s\n\n", provider.ContentType())

	// Demonstrate manifest diff
	fmt.Println()
	fmt.Println("=== Manifest Diff Example ===")
	fmt.Println()

	// Create updated manifest
	updatedManifest := manifest.Clone()
	updatedManifest.AddSchema(farp.SchemaDescriptor{
		Type:        farp.SchemaTypeAsyncAPI,
		SpecVersion: "3.0.0",
		Location: farp.SchemaLocation{
			Type:         farp.LocationTypeRegistry,
			RegistryPath: "/schemas/user-service/v1/asyncapi",
		},
		ContentType: "application/json",
		Hash:        "dummy-hash-67890",
		Size:        512,
	})
	updatedManifest.AddCapability(farp.CapabilitySSE.String())
	updatedManifest.UpdateChecksum()

	// Calculate diff
	diff := farp.DiffManifests(manifest, updatedManifest)

	fmt.Printf("✓ Calculated manifest diff\n")
	fmt.Printf("  Has changes: %v\n", diff.HasChanges())
	fmt.Printf("  Schemas added: %d\n", len(diff.SchemasAdded))
	fmt.Printf("  Capabilities added: %d\n\n", len(diff.CapabilitiesAdded))

	if len(diff.SchemasAdded) > 0 {
		fmt.Println("  New schemas:")
		for _, schema := range diff.SchemasAdded {
			fmt.Printf("    - %s (%s)\n", schema.Type, schema.SpecVersion)
		}
	}

	if len(diff.CapabilitiesAdded) > 0 {
		fmt.Println("  New capabilities:")
		for _, cap := range diff.CapabilitiesAdded {
			fmt.Printf("    - %s\n", cap)
		}
	}

	fmt.Println()
	fmt.Println("=== Watch Example ===")
	fmt.Println()

	// Watch for manifest changes
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	fmt.Println("✓ Starting manifest watcher...")

	go func() {
		registry.WatchManifests(watchCtx, "user-service", func(event *farp.ManifestEvent) {
			fmt.Printf("\n  → Manifest event: %s\n", event.Type)
			fmt.Printf("    Service: %s\n", event.Manifest.ServiceName)
			fmt.Printf("    Instance: %s\n", event.Manifest.InstanceID)
			fmt.Printf("    Timestamp: %d\n", event.Timestamp)
		})
	}()

	// Wait a moment for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Update the manifest to trigger event
	fmt.Println("\n  Updating manifest...")
	if err := registry.UpdateManifest(ctx, updatedManifest); err != nil {
		panic(err)
	}

	// Wait for event to be processed
	time.Sleep(200 * time.Millisecond)

	fmt.Println()
	fmt.Println("=== Example Complete ===")
}
