package router

import (
	"encoding"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// schemaGenerator generates JSON schemas from Go types.
type schemaGenerator struct {
	schemas         map[string]*Schema
	components      map[string]*Schema // Reference to spec components for registering nested types
	logger          Logger             // Optional logger for collision warnings
	typeRegistry    map[string]string  // Maps component name -> full qualified type name for collision detection
	reverseRegistry map[string]string  // Maps full qualified type name -> component name (reverse lookup)
}

// setLogger sets the logger for collision warnings.
func (g *schemaGenerator) setLogger(logger Logger) {
	g.logger = logger
}

// newSchemaGenerator creates a new schema generator.
func newSchemaGenerator(components map[string]*Schema, logger Logger) *schemaGenerator {
	return &schemaGenerator{
		schemas:         make(map[string]*Schema),
		components:      components,
		logger:          logger,
		typeRegistry:    make(map[string]string),
		reverseRegistry: make(map[string]string),
	}
}

// GenerateSchema generates a JSON schema from a Go type.
func (g *schemaGenerator) GenerateSchema(t any) (*Schema, error) {
	if t == nil {
		return nil, nil //nolint:nilnil // No schema for nil type
	}

	typ := reflect.TypeOf(t)

	// Handle pointer types
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	return g.generateSchemaFromType(typ)
}

func (g *schemaGenerator) generateSchemaFromType(typ reflect.Type) (*Schema, error) {
	schema := &Schema{}

	// Handle time.Time specially before switch
	if typ == reflect.TypeFor[time.Time]() {
		schema.Type = "string"
		schema.Format = "date-time"

		return schema, nil
	}

	// Check if type implements encoding.TextMarshaler or json.Marshaler
	// These types should be serialized as strings in JSON/OpenAPI
	if implementsTextMarshaler(typ) || implementsJSONMarshaler(typ) {
		schema.Type = "string"
		schema.Format = detectSchemaFormat(typ)

		return schema, nil
	}

	// Check if type implements SchemaTyper to declare its own schema type
	if implementsSchemaTyper(typ) {
		val := reflect.New(typ).Interface()
		if st, ok := val.(SchemaTyper); ok {
			schema.Type = st.SchemaType()
			schema.Format = detectSchemaFormat(typ)

			return schema, nil
		}
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

		itemsSchema, err := g.generateSchemaFromType(typ.Elem())
		if err != nil {
			return nil, err
		}

		schema.Items = itemsSchema
	case reflect.Map:
		schema.Type = "object"
		schema.AdditionalProperties = true
	case reflect.Ptr:
		return g.generateSchemaFromType(typ.Elem())
	case reflect.Interface:
		// Generic interface - allow any type
		return &Schema{}, nil
	default:
		schema.Type = "object"
	}

	return schema, nil
}

func (g *schemaGenerator) generateStructSchema(typ reflect.Type) (*Schema, error) {
	schema := &Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
	}

	var required []string

	for i := range typ.NumField() {
		field := typ.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get JSON tag first to determine if embedded field should be flattened
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue // Skip fields with json:"-"
		}

		// Handle embedded/anonymous struct fields
		// Only flatten if no explicit JSON tag name is provided
		if field.Anonymous {
			jsonName, _ := parseJSONTag(jsonTag)

			// If there's an explicit JSON name, treat as regular field (not flattened)
			if jsonName != "" {
				// Fall through to regular field handling
			} else {
				// Flatten the embedded struct
				embeddedSchema, embeddedRequired, err := g.flattenEmbeddedStruct(field)
				if err != nil {
					return nil, err
				}

				// Merge embedded properties into parent schema
				maps.Copy(schema.Properties, embeddedSchema)

				// Merge required fields
				required = append(required, embeddedRequired...)

				continue
			}
		}

		// Parse JSON tag
		jsonName, omitempty := parseJSONTag(jsonTag)
		if jsonName == "" {
			jsonName = field.Name
		}

		// Generate field schema
		fieldSchema, err := g.generateFieldSchema(field)
		if err != nil {
			return nil, err
		}

		schema.Properties[jsonName] = fieldSchema

		// Determine if field is required
		// Check for optional tag first (explicit opt-out), then required tag (explicit opt-in), then fall back to omitempty logic
		if optionalTag := field.Tag.Get("optional"); optionalTag == "true" {
			// Explicitly marked as optional, skip adding to required
		} else if requiredTag := field.Tag.Get("required"); requiredTag == "true" {
			required = append(required, jsonName)
		} else if !omitempty && field.Type.Kind() != reflect.Ptr {
			required = append(required, jsonName)
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema, nil
}

// flattenEmbeddedStruct processes an embedded/anonymous struct field and returns its flattened properties.
func (g *schemaGenerator) flattenEmbeddedStruct(field reflect.StructField) (map[string]*Schema, []string, error) {
	fieldType := field.Type

	// Handle pointer types
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// If it's not a struct, we can't flatten it
	if fieldType.Kind() != reflect.Struct {
		return nil, nil, nil
	}

	properties := make(map[string]*Schema)

	var required []string

	// Recursively process embedded struct fields
	for i := range fieldType.NumField() {
		embeddedField := fieldType.Field(i)

		// Skip unexported fields
		if !embeddedField.IsExported() {
			continue
		}

		// Handle nested embedded structs recursively
		if embeddedField.Anonymous {
			nestedProps, nestedRequired, err := g.flattenEmbeddedStruct(embeddedField)
			if err != nil {
				return nil, nil, err
			}

			// Merge nested properties
			maps.Copy(properties, nestedProps)

			required = append(required, nestedRequired...)

			continue
		}

		// Get JSON tag
		jsonTag := embeddedField.Tag.Get("json")
		if jsonTag == "-" {
			continue // Skip fields with json:"-"
		}

		// Parse JSON tag
		jsonName, omitempty := parseJSONTag(jsonTag)
		if jsonName == "" {
			jsonName = embeddedField.Name
		}

		// Generate field schema
		fieldSchema, err := g.generateFieldSchema(embeddedField)
		if err != nil {
			return nil, nil, err
		}

		properties[jsonName] = fieldSchema

		// Determine if field is required
		// Check for optional tag first (explicit opt-out), then required tag (explicit opt-in), then fall back to omitempty logic
		if optionalTag := embeddedField.Tag.Get("optional"); optionalTag == "true" {
			// Explicitly marked as optional, skip adding to required
		} else if requiredTag := embeddedField.Tag.Get("required"); requiredTag == "true" {
			required = append(required, jsonName)
		} else if !omitempty && embeddedField.Type.Kind() != reflect.Ptr {
			required = append(required, jsonName)
		}
	}

	return properties, required, nil
}

func (g *schemaGenerator) generateFieldSchema(field reflect.StructField) (*Schema, error) {
	fieldType := field.Type

	// Handle pointer types
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// Check if this is a named enum type that should be a component reference
	// Only extract if it has enum values (via EnumValuer interface or enum tag)
	if g.shouldBeEnumComponentRef(fieldType, field) {
		return g.createOrReuseEnumComponentRef(fieldType, field)
	}

	// Check if this is a named struct type that should be a component reference
	if g.shouldBeComponentRef(fieldType) {
		return g.createOrReuseComponentRef(fieldType, field)
	}

	// Handle slices/arrays of named types
	if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
		elemType := fieldType.Elem()
		if elemType.Kind() == reflect.Ptr {
			elemType = elemType.Elem()
		}

		// Check for enum arrays first
		if g.shouldBeEnumComponentRef(elemType, field) {
			return g.createArrayWithEnumComponentRef(elemType, field)
		}

		if g.shouldBeComponentRef(elemType) {
			return g.createArrayWithComponentRef(elemType, field)
		}
	}

	// Default behavior for primitives and inline types
	schema, err := g.generateSchemaFromType(field.Type)
	if err != nil {
		return nil, err
	}

	g.applyStructTags(schema, field)

	return schema, nil
}

