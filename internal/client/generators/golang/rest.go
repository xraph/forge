package golang

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// RESTGenerator generates REST client code
type RESTGenerator struct {
	typesGen *TypesGenerator
}

// NewRESTGenerator creates a new REST generator
func NewRESTGenerator() *RESTGenerator {
	return &RESTGenerator{
		typesGen: NewTypesGenerator(),
	}
}

// Generate generates the rest.go file
func (r *RESTGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	// Imports
	buf.WriteString("import (\n")
	buf.WriteString("\t\"bytes\"\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString("\t\"encoding/json\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\t\"io\"\n")
	buf.WriteString("\t\"net/http\"\n")
	buf.WriteString("\t\"net/url\"\n")
	buf.WriteString("\t\"strings\"\n")
	buf.WriteString(")\n\n")

	// Generate helper functions
	buf.WriteString(r.generateHelpers(config))

	// Generate endpoint methods
	for _, endpoint := range spec.Endpoints {
		methodCode := r.generateEndpointMethod(endpoint, spec, config)
		buf.WriteString(methodCode)
		buf.WriteString("\n")
	}

	return buf.String()
}

// generateHelpers generates helper functions
func (r *RESTGenerator) generateHelpers(config client.GeneratorConfig) string {
	var buf strings.Builder

	// doRequest helper
	buf.WriteString("// doRequest performs an HTTP request\n")
	buf.WriteString("func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {\n")
	buf.WriteString("\tvar reqBody io.Reader\n\n")
	buf.WriteString("\tif body != nil {\n")
	buf.WriteString("\t\tdata, err := json.Marshal(body)\n")
	buf.WriteString("\t\tif err != nil {\n")
	buf.WriteString("\t\t\treturn fmt.Errorf(\"marshal request body: %w\", err)\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t\treqBody = bytes.NewReader(data)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treq, err := http.NewRequestWithContext(ctx, method, c.buildURL(path), reqBody)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"create request: %w\", err)\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tif body != nil {\n")
	buf.WriteString("\t\treq.Header.Set(\"Content-Type\", \"application/json\")\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\treq.Header.Set(\"Accept\", \"application/json\")\n\n")

	buf.WriteString("\tc.addAuth(req)\n\n")

	buf.WriteString("\tresp, err := c.httpClient.Do(req)\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\treturn fmt.Errorf(\"do request: %w\", err)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\tdefer resp.Body.Close()\n\n")

	buf.WriteString("\tif resp.StatusCode < 200 || resp.StatusCode >= 300 {\n")
	buf.WriteString("\t\tbody, _ := io.ReadAll(resp.Body)\n")
	buf.WriteString("\t\treturn &APIError{\n")
	buf.WriteString("\t\t\tStatusCode: resp.StatusCode,\n")
	buf.WriteString("\t\t\tMessage:    string(body),\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\tif result != nil && resp.StatusCode != http.StatusNoContent {\n")
	buf.WriteString("\t\tif err := json.NewDecoder(resp.Body).Decode(result); err != nil {\n")
	buf.WriteString("\t\t\treturn fmt.Errorf(\"decode response: %w\", err)\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

// generateEndpointMethod generates a method for an endpoint
func (r *RESTGenerator) generateEndpointMethod(endpoint client.Endpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	methodName := r.generateMethodName(endpoint)

	// Generate method signature
	buf.WriteString(fmt.Sprintf("// %s %s\n", methodName, endpoint.Summary))
	if endpoint.Description != "" {
		buf.WriteString(fmt.Sprintf("// %s\n", endpoint.Description))
	}
	if endpoint.Deprecated {
		buf.WriteString("// Deprecated: This endpoint is deprecated\n")
	}

	// Determine parameters and return type
	params := r.generateParameters(endpoint, spec)
	returnType := r.generateReturnType(endpoint, spec)

	buf.WriteString(fmt.Sprintf("func (c *Client) %s(ctx context.Context", methodName))
	if params != "" {
		buf.WriteString(", " + params)
	}
	buf.WriteString(") ")

	if returnType != "" {
		buf.WriteString(fmt.Sprintf("(*%s, error) {\n", returnType))
	} else {
		buf.WriteString("error {\n")
	}

	// Method body

	// Build path with path parameters
	pathExpr := r.generatePathExpression(endpoint)
	buf.WriteString(fmt.Sprintf("\tpath := %s\n\n", pathExpr))

	// Add query parameters
	if len(endpoint.QueryParams) > 0 {
		buf.WriteString("\tq := url.Values{}\n")
		for _, param := range endpoint.QueryParams {
			paramName := r.toGoParamName(param.Name)
			if param.Required {
				buf.WriteString(fmt.Sprintf("\tq.Set(\"%s\", fmt.Sprint(%s))\n", param.Name, paramName))
			} else {
				buf.WriteString(fmt.Sprintf("\tif %s != nil {\n", paramName))
				buf.WriteString(fmt.Sprintf("\t\tq.Set(\"%s\", fmt.Sprint(*%s))\n", param.Name, paramName))
				buf.WriteString("\t}\n")
			}
		}
		buf.WriteString("\tif len(q) > 0 {\n")
		buf.WriteString("\t\tpath += \"?\" + q.Encode()\n")
		buf.WriteString("\t}\n\n")
	}

	// Determine request body
	var requestVar string
	if endpoint.RequestBody != nil && endpoint.RequestBody.Required {
		requestVar = "req"
	} else if endpoint.RequestBody != nil {
		requestVar = "req"
	}

	// Determine result variable
	var resultVar string
	if returnType != "" {
		resultVar = "&result"
		buf.WriteString(fmt.Sprintf("\tvar result %s\n", returnType))
	}

	// Make the request
	buf.WriteString(fmt.Sprintf("\terr := c.doRequest(ctx, \"%s\", path, %s, %s)\n",
		endpoint.Method,
		r.orNil(requestVar),
		r.orNil(resultVar)))

	buf.WriteString("\tif err != nil {\n")
	if returnType != "" {
		buf.WriteString("\t\treturn nil, err\n")
	} else {
		buf.WriteString("\t\treturn err\n")
	}
	buf.WriteString("\t}\n\n")

	if returnType != "" {
		buf.WriteString("\treturn &result, nil\n")
	} else {
		buf.WriteString("\treturn nil\n")
	}

	buf.WriteString("}\n")

	return buf.String()
}

// generateMethodName generates a method name from an endpoint
func (r *RESTGenerator) generateMethodName(endpoint client.Endpoint) string {
	if endpoint.OperationID != "" {
		return r.typesGen.toGoFieldName(endpoint.OperationID)
	}

	// Generate from path and method
	path := strings.TrimPrefix(endpoint.Path, "/")
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.ReplaceAll(path, "-", "_")

	method := strings.Title(strings.ToLower(endpoint.Method))
	return method + r.typesGen.toGoFieldName(path)
}

// generateParameters generates method parameters
func (r *RESTGenerator) generateParameters(endpoint client.Endpoint, spec *client.APISpec) string {
	var params []string

	// Path parameters
	for _, param := range endpoint.PathParams {
		paramName := r.toGoParamName(param.Name)
		goType := r.typesGen.schemaToGoType(param.Schema, spec)
		params = append(params, fmt.Sprintf("%s %s", paramName, goType))
	}

	// Query parameters
	for _, param := range endpoint.QueryParams {
		paramName := r.toGoParamName(param.Name)
		goType := r.typesGen.schemaToGoType(param.Schema, spec)
		if !param.Required {
			goType = "*" + goType
		}
		params = append(params, fmt.Sprintf("%s %s", paramName, goType))
	}

	// Request body
	if endpoint.RequestBody != nil {
		// Get the JSON content type schema
		if media, ok := endpoint.RequestBody.Content["application/json"]; ok && media.Schema != nil {
			typeName := r.typesGen.generateRequestTypeName(endpoint)
			if r.typesGen.isSchemaRef(media.Schema) {
				typeName = r.typesGen.extractRefName(media.Schema.Ref)
			}

			if endpoint.RequestBody.Required {
				params = append(params, fmt.Sprintf("req %s", typeName))
			} else {
				params = append(params, fmt.Sprintf("req *%s", typeName))
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
				if r.typesGen.isSchemaRef(media.Schema) {
					return r.typesGen.extractRefName(media.Schema.Ref)
				}
				return r.typesGen.generateResponseTypeName(endpoint, statusCode)
			}
		}
	}

	return ""
}

// generatePathExpression generates the path expression with parameters
func (r *RESTGenerator) generatePathExpression(endpoint client.Endpoint) string {
	path := endpoint.Path

	// Replace path parameters
	for _, param := range endpoint.PathParams {
		paramName := r.toGoParamName(param.Name)
		placeholder := fmt.Sprintf("{%s}", param.Name)
		path = strings.ReplaceAll(path, placeholder, fmt.Sprintf("\" + fmt.Sprint(%s) + \"", paramName))
	}

	return fmt.Sprintf("\"%s\"", path)
}

// toGoParamName converts a parameter name to Go naming convention
func (r *RESTGenerator) toGoParamName(name string) string {
	// Convert to camelCase
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-'
	})

	if len(parts) == 0 {
		return name
	}

	result := strings.ToLower(parts[0])
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			result += strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}

	return result
}

// orNil returns the variable name or "nil"
func (r *RESTGenerator) orNil(varName string) string {
	if varName == "" {
		return "nil"
	}
	return varName
}
