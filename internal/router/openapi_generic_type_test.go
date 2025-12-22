package router

import (
	"encoding/json"
	"testing"
)

// Test generic types and type aliases in response schemas
func TestGenericTypeResponseSchema(t *testing.T) {
	// Simulate the user's structure
	type Workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	type PageMeta struct {
		Page       int `json:"page"`
		Limit      int `json:"limit"`
		Total      int `json:"total"`
		TotalPages int `json:"totalPages"`
	}

	type PaginatedResponse[T any] struct {
		Data []T       `json:"data"`
		Meta *PageMeta `json:"meta"`
	}

	// Type alias (like the user has)
	type ListWorkspacesResult = PaginatedResponse[*Workspace]

	// Wrapped in response struct (user's current approach)
	type ListWorkspacesResponse struct {
		Body ListWorkspacesResult `body:"body"`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	// Test 1: User's current approach (wrapped with body tag)
	router.GET("/api/workspaces/wrapped", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "Wrapped response", ListWorkspacesResponse{}))

	// Test 2: Direct type alias
	router.GET("/api/workspaces/direct-alias", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "Direct alias", ListWorkspacesResult{}))

	// Test 3: Direct generic type
	router.GET("/api/workspaces/direct-generic", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "Direct generic", PaginatedResponse[*Workspace]{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	specJSON, _ := json.MarshalIndent(spec, "", "  ")
	t.Logf("Generated spec:\n%s", string(specJSON))

	// Check wrapped response
	if path, ok := spec.Paths["/api/workspaces/wrapped"]; ok {
		if path.Get != nil && path.Get.Responses != nil {
			if resp, ok := path.Get.Responses["200"]; ok {
				t.Logf("\n=== Wrapped Response Schema ===")
				respJSON, _ := json.MarshalIndent(resp, "", "  ")
				t.Logf("%s", string(respJSON))
			}
		}
	}

	// Check direct alias response
	if path, ok := spec.Paths["/api/workspaces/direct-alias"]; ok {
		if path.Get != nil && path.Get.Responses != nil {
			if resp, ok := path.Get.Responses["200"]; ok {
				t.Logf("\n=== Direct Alias Response Schema ===")
				respJSON, _ := json.MarshalIndent(resp, "", "  ")
				t.Logf("%s", string(respJSON))
			}
		}
	}

	// Check direct generic response
	if path, ok := spec.Paths["/api/workspaces/direct-generic"]; ok {
		if path.Get != nil && path.Get.Responses != nil {
			if resp, ok := path.Get.Responses["200"]; ok {
				t.Logf("\n=== Direct Generic Response Schema ===")
				respJSON, _ := json.MarshalIndent(resp, "", "  ")
				t.Logf("%s", string(respJSON))
			}
		}
	}
}