// shouldBeComponentRef determines if a type should be extracted as a component.
func (g *schemaGenerator) shouldBeComponentRef(typ reflect.Type) bool {
	if typ.Kind() != reflect.Struct || typ.Name() == "" || typ == reflect.TypeFor[time.Time]() {
		return false
	}

	// Types that serialize as strings (TextMarshaler/JSONMarshaler) don't need
	// to be named components - they resolve to just type: string inline.
	if implementsTextMarshaler(typ) || implementsJSONMarshaler(typ) {
		return false
	}

	// Types implementing SchemaTyper declare their own schema type and should be inline.
	if implementsSchemaTyper(typ) {
		return false
	}

	return true
}

// shouldBeEnumComponentRef determines if a type should be extracted as an enum component.
// It checks if the type is a named custom type with marshaler interface AND has enum values
// (either via EnumValuer interface or enum struct tag).
func (g *schemaGenerator) shouldBeEnumComponentRef(typ reflect.Type, field reflect.StructField) bool {
	// Must be a named type (not built-in or time.Time) with marshaler interface
	if typ.Name() == "" || typ.PkgPath() == "" {
		return false
	}

	// Exclude time.Time - it has special handling
	if typ == reflect.TypeFor[time.Time]() {
		return false
	}

	// Only extract if it has marshaler interface
	if !implementsTextMarshaler(typ) && !implementsJSONMarshaler(typ) {
		return false
	}

	// Only extract as component if it has enum values (EnumValuer or enum tag)
	// This maintains backward compatibility - types without enum values stay inline
	enumValuerType := reflect.TypeFor[EnumValuer]()
	hasEnumValuer := typ.Implements(enumValuerType) || reflect.PointerTo(typ).Implements(enumValuerType)
	hasEnumTag := field.Tag.Get("enum") != ""

	return hasEnumValuer || hasEnumTag
}

