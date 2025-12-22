package router

import (
	"encoding/json"
	"testing"
)

// Test that query parameters with enum types generate component references

type TestQueryStatus string

const (
	TestQueryStatusActive   TestQueryStatus = "active"
	TestQueryStatusInactive TestQueryStatus = "inactive"
)

func (TestQueryStatus) EnumValues() []any {
	return []any{"active", "inactive", "pending"}
}

func (s TestQueryStatus) MarshalText() ([]byte, error) {
	return []byte(s), nil
}

type TestQueryParams struct {
	Status TestQueryStatus `query:"status"`
	Name   string          `query:"name"`
}

func TestQueryParamsGenerateEnumComponentRef(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	// Generate query parameters
	params := generateQueryParamsFromStruct(gen, &TestQueryParams{})

	// Debug: Print all params
	for i, p := range params {
		t.Logf("Param %d: name=%s, schema.Type=%s, schema.Ref=%s", i, p.Name, p.Schema.Type, p.Schema.Ref)
	}

	// Debug: Print components
	componentsJSON, _ := json.MarshalIndent(components, "", "  ")
	t.Logf("Components: %s", string(componentsJSON))

	// Find status parameter
	var statusParam *Parameter
	for i := range params {
		if params[i].Name == "status" {
			statusParam = &params[i]
			break
		}
	}

	if statusParam == nil {
		t.Fatal("status parameter not found")
	}

	// VERIFY: Status parameter should reference component
	if statusParam.Schema.Ref != "#/components/schemas/TestQueryStatus" {
		t.Errorf("Expected status to reference TestQueryStatus component")
		t.Errorf("Got: Type=%s, Ref=%s", statusParam.Schema.Type, statusParam.Schema.Ref)
	}

	// VERIFY: Component should exist
	if _, exists := components["TestQueryStatus"]; !exists {
		t.Error("TestQueryStatus component should exist")
	}

	// VERIFY: Component should have enum values
	if comp := components["TestQueryStatus"]; comp != nil {
		if len(comp.Enum) != 3 {
			t.Errorf("Expected 3 enum values, got %d", len(comp.Enum))
		}
		if comp.Type != "string" {
			t.Errorf("Expected component type 'string', got %s", comp.Type)
		}
	}
}
