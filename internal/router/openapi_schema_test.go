package router

import (
	"reflect"
	"testing"
	"time"
)

// Test types for nested struct schema generation.
type testAddress struct {
	Street  string `description:"Street address" json:"street"`
	City    string `description:"City name"      json:"city"`
	ZipCode string `description:"Postal code"    json:"zipCode"`
}

type testMetadata struct {
	CreatedAt time.Time         `description:"Creation timestamp" json:"created_at"`
	UpdatedAt time.Time         `description:"Update timestamp"   json:"updated_at"`
	Tags      []string          `description:"Tags"               json:"tags,omitempty"`
	Custom    map[string]string `description:"Custom fields"      json:"custom,omitempty"`
}

type testAuthFactor struct {
	FactorID int          `description:"Factor ID"   json:"factor_id"`
	Type     string       `description:"Factor type" json:"type"`
	Name     string       `description:"Factor name" json:"name"`
	Metadata testMetadata `description:"Metadata"    json:"metadata,omitempty"`
}

type testUserProfile struct {
	UserID         string           `description:"User ID"         json:"user_id"`
	Name           string           `description:"User name"       json:"name"`
	Address        testAddress      `description:"Primary address" json:"address"`
	BillingAddress *testAddress     `description:"Billing address" json:"billing_address,omitempty"`
	Factors        []testAuthFactor `description:"Auth factors"    json:"factors,omitempty"`
}

type testChallengeResponse struct {
	ChallengeID      int              `description:"Challenge ID"      json:"challenge_id"`
	SessionID        string           `description:"Session ID"        json:"session_id"`
	AvailableFactors []testAuthFactor `description:"Available factors" json:"available_factors"`
	ExpiresAt        time.Time        `description:"Expiration time"   json:"expires_at"`
}

func TestNestedStructComponentGeneration(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components)

	// Generate schema for a type with nested structs
	schema := gen.GenerateSchema(&testUserProfile{})

	// Verify main schema is generated
	if schema == nil {
		t.Fatal("Expected schema to be generated")
	}

	if schema.Type != "object" {
		t.Errorf("Expected schema type 'object', got %s", schema.Type)
	}

	// Verify the address field is a component reference
	addressField, ok := schema.Properties["address"]
	if !ok {
		t.Fatal("Expected 'address' field in schema properties")
	}

	if addressField.Ref != "#/components/schemas/testAddress" {
		t.Errorf("Expected address to be a component ref, got Ref=%s, Type=%s", addressField.Ref, addressField.Type)
	}

	// Verify billing_address (pointer type) is also a component reference
	billingField, ok := schema.Properties["billing_address"]
	if !ok {
		t.Fatal("Expected 'billing_address' field in schema properties")
	}

	if billingField.Ref != "#/components/schemas/testAddress" {
		t.Errorf("Expected billing_address to be a component ref, got Ref=%s", billingField.Ref)
	}

	// Verify factors (slice of structs) has proper array schema with component ref
	factorsField, ok := schema.Properties["factors"]
	if !ok {
		t.Fatal("Expected 'factors' field in schema properties")
	}

	if factorsField.Type != "array" {
		t.Errorf("Expected factors type 'array', got %s", factorsField.Type)
	}

	if factorsField.Items == nil {
		t.Fatal("Expected factors to have items schema")
	}

	if factorsField.Items.Ref != "#/components/schemas/testAuthFactor" {
		t.Errorf("Expected factors items to be a component ref, got Ref=%s", factorsField.Items.Ref)
	}

	// Verify components were registered
	if _, ok := components["testAddress"]; !ok {
		t.Error("Expected testAddress to be registered in components")
	}

	if _, ok := components["testAuthFactor"]; !ok {
		t.Error("Expected testAuthFactor to be registered in components")
	}

	// Verify nested component (Metadata inside AuthFactor) is also registered
	if _, ok := components["testMetadata"]; !ok {
		t.Error("Expected testMetadata to be registered in components")
	}
}

func TestChallengeResponseNestedStructs(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components)

	// This tests the exact scenario from the user's issue
	schema := gen.GenerateSchema(&testChallengeResponse{})

	if schema == nil {
		t.Fatal("Expected schema to be generated")
	}

	// Verify available_factors is an array with component ref
	availableFactorsField, ok := schema.Properties["available_factors"]
	if !ok {
		t.Fatal("Expected 'available_factors' field in schema properties")
	}

	if availableFactorsField.Type != "array" {
		t.Errorf("Expected available_factors type 'array', got %s", availableFactorsField.Type)
	}

	if availableFactorsField.Items == nil {
		t.Fatal("Expected available_factors to have items schema")
	}

	if availableFactorsField.Items.Ref != "#/components/schemas/testAuthFactor" {
		t.Errorf("Expected available_factors items to be a component ref to testAuthFactor, got Ref=%s",
			availableFactorsField.Items.Ref)
	}

	// Verify the AuthFactor component exists and has proper structure
	authFactorSchema, ok := components["testAuthFactor"]
	if !ok {
		t.Fatal("Expected testAuthFactor to be registered in components")
	}

	if authFactorSchema.Type != "object" {
		t.Errorf("Expected testAuthFactor type 'object', got %s", authFactorSchema.Type)
	}

	// Verify factor_id field exists
	if _, ok := authFactorSchema.Properties["factor_id"]; !ok {
		t.Error("Expected 'factor_id' field in testAuthFactor schema")
	}

	// Verify type field exists
	if _, ok := authFactorSchema.Properties["type"]; !ok {
		t.Error("Expected 'type' field in testAuthFactor schema")
	}

	// Verify metadata is also a component ref (deeply nested)
	metadataField, ok := authFactorSchema.Properties["metadata"]
	if !ok {
		t.Error("Expected 'metadata' field in testAuthFactor schema")
	}

	if metadataField.Ref != "#/components/schemas/testMetadata" {
		t.Errorf("Expected metadata to be a component ref, got Ref=%s", metadataField.Ref)
	}
}