// createOrReuseComponentRef creates or reuses a component reference for a struct type.
// Collisions are auto-resolved by namespacing the component name.
func (g *schemaGenerator) createOrReuseComponentRef(typ reflect.Type, field reflect.StructField) (*Schema, error) {
	componentName := g.resolveComponentName(typ)

	// Register the component if not already registered
	if _, exists := g.schemas[componentName]; !exists {
		// Create placeholder BEFORE recursing to break circular references
		placeholder := &Schema{Type: "object"}

		g.schemas[componentName] = placeholder
		if g.components != nil {
			g.components[componentName] = placeholder
		}

		// Now generate the actual schema - circular refs will find the placeholder
		componentSchema, err := g.generateSchemaFromType(typ)
		if err != nil {
			return nil, err
		}

		g.schemas[componentName] = componentSchema

		// Update spec components if available
		if g.components != nil {
			g.components[componentName] = componentSchema
		}
	}

	// Return a reference schema
	refSchema := &Schema{
		Ref: "#/components/schemas/" + componentName,
	}

	// Apply struct tags to the reference (for description, etc.)
	if desc := field.Tag.Get("description"); desc != "" {
		refSchema.Description = desc
	}

	if title := field.Tag.Get("title"); title != "" {
		refSchema.Title = title
	}

	return refSchema, nil
}

// createArrayWithComponentRef creates an array schema with component reference for elements.
// Collisions are auto-resolved by namespacing the component name.
func (g *schemaGenerator) createArrayWithComponentRef(elemType reflect.Type, field reflect.StructField) (*Schema, error) {
	componentName := g.resolveComponentName(elemType)

	// Register the element type as a component if not already registered
	if _, exists := g.schemas[componentName]; !exists {
		// Create placeholder BEFORE recursing to break circular references
		placeholder := &Schema{Type: "object"}

		g.schemas[componentName] = placeholder
		if g.components != nil {
			g.components[componentName] = placeholder
		}

		// Now generate the actual schema - circular refs will find the placeholder
		componentSchema, err := g.generateSchemaFromType(elemType)
		if err != nil {
			return nil, err
		}

		g.schemas[componentName] = componentSchema

		// Update spec components if available
		if g.components != nil {
			g.components[componentName] = componentSchema
		}
	}

	// Return array schema with ref to component
	arraySchema := &Schema{
		Type: "array",
		Items: &Schema{
			Ref: "#/components/schemas/" + componentName,
		},
	}

	// Apply struct tags to the array
	g.applyStructTags(arraySchema, field)

	return arraySchema, nil
}

