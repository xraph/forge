package router

import (
	"reflect"
	"strings"

	"github.com/xraph/forge/internal/pathspec"
)

// PathParam represents a parsed path parameter.
type PathParam struct {
	Name        string
	Description string
	Schema      *Schema
}

// extractPathParamsFromPath parses path parameters from a route path.
//
// It reads the parsed Pattern rather than splitting the string again, which is
// why wildcard segments now appear (they never did before) and why a
// constrained parameter gets a matching schema instead of a bare string.
func extractPathParamsFromPath(path string) []PathParam {
	pattern, err := pathspec.Parse(path)
	if err != nil {
		return nil
	}

	var params []PathParam

	for _, seg := range pattern.Segments {
		if seg.Kind == pathspec.KindStatic {
			continue
		}

		params = append(params, PathParam{
			Name:        seg.Name,
			Description: "Path parameter: " + seg.Name,
			Schema:      schemaForConstraint(seg),
		})
	}

	return params
}

// schemaForConstraint maps the closed constraint vocabulary onto OpenAPI
// types. uuid and alpha stay strings: "format: uuid" would be a lie for a
// parameter forge only checks the shape of.
func schemaForConstraint(seg pathspec.Segment) *Schema {
	switch seg.Constraint {
	case pathspec.ConstraintInt, pathspec.ConstraintUint:
		return &Schema{Type: "integer"}

	case pathspec.ConstraintEnum:
		values := make([]any, len(seg.Enum))
		for i, v := range seg.Enum {
			values[i] = v
		}

		return &Schema{Type: "string", Enum: values}

	default:
		return &Schema{Type: "string"}
	}
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
	pattern, err := pathspec.Parse(path)
	if err != nil {
		return path
	}

	return pattern.Render(pathspec.SyntaxOpenAPI)
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
