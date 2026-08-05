package router

import (
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// openAPIGenerator generates OpenAPI 3.1.0 specifications from a router.
type openAPIGenerator struct {
	config      OpenAPIConfig
	router      Router
	container   any    // DI container (optional)
	httpAddress string // HTTP server address for automatic localhost server
	schemas     *schemaGenerator

	// mu serialises generation and guards the cache below.
	//
	// Building a document writes into the schema generator's maps -- the
	// component map, the $ref index, the name registry -- all of which are
	// shared across calls. Two callers at once therefore raced on plain Go
	// maps, and a concurrent map write is a fatal error, not a panic: no
	// recover() sees it and the process dies. The spec is served over HTTP, so
	// two simultaneous requests for it were enough to take a server down.
	mu sync.Mutex
	// cached is the last document built, and cachedRev the route-table
	// revision it was built from. The spec is a pure function of the routes and
	// the config, so while the revision holds, the same document is still
	// correct -- and regenerating it per request was pure waste besides.
	cached    *OpenAPISpec
	cachedRev uint64
	cachedOK  bool
}

// newOpenAPIGenerator creates a new OpenAPI generator.
func newOpenAPIGenerator(config OpenAPIConfig, router Router, container any, httpAddress string) *openAPIGenerator {
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
		config:      config,
		router:      router,
		container:   container,
		httpAddress: httpAddress,
		schemas:     newSchemaGenerator(componentsSchemas, nil), // Logger will be set via setLogger if available
	}
}

// Generate returns the OpenAPI specification for the router's current routes.
//
// The returned document is shared between callers and must be treated as
// read-only; mutating it corrupts every other holder's copy.
func (g *openAPIGenerator) Generate() (*OpenAPISpec, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Read the revision before generating, not after. If a route is registered
	// while this call runs, the document may or may not include it, but it is
	// tagged with the older revision and so the next call rebuilds.
	rev, versioned := currentRouteRevision(g.router)

	if versioned && g.cachedOK && g.cachedRev == rev {
		return g.cached, nil
	}

	spec, err := g.generate()
	if err != nil {
		return nil, err
	}

	if versioned {
		g.cached, g.cachedRev, g.cachedOK = spec, rev, true
	}

	return spec, nil
}

// currentRouteRevision reports the router's route-table revision, and whether
// the router tracks one at all.
func currentRouteRevision(r Router) (uint64, bool) {
	if src, ok := r.(routeRevisionSource); ok {
		return src.routeRevision()
	}

	return 0, false
}

// generate builds the complete OpenAPI specification. The caller holds g.mu.
func (g *openAPIGenerator) generate() (*OpenAPISpec, error) {
	// Start a fresh document: drops the $ref bookkeeping from any previous
	// call while keeping the type registry, so names stay stable across calls.
	g.schemas.beginSpec()

	// Build servers list with automatic localhost server
	servers := g.buildServers()

	spec := &OpenAPISpec{
		OpenAPI: g.config.OpenAPIVersion,
		Info: Info{
			Title:       g.config.Title,
			Description: g.config.Description,
			Version:     g.config.Version,
			Contact:     g.config.Contact,
			License:     g.config.License,
		},
		Servers: servers,
		Paths:   make(map[string]*PathItem),
		Components: &Components{
			Schemas: g.schemas.components, // Use the shared components map
			// Cloned, never aliased. addAuthSecuritySchemes maps.Copy's the auth
			// extension's schemes into this map, so aliasing it wrote them into
			// the caller's config -- and into every document already returned,
			// since they all shared that one map.
			SecuritySchemes: maps.Clone(g.config.Security),
		},
		// Copied for the same reason: a returned document must not share
		// mutable state with the config it was built from.
		Tags:         slices.Clone(g.config.Tags),
		ExternalDocs: g.config.ExternalDocs,
	}

	// Add auth provider security schemes if auth extension is registered
	g.addAuthSecuritySchemes(spec)

	// Process all routes
	routes := g.router.Routes()
	for _, route := range routes {
		if err := g.processRoute(spec, route); err != nil {
			return nil, err
		}
	}

	// Process webhooks
	processWebhooks(spec, routes)

	// Every type is known now, so component names can be settled: types whose
	// bare name nobody else wants keep it, contested ones are qualified.
	g.schemas.finalizeComponentNames()

	// One contest cannot be settled by qualifying anybody: two types pinned to
	// the same explicit name. Shipping that document would hand one endpoint
	// the other type's schema, so it is an error rather than a warning.
	//
	// This runs before the detach below because there is no document to hand
	// back on this path -- cloning first would be work thrown away.
	if err := g.schemas.pinConflictError(); err != nil {
		return nil, err
	}

	// Detach the components map from the generator. Up to here the spec pointed
	// at g.schemas.components directly, so the next generation's beginSpec --
	// which clears that map in place -- would have emptied a document already
	// handed to a caller. The schemas themselves are rebuilt from scratch each
	// time, so a shallow copy is enough to make this document immutable.
	spec.Components.Schemas = maps.Clone(g.schemas.components)

	return spec, nil
}