// createOrReuseEnumComponentRef creates or reuses a component reference for an enum type.
// Collisions are auto-resolved by namespacing the component name.
func (g *schemaGenerator) createOrReuseEnumComponentRef(typ reflect.Type, field reflect.StructField) (*Schema, error) {
	// Check if type has a custom enum name via EnumNamer interface
	componentName := getEnumComponentName(typ)
	defaultName := GetTypeName(typ)

	if componentName == defaultName {
		// No custom name; use collision resolution
		componentName = g.resolveComponentName(typ)
	} else {
		// Custom name from EnumNamer; register it directly
		qualifiedName := getQualifiedTypeName(typ)
		if _, exists := g.typeRegistry[componentName]; !exists {
			g.typeRegistry[componentName] = qualifiedName
			g.reverseRegistry[qualifiedName] = componentName
		}
	}

	// Register component if not exists
	if _, exists := g.schemas[componentName]; !exists {
		enumSchema := &Schema{
			Type: getBaseTypeForEnum(typ),
		}

		// Extract enum values (EnumValuer interface > struct tag > none)
		enumValues := extractEnumValues(typ, field)
		if len(enumValues) > 0 {
			enumSchema.Enum = enumValues
		}

		g.schemas[componentName] = enumSchema
		if g.components != nil {
			g.components[componentName] = enumSchema
		}
	}

	// Return reference
	refSchema := &Schema{
		Ref: "#/components/schemas/" + componentName,
	}

	// Apply field-level description/title
	if desc := field.Tag.Get("description"); desc != "" {
		refSchema.Description = desc
	}

	if title := field.Tag.Get("title"); title != "" {
		refSchema.Title = title
	}

	return refSchema, nil
}

// createArrayWithEnumComponentRef creates an array schema with enum component reference.
// Collisions are auto-resolved by namespacing the component name.
func (g *schemaGenerator) createArrayWithEnumComponentRef(elemType reflect.Type, field reflect.StructField) (*Schema, error) {
	// Check if type has a custom enum name via EnumNamer interface
	componentName := getEnumComponentName(elemType)
	defaultName := GetTypeName(elemType)

	if componentName == defaultName {
		// No custom name; use collision resolution
		componentName = g.resolveComponentName(elemType)
	} else {
		// Custom name from EnumNamer; register it directly
		qualifiedName := getQualifiedTypeName(elemType)
		if _, exists := g.typeRegistry[componentName]; !exists {
			g.typeRegistry[componentName] = qualifiedName
			g.reverseRegistry[qualifiedName] = componentName
		}
	}

	// Register component if not exists
	if _, exists := g.schemas[componentName]; !exists {
		enumSchema := &Schema{
			Type: getBaseTypeForEnum(elemType),
		}

		// Extract enum values
		enumValues := extractEnumValues(elemType, field)
		if len(enumValues) > 0 {
			enumSchema.Enum = enumValues
		}

		g.schemas[componentName] = enumSchema
		if g.components != nil {
			g.components[componentName] = enumSchema
		}
	}

	// Return array with component reference
	arraySchema := &Schema{
		Type: "array",
		Items: &Schema{
			Ref: "#/components/schemas/" + componentName,
		},
	}

	g.applyStructTags(arraySchema, field)

	return arraySchema, nil
}

// getEnumComponentName returns the component name for an enum type.
// It checks if the type implements EnumNamer interface for a custom name,
// otherwise falls back to the type name.
func getEnumComponentName(typ reflect.Type) string {
	// Check if type implements EnumNamer interface
	enumNamerType := reflect.TypeFor[EnumNamer]()

	// Check both value and pointer receivers
	if typ.Implements(enumNamerType) || reflect.PointerTo(typ).Implements(enumNamerType) {
		var instance reflect.Value
		if typ.Implements(enumNamerType) {
			instance = reflect.Zero(typ)
		} else {
			instance = reflect.New(typ)
		}

		method := instance.MethodByName("EnumComponentName")
		if method.IsValid() {
			results := method.Call(nil)
			if len(results) > 0 {
				if customName, ok := results[0].Interface().(string); ok && customName != "" {
					return customName
				}
			}
		}
	}

	// Fall back to default type name
	return GetTypeName(typ)
}

// getBaseTypeForEnum determines the base OpenAPI type for an enum.
func getBaseTypeForEnum(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "string"
	}
}

