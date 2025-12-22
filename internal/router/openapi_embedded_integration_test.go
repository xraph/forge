package router

import (
	"encoding/json"
	"testing"
)

// Pagination types similar to user's example
type PaginationParamsIntegration struct {
	Page  int `query:"page" json:"page" description:"Page number" default:"1"`
	Limit int `query:"limit" json:"limit" description:"Items per page" default:"10"`
}

type ListWorkspacesRequestIntegration struct {
	PaginationParamsIntegration
	Plan string `query:"plan" json:"plan,omitempty" description:"Filter by plan"`
}

type WorkspaceIntegration struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Plan string `json:"plan"`
}

type ListWorkspacesResponseIntegration struct {
	Workspaces []WorkspaceIntegration `json:"workspaces"`
	Total      int                    `json:"total"`
}

func TestOpenAPIGenerator_EmbeddedStructIntegration(t *testing.T) {
	// Create a router with OpenAPI
	r := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:       "Workspace API",
		Description: "Testing embedded struct flattening",
		Version:     "1.0.0",
	}))

	// Register route with embedded struct in request
	r.GET("/workspaces", func(ctx Context) error {
		var req ListWorkspacesRequestIntegration
		if err := ctx.BindRequest(&req); err != nil {
			return err
		}
		return ctx.JSON(200, ListWorkspacesResponseIntegration{
			Workspaces: []WorkspaceIntegration{{ID: "1", Name: "Test", Plan: "pro"}},
			Total:      1,
		})
	},
		WithRequestSchema(ListWorkspacesRequestIntegration{}),
		WithResponse(200, "List of workspaces", ListWorkspacesResponseIntegration{}),
	)

	// Generate OpenAPI spec
	spec := r.OpenAPISpec()

	// Verify spec was generated
	if spec == nil {
		t.Fatal("Expected OpenAPI spec to be generated")
	}

	// Convert to JSON for inspection
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal spec: %v", err)
	}

	t.Logf("Generated OpenAPI Spec:\n%s", string(specJSON))

	// Verify the GET /workspaces operation exists
	if spec.Paths == nil || spec.Paths["/workspaces"] == nil {
		t.Fatal("Expected /workspaces path to be in spec")
	}

	operation := spec.Paths["/workspaces"].Get
	if operation == nil {
		t.Fatal("Expected GET operation on /workspaces")
	}

	// Verify query parameters are flattened from embedded struct
	if len(operation.Parameters) < 2 {
		t.Errorf("Expected at least 2 parameters (page, limit), got %d", len(operation.Parameters))
	}

	// Check for flattened parameters
	foundPage := false
	foundLimit := false
	foundPlan := false

	for _, param := range operation.Parameters {
		switch param.Name {
		case "page":
			foundPage = true
			if param.In != "query" {
				t.Error("Expected page parameter to be in query")
			}
			if param.Schema.Type != "integer" {
				t.Error("Expected page parameter to be integer")
			}
			if param.Schema.Description != "Page number" {
				t.Error("Expected page description to be preserved")
			}
		case "limit":
			foundLimit = true
			if param.In != "query" {
				t.Error("Expected limit parameter to be in query")
			}
			if param.Schema.Type != "integer" {
				t.Error("Expected limit parameter to be integer")
			}
		case "plan":
			foundPlan = true
			if param.In != "query" {
				t.Error("Expected plan parameter to be in query")
			}
			if param.Schema.Type != "string" {
				t.Error("Expected plan parameter to be string")
			}
		}
	}

	if !foundPage {
		t.Error("Expected 'page' parameter to be in operation (should be flattened from embedded struct)")
	}

	if !foundLimit {
		t.Error("Expected 'limit' parameter to be in operation (should be flattened from embedded struct)")
	}

	if !foundPlan {
		t.Error("Expected 'plan' parameter to be in operation")
	}

	// Verify response exists (schema details depend on WithResponse implementation)
	if operation.Responses == nil || operation.Responses["200"] == nil {
		t.Error("Expected 200 response to be defined")
	}

	t.Log("✓ Successfully verified embedded struct flattening in OpenAPI spec")
	t.Log("✓ Query parameters from embedded PaginationParams are correctly flattened")
}

// Test with multiple levels of embedding
type BaseFilterIntegration struct {
	SortBy    string `query:"sort_by" json:"sort_by,omitempty"`
	SortOrder string `query:"sort_order" json:"sort_order,omitempty"`
}

type ExtendedPaginationIntegration struct {
	PaginationParamsIntegration
	BaseFilterIntegration
}

type AdvancedSearchRequestIntegration struct {
	ExtendedPaginationIntegration
	Query string `query:"q" json:"q" description:"Search query"`
}

func TestOpenAPIGenerator_NestedEmbeddedStructs(t *testing.T) {
	// Create a router with OpenAPI
	r := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Search API",
		Version: "1.0.0",
	}))

	// Register route with multiple levels of embedding
	r.GET("/search", func(ctx Context) error {
		return ctx.JSON(200, map[string]any{"results": []string{}})
	},
		WithRequestSchema(AdvancedSearchRequestIntegration{}),
	)

	// Generate OpenAPI spec
	spec := r.OpenAPISpec()

	operation := spec.Paths["/search"].Get
	if operation == nil {
		t.Fatal("Expected GET operation on /search")
	}

	// Verify all nested embedded fields are flattened
	expectedParams := map[string]bool{
		"page":       false,
		"limit":      false,
		"sort_by":    false,
		"sort_order": false,
		"q":          false,
	}

	for _, param := range operation.Parameters {
		if _, exists := expectedParams[param.Name]; exists {
			expectedParams[param.Name] = true
		}
	}

	for paramName, found := range expectedParams {
		if !found {
			t.Errorf("Expected parameter '%s' to be in operation (from nested embedded structs)", paramName)
		}
	}

	t.Log("✓ Successfully verified nested embedded struct flattening")
}
