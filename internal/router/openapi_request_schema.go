package router

import (
	"reflect"
	"strings"
)

// RequestComponents holds all components of a request extracted from a unified schema.
type RequestComponents struct {
	PathParams   []Parameter
	QueryParams  []Parameter
	HeaderParams []Parameter
	BodySchema   *Schema
	HasBody      bool
	IsMultipart  bool // true if the schema contains file uploads or form:"" tags
}

// extractUnifiedRequestComponents extracts all request components from a unified struct
// Fields are classified based on struct tags in priority order:
// 1. path:"name" - Path parameter
// 2. query:"name" - Query parameter
// 3. header:"name" - Header parameter
// 4. body:"" or json:"name" - Body field.
func extractUnifiedRequestComponents(schemaGen *schemaGenerator, schemaType interface{}) *RequestComponents {
	components := &RequestComponents{
		PathParams:   []Parameter{},
		QueryParams:  []Parameter{},
		HeaderParams: []Parameter{},
	}

	rt := reflect.TypeOf(schemaType)
	if rt == nil {
		return components
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		// Not a struct, treat as body schema
		components.BodySchema = schemaGen.GenerateSchema(schemaType)
		components.HasBody = true

		return components
	}

	// Classify fields by tags
	bodyProperties := make(map[string]*Schema)

	var bodyRequired []string

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check tags in priority order
		if pathTag := field.Tag.Get("path"); pathTag != "" {
			// Path parameter
			param := generatePathParamFromField(schemaGen, field, pathTag)
			components.PathParams = append(components.PathParams, param)
		} else if queryTag := field.Tag.Get("query"); queryTag != "" {
			// Query parameter
			param := generateQueryParamFromField(schemaGen, field, queryTag)
			components.QueryParams = append(components.QueryParams, param)
		} else if headerTag := field.Tag.Get("header"); headerTag != "" {
			// Header parameter
			param := generateHeaderParamFromField(schemaGen, field, headerTag)
			components.HeaderParams = append(components.HeaderParams, param)
		} else if formTag := field.Tag.Get("form"); formTag != "" {
			// Multipart form field - used for multipart/form-data requests
			components.HasBody = true
			components.IsMultipart = true

			// Get field name for form
			formName := field.Name

			name, _ := parseTagWithOmitempty(formTag)
			if name != "" && name != "-" {
				formName = name
			}

			// Generate field schema
			fieldSchema := schemaGen.generateSchemaFromType(field.Type)
			schemaGen.applyStructTags(fieldSchema, field)
			bodyProperties[formName] = fieldSchema

			// Check if required
			if isFieldRequired(field) {
				bodyRequired = append(bodyRequired, formName)
			}
		} else if bodyTag := field.Tag.Get("body"); bodyTag != "" || field.Tag.Get("json") != "" {
			// Body field - explicit body tag or json tag
			components.HasBody = true

			// Get field name for body
			jsonName := field.Name
			if jsonTag := field.Tag.Get("json"); jsonTag != "" {
				name, _ := parseJSONTag(jsonTag)
				if name != "" && name != "-" {
					jsonName = name
				}
			} else if bodyTag != "" && bodyTag != "-" {
				jsonName = bodyTag
			}

			// Generate field schema
			fieldSchema := schemaGen.generateSchemaFromType(field.Type)
			schemaGen.applyStructTags(fieldSchema, field)

			// Check if field is a file (binary format)
			if fieldSchema.Format == "binary" {
				components.IsMultipart = true
			}

			bodyProperties[jsonName] = fieldSchema

			// Check if required
			if isFieldRequired(field) {
				bodyRequired = append(bodyRequired, jsonName)
			}
		}
	}

	// Create body schema if we have body fields
	if components.HasBody && len(bodyProperties) > 0 {
		components.BodySchema = &Schema{
			Type:       "object",
			Properties: bodyProperties,
		}
		if len(bodyRequired) > 0 {
			components.BodySchema.Required = bodyRequired
		}
	}

	return components
}

