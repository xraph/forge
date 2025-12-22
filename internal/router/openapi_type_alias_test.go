package router

import (
	"encoding/json"
	"testing"
)

// TestTypeAliasResolution verifies that type aliases are properly resolved,
// not converted to "string"
func TestTypeAliasResolution(t *testing.T) {
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
		Meta *PageMeta `json:"meta,omitempty"`
	}

	// Type alias (this is what the user has)
	type ListWorkspacesResult = PaginatedResponse[*Workspace]

	type ListWorkspacesResponse struct {
		Body ListWorkspacesResult `json:"body" body:""`
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

	// Get the response content schema
	response := spec.Paths["/api/workspaces"].Get.Responses["200"]
	if response == nil || response.Content == nil {
		t.Fatal("Response or content is nil")
	}

	jsonContent := response.Content["application/json"]
	if jsonContent == nil || jsonContent.Schema == nil {
		t.Fatal("JSON content or schema is nil")
	}

	schemaJSON, _ := json.MarshalIndent(jsonContent.Schema, "", "  ")
	t.Logf("Response schema:\n%s", string(schemaJSON))

	// Should be a $ref, not an inline schema
	if jsonContent.Schema.Ref == "" {
		t.Error("Expected a $ref in schema")
	}

	// Should reference PaginatedResponse, not ListWorkspacesResponse
	if jsonContent.Schema.Ref == "#/components/schemas/ListWorkspacesResponse" {
		t.Error("Schema should NOT reference the wrapper struct")
	}

	// Verify it's referencing the generic type
	t.Logf("✓ Schema correctly references: %s", jsonContent.Schema.Ref)

	// Follow the ref and verify it's a proper object schema, NOT "string"
	if spec.Components != nil && spec.Components.Schemas != nil {
		// Extract component name from ref
		prefix := "#/components/schemas/"
		refName := jsonContent.Schema.Ref
		if len(refName) > len(prefix) && refName[:len(prefix)] == prefix {
			componentName := refName[len(prefix):]
			component := spec.Components.Schemas[componentName]
			if component == nil {
				t.Fatalf("Referenced component %s not found", componentName)
			}

			componentJSON, _ := json.MarshalIndent(component, "", "  ")
			t.Logf("Component schema:\n%s", string(componentJSON))

			// Verify it's an object, not a string
			if component.Type == "string" {
				t.Error("❌ Component type is 'string' - this is the bug!")
			} else if component.Type == "object" {
				t.Log("✓ Component is properly typed as 'object'")
			}

			// Verify it has the expected properties
			if component.Properties == nil {
				t.Error("Component should have properties")
			} else {
				if _, hasData := component.Properties["data"]; hasData {
					t.Log("✓ Component has 'data' property")
				} else {
					t.Error("Component missing 'data' property")
				}

				if _, hasMeta := component.Properties["meta"]; hasMeta {
					t.Log("✓ Component has 'meta' property")
				} else {
					t.Error("Component missing 'meta' property")
				}
			}

			// Verify required fields
			if len(component.Required) == 0 {
				t.Error("Component should have required fields")
			} else {
				t.Logf("✓ Component has %d required field(s): %v", len(component.Required), component.Required)
			}
		}
	}
}

// TestTypeAliasWithoutJSONTag tests the exact user scenario without json tag
func TestTypeAliasWithoutJSONTag(t *testing.T) {
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
		Meta *PageMeta `json:"meta,omitempty"`
	}

	type ListWorkspacesResult = PaginatedResponse[*Workspace]

	// Without json tag (user might try this)
	type ListWorkspacesResponse struct {
		Body ListWorkspacesResult `body:""`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/api/workspaces", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "List of workspaces", ListWorkspacesResponse{}))

	spec := router.OpenAPISpec()
	response := spec.Paths["/api/workspaces"].Get.Responses["200"]
	jsonContent := response.Content["application/json"]

	// Verify not "string"
	if jsonContent.Schema.Ref == "" {
		t.Error("Expected a $ref in schema")
	} else {
		t.Logf("✓ Schema is a reference: %s", jsonContent.Schema.Ref)
	}

	// Get the actual component
	if spec.Components != nil && spec.Components.Schemas != nil {
		// Strip "#/components/schemas/" prefix
		prefix := "#/components/schemas/"
		refName := jsonContent.Schema.Ref
		if len(refName) > len(prefix) {
			refName = refName[len(prefix):]
		}
		component := spec.Components.Schemas[refName]
		if component == nil {
			t.Fatalf("Component not found: %s", refName)
		}

		if component.Type == "string" {
			t.Fatalf("❌ BUG: Component type is 'string', expected 'object'")
		}

		if component.Type == "object" && component.Properties != nil {
			t.Logf("✓ Component is properly typed as object with %d properties", len(component.Properties))
		}
	}
}
