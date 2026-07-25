package typescript

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// RESTGenerator generates TypeScript REST client code.
type RESTGenerator struct{}

// NewRESTGenerator creates a new REST generator.
func NewRESTGenerator() *RESTGenerator {
	return &RESTGenerator{}
}

// EndpointNode represents a node in the endpoint tree for nested structure generation.
type EndpointNode struct {
	MethodName string                   // Leaf node: actual method name
	Endpoint   *client.Endpoint         // Leaf node: the endpoint data
	Children   map[string]*EndpointNode // Branch node: nested namespaces
	IsLeaf     bool                     // Whether this is a method or namespace
}

// buildEndpointTree groups endpoints by dot-separated namespaces.
func (r *RESTGenerator) buildEndpointTree(endpoints []client.Endpoint) *EndpointNode {
	root := &EndpointNode{Children: make(map[string]*EndpointNode)}

	for i := range endpoints {
		endpoint := &endpoints[i]

		opID := endpoint.OperationID
		if opID == "" {
			// Generate from path+method
			opID = r.generateOperationIDFromPath(*endpoint)
		}

		parts := strings.Split(opID, ".")
		r.insertIntoTree(root, parts, endpoint)
	}

	return root
}

// insertIntoTree recursively inserts an endpoint into the tree.
func (r *RESTGenerator) insertIntoTree(node *EndpointNode, parts []string, endpoint *client.Endpoint) {
	if len(parts) == 1 {
		// Leaf node - actual method
		name := parts[0]

		if existing := node.Children[name]; existing != nil && !existing.IsLeaf {
			// A namespace already occupies this name (e.g. "users.active.list"
			// was inserted before "users"). Keep the namespace and hang the
			// method inside it under its own name, rather than discarding the
			// subtree. This mirrors the leaf-then-branch conversion below, so
			// both insertion orders produce the same tree shape.
			existing.Children[name] = &EndpointNode{
				MethodName: name,
				Endpoint:   endpoint,
				IsLeaf:     true,
			}

			return
		}

		node.Children[name] = &EndpointNode{
			MethodName: name,
			Endpoint:   endpoint,
			IsLeaf:     true,
		}

		return
	}

	// Branch node - namespace
	namespace := parts[0]
	child := node.Children[namespace]

	if child == nil {
		// Create new branch node
		child = &EndpointNode{
			Children: make(map[string]*EndpointNode),
			IsLeaf:   false,
		}
		node.Children[namespace] = child
	} else if child.IsLeaf {
		// Convert leaf to branch - this handles cases where we have both
		// "users.list" and "users.active.list" - "users" needs to be both
		// a namespace and have a method
		existingEndpoint := child.Endpoint
		child.IsLeaf = false
		child.Endpoint = nil
		child.Children = make(map[string]*EndpointNode)

		// Re-insert the existing endpoint as a direct child
		// This preserves the original method at this level
		child.Children[child.MethodName] = &EndpointNode{
			MethodName: child.MethodName,
			Endpoint:   existingEndpoint,
			IsLeaf:     true,
		}
		child.MethodName = ""
	}

	r.insertIntoTree(child, parts[1:], endpoint)
}

// generateOperationIDFromPath creates an operation ID from path and method.
func (r *RESTGenerator) generateOperationIDFromPath(endpoint client.Endpoint) string {
	path := strings.TrimPrefix(endpoint.Path, "/")
	path = strings.ReplaceAll(path, "/", ".")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.ReplaceAll(path, "-", "")

	method := strings.ToLower(endpoint.Method)

	return method + "." + path
}

// Generate generates the REST client methods.
func (r *RESTGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	base := config.APIName
	if base == "" {
		base = "Client"
	}

	buf.WriteString("import { RequestConfig } from './fetch';\n")
	fmt.Fprintf(&buf, "import { %s } from './client';\n", base)
	buf.WriteString("import * as types from './types';\n\n")

	// Extend the main client class
	fmt.Fprintf(&buf, "export class RESTClient extends %s {\n", base)

	// Build endpoint tree from all endpoints
	tree := r.buildEndpointTree(spec.Endpoints)

	// Generate nested properties from tree
	r.generateTreeNode(&buf, tree, spec, config, 2, true)

	buf.WriteString("}\n")

	return buf.String()
}

