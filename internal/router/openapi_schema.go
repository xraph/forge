package router

import (
	"reflect"
	"strconv"
	"strings"
	"time"
)

// schemaGenerator generates JSON schemas from Go types
type schemaGenerator struct {
	schemas    map[string]*Schema
	components map[string]*Schema // Reference to spec components for registering nested types
}

// newSchemaGenerator creates a new schema generator
func newSchemaGenerator(components map[string]*Schema) *schemaGenerator {
	return &schemaGenerator{
		schemas:    make(map[string]*Schema),
		components: components,
	}
}

// GenerateSchema generates a JSON schema from a Go type
func (g *schemaGenerator) GenerateSchema(t interface{}) *Schema {
	if t == nil {
		return nil
	}

	typ := reflect.TypeOf(t)

	// Handle pointer types
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	return g.generateSchemaFromType(typ)
}

func (g *schemaGenerator) generateSchemaFromType(typ reflect.Type) *Schema {
	schema := &Schema{}

	// Handle time.Time specially before switch
	if typ == reflect.TypeOf(time.Time{}) {
		schema.Type = "string"
		schema.Format = "date-time"
		return schema
	}

	switch typ.Kind() {
	case reflect.String:
		schema.Type = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema.Type = "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = "integer"
	case reflect.Float32, reflect.Float64:
		schema.Type = "number"
	case reflect.Bool:
		schema.Type = "boolean"
	case reflect.Struct:
		return g.generateStructSchema(typ)
	case reflect.Slice, reflect.Array:
		schema.Type = "array"
		schema.Items = g.generateSchemaFromType(typ.Elem())
	case reflect.Map:
		schema.Type = "object"
		schema.AdditionalProperties = true
	case reflect.Ptr:
		return g.generateSchemaFromType(typ.Elem())
	case reflect.Interface:
		// Generic interface - allow any type
		return &Schema{}
	default:
		schema.Type = "object"
	}

	return schema
}

func (g *schemaGenerator) generateStructSchema(typ reflect.Type) *Schema {
	schema := &Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
	}

	var required []string

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get JSON tag
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue // Skip fields with json:"-"
		}

		// Parse JSON tag
		jsonName, omitempty := parseJSONTag(jsonTag)
		if jsonName == "" {
			jsonName = field.Name
		}

		// Generate field schema
		fieldSchema := g.generateFieldSchema(field)
		schema.Properties[jsonName] = fieldSchema

		// Determine if field is required
		// Check for required tag first, then fall back to omitempty logic
		if requiredTag := field.Tag.Get("required"); requiredTag == "true" {
			required = append(required, jsonName)
		} else if !omitempty && field.Type.Kind() != reflect.Ptr {
			required = append(required, jsonName)
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema
}

func (g *schemaGenerator) generateFieldSchema(field reflect.StructField) *Schema {
	fieldType := field.Type
	
	// Handle pointer types
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}
	
	// Check if this is a named struct type that should be a component reference
	if g.shouldBeComponentRef(fieldType) {
		return g.createOrReuseComponentRef(fieldType, field)
	}
	
	// Handle slices/arrays of named structs
	if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
		elemType := fieldType.Elem()
		if elemType.Kind() == reflect.Ptr {
			elemType = elemType.Elem()
		}
		
		if g.shouldBeComponentRef(elemType) {
			return g.createArrayWithComponentRef(elemType, field)
		}
	}
	
	// Default behavior for primitives and inline types
	schema := g.generateSchemaFromType(field.Type)
	g.applyStructTags(schema, field)
	
	return schema
}

// shouldBeComponentRef determines if a type should be extracted as a component
func (g *schemaGenerator) shouldBeComponentRef(typ reflect.Type) bool {
	// Only named struct types (not time.Time or anonymous structs)
	return typ.Kind() == reflect.Struct && 
		typ.Name() != "" && 
		typ != reflect.TypeOf(time.Time{})
}

// createOrReuseComponentRef creates or reuses a component reference for a struct type
func (g *schemaGenerator) createOrReuseComponentRef(typ reflect.Type, field reflect.StructField) *Schema {
	typeName := GetTypeName(typ)
	
	// Register the component if not already registered
	if _, exists := g.schemas[typeName]; !exists {
		componentSchema := g.generateSchemaFromType(typ)
		g.schemas[typeName] = componentSchema
		
		// Add to spec components if available
		if g.components != nil {
			g.components[typeName] = componentSchema
			// Debug: Uncomment to trace component registration
			// fmt.Printf("[DEBUG] Registered component: %s (total: %d)\n", typeName, len(g.components))
		}
	}
	
	// Return a reference schema
	refSchema := &Schema{
		Ref: "#/components/schemas/" + typeName,
	}
	
	// Apply struct tags to the reference (for description, etc.)
	// Note: Some tags like description should be on the reference, not the component
	if desc := field.Tag.Get("description"); desc != "" {
		refSchema.Description = desc
	}
	if title := field.Tag.Get("title"); title != "" {
		refSchema.Title = title
	}
	
	return refSchema
}

// createArrayWithComponentRef creates an array schema with component reference for elements
func (g *schemaGenerator) createArrayWithComponentRef(elemType reflect.Type, field reflect.StructField) *Schema {
	typeName := GetTypeName(elemType)
	
	// Register the element type as a component if not already registered
	if _, exists := g.schemas[typeName]; !exists {
		componentSchema := g.generateSchemaFromType(elemType)
		g.schemas[typeName] = componentSchema
		
		// Add to spec components if available
		if g.components != nil {
			g.components[typeName] = componentSchema
		}
	}
	
	// Return array schema with ref to component
	arraySchema := &Schema{
		Type: "array",
		Items: &Schema{
			Ref: "#/components/schemas/" + typeName,
		},
	}
	
	// Apply struct tags to the array
	g.applyStructTags(arraySchema, field)
	
	return arraySchema
}

