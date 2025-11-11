package thrift

import (
	"context"
	"testing"

	"github.com/xraph/farp"
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
		idlFiles    []string
		wantVersion string
	}{
		{
			name:        "default version",
			specVersion: "",
			idlFiles:    nil,
			wantVersion: "0.19.0",
		},
		{
			name:        "explicit version",
			specVersion: "0.18.0",
			idlFiles:    []string{"user.thrift"},
			wantVersion: "0.18.0",
		},
		{
			name:        "with multiple IDL files",
			specVersion: "0.19.0",
			idlFiles:    []string{"user.thrift", "product.thrift"},
			wantVersion: "0.19.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider(tt.specVersion, tt.idlFiles)
			if p.SpecVersion() != tt.wantVersion {
				t.Errorf("SpecVersion() = %v, want %v", p.SpecVersion(), tt.wantVersion)
			}

			if p.Type() != farp.SchemaTypeThrift {
				t.Errorf("Type() = %v, want %v", p.Type(), farp.SchemaTypeThrift)
			}
		})
	}
}

func TestProvider_Generate(t *testing.T) {
	app := &mockApp{
		name:    "test-service",
		version: "1.0.0",
	}

	tests := []struct {
		name     string
		idlFiles []string
	}{
		{
			name:     "without IDL files",
			idlFiles: nil,
		},
		{
			name:     "with IDL files",
			idlFiles: []string{"test.thrift"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider("0.19.0", tt.idlFiles)

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
		})
	}
}

func TestProvider_Validate(t *testing.T) {
	p := NewProvider("0.19.0", nil)

	tests := []struct {
		name    string
		schema  any
		wantErr bool
	}{
		{
			name: "valid schema",
			schema: map[string]any{
				"thrift_version": "0.19.0",
				"services": []any{
					map[string]any{
						"name": "TestService",
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
			name: "missing thrift_version",
			schema: map[string]any{
				"services": []any{},
			},
			wantErr: true,
		},
		{
			name: "missing services",
			schema: map[string]any{
				"thrift_version": "0.19.0",
			},
			wantErr: true,
		},
		{
			name: "services not an array",
			schema: map[string]any{
				"thrift_version": "0.19.0",
				"services":       "not an array",
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

	p := NewProvider("0.19.0", nil)

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

	// Hash should be deterministic
	hash2, err := p.Hash(schema)
	if err != nil {
		t.Fatalf("Hash() second call error = %v", err)
	}

	if hash != hash2 {
		t.Error("Hash() is not deterministic")
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

	p := NewProvider("0.19.0", nil)

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
				"url": "http://localhost:8080/thrift.json",
			},
			wantErr: false,
		},
		{
			name:         "Registry location",
			locationType: farp.LocationTypeRegistry,
			locationConfig: map[string]string{
				"registry_path": "/schemas/test-service/thrift",
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

				if descriptor.Type != farp.SchemaTypeThrift {
					t.Errorf("descriptor.Type = %v, want %v", descriptor.Type, farp.SchemaTypeThrift)
				}

				if tt.locationType == farp.LocationTypeInline && descriptor.InlineSchema == nil {
					t.Error("descriptor.InlineSchema is nil for inline location")
				}
			}
		})
	}
}

func TestProvider_SetGetIDLFiles(t *testing.T) {
	p := NewProvider("0.19.0", nil)

	files := []string{"user.thrift", "product.thrift"}
	p.SetIDLFiles(files)

	gotFiles := p.GetIDLFiles()
	if len(gotFiles) != len(files) {
		t.Errorf("GetIDLFiles() returned %d files, want %d", len(gotFiles), len(files))
	}
}

func TestProvider_ContentType(t *testing.T) {
	p := NewProvider("0.19.0", nil)

	if p.ContentType() != "application/json" {
		t.Errorf("ContentType() = %v, want application/json", p.ContentType())
	}
}

func TestProvider_Endpoint(t *testing.T) {
	p := NewProvider("0.19.0", nil)

	// Thrift typically doesn't have HTTP endpoints
	if p.Endpoint() != "" {
		t.Errorf("Endpoint() = %v, want empty string", p.Endpoint())
	}
}
