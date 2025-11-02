package farp

import (
	"testing"
)

func TestNewManifest(t *testing.T) {
	manifest := NewManifest("test-service", "v1.0.0", "instance-123")

	if manifest.ServiceName != "test-service" {
		t.Errorf("expected service name 'test-service', got '%s'", manifest.ServiceName)
	}

	if manifest.ServiceVersion != "v1.0.0" {
		t.Errorf("expected version 'v1.0.0', got '%s'", manifest.ServiceVersion)
	}

	if manifest.InstanceID != "instance-123" {
		t.Errorf("expected instance ID 'instance-123', got '%s'", manifest.InstanceID)
	}

	if manifest.Version != ProtocolVersion {
		t.Errorf("expected protocol version '%s', got '%s'", ProtocolVersion, manifest.Version)
	}

	if len(manifest.Schemas) != 0 {
		t.Errorf("expected empty schemas, got %d", len(manifest.Schemas))
	}

	if manifest.UpdatedAt == 0 {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestManifest_AddSchema(t *testing.T) {
	manifest := NewManifest("test-service", "v1.0.0", "instance-123")

	schema := SchemaDescriptor{
		Type:        SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location: SchemaLocation{
			Type: LocationTypeHTTP,
			URL:  "http://test.com/openapi.json",
		},
		ContentType: "application/json",
		Hash:        "abc123",
		Size:        1024,
	}

	manifest.AddSchema(schema)

	if len(manifest.Schemas) != 1 {
		t.Errorf("expected 1 schema, got %d", len(manifest.Schemas))
	}

	if manifest.Schemas[0].Type != SchemaTypeOpenAPI {
		t.Errorf("expected type OpenAPI, got %s", manifest.Schemas[0].Type)
	}
}

func TestManifest_AddCapability(t *testing.T) {
	manifest := NewManifest("test-service", "v1.0.0", "instance-123")

	manifest.AddCapability("rest")
	manifest.AddCapability("grpc")
	manifest.AddCapability("rest") // Duplicate

	if len(manifest.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(manifest.Capabilities))
	}

	if !manifest.HasCapability("rest") {
		t.Error("expected to have 'rest' capability")
	}

	if !manifest.HasCapability("grpc") {
		t.Error("expected to have 'grpc' capability")
	}

	if manifest.HasCapability("websocket") {
		t.Error("expected not to have 'websocket' capability")
	}
}

func TestManifest_UpdateChecksum(t *testing.T) {
	manifest := NewManifest("test-service", "v1.0.0", "instance-123")

	schema := SchemaDescriptor{
		Type:        SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location: SchemaLocation{
			Type: LocationTypeHTTP,
			URL:  "http://test.com/openapi.json",
		},
		ContentType: "application/json",
		Hash:        "abc123",
		Size:        1024,
	}

	manifest.AddSchema(schema)

	if err := manifest.UpdateChecksum(); err != nil {
		t.Fatalf("UpdateChecksum failed: %v", err)
	}

	if manifest.Checksum == "" {
		t.Error("expected checksum to be set")
	}

	if len(manifest.Checksum) != 64 {
		t.Errorf("expected checksum length 64, got %d", len(manifest.Checksum))
	}
}

func TestManifest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *SchemaManifest
		wantErr bool
	}{
		{
			name: "valid manifest",
			setup: func() *SchemaManifest {
				m := NewManifest("test-service", "v1.0.0", "instance-123")
				m.Endpoints.Health = "/health"
				m.AddSchema(SchemaDescriptor{
					Type:        SchemaTypeOpenAPI,
					SpecVersion: "3.1.0",
					Location: SchemaLocation{
						Type: LocationTypeHTTP,
						URL:  "http://test.com/openapi.json",
					},
					ContentType: "application/json",
					Hash:        "1234567890123456789012345678901234567890123456789012345678901234",
					Size:        1024,
				})
				m.UpdateChecksum()
				return m
			},
			wantErr: false,
		},
		{
			name: "missing service name",
			setup: func() *SchemaManifest {
				m := NewManifest("", "v1.0.0", "instance-123")
				m.Endpoints.Health = "/health"
				return m
			},
			wantErr: true,
		},
		{
			name: "missing instance ID",
			setup: func() *SchemaManifest {
				m := NewManifest("test-service", "v1.0.0", "")
				m.Endpoints.Health = "/health"
				return m
			},
			wantErr: true,
		},
		{
			name: "missing health endpoint",
			setup: func() *SchemaManifest {
				m := NewManifest("test-service", "v1.0.0", "instance-123")
				return m
			},
			wantErr: true,
		},
		{
			name: "invalid schema type",
			setup: func() *SchemaManifest {
				m := NewManifest("test-service", "v1.0.0", "instance-123")
				m.Endpoints.Health = "/health"
				m.AddSchema(SchemaDescriptor{
					Type:        SchemaType("invalid"),
					SpecVersion: "1.0.0",
					Location: SchemaLocation{
						Type: LocationTypeHTTP,
						URL:  "http://test.com/schema",
					},
					ContentType: "application/json",
					Hash:        "1234567890123456789012345678901234567890123456789012345678901234",
					Size:        1024,
				})
				return m
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := tt.setup()
			err := manifest.Validate()

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestManifest_GetSchema(t *testing.T) {
	manifest := NewManifest("test-service", "v1.0.0", "instance-123")

	openAPISchema := SchemaDescriptor{
		Type:        SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location: SchemaLocation{
			Type: LocationTypeHTTP,
			URL:  "http://test.com/openapi.json",
		},
		ContentType: "application/json",
		Hash:        "abc123",
		Size:        1024,
	}

	manifest.AddSchema(openAPISchema)

	// Test found
	schema, found := manifest.GetSchema(SchemaTypeOpenAPI)
	if !found {
		t.Error("expected to find OpenAPI schema")
	}
	if schema.Type != SchemaTypeOpenAPI {
		t.Errorf("expected OpenAPI type, got %s", schema.Type)
	}

	// Test not found
	_, found = manifest.GetSchema(SchemaTypeAsyncAPI)
	if found {
		t.Error("expected not to find AsyncAPI schema")
	}
}

func TestManifest_Clone(t *testing.T) {
	original := NewManifest("test-service", "v1.0.0", "instance-123")
	original.AddCapability("rest")
	original.AddSchema(SchemaDescriptor{
		Type:        SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location: SchemaLocation{
			Type: LocationTypeHTTP,
			URL:  "http://test.com/openapi.json",
		},
		ContentType: "application/json",
		Hash:        "abc123",
		Size:        1024,
	})
	original.Endpoints.Health = "/health"

	clone := original.Clone()

	// Verify deep copy
	if clone.ServiceName != original.ServiceName {
		t.Error("clone has different service name")
	}

	if len(clone.Schemas) != len(original.Schemas) {
		t.Error("clone has different number of schemas")
	}

	if len(clone.Capabilities) != len(original.Capabilities) {
		t.Error("clone has different number of capabilities")
	}

	// Modify clone and verify original is unchanged
	clone.AddCapability("grpc")

	if len(original.Capabilities) == len(clone.Capabilities) {
		t.Error("modifying clone affected original")
	}
}

func TestManifest_JSON(t *testing.T) {
	manifest := NewManifest("test-service", "v1.0.0", "instance-123")
	manifest.Endpoints.Health = "/health"
	manifest.AddSchema(SchemaDescriptor{
		Type:        SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location: SchemaLocation{
			Type: LocationTypeHTTP,
			URL:  "http://test.com/openapi.json",
		},
		ContentType: "application/json",
		Hash:        "abc123",
		Size:        1024,
	})

	// Test ToJSON
	data, err := manifest.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty JSON")
	}

	// Test FromJSON
	parsed, err := FromJSON(data)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	if parsed.ServiceName != manifest.ServiceName {
		t.Error("parsed manifest has different service name")
	}

	if len(parsed.Schemas) != len(manifest.Schemas) {
		t.Error("parsed manifest has different number of schemas")
	}
}

func TestCalculateSchemaChecksum(t *testing.T) {
	schema := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":   "Test API",
			"version": "1.0.0",
		},
	}

	checksum1, err := CalculateSchemaChecksum(schema)
	if err != nil {
		t.Fatalf("CalculateSchemaChecksum failed: %v", err)
	}

	if len(checksum1) != 64 {
		t.Errorf("expected checksum length 64, got %d", len(checksum1))
	}

	// Same schema should produce same checksum
	checksum2, err := CalculateSchemaChecksum(schema)
	if err != nil {
		t.Fatalf("CalculateSchemaChecksum failed: %v", err)
	}

	if checksum1 != checksum2 {
		t.Error("same schema produced different checksums")
	}

	// Different schema should produce different checksum
	schema["info"].(map[string]interface{})["version"] = "2.0.0"
	checksum3, err := CalculateSchemaChecksum(schema)
	if err != nil {
		t.Fatalf("CalculateSchemaChecksum failed: %v", err)
	}

	if checksum1 == checksum3 {
		t.Error("different schemas produced same checksum")
	}
}

