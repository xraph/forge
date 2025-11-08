package discovery

import (
	"context"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/farp"
)

// TestFARPIntegration validates FARP schema publisher integration with service discovery.
func TestFARPIntegration(t *testing.T) {
	// Create a test app
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:    "test-service",
		Version: "1.0.0",
	}))

	// Configure discovery extension with FARP enabled
	config := Config{
		Enabled: true,
		Backend: "memory",
		Service: ServiceConfig{
			Name:    "test-service",
			Version: "1.0.0",
			Address: "127.0.0.1",
			Port:    8080,
		},
		FARP: FARPConfig{
			Enabled:      true,
			AutoRegister: true,
			Strategy:     "push",
			Capabilities: []string{"rest", "grpc", "graphql"},
			Endpoints: FARPEndpointsConfig{
				Health:         "/health",
				Metrics:        "/metrics",
				OpenAPI:        "/openapi.json",
				AsyncAPI:       "/asyncapi.json",
				GraphQL:        "/graphql",
				GRPCReflection: true,
			},
		},
	}

	ext := NewExtension(WithConfig(config))

	// Cast to Extension to access private fields for testing
	discExt := ext.(*Extension)

	// Register extension
	if err := discExt.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Verify FARP schema publisher was initialized
	if discExt.schemaPublisher == nil {
		t.Fatal("FARP schema publisher was not initialized")
	}

	// Verify schema publisher has correct config
	if !discExt.schemaPublisher.config.Enabled {
		t.Error("FARP should be enabled")
	}

	if !discExt.schemaPublisher.config.AutoRegister {
		t.Error("FARP auto-register should be enabled")
	}

	if len(discExt.schemaPublisher.config.Capabilities) != 3 {
		t.Errorf("expected 3 capabilities, got %d", len(discExt.schemaPublisher.config.Capabilities))
	}

	// Start extension (this will trigger FARP schema publication)
	ctx := context.Background()
	if err := discExt.Start(ctx); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	// Verify service was registered
	instance := discExt.createServiceInstance()
	if instance.Name != "test-service" {
		t.Errorf("service name = %s, want test-service", instance.Name)
	}

	// Verify FARP metadata was injected into service instance
	if instance.Metadata == nil {
		t.Fatal("service instance metadata is nil")
	}

	// Check for FARP metadata keys
	if _, ok := instance.Metadata["farp.enabled"]; !ok {
		t.Error("missing farp.enabled metadata")
	}

	// Cleanup
	if err := discExt.Stop(context.Background()); err != nil {
		t.Errorf("failed to stop extension: %v", err)
	}
}

// TestFARPMetadataGeneration validates that FARP metadata is correctly generated.
func TestFARPMetadataGeneration(t *testing.T) {
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:    "test-service",
		Version: "1.0.0",
	}))

	config := FARPConfig{
		Enabled:      true,
		AutoRegister: true,
		Endpoints: FARPEndpointsConfig{
			OpenAPI:        "/openapi.json",
			AsyncAPI:       "/asyncapi.json",
			GraphQL:        "/graphql",
			GRPCReflection: true,
		},
		Capabilities: []string{"rest", "grpc", "graphql", "websocket"},
		Strategy:     "pull",
	}

	publisher := NewSchemaPublisher(config, app)

	// Generate metadata
	baseURL := "http://test-service:8080"
	metadata := publisher.GetMetadataForDiscovery(baseURL)

	// Verify required metadata keys
	expectedKeys := []string{
		"farp.enabled",
		"farp.manifest",
		"farp.openapi",
		"farp.openapi.path",
		"farp.asyncapi",
		"farp.asyncapi.path",
		"farp.graphql",
		"farp.graphql.path",
		"farp.grpc.reflection",
		"farp.capabilities",
		"farp.strategy",
	}

	for _, key := range expectedKeys {
		if _, ok := metadata[key]; !ok {
			t.Errorf("missing required metadata key: %s", key)
		}
	}

	// Verify values
	if metadata["farp.enabled"] != "true" {
		t.Errorf("farp.enabled = %s, want true", metadata["farp.enabled"])
	}

	if metadata["farp.manifest"] != baseURL+"/_farp/manifest" {
		t.Errorf("farp.manifest = %s, want %s", metadata["farp.manifest"], baseURL+"/_farp/manifest")
	}

	if metadata["farp.strategy"] != "pull" {
		t.Errorf("farp.strategy = %s, want pull", metadata["farp.strategy"])
	}

	if metadata["farp.grpc.reflection"] != "true" {
		t.Errorf("farp.grpc.reflection = %s, want true", metadata["farp.grpc.reflection"])
	}
}

// TestFARPWithDifferentBackends validates FARP works with different discovery backends.
func TestFARPWithDifferentBackends(t *testing.T) {
	tests := []struct {
		name    string
		backend string
	}{
		{"memory backend", "memory"},
		{"mdns backend", "mdns"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := forge.New(forge.WithConfig(forge.AppConfig{
				Name:    "test-service",
				Version: "1.0.0",
			}))

			config := Config{
				Enabled: true,
				Backend: tt.backend,
				Service: ServiceConfig{
					Name:    "test-service",
					Version: "1.0.0",
					Address: "127.0.0.1",
					Port:    8080,
				},
				FARP: FARPConfig{
					Enabled:      true,
					AutoRegister: true,
					Strategy:     "hybrid",
					Capabilities: []string{"rest"},
					Endpoints: FARPEndpointsConfig{
						Health:  "/health",
						OpenAPI: "/openapi.json",
					},
				},
			}

			if tt.backend == "mdns" {
				config.MDNS = MDNSConfig{
					Domain: "local.",
				}
			}

			ext := NewExtension(WithConfig(config))
			discExt := ext.(*Extension)

			if err := discExt.Register(app); err != nil {
				t.Fatalf("failed to register extension: %v", err)
			}

			if discExt.schemaPublisher == nil {
				t.Fatal("FARP schema publisher was not initialized")
			}

			// Verify FARP metadata is generated
			instance := discExt.createServiceInstance()
			if _, ok := instance.Metadata["farp.enabled"]; !ok {
				t.Error("FARP metadata not injected into service instance")
			}
		})
	}
}

