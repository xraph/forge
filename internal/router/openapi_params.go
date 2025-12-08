package router

import (
	"reflect"
	"strings"
)

// PathParam represents a parsed path parameter.
type PathParam struct {
	Name        string
	Description string
	Schema      *Schema
}

// extractPathParamsFromPath parses path parameters from a URL path
// Supports both :param and {param} style parameters.
func extractPathParamsFromPath(path string) []PathParam {
	var params []PathParam

	// Parse path for :param style parameters
	parts := strings.SplitSeq(path, "/")
	for part := range parts {
		var paramName string

		if after, ok := strings.CutPrefix(part, ":"); ok {
			// :param style (e.g., /users/:id)
			paramName = after
		} else if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			// {param} style (e.g., /users/{id})
			paramName = strings.TrimPrefix(strings.TrimSuffix(part, "}"), "{")
		}

		if paramName != "" {
			params = append(params, PathParam{
				Name:        paramName,
				Description: "Path parameter: " + paramName,
				Schema: &Schema{
					Type: "string",
				},
			})
		}
	}

	return params
}

// convertPathParamsToOpenAPIParams converts PathParam to OpenAPI Parameter.
func convertPathParamsToOpenAPIParams(pathParams []PathParam) []Parameter {
	params := make([]Parameter, len(pathParams))
	for i, pp := range pathParams {
		params[i] = Parameter{
			Name:        pp.Name,
			In:          "path",
			Description: pp.Description,
			Required:    true,
			Schema:      pp.Schema,
		}
	}

	return params
}

// generateQueryParamsFromStruct generates query parameters from a struct type.
func generateQueryParamsFromStruct(schemaGen *schemaGenerator, structType any) []Parameter {
	rt := reflect.TypeOf(structType)
	if rt == nil {
		return nil
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return nil
	}

	var params []Parameter

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded/anonymous struct fields - flatten them
		if field.Anonymous {
			// Check if the embedded field has a query tag (which would mean it's not truly flattened)
			queryTag := field.Tag.Get("query")
			if queryTag == "" {
				// Recursively extract query params from embedded struct
				embeddedParams := flattenEmbeddedQueryParams(schemaGen, field)
				params = append(params, embeddedParams...)
				continue
			}
		}

		// Get query tag
		queryTag := field.Tag.Get("query")
		if queryTag == "" || queryTag == "-" {
			continue
		}

		// Parse tag
		paramName, omitempty := parseTagWithOmitempty(queryTag)
		if paramName == "" {
			paramName = field.Name
		}

		// Generate schema for the field (use generateFieldSchema to support enum components)
		fieldSchema, err := schemaGen.generateFieldSchema(field)
		if err != nil {
			// Skip parameter on error (collision detected)
			continue
		}

		// Determine if required
		// Check for optional tag first (explicit opt-out), then required tag (explicit opt-in), then fall back to omitempty logic
		required := false
		if field.Tag.Get("optional") == "true" {
			required = false
		} else if field.Tag.Get("required") == "true" {
			required = true
		} else if !omitempty && field.Type.Kind() != reflect.Ptr {
			required = true
		}

		params = append(params, Parameter{
			Name:        paramName,
			In:          "query",
			Description: fieldSchema.Description,
			Required:    required,
			Schema:      fieldSchema,
		})
	}

	return params
}

// flattenEmbeddedQueryParams recursively extracts query parameters from an embedded struct.
func flattenEmbeddedQueryParams(schemaGen *schemaGenerator, field reflect.StructField) []Parameter {
	fieldType := field.Type

	// Handle pointer types
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// If it's not a struct, return empty
	if fieldType.Kind() != reflect.Struct {
		return nil
	}

	var params []Parameter

	// Recursively process embedded struct fields
	for i := 0; i < fieldType.NumField(); i++ {
		embeddedField := fieldType.Field(i)

		// Skip unexported fields
		if !embeddedField.IsExported() {
			continue
		}

		// Handle nested embedded structs recursively
		if embeddedField.Anonymous {
			queryTag := embeddedField.Tag.Get("query")
			if queryTag == "" {
				nestedParams := flattenEmbeddedQueryParams(schemaGen, embeddedField)
				params = append(params, nestedParams...)
				continue
			}
		}

		// Get query tag
		queryTag := embeddedField.Tag.Get("query")
		if queryTag == "" || queryTag == "-" {
			continue
		}

		// Parse tag
		paramName, omitempty := parseTagWithOmitempty(queryTag)
		if paramName == "" {
			paramName = embeddedField.Name
		}

		// Generate schema for the field (use generateFieldSchema to support enum components)
		fieldSchema, err := schemaGen.generateFieldSchema(embeddedField)
		if err != nil {
			continue // Skip parameter on error
		}

		// Determine if required
		// Check for optional tag first (explicit opt-out), then required tag (explicit opt-in), then fall back to omitempty logic
		required := false
		if embeddedField.Tag.Get("optional") == "true" {
			required = false
		} else if embeddedField.Tag.Get("required") == "true" {
			required = true
		} else if !omitempty && embeddedField.Type.Kind() != reflect.Ptr {
			required = true
		}

		params = append(params, Parameter{
			Name:        paramName,
			In:          "query",
			Description: fieldSchema.Description,
			Required:    required,
			Schema:      fieldSchema,
		})
	}

	return params
}