func TestDiffManifests(t *testing.T) {
	// Create old manifest
	old := NewManifest("test-service", "v1.0.0", "instance-123")
	old.AddSchema(SchemaDescriptor{
		Type:        SchemaTypeOpenAPI,
		SpecVersion: "3.1.0",
		Location:    SchemaLocation{Type: LocationTypeHTTP, URL: "http://test.com/openapi.json"},
		ContentType: "application/json",
		Hash:        "hash1",
		Size:        1024,
	})
	old.AddCapability("rest")
	old.Endpoints.Health = "/health"

	// Create new manifest
	new := old.Clone()
	new.AddSchema(SchemaDescriptor{
		Type:        SchemaTypeAsyncAPI,
		SpecVersion: "3.0.0",
		Location:    SchemaLocation{Type: LocationTypeHTTP, URL: "http://test.com/asyncapi.json"},
		ContentType: "application/json",
		Hash:        "hash2",
		Size:        512,
	})
	new.AddCapability("websocket")

	// Change existing schema
	new.Schemas[0].Hash = "hash1-updated"

	diff := DiffManifests(old, new)

	if !diff.HasChanges() {
		t.Error("expected changes to be detected")
	}

	if len(diff.SchemasAdded) != 1 {
		t.Errorf("expected 1 schema added, got %d", len(diff.SchemasAdded))
	}

	if len(diff.SchemasChanged) != 1 {
		t.Errorf("expected 1 schema changed, got %d", len(diff.SchemasChanged))
	}

	if len(diff.CapabilitiesAdded) != 1 {
		t.Errorf("expected 1 capability added, got %d", len(diff.CapabilitiesAdded))
	}
}

