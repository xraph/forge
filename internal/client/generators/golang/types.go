package golang

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// TypesGenerator generates Go type definitions.
type TypesGenerator struct{}

// NewTypesGenerator creates a new types generator.
func NewTypesGenerator() *TypesGenerator {
	return &TypesGenerator{}
}

// Generate generates the types.go file.
func (t *TypesGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	buf.WriteString("import (\n")
	buf.WriteString("\t\"time\"\n")
	buf.WriteString(")\n\n")

	// Generate types from schemas
	if len(spec.Schemas) > 0 {
		buf.WriteString("// Generated types\n\n")

		// Sort schema names for consistent output
		names := make([]string, 0, len(spec.Schemas))
		for name := range spec.Schemas {
			names = append(names, name)
		}

		sort.Strings(names)

		for _, name := range names {
			schema := spec.Schemas[name]
			typeCode := t.generateType(name, schema, spec)
			buf.WriteString(typeCode)
			buf.WriteString("\n")
		}
	}

	// Generate request/response types from endpoints
	for _, endpoint := range spec.Endpoints {
		if endpoint.RequestBody != nil {
			for contentType, media := range endpoint.RequestBody.Content {
				if contentType == "application/json" && media.Schema != nil {
					typeName := t.generateRequestTypeName(endpoint)
					if !t.isSchemaRef(media.Schema) {
						typeCode := t.generateType(typeName, media.Schema, spec)
						buf.WriteString(typeCode)
						buf.WriteString("\n")
					}
				}
			}
		}

		for statusCode, resp := range endpoint.Responses {
			for contentType, media := range resp.Content {
				if contentType == "application/json" && media.Schema != nil {
					typeName := t.generateResponseTypeName(endpoint, statusCode)
					if !t.isSchemaRef(media.Schema) {
						typeCode := t.generateType(typeName, media.Schema, spec)
						buf.WriteString(typeCode)
						buf.WriteString("\n")
					}
				}
			}
		}
	}

	return buf.String()
}

// generateType generates a Go type definition from a schema.
func (t *TypesGenerator) generateType(name string, schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return ""
	}

	// Handle schema references
	if schema.Ref != "" {
		refSchema := spec.ResolveSchemaRef(schema.Ref)
		if refSchema != nil {
			return t.generateType(name, refSchema, spec)
		}
	}

	var buf strings.Builder

	// Add description as comment
	if schema.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s %s\n", name, schema.Description))
	} else {
		buf.WriteString(fmt.Sprintf("// %s represents a %s\n", name, name))
	}

	// Handle polymorphic types
	if len(schema.OneOf) > 0 {
		return t.generateOneOfType(name, schema, spec)
	}

	if len(schema.AnyOf) > 0 {
		return t.generateAnyOfType(name, schema, spec)
	}

	if len(schema.AllOf) > 0 {
		return t.generateAllOfType(name, schema, spec)
	}

	switch schema.Type {
	case "object":
		buf.WriteString(fmt.Sprintf("type %s struct {\n", name))

		// Sort properties for consistent output
		propNames := make([]string, 0, len(schema.Properties))
		for propName := range schema.Properties {
			propNames = append(propNames, propName)
		}

		sort.Strings(propNames)

		for _, propName := range propNames {
			prop := schema.Properties[propName]
			fieldName := t.toGoFieldName(propName)
			goType := t.schemaToGoType(prop, spec)
			required := t.isRequired(propName, schema.Required)

			// Add JSON tag
			jsonTag := propName
			if prop.Nullable || !required {
				jsonTag += ",omitempty"
			}

			// Add description as comment
			comment := ""
			if prop.Description != "" {
				comment = " // " + prop.Description
			}

			buf.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"`%s\n",
				fieldName, goType, jsonTag, comment))
		}

		buf.WriteString("}\n")

	case "array":
		// For top-level arrays, create a type alias
		if schema.Items != nil {
			itemType := t.schemaToGoType(schema.Items, spec)
			buf.WriteString(fmt.Sprintf("type %s []%s\n", name, itemType))
		}

	default:
		// For primitive types, create a type alias
		goType := t.schemaToGoType(schema, spec)
		buf.WriteString(fmt.Sprintf("type %s %s\n", name, goType))
	}

	return buf.String()
}

