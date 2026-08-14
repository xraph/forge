package router

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// resolveBodySchema follows a request body's $ref into components, and returns
// a schema that is not a reference unchanged.
//
// A unified request's body is synthesized from whatever fields carry no path,
// query or header tag, and is registered as a component so that consumers which
// key off a schema name -- client codec tables, most of all -- can find it. That
// makes the emitted body a $ref, so a test reading its properties has to take
// the hop first.
func resolveBodySchema(t *testing.T, spec *OpenAPISpec, schema *Schema) *Schema {
	t.Helper()

	if schema == nil || schema.Ref == "" {
		return schema
	}

	const prefix = "#/components/schemas/"

	name := strings.TrimPrefix(schema.Ref, prefix)
	if name == schema.Ref {
		t.Fatalf("request body $ref %q is not a components/schemas reference", schema.Ref)
	}

	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Fatalf("request body references %q but the spec has no components", name)
	}

	resolved, ok := spec.Components.Schemas[name]
	if !ok {
		t.Fatalf("request body references %q, which is not in components/schemas", name)
	}

	return resolved
}

// TestUserExampleEmbeddedStruct verifies the user's exact reported issue is fixed
// Uses PaginationParams and ListWorkspacesRequest from openapi_schema_embedded_test.go.
func TestUserExampleEmbeddedStruct(t *testing.T) {
	// Test 1: Schema generation - embedded fields should be flattened
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	schema, err := gen.GenerateSchema(ListWorkspacesRequest{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	// Verify all fields are flattened at top level
	if _, exists := schema.Properties["page"]; !exists {
		t.Error("Expected 'page' field from embedded PaginationParams to be flattened")
	}

	if _, exists := schema.Properties["limit"]; !exists {
		t.Error("Expected 'limit' field from embedded PaginationParams to be flattened")
	}

	if _, exists := schema.Properties["plan"]; !exists {
		t.Error("Expected 'plan' field to be present")
	}

	// Test 2: OpenAPI parameter extraction
	// Note: Only fields with query:"" tags are extracted as query parameters
	queryParams := generateQueryParamsFromStruct(gen, ListWorkspacesRequest{})

	foundPlan := false

	for _, param := range queryParams {
		if param.Name == "plan" {
			foundPlan = true

			break
		}
	}

	if !foundPlan {
		t.Error("Expected 'plan' query parameter to be extracted")
	}

	// page and limit don't have query tags in this test, so they're body fields

	// Test 3: Full OpenAPI spec generation
	r := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	r.GET("/workspaces", func(ctx Context) error {
		return ctx.JSON(200, map[string]any{})
	},
		WithRequestSchema(ListWorkspacesRequest{}),
	)

	spec := r.OpenAPISpec()
	if spec == nil {
		t.Fatal("Expected OpenAPI spec to be generated")
	}

	operation := spec.Paths["/workspaces"].Get
	if operation == nil {
		t.Fatal("Expected GET operation on /workspaces")
	}

	// Verify query parameters and body schema
	foundPlanParam := false

	for _, param := range operation.Parameters {
		if param.Name == "plan" {
			foundPlanParam = true

			break
		}
	}

	if !foundPlanParam {
		t.Error("Expected 'plan' query parameter in OpenAPI spec")
	}

	// Verify that page and limit are in the request body (they don't have query tags)
	if operation.RequestBody == nil {
		t.Fatal("Expected request body to be defined")
	}

	bodySchema := operation.RequestBody.Content["application/json"].Schema
	if bodySchema == nil {
		t.Fatal("Expected body schema to be defined")
	}

	// A synthesized body is registered as a component and referenced here, so
	// the properties this test is about live one hop away. What is being
	// asserted is unchanged: page and limit carry no query tag, so the field
	// walk must have left them in the body rather than promoting them to
	// parameters alongside plan.
	bodySchema = resolveBodySchema(t, spec, bodySchema)

	if _, exists := bodySchema.Properties["page"]; !exists {
		t.Error("Expected 'page' to be in request body schema")
	}

	if _, exists := bodySchema.Properties["limit"]; !exists {
		t.Error("Expected 'limit' to be in request body schema")
	}

	// Log the generated spec for verification
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	require.NoError(t, err)
	t.Logf("Generated OpenAPI spec:\n%s", string(specJSON))

	t.Log("✓ User's embedded struct issue is FIXED!")
	t.Log("✓ Nested structs are properly flattened")
	t.Log("✓ Schema components are correctly generated")
}
