package router

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Replicate user's exact scenario

// Mock pagination params (embedded struct).
type TestPaginationParams struct {
	Page  int `query:"page"`
	Limit int `query:"limit"`
}

// User's enum types.
type TestConnectorCategory string
type TestConnectorType string
type TestConnectorStatus string

const (
	TestCategoryIdentity    TestConnectorCategory = "identity"
	TestCategoryApplication TestConnectorCategory = "application"
	TestCategoryDataOnly    TestConnectorCategory = "data_only"
)

const (
	TestTypeOAuth2    TestConnectorType = "oauth2"
	TestTypeAPIKey    TestConnectorType = "apikey"
	TestTypeBasicAuth TestConnectorType = "basicauth"
	TestTypeSCIM      TestConnectorType = "scim"
)

const (
	TestStatusActive     TestConnectorStatus = "active"
	TestStatusInactive   TestConnectorStatus = "inactive"
	TestStatusBeta       TestConnectorStatus = "beta"
	TestStatusDeprecated TestConnectorStatus = "deprecated"
)

// Add EnumValues to all types.
func (TestConnectorCategory) EnumValues() []any {
	return []any{"identity", "application", "data_only"}
}

func (TestConnectorType) EnumValues() []any {
	return []any{"oauth2", "apikey", "basicauth", "scim"}
}

func (TestConnectorStatus) EnumValues() []any {
	return []any{"active", "inactive", "beta", "deprecated"}
}

// Add MarshalText to all types.
func (c TestConnectorCategory) MarshalText() ([]byte, error) {
	return []byte(c), nil
}

func (t TestConnectorType) MarshalText() ([]byte, error) {
	return []byte(t), nil
}

func (s TestConnectorStatus) MarshalText() ([]byte, error) {
	return []byte(s), nil
}

// User's request struct (exact replica).
type TestListConnectorsRequest struct {
	TestPaginationParams

	Category TestConnectorCategory `json:"category,omitempty" query:"category"`
	Type     TestConnectorType     `json:"type,omitempty"     query:"type"`
	Status   TestConnectorStatus   `json:"status,omitempty"   query:"status"`
}

func TestUserScenarioQueryEnums(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	// Generate query parameters exactly as user would
	params := generateQueryParamsFromStruct(gen, &TestListConnectorsRequest{})

	t.Logf("Generated %d parameters:", len(params))

	for i, p := range params {
		t.Logf("  [%d] name=%s, in=%s, type=%s, ref=%s",
			i, p.Name, p.In, p.Schema.Type, p.Schema.Ref)
	}

	// Print components
	componentsJSON, err := json.MarshalIndent(components, "", "  ")
	require.NoError(t, err)
	t.Logf("\nComponents:\n%s", string(componentsJSON))

	// Verify category parameter
	var categoryParam *Parameter

	for i := range params {
		if params[i].Name == "category" {
			categoryParam = &params[i]

			break
		}
	}

	if categoryParam == nil {
		t.Fatal("category parameter not found")
	}

	if categoryParam.Schema.Ref != "#/components/schemas/TestConnectorCategory" {
		t.Errorf("FAIL: category should reference component")
		t.Errorf("Got: type=%s, ref=%s", categoryParam.Schema.Type, categoryParam.Schema.Ref)
	} else {
		t.Logf("✓ category correctly references TestConnectorCategory component")
	}

	// Verify type parameter
	var typeParam *Parameter

	for i := range params {
		if params[i].Name == "type" {
			typeParam = &params[i]

			break
		}
	}

	if typeParam == nil {
		t.Fatal("type parameter not found")
	}

	if typeParam.Schema.Ref != "#/components/schemas/TestConnectorType" {
		t.Errorf("FAIL: type should reference component")
		t.Errorf("Got: type=%s, ref=%s", typeParam.Schema.Type, typeParam.Schema.Ref)
	} else {
		t.Logf("✓ type correctly references TestConnectorType component")
	}

	// Verify status parameter
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

	if statusParam.Schema.Ref != "#/components/schemas/TestConnectorStatus" {
		t.Errorf("FAIL: status should reference component")
		t.Errorf("Got: type=%s, ref=%s", statusParam.Schema.Type, statusParam.Schema.Ref)
	} else {
		t.Logf("✓ status correctly references TestConnectorStatus component")
	}

	// Verify all components exist
	expectedComponents := []string{"TestConnectorCategory", "TestConnectorType", "TestConnectorStatus"}
	for _, name := range expectedComponents {
		if _, exists := components[name]; !exists {
			t.Errorf("Component %s should exist", name)
		} else {
			t.Logf("✓ Component %s exists", name)
		}
	}
}
