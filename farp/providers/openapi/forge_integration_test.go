package openapi

import (
	"context"
	"testing"

	"github.com/xraph/farp"
)

// Mock router that implements OpenAPISpec().
type mockOpenAPIRouter struct {
	spec any
}

func (m *mockOpenAPIRouter) OpenAPISpec() any {
	return m.spec
}

func TestForgeProvider_GenerateFromRouter(t *testing.T) {
	provider := NewForgeProvider("3.1.0", "/openapi.json")

	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"paths": map[string]any{
			"/test": map[string]any{
				"get": map[string]any{
					"summary": "Test endpoint",
				},
			},
		},
	}

	router := &mockOpenAPIRouter{spec: spec}

	generated, err := provider.GenerateFromRouter(router)
	if err != nil {
		t.Fatalf("GenerateFromRouter failed: %v", err)
	}

	if generated == nil {
		t.Fatal("Generated spec is nil")
	}

	// Verify it's the same spec
	generatedMap, ok := generated.(map[string]any)
	if !ok {
		t.Fatal("Generated spec is not a map")
	}

	if generatedMap["openapi"] != "3.1.0" {
		t.Errorf("Expected openapi version 3.1.0, got %v", generatedMap["openapi"])
	}
}

func TestForgeProvider_GenerateFromRouter_NilSpec(t *testing.T) {
	provider := NewForgeProvider("3.1.0", "/openapi.json")
	router := &mockOpenAPIRouter{spec: nil}

	_, err := provider.GenerateFromRouter(router)
	if err == nil {
		t.Error("Expected error for nil spec, got nil")
	}
}

func TestForgeProvider_GenerateFromRouter_InvalidType(t *testing.T) {
	provider := NewForgeProvider("3.1.0", "/openapi.json")

	// Pass a type that doesn't implement OpenAPISpec()
	_, err := provider.GenerateFromRouter("not a router")
	if err == nil {
		t.Error("Expected error for invalid type, got nil")
	}
}

func TestForgeProvider_Validate(t *testing.T) {
	provider := NewForgeProvider("3.1.0", "/openapi.json")

	validSpec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"paths": map[string]any{},
	}

	err := provider.Validate(validSpec)
	if err != nil {
		t.Errorf("Validate failed for valid spec: %v", err)
	}
}

func TestForgeProvider_Validate_MissingFields(t *testing.T) {
	provider := NewForgeProvider("3.1.0", "/openapi.json")

	tests := []struct {
		name string
		spec map[string]any
	}{
		{
			name: "missing openapi field",
			spec: map[string]any{
				"info":  map[string]any{"title": "Test"},
				"paths": map[string]any{},
			},
		},
		{
			name: "missing info field",
			spec: map[string]any{
				"openapi": "3.1.0",
				"paths":   map[string]any{},
			},
		},
		{
			name: "missing paths field",
			spec: map[string]any{
				"openapi": "3.1.0",
				"info":    map[string]any{"title": "Test"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.Validate(tt.spec)
			if err == nil {
				t.Errorf("Expected validation error for %s", tt.name)
			}
		})
	}
}

func TestForgeProvider_Validate_NilSpec(t *testing.T) {
	provider := NewForgeProvider("3.1.0", "/openapi.json")

	err := provider.Validate(nil)
	if err == nil {
		t.Error("Expected error for nil spec, got nil")
	}
}

// Mock application that implements farp.Application and OpenAPISpecProvider.
type mockApp struct {
	name    string
	version string
	spec    any
}

func (m *mockApp) Name() string     { return m.name }
func (m *mockApp) Version() string  { return m.version }
func (m *mockApp) Routes() any      { return nil } // Required by farp.Application
func (m *mockApp) OpenAPISpec() any { return m.spec }

func TestForgeProvider_Generate_WithSpecProvider(t *testing.T) {
	provider := NewForgeProvider("3.1.0", "/openapi.json")

	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "App API",
			"version": "2.0.0",
		},
		"paths": map[string]any{},
	}

	app := &mockApp{
		name:    "test-app",
		version: "2.0.0",
		spec:    spec,
	}

	ctx := context.Background()

	generated, err := provider.Generate(ctx, app)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if generated == nil {
		t.Fatal("Generated spec is nil")
	}
}

func TestCreateForgeDescriptor(t *testing.T) {
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"paths": map[string]any{},
	}

	router := &mockOpenAPIRouter{spec: spec}

	locationConfig := map[string]string{
		"url": "http://localhost:8080/openapi.json",
	}

	descriptor, err := CreateForgeDescriptor(router, farp.LocationTypeHTTP, locationConfig)
	if err != nil {
		t.Fatalf("CreateForgeDescriptor failed: %v", err)
	}

	if descriptor.Type != farp.SchemaTypeOpenAPI {
		t.Errorf("Expected type %s, got %s", farp.SchemaTypeOpenAPI, descriptor.Type)
	}

	if descriptor.SpecVersion != "3.1.0" {
		t.Errorf("Expected spec version 3.1.0, got %s", descriptor.SpecVersion)
	}

	if descriptor.Location.Type != farp.LocationTypeHTTP {
		t.Errorf("Expected location type %s, got %s", farp.LocationTypeHTTP, descriptor.Location.Type)
	}

	if descriptor.Location.URL != "http://localhost:8080/openapi.json" {
		t.Errorf("Expected URL http://localhost:8080/openapi.json, got %s", descriptor.Location.URL)
	}

	if descriptor.Hash == "" {
		t.Error("Expected hash to be set")
	}

	if descriptor.Size == 0 {
		t.Error("Expected size to be > 0")
	}
}

func TestCreateForgeDescriptor_InlineLocation(t *testing.T) {
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"paths": map[string]any{},
	}

	router := &mockOpenAPIRouter{spec: spec}

	descriptor, err := CreateForgeDescriptor(router, farp.LocationTypeInline, map[string]string{})
	if err != nil {
		t.Fatalf("CreateForgeDescriptor failed: %v", err)
	}

	if descriptor.Location.Type != farp.LocationTypeInline {
		t.Errorf("Expected location type %s, got %s", farp.LocationTypeInline, descriptor.Location.Type)
	}

	if descriptor.InlineSchema == nil {
		t.Error("Expected inline schema to be set")
	}
}
