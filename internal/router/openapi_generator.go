package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// openAPIGenerator generates OpenAPI 3.1.0 specifications from a router
type openAPIGenerator struct {
	config    OpenAPIConfig
	router    Router
	container interface{} // DI container (optional)
	schemas   *schemaGenerator
}

// newOpenAPIGenerator creates a new OpenAPI generator
func newOpenAPIGenerator(config OpenAPIConfig, router Router, container interface{}) *openAPIGenerator {
	// Set defaults
	if config.OpenAPIVersion == "" {
		config.OpenAPIVersion = "3.1.0"
	}
	if config.UIPath == "" {
		config.UIPath = "/swagger"
	}
	if config.SpecPath == "" {
		config.SpecPath = "/openapi.json"
	}

	// Create components map that will be shared with schema generator
	// This allows the generator to register nested struct types as components
	componentsSchemas := make(map[string]*Schema)

	return &openAPIGenerator{
		config:    config,
		router:    router,
		container: container,
		schemas:   newSchemaGenerator(componentsSchemas),
	}
}

// Generate creates the complete OpenAPI specification
func (g *openAPIGenerator) Generate() *OpenAPISpec {
	spec := &OpenAPISpec{
		OpenAPI: g.config.OpenAPIVersion,
		Info: Info{
			Title:       g.config.Title,
			Description: g.config.Description,
			Version:     g.config.Version,
			Contact:     g.config.Contact,
			License:     g.config.License,
		},
		Servers: g.config.Servers,
		Paths:   make(map[string]*PathItem),
		Components: &Components{
			Schemas:         g.schemas.components, // Use the shared components map
			SecuritySchemes: g.config.Security,
		},
		Tags:         g.config.Tags,
		ExternalDocs: g.config.ExternalDocs,
	}

	// Add auth provider security schemes if auth extension is registered
	g.addAuthSecuritySchemes(spec)

	// Process all routes
	routes := g.router.Routes()
	for _, route := range routes {
		g.processRoute(spec, route)
	}

	// Process webhooks
	processWebhooks(spec, routes)

	return spec
}

// addAuthSecuritySchemes retrieves security schemes from the auth registry
// and adds them to the OpenAPI spec components
func (g *openAPIGenerator) addAuthSecuritySchemes(spec *OpenAPISpec) {
	if g.container == nil {
		return
	}

	// Try to get the auth registry from the container
	// We use reflection to avoid direct import of auth extension
	type registryGetter interface {
		Get(name string) (interface{}, error)
	}

	if getter, ok := g.container.(registryGetter); ok {
		// Try to get auth registry
		registryInterface, err := getter.Get("auth:registry")
		if err != nil {
			// Auth extension not registered, skip
			return
		}

		// Use type assertion to call OpenAPISchemes() method
		// The auth registry returns SecurityScheme types from shared package
		type authRegistry interface {
			OpenAPISchemes() map[string]SecurityScheme
		}

		if authReg, ok := registryInterface.(authRegistry); ok {
			schemes := authReg.OpenAPISchemes()

			if spec.Components == nil {
				spec.Components = &Components{
					SecuritySchemes: make(map[string]SecurityScheme),
				}
			}
			if spec.Components.SecuritySchemes == nil {
				spec.Components.SecuritySchemes = make(map[string]SecurityScheme)
			}

			// Merge auth provider security schemes
			for name, scheme := range schemes {
				spec.Components.SecuritySchemes[name] = scheme
			}
		}
	}
}

// processRoute converts a route to an OpenAPI operation
func (g *openAPIGenerator) processRoute(spec *OpenAPISpec, route RouteInfo) {
	// Get or create path item
	pathItem := spec.Paths[route.Path]
	if pathItem == nil {
		pathItem = &PathItem{}
		spec.Paths[route.Path] = pathItem
	}

	// Create operation
	operation := &Operation{
		Summary:     route.Summary,
		Description: route.Description,
		OperationID: route.Name,
		Tags:        route.Tags,
		Responses:   make(map[string]*Response),
		Parameters:  []Parameter{},
	}

	// Check for unified request schema first
	if unifiedSchema, ok := route.Metadata["openapi.requestSchema.unified"]; ok {
		// Use unified extraction
		components := extractUnifiedRequestComponents(g.schemas, unifiedSchema)

		// Add parameters from unified schema
		operation.Parameters = append(operation.Parameters, components.PathParams...)
		operation.Parameters = append(operation.Parameters, components.QueryParams...)
		operation.Parameters = append(operation.Parameters, components.HeaderParams...)

		// Add body schema if present
		if components.HasBody && components.BodySchema != nil {
			operation.RequestBody = g.buildRequestBody(spec, components.BodySchema, route.Metadata, components.IsMultipart)
		}
	} else {
		// Legacy approach - separate extraction
		// Extract path parameters
		pathParams := g.extractPathParameters(route.Path, route.Metadata)
		operation.Parameters = append(operation.Parameters, pathParams...)

		// Extract query parameters
		queryParams := g.extractQueryParameters(route.Metadata)
		operation.Parameters = append(operation.Parameters, queryParams...)

		// Extract header parameters
		headerParams := g.extractHeaderParameters(route.Metadata)
		operation.Parameters = append(operation.Parameters, headerParams...)

		// Process request body schema
		if requestBody := g.extractRequestSchema(spec, route); requestBody != nil {
			operation.RequestBody = requestBody
		}
	}

	// Process response schemas
	g.extractResponseSchemas(spec, operation, route)

	// Process security requirements
	g.processSecurityRequirements(operation, route.Metadata)

	// Process deprecation
	if route.Metadata != nil {
		if deprecated, ok := route.Metadata["deprecated"].(bool); ok && deprecated {
			operation.Deprecated = deprecated
		}
	}

	// Process callbacks
	processCallbacks(operation, route.Metadata)

	// Add default 200 response if none specified
	if len(operation.Responses) == 0 {
		operation.Responses["200"] = &Response{
			Description: "Success",
		}
	}

	// Set operation on path item based on method
	g.setOperation(pathItem, route.Method, operation)
}

