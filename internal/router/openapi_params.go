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

// paramFromField builds one OpenAPI parameter from a struct field.
//
// This is the single place a parameter is derived from a field. There used to
// be two: the struct-walking helpers in this file resolved a field's schema
// through generateFieldSchema -- the same entry point a struct property uses,
// which is what turns an enum type into a $ref at its component -- while the
// per-field helpers on the unified WithRequestSchema path called
// generateSchemaFromType directly and got a bare `type: string` back. The same
// enum type therefore documented its four permitted values as a property and
// silently accepted any string as a query parameter. Routing both through here
// means a fix to enum, component or requiredness resolution cannot reach one
// path and miss the other.
//
// `in` is the parameter location. Path parameters are required by definition;
// every other location takes its requiredness from the field's tags.
func paramFromField(schemaGen *schemaGenerator, field reflect.StructField, in, tagValue string) (Parameter, error) {
	paramName, omitempty := parseTagWithOmitempty(tagValue)
	if paramName == "" {
		paramName = field.Name
	}

	fieldSchema, err := schemaGen.generateFieldSchema(field)
	if err != nil {
		return Parameter{}, err
	}

	return Parameter{
		Name:        paramName,
		In:          in,
		Description: fieldSchema.Description,
		Required:    in == "path" || paramRequiredFromTags(field, omitempty),
		Schema:      fieldSchema,
	}, nil
}

// paramRequiredFromTags reports whether a non-path parameter must be supplied.
//
// Tag priority: an explicit optional opt-out, then an explicit required opt-in,
// then a default value (which makes the parameter implicitly optional), and
// finally the omitempty/pointer fallback.
func paramRequiredFromTags(field reflect.StructField, omitempty bool) bool {
	switch {
	case field.Tag.Get("optional") == "true":
		return false
	case field.Tag.Get("required") == "true":
		return true
	case field.Tag.Get("default") != "":
		return false
	default:
		return !omitempty && field.Type.Kind() != reflect.Ptr
	}
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

	for i := range rt.NumField() {
		field := rt.Field(i)

		// Skip unexported fields, but descend into anonymous ones so an embedded
		// lowercase-named struct still promotes its exported fields.
		if skipStructField(field) {
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

		param, err := paramFromField(schemaGen, field, "query", queryTag)
		if err != nil {
			// Skip parameter on error (collision detected)
			continue
		}

		params = append(params, param)
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
	for i := range fieldType.NumField() {
		embeddedField := fieldType.Field(i)

		// Skip unexported fields, but descend into anonymous ones so an embedded
		// lowercase-named struct still promotes its exported fields.
		if skipStructField(embeddedField) {
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

		param, err := paramFromField(schemaGen, embeddedField, "query", queryTag)
		if err != nil {
			continue // Skip parameter on error
		}

		params = append(params, param)
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
	for i := range fieldType.NumField() {
		embeddedField := fieldType.Field(i)

		// Skip unexported fields, but descend into anonymous ones so an embedded
		// lowercase-named struct still promotes its exported fields.
		if skipStructField(embeddedField) {
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

		param, err := paramFromField(schemaGen, embeddedField, "header", headerTag)
		if err != nil {
			continue // Skip parameter on error
		}

		params = append(params, param)
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

	for i := range rt.NumField() {
		field := rt.Field(i)

		// Skip unexported fields, but descend into anonymous ones so an embedded
		// lowercase-named struct still promotes its exported fields.
		if skipStructField(field) {
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

		param, err := paramFromField(schemaGen, field, "header", headerTag)
		if err != nil {
			// Skip parameter on error (collision detected)
			continue
		}

		params = append(params, param)
	}

	return params
}

// ConvertPathToOpenAPIFormat converts a path with :param style parameters
// to OpenAPI's {param} style format.
// e.g., /api/workspaces/:workspace_id/users -> /api/workspaces/{workspace_id}/users.
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