// TestFARPSchemaTypes validates all schema types are supported.
func TestFARPSchemaTypes(t *testing.T) {
	schemaTypes := []farp.SchemaType{
		farp.SchemaTypeOpenAPI,
		farp.SchemaTypeAsyncAPI,
		farp.SchemaTypeGRPC,
		farp.SchemaTypeGraphQL,
		farp.SchemaTypeORPC,
		farp.SchemaTypeThrift,
		farp.SchemaTypeAvro,
	}

	for _, schemaType := range schemaTypes {
		t.Run(string(schemaType), func(t *testing.T) {
			if !schemaType.IsValid() {
				t.Errorf("schema type %s is not valid", schemaType)
			}
		})
	}
}

// TestServiceInstanceWithFARPMetadata validates service instance contains FARP metadata.
func TestServiceInstanceWithFARPMetadata(t *testing.T) {
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:    "test-service",
		Version: "1.0.0",
	}))

	config := Config{
		Enabled: true,
		Backend: "memory",
		Service: ServiceConfig{
			Name:    "test-service",
			Version: "1.0.0",
			Address: "192.168.1.100",
			Port:    8080,
		},
		FARP: FARPConfig{
			Enabled:      true,
			AutoRegister: true,
			Endpoints: FARPEndpointsConfig{
				OpenAPI:        "/openapi.json",
				AsyncAPI:       "/asyncapi.json",
				GraphQL:        "/graphql",
				GRPCReflection: true,
			},
			Capabilities: []string{"rest", "grpc", "graphql", "websocket"},
		},
	}

	ext := NewExtension(WithConfig(config))

	discExt := ext.(*Extension)
	if err := discExt.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	instance := discExt.createServiceInstance()

	// Verify basic service info
	if instance.Name != "test-service" {
		t.Errorf("service name = %s, want test-service", instance.Name)
	}

	if instance.Address != "192.168.1.100" {
		t.Errorf("service address = %s, want 192.168.1.100", instance.Address)
	}

	if instance.Port != 8080 {
		t.Errorf("service port = %d, want 8080", instance.Port)
	}

	// Verify FARP metadata
	if instance.Metadata == nil {
		t.Fatal("service instance metadata is nil")
	}

	expectedMetadata := map[string]string{
		"farp.enabled":         "true",
		"farp.manifest":        "http://192.168.1.100:8080/_farp/manifest",
		"farp.openapi":         "http://192.168.1.100:8080/openapi.json",
		"farp.asyncapi":        "http://192.168.1.100:8080/asyncapi.json",
		"farp.graphql":         "http://192.168.1.100:8080/graphql",
		"farp.grpc.reflection": "true",
	}

	for key, expectedValue := range expectedMetadata {
		if actualValue, ok := instance.Metadata[key]; !ok {
			t.Errorf("missing metadata key: %s", key)
		} else if actualValue != expectedValue {
			t.Errorf("metadata[%s] = %s, want %s", key, actualValue, expectedValue)
		}
	}
}

// TestFARPRegistryIntegration validates FARP manifest registration.
func TestFARPRegistryIntegration(t *testing.T) {
	app := forge.New(forge.WithConfig(forge.AppConfig{
		Name:    "test-service",
		Version: "1.0.0",
	}))

	config := FARPConfig{
		Enabled:      true,
		AutoRegister: true,
		Capabilities: []string{"rest", "grpc"},
		Endpoints: FARPEndpointsConfig{
			Health:  "/health",
			Metrics: "/metrics",
		},
	}

	publisher := NewSchemaPublisher(config, app)

	// Publish schemas
	ctx := context.Background()
	instanceID := "test-service-12345"

	err := publisher.Publish(ctx, instanceID)
	if err != nil {
		t.Fatalf("failed to publish schemas: %v", err)
	}

	// Get manifest from registry
	manifest, err := publisher.GetManifest(ctx, instanceID)
	if err != nil {
		t.Fatalf("failed to get manifest: %v", err)
	}

	// Verify manifest
	if manifest.ServiceName != "test-service" {
		t.Errorf("manifest.ServiceName = %s, want test-service", manifest.ServiceName)
	}

	if manifest.ServiceVersion != "1.0.0" {
		t.Errorf("manifest.ServiceVersion = %s, want 1.0.0", manifest.ServiceVersion)
	}

	if manifest.InstanceID != instanceID {
		t.Errorf("manifest.InstanceID = %s, want %s", manifest.InstanceID, instanceID)
	}

	if len(manifest.Capabilities) != 2 {
		t.Errorf("len(manifest.Capabilities) = %d, want 2", len(manifest.Capabilities))
	}

	if manifest.Endpoints.Health != "/health" {
		t.Errorf("manifest.Endpoints.Health = %s, want /health", manifest.Endpoints.Health)
	}
}