func TestPrimitiveTypesNotCreatedAsComponents(t *testing.T) {
	type testSimple struct {
		Name  string   `json:"name"`
		Age   int      `json:"age"`
		Tags  []string `json:"tags"`
		Admin bool     `json:"admin"`
	}

	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components)

	schema := gen.GenerateSchema(&testSimple{})

	if schema == nil {
		t.Fatal("Expected schema to be generated")
	}

	// Verify primitive fields are inline, not refs
	nameField, ok := schema.Properties["name"]
	if !ok {
		t.Fatal("Expected 'name' field in schema properties")
	}

	if nameField.Ref != "" {
		t.Error("Expected primitive field 'name' to be inline, not a ref")
	}

	if nameField.Type != "string" {
		t.Errorf("Expected name type 'string', got %s", nameField.Type)
	}

	// Verify array of primitives is inline
	tagsField, ok := schema.Properties["tags"]
	if !ok {
		t.Fatal("Expected 'tags' field in schema properties")
	}

	if tagsField.Ref != "" {
		t.Error("Expected array field 'tags' to be inline, not a ref")
	}

	if tagsField.Type != "array" {
		t.Errorf("Expected tags type 'array', got %s", tagsField.Type)
	}

	if tagsField.Items.Type != "string" {
		t.Errorf("Expected tags items type 'string', got %s", tagsField.Items.Type)
	}

	// Verify no primitive types were added to components
	if len(components) > 0 {
		t.Errorf("Expected no components to be registered for primitive types, got %d", len(components))
	}
}

func TestTimeTypeNotCreatedAsComponent(t *testing.T) {
	type testWithTime struct {
		CreatedAt time.Time  `json:"created_at"`
		UpdatedAt *time.Time `json:"updated_at,omitempty"`
	}

	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components)

	schema := gen.GenerateSchema(&testWithTime{})

	if schema == nil {
		t.Fatal("Expected schema to be generated")
	}

	// Verify time.Time fields are inline string with date-time format
	createdAtField, ok := schema.Properties["created_at"]
	if !ok {
		t.Fatal("Expected 'created_at' field in schema properties")
	}

	if createdAtField.Ref != "" {
		t.Error("Expected time.Time field to be inline, not a ref")
	}

	if createdAtField.Type != "string" {
		t.Errorf("Expected created_at type 'string', got %s", createdAtField.Type)
	}

	if createdAtField.Format != "date-time" {
		t.Errorf("Expected created_at format 'date-time', got %s", createdAtField.Format)
	}

	// Verify no Time component was created
	if _, ok := components["Time"]; ok {
		t.Error("Expected time.Time NOT to be registered as a component")
	}
}

func TestComponentReuseAcrossMultipleSchemas(t *testing.T) {
	type testUser struct {
		ID      string      `json:"id"`
		Address testAddress `json:"address"`
	}

	type testCompany struct {
		Name    string      `json:"name"`
		Address testAddress `json:"address"`
	}

	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components)

	// Generate schemas for both types
	userSchema := gen.GenerateSchema(&testUser{})
	companySchema := gen.GenerateSchema(&testCompany{})

	if userSchema == nil || companySchema == nil {
		t.Fatal("Expected both schemas to be generated")
	}

	// Verify both use the same component reference
	userAddressField := userSchema.Properties["address"]
	companyAddressField := companySchema.Properties["address"]

	if userAddressField.Ref != companyAddressField.Ref {
		t.Error("Expected both schemas to reference the same Address component")
	}

	if userAddressField.Ref != "#/components/schemas/testAddress" {
		t.Errorf("Expected component ref to testAddress, got %s", userAddressField.Ref)
	}

	// Verify only one Address component was registered
	if len(components) != 1 {
		t.Errorf("Expected exactly 1 component (testAddress), got %d", len(components))
	}

	if _, ok := components["testAddress"]; !ok {
		t.Error("Expected testAddress to be registered")
	}
}

func TestGetTypeName(t *testing.T) {
	tests := []struct {
		name     string
		input    reflect.Type
		expected string
	}{
		{
			name:     "named struct",
			input:    reflect.TypeOf(testAddress{}),
			expected: "testAddress",
		},
		{
			name:     "pointer to named struct",
			input:    reflect.TypeOf(&testAddress{}),
			expected: "testAddress",
		},
		{
			name:     "primitive type",
			input:    reflect.TypeOf("string"),
			expected: "string",
		},
		{
			name:     "anonymous struct",
			input:    reflect.TypeOf(struct{ Name string }{}),
			expected: "Object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetTypeName(tt.input)
			if result != tt.expected {
				t.Errorf("GetTypeName() = %s, expected %s", result, tt.expected)
			}
		})
	}
}