// setOperation sets an operation on a path item based on HTTP method
func (g *openAPIGenerator) setOperation(pathItem *PathItem, method string, operation *Operation) {
	switch strings.ToUpper(method) {
	case "GET":
		pathItem.Get = operation
	case "POST":
		pathItem.Post = operation
	case "PUT":
		pathItem.Put = operation
	case "DELETE":
		pathItem.Delete = operation
	case "PATCH":
		pathItem.Patch = operation
	case "OPTIONS":
		pathItem.Options = operation
	case "HEAD":
		pathItem.Head = operation
	}
}

// RegisterEndpoints registers OpenAPI spec and Swagger UI endpoints
func (g *openAPIGenerator) RegisterEndpoints() {
	// Register spec endpoint
	if g.config.SpecEnabled {
		// nolint:gosec // G104: Router registration errors are not possible here
		g.router.GET(g.config.SpecPath, g.specHandler())
	}

	// Register Swagger UI endpoint
	if g.config.UIEnabled {
		// nolint:gosec // G104: Router registration errors are not possible here
		g.router.GET(g.config.UIPath, g.uiHandler())
	}
}

// specHandler returns the OpenAPI spec as JSON
func (g *openAPIGenerator) specHandler() interface{} {
	return func(ctx Context) error {
		spec := g.Generate()

		if g.config.PrettyJSON {
			return ctx.JSON(http.StatusOK, spec)
		}

		// Compact JSON
		data, err := json.Marshal(spec)
		if err != nil {
			return err
		}

		ctx.Response().Header().Set("Content-Type", "application/json")
		// nolint:gosec // G104: Response write errors are handled by the framework
		ctx.Response().Write(data)
		return nil
	}
}

// uiHandler returns the Swagger UI HTML
func (g *openAPIGenerator) uiHandler() interface{} {
	return func(ctx Context) error {
		html := g.generateSwaggerHTML()
		ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		// nolint:gosec // G104: Response write errors are handled by the framework
		ctx.Response().Write([]byte(html))
		return nil
	}
}

// generateSwaggerHTML generates the Swagger UI HTML
func (g *openAPIGenerator) generateSwaggerHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>` + g.config.Title + ` - API Documentation</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@latest/swagger-ui.css" />
    <style>
        body {
            margin: 0;
            padding: 0;
        }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@latest/swagger-ui-bundle.js"></script>
    <script src="https://unpkg.com/swagger-ui-dist@latest/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            window.ui = SwaggerUIBundle({
                url: "` + g.config.SpecPath + `",
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout",
                defaultModelsExpandDepth: 1,
                defaultModelExpandDepth: 1,
                displayRequestDuration: true,
                filter: true,
                tryItOutEnabled: true
            });
        };
    </script>
