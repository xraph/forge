package router

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUserResponseStructure tests the exact user's structure.
func TestUserResponseStructure(t *testing.T) {
	// Exactly as the user has it
	type Workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	type PageMeta struct {
		Page  int `json:"page"`
		Limit int `json:"limit"`
		Total int `json:"total"`
	}

	type PaginatedResponse[T any] struct {
		Data []T       `json:"data"`
		Meta *PageMeta `json:"meta"`
	}

	// Type alias
	type ListWorkspacesResult = PaginatedResponse[*Workspace]

	// Response with headers and body (user's exact structure)
	type ListWorkspacesResponse struct {
		Body ListWorkspacesResult `body:""` // Note: body:"" with no json tag
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/api/workspaces", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "List of workspaces", ListWorkspacesResponse{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	specJSON, err := json.MarshalIndent(spec, "", "  ")
	require.NoError(t, err)
	t.Logf("Generated spec:\n%s", string(specJSON))

	// Check the response
	pathItem := spec.Paths["/api/workspaces"]
	if pathItem == nil || pathItem.Get == nil {
		t.Fatal("GET /api/workspaces not found")
	}

	response := pathItem.Get.Responses["200"]
	if response == nil {
		t.Fatal("200 response not found")
	}

	// Should have content
	if response.Content == nil {
		t.Fatal("Expected response content")
	}

	jsonContent := response.Content["application/json"]
	if jsonContent == nil {
		t.Fatal("Expected application/json content")
	}

	// Check the schema
	schemaJSON, marshalErr := json.MarshalIndent(jsonContent.Schema, "", "  ")
	require.NoError(t, marshalErr)
	t.Logf("Response schema:\n%s", string(schemaJSON))

	// The schema should be a ref to PaginatedResponse, NOT to ListWorkspacesResponse
	switch jsonContent.Schema.Ref {
	case "":
		t.Error("Expected a $ref in schema")
	case "#/components/schemas/ListWorkspacesResponse":
		t.Error("Schema should NOT reference ListWorkspacesResponse wrapper")
	default:
		t.Logf("✓ Schema correctly unwrapped: %s", jsonContent.Schema.Ref)
	}

	// Verify the components
	if spec.Components != nil && spec.Components.Schemas != nil {
		// Should have PaginatedResponse schema
		found := false

		for name := range spec.Components.Schemas {
			t.Logf("Component: %s", name)

			if name == "PaginatedResponse" ||
				(len(name) > 17 && name[:17] == "PaginatedResponse") {
				found = true
			}
		}

		if !found {
			t.Error("Expected PaginatedResponse component")
		}
	}
}

// TestUserResponseWithJSONTag tests with json tag included.
func TestUserResponseWithJSONTag(t *testing.T) {
	type Workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	type PageMeta struct {
		Page  int `json:"page"`
		Limit int `json:"limit"`
		Total int `json:"total"`
	}

	type PaginatedResponse[T any] struct {
		Data []T       `json:"data"`
		Meta *PageMeta `json:"meta"`
	}

	type ListWorkspacesResult = PaginatedResponse[*Workspace]

	// With json tag (as user currently has)
	type ListWorkspacesResponse struct {
		Body ListWorkspacesResult `body:"" json:"body"`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/api/workspaces", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "List of workspaces", ListWorkspacesResponse{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	response := spec.Paths["/api/workspaces"].Get.Responses["200"]
	jsonContent := response.Content["application/json"]

	schemaJSON, marshalErr := json.MarshalIndent(jsonContent.Schema, "", "  ")
	require.NoError(t, marshalErr)
	t.Logf("Response schema:\n%s", string(schemaJSON))

	// Should be unwrapped
	if jsonContent.Schema.Ref == "#/components/schemas/ListWorkspacesResponse" {
		t.Error("Schema should NOT reference ListWorkspacesResponse wrapper, it should be unwrapped")
	} else {
		t.Logf("✓ Schema correctly unwrapped: %s", jsonContent.Schema.Ref)
	}
}
