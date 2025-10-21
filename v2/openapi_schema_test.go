package forge

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestUser struct {
	ID        string                 `json:"id" description:"User ID" example:"usr_123" readOnly:"true"`
	Name      string                 `json:"name" minLength:"1" maxLength:"100" example:"John Doe"`
	Email     string                 `json:"email" format:"email" example:"john@example.com"`
	Age       int                    `json:"age" minimum:"0" maximum:"150" example:"30"`
	IsActive  bool                   `json:"is_active" example:"true"`
	CreatedAt time.Time              `json:"created_at" format:"date-time"`
	Tags      []string               `json:"tags" minItems:"1" maxItems:"10" uniqueItems:"true"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type TestProduct struct {
	ID       string  `json:"id"`
	Name     string  `json:"name" minLength:"1"`
	Price    float64 `json:"price" minimum:"0" exclusiveMinimum:"true"`
	Category string  `json:"category" enum:"electronics,books,clothing"`
	Stock    *int    `json:"stock,omitempty" minimum:"0"`
	Optional *string `json:"optional,omitempty"`
}

func TestSchemaGenerator_New(t *testing.T) {
	gen := newSchemaGenerator()
	assert.NotNil(t, gen)
	assert.NotNil(t, gen.schemas)
}

func TestSchemaGenerator_GenerateSchema_Nil(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(nil)
	assert.Nil(t, schema)
}

func TestSchemaGenerator_StringType(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema("")
	require.NotNil(t, schema)
	assert.Equal(t, "string", schema.Type)
}

func TestSchemaGenerator_IntegerType(t *testing.T) {
	gen := newSchemaGenerator()

	tests := []struct {
		name  string
		value interface{}
	}{
		{"int", int(0)},
		{"int8", int8(0)},
		{"int16", int16(0)},
		{"int32", int32(0)},
		{"int64", int64(0)},
		{"uint", uint(0)},
		{"uint8", uint8(0)},
		{"uint16", uint16(0)},
		{"uint32", uint32(0)},
		{"uint64", uint64(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := gen.GenerateSchema(tt.value)
			require.NotNil(t, schema)
			assert.Equal(t, "integer", schema.Type)
		})
	}
}

func TestSchemaGenerator_NumberType(t *testing.T) {
	gen := newSchemaGenerator()

	tests := []struct {
		name  string
		value interface{}
	}{
		{"float32", float32(0)},
		{"float64", float64(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := gen.GenerateSchema(tt.value)
			require.NotNil(t, schema)
			assert.Equal(t, "number", schema.Type)
		})
	}
}

func TestSchemaGenerator_BooleanType(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(true)
	require.NotNil(t, schema)
	assert.Equal(t, "boolean", schema.Type)
}

func TestSchemaGenerator_ArrayType(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema([]string{})
	require.NotNil(t, schema)
	assert.Equal(t, "array", schema.Type)
	assert.NotNil(t, schema.Items)
	assert.Equal(t, "string", schema.Items.Type)
}

func TestSchemaGenerator_MapType(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(map[string]interface{}{})
	require.NotNil(t, schema)
	assert.Equal(t, "object", schema.Type)
	assert.Equal(t, true, schema.AdditionalProperties)
}

func TestSchemaGenerator_TimeType(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(time.Time{})
	require.NotNil(t, schema)
	assert.Equal(t, "string", schema.Type)
	assert.Equal(t, "date-time", schema.Format)
}

func TestSchemaGenerator_PointerType(t *testing.T) {
	gen := newSchemaGenerator()
	str := "test"
	schema := gen.GenerateSchema(&str)
	require.NotNil(t, schema)
	assert.Equal(t, "string", schema.Type)
}

func TestSchemaGenerator_StructType(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(TestUser{})
	require.NotNil(t, schema)
	assert.Equal(t, "object", schema.Type)
	assert.NotEmpty(t, schema.Properties)
	assert.NotEmpty(t, schema.Required)
}

func TestSchemaGenerator_StructFields(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(TestUser{})
	require.NotNil(t, schema)

	// Check ID field with readOnly
	idSchema := schema.Properties["id"]
	require.NotNil(t, idSchema)
	assert.Equal(t, "string", idSchema.Type)
	assert.Equal(t, "User ID", idSchema.Description)
	assert.Equal(t, "usr_123", idSchema.Example)
	assert.True(t, idSchema.ReadOnly)

	// Check name field with length constraints
	nameSchema := schema.Properties["name"]
	require.NotNil(t, nameSchema)
	assert.Equal(t, "string", nameSchema.Type)
	assert.Equal(t, 1, nameSchema.MinLength)
	assert.Equal(t, 100, nameSchema.MaxLength)
	assert.Equal(t, "John Doe", nameSchema.Example)

	// Check email field with format
	emailSchema := schema.Properties["email"]
	require.NotNil(t, emailSchema)
	assert.Equal(t, "string", emailSchema.Type)
	assert.Equal(t, "email", emailSchema.Format)

	// Check age field with numeric constraints
	ageSchema := schema.Properties["age"]
	require.NotNil(t, ageSchema)
	assert.Equal(t, "integer", ageSchema.Type)
	assert.Equal(t, 0.0, ageSchema.Minimum)
	assert.Equal(t, 150.0, ageSchema.Maximum)

	// Check array field with constraints
	tagsSchema := schema.Properties["tags"]
	require.NotNil(t, tagsSchema)
	assert.Equal(t, "array", tagsSchema.Type)
	assert.Equal(t, 1, tagsSchema.MinItems)
	assert.Equal(t, 10, tagsSchema.MaxItems)
	assert.True(t, tagsSchema.UniqueItems)

	// Check time field
	createdSchema := schema.Properties["created_at"]
	require.NotNil(t, createdSchema)
	assert.Equal(t, "string", createdSchema.Type)
	assert.Equal(t, "date-time", createdSchema.Format)

	// Check required fields
	assert.Contains(t, schema.Required, "id")
	assert.Contains(t, schema.Required, "name")
	assert.Contains(t, schema.Required, "email")
	assert.NotContains(t, schema.Required, "metadata") // omitempty
}

func TestSchemaGenerator_EnumField(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(TestProduct{})
	require.NotNil(t, schema)

	categorySchema := schema.Properties["category"]
	require.NotNil(t, categorySchema)
	assert.Equal(t, "string", categorySchema.Type)
	assert.Len(t, categorySchema.Enum, 3)
}

func TestSchemaGenerator_ExclusiveMinimum(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(TestProduct{})
	require.NotNil(t, schema)

	priceSchema := schema.Properties["price"]
	require.NotNil(t, priceSchema)
	assert.Equal(t, "number", priceSchema.Type)
	assert.Equal(t, 0.0, priceSchema.Minimum)
	assert.True(t, priceSchema.ExclusiveMinimum)
}

func TestSchemaGenerator_OptionalFields(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(TestProduct{})
	require.NotNil(t, schema)

	// Pointer fields with omitempty should not be required
	assert.NotContains(t, schema.Required, "stock")
	assert.NotContains(t, schema.Required, "optional")

	// Non-pointer, non-omitempty fields should be required
	assert.Contains(t, schema.Required, "id")
	assert.Contains(t, schema.Required, "name")
}

func TestParseJSONTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		wantName string
		wantOmit bool
	}{
		{"simple", "field_name", "field_name", false},
		{"with omitempty", "field_name,omitempty", "field_name", true},
		{"dash", "-", "-", false},
		{"empty", "", "", false},
		{"multiple options", "field,omitempty,string", "field", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, omit := parseJSONTag(tt.tag)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantOmit, omit)
		})
	}
}

func TestParseExample(t *testing.T) {
	tests := []struct {
		name       string
		example    string
		schemaType string
		want       interface{}
	}{
		{"integer", "42", "integer", 42},
		{"number", "3.14", "number", 3.14},
		{"boolean true", "true", "boolean", true},
		{"boolean false", "false", "boolean", false},
		{"string", "hello", "string", "hello"},
		{"array", "a,b,c", "array", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseExample(tt.example, tt.schemaType)
			assert.Equal(t, tt.want, result)
		})
	}
}

type NestedStruct struct {
	Outer string `json:"outer"`
	Inner struct {
		Field string `json:"field"`
	} `json:"inner"`
}

func TestSchemaGenerator_NestedStruct(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(NestedStruct{})
	require.NotNil(t, schema)
	assert.Equal(t, "object", schema.Type)
	assert.Contains(t, schema.Properties, "outer")
	assert.Contains(t, schema.Properties, "inner")

	innerSchema := schema.Properties["inner"]
	require.NotNil(t, innerSchema)
	assert.Equal(t, "object", innerSchema.Type)
}

type IgnoredFieldsStruct struct {
	Exported   string `json:"exported"`
	unexported string
	Ignored    string `json:"-"`
}

func TestSchemaGenerator_IgnoredFields(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(IgnoredFieldsStruct{})
	require.NotNil(t, schema)

	assert.Contains(t, schema.Properties, "exported")
	assert.NotContains(t, schema.Properties, "unexported")
	assert.NotContains(t, schema.Properties, "Ignored")
}

func TestGetTypeName(t *testing.T) {
	tests := []struct {
		name string
		typ  interface{}
		want string
	}{
		{"basic struct", TestUser{}, "TestUser"},
		{"pointer", &TestUser{}, "TestUser"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ := getReflectType(tt.typ)
			name := GetTypeName(typ)
			assert.Contains(t, name, tt.want)
		})
	}
}

func getReflectType(v interface{}) reflect.Type {
	return reflect.TypeOf(v)
}

type ValidationStruct struct {
	Pattern    string   `json:"pattern" pattern:"^[a-z]+$"`
	MultipleOf float64  `json:"multiple_of" multipleOf:"5"`
	MaxProps   struct{} `json:"max_props" maxProperties:"10"`
	MinProps   struct{} `json:"min_props" minProperties:"1"`
}

func TestSchemaGenerator_ValidationTags(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(ValidationStruct{})
	require.NotNil(t, schema)

	patternSchema := schema.Properties["pattern"]
	require.NotNil(t, patternSchema)
	assert.Equal(t, "^[a-z]+$", patternSchema.Pattern)

	multipleSchema := schema.Properties["multiple_of"]
	require.NotNil(t, multipleSchema)
	assert.Equal(t, 5.0, multipleSchema.MultipleOf)
}

type DeprecatedStruct struct {
	Current    string `json:"current"`
	Deprecated string `json:"deprecated" deprecated:"true"`
}

func TestSchemaGenerator_DeprecatedField(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(DeprecatedStruct{})
	require.NotNil(t, schema)

	deprecatedSchema := schema.Properties["deprecated"]
	require.NotNil(t, deprecatedSchema)
	assert.True(t, deprecatedSchema.Deprecated)

	currentSchema := schema.Properties["current"]
	require.NotNil(t, currentSchema)
	assert.False(t, currentSchema.Deprecated)
}

type NullableStruct struct {
	Required string  `json:"required"`
	Nullable *string `json:"nullable" nullable:"true"`
}

func TestSchemaGenerator_NullableField(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(NullableStruct{})
	require.NotNil(t, schema)

	nullableSchema := schema.Properties["nullable"]
	require.NotNil(t, nullableSchema)
	assert.True(t, nullableSchema.Nullable)
}

type ReadWriteStruct struct {
	ID       string `json:"id" readOnly:"true"`
	Password string `json:"password" writeOnly:"true"`
	Normal   string `json:"normal"`
}

func TestSchemaGenerator_ReadWriteOnly(t *testing.T) {
	gen := newSchemaGenerator()
	schema := gen.GenerateSchema(ReadWriteStruct{})
	require.NotNil(t, schema)

	idSchema := schema.Properties["id"]
	require.NotNil(t, idSchema)
	assert.True(t, idSchema.ReadOnly)
	assert.False(t, idSchema.WriteOnly)

	passwordSchema := schema.Properties["password"]
	require.NotNil(t, passwordSchema)
	assert.False(t, passwordSchema.ReadOnly)
	assert.True(t, passwordSchema.WriteOnly)

	normalSchema := schema.Properties["normal"]
	require.NotNil(t, normalSchema)
	assert.False(t, normalSchema.ReadOnly)
	assert.False(t, normalSchema.WriteOnly)
}