// generatePathParamFromField generates a path parameter from a struct field.
func generatePathParamFromField(schemaGen *schemaGenerator, field reflect.StructField, tagValue string) Parameter {
	// Parse tag value
	paramName, _ := parseTagWithOmitempty(tagValue)
	if paramName == "" {
		paramName = field.Name
	}

	// Generate schema
	fieldSchema := schemaGen.generateSchemaFromType(field.Type)
	schemaGen.applyStructTags(fieldSchema, field)

	// Path parameters are always required
	required := true

	return Parameter{
		Name:        paramName,
		In:          "path",
		Description: fieldSchema.Description,
		Required:    required,
		Schema:      fieldSchema,
	}
}

// generateQueryParamFromField generates a query parameter from a struct field.
func generateQueryParamFromField(schemaGen *schemaGenerator, field reflect.StructField, tagValue string) Parameter {
	// Parse tag value
	paramName, omitempty := parseTagWithOmitempty(tagValue)
	if paramName == "" {
		paramName = field.Name
	}

	// Generate schema
	fieldSchema := schemaGen.generateSchemaFromType(field.Type)
	schemaGen.applyStructTags(fieldSchema, field)

	// Determine if required
	required := field.Tag.Get("required") == "true"
	if !required && !omitempty && field.Type.Kind() != reflect.Ptr {
		required = true
	}

	return Parameter{
		Name:        paramName,
		In:          "query",
		Description: fieldSchema.Description,
		Required:    required,
		Schema:      fieldSchema,
	}
}

// generateHeaderParamFromField generates a header parameter from a struct field.
func generateHeaderParamFromField(schemaGen *schemaGenerator, field reflect.StructField, tagValue string) Parameter {
	// Parse tag value
	paramName, omitempty := parseTagWithOmitempty(tagValue)
	if paramName == "" {
		paramName = field.Name
	}

	// Generate schema
	fieldSchema := schemaGen.generateSchemaFromType(field.Type)
	schemaGen.applyStructTags(fieldSchema, field)

	// Determine if required
	required := field.Tag.Get("required") == "true"
	if !required && !omitempty && field.Type.Kind() != reflect.Ptr {
		required = true
	}

	return Parameter{
		Name:        paramName,
		In:          "header",
		Description: fieldSchema.Description,
		Required:    required,
		Schema:      fieldSchema,
	}
}

// isFieldRequired determines if a body field is required.
func isFieldRequired(field reflect.StructField) bool {
	// Explicit required tag
	if field.Tag.Get("required") == "true" {
		return true
	}

	// Check JSON tag for omitempty
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		_, omitempty := parseJSONTag(jsonTag)
		if omitempty {
			return false
		}
	}

	// Check body tag for omitempty
	if bodyTag := field.Tag.Get("body"); bodyTag != "" {
		parts := strings.Split(bodyTag, ",")
		for _, part := range parts {
			if part == "omitempty" {
				return false
			}
		}
	}

	// Non-pointer types without omitempty are required
	if field.Type.Kind() != reflect.Ptr {
		return true
	}

	return false
}

// hasUnifiedTags checks if a struct has path, query, header, or form tags
// This is used to determine if we should use unified extraction or legacy behavior.
func hasUnifiedTags(schemaType interface{}) bool {
	rt := reflect.TypeOf(schemaType)
	if rt == nil {
		return false
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return false
	}

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		// Check for unified tags
		if field.Tag.Get("path") != "" ||
			field.Tag.Get("query") != "" ||
			field.Tag.Get("header") != "" ||
			field.Tag.Get("form") != "" {
			return true
		}
	}

	return false
}

// inferRequestComponents infers what parts of the request a struct represents
// Used for backward compatibility with existing code.
func inferRequestComponents(schemaType interface{}) string {
	rt := reflect.TypeOf(schemaType)
	if rt == nil {
		return "body"
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return "body"
	}

	hasQuery := false
	hasHeader := false
	hasBody := false

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		if field.Tag.Get("query") != "" {
			hasQuery = true
		}

		if field.Tag.Get("header") != "" {
			hasHeader = true
		}

		if field.Tag.Get("json") != "" || field.Tag.Get("body") != "" {
			hasBody = true
		}
	}

	// If only has one type, infer that
	if hasQuery && !hasHeader && !hasBody {
		return "query"
	}

	if hasHeader && !hasQuery && !hasBody {
		return "header"
	}

	// Default to body
	return "body"
}