// isValidTSIdentifier reports whether name can be used verbatim as an unquoted
// object-literal key or class member name.
func isValidTSIdentifier(name string) bool {
	if name == "" {
		return false
	}

	for i, c := range name {
		switch {
		case c == '_' || c == '$':
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}

	return true
}

// tsPropertyKey returns name quoted if it is not a valid TypeScript identifier,
// so keys like "platform-admin" or "3dtiles" emit as valid property names.
// Uses json.Marshal to properly escape quotes and backslashes.
func tsPropertyKey(name string) string {
	if isValidTSIdentifier(name) {
		return name
	}

	b, err := json.Marshal(name)
	if err != nil {
		return "'" + name + "'" // unreachable for strings; keep the old shape as a fallback
	}

	return string(b)
}

// generateTreeNode recursively generates TypeScript object literals.
func (r *RESTGenerator) generateTreeNode(buf *strings.Builder, node *EndpointNode, spec *client.APISpec, config client.GeneratorConfig, indent int, isRoot bool) {
	indentStr := strings.Repeat(" ", indent)

	// Sort keys for deterministic output
	var keys []string
	for name := range node.Children {
		keys = append(keys, name)
	}

	sort.Strings(keys)

	for _, name := range keys {
		child := node.Children[name]
		if child.IsLeaf {
			// Generate arrow function for leaf nodes (actual methods)
			r.generateArrowFunction(buf, name, child.Endpoint, spec, config, indent, isRoot)
		} else {
			// Generate nested object for branch nodes (namespaces)
			if isRoot {
				fmt.Fprintf(buf, "%spublic readonly %s = {\n", indentStr, tsPropertyKey(name))
			} else {
				fmt.Fprintf(buf, "%s%s: {\n", indentStr, tsPropertyKey(name))
			}

			r.generateTreeNode(buf, child, spec, config, indent+2, false)

			// Root members are class fields (terminated with ';'); nested
			// members are object-literal entries (terminated with ',').
			if isRoot {
				fmt.Fprintf(buf, "%s};\n\n", indentStr)
			} else {
				fmt.Fprintf(buf, "%s},\n\n", indentStr)
			}
		}
	}
}

// generateArrowFunction generates a single arrow function method.
func (r *RESTGenerator) generateArrowFunction(buf *strings.Builder, methodName string, endpoint *client.Endpoint, spec *client.APISpec, config client.GeneratorConfig, indent int, isRoot bool) {
	indentStr := strings.Repeat(" ", indent)

	// Generate JSDoc
	fmt.Fprintf(buf, "%s/**\n", indentStr)

	if endpoint.Summary != "" {
		fmt.Fprintf(buf, "%s * %s\n", indentStr, endpoint.Summary)
	}

	if endpoint.Description != "" {
		fmt.Fprintf(buf, "%s * %s\n", indentStr, endpoint.Description)
	}

	if endpoint.Deprecated {
		fmt.Fprintf(buf, "%s * @deprecated\n", indentStr)
	}

	fmt.Fprintf(buf, "%s */\n", indentStr)

	// Generate arrow function
	params := r.generateParameters(*endpoint, spec)
	if params != "" {
		params += ", "
	}

	params += "options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }"

	returnType := r.generateReturnType(*endpoint, spec)

	if isRoot {
		// Root level property
		fmt.Fprintf(buf, "%spublic readonly %s = async (%s): Promise<%s> => {\n",
			indentStr, tsPropertyKey(methodName), params, returnType)
	} else {
		// Nested property
		fmt.Fprintf(buf, "%s%s: async (%s): Promise<%s> => {\n",
			indentStr, tsPropertyKey(methodName), params, returnType)
	}

	// Generate method body (path, query params, request config)
	r.generateMethodBody(buf, endpoint, spec, indent+2)

	// Root members are class fields (terminated with ';'); nested members are
	// object-literal entries (terminated with ',').
	if isRoot {
		fmt.Fprintf(buf, "%s};\n\n", indentStr)
	} else {
		fmt.Fprintf(buf, "%s},\n\n", indentStr)
	}
}

// generateMethodBody generates the method implementation.
func (r *RESTGenerator) generateMethodBody(buf *strings.Builder, endpoint *client.Endpoint, spec *client.APISpec, indent int) {
	indentStr := strings.Repeat(" ", indent)

	// Build URL. The accumulator is prefixed with underscores so it never
	// collides with a request parameter that happens to be named "path".
	pathExpr := r.generatePathExpression(*endpoint)
	fmt.Fprintf(buf, "%slet __path = %s;\n", indentStr, pathExpr)

	// Add query parameters
	if len(endpoint.QueryParams) > 0 {
		fmt.Fprintf(buf, "%sconst queryParams: Record<string, any> = {};\n", indentStr)

		for _, param := range endpoint.QueryParams {
			paramName := r.toTSParamName(param.Name)
			if param.Required {
				fmt.Fprintf(buf, "%squeryParams['%s'] = %s;\n", indentStr, param.Name, paramName)
			} else {
				fmt.Fprintf(buf, "%sif (%s !== undefined) {\n", indentStr, paramName)
				fmt.Fprintf(buf, "%s  queryParams['%s'] = %s;\n", indentStr, param.Name, paramName)
				fmt.Fprintf(buf, "%s}\n", indentStr)
			}
		}

		fmt.Fprintf(buf, "%sconst queryString = new URLSearchParams(queryParams).toString();\n", indentStr)
		fmt.Fprintf(buf, "%sif (queryString) {\n", indentStr)
		fmt.Fprintf(buf, "%s  __path += '?' + queryString;\n", indentStr)
		fmt.Fprintf(buf, "%s}\n", indentStr)
	}

	// Build request config
	fmt.Fprintf(buf, "%sconst config: RequestConfig = {\n", indentStr)
	fmt.Fprintf(buf, "%s  method: '%s',\n", indentStr, strings.ToUpper(endpoint.Method))
	fmt.Fprintf(buf, "%s  url: __path,\n", indentStr)

	// Only forward a `body` when a matching `body` parameter was generated
	// (see generateParameters); otherwise the shorthand references nothing.
	if r.hasBodyParam(endpoint) {
		fmt.Fprintf(buf, "%s  body,\n", indentStr)
	}

	// Headers
	if len(endpoint.HeaderParams) > 0 {
		fmt.Fprintf(buf, "%s  headers: {\n", indentStr)

		for _, param := range endpoint.HeaderParams {
			paramName := r.toTSParamName(param.Name)
			if param.Required {
				fmt.Fprintf(buf, "%s    '%s': %s,\n", indentStr, param.Name, paramName)
			} else {
				fmt.Fprintf(buf, "%s    ...(%s ? { '%s': %s } : {}),\n", indentStr, paramName, param.Name, paramName)
			}
		}

		fmt.Fprintf(buf, "%s  },\n", indentStr)
	}

	fmt.Fprintf(buf, "%s  signal: options?.signal,\n", indentStr)
	fmt.Fprintf(buf, "%s  retry: options?.retry,\n", indentStr)
	fmt.Fprintf(buf, "%s};\n\n", indentStr)

	// Make request
	returnType := r.generateReturnType(*endpoint, spec)
	if returnType != "void" {
		fmt.Fprintf(buf, "%sreturn this.request<%s>(config);\n", indentStr, returnType)
	} else {
		fmt.Fprintf(buf, "%sawait this.request(config);\n", indentStr)
	}
}

// generateEndpointMethod generates a TypeScript method for an endpoint.
func (r *RESTGenerator) generateEndpointMethod(endpoint client.Endpoint, spec *client.APISpec, config client.GeneratorConfig) string {
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

	// Method signature with optional options parameter
	params := r.generateParameters(endpoint, spec)
	if params != "" {
		params += ", "
	}

	params += "options?: { signal?: AbortSignal; retry?: { maxAttempts?: number } }"

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
	buf.WriteString("    const config: RequestConfig = {\n")
	buf.WriteString(fmt.Sprintf("      method: '%s',\n", strings.ToUpper(endpoint.Method)))
	buf.WriteString("      url: path,\n")

	// Add request body
	if endpoint.RequestBody != nil {
		buf.WriteString("      body,\n")
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

	// Add signal and retry from options
	buf.WriteString("      signal: options?.signal,\n")
	buf.WriteString("      retry: options?.retry,\n")

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

// generateMethodName generates a method name from an endpoint.
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

// hasBodyParam reports whether the endpoint carries a JSON request body, which
// is the sole condition under which a `body` parameter is generated.
func (r *RESTGenerator) hasBodyParam(endpoint *client.Endpoint) bool {
	if endpoint.RequestBody == nil {
		return false
	}

	media, ok := endpoint.RequestBody.Content["application/json"]

	return ok && media.Schema != nil
}

// generateParameters generates method parameters. Query and header parameters
// are always emitted as optional, so required parameters (path params and a
// required body) are grouped first to avoid an optional-before-required
// signature, which is a TypeScript error.
func (r *RESTGenerator) generateParameters(endpoint client.Endpoint, spec *client.APISpec) string {
	var required []string
	var optional []string

	// Path parameters are always required.
	for _, param := range endpoint.PathParams {
		paramName := r.toTSParamName(param.Name)
		tsType := r.schemaToTSType(param.Schema, spec)
		required = append(required, fmt.Sprintf("%s: %s", paramName, tsType))
	}

	// Request body: a required body joins the required group (placed before the
	// optional query/header params); an optional body is appended last.
	var optionalBody string

	if r.hasBodyParam(&endpoint) {
		media := endpoint.RequestBody.Content["application/json"]
		typeName := r.getSchemaTypeName(media.Schema, spec)

		if endpoint.RequestBody.Required {
			required = append(required, "body: "+typeName)
		} else {
			optionalBody = "body?: " + typeName
		}
	}

	// Query parameters (always emitted optional).
	for _, param := range endpoint.QueryParams {
		paramName := r.toTSParamName(param.Name)

		tsType := r.schemaToTSType(param.Schema, spec)
		if !param.Required {
			tsType += " | undefined"
		}

		optional = append(optional, fmt.Sprintf("%s?: %s", paramName, tsType))
	}

	// Header parameters (always emitted optional).
	for _, param := range endpoint.HeaderParams {
		paramName := r.toTSParamName(param.Name)

		tsType := r.schemaToTSType(param.Schema, spec)
		if !param.Required {
			tsType += " | undefined"
		}

		optional = append(optional, fmt.Sprintf("%s?: %s", paramName, tsType))
	}

	if optionalBody != "" {
		optional = append(optional, optionalBody)
	}

	return strings.Join(append(required, optional...), ", ")
}

// generateReturnType generates the return type for an endpoint.
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

// generatePathExpression generates the path expression with parameters.
func (r *RESTGenerator) generatePathExpression(endpoint client.Endpoint) string {
	path := endpoint.Path

	// Replace path parameters with template literals
	for _, param := range endpoint.PathParams {
		paramName := r.toTSParamName(param.Name)
		placeholder := fmt.Sprintf("{%s}", param.Name)
		// Path segments must be escaped: an unencoded '/' or '?' in a value
		// silently changes which route the request reaches.
		path = strings.ReplaceAll(path, placeholder, "${encodeURIComponent(String("+paramName+"))}")
	}

	return fmt.Sprintf("`%s`", path)
}

// getSchemaTypeName gets the type name for a schema.
func (r *RESTGenerator) getSchemaTypeName(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "any"
	}

	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")

		// Types are imported under the `types` namespace (see the file header
		// `import * as types from './types'`), so referenced types must be
		// qualified to resolve.
		return "types." + parts[len(parts)-1]
	}

	return r.schemaToTSType(schema, spec)
}

// schemaToTSType converts a schema to TypeScript type.
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

// toTSParamName converts a parameter name to TypeScript naming convention (camelCase).
func (r *RESTGenerator) toTSParamName(name string) string {
	return r.toCamelCase(name)
}

// toCamelCase converts a string to camelCase.
func (r *RESTGenerator) toCamelCase(s string) string {
	return toCamel(s)
}
