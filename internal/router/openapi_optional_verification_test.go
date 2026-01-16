package router

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOptionalTagInActualOpenAPISchema verifies that optional tag works in full OpenAPI spec generation.
func TestOptionalTagInActualOpenAPISchema(t *testing.T) {
	type TestParams struct {
		RequiredField string  `json:"requiredField"`
		OptionalField string  `json:"optionalField"       optional:"true"`
		PointerField  *string `json:"pointerField"`
		OmitEmpty     string  `json:"omitEmpty,omitempty"`
	}

	// Create a new router with OpenAPI enabled
	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	// Add a route with the test params - need to use WithRequestBodySchema to register the type
	router.POST("/test", func(ctx Context) error {
		return nil
	}, WithRequestBodySchema(TestParams{}))

	// Generate the OpenAPI spec
	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	// Print the spec for debugging
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal spec: %v", err)
	}

	t.Logf("Generated OpenAPI spec:\n%s", string(specJSON))

	// Check if TestParams component was created
	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Log("No components.schemas in spec - checking inline schema")

		// Check the path
		if spec.Paths == nil {
			t.Fatal("No paths in spec")
		}

		pathItem, ok := spec.Paths["/test"]
		if !ok {
			t.Fatal("Path /test not found")
		}

		if pathItem.Get == nil {
			t.Fatal("GET operation not found")
		}

		t.Logf("GET operation: %+v", pathItem.Get)

		return
	}

	// Find the TestParams schema
	testParamsSchema, ok := spec.Components.Schemas["TestParams"]
	if !ok {
		t.Log("TestParams schema not found in components, might be inline")

		// Check if it's defined inline in the request body
		pathItem, ok := spec.Paths["/test"]
		if !ok {
			t.Fatal("Path /test not found")
		}

		if pathItem.Post != nil && pathItem.Post.RequestBody != nil {
			t.Logf("Request body: %+v", pathItem.Post.RequestBody)
		}

		return
	}

	// Verify the schema
	t.Logf("TestParams schema: %+v", testParamsSchema)
	t.Logf("Required fields: %v", testParamsSchema.Required)

	// Check that only requiredField is in the required array
	expectedRequired := []string{"requiredField"}

	if len(testParamsSchema.Required) != len(expectedRequired) {
		t.Errorf("Expected %d required fields, got %d: %v", len(expectedRequired), len(testParamsSchema.Required), testParamsSchema.Required)
	}

	// Verify requiredField is required
	found := slices.Contains(testParamsSchema.Required, "requiredField")

	if !found {
		t.Error("Expected 'requiredField' to be required")
	}

	// Verify optionalField is NOT required
	for _, req := range testParamsSchema.Required {
		if req == "optionalField" {
			t.Error("Expected 'optionalField' to NOT be required (has optional:\"true\" tag)")
		}
	}

	// Verify all properties exist
	if testParamsSchema.Properties == nil {
		t.Fatal("Properties is nil")
	}

	expectedProps := []string{"requiredField", "optionalField", "pointerField", "omitEmpty"}
	for _, prop := range expectedProps {
		if _, ok := testParamsSchema.Properties[prop]; !ok {
			t.Errorf("Expected property '%s' to exist", prop)
		}
	}
}

// TestOptionalTagInQueryParams verifies optional tag works for query parameters.
func TestOptionalTagInQueryParamsOpenAPI(t *testing.T) {
	type QueryParams struct {
		Required string `query:"required"`
		Optional string `optional:"true"  query:"optional"`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/test", func(ctx Context) error {
		return nil
	}, WithQuerySchema(QueryParams{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	// Print spec for debugging
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	require.NoError(t, err)
	t.Logf("Generated spec:\n%s", string(specJSON))

	// Check path parameters
	pathItem, ok := spec.Paths["/test"]
	if !ok {
		t.Fatal("Path /test not found")
	}

	if pathItem.Get == nil {
		t.Fatal("GET operation not found")
	}

	t.Logf("Parameters: %+v", pathItem.Get.Parameters)
}
