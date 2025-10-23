package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// RESTGenerator generates TypeScript REST client code
type RESTGenerator struct{}

// NewRESTGenerator creates a new REST generator
func NewRESTGenerator() *RESTGenerator {
	return &RESTGenerator{}
}

// Generate generates the REST client methods
func (r *RESTGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("import { AxiosRequestConfig } from 'axios';\n")
	buf.WriteString("import { Client } from './client';\n")
	buf.WriteString("import * as types from './types';\n\n")

	// Extend the main client class
	buf.WriteString("export class RESTClient extends Client {\n")

	// Generate method for each endpoint
	for _, endpoint := range spec.Endpoints {
		methodCode := r.generateEndpointMethod(endpoint, spec)
		buf.WriteString(methodCode)
		buf.WriteString("\n")
	}

	buf.WriteString("}\n")

	return buf.String()
}

// generateEndpointMethod generates a TypeScript method for an endpoint
func (r *RESTGenerator) generateEndpointMethod(endpoint client.Endpoint, spec *client.APISpec) string {
	var buf strings.Builder

	methodName := r.generateMethodName(endpoint)

	// JSDoc comment
	buf.WriteString("  /**\n")
	if endpoint.Summary != "" {
		buf.WriteString(fmt.Sprintf("   * %s\n", endpoint.Summary))
	}
	if endpoint.Description != "" {
		buf.WriteString(fmt.Sprintf("   * %s\n", endpoint.Description))
	}
	if endpoint.Deprecated {
		buf.WriteString("   * @deprecated\n")
	}
	buf.WriteString("   */\n")

	// Method signature
	params := r.generateParameters(endpoint, spec)
	returnType := r.generateReturnType(endpoint, spec)

	buf.WriteString(fmt.Sprintf("  async %s(%s): Promise<%s> {\n", methodName, params, returnType))

	// Build URL
	pathExpr := r.generatePathExpression(endpoint)
	buf.WriteString(fmt.Sprintf("    let path = %s;\n", pathExpr))

	// Add query parameters
	if len(endpoint.QueryParams) > 0 {
		buf.WriteString("    const queryParams: Record<string, any> = {};\n")
		for _, param := range endpoint.QueryParams {
			paramName := r.toTSParamName(param.Name)
			if param.Required {
				buf.WriteString(fmt.Sprintf("    queryParams['%s'] = %s;\n", param.Name, paramName))
			} else {
				buf.WriteString(fmt.Sprintf("    if (%s !== undefined) {\n", paramName))
				buf.WriteString(fmt.Sprintf("      queryParams['%s'] = %s;\n", param.Name, paramName))
				buf.WriteString("    }\n")
			}
		}
		buf.WriteString("    const queryString = new URLSearchParams(queryParams).toString();\n")
		buf.WriteString("    if (queryString) {\n")
		buf.WriteString("      path += '?' + queryString;\n")
		buf.WriteString("    }\n")
	}

	// Build request config
	buf.WriteString("    const config: AxiosRequestConfig = {\n")
	buf.WriteString(fmt.Sprintf("      method: '%s',\n", strings.ToUpper(endpoint.Method)))
	buf.WriteString("      url: path,\n")

	// Add request body
	if endpoint.RequestBody != nil {
		buf.WriteString("      data: body,\n")
	}

	// Add custom headers
	if len(endpoint.HeaderParams) > 0 {
		buf.WriteString("      headers: {\n")
		for _, param := range endpoint.HeaderParams {
			paramName := r.toTSParamName(param.Name)
			if param.Required {
				buf.WriteString(fmt.Sprintf("        '%s': %s,\n", param.Name, paramName))
			} else {
				buf.WriteString(fmt.Sprintf("        ...(%s ? { '%s': %s } : {}),\n", paramName, param.Name, paramName))
			}
		}
		buf.WriteString("      },\n")
	}

	buf.WriteString("    };\n\n")

	// Make request
	if returnType != "void" {
		buf.WriteString("    return this.request<")
		buf.WriteString(returnType)
		buf.WriteString(">(config);\n")
	} else {
		buf.WriteString("    await this.request(config);\n")
	}

	buf.WriteString("  }\n")

	return buf.String()
}

