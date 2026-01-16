package router

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUnifiedRequestSchemaWithOptionalTag verifies that optional tag works with WithRequestSchema.
func TestUnifiedRequestSchemaWithOptionalTag(t *testing.T) {
	type ListWorkspacesRequest struct {
		// Query parameters with optional tag
		Page   int    `default:"1"  optional:"true" query:"page"`
		Limit  int    `default:"10" optional:"true" query:"limit"`
		Search string `default:""   optional:"true" query:"search"`
		Filter string `default:""   optional:"true" query:"filter"`
		Plan   string `query:"plan"` // Required (no optional tag)
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	// Use WithRequestSchema (unified schema) like the user's code
	router.GET("/api/workspaces", func(ctx Context) error {
		return nil
	}, WithRequestSchema(ListWorkspacesRequest{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	// Print spec for debugging
	specJSON, err := json.MarshalIndent(spec, "", "  ")
	require.NoError(t, err)
	t.Logf("Generated spec:\n%s", string(specJSON))

	// Check path
	pathItem, ok := spec.Paths["/api/workspaces"]
	if !ok {
		t.Fatal("Path /api/workspaces not found")
	}

	if pathItem.Get == nil {
		t.Fatal("GET operation not found")
	}

	// Verify parameters
	params := pathItem.Get.Parameters
	t.Logf("Found %d parameters", len(params))

	paramMap := make(map[string]Parameter)
	for _, p := range params {
		paramMap[p.Name] = p
		t.Logf("Parameter: name=%s, required=%v", p.Name, p.Required)
	}

	// Verify page parameter (optional)
	if p, ok := paramMap["page"]; ok {
		if p.Required {
			t.Error("Expected 'page' parameter to be optional (has optional:\"true\" tag)")
		}

		t.Log("✓ page parameter is correctly marked as optional")
	} else {
		t.Error("Parameter 'page' not found")
	}

	// Verify limit parameter (optional)
	if p, ok := paramMap["limit"]; ok {
		if p.Required {
			t.Error("Expected 'limit' parameter to be optional (has optional:\"true\" tag)")
		}

		t.Log("✓ limit parameter is correctly marked as optional")
	} else {
		t.Error("Parameter 'limit' not found")
	}

	// Verify search parameter (optional)
	if p, ok := paramMap["search"]; ok {
		if p.Required {
			t.Error("Expected 'search' parameter to be optional (has optional:\"true\" tag)")
		}

		t.Log("✓ search parameter is correctly marked as optional")
	} else {
		t.Error("Parameter 'search' not found")
	}

	// Verify filter parameter (optional)
	if p, ok := paramMap["filter"]; ok {
		if p.Required {
			t.Error("Expected 'filter' parameter to be optional (has optional:\"true\" tag)")
		}

		t.Log("✓ filter parameter is correctly marked as optional")
	} else {
		t.Error("Parameter 'filter' not found")
	}

	// Verify plan parameter (required - no optional tag)
	if p, ok := paramMap["plan"]; ok {
		if !p.Required {
			t.Error("Expected 'plan' parameter to be required (no optional tag)")
		}

		t.Log("✓ plan parameter is correctly marked as required")
	} else {
		t.Error("Parameter 'plan' not found")
	}
}

// TestUnifiedRequestSchemaWithEmbeddedOptional tests optional tag with embedded structs.
func TestUnifiedRequestSchemaWithEmbeddedOptional(t *testing.T) {
	type BasePaginationParams struct {
		Page  int `default:"1"  optional:"true" query:"page"`
		Limit int `default:"10" optional:"true" query:"limit"`
	}

	type SearchParams struct {
		Search string `default:"" optional:"true" query:"search"`
		Filter string `default:"" optional:"true" query:"filter"`
	}

	type ListRequest struct {
		BasePaginationParams
		SearchParams

		Required string `query:"required"` // Required field
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/items", func(ctx Context) error {
		return nil
	}, WithRequestSchema(ListRequest{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	pathItem := spec.Paths["/items"]
	if pathItem == nil || pathItem.Get == nil {
		t.Fatal("GET /items operation not found")
	}

	paramMap := make(map[string]bool)
	for _, p := range pathItem.Get.Parameters {
		paramMap[p.Name] = p.Required
		t.Logf("Parameter: %s, required=%v", p.Name, p.Required)
	}

	// All embedded optional fields should be optional
	optionalFields := []string{"page", "limit", "search", "filter"}
	for _, field := range optionalFields {
		if required, ok := paramMap[field]; !ok {
			t.Errorf("Parameter '%s' not found", field)
		} else if required {
			t.Errorf("Expected '%s' to be optional (has optional:\"true\" tag)", field)
		} else {
			t.Logf("✓ %s is correctly optional", field)
		}
	}

	// Required field should be required
	if required, ok := paramMap["required"]; !ok {
		t.Error("Parameter 'required' not found")
	} else if !required {
		t.Error("Expected 'required' to be required")
	} else {
		t.Log("✓ required is correctly required")
	}
}
