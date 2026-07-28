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

	// json.Marshal never errors on a Go string: strings are always valid
	// UTF-8-marshalable values (invalid UTF-8 bytes are replaced, not rejected).
	b, _ := json.Marshal(name)

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

	returnType, _ := r.generateReturnType(*endpoint, spec)

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

	// Computed once up front: both the config object (which needs to tell
	// executeRequest whether an empty body is a legitimate outcome for this
	// specific endpoint) and the final discard-vs-forward decision below
	// depend on it.
	returnType, hasVoid := r.generateReturnType(*endpoint, spec)

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

	// Tell executeRequest whether an empty body is a legitimate outcome for
	// THIS endpoint specifically. executeRequest is one generic function
	// shared by every method, so it cannot infer this from the bytes alone —
	// an empty text/plain body and an empty octet-stream body are both
	// perfectly valid non-void payloads, and collapsing them to `undefined`
	// unconditionally would silently corrupt them for any endpoint that
	// never declared a no-content 2xx. Only set the flag (never emit it as
	// `false`) when the spec actually contributes a void member, i.e. when
	// generateReturnType found at least one 2xx response with no content.
	if hasVoid {
		fmt.Fprintf(buf, "%s  allowEmptyBody: true,\n", indentStr)
	}

	fmt.Fprintf(buf, "%s};\n\n", indentStr)

	// Make request.
	//
	// The comparison below is deliberately an exact match against the literal
	// string "void", not a "contains void" check on a union. Discarding is
	// correct only when NO caller could ever want the resolved value — true
	// for the exact-void case (every 2xx response is content-less, so
	// generateReturnType's dedupe collapses the whole union to "void"), but
	// not for a mixed union such as "types.User | void" (a 200-with-body
	// alongside a 202-with-none): there, a caller hitting the 200 path
	// legitimately wants the User back, so the value must be forwarded via
	// `return this.request<T>(config)`, not thrown away. This is safe
	// specifically because executeRequest's response parsing (fetch_client.go)
	// now resolves an empty body to a real `undefined` — but only when
	// `allowEmptyBody` says the spec actually allows that for this call — so
	// `types.User | void` callers who write `if (result) { result.id }` get a
	// guard that is actually meaningful, not a compiling lie.
	if returnType != "void" {
		fmt.Fprintf(buf, "%sreturn this.request<%s>(config);\n", indentStr, returnType)
	} else {
		fmt.Fprintf(buf, "%sawait this.request(config);\n", indentStr)
	}
}

// hasBodyParam reports whether the endpoint carries a usable request body —
// any content type requestBodyContentType can resolve to, not just
// "application/json" — which is the condition under which a `body` parameter
// is generated.
func (r *RESTGenerator) hasBodyParam(endpoint *client.Endpoint) bool {
	return r.requestBodyContentType(endpoint) != ""
}

// requestBodyContentType selects the single content type an endpoint's
// request body is generated for, following the precedence responseBodyType
// established for responses — application/json, then text/*, then anything
// else — with one deliberate difference. Here application/json only wins
// when it actually carries a schema; a schemaless entry is skipped, INCLUDING
// in the final fallback, because mapping it back to `any` would erase a
// sibling multipart/form-data or octet-stream body that the caller can
// actually use. responseBodyType has no equivalent hazard: its fallback
// returns a hard "Blob" and can never re-enter the JSON branch.
// RequestBody.Content is a map, so an endpoint
// COULD declare more than one content type (e.g. a spec offering both JSON
// and multipart upload for the same operation); a generated TypeScript
// method can only accept one shape for its `body` parameter, so exactly one
// content type must win, and it must win the same way every generation run
// picks it — hence sorting rather than ranging the map directly. Returns ""
// when there is no usable request body at all.
func (r *RESTGenerator) requestBodyContentType(endpoint *client.Endpoint) string {
	if endpoint.RequestBody == nil || len(endpoint.RequestBody.Content) == 0 {
		return ""
	}

	if media, ok := endpoint.RequestBody.Content["application/json"]; ok && media.Schema != nil {
		return "application/json"
	}

	for _, contentType := range sortedKeys(endpoint.RequestBody.Content) {
		if strings.HasPrefix(contentType, "text/") {
			return contentType
		}
	}

	// Fall back to the first remaining content type, skipping
	// "application/json" — reaching here means the JSON entry exists but
	// carries no schema (`content: {application/json: {}}` is legal
	// OpenAPI), so selecting it would map back to `any` via
	// requestBodyParamType and silently erase a perfectly good
	// multipart/form-data or application/octet-stream sibling. "a" sorts
	// first, so without this skip the schemaless JSON entry wins every
	// mixed-content body.
	keys := sortedKeys(endpoint.RequestBody.Content)
	for _, contentType := range keys {
		if contentType != "application/json" {
			return contentType
		}
	}

	// Schemaless application/json really was the only option.
	if len(keys) == 0 {
		return ""
	}

	return keys[0]
}

