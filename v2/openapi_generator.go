package forge

import (
	"encoding/json"
	"net/http"
	"strings"
)

// openAPIGenerator generates OpenAPI 3.1.0 specifications from a router
type openAPIGenerator struct {
	config  OpenAPIConfig
	router  Router
	schemas *schemaGenerator
}

// newOpenAPIGenerator creates a new OpenAPI generator
func newOpenAPIGenerator(config OpenAPIConfig, router Router) *openAPIGenerator {
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

	return &openAPIGenerator{
		config:  config,
		router:  router,
		schemas: newSchemaGenerator(),
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
			Schemas:         make(map[string]*Schema),
			SecuritySchemes: g.config.Security,
		},
		Tags:         g.config.Tags,
		ExternalDocs: g.config.ExternalDocs,
	}

	// Process all routes
	routes := g.router.Routes()
	for _, route := range routes {
		g.processRoute(spec, route)
	}

	return spec
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
	}

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
		g.router.GET(g.config.SpecPath, g.specHandler())
	}

	// Register Swagger UI endpoint
	if g.config.UIEnabled {
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
		ctx.Response().Write(data)
		return nil
	}
}

// uiHandler returns the Swagger UI HTML
func (g *openAPIGenerator) uiHandler() interface{} {
	return func(ctx Context) error {
		html := g.generateSwaggerHTML()
		ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
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