// buildServers constructs the servers list with automatic localhost server if httpAddress is set.
func (g *openAPIGenerator) buildServers() []OpenAPIServer {
	servers := make([]OpenAPIServer, 0)

	// Add automatic localhost server if httpAddress is set
	if g.httpAddress != "" {
		port := extractPort(g.httpAddress)
		if port != "" {
			localhostServer := OpenAPIServer{
				URL:         "http://localhost:" + port,
				Description: "Development server",
			}
			servers = append(servers, localhostServer)
		}
	}

	// Add configured servers
	servers = append(servers, g.config.Servers...)

	return servers
}

// extractPort extracts the port from an HTTP address like ":8080" or "localhost:8080" or "0.0.0.0:8080".
func extractPort(address string) string {
	// Handle cases like ":8080"
	if len(address) > 0 && address[0] == ':' {
		return address[1:]
	}

	// Handle cases like "localhost:8080" or "0.0.0.0:8080"
	for i := len(address) - 1; i >= 0; i-- {
		if address[i] == ':' {
			return address[i+1:]
		}
	}

	return ""
}

// addAuthSecuritySchemes retrieves security schemes from the auth registry
// and adds them to the OpenAPI spec components.
func (g *openAPIGenerator) addAuthSecuritySchemes(spec *OpenAPISpec) {
	if g.container == nil {
		return
	}

	// Try to get the auth registry from the container
	// We use reflection to avoid direct import of auth extension
	type registryGetter interface {
		Get(name string) (any, error)
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
			maps.Copy(spec.Components.SecuritySchemes, schemes)
		}
	}
}