// flattenEmbeddedHeaderParams recursively extracts header parameters from an embedded struct.
func flattenEmbeddedHeaderParams(schemaGen *schemaGenerator, field reflect.StructField) []Parameter {
	fieldType := field.Type

	// Handle pointer types
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// If it's not a struct, return empty
	if fieldType.Kind() != reflect.Struct {
		return nil
	}

	var params []Parameter

	// Recursively process embedded struct fields
	for i := 0; i < fieldType.NumField(); i++ {
		embeddedField := fieldType.Field(i)

		// Skip unexported fields
		if !embeddedField.IsExported() {
			continue
		}

		// Handle nested embedded structs recursively
		if embeddedField.Anonymous {
			headerTag := embeddedField.Tag.Get("header")
			if headerTag == "" {
				nestedParams := flattenEmbeddedHeaderParams(schemaGen, embeddedField)
				params = append(params, nestedParams...)
				continue
			}
		}

		// Get header tag
		headerTag := embeddedField.Tag.Get("header")
		if headerTag == "" || headerTag == "-" {
			continue
		}

		// Parse tag
		paramName, omitempty := parseTagWithOmitempty(headerTag)
		if paramName == "" {
			paramName = embeddedField.Name
		}

		// Generate schema for the field (use generateFieldSchema to support enum components)
		fieldSchema, err := schemaGen.generateFieldSchema(embeddedField)
		if err != nil {
			continue // Skip parameter on error
		}

		// Determine if required
		// Check for optional tag first (explicit opt-out), then required tag (explicit opt-in), then fall back to omitempty logic
		required := false
		if embeddedField.Tag.Get("optional") == "true" {
			required = false
		} else if embeddedField.Tag.Get("required") == "true" {
			required = true
		} else if !omitempty && embeddedField.Type.Kind() != reflect.Ptr {
			required = true
		}

		params = append(params, Parameter{
			Name:        paramName,
			In:          "header",
			Description: fieldSchema.Description,
			Required:    required,
			Schema:      fieldSchema,
		})
	}

	return params
}

// generateHeaderParamsFromStruct generates header parameters from a struct type.
func generateHeaderParamsFromStruct(schemaGen *schemaGenerator, structType any) []Parameter {
	rt := reflect.TypeOf(structType)
	if rt == nil {
		return nil
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return nil
	}

	var params []Parameter

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded/anonymous struct fields - flatten them
		if field.Anonymous {
			// Check if the embedded field has a header tag (which would mean it's not truly flattened)
			headerTag := field.Tag.Get("header")
			if headerTag == "" {
				// Recursively extract header params from embedded struct
				embeddedParams := flattenEmbeddedHeaderParams(schemaGen, field)
				params = append(params, embeddedParams...)
				continue
			}
		}

		// Get header tag
		headerTag := field.Tag.Get("header")
		if headerTag == "" || headerTag == "-" {
			continue
		}

		// Parse tag
		paramName, omitempty := parseTagWithOmitempty(headerTag)
		if paramName == "" {
			paramName = field.Name
		}

		// Generate schema for the field (use generateFieldSchema to support enum components)
		fieldSchema, err := schemaGen.generateFieldSchema(field)
		if err != nil {
			// Skip parameter on error (collision detected)
			continue
		}

		// Determine if required
		// Check for optional tag first (explicit opt-out), then required tag (explicit opt-in), then fall back to omitempty logic
		required := false
		if field.Tag.Get("optional") == "true" {
			required = false
		} else if field.Tag.Get("required") == "true" {
			required = true
		} else if !omitempty && field.Type.Kind() != reflect.Ptr {
			required = true
		}

		params = append(params, Parameter{
			Name:        paramName,
			In:          "header",
			Description: fieldSchema.Description,
			Required:    required,
			Schema:      fieldSchema,
		})
	}

	return params
}

// ConvertPathToOpenAPIFormat converts a path with :param style parameters
// to OpenAPI's {param} style format.
// e.g., /api/workspaces/:workspace_id/users -> /api/workspaces/{workspace_id}/users
func ConvertPathToOpenAPIFormat(path string) string {
	var result strings.Builder
	parts := strings.Split(path, "/")

	for i, part := range parts {
		if i > 0 {
			result.WriteString("/")
		}

		if after, ok := strings.CutPrefix(part, ":"); ok {
			// Convert :param to {param}
			result.WriteString("{")
			result.WriteString(after)
			result.WriteString("}")
		} else {
			result.WriteString(part)
		}
	}

	return result.String()
}

// parseTagWithOmitempty parses a struct tag and returns the name and omitempty flag.
func parseTagWithOmitempty(tag string) (name string, omitempty bool) {
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

// mergeParameters merges multiple parameter slices, removing duplicates by name and location.
func mergeParameters(paramSets ...[]Parameter) []Parameter {
	seen := make(map[string]bool)

	var result []Parameter

	for _, params := range paramSets {
		for _, param := range params {
			key := param.In + ":" + param.Name
			if !seen[key] {
				seen[key] = true

				result = append(result, param)
			}
		}
	}

	return result
}
