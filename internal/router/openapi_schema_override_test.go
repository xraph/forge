package router

import (
	"testing"
)

// TestSchemaNameOverride verifies that the schema:"name" tag overrides component names
func TestSchemaNameOverride(t *testing.T) {
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

	// Type alias (would normally generate long generic name)
	type ListWorkspacesResult = PaginatedResponse[*Workspace]

	// Response with schema name override
	type ListWorkspacesResponse struct {
		Body ListWorkspacesResult `body:"" schema:"WorkspaceList"`
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

	// Get the response schema
	response := spec.Paths["/api/workspaces"].Get.Responses["200"]
	if response == nil || response.Content == nil {
		t.Fatal("Response or content is nil")
	}

	jsonContent := response.Content["application/json"]
	if jsonContent == nil || jsonContent.Schema == nil {
		t.Fatal("JSON content or schema is nil")
	}

	// Should reference the custom schema name
	expectedRef := "#/components/schemas/WorkspaceList"
	if jsonContent.Schema.Ref != expectedRef {
		t.Errorf("Expected schema ref %q, got %q", expectedRef, jsonContent.Schema.Ref)
	} else {
		t.Logf("✓ Schema uses custom name: %s", jsonContent.Schema.Ref)
	}

	// Component should exist with custom name
	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Fatal("Components or schemas is nil")
	}

	component := spec.Components.Schemas["WorkspaceList"]
	if component == nil {
		t.Error("Component 'WorkspaceList' not found")
		t.Logf("Available components:")
		for name := range spec.Components.Schemas {
			t.Logf("  - %s", name)
		}
	} else {
		t.Log("✓ Component registered with custom name")
	}

	// Verify the component has correct structure
	if component != nil {
		if component.Type != "object" {
			t.Errorf("Expected component type 'object', got %q", component.Type)
		}
		if component.Properties == nil {
			t.Error("Component should have properties")
		} else {
			if _, hasData := component.Properties["data"]; !hasData {
				t.Error("Component missing 'data' property")
			} else {
				t.Log("✓ Component has 'data' property")
			}
			if _, hasMeta := component.Properties["meta"]; !hasMeta {
				t.Error("Component missing 'meta' property")
			} else {
				t.Log("✓ Component has 'meta' property")
			}
		}
	}
}

// TestSchemaNameOverrideWithHeaders verifies schema override works with response headers
func TestSchemaNameOverrideWithHeaders(t *testing.T) {
	type Item struct {
		ID string `json:"id"`
	}

	type ItemList struct {
		Items []Item `json:"items"`
	}

	type CachedItemListResponse struct {
		CacheControl string   `header:"Cache-Control"`
		ETag         string   `header:"ETag"`
		Body         ItemList `body:"" schema:"CachedItems"`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/api/items", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "Cached items", CachedItemListResponse{}))

	spec := router.OpenAPISpec()
	response := spec.Paths["/api/items"].Get.Responses["200"]

	// Check headers exist
	if response.Headers == nil || len(response.Headers) == 0 {
		t.Error("Expected response headers")
	} else {
		t.Logf("✓ Response has %d headers", len(response.Headers))
	}

	// Check schema uses custom name
	jsonContent := response.Content["application/json"]
	expectedRef := "#/components/schemas/CachedItems"
	if jsonContent.Schema.Ref != expectedRef {
		t.Errorf("Expected schema ref %q, got %q", expectedRef, jsonContent.Schema.Ref)
	} else {
		t.Logf("✓ Schema uses custom name: %s", jsonContent.Schema.Ref)
	}

	// Verify component exists
	if spec.Components.Schemas["CachedItems"] == nil {
		t.Error("Component 'CachedItems' not found")
	} else {
		t.Log("✓ Component registered with custom name")
	}
}

// TestSchemaNameOverrideFallback verifies fallback when no override is provided
func TestSchemaNameOverrideFallback(t *testing.T) {
	type SimpleData struct {
		Value string `json:"value"`
	}

	// Without schema override, should use default name
	type ResponseNoOverride struct {
		Body SimpleData `body:""`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/api/test", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "Test", ResponseNoOverride{}))

	spec := router.OpenAPISpec()
	response := spec.Paths["/api/test"].Get.Responses["200"]
	jsonContent := response.Content["application/json"]

	// Should still have a ref (using default name)
	if jsonContent.Schema.Ref == "" {
		t.Error("Expected a schema ref")
	} else {
		t.Logf("✓ Schema uses default name: %s", jsonContent.Schema.Ref)
	}

	// The ref should contain "SimpleData"
	if jsonContent.Schema.Ref != "#/components/schemas/SimpleData" {
		t.Errorf("Expected ref to SimpleData, got %s", jsonContent.Schema.Ref)
	}
}