func (g *schemaGenerator) applyStructTags(schema *Schema, field reflect.StructField) {
	// Description
	if desc := field.Tag.Get("description"); desc != "" {
		schema.Description = desc
	}

	// Title
	if title := field.Tag.Get("title"); title != "" {
		schema.Title = title
	}

	// Example
	if example := field.Tag.Get("example"); example != "" {
		schema.Example = parseExample(example, schema.Type)
	}

	// Default value
	if defaultVal := field.Tag.Get("default"); defaultVal != "" {
		schema.Default = parseExample(defaultVal, schema.Type)
	}

	// Format (including binary for file uploads)
	if format := field.Tag.Get("format"); format != "" {
		schema.Format = format
		// Special handling for binary format (file uploads)
		if format == "binary" || format == "byte" {
			schema.Type = "string"
		}
	}

	// Const value (for discriminator types)
	if constVal := field.Tag.Get("const"); constVal != "" {
		schema.Enum = []interface{}{constVal}
	}

	// Pattern
	if pattern := field.Tag.Get("pattern"); pattern != "" {
		schema.Pattern = pattern
	}

	// Enum
	if enum := field.Tag.Get("enum"); enum != "" {
		enumValues := strings.Split(enum, ",")
		schema.Enum = make([]interface{}, len(enumValues))
		for i, v := range enumValues {
			schema.Enum[i] = strings.TrimSpace(v)
		}
	}

	// String validation
	if minLength := field.Tag.Get("minLength"); minLength != "" {
		if val, err := strconv.Atoi(minLength); err == nil {
			schema.MinLength = val
		}
	}
	if maxLength := field.Tag.Get("maxLength"); maxLength != "" {
		if val, err := strconv.Atoi(maxLength); err == nil {
			schema.MaxLength = val
		}
	}

	// Number validation
	if minimum := field.Tag.Get("minimum"); minimum != "" {
		if val, err := strconv.ParseFloat(minimum, 64); err == nil {
			schema.Minimum = val
		}
	}
	if maximum := field.Tag.Get("maximum"); maximum != "" {
		if val, err := strconv.ParseFloat(maximum, 64); err == nil {
			schema.Maximum = val
		}
	}
	if multipleOf := field.Tag.Get("multipleOf"); multipleOf != "" {
		if val, err := strconv.ParseFloat(multipleOf, 64); err == nil {
			schema.MultipleOf = val
		}
	}

	// Exclusive minimum/maximum
	if exclusiveMin := field.Tag.Get("exclusiveMinimum"); exclusiveMin == "true" {
		schema.ExclusiveMinimum = true
	}
	if exclusiveMax := field.Tag.Get("exclusiveMaximum"); exclusiveMax == "true" {
		schema.ExclusiveMaximum = true
	}

	// Array validation
	if minItems := field.Tag.Get("minItems"); minItems != "" {
		if val, err := strconv.Atoi(minItems); err == nil {
			schema.MinItems = val
		}
	}
	if maxItems := field.Tag.Get("maxItems"); maxItems != "" {
		if val, err := strconv.Atoi(maxItems); err == nil {
			schema.MaxItems = val
		}
	}
	if uniqueItems := field.Tag.Get("uniqueItems"); uniqueItems == "true" {
		schema.UniqueItems = true
	}

	// Object validation
	if minProps := field.Tag.Get("minProperties"); minProps != "" {
		if val, err := strconv.Atoi(minProps); err == nil {
			schema.MinProperties = val
		}
	}
	if maxProps := field.Tag.Get("maxProperties"); maxProps != "" {
		if val, err := strconv.Atoi(maxProps); err == nil {
			schema.MaxProperties = val
		}
	}

	// Nullable, ReadOnly, WriteOnly
	if nullable := field.Tag.Get("nullable"); nullable == "true" {
		schema.Nullable = true
	}
	if readOnly := field.Tag.Get("readOnly"); readOnly == "true" {
		schema.ReadOnly = true
	}
	if writeOnly := field.Tag.Get("writeOnly"); writeOnly == "true" {
		schema.WriteOnly = true
	}
	if deprecated := field.Tag.Get("deprecated"); deprecated == "true" {
		schema.Deprecated = true
	}
}

// parseJSONTag parses a JSON struct tag
func parseJSONTag(tag string) (name string, omitempty bool) {
	if tag == "" {
		return "", false
	}

	parts := strings.Split(tag, ",")
	name = parts[0]

	for i := 1; i < len(parts); i++ {
		if parts[i] == "omitempty" {
			omitempty = true
			break
		}
	}

	return name, omitempty
}

// parseExample converts string example to appropriate type
func parseExample(example, schemaType string) interface{} {
	switch schemaType {
	case "integer":
		if val, err := strconv.Atoi(example); err == nil {
			return val
		}
	case "number":
		if val, err := strconv.ParseFloat(example, 64); err == nil {
			return val
		}
	case "boolean":
		if val, err := strconv.ParseBool(example); err == nil {
			return val
		}
	case "array":
		// Split by comma for array examples
		return strings.Split(example, ",")
	}

	return example
}

// GetTypeName returns a qualified type name for schema references
func GetTypeName(t reflect.Type) string {
	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// For named types, use just the type name (without package prefix)
	// This makes OpenAPI component names cleaner (e.g., "User" instead of "main.User")
	if t.Name() != "" {
		return t.Name()
	}

	// Fallback for anonymous types
	return "Object"
}
