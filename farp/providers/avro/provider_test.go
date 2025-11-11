package avro

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
		{"method": "POST", "path": "/api/data"},
	}
}

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name         string
		specVersion  string
		schemaFiles  []string
		wantVersion  string
		wantEndpoint string
	}{
		{
			name:         "default version",
			specVersion:  "",
			schemaFiles:  nil,
			wantVersion:  "1.11.1",
			wantEndpoint: "/avro.json",
		},
		{
			name:         "explicit version",
			specVersion:  "1.10.0",
			schemaFiles:  []string{"user.avsc"},
			wantVersion:  "1.10.0",
			wantEndpoint: "/avro.json",
		},
		{
			name:         "with multiple schema files",
			specVersion:  "1.11.1",
			schemaFiles:  []string{"user.avsc", "product.avsc"},
			wantVersion:  "1.11.1",
			wantEndpoint: "/avro.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider(tt.specVersion, tt.schemaFiles)
			if p.SpecVersion() != tt.wantVersion {
				t.Errorf("SpecVersion() = %v, want %v", p.SpecVersion(), tt.wantVersion)
			}

			if p.Endpoint() != tt.wantEndpoint {
				t.Errorf("Endpoint() = %v, want %v", p.Endpoint(), tt.wantEndpoint)
			}

			if p.Type() != farp.SchemaTypeAvro {
				t.Errorf("Type() = %v, want %v", p.Type(), farp.SchemaTypeAvro)
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
		name        string
		schemaFiles []string
	}{
		{
			name:        "without schema files",
			schemaFiles: nil,
		},
		{
			name:        "with schema files",
			schemaFiles: []string{"test.avsc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider("1.11.1", tt.schemaFiles)

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
	p := NewProvider("1.11.1", nil)

	tests := []struct {
		name    string
		schema  any
		wantErr bool
	}{
		{
			name: "valid schema",
			schema: map[string]any{
				"avro_version": "1.11.1",
				"protocol":     "TestProtocol",
				"namespace":    "com.test",
				"types": []any{
					map[string]any{
						"type": "record",
						"name": "TestRecord",
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
			name: "missing protocol",
			schema: map[string]any{
				"namespace": "com.test",
				"types":     []any{},
			},
			wantErr: true,
		},
		{
			name: "missing namespace",
			schema: map[string]any{
				"protocol": "TestProtocol",
				"types":    []any{},
			},
			wantErr: true,
		},
		{
			name: "missing types",
			schema: map[string]any{
				"protocol":  "TestProtocol",
				"namespace": "com.test",
			},
			wantErr: true,
		},
		{
			name: "types not an array",
			schema: map[string]any{
				"protocol":  "TestProtocol",
				"namespace": "com.test",
				"types":     "not an array",
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

	p := NewProvider("1.11.1", nil)

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

	p := NewProvider("1.11.1", nil)

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
				"url": "http://localhost:8080/avro.json",
			},
			wantErr: false,
		},
		{
			name:         "HTTP with headers",
			locationType: farp.LocationTypeHTTP,
			locationConfig: map[string]string{
				"url":     "http://localhost:8080/avro.json",
				"headers": `{"Authorization":"Bearer token"}`,
			},
			wantErr: false,
		},
		{
			name:         "Registry location",
			locationType: farp.LocationTypeRegistry,
			locationConfig: map[string]string{
				"registry_path": "/schemas/test-service/avro",
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
		{
			name:           "Registry without path",
			locationType:   farp.LocationTypeRegistry,
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

				if descriptor.Type != farp.SchemaTypeAvro {
					t.Errorf("descriptor.Type = %v, want %v", descriptor.Type, farp.SchemaTypeAvro)
				}

				if descriptor.Hash == "" {
					t.Error("descriptor.Hash is empty")
				}

				if descriptor.Size == 0 {
					t.Error("descriptor.Size is zero")
				}

				if tt.locationType == farp.LocationTypeInline && descriptor.InlineSchema == nil {
					t.Error("descriptor.InlineSchema is nil for inline location")
				}
			}
		})
	}
}

func TestProvider_SetGetSchemaFiles(t *testing.T) {
	p := NewProvider("1.11.1", nil)

	files := []string{"user.avsc", "product.avsc"}
	p.SetSchemaFiles(files)

	gotFiles := p.GetSchemaFiles()
	if len(gotFiles) != len(files) {
		t.Errorf("GetSchemaFiles() returned %d files, want %d", len(gotFiles), len(files))
	}
}

func TestProvider_ContentType(t *testing.T) {
	p := NewProvider("1.11.1", nil)

	if p.ContentType() != "application/json" {
		t.Errorf("ContentType() = %v, want application/json", p.ContentType())
	}
}

func TestProvider_SetEndpoint(t *testing.T) {
	p := NewProvider("1.11.1", nil)

	newEndpoint := "/api/v2/avro.json"
	p.SetEndpoint(newEndpoint)

	if p.Endpoint() != newEndpoint {
		t.Errorf("Endpoint() = %v, want %v", p.Endpoint(), newEndpoint)
	}
}