// processRoute converts a route to an OpenAPI operation.
func (g *openAPIGenerator) processRoute(spec *OpenAPISpec, route RouteInfo) error {
	// Check if route is excluded from OpenAPI
	if exclude, ok := route.Metadata["openapi.exclude"].(bool); ok && exclude {
		return nil // Skip this route
	}

	// Convert path to OpenAPI format (e.g., :param -> {param})
	openAPIPath := ConvertPathToOpenAPIFormat(route.Path)

	// Get or create path item
	pathItem := spec.Paths[openAPIPath]
	if pathItem == nil {
		pathItem = &PathItem{}
		spec.Paths[openAPIPath] = pathItem
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
		components, err := extractUnifiedRequestComponents(g.schemas, unifiedSchema)
		if err != nil {
			return err
		}

		// Add parameters from unified schema
		operation.Parameters = append(operation.Parameters, components.PathParams...)
		operation.Parameters = append(operation.Parameters, components.QueryParams...)
		operation.Parameters = append(operation.Parameters, components.HeaderParams...)

		// Add body schema if present. A struct whose fields are all path, query
		// or header parameters contributes no body, and neither does one whose
		// body component turned out empty -- see requestBodyCarriesContent.
		if components.HasBody && requestBodyCarriesContent(components.BodySchema) {
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
		requestBody, err := g.extractRequestSchema(spec, route)
		if err != nil {
			return err
		}

		if requestBody != nil {
			operation.RequestBody = requestBody
		}
	}

	// Process response schemas
	if err := g.extractResponseSchemas(spec, operation, route); err != nil {
		return err
	}

	// Process security requirements
	g.processSecurityRequirements(operation, route.Metadata)

	// Process deprecation
	if route.Metadata != nil {
		if deprecated, ok := route.Metadata["deprecated"].(bool); ok && deprecated {
			operation.Deprecated = deprecated
		}
	}

	// Process client-generation declarations (forge.client.*) into x-forge-* extensions
	applyForgeExtensions(operation, route.Metadata)

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

	return nil
}

// setOperation sets an operation on a path item based on HTTP method.
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

// applyForgeExtensions copies client-generation declarations from route
// metadata onto an operation as x-forge-* extensions.
//
// Routes that declared nothing get no extensions at all, so a spec generated by
// an application that never opts in is unchanged from today's output.
func applyForgeExtensions(op *Operation, metadata map[string]any) {
	if metadata == nil {
		return
	}

	set := func(key string, value any) {
		if op.Extensions == nil {
			op.Extensions = make(map[string]any)
		}

		op.Extensions[key] = value
	}

	if def, ok := metadata["forge.client.entity"].(EntityDef); ok {
		set("x-forge-entity", map[string]any{
			"type":    def.Type,
			"idField": def.IDField,
		})
	}

	if v, ok := metadata["forge.client.noEntity"].(bool); ok && v {
		set("x-forge-no-entity", true)
	}

	if tags, ok := metadata["forge.client.invalidates"].([]string); ok && len(tags) > 0 {
		set("x-forge-invalidates", tags)
	}

	if tags, ok := metadata["forge.client.noInvalidation"].([]string); ok && len(tags) > 0 {
		set("x-forge-no-invalidation", tags)
	}
}

// RegisterEndpoints registers OpenAPI spec and Swagger UI endpoints.
func (g *openAPIGenerator) RegisterEndpoints() {
	// Register spec endpoint
	if g.config.SpecEnabled {
		_ = g.router.GET(g.config.SpecPath, g.specHandler())
	}

	// Register Swagger UI endpoint
	if g.config.UIEnabled {
		_ = g.router.GET(g.config.UIPath, g.uiHandler())
	}
}

// specHandler returns the OpenAPI spec as JSON.
func (g *openAPIGenerator) specHandler() any {
	return func(ctx Context) error {
		spec, err := g.Generate()
		if err != nil {
			return ctx.JSON(http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
		}

		if g.config.PrettyJSON {
			return ctx.JSON(http.StatusOK, spec)
		}

		// Compact JSON
		data, err := json.Marshal(spec)
		if err != nil {
			return err
		}

		ctx.Response().Header().Set("Content-Type", "application/json")
		_, _ = ctx.Response().Write(data)

		return nil
	}
}

// uiHandler returns the Swagger UI HTML.
func (g *openAPIGenerator) uiHandler() any {
	return func(ctx Context) error {
		html := g.generateSwaggerHTML()

		ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = ctx.Response().Write([]byte(html))

		return nil
	}
}

// generateSwaggerHTML generates the Swagger UI HTML.
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

// requestBodyCarriesContent reports whether a schema describes anything a
// client could actually put in a request body.
//
// The question this answers is "does this request have a body at all", and it
// is asked of both request paths -- the unified one and the legacy one -- so a
// route cannot acquire a body on one path that it would not have on the other.
// An object with no properties is the empty request struct handlers use to say
// "this endpoint takes nothing"; describing that as a body is what put a
// requestBody on every route that named a request type, GETs included.
//
// This is the whole of the "does a body exist" question, and it is deliberately
// the whole of the fix. requestBody.required is a separate question with a
// separate answer -- see buildRequestBody -- and conflating the two is an error
// in the opposite direction: schema.required lists which properties must be
// present IF a body is sent, and says nothing about whether one must be sent.
func requestBodyCarriesContent(schema *Schema) bool {
	if schema == nil {
		return false
	}

	switch {
	case schema.Ref != "":
		return true
	case len(schema.Properties) > 0:
		return true
	case schema.AdditionalProperties != nil:
		return true
	case schema.Items != nil:
		return true
	case len(schema.AllOf) > 0, len(schema.AnyOf) > 0, len(schema.OneOf) > 0:
		return true
	case schema.Type != "" && schema.Type != "object":
		// A scalar or array body: type: string, type: array and friends all
		// describe something to send even with no properties of their own.
		return true
	}

	return false
}

// buildRequestBody builds a RequestBody from a schema.
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
		// An emitted body is required. requestBody.required and schema.required
		// are orthogonal in OpenAPI -- the first says a body must be sent, the
		// second says which properties must be present in one that is -- so a
		// body whose every property is optional must still be sent, even as {}.
		// Nothing in route metadata lets an author say otherwise today:
		// WithRequestBody's RequestBodyDef carries a Required field, but the
		// generator reads none of that option's three fields. Wiring it up is a
		// separate change; until then this is unconditional.
		Required: true,
		Content:  make(map[string]*MediaType),
	}

	// Get examples if specified
	var examples map[string]*Example
	if examplesData, ok := metadata["openapi.requestExamples"].(map[string]any); ok {
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

// extractRequestSchema extracts request schema from route metadata.
func (g *openAPIGenerator) extractRequestSchema(spec *OpenAPISpec, route RouteInfo) (*RequestBody, error) {
	if route.Metadata == nil {
		return nil, nil //nolint:nilnil // No request body for route without metadata
	}

	var (
		schema       *Schema
		componentFor reflect.Type // non-nil once the schema is worth registering
		contentTypes []string
	)

	// Check for manually specified schema

	if schemaVal, ok := route.Metadata["openapi.requestSchema"]; ok {
		if s, ok := schemaVal.(*Schema); ok {
			schema = s
		} else {
			// Generate from struct
			var err error

			schema, err = g.schemas.GenerateSchema(schemaVal)
			if err != nil {
				return nil, err // Return error
			}
		}
	} else if reqType, ok := route.Metadata["openapi.requestType"]; ok {
		// Check for auto-detected request type from opinionated handler
		if rt, ok := reqType.(reflect.Type); ok {
			// Create a zero value of the type for schema generation
			instance := reflect.New(rt).Interface()

			var err error

			schema, err = g.schemas.GenerateSchema(instance)
			if err != nil {
				return nil, err // Return error
			}

			componentFor = rt
		}
	}

	if schema == nil {
		return nil, nil //nolint:nilnil // No request schema for route without schema metadata
	}

	// A request type that describes no body -- the empty struct handlers use to
	// say "this endpoint takes nothing" -- gets no requestBody at all. Emitting
	// one put a body on every such route, GETs included, and the component
	// registration below would have left an unreferenced schema behind with it.
	if !requestBodyCarriesContent(schema) {
		return nil, nil //nolint:nilnil // Request type carries no body
	}

	// Store in components for reuse. Deliberately after the check above: a type
	// that contributes no body must not appear in components either.
	if componentFor != nil && spec.Components != nil {
		schema = g.schemas.registerComponent(componentFor, "", schema)
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

	// Build request body. Required is unconditional for the same reason it is
	// in buildRequestBody: a body that exists must be sent, whatever its
	// properties' own requiredness says.
	requestBody := &RequestBody{
		Description: "Request body",
		Required:    true,
		Content:     make(map[string]*MediaType),
	}

	// Get examples if specified
	var examples map[string]*Example
	if examplesData, ok := route.Metadata["openapi.requestExamples"].(map[string]any); ok {
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

	return requestBody, nil
}

// extractResponseSchemas processes response schemas from route metadata.
func (g *openAPIGenerator) extractResponseSchemas(spec *OpenAPISpec, operation *Operation, route RouteInfo) error {
	if route.Metadata == nil {
		return nil
	}

	// Get content types
	contentTypes := []string{"application/json"}
	if types, ok := route.Metadata["openapi.responseContentTypes"].([]string); ok {
		contentTypes = types
	}

	// Get response examples if specified
	var responseExamples map[int]map[string]*Example
	if examplesData, ok := route.Metadata["openapi.responseExamples"].(map[int]map[string]any); ok {
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
			var (
				schema  *Schema
				headers map[string]*Header
			)

			if s, ok := respDef.Schema.(*Schema); ok {
				schema = s
			} else if respDef.Schema != nil {
				// Check if this is a unified response schema with headers
				components, err := extractUnifiedResponseComponents(g.schemas, respDef.Schema)
				if err != nil {
					return err
				}

				// Use extracted headers if present
				if len(components.Headers) > 0 {
					headers = components.Headers
				}

				// Use extracted body schema if present
				if components.HasBody {
					schema = components.BodySchema

					// Try to store in components if it's a named struct type
					if rt := reflect.TypeOf(respDef.Schema); rt != nil {
						if rt.Kind() == reflect.Ptr {
							rt = rt.Elem()
						}

						if rt.Kind() == reflect.Struct {
							// Check if the body schema is already a reference (unwrapped body:"")
							// If so, use it directly without creating a wrapper component
							if schema != nil && schema.Ref != "" { //nolint:gocritic // ifElseChain: schema resolution logic clearer with if-else
								// Already a ref (unwrapped), use directly
								// Don't create a wrapper component
							} else if len(components.Headers) > 0 {
								// For unified responses with headers, register body schema as component
								// Use custom schema name if provided, otherwise default to TypeName + "Body"
								if spec.Components != nil {
									if name := components.BodySchemaName; name != "" {
										schema = g.schemas.registerPinnedComponent(name, rt, schema)
									} else {
										schema = g.schemas.registerComponent(rt, "Body", schema)
									}
								}
							} else {
								// No headers and no unwrapping, register full struct as component
								if spec.Components != nil {
									schema = g.schemas.registerComponent(rt, "", schema)
								}
							}
						}
					}
				} else if len(components.Headers) == 0 {
					// Fall back to generating schema normally ONLY if there are no headers
					// (If there are headers but no body, schema should remain nil - headers-only response)
					schema, err = g.schemas.GenerateSchema(respDef.Schema)
					if err != nil {
						return err
					}

					// Try to store in components
					if rt := reflect.TypeOf(respDef.Schema); rt != nil {
						if rt.Kind() == reflect.Ptr {
							rt = rt.Elem()
						}

						if rt.Kind() == reflect.Struct && spec.Components != nil {
							schema = g.schemas.registerComponent(rt, "", schema)
						}
					}
				}
				// else: headers but no body - schema remains nil (headers-only response)
			}

			response := &Response{
				Description: respDef.Description,
			}

			// Only add content if there's a schema (body)
			if schema != nil {
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

				response.Content = content
			}

			// Add headers if extracted
			if len(headers) > 0 {
				response.Headers = headers
			}

			operation.Responses[strconv.Itoa(statusCode)] = response
		}

		return nil
	}

	// Check for auto-detected response type from opinionated handler
	if respType, ok := route.Metadata["openapi.responseType"]; ok {
		if rt, ok := respType.(reflect.Type); ok {
			// Create a zero value of the type for schema generation
			instance := reflect.New(rt).Interface()

			schema, err := g.schemas.GenerateSchema(instance)
			if err != nil {
				return err // Return error
			}

			// Store in components for reuse
			if spec.Components != nil {
				schema = g.schemas.registerComponent(rt, "", schema)
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

	return nil
}

// extractPathParameters parses path parameters from the path string.
func (g *openAPIGenerator) extractPathParameters(path string, metadata map[string]any) []Parameter {
	pathParams := extractPathParamsFromPath(path)

	return convertPathParamsToOpenAPIParams(pathParams)
}

// extractQueryParameters extracts query parameters from metadata.
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

// extractHeaderParameters extracts header parameters from metadata.
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

// processSecurityRequirements adds security requirements to operation.
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
			if len(scopes) > 0 {
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
			if len(scopes) > 0 {
				req[provider] = scopes
			} else {
				req[provider] = []string{}
			}

			operation.Security = append(operation.Security, req)
		}
	}
}
