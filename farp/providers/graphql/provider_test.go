package graphql

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
		{"method": "POST", "path": "/graphql"},
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
			wantVersion:  "2021",
			wantEndpoint: "/graphql",
		},
		{
			name:         "explicit values",
			specVersion:  "2018",
			endpoint:     "/api/graphql",
			wantVersion:  "2018",
			wantEndpoint: "/api/graphql",
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

			if p.Type() != farp.SchemaTypeGraphQL {
				t.Errorf("Type() = %v, want %v", p.Type(), farp.SchemaTypeGraphQL)
			}
		})
	}
}

func TestProvider_Generate_SDL(t *testing.T) {
	app := &mockApp{
		name:    "test-service",
		version: "1.0.0",
	}

	p := NewProvider("2021", "/graphql")
	p.UseSDL() // Ensure SDL mode

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

	// Check for SDL field
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		t.Fatal("schema is not a map")
	}

	if _, ok := schemaMap["sdl"]; !ok {
		t.Error("schema missing 'sdl' field")
	}

	format, _ := schemaMap["format"].(string)
	if format != "SDL" {
		t.Errorf("format = %v, want SDL", format)
	}
}

func TestProvider_Generate_Introspection(t *testing.T) {
	app := &mockApp{
		name:    "test-service",
		version: "1.0.0",
	}

	p := NewProvider("2021", "/graphql")
	p.UseIntrospection() // Use introspection mode

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

	// Check for introspection structure
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		t.Fatal("schema is not a map")
	}

	if _, ok := schemaMap["data"]; !ok {
		t.Error("schema missing 'data' field")
	}

	format, _ := schemaMap["format"].(string)
	if format != "introspection" {
		t.Errorf("format = %v, want introspection", format)
	}
}

func TestProvider_Validate(t *testing.T) {
	p := NewProvider("2021", "/graphql")

	tests := []struct {
		name    string
		schema  any
		wantErr bool
	}{
		{
			name: "valid SDL schema",
			schema: map[string]any{
				"format": "SDL",
				"sdl":    "type Query { hello: String }",
			},
			wantErr: false,
		},
		{
			name: "valid introspection schema",
			schema: map[string]any{
				"format": "introspection",
				"data": map[string]any{
					"__schema": map[string]any{
						"types": []any{},
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
			name: "SDL without sdl field",
			schema: map[string]any{
				"format": "SDL",
			},
			wantErr: true,
		},
		{
			name: "introspection without data field",
			schema: map[string]any{
				"format": "introspection",
			},
			wantErr: true,
		},
		{
			name: "introspection without __schema",
			schema: map[string]any{
				"format": "introspection",
				"data":   map[string]any{},
			},
			wantErr: true,
		},
		{
			name: "unknown format",
			schema: map[string]any{
				"format": "unknown",
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

	p := NewProvider("2021", "/graphql")

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

	p := NewProvider("2021", "/graphql")

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
				"url": "http://localhost:8080/graphql",
			},
			wantErr: false,
		},
		{
			name:         "Registry location",
			locationType: farp.LocationTypeRegistry,
			locationConfig: map[string]string{
				"registry_path": "/schemas/test-service/graphql",
			},
			wantErr: false,
		},
		{
			name:           "Inline location",
			locationType:   farp.LocationTypeInline,
			locationConfig: map[string]string{},
			wantErr:        false,
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

				if descriptor.Type != farp.SchemaTypeGraphQL {
					t.Errorf("descriptor.Type = %v, want %v", descriptor.Type, farp.SchemaTypeGraphQL)
				}

				if tt.locationType == farp.LocationTypeInline && descriptor.InlineSchema == nil {
					t.Error("descriptor.InlineSchema is nil for inline location")
				}
			}
		})
	}
}

func TestProvider_ContentType(t *testing.T) {
	tests := []struct {
		name            string
		useSDL          bool
		wantContentType string
	}{
		{
			name:            "SDL mode",
			useSDL:          true,
			wantContentType: "application/graphql",
		},
		{
			name:            "Introspection mode",
			useSDL:          false,
			wantContentType: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider("2021", "/graphql")
			if tt.useSDL {
				p.UseSDL()
			} else {
				p.UseIntrospection()
			}

			if p.ContentType() != tt.wantContentType {
				t.Errorf("ContentType() = %v, want %v", p.ContentType(), tt.wantContentType)
			}
		})
	}
}

func TestProvider_SetEndpoint(t *testing.T) {
	p := NewProvider("2021", "/graphql")

	newEndpoint := "/api/v2/graphql"
	p.SetEndpoint(newEndpoint)

	if p.Endpoint() != newEndpoint {
		t.Errorf("Endpoint() = %v, want %v", p.Endpoint(), newEndpoint)
	}
}