// extractEnumValues extracts enum values from EnumValuer interface or struct tag.
func extractEnumValues(typ reflect.Type, field reflect.StructField) []any {
	// Priority 1: Check if type implements EnumValuer interface
	enumValuerType := reflect.TypeFor[EnumValuer]()

	// Check both value and pointer receivers
	if typ.Implements(enumValuerType) || reflect.PointerTo(typ).Implements(enumValuerType) {
		var instance reflect.Value
		if typ.Implements(enumValuerType) {
			instance = reflect.Zero(typ)
		} else {
			instance = reflect.New(typ)
		}

		method := instance.MethodByName("EnumValues")
		if method.IsValid() {
			results := method.Call(nil)
			if len(results) > 0 {
				if enumVals, ok := results[0].Interface().([]any); ok && len(enumVals) > 0 {
					return enumVals
				}
			}
		}
	}

	// Priority 2: Fall back to struct tag
	if enumTag := field.Tag.Get("enum"); enumTag != "" {
		enumStrings := strings.Split(enumTag, ",")

		enumValues := make([]any, len(enumStrings))
		for i, v := range enumStrings {
			enumValues[i] = strings.TrimSpace(v)
		}

		return enumValues
	}

	// Priority 3: No enum constraint
	return nil
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
		schema.Enum = []any{constVal}
	}

	// Pattern
	if pattern := field.Tag.Get("pattern"); pattern != "" {
		schema.Pattern = pattern
	}

	// Enum
	if enum := field.Tag.Get("enum"); enum != "" {
		enumValues := strings.Split(enum, ",")

		schema.Enum = make([]any, len(enumValues))
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

// parseJSONTag parses a JSON struct tag.
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

// parseExample converts string example to appropriate type.
func parseExample(example, schemaType string) any {
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

// GetTypeName returns a qualified type name for schema references.
func GetTypeName(t reflect.Type) string {
	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// For named types, use just the type name (without package prefix)
	// This makes OpenAPI component names cleaner (e.g., "User" instead of "main.User")
	if t.Name() != "" {
		// Clean up generic type names to remove verbose package paths
		// e.g., "PaginatedResponse[*github.com/user/pkg.Model]" -> "PaginatedResponse[Model]"
		return cleanGenericTypeName(t.Name())
	}

	// Fallback for anonymous types
	return "Object"
}

// cleanGenericTypeName removes package paths from generic type parameter names
// Input:  "router.PaginatedResponse[*github.com/wakflo/kineta/extensions/workspace.Workspace]"
// Output: "PaginatedResponse[*Workspace]".
func cleanGenericTypeName(name string) string {
	if !strings.Contains(name, "[") {
		// Not a generic type, return as-is
		return name
	}

	// Split into base type and generic parameters
	bracketIdx := strings.Index(name, "[")
	if bracketIdx == -1 {
		return name
	}

	baseType := name[:bracketIdx]
	rest := name[bracketIdx:] // "[...]"

	// Clean the base type (remove package prefix)
	baseType = cleanTypeParam(baseType)

	var result strings.Builder
	result.WriteString(baseType)

	inBracket := false

	var current strings.Builder

	for i := range len(rest) {
		ch := rest[i]

		switch ch {
		case '[':
			// Start of generic parameters
			result.WriteByte(ch)

			inBracket = true

			current.Reset()

		case ']':
			// End of generic parameters - clean up the accumulated parameter
			if inBracket {
				param := current.String()
				cleanedParam := cleanTypeParam(param)
				result.WriteString(cleanedParam)
				result.WriteByte(ch)

				inBracket = false

				current.Reset()
			} else {
				result.WriteByte(ch)
			}

		case ',':
			// Parameter separator - clean up accumulated parameter
			if inBracket {
				param := current.String()
				cleanedParam := cleanTypeParam(param)
				result.WriteString(cleanedParam)
				result.WriteByte(ch)
				current.Reset()
			} else {
				result.WriteByte(ch)
			}

		default:
			if inBracket {
				current.WriteByte(ch)
			} else {
				result.WriteByte(ch)
			}
		}
	}

	// Handle any remaining parameter
	if current.Len() > 0 {
		param := current.String()
		cleanedParam := cleanTypeParam(param)
		result.WriteString(cleanedParam)
	}

	return result.String()
}

// cleanTypeParam cleans a single type parameter
// Input:  "*github.com/wakflo/kineta/extensions/workspace.Workspace"
// Output: "Workspace".
func cleanTypeParam(param string) string {
	param = strings.TrimSpace(param)

	// Handle pointer prefix
	pointerPrefix := ""
	if strings.HasPrefix(param, "*") {
		pointerPrefix = "*"
		param = param[1:]
	}

	// Find the last dot (type name separator)
	lastDot := strings.LastIndex(param, ".")
	if lastDot != -1 {
		// Extract just the type name
		typeName := param[lastDot+1:]

		// Handle the special ·N suffix that Go adds for type instances
		if idx := strings.Index(typeName, "·"); idx != -1 {
			typeName = typeName[:idx]
		}

		return pointerPrefix + typeName
	}

	// No package path, return as-is (e.g., "int", "string")
	return pointerPrefix + param
}

// getQualifiedTypeName returns the full qualified type name (package path + type name).
func getQualifiedTypeName(t reflect.Type) string {
	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Get package path and type name
	pkgPath := t.PkgPath()
	typeName := t.Name()

	if typeName == "" {
		return "Object"
	}

	if pkgPath != "" {
		return pkgPath + "." + typeName
	}

	return typeName
}

// titleCase capitalizes the first character of a string.
func titleCase(s string) string {
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}

// buildNamespacedCandidates generates progressively more specific candidate names
// from a package path and type name to resolve naming collisions.
//
// Examples:
//
//	"github.com/xraph/relay/id", "ID"              -> ["RelayID", "RelayIdID"]
//	"github.com/xraph/cortex/communication", "Style" -> ["CommunicationStyle", "CortexStyle", "CortexCommunicationStyle"]
//	"github.com/xraph/sentinel/baseline", "Result"   -> ["BaselineResult", "SentinelResult", "SentinelBaselineResult"]
func buildNamespacedCandidates(pkgPath, typeName string) []string {
	if pkgPath == "" {
		return []string{typeName}
	}

	parts := strings.Split(pkgPath, "/")

	// Extract meaningful segments (skip domain parts containing dots like "github.com")
	var meaningful []string
	for _, p := range parts {
		if !strings.Contains(p, ".") {
			meaningful = append(meaningful, p)
		}
	}

	// Skip the org/user segment (e.g., "xraph")
	if len(meaningful) > 1 {
		meaningful = meaningful[1:]
	}

	// meaningful now contains: [module, sub-package1, sub-package2, ...]
	var candidates []string

	if len(meaningful) >= 2 {
		module := meaningful[0]              // e.g., "relay"
		sub := meaningful[len(meaningful)-1] // e.g., "id" or "communication"

		// Candidate 1: sub-package + type (e.g., "BaselineResult", "CommunicationStyle")
		// Skip if sub-package name matches type name (case-insensitive, e.g., "id"+"ID" -> redundant)
		if !strings.EqualFold(sub, typeName) {
			candidates = append(candidates, titleCase(sub)+typeName)
		}

		// Candidate 2: module + type (e.g., "RelayID", "SentinelResult")
		moduleCandidate := titleCase(module) + typeName
		if len(candidates) == 0 || candidates[len(candidates)-1] != moduleCandidate {
			candidates = append(candidates, moduleCandidate)
		}

		// Candidate 3: module + sub-package + type (e.g., "CortexCommunicationStyle")
		fullCandidate := titleCase(module) + titleCase(sub) + typeName
		candidates = append(candidates, fullCandidate)
	} else if len(meaningful) == 1 {
		candidates = append(candidates, titleCase(meaningful[0])+typeName)
	}

	if len(candidates) == 0 {
		candidates = append(candidates, typeName)
	}

	return candidates
}

// resolveComponentName returns a unique component name for the given type.
// The first type to claim a short name keeps it; colliding types get namespaced names.
// This method is idempotent: calling it again for the same type returns the same name.
func (g *schemaGenerator) resolveComponentName(typ reflect.Type) string {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	shortName := GetTypeName(typ)
	qualifiedName := getQualifiedTypeName(typ)

	// Fast path: check if this exact type was already registered
	if existingName, ok := g.reverseRegistry[qualifiedName]; ok {
		return existingName
	}

	// Try the short name first
	if existingQualified, exists := g.typeRegistry[shortName]; !exists {
		// Short name is free
		g.typeRegistry[shortName] = qualifiedName
		g.reverseRegistry[qualifiedName] = shortName

		return shortName
	} else if existingQualified == qualifiedName {
		// Same type already registered under short name
		g.reverseRegistry[qualifiedName] = shortName

		return shortName
	}

	// Collision: try namespaced candidates
	candidates := buildNamespacedCandidates(typ.PkgPath(), typ.Name())
	for _, candidate := range candidates {
		if existingQualified, exists := g.typeRegistry[candidate]; !exists {
			g.typeRegistry[candidate] = qualifiedName
			g.reverseRegistry[qualifiedName] = candidate

			if g.logger != nil {
				g.logger.Info(fmt.Sprintf(
					"schema component name collision resolved: '%s' from '%s' registered as '%s'",
					shortName, qualifiedName, candidate))
			}

			return candidate
		} else if existingQualified == qualifiedName {
			g.reverseRegistry[qualifiedName] = candidate

			return candidate
		}
	}

	// All candidates collided: append numeric suffix
	base := candidates[0]
	for i := 2; ; i++ {
		candidate := base + strconv.Itoa(i)
		if _, exists := g.typeRegistry[candidate]; !exists {
			g.typeRegistry[candidate] = qualifiedName
			g.reverseRegistry[qualifiedName] = candidate

			if g.logger != nil {
				g.logger.Info(fmt.Sprintf(
					"schema component name collision resolved: '%s' from '%s' registered as '%s'",
					shortName, qualifiedName, candidate))
			}

			return candidate
		}
	}
}

// implementsTextMarshaler checks if a type implements encoding.TextMarshaler.
func implementsTextMarshaler(typ reflect.Type) bool {
	textMarshalerType := reflect.TypeFor[encoding.TextMarshaler]()

	return typ.Implements(textMarshalerType)
}

// implementsJSONMarshaler checks if a type implements json.Marshaler.
func implementsJSONMarshaler(typ reflect.Type) bool {
	jsonMarshalerType := reflect.TypeFor[json.Marshaler]()

	return typ.Implements(jsonMarshalerType)
}

// implementsSchemaTyper checks if a type implements the SchemaTyper interface.
func implementsSchemaTyper(typ reflect.Type) bool {
	schemaTyperType := reflect.TypeFor[SchemaTyper]()

	return typ.Implements(schemaTyperType) || reflect.PointerTo(typ).Implements(schemaTyperType)
}

// implementsSchemaFormatter checks if a type implements the SchemaFormatter interface.
func implementsSchemaFormatter(typ reflect.Type) bool {
	schemaFormatterType := reflect.TypeFor[SchemaFormatter]()

	return typ.Implements(schemaFormatterType) || reflect.PointerTo(typ).Implements(schemaFormatterType)
}

// detectSchemaFormat returns the OpenAPI format for a type.
// It first checks if the type implements SchemaFormatter, then falls back to
// auto-detection based on well-known type names and package paths.
func detectSchemaFormat(typ reflect.Type) string {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	// Check SchemaFormatter interface first
	if implementsSchemaFormatter(typ) {
		val := reflect.New(typ).Interface()
		if sf, ok := val.(SchemaFormatter); ok {
			if f := sf.SchemaFormat(); f != "" {
				return f
			}
		}
	}

	// Auto-detect well-known types by name and package path
	typeName := typ.Name()
	pkgPath := typ.PkgPath()

	switch {
	case typeName == "UUID" && strings.Contains(pkgPath, "uuid"):
		return "uuid"
	case typeName == "ID" && strings.Contains(pkgPath, "xid"):
		return "xid"
	case typeName == "ULID" && strings.Contains(pkgPath, "ulid"):
		return "ulid"
	case typeName == "NullUUID" && strings.Contains(pkgPath, "uuid"):
		return "uuid"
	}

	return ""
}
