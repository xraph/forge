package router

import (
	"testing"
)

// TestOptionalTagSupport tests that the optional:"true" tag marks fields as not required
func TestOptionalTagSupport(t *testing.T) {
	// Test struct with various optional configurations
	type RequestParams struct {
		// Non-pointer without optional tag - should be required (default behavior)
		Required string `json:"required"`

		// Non-pointer with optional tag - should NOT be required
		OptionalField string `json:"optionalField" optional:"true"`

		// Pointer field - should NOT be required (default behavior)
		PointerField *string `json:"pointerField"`

		// Non-pointer with omitempty - should NOT be required
		OmitEmpty string `json:"omitEmpty,omitempty"`

		// Non-pointer with explicit required tag - should be required
		ExplicitRequired string `json:"explicitRequired" required:"true"`

		// Non-pointer with optional tag (takes precedence over required tag)
		OptionalWins string `json:"optionalWins" optional:"true" required:"true"`
	}

	gen := newSchemaGenerator(make(map[string]*Schema), nil)
	schema, err := gen.GenerateSchema(RequestParams{})
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Verify schema type
	if schema.Type != "object" {
		t.Errorf("Expected schema type 'object', got '%s'", schema.Type)
	}

	// Verify all fields are in properties
	expectedFields := []string{"required", "optionalField", "pointerField", "omitEmpty", "explicitRequired", "optionalWins"}
	if len(schema.Properties) != len(expectedFields) {
		t.Errorf("Expected %d properties, got %d", len(expectedFields), len(schema.Properties))
	}

	// Verify required fields
	// Should include: required, explicitRequired
	// Should NOT include: optionalField, pointerField, omitEmpty, optionalWins
	expectedRequired := []string{"required", "explicitRequired"}
	
	if len(schema.Required) != len(expectedRequired) {
		t.Errorf("Expected %d required fields, got %d: %v", len(expectedRequired), len(schema.Required), schema.Required)
	}

	// Check each expected required field
	requiredMap := make(map[string]bool)
	for _, r := range schema.Required {
		requiredMap[r] = true
	}

	for _, expected := range expectedRequired {
		if !requiredMap[expected] {
			t.Errorf("Expected field '%s' to be required, but it's not", expected)
		}
	}

	// Check that optional fields are NOT required
	notRequiredFields := []string{"optionalField", "pointerField", "omitEmpty", "optionalWins"}
	for _, field := range notRequiredFields {
		if requiredMap[field] {
			t.Errorf("Expected field '%s' to NOT be required, but it is", field)
		}
	}
}

// TestOptionalTagInEmbeddedStruct tests that optional tag works in embedded structs
func TestOptionalTagInEmbeddedStruct(t *testing.T) {
	type BaseParams struct {
		Search string `json:"search" optional:"true"`
		Filter string `json:"filter" optional:"true"`
	}

	type PaginationParams struct {
		BaseParams
		Limit  int `json:"limit" default:"10"`
		Offset int `json:"offset" optional:"true"`
	}

	gen := newSchemaGenerator(make(map[string]*Schema), nil)
	schema, err := gen.GenerateSchema(PaginationParams{})
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Verify that embedded fields are flattened
	if _, ok := schema.Properties["search"]; !ok {
		t.Error("Expected 'search' field from embedded struct")
	}

	if _, ok := schema.Properties["filter"]; !ok {
		t.Error("Expected 'filter' field from embedded struct")
	}

	// Verify required fields
	// Only 'limit' should be required (non-pointer, no optional tag, no omitempty)
	// search, filter, and offset have optional:"true"
	expectedRequired := []string{"limit"}
	
	if len(schema.Required) != len(expectedRequired) {
		t.Errorf("Expected %d required fields, got %d: %v", len(expectedRequired), len(schema.Required), schema.Required)
	}

	// Check that limit is required
	requiredMap := make(map[string]bool)
	for _, r := range schema.Required {
		requiredMap[r] = true
	}

	if !requiredMap["limit"] {
		t.Error("Expected 'limit' to be required")
	}

	// Check that optional fields are NOT required
	notRequiredFields := []string{"search", "filter", "offset"}
	for _, field := range notRequiredFields {
		if requiredMap[field] {
			t.Errorf("Expected field '%s' to NOT be required, but it is", field)
		}
	}
}