// schemaToGoType converts a schema to a Go type string.
func (t *TypesGenerator) schemaToGoType(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "interface{}"
	}

	// Handle references
	if schema.Ref != "" {
		refName := t.extractRefName(schema.Ref)

		return refName
	}

	// Handle nullable types with pointer
	needsPointer := schema.Nullable

	var baseType string

	switch schema.Type {
	case "string":
		switch schema.Format {
		case "date-time":
			baseType = "time.Time"
		case "date":
			baseType = "time.Time"
		case "byte", "binary":
			baseType = "[]byte"
		default:
			baseType = "string"
		}

	case "integer":
		switch schema.Format {
		case "int32":
			baseType = "int32"
		case "int64":
			baseType = "int64"
		default:
			baseType = "int"
		}

	case "number":
		switch schema.Format {
		case "float":
			baseType = "float32"
		case "double":
			baseType = "float64"
		default:
			baseType = "float64"
		}

	case "boolean":
		baseType = "bool"

	case "array":
		if schema.Items != nil {
			itemType := t.schemaToGoType(schema.Items, spec)
			baseType = "[]" + itemType
			needsPointer = false // Arrays are already reference types
		} else {
			baseType = "[]interface{}"
			needsPointer = false
		}

	case "object":
		if len(schema.Properties) > 0 {
			// Anonymous struct
			baseType = t.generateAnonymousStruct(schema, spec)
			needsPointer = false
		} else {
			baseType = "map[string]interface{}"
			needsPointer = false
		}

	default:
		baseType = "interface{}"
		needsPointer = false
	}

	if needsPointer {
		return "*" + baseType
	}

	return baseType
}

// generateAnonymousStruct generates an anonymous struct for inline objects.
func (t *TypesGenerator) generateAnonymousStruct(schema *client.Schema, spec *client.APISpec) string {
	var buf strings.Builder

	buf.WriteString("struct {\n")

	// Sort properties
	propNames := make([]string, 0, len(schema.Properties))
	for propName := range schema.Properties {
		propNames = append(propNames, propName)
	}

	sort.Strings(propNames)

	for _, propName := range propNames {
		prop := schema.Properties[propName]
		fieldName := t.toGoFieldName(propName)
		goType := t.schemaToGoType(prop, spec)
		required := t.isRequired(propName, schema.Required)

		jsonTag := propName
		if prop.Nullable || !required {
			jsonTag += ",omitempty"
		}

		buf.WriteString(fmt.Sprintf("\t\t%s %s `json:\"%s\"`\n", fieldName, goType, jsonTag))
	}

	buf.WriteString("\t}")

	return buf.String()
}

// toGoFieldName converts a field name to Go naming convention.
func (t *TypesGenerator) toGoFieldName(name string) string {
	// Split by underscore or dash
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-'
	})

	var (
		result      string
		resultSb278 strings.Builder
	)

	for _, part := range parts {
		if len(part) > 0 {
			resultSb278.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}

	result += resultSb278.String()

	// If empty, return the original name capitalized
	if result == "" {
		if len(name) > 0 {
			return strings.ToUpper(name[:1]) + name[1:]
		}

		return name
	}

	return result
}

// isRequired checks if a property is required.
func (t *TypesGenerator) isRequired(propName string, required []string) bool {
	return slices.Contains(required, propName)
}

// isSchemaRef checks if a schema is a reference.
func (t *TypesGenerator) isSchemaRef(schema *client.Schema) bool {
	return schema != nil && schema.Ref != ""
}

// extractRefName extracts the type name from a schema reference.
func (t *TypesGenerator) extractRefName(ref string) string {
	parts := strings.Split(ref, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return "Unknown"
}

// generateRequestTypeName generates a name for a request type.
func (t *TypesGenerator) generateRequestTypeName(endpoint client.Endpoint) string {
	if endpoint.OperationID != "" {
		return t.toGoFieldName(endpoint.OperationID) + "Request"
	}
	// Fallback: generate from path and method
	path := strings.ReplaceAll(endpoint.Path, "/", "_")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")

	return t.toGoFieldName(endpoint.Method + path + "Request")
}

// generateResponseTypeName generates a name for a response type.
func (t *TypesGenerator) generateResponseTypeName(endpoint client.Endpoint, statusCode int) string {
	if endpoint.OperationID != "" {
		return t.toGoFieldName(endpoint.OperationID) + "Response"
	}
	// Fallback
	path := strings.ReplaceAll(endpoint.Path, "/", "_")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")

	return t.toGoFieldName(fmt.Sprintf("%s%sResponse%d", endpoint.Method, path, statusCode))
}

// generateOneOfType generates a Go interface for oneOf schemas.
func (t *TypesGenerator) generateOneOfType(name string, schema *client.Schema, spec *client.APISpec) string {
	var buf strings.Builder

	// Generate interface
	if schema.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s %s\n", name, schema.Description))
	} else {
		buf.WriteString(fmt.Sprintf("// %s is a union type (oneOf)\n", name))
	}

	buf.WriteString(fmt.Sprintf("type %s interface {\n", name))
	buf.WriteString(fmt.Sprintf("\tis%s()\n", name))
	buf.WriteString("}\n\n")

	// Generate concrete types for each option
	for i, option := range schema.OneOf {
		typeName := fmt.Sprintf("%sOption%d", name, i+1)

		// If the option has a ref, use that name instead
		if option.Ref != "" {
			refName := t.extractRefName(option.Ref)
			buf.WriteString(fmt.Sprintf("func (%s) is%s() {}\n\n", refName, name))
		} else {
			// Generate a concrete struct for inline schemas
			typeCode := t.generateType(typeName, option, spec)
			buf.WriteString(typeCode)
			buf.WriteString(fmt.Sprintf("func (%s) is%s() {}\n\n", typeName, name))
		}
	}

	// Add discriminator support if present
	if schema.Discriminator != nil && schema.Discriminator.PropertyName != "" {
		buf.WriteString(fmt.Sprintf("// Discriminator field: %s\n", schema.Discriminator.PropertyName))
	}

	return buf.String()
}