</body>
</html>`
}

// buildRequestBody builds a RequestBody from a schema
func (g *openAPIGenerator) buildRequestBody(spec *OpenAPISpec, schema *Schema, metadata map[string]any, isMultipart bool) *RequestBody {
	// Get content types
	var contentTypes []string
	if types, ok := metadata["openapi.requestContentTypes"].([]string); ok {
		contentTypes = types
	} else {
		// Auto-detect content type based on schema
		if isMultipart {
			contentTypes = []string{"multipart/form-data"}
		} else {
			contentTypes = []string{"application/json"}
		}
	}

	// Build request body
	requestBody := &RequestBody{
		Description: "Request body",
		Required:    true,
		Content:     make(map[string]*MediaType),
	}

	// Get examples if specified
	var examples map[string]*Example
	if examplesData, ok := metadata["openapi.requestExamples"].(map[string]interface{}); ok {
		examples = make(map[string]*Example)
		for name, exampleValue := range examplesData {
			examples[name] = &Example{
				Summary: name,
				Value:   exampleValue,
			}
		}
	}

	for _, contentType := range contentTypes {
		mediaType := &MediaType{
			Schema: schema,
		}
		if examples != nil {
			mediaType.Examples = examples
		}

		// Add encoding for multipart/form-data
		if contentType == "multipart/form-data" && schema != nil && schema.Properties != nil {
			encoding := make(map[string]*Encoding)
			for propName, propSchema := range schema.Properties {
				if propSchema.Format == "binary" {
					encoding[propName] = &Encoding{
						ContentType: "application/octet-stream",
					}
				}
			}
			if len(encoding) > 0 {
				mediaType.Encoding = encoding
			}
		}

		requestBody.Content[contentType] = mediaType
	}

	return requestBody
}

// extractRequestSchema extracts request schema from route metadata
func (g *openAPIGenerator) extractRequestSchema(spec *OpenAPISpec, route RouteInfo) *RequestBody {
	if route.Metadata == nil {
		return nil
	}

	var schema *Schema
	var contentTypes []string

	// Check for manually specified schema
	if schemaVal, ok := route.Metadata["openapi.requestSchema"]; ok {
		if s, ok := schemaVal.(*Schema); ok {
			schema = s
		} else {
			// Generate from struct
			schema = g.schemas.GenerateSchema(schemaVal)
		}
	} else if reqType, ok := route.Metadata["openapi.requestType"]; ok {
		// Check for auto-detected request type from opinionated handler
		if rt, ok := reqType.(reflect.Type); ok {
			// Create a zero value of the type for schema generation
			instance := reflect.New(rt).Interface()
			schema = g.schemas.GenerateSchema(instance)

			// Store in components for reuse
			typeName := GetTypeName(rt)
			if typeName != "" && spec.Components != nil {
				spec.Components.Schemas[typeName] = schema
				// Use reference instead
				schema = &Schema{
					Ref: "#/components/schemas/" + typeName,
				}
			}
		}
	}

	if schema == nil {
		return nil
	}

	// Apply discriminator if specified
	if discriminator, ok := route.Metadata["openapi.discriminator"].(DiscriminatorConfig); ok && schema.Ref == "" {
		schema.Discriminator = &Discriminator{
			PropertyName: discriminator.PropertyName,
			Mapping:      discriminator.Mapping,
		}
	}

	// Get content types
	if types, ok := route.Metadata["openapi.requestContentTypes"].([]string); ok {
		contentTypes = types
	} else {
		contentTypes = []string{"application/json"}
	}

	// Build request body
	requestBody := &RequestBody{
		Description: "Request body",
		Required:    true,
		Content:     make(map[string]*MediaType),
	}

	// Get examples if specified
	var examples map[string]*Example
	if examplesData, ok := route.Metadata["openapi.requestExamples"].(map[string]interface{}); ok {
		examples = make(map[string]*Example)
		for name, exampleValue := range examplesData {
			examples[name] = &Example{
				Summary: name,
				Value:   exampleValue,
			}
		}
	}

	for _, contentType := range contentTypes {
		mediaType := &MediaType{
			Schema: schema,
		}
		if examples != nil {
			mediaType.Examples = examples
		}
		requestBody.Content[contentType] = mediaType
	}

	return requestBody
}

// extractResponseSchemas processes response schemas from route metadata
func (g *openAPIGenerator) extractResponseSchemas(spec *OpenAPISpec, operation *Operation, route RouteInfo) {
	if route.Metadata == nil {
		return
	}

	// Get content types
	contentTypes := []string{"application/json"}
	if types, ok := route.Metadata["openapi.responseContentTypes"].([]string); ok {
		contentTypes = types
	}

	// Get response examples if specified
	var responseExamples map[int]map[string]*Example
	if examplesData, ok := route.Metadata["openapi.responseExamples"].(map[int]map[string]interface{}); ok {
		responseExamples = make(map[int]map[string]*Example)
		for statusCode, examples := range examplesData {
			responseExamples[statusCode] = make(map[string]*Example)
			for name, exampleValue := range examples {
				responseExamples[statusCode][name] = &Example{
					Summary: name,
					Value:   exampleValue,
				}
			}
		}
	}

	// Check for manually specified response schemas
	if responseSchemas, ok := route.Metadata["openapi.responseSchemas"].(map[int]*ResponseSchemaDef); ok {
		for statusCode, respDef := range responseSchemas {
			var schema *Schema

			if s, ok := respDef.Schema.(*Schema); ok {
				schema = s
			} else if respDef.Schema != nil {
				schema = g.schemas.GenerateSchema(respDef.Schema)

				// Try to store in components if it's a struct type
				if rt := reflect.TypeOf(respDef.Schema); rt != nil {
					if rt.Kind() == reflect.Ptr {
						rt = rt.Elem()
					}
					if rt.Kind() == reflect.Struct {
						typeName := GetTypeName(rt)
						if typeName != "" && spec.Components != nil {
							spec.Components.Schemas[typeName] = schema
							schema = &Schema{
								Ref: "#/components/schemas/" + typeName,
							}
						}
					}
				}
			}

			content := make(map[string]*MediaType)
			for _, contentType := range contentTypes {
				mediaType := &MediaType{
					Schema: schema,
				}
				// Add examples if available for this status code
				if examples, ok := responseExamples[statusCode]; ok {
					mediaType.Examples = examples
				}
				content[contentType] = mediaType
			}

			operation.Responses[fmt.Sprintf("%d", statusCode)] = &Response{
				Description: respDef.Description,
				Content:     content,
			}
		}
		return
	}

	// Check for auto-detected response type from opinionated handler
	if respType, ok := route.Metadata["openapi.responseType"]; ok {
		if rt, ok := respType.(reflect.Type); ok {
			// Create a zero value of the type for schema generation
			instance := reflect.New(rt).Interface()
			schema := g.schemas.GenerateSchema(instance)

			// Store in components for reuse
			typeName := GetTypeName(rt)
			if typeName != "" && spec.Components != nil {
				spec.Components.Schemas[typeName] = schema
				schema = &Schema{
					Ref: "#/components/schemas/" + typeName,
				}
			}

			content := make(map[string]*MediaType)
			for _, contentType := range contentTypes {
				mediaType := &MediaType{
					Schema: schema,
				}
				// Add examples if available for status 200
				if examples, ok := responseExamples[200]; ok {
					mediaType.Examples = examples
				}
				content[contentType] = mediaType
			}

			operation.Responses["200"] = &Response{
				Description: "Success",
				Content:     content,
			}
		}
	}
}

// extractPathParameters parses path parameters from the path string
func (g *openAPIGenerator) extractPathParameters(path string, metadata map[string]any) []Parameter {
	pathParams := extractPathParamsFromPath(path)
	return convertPathParamsToOpenAPIParams(pathParams)
}

// extractQueryParameters extracts query parameters from metadata
func (g *openAPIGenerator) extractQueryParameters(metadata map[string]any) []Parameter {
	if metadata == nil {
		return nil
	}

	// Check for query schema
	querySchema, ok := metadata["openapi.querySchema"]
	if !ok {
		return nil
	}

	return generateQueryParamsFromStruct(g.schemas, querySchema)
}

// extractHeaderParameters extracts header parameters from metadata
func (g *openAPIGenerator) extractHeaderParameters(metadata map[string]any) []Parameter {
	if metadata == nil {
		return nil
	}

	// Check for header schema
	headerSchema, ok := metadata["openapi.headerSchema"]
	if !ok {
		return nil
	}

	return generateHeaderParamsFromStruct(g.schemas, headerSchema)
}

// processSecurityRequirements adds security requirements to operation
func (g *openAPIGenerator) processSecurityRequirements(operation *Operation, metadata map[string]any) {
	if metadata == nil {
		return
	}

	// Handle legacy "security" metadata (deprecated)
	if schemes, ok := metadata["security"].([]string); ok && len(schemes) > 0 {
		operation.Security = make([]SecurityRequirement, len(schemes))
		for i, scheme := range schemes {
			operation.Security[i] = SecurityRequirement{
				scheme: []string{},
			}
		}
		return
	}

	// Handle new auth.providers metadata
	providers, hasProviders := metadata["auth.providers"].([]string)
	if !hasProviders || len(providers) == 0 {
		return
	}

	scopes, _ := metadata["auth.scopes"].([]string)
	mode, _ := metadata["auth.mode"].(string)

	// Initialize security requirements
	operation.Security = []SecurityRequirement{}

	if mode == "and" {
		// AND mode: all providers in a single security requirement (rare in OpenAPI)
		// OpenAPI treats multiple schemes in one requirement as AND
		req := SecurityRequirement{}
		for _, provider := range providers {
			if scopes != nil && len(scopes) > 0 {
				req[provider] = scopes
			} else {
				req[provider] = []string{}
			}
		}
		operation.Security = append(operation.Security, req)
	} else {
		// OR mode (default): each provider as separate security requirement
		// OpenAPI treats multiple security requirements as OR
		for _, provider := range providers {
			req := SecurityRequirement{}
			if scopes != nil && len(scopes) > 0 {
				req[provider] = scopes
			} else {
				req[provider] = []string{}
			}
			operation.Security = append(operation.Security, req)
		}
	}
}
