package router

import (
	"encoding/json"
	"testing"
)

// TestResponseWithHeaders tests that response schemas can include headers
func TestResponseWithHeaders(t *testing.T) {
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

	// Response with headers and body
	type ListWorkspacesResponse struct {
		// Response headers
		CacheControl string `header:"Cache-Control"`
		ETag         string `header:"ETag"`
		XRateLimit   int    `header:"X-Rate-Limit"`

		// Response body
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

	specJSON, _ := json.MarshalIndent(spec, "", "  ")
	t.Logf("Generated spec:\n%s", string(specJSON))

	// Check the response
	pathItem, ok := spec.Paths["/api/workspaces"]
	if !ok {
		t.Fatal("Path /api/workspaces not found")
	}

	if pathItem.Get == nil {
		t.Fatal("GET operation not found")
	}

	response, ok := pathItem.Get.Responses["200"]
	if !ok {
		t.Fatal("200 response not found")
	}

	// Verify headers
	if response.Headers == nil || len(response.Headers) == 0 {
		t.Fatal("Expected response headers, got none")
	}

	t.Logf("Found %d response headers", len(response.Headers))

	// Check each header
	expectedHeaders := []string{"Cache-Control", "ETag", "X-Rate-Limit"}
	for _, headerName := range expectedHeaders {
		if header, ok := response.Headers[headerName]; !ok {
			t.Errorf("Expected header '%s' not found", headerName)
		} else {
			t.Logf("✓ Found header: %s (schema: %+v)", headerName, header.Schema)
		}
	}

	// Verify body schema
	if response.Content == nil {
		t.Fatal("Expected response content, got none")
	}

	jsonContent, ok := response.Content["application/json"]
	if !ok {
		t.Fatal("Expected application/json content type")
	}

	if jsonContent.Schema == nil {
		t.Fatal("Expected response schema")
	}

	t.Logf("✓ Response has body schema")

	// The body should contain the PaginatedResponse data, not wrapped in "Body"
	schemaJSON, _ := json.MarshalIndent(jsonContent.Schema, "", "  ")
	t.Logf("Response body schema:\n%s", string(schemaJSON))
}

// TestResponseWithoutHeaders tests backward compatibility - responses without headers
func TestResponseWithoutHeaders(t *testing.T) {
	type SimpleResponse struct {
		Message string `json:"message"`
		Success bool   `json:"success"`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/api/simple", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "Simple response", SimpleResponse{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	pathItem := spec.Paths["/api/simple"]
	if pathItem == nil || pathItem.Get == nil {
		t.Fatal("GET /api/simple operation not found")
	}

	response := pathItem.Get.Responses["200"]
	if response == nil {
		t.Fatal("200 response not found")
	}

	// Should have no headers
	if response.Headers != nil && len(response.Headers) > 0 {
		t.Errorf("Expected no headers for simple response, got %d", len(response.Headers))
	}

	// Should have content
	if response.Content == nil {
		t.Fatal("Expected response content")
	}

	t.Log("✓ Simple response (no headers) works correctly")
}

// TestResponseHeadersOptional tests optional headers
func TestResponseHeadersOptional(t *testing.T) {
	type ResponseWithOptionalHeaders struct {
		// Required header
		ContentType string `header:"Content-Type"`

		// Optional headers
		ETag         string `header:"ETag" optional:"true"`
		LastModified string `header:"Last-Modified" optional:"true"`

		// Body
		Data string `json:"data" body:""`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.GET("/api/data", func(ctx Context) error {
		return nil
	}, WithResponseSchema(200, "Data with headers", ResponseWithOptionalHeaders{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	response := spec.Paths["/api/data"].Get.Responses["200"]

	// Check required header
	if header, ok := response.Headers["Content-Type"]; !ok {
		t.Error("Expected Content-Type header")
	} else {
		if !header.Required {
			t.Error("Expected Content-Type to be required")
		} else {
			t.Log("✓ Content-Type is required")
		}
	}

	// Check optional headers
	optionalHeaders := []string{"ETag", "Last-Modified"}
	for _, headerName := range optionalHeaders {
		if header, ok := response.Headers[headerName]; !ok {
			t.Errorf("Expected header '%s'", headerName)
		} else {
			if header.Required {
				t.Errorf("Expected header '%s' to be optional", headerName)
			} else {
				t.Logf("✓ %s is optional", headerName)
			}
		}
	}
}

// TestResponseOnlyHeaders tests response with only headers (no body)
func TestResponseOnlyHeaders(t *testing.T) {
	type ResponseOnlyHeaders struct {
		Location string `header:"Location"`
		XTraceID string `header:"X-Trace-ID"`
	}

	router := NewRouter(WithOpenAPI(OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}))

	router.POST("/api/redirect", func(ctx Context) error {
		return nil
	}, WithResponseSchema(302, "Redirect", ResponseOnlyHeaders{}))

	spec := router.OpenAPISpec()
	if spec == nil {
		t.Fatal("OpenAPI spec is nil")
	}

	response := spec.Paths["/api/redirect"].Post.Responses["302"]

	// Debug: Print the response
	respJSON, _ := json.MarshalIndent(response, "", "  ")
	t.Logf("Response:\n%s", string(respJSON))

	// Should have headers
	if len(response.Headers) != 2 {
		t.Errorf("Expected 2 headers, got %d", len(response.Headers))
	}

	// Should have no content (headers only)
	if response.Content != nil && len(response.Content) > 0 {
		t.Errorf("Expected no content for headers-only response, got %d content types", len(response.Content))
	} else {
		t.Log("✓ Headers-only response works correctly")
	}
}