// generateAnyOfType generates a Go interface for anyOf schemas.
func (t *TypesGenerator) generateAnyOfType(name string, schema *client.Schema, spec *client.APISpec) string {
	var buf strings.Builder

	// AnyOf is similar to oneOf in Go - use interface
	if schema.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s %s\n", name, schema.Description))
	} else {
		buf.WriteString(fmt.Sprintf("// %s is a union type (anyOf)\n", name))
	}

	buf.WriteString(fmt.Sprintf("type %s interface {\n", name))
	buf.WriteString(fmt.Sprintf("\tis%s()\n", name))
	buf.WriteString("}\n\n")

	// Generate concrete types for each option
	for i, option := range schema.AnyOf {
		typeName := fmt.Sprintf("%sVariant%d", name, i+1)

		if option.Ref != "" {
			refName := t.extractRefName(option.Ref)
			buf.WriteString(fmt.Sprintf("func (%s) is%s() {}\n\n", refName, name))
		} else {
			typeCode := t.generateType(typeName, option, spec)
			buf.WriteString(typeCode)
			buf.WriteString(fmt.Sprintf("func (%s) is%s() {}\n\n", typeName, name))
		}
	}

	return buf.String()
}

// generateAllOfType generates a Go struct with embedded types for allOf schemas.
func (t *TypesGenerator) generateAllOfType(name string, schema *client.Schema, spec *client.APISpec) string {
	var buf strings.Builder

	if schema.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s %s\n", name, schema.Description))
	} else {
		buf.WriteString(fmt.Sprintf("// %s is a composition type (allOf)\n", name))
	}

	buf.WriteString(fmt.Sprintf("type %s struct {\n", name))

	// Embed all schemas
	for i, allOfSchema := range schema.AllOf {
		if allOfSchema.Ref != "" { //nolint:gocritic // ifElseChain: schema type handling clearer with if-else
			// Embed the referenced type
			refName := t.extractRefName(allOfSchema.Ref)
			buf.WriteString(fmt.Sprintf("\t%s\n", refName))
		} else if allOfSchema.Type == "object" && len(allOfSchema.Properties) > 0 {
			// Inline properties
			propNames := make([]string, 0, len(allOfSchema.Properties))
			for propName := range allOfSchema.Properties {
				propNames = append(propNames, propName)
			}

			sort.Strings(propNames)

			for _, propName := range propNames {
				prop := allOfSchema.Properties[propName]
				fieldName := t.toGoFieldName(propName)
				goType := t.schemaToGoType(prop, spec)
				required := t.isRequired(propName, allOfSchema.Required)

				jsonTag := propName
				if prop.Nullable || !required {
					jsonTag += ",omitempty"
				}

				comment := ""
				if prop.Description != "" {
					comment = " // " + prop.Description
				}

				buf.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"`%s\n",
					fieldName, goType, jsonTag, comment))
			}
		} else {
			// Create an embedded inline struct
			inlineName := fmt.Sprintf("%sComposed%d", name, i+1)
			typeCode := t.generateType(inlineName, allOfSchema, spec)
			buf.WriteString(typeCode)
			buf.WriteString(fmt.Sprintf("\t%s\n", inlineName))
		}
	}

	buf.WriteString("}\n")

	return buf.String()
}