// requestBodyParamType maps the content type requestBodyContentType selected
// to the TypeScript type of the generated `body` parameter:
// application/json -> the declared schema's type (a real interface/type
// name, or a primitive, so callers get full type safety); multipart/form-data
// -> FormData, the DOM type a caller assembles an upload with (setting a
// Content-Type header for it manually — including a JSON default — breaks
// the request, since the browser/runtime computes the multipart boundary
// only when it sets the header itself; see fetch.ts's executeRequest);
// text/* -> string; anything else (e.g. application/octet-stream, an image
// type, or any other opaque binary content type) -> Blob, the DOM type for an
// opaque byte payload, mirroring responseBodyType's fallback for the same
// reason. Returns "" when hasBodyParam(endpoint) is false; callers must check
// that first.
func (r *RESTGenerator) requestBodyParamType(endpoint *client.Endpoint, spec *client.APISpec) string {
	contentType := r.requestBodyContentType(endpoint)

	switch {
	case contentType == "":
		return ""
	case contentType == "application/json":
		media := endpoint.RequestBody.Content[contentType]
		return r.getSchemaTypeName(media.Schema, spec)
	case contentType == "multipart/form-data":
		return "FormData"
	case contentType == "application/x-www-form-urlencoded":
		// URLSearchParams, not Blob: this is the idiomatic DOM type for
		// form-urlencoded, it is a native BodyInit fetch serialises itself
		// (and sets the matching Content-Type for), and it is the most
		// common non-JSON request body in practice. Handing the caller a
		// Blob here would make them hand-encode the pairs themselves.
		return "URLSearchParams"
	case strings.HasPrefix(contentType, "text/"):
		return "string"
	default:
		return "Blob"
	}
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
		typeName := r.requestBodyParamType(&endpoint, spec)

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

// generateReturnType generates the return type for an endpoint by unioning
// the body type of every declared 2xx response (not just 200/201 JSON), and
// reports whether that union includes a "void" member (i.e. whether the
// endpoint declares at least one no-content 2xx response, or none at all).
//
// The second return value exists so callers that need to know "can a
// legitimate response for this endpoint have no body" don't have to
// re-derive it by substring-searching the joined type string for "void" —
// a schema legitimately named something containing "void" (e.g. a type
// called "Avoidance") would false-positive a naive `strings.Contains`. This
// drives generateMethodBody's `allowEmptyBody` flag: fetch_client.go's
// executeRequest only converts an empty body to `undefined` when the spec
// actually declared a no-content 2xx for that call; otherwise an empty body
// is parsed as the type normally would (e.g. "" for text/plain, a
// zero-byte Blob for a binary body, a thrown SyntaxError for JSON) — see
// executeRequest's own comment for why unconditional empty-to-undefined
// conversion was wrong.
//
// Responses is a map[int]*client.Response, so the status codes are collected
// and sorted before iterating — ranging the map directly would make the
// union's member order (and therefore the generated file's bytes)
// non-deterministic across runs.
//
// Non-2xx responses (including Endpoint.DefaultError) are deliberately
// excluded: they describe the shape of a thrown error, not a resolved
// value — handleErrorResponse in fetch_client.go throws before the "Parse
// response" step ever runs, so a 4xx/5xx/default body never flows through
// the Promise<T> this function types.
func (r *RESTGenerator) generateReturnType(endpoint client.Endpoint, spec *client.APISpec) (string, bool) {
	codes := make([]int, 0, len(endpoint.Responses))

	for code := range endpoint.Responses {
		if code >= 200 && code < 300 {
			codes = append(codes, code)
		}
	}

	sort.Ints(codes)

	var types []string

	seen := make(map[string]bool, len(codes))
	hasVoid := false

	for _, code := range codes {
		t := r.responseBodyType(endpoint.Responses[code], spec)

		if t == "void" {
			hasVoid = true
		}

		if !seen[t] {
			seen[t] = true

			types = append(types, t)
		}
	}

	if len(types) == 0 {
		// No 2xx responses declared at all (e.g. only a default error). The
		// success shape is entirely unspecified, so treat an empty body as
		// legitimate rather than as something to parse against a type nobody
		// declared.
		return "void", true
	}

	return strings.Join(types, " | "), hasVoid
}

// responseBodyType maps a single response's declared content to a TypeScript
// type, honouring content-type precedence: application/json first (a schema
// resolves to a concrete type, or falls back through schemaToTSType — which
// is also where a "binary" format wins over a JSON content-type on a
// contradictory schema, matching the precedence formatTSType already
// establishes for object properties), then any text/* media type -> string,
// then any other media type -> Blob (the DOM type for an opaque body such as
// a file download). A response with no Content at all contributes "void".
func (r *RESTGenerator) responseBodyType(resp *client.Response, spec *client.APISpec) string {
	if resp == nil || len(resp.Content) == 0 {
		return "void"
	}

	if media, ok := resp.Content["application/json"]; ok && media.Schema != nil {
		return r.getSchemaTypeName(media.Schema, spec)
	}

	for _, contentType := range sortedKeys(resp.Content) {
		if strings.HasPrefix(contentType, "text/") {
			return "string"
		}
	}

	return "Blob"
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

	// Enum wins over format for the same reason as generator.go's
	// schemaToTSType: the enum lists the exact permitted literal values,
	// which is more specific than a format hint on the base type.
	//
	// NOTE: unlike generator.go's schemaToTSType, this implementation does not
	// handle schema.Nullable anywhere (pre-existing behaviour, not addressed
	// here), so neither the enum branch below nor the format branch appends
	// " | null".
	if et := enumTSType(schema); et != "" {
		return et
	}

	if ft := formatTSType(schema); ft != "" {
		return ft
	}

	switch schema.Type {
	case "string":
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
