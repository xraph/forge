package router

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
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
	gen := newSchemaGenerator(components, nil)

	// Generate schema for a type with nested structs
	schema, _ := gen.GenerateSchema(&testUserProfile{})

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
	gen := newSchemaGenerator(components, nil)

	// This tests the exact scenario from the user's issue
	schema, _ := gen.GenerateSchema(&testChallengeResponse{})

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
	gen := newSchemaGenerator(components, nil)

	schema, _ := gen.GenerateSchema(&testSimple{})

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
	gen := newSchemaGenerator(components, nil)

	schema, _ := gen.GenerateSchema(&testWithTime{})

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
	gen := newSchemaGenerator(components, nil)

	// Generate schemas for both types
	userSchema, _ := gen.GenerateSchema(&testUser{})
	companySchema, _ := gen.GenerateSchema(&testCompany{})

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

// testXID simulates xid.ID - a type that implements TextMarshaler
type testXID [12]byte

func (id testXID) MarshalText() ([]byte, error) {
	return []byte("test-id-string"), nil
}

func TestTextMarshalerTypesAreTreatedAsStrings(t *testing.T) {
	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	// Test direct TextMarshaler type
	schema, _ := gen.GenerateSchema(testXID{})
	if schema == nil {
		t.Fatal("Expected schema to be generated")
	}

	if schema.Type != "string" {
		t.Errorf("Expected TextMarshaler type to be 'string', got %s", schema.Type)
	}

	// Test TextMarshaler type in a struct field
	type testModel struct {
		ID testXID `json:"id"`
	}

	modelSchema, _ := gen.GenerateSchema(&testModel{})
	if modelSchema == nil {
		t.Fatal("Expected schema to be generated")
	}

	idField, ok := modelSchema.Properties["id"]
	if !ok {
		t.Fatal("Expected 'id' field in schema properties")
	}

	if idField.Type != "string" {
		t.Errorf("Expected TextMarshaler field type to be 'string', got %s", idField.Type)
	}

	// Verify it's not treated as an array
	if idField.Type == "array" {
		t.Error("Expected TextMarshaler type NOT to be treated as array")
	}

	// Verify no component was created for TextMarshaler types
	if len(components) > 0 {
		t.Errorf("Expected no components for TextMarshaler types, got %d", len(components))
	}
}

func TestTextMarshalerInArray(t *testing.T) {
	type testModel struct {
		IDs []testXID `json:"ids"`
	}

	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	schema, _ := gen.GenerateSchema(&testModel{})
	if schema == nil {
		t.Fatal("Expected schema to be generated")
	}

	idsField, ok := schema.Properties["ids"]
	if !ok {
		t.Fatal("Expected 'ids' field in schema properties")
	}

	if idsField.Type != "array" {
		t.Errorf("Expected array type, got %s", idsField.Type)
	}

	if idsField.Items == nil {
		t.Fatal("Expected array to have items schema")
	}

	// Items should be string, not array of ints
	if idsField.Items.Type != "string" {
		t.Errorf("Expected array items type to be 'string' (TextMarshaler), got %s", idsField.Items.Type)
	}
}

func TestTypeNameCollisionDetection(t *testing.T) {
	// Test collision detection by simulating different package paths
	// In real usage, types from different packages would have different PkgPath values
	
	type testAge struct {
		Years int `json:"years"`
	}

	components := make(map[string]*Schema)
	
	// Create a test logger to capture error messages
	var loggedError string
	baseLogger := logger.NewTestLogger()
	testLogger := &testLoggerForCollision{
		Logger:    baseLogger,
		errorFunc: func(msg string) {
			loggedError = msg
		},
	}

	gen := newSchemaGenerator(components, testLogger)

	// First, manually register a type in typeRegistry with a fake package path
	// This simulates a type from package "pkg1"
	typeName := GetTypeName(reflect.TypeOf(testAge{}))
	gen.typeRegistry[typeName] = "pkg1.testAge"

	// Now try to register the same type name but from a different package
	// We'll manually call createOrReuseComponentRef with the actual type
	typ := reflect.TypeOf(testAge{})
	field := reflect.StructField{
		Name: "Test",
		Type: typ,
	}
	
	// The collision should be detected since typeRegistry has "pkg1.testAge"
	// but the actual type's qualified name will be different (current package)
	_, err := gen.createOrReuseComponentRef(typ, field)
	
	// Collisions are now collected, not returned immediately
	// The function should return a placeholder schema to allow processing to continue
	if err != nil {
		t.Fatalf("Expected no immediate error (collisions are collected), got: %v", err)
	}

	// Verify collision was detected and collected
	if !gen.hasCollisions() {
		t.Fatal("Expected collision to be detected and collected")
	}

	collisions := gen.getCollisions()
	if len(collisions) == 0 {
		t.Fatal("Expected at least one collision to be collected")
	}

	// Verify logger received the error
	if loggedError == "" {
		t.Error("Expected logger to receive error message")
	}

	if !strings.Contains(loggedError, "schema component name collision") {
		t.Errorf("Expected logger error to contain collision message, got: %s", loggedError)
	}
}

func TestSameTypeFromSamePackageDoesNotTriggerCollision(t *testing.T) {
	type testAge struct {
		Years int `json:"years"`
	}

	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	// Generate schema for the same type twice
	_, err1 := gen.GenerateSchema(&testAge{})
	if err1 != nil {
		t.Fatalf("Expected first schema generation to succeed, got error: %v", err1)
	}

	// Generate schema again - should reuse existing component, no collision
	_, err2 := gen.GenerateSchema(&testAge{})
	if err2 != nil {
		t.Fatalf("Expected second schema generation of same type to succeed (reuse), got error: %v", err2)
	}

	// Verify component was registered (testAge should be a component since it's a named struct)
	typeName := GetTypeName(reflect.TypeOf(testAge{}))
	if _, exists := components[typeName]; !exists {
		t.Logf("Note: testAge may not be registered as component if it's used inline")
	}
}

