package orpc

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
		{"method": "POST", "path": "/rpc"},
	}
}

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name         string
		specVersion  string
		endpoint     string
		wantVersion  string
		wantEndpoint string
	}{
		{
			name:         "default values",
			specVersion:  "",
			endpoint:     "",
			wantVersion:  "1.0.0",
			wantEndpoint: "/orpc.json",
		},
		{
			name:         "explicit values",
			specVersion:  "2.0.0",
			endpoint:     "/api/orpc.json",
			wantVersion:  "2.0.0",
			wantEndpoint: "/api/orpc.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider(tt.specVersion, tt.endpoint)
			if p.SpecVersion() != tt.wantVersion {
				t.Errorf("SpecVersion() = %v, want %v", p.SpecVersion(), tt.wantVersion)
			}

			if p.Endpoint() != tt.wantEndpoint {
				t.Errorf("Endpoint() = %v, want %v", p.Endpoint(), tt.wantEndpoint)
			}

			if p.Type() != farp.SchemaTypeORPC {
				t.Errorf("Type() = %v, want %v", p.Type(), farp.SchemaTypeORPC)
			}
		})
	}
}

func TestProvider_Generate(t *testing.T) {
	app := &mockApp{
		name:    "test-service",
		version: "1.0.0",
	}

	p := NewProvider("1.0.0", "/orpc.json")

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

	// Check for expected fields
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		t.Fatal("schema is not a map")
	}

	if _, ok := schemaMap["orpc"]; !ok {
		t.Error("schema missing 'orpc' field")
	}

	if _, ok := schemaMap["procedures"]; !ok {
		t.Error("schema missing 'procedures' field")
	}

	if _, ok := schemaMap["info"]; !ok {
		t.Error("schema missing 'info' field")
	}
}

func TestProvider_Validate(t *testing.T) {
	p := NewProvider("1.0.0", "/orpc.json")

	tests := []struct {
		name    string
		schema  any
		wantErr bool
	}{
		{
			name: "valid schema",
			schema: map[string]any{
				"orpc": "1.0.0",
				"info": map[string]any{
					"title":   "Test API",
					"version": "1.0.0",
				},
				"procedures": map[string]any{
					"getProcedure": map[string]any{
						"input": map[string]any{
							"type": "object",
						},
						"output": map[string]any{
							"type": "object",
						},
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
			name: "missing orpc field",
			schema: map[string]any{
				"info":       map[string]any{},
				"procedures": map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "missing info field",
			schema: map[string]any{
				"orpc":       "1.0.0",
				"procedures": map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "missing procedures field",
			schema: map[string]any{
				"orpc": "1.0.0",
				"info": map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "procedures not an object",
			schema: map[string]any{
				"orpc":       "1.0.0",
				"info":       map[string]any{},
				"procedures": "not an object",
			},
			wantErr: true,
		},
		{
			name: "procedure missing input",
			schema: map[string]any{
				"orpc": "1.0.0",
				"info": map[string]any{},
				"procedures": map[string]any{
					"test": map[string]any{
						"output": map[string]any{},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "procedure missing output",
			schema: map[string]any{
				"orpc": "1.0.0",
				"info": map[string]any{},
				"procedures": map[string]any{
					"test": map[string]any{
						"input": map[string]any{},
					},
				},
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

	p := NewProvider("1.0.0", "/orpc.json")

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

	p := NewProvider("1.0.0", "/orpc.json")

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
				"url": "http://localhost:8080/orpc.json",
			},
			wantErr: false,
		},
		{
			name:         "HTTP with headers",
			locationType: farp.LocationTypeHTTP,
			locationConfig: map[string]string{
				"url":     "http://localhost:8080/orpc.json",
				"headers": `{"Authorization":"Bearer token"}`,
			},
			wantErr: false,
		},
		{
			name:         "Registry location",
			locationType: farp.LocationTypeRegistry,
			locationConfig: map[string]string{
				"registry_path": "/schemas/test-service/orpc",
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

				if descriptor.Type != farp.SchemaTypeORPC {
					t.Errorf("descriptor.Type = %v, want %v", descriptor.Type, farp.SchemaTypeORPC)
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

func TestProvider_SetEndpoint(t *testing.T) {
	p := NewProvider("1.0.0", "/orpc.json")

	newEndpoint := "/api/v2/orpc.json"
	p.SetEndpoint(newEndpoint)

	if p.Endpoint() != newEndpoint {
		t.Errorf("Endpoint() = %v, want %v", p.Endpoint(), newEndpoint)
	}
}

func TestProvider_ContentType(t *testing.T) {
	p := NewProvider("1.0.0", "/orpc.json")

	if p.ContentType() != "application/json" {
		t.Errorf("ContentType() = %v, want application/json", p.ContentType())
	}
}