// TestOptionalTagWithQueryParams tests that optional tag works for query parameters
func TestOptionalTagWithQueryParams(t *testing.T) {
	type QueryParams struct {
		SortBy string `query:"sort_by" default:"created_at"`
		Order  string `query:"order" default:"desc"`
		Search string `query:"search" optional:"true"`
		Filter string `query:"filter" optional:"true"`
		Page   int    `query:"page" default:"1"`
	}

	gen := newSchemaGenerator(make(map[string]*Schema), nil)
	params := generateQueryParamsFromStruct(gen, QueryParams{})

	// Verify all query parameters are generated
	if len(params) != 5 {
		t.Errorf("Expected 5 query parameters, got %d", len(params))
	}

	// Create a map for easier checking
	paramMap := make(map[string]Parameter)
	for _, p := range params {
		paramMap[p.Name] = p
	}

	// Verify that fields without optional tag are required
	requiredFields := []string{"sort_by", "order", "page"}
	for _, field := range requiredFields {
		param, ok := paramMap[field]
		if !ok {
			t.Errorf("Expected parameter '%s' to exist", field)
			continue
		}
		if !param.Required {
			t.Errorf("Expected parameter '%s' to be required, but it's not", field)
		}
	}

	// Verify that fields with optional tag are NOT required
	optionalFields := []string{"search", "filter"}
	for _, field := range optionalFields {
		param, ok := paramMap[field]
		if !ok {
			t.Errorf("Expected parameter '%s' to exist", field)
			continue
		}
		if param.Required {
			t.Errorf("Expected parameter '%s' to NOT be required, but it is", field)
		}
	}
}

// TestOptionalTagWithHeaderParams tests that optional tag works for header parameters
func TestOptionalTagWithHeaderParams(t *testing.T) {
	type HeaderParams struct {
		Authorization string `header:"Authorization"` // Required by default
		ContentType   string `header:"Content-Type"`  // Required by default
		TraceID       string `header:"X-Trace-ID" optional:"true"`
		RequestID     string `header:"X-Request-ID" optional:"true"`
	}

	gen := newSchemaGenerator(make(map[string]*Schema), nil)
	params := generateHeaderParamsFromStruct(gen, HeaderParams{})

	// Verify all header parameters are generated
	if len(params) != 4 {
		t.Errorf("Expected 4 header parameters, got %d", len(params))
	}

	// Create a map for easier checking
	paramMap := make(map[string]Parameter)
	for _, p := range params {
		paramMap[p.Name] = p
	}

	// Verify that fields without optional tag are required
	requiredFields := []string{"Authorization", "Content-Type"}
	for _, field := range requiredFields {
		param, ok := paramMap[field]
		if !ok {
			t.Errorf("Expected parameter '%s' to exist", field)
			continue
		}
		if !param.Required {
			t.Errorf("Expected parameter '%s' to be required, but it's not", field)
		}
	}

	// Verify that fields with optional tag are NOT required
	optionalFields := []string{"X-Trace-ID", "X-Request-ID"}
	for _, field := range optionalFields {
		param, ok := paramMap[field]
		if !ok {
			t.Errorf("Expected parameter '%s' to exist", field)
			continue
		}
		if param.Required {
			t.Errorf("Expected parameter '%s' to NOT be required, but it is", field)
		}
	}
}

// TestOptionalTagPrecedence tests that optional tag takes precedence over required tag
func TestOptionalTagPrecedence(t *testing.T) {
	type ConflictingTags struct {
		// optional:"true" should take precedence over required:"true"
		Field1 string `json:"field1" optional:"true" required:"true"`
		
		// required:"true" should work when optional is not set
		Field2 string `json:"field2" required:"true"`
	}

	gen := newSchemaGenerator(make(map[string]*Schema), nil)
	schema, err := gen.GenerateSchema(ConflictingTags{})
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Only field2 should be required
	if len(schema.Required) != 1 {
		t.Errorf("Expected 1 required field, got %d: %v", len(schema.Required), schema.Required)
	}

	if len(schema.Required) > 0 && schema.Required[0] != "field2" {
		t.Errorf("Expected 'field2' to be required, got '%s'", schema.Required[0])
	}

	// Verify field1 is NOT in required list
	for _, r := range schema.Required {
		if r == "field1" {
			t.Error("Expected 'field1' to NOT be required (optional should take precedence), but it is")
		}
	}
}

