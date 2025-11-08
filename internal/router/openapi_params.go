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

		// Generate schema for the field
		fieldSchema := schemaGen.generateSchemaFromType(field.Type)

		// Apply struct tags
		schemaGen.applyStructTags(fieldSchema, field)

		// Determine if required
		required := field.Tag.Get("required") == "true"
		if !required && !omitempty && field.Type.Kind() != reflect.Ptr {
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

		// Generate schema for the field
		fieldSchema := schemaGen.generateSchemaFromType(field.Type)

		// Apply struct tags
		schemaGen.applyStructTags(fieldSchema, field)

		// Determine if required
		required := field.Tag.Get("required") == "true"
		if !required && !omitempty && field.Type.Kind() != reflect.Ptr {
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