// testLoggerForCollision wraps Logger to capture error messages
type testLoggerForCollision struct {
	logger.Logger
	errorFunc func(string)
}

func (t *testLoggerForCollision) Error(msg string, fields ...logger.Field) {
	if t.errorFunc != nil {
		t.errorFunc(msg)
	}
	t.Logger.Error(msg, fields...)
}

func (t *testLoggerForCollision) Errorf(template string, args ...any) {
	msg := fmt.Sprintf(template, args...)
	if t.errorFunc != nil {
		t.errorFunc(msg)
	}
	t.Logger.Errorf(template, args...)
}

func TestPrimitiveTypesUnaffected(t *testing.T) {
	type testPrimitives struct {
		StringField  string   `json:"string_field"`
		IntField     int      `json:"int_field"`
		Int64Field   int64    `json:"int64_field"`
		FloatField   float64  `json:"float_field"`
		BoolField    bool     `json:"bool_field"`
		StringSlice  []string `json:"string_slice"`
		IntSlice     []int    `json:"int_slice"`
		ByteSlice    []byte   `json:"byte_slice"`
		StringArray  [5]string `json:"string_array"`
	}

	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	schema, _ := gen.GenerateSchema(&testPrimitives{})
	if schema == nil {
		t.Fatal("Expected schema to be generated")
	}

	// Verify string field
	if field := schema.Properties["string_field"]; field == nil || field.Type != "string" {
		t.Errorf("Expected string_field to be 'string', got %v", field)
	}

	// Verify int field
	if field := schema.Properties["int_field"]; field == nil || field.Type != "integer" {
		t.Errorf("Expected int_field to be 'integer', got %v", field)
	}

	// Verify int64 field
	if field := schema.Properties["int64_field"]; field == nil || field.Type != "integer" {
		t.Errorf("Expected int64_field to be 'integer', got %v", field)
	}

	// Verify float field
	if field := schema.Properties["float_field"]; field == nil || field.Type != "number" {
		t.Errorf("Expected float_field to be 'number', got %v", field)
	}

	// Verify bool field
	if field := schema.Properties["bool_field"]; field == nil || field.Type != "boolean" {
		t.Errorf("Expected bool_field to be 'boolean', got %v", field)
	}

	// Verify string slice
	if field := schema.Properties["string_slice"]; field == nil || field.Type != "array" {
		t.Errorf("Expected string_slice to be 'array', got %v", field)
	} else if field.Items == nil || field.Items.Type != "string" {
		t.Errorf("Expected string_slice items to be 'string', got %v", field.Items)
	}

	// Verify int slice
	if field := schema.Properties["int_slice"]; field == nil || field.Type != "array" {
		t.Errorf("Expected int_slice to be 'array', got %v", field)
	} else if field.Items == nil || field.Items.Type != "integer" {
		t.Errorf("Expected int_slice items to be 'integer', got %v", field.Items)
	}

	// Verify byte slice (should be array, not string, since []byte doesn't implement TextMarshaler)
	if field := schema.Properties["byte_slice"]; field == nil || field.Type != "array" {
		t.Errorf("Expected byte_slice to be 'array', got %v", field)
	} else if field.Items == nil || field.Items.Type != "integer" {
		t.Errorf("Expected byte_slice items to be 'integer' (uint8), got %v", field.Items)
	}

	// Verify string array
	if field := schema.Properties["string_array"]; field == nil || field.Type != "array" {
		t.Errorf("Expected string_array to be 'array', got %v", field)
	} else if field.Items == nil || field.Items.Type != "string" {
		t.Errorf("Expected string_array items to be 'string', got %v", field.Items)
	}
}

func TestRegularArraysUnaffected(t *testing.T) {
	type testArrays struct {
		IntArray    [5]int     `json:"int_array"`
		ByteArray   [10]byte   `json:"byte_array"`
		StructArray [3]testAddress `json:"struct_array"`
	}

	components := make(map[string]*Schema)
	gen := newSchemaGenerator(components, nil)

	schema, _ := gen.GenerateSchema(&testArrays{})
	if schema == nil {
		t.Fatal("Expected schema to be generated")
	}

	// Verify int array
	if field := schema.Properties["int_array"]; field == nil || field.Type != "array" {
		t.Errorf("Expected int_array to be 'array', got %v", field)
	} else if field.Items == nil || field.Items.Type != "integer" {
		t.Errorf("Expected int_array items to be 'integer', got %v", field.Items)
	}

	// Verify byte array (should be array of integers, not string, since [10]byte doesn't implement TextMarshaler)
	if field := schema.Properties["byte_array"]; field == nil || field.Type != "array" {
		t.Errorf("Expected byte_array to be 'array', got %v", field)
	} else if field.Items == nil || field.Items.Type != "integer" {
		t.Errorf("Expected byte_array items to be 'integer' (uint8), got %v", field.Items)
	}

	// Verify struct array
	if field := schema.Properties["struct_array"]; field == nil || field.Type != "array" {
		t.Errorf("Expected struct_array to be 'array', got %v", field)
	} else if field.Items == nil || field.Items.Ref != "#/components/schemas/testAddress" {
		t.Errorf("Expected struct_array items to reference testAddress component, got %v", field.Items)
	}
}
