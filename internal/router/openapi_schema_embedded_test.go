package router

import (
	"testing"
)

// Test embedded struct flattening
type PaginationParams struct {
	Page  int `json:"page" description:"Page number"`
	Limit int `json:"limit" description:"Items per page"`
}

type ListWorkspacesRequest struct {
	PaginationParams
	Plan string `query:"plan" json:"plan,omitempty" description:"Filter by plan"`
}

type NestedRequest struct {
	PaginationParams `json:"pagination"`
	Name             string `json:"name"`
}

func TestEmbeddedStructFlattening(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	// Test embedded struct - should be flattened
	schema, err := gen.GenerateSchema(ListWorkspacesRequest{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	if schema.Type != "object" {
		t.Errorf("Expected type=object, got %s", schema.Type)
	}

	// Should have flattened fields: page, limit, plan
	expectedFields := []string{"page", "limit", "plan"}
	for _, field := range expectedFields {
		if _, exists := schema.Properties[field]; !exists {
			t.Errorf("Expected field %s to be present in schema", field)
		}
	}

	// Verify field schemas
	if pageSchema := schema.Properties["page"]; pageSchema != nil {
		if pageSchema.Type != "integer" {
			t.Errorf("Expected page type=integer, got %s", pageSchema.Type)
		}
		if pageSchema.Description != "Page number" {
			t.Errorf("Expected page description to be preserved")
		}
	}

	if limitSchema := schema.Properties["limit"]; limitSchema != nil {
		if limitSchema.Type != "integer" {
			t.Errorf("Expected limit type=integer, got %s", limitSchema.Type)
		}
	}

	if planSchema := schema.Properties["plan"]; planSchema != nil {
		if planSchema.Type != "string" {
			t.Errorf("Expected plan type=string, got %s", planSchema.Type)
		}
	}

	// page and limit should be required (no omitempty), plan should not be (has omitempty)
	requiredCount := len(schema.Required)
	if requiredCount != 2 {
		t.Errorf("Expected 2 required fields, got %d: %v", requiredCount, schema.Required)
	}
}

func TestNamedEmbeddedStruct(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	// Test named embedded struct - should NOT be flattened
	schema, err := gen.GenerateSchema(NestedRequest{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	if schema.Type != "object" {
		t.Errorf("Expected type=object, got %s", schema.Type)
	}

	// Should have fields: pagination, name (not flattened)
	if _, exists := schema.Properties["pagination"]; !exists {
		t.Errorf("Expected field 'pagination' to be present")
	}

	if _, exists := schema.Properties["name"]; !exists {
		t.Errorf("Expected field 'name' to be present")
	}

	// Should NOT have direct page/limit fields
	if _, exists := schema.Properties["page"]; exists {
		t.Errorf("Field 'page' should not be flattened when struct is not anonymous")
	}
}

type BaseModel struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

type ComplexEmbedded struct {
	BaseModel
	PaginationParams
	Status string `json:"status"`
}

func TestMultipleEmbeddedStructs(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	// Test multiple embedded structs - all should be flattened
	schema, err := gen.GenerateSchema(ComplexEmbedded{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	// Should have all fields flattened: id, created_at, page, limit, status
	expectedFields := []string{"id", "created_at", "page", "limit", "status"}
	for _, field := range expectedFields {
		if _, exists := schema.Properties[field]; !exists {
			t.Errorf("Expected field %s to be present in schema", field)
		}
	}

	// Verify we have exactly these fields (no duplicates or missing)
	if len(schema.Properties) != len(expectedFields) {
		t.Errorf("Expected %d properties, got %d", len(expectedFields), len(schema.Properties))
	}
}

type EmbeddedWithPointer struct {
	*PaginationParams
	Query string `json:"query"`
}

func TestEmbeddedPointerStruct(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	// Test pointer to embedded struct - should still be flattened
	schema, err := gen.GenerateSchema(EmbeddedWithPointer{})
	if err != nil {
		t.Fatalf("GenerateSchema failed: %v", err)
	}

	// Should have flattened fields even with pointer
	expectedFields := []string{"page", "limit", "query"}
	for _, field := range expectedFields {
		if _, exists := schema.Properties[field]; !exists {
			t.Errorf("Expected field %s to be present in schema", field)
		}
	}
}

