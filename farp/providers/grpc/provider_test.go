package grpc

import (
	"context"
	"testing"

	"github.com/xraph/forge/farp"
)

// mockApp implements farp.Application for testing.
type mockApp struct {
	name    string
	version string
}

func (m *mockApp) Name() string {
	return m.name
}

func (m *mockApp) Version() string {
	return m.version
}

func (m *mockApp) Routes() any {
	return []map[string]string{
		{"method": "POST", "path": "/api/users"},
	}
}

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name        string
		specVersion string
		protoFiles  []string
		wantVersion string
	}{
		{
			name:        "default version",
			specVersion: "",
			protoFiles:  nil,
			wantVersion: "proto3",
		},
		{
			name:        "explicit proto3",
			specVersion: "proto3",
			protoFiles:  []string{"user.proto"},
			wantVersion: "proto3",
		},
		{
			name:        "proto2",
			specVersion: "proto2",
			protoFiles:  nil,
			wantVersion: "proto2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider(tt.specVersion, tt.protoFiles)
			if p.SpecVersion() != tt.wantVersion {
				t.Errorf("SpecVersion() = %v, want %v", p.SpecVersion(), tt.wantVersion)
			}

			if p.Type() != farp.SchemaTypeGRPC {
				t.Errorf("Type() = %v, want %v", p.Type(), farp.SchemaTypeGRPC)
			}
		})
	}
}

func TestProvider_Generate(t *testing.T) {
	app := &mockApp{
		name:    "test-service",
		version: "1.0.0",
	}

	p := NewProvider("proto3", nil)

	schema, err := p.Generate(context.Background(), app)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if schema == nil {
		t.Fatal("Generate() returned nil schema")
	}

	// Validate the generated schema
	if err := p.Validate(schema); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestProvider_Validate(t *testing.T) {
	p := NewProvider("proto3", nil)

	tests := []struct {
		name    string
		schema  any
		wantErr bool
	}{
		{
			name: "valid schema",
			schema: map[string]any{
				"file": []any{
					map[string]any{
						"name":    "test.proto",
						"package": "test",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "not a map",
			schema:  "invalid",
			wantErr: true,
		},
		{
			name:    "missing file field",
			schema:  map[string]any{},
			wantErr: true,
		},
		{
			name: "file is not an array",
			schema: map[string]any{
				"file": "not an array",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.Validate(tt.schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProvider_HashAndSerialize(t *testing.T) {
	app := &mockApp{
		name:    "test-service",
		version: "1.0.0",
	}

	p := NewProvider("proto3", nil)

	schema, err := p.Generate(context.Background(), app)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Test Hash
	hash, err := p.Hash(schema)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	if hash == "" {
		t.Error("Hash() returned empty string")
	}

	// Test Serialize
	data, err := p.Serialize(schema)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}

	if len(data) == 0 {
		t.Error("Serialize() returned empty data")
	}
}

func TestProvider_GenerateDescriptor(t *testing.T) {
	app := &mockApp{
		name:    "test-service",
		version: "1.0.0",
	}

	p := NewProvider("proto3", nil)

	tests := []struct {
		name           string
		locationType   farp.LocationType
		locationConfig map[string]string
		wantErr        bool
	}{
		{
			name:         "HTTP location",
			locationType: farp.LocationTypeHTTP,
			locationConfig: map[string]string{
				"url": "http://localhost:8080/grpc.json",
			},
			wantErr: false,
		},
		{
			name:         "Registry location",
			locationType: farp.LocationTypeRegistry,
			locationConfig: map[string]string{
				"registry_path": "/schemas/test-service/grpc",
			},
			wantErr: false,
		},
		{
			name:           "Inline location",
			locationType:   farp.LocationTypeInline,
			locationConfig: map[string]string{},
			wantErr:        false,
		},
		{
			name:           "HTTP without URL",
			locationType:   farp.LocationTypeHTTP,
			locationConfig: map[string]string{},
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			descriptor, err := p.GenerateDescriptor(
				context.Background(),
				app,
				tt.locationType,
				tt.locationConfig,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateDescriptor() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr {
				if descriptor == nil {
					t.Fatal("GenerateDescriptor() returned nil descriptor")
				}

				if descriptor.Type != farp.SchemaTypeGRPC {
					t.Errorf("descriptor.Type = %v, want %v", descriptor.Type, farp.SchemaTypeGRPC)
				}

				if tt.locationType == farp.LocationTypeInline && descriptor.InlineSchema == nil {
					t.Error("descriptor.InlineSchema is nil for inline location")
				}
			}
		})
	}
}

func TestProvider_SetProtoFiles(t *testing.T) {
	p := NewProvider("proto3", nil)

	files := []string{"user.proto", "product.proto"}
	p.SetProtoFiles(files)

	// No direct way to verify, but should not panic
	// In actual implementation, this would affect Generate() behavior
}

func TestProvider_EnableReflection(t *testing.T) {
	p := NewProvider("proto3", []string{"test.proto"})
	p.EnableReflection()

	// Should clear proto files to use reflection
	// No direct way to verify, but should not panic
}