// generateMethodName generates a method name from an endpoint
func (r *RESTGenerator) generateMethodName(endpoint client.Endpoint) string {
	if endpoint.OperationID != "" {
		return r.toCamelCase(endpoint.OperationID)
	}

	// Generate from path and method
	path := strings.TrimPrefix(endpoint.Path, "/")
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.ReplaceAll(path, "-", "_")

	method := strings.ToLower(endpoint.Method)
	return r.toCamelCase(method + "_" + path)
}

// generateParameters generates method parameters
func (r *RESTGenerator) generateParameters(endpoint client.Endpoint, spec *client.APISpec) string {
	var params []string

	// Path parameters
	for _, param := range endpoint.PathParams {
		paramName := r.toTSParamName(param.Name)
		tsType := r.schemaToTSType(param.Schema, spec)
		params = append(params, fmt.Sprintf("%s: %s", paramName, tsType))
	}

	// Query parameters
	for _, param := range endpoint.QueryParams {
		paramName := r.toTSParamName(param.Name)
		tsType := r.schemaToTSType(param.Schema, spec)
		if !param.Required {
			tsType += " | undefined"
		}
		params = append(params, fmt.Sprintf("%s?: %s", paramName, tsType))
	}

	// Header parameters
	for _, param := range endpoint.HeaderParams {
		paramName := r.toTSParamName(param.Name)
		tsType := r.schemaToTSType(param.Schema, spec)
		if !param.Required {
			tsType += " | undefined"
		}
		params = append(params, fmt.Sprintf("%s?: %s", paramName, tsType))
	}

	// Request body
	if endpoint.RequestBody != nil {
		if media, ok := endpoint.RequestBody.Content["application/json"]; ok && media.Schema != nil {
			typeName := r.getSchemaTypeName(media.Schema, spec)
			if endpoint.RequestBody.Required {
				params = append(params, fmt.Sprintf("body: %s", typeName))
			} else {
				params = append(params, fmt.Sprintf("body?: %s", typeName))
			}
		}
	}

	return strings.Join(params, ", ")
}

// generateReturnType generates the return type for an endpoint
func (r *RESTGenerator) generateReturnType(endpoint client.Endpoint, spec *client.APISpec) string {
	// Look for 200 or 201 response
	for _, statusCode := range []int{200, 201} {
		if resp, ok := endpoint.Responses[statusCode]; ok {
			if media, ok := resp.Content["application/json"]; ok && media.Schema != nil {
				return r.getSchemaTypeName(media.Schema, spec)
			}
		}
	}

	// Check for 204 No Content
	if _, ok := endpoint.Responses[204]; ok {
		return "void"
	}

	return "any"
}

// generatePathExpression generates the path expression with parameters
func (r *RESTGenerator) generatePathExpression(endpoint client.Endpoint) string {
	path := endpoint.Path

	// Replace path parameters with template literals
	for _, param := range endpoint.PathParams {
		paramName := r.toTSParamName(param.Name)
		placeholder := fmt.Sprintf("{%s}", param.Name)
		path = strings.ReplaceAll(path, placeholder, "${"+paramName+"}")
	}

	return fmt.Sprintf("`%s`", path)
}

// getSchemaTypeName gets the type name for a schema
func (r *RESTGenerator) getSchemaTypeName(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "any"
	}

	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")
		return parts[len(parts)-1]
	}

	return r.schemaToTSType(schema, spec)
}

// schemaToTSType converts a schema to TypeScript type
func (r *RESTGenerator) schemaToTSType(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "any"
	}

	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")
		return "types." + parts[len(parts)-1]
	}

	switch schema.Type {
	case "string":
		if len(schema.Enum) > 0 {
			var values []string
			for _, v := range schema.Enum {
				values = append(values, fmt.Sprintf("'%v'", v))
			}
			return strings.Join(values, " | ")
		}
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if schema.Items != nil {
			return r.schemaToTSType(schema.Items, spec) + "[]"
		}
		return "any[]"
	case "object":
		return "Record<string, any>"
	}

	return "any"
}

// toTSParamName converts a parameter name to TypeScript naming convention (camelCase)
func (r *RESTGenerator) toTSParamName(name string) string {
	return r.toCamelCase(name)
}

// toCamelCase converts a string to camelCase
func (r *RESTGenerator) toCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	if len(parts) == 0 {
		return s
	}

	result := strings.ToLower(parts[0])
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			result += strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
		}
	}

	return result
}