func TestSchemaLocation_Validate(t *testing.T) {
	tests := []struct {
		name     string
		location SchemaLocation
		wantErr  bool
	}{
		{
			name: "valid HTTP location",
			location: SchemaLocation{
				Type: LocationTypeHTTP,
				URL:  "http://test.com/schema.json",
			},
			wantErr: false,
		},
		{
			name: "valid registry location",
			location: SchemaLocation{
				Type:         LocationTypeRegistry,
				RegistryPath: "/schemas/test/v1",
			},
			wantErr: false,
		},
		{
			name: "valid inline location",
			location: SchemaLocation{
				Type: LocationTypeInline,
			},
			wantErr: false,
		},
		{
			name: "HTTP without URL",
			location: SchemaLocation{
				Type: LocationTypeHTTP,
			},
			wantErr: true,
		},
		{
			name: "registry without path",
			location: SchemaLocation{
				Type: LocationTypeRegistry,
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			location: SchemaLocation{
				Type: LocationType("invalid"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.location.Validate()

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func BenchmarkManifest_UpdateChecksum(b *testing.B) {
	manifest := NewManifest("test-service", "v1.0.0", "instance-123")
	for i := 0; i < 10; i++ {
		manifest.AddSchema(SchemaDescriptor{
			Type:        SchemaTypeOpenAPI,
			SpecVersion: "3.1.0",
			Location:    SchemaLocation{Type: LocationTypeHTTP, URL: "http://test.com"},
			ContentType: "application/json",
			Hash:        "abc123",
			Size:        1024,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manifest.UpdateChecksum()
	}
}

func BenchmarkCalculateSchemaChecksum(b *testing.B) {
	schema := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"paths": make(map[string]interface{}),
	}

	// Add 100 paths
	paths := schema["paths"].(map[string]interface{})
	for i := 0; i < 100; i++ {
		paths["/path"+string(rune(i))] = map[string]interface{}{
			"get": map[string]interface{}{
				"summary": "Test endpoint",
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CalculateSchemaChecksum(schema)
	}
}

