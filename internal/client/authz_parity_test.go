package client

import (
	"context"
	"reflect"
	"testing"

	"github.com/xraph/forge/internal/shared"
)

// TestAuthzParityBetweenIRBuilders asserts that a router-derived spec and a
// file-derived spec produce identical Authorization for the same API.
//
// The two builders resolving the same metadata differently has been a
// recurring defect in this package, which is why resolveEndpointAuthz is
// called from exactly two places and why this test exists to keep it that way.
// If you are here because this test failed, the likely cause is new
// authorization logic inlined at one call site instead of added to the
// resolver.
func TestAuthzParityBetweenIRBuilders(t *testing.T) {
	ext := map[string]any{
		"x-forge-authz": map[string]any{
			"roles":       []any{"editor", "admin", "viewer"},
			"permissions": []any{"users:write", "users:delete", "users:read"},
		},
	}

	fromResolver := resolveEndpointAuthz(ext)

	// Build both an introspected and a parsed endpoint carrying the same
	// extension, and assert each matches the resolver's own answer.
	live := introspectedEndpointForTest(t, ext)
	file := parsedEndpointForTest(t, ext)

	if !reflect.DeepEqual(live.Authorization, fromResolver) {
		t.Errorf("introspector Authorization = %+v, want %+v", live.Authorization, fromResolver)
	}

	if !reflect.DeepEqual(file.Authorization, fromResolver) {
		t.Errorf("spec parser Authorization = %+v, want %+v", file.Authorization, fromResolver)
	}
}

// introspectedEndpointForTest drives Introspector.extractFromOpenAPI, the
// real live-router builder, against a hand-built in-memory *shared.OpenAPISpec
// carrying ext on its one operation, and returns the resulting endpoint.
//
// This is not a stub: extractFromOpenAPI is the exact function
// Introspector.Introspect calls for a live router's OpenAPI document, and
// nothing here computes Authorization itself.
func introspectedEndpointForTest(t *testing.T, ext map[string]any) *Endpoint {
	t.Helper()

	openAPI := &shared.OpenAPISpec{
		OpenAPI: "3.0.0",
		Info:    shared.Info{Title: "Authz Parity", Version: "1.0.0"},
		Paths: map[string]*shared.PathItem{
			"/widgets": {
				Get: &shared.Operation{
					OperationID: "widgetList",
					Responses: map[string]*shared.Response{
						"200": {Description: "ok"},
					},
					Extensions: ext,
				},
			},
		},
	}

	spec := &APISpec{Schemas: map[string]*Schema{}, Security: []SecurityScheme{}}
	introspector := &Introspector{}

	if err := introspector.extractFromOpenAPI(spec, openAPI); err != nil {
		t.Fatalf("extractFromOpenAPI: %v", err)
	}

	if len(spec.Endpoints) != 1 {
		t.Fatalf("Endpoints = %d, want 1: %+v", len(spec.Endpoints), spec.Endpoints)
	}

	return &spec.Endpoints[0]
}

// parsedEndpointForTest drives NewSpecParser().ParseFile, the real
// file-based builder, against a YAML document written to disk carrying the
// same extension on its one operation, and returns the resulting endpoint.
//
// It reuses writeYAMLSpec from spec_parser_yaml_meta_test.go rather than
// duplicating it, and follows that file's established pattern of driving
// ParseFile against a real file on disk instead of a hand-built IR fixture.
func parsedEndpointForTest(t *testing.T, ext map[string]any) *Endpoint {
	t.Helper()

	operation := map[string]any{
		"operationId": "widgetList",
		"responses": map[string]any{
			"200": map[string]any{"description": "ok"},
		},
	}

	// Merge the same extension keys the live-router side receives directly,
	// rather than hard-coding the extension name here -- both helpers must
	// carry the identical raw ext into their respective builder.
	for k, v := range ext {
		operation[k] = v
	}

	path := writeYAMLSpec(t, "openapi.yaml", map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Authz Parity", "version": "1.0.0"},
		"paths": map[string]any{
			"/widgets": map[string]any{
				"get": operation,
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(spec.Endpoints) != 1 {
		t.Fatalf("Endpoints = %d, want 1: %+v", len(spec.Endpoints), spec.Endpoints)
	}

	return &spec.Endpoints[0]
}
