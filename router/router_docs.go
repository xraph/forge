package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/logger"
)

// EnableDocumentation Documentation methods for Router interface
func (g *group) EnableDocumentation(config DocumentationConfig) Router {
	g.parent.EnableDocumentation(config)
	return g
}

// EnableDocumentation Documentation methods for Router interface
func (r *router) EnableDocumentation(config DocumentationConfig) Router {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Create documentation generator
	docGen := NewDocumentationGenerator(config, r.logger)

	// Analyze existing routes
	if err := docGen.AnalyzeRoutes(r.routes); err != nil {
		r.logger.Error("Failed to analyze routes for documentation", logger.Error(err))
		return r
	}

	// Register documentation endpoints
	if config.EnableSwaggerUI {
		r.setupSwaggerUI(config, docGen)
	}

	if config.EnableReDoc {
		r.setupReDoc(config, docGen)
	}

	if config.EnableAsyncAPIUI {
		r.setupAsyncAPIUI(config, docGen)
	}

	// Register spec endpoints
	r.setupSpecEndpoints(config, docGen)

	if config.EnablePostman {
		r.setupPostmanEndpoint(config, docGen)
	}

	r.logger.Info("Documentation system enabled",
		logger.String("swagger_ui", config.SwaggerUIPath),
		logger.String("redoc", config.ReDocPath),
		logger.String("asyncapi_ui", config.AsyncAPIUIPath),
		logger.String("openapi_spec", config.OpenAPISpecPath),
		logger.String("asyncapi_spec", config.AsyncAPISpecPath),
	)

	return r
}

// setupSwaggerUI sets up Swagger UI endpoint
func (r *router) setupSwaggerUI(config DocumentationConfig, docGen *DocumentationGenerator) {
	swaggerHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s - API Documentation</title>
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui.css" />
    <link rel="icon" type="image/png" href="https://unpkg.com/swagger-ui-dist@5.9.0/favicon-32x32.png" sizes="32x32" />
    <link rel="icon" type="image/png" href="https://unpkg.com/swagger-ui-dist@5.9.0/favicon-16x16.png" sizes="16x16" />
    <style>
        html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
        *, *:before, *:after { box-sizing: inherit; }
        body { margin:0; background: #fafafa; }
        .swagger-ui .topbar { background-color: #1976d2; }
        .swagger-ui .topbar .download-url-wrapper { display: none; }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-bundle.js"></script>
    <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            const ui = SwaggerUIBundle({
                url: '%s',
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
                validatorUrl: null,
                docExpansion: "none",
                operationsSorter: "alpha",
                tagsSorter: "alpha",
                filter: true,
                showExtensions: true,
                showCommonExtensions: true,
                defaultModelExpandDepth: 2,
                defaultModelsExpandDepth: 1,
                displayRequestDuration: true,
                tryItOutEnabled: true,
                persistAuthorization: true,
                supportedSubmitMethods: ['get', 'post', 'put', 'delete', 'patch', 'head', 'options'],
                onComplete: function() {
                    console.log('Swagger UI loaded');
                }
            });
            
            // Custom styling
            const style = document.createElement('style');
            style.textContent =
		.swagger-ui .info .title { color: #1976d2; }
		.swagger-ui .scheme-container { background: #1976d2; }
		.swagger-ui .btn.authorize { background-color: #1976d2; border-color: #1976d2; }
		.swagger-ui .btn.authorize:hover { background-color: #1565c0; }
	;
            document.head.appendChild(style);
            
            window.ui = ui;
        };
    </script>
</body>
</html>`, config.Title, config.OpenAPISpecPath)

	r.Get(config.SwaggerUIPath, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(swaggerHTML))
	})
}

// setupReDoc sets up ReDoc endpoint
func (r *router) setupReDoc(config DocumentationConfig, docGen *DocumentationGenerator) {
	redocHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>%s - API Documentation</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <link href="https://fonts.googleapis.com/css?family=Montserrat:300,400,700|Roboto:300,400,700" rel="stylesheet">
    <style>
        body { margin: 0; padding: 0; }
        redoc { display: block; }
    </style>
</head>
<body>
    <redoc spec-url='%s'></redoc>
    <script src="https://cdn.jsdelivr.net/npm/redoc@2.1.3/bundles/redoc.standalone.js"></script>
    <script>
        Redoc.init('%s', {
            scrollYOffset: 50,
            hideDownloadButton: false,
            theme: {
                colors: {
                    primary: {
                        main: '#1976d2'
                    }
                },
                typography: {
                    fontSize: '14px',
                    lineHeight: '1.5',
                    code: {
                        fontSize: '13px',
                        fontFamily: 'Courier, monospace'
                    },
                    headings: {
                        fontFamily: 'Montserrat, sans-serif',
                        fontWeight: '400'
                    }
                },
                sidebar: {
                    width: '260px',
                    backgroundColor: '#fafafa'
                },
                rightPanel: {
                    backgroundColor: '#263238',
                    width: '40%%'
                }
            }
        });
    </script>
</body>
</html>`, config.Title, config.OpenAPISpecPath, config.OpenAPISpecPath)

	r.Get(config.ReDocPath, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(redocHTML))
	})
}

// setupAsyncAPIUI sets up AsyncAPI UI endpoint
func (r *router) setupAsyncAPIUI(config DocumentationConfig, docGen *DocumentationGenerator) {
	asyncAPIHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>%s - AsyncAPI Documentation</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <link rel="stylesheet" href="https://unpkg.com/@asyncapi/react-component@1.0.0-next.39/styles/default.min.css">
    <style>
        body { margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', 'Fira Sans', 'Droid Sans', 'Helvetica Neue', sans-serif; }
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        .header { text-align: center; margin-bottom: 30px; }
        .header h1 { color: #1976d2; margin: 0; }
        .header p { color: #666; margin: 10px 0; }
        .loading { text-align: center; padding: 50px; }
        .error { color: #f44336; text-align: center; padding: 20px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>%s</h1>
            <p>Real-time API Documentation</p>
        </div>
        <div id="asyncapi-doc">
            <div class="loading">Loading AsyncAPI documentation...</div>
        </div>
    </div>
    
    <script src="https://unpkg.com/@asyncapi/react-component@1.0.0-next.39/browser/standalone/index.js"></script>
    <script>
        fetch('%s')
            .then(response => response.json())
            .then(schema => {
                const container = document.getElementById('asyncapi-doc');
                container.innerHTML = '';
                
                AsyncAPIComponent.render(schema, container, {
                    config: {
                        show: {
                            sidebar: true,
                            info: true,
                            servers: true,
                            operations: true,
                            messages: true,
                            schemas: true,
                            errors: true
                        },
                        expand: {
                            messageExamples: true,
                            schemaExamples: true
                        },
                        sidebar: {
                            showServers: true,
                            showOperations: true
                        }
                    }
                });
            })
            .catch(error => {
                console.error('Error loading AsyncAPI spec:', error);
                document.getElementById('asyncapi-doc').innerHTML = 
                    '<div class="error">Error loading AsyncAPI documentation: ' + error.message + '</div>';
            });
    </script>
</body>
</html>`, config.Title, config.Title, config.AsyncAPISpecPath)

	r.Get(config.AsyncAPIUIPath, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(asyncAPIHTML))
	})
}

// setupSpecEndpoints sets up specification endpoints
func (r *router) setupSpecEndpoints(config DocumentationConfig, docGen *DocumentationGenerator) {
	// OpenAPI JSON endpoint - only register if path is configured
	if config.OpenAPISpecPath != "" {
		r.Get(config.OpenAPISpecPath, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			// Re-analyze routes to get latest state
			if err := docGen.AnalyzeRoutes(r.routes); err != nil {
				r.logger.Error("Failed to analyze routes", logger.Error(err))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			jsonData, err := docGen.GetOpenAPIJSON()
			if err != nil {
				r.logger.Error("Failed to generate OpenAPI JSON", logger.Error(err))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			w.Write(jsonData)
		})
	}

	// AsyncAPI JSON endpoint - only register if path is configured
	if config.AsyncAPISpecPath != "" {
		r.Get(config.AsyncAPISpecPath, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			// Re-analyze routes to get latest state
			if err := docGen.AnalyzeRoutes(r.routes); err != nil {
				r.logger.Error("Failed to analyze routes", logger.Error(err))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			jsonData, err := docGen.GetAsyncAPIJSON()
			if err != nil {
				r.logger.Error("Failed to generate AsyncAPI JSON", logger.Error(err))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			w.Write(jsonData)
		})
	}

	// OpenAPI YAML endpoint - only register if path is configured
	if config.OpenAPISpecPath != "" {
		yamlPath := strings.Replace(config.OpenAPISpecPath, ".json", ".yaml", 1)
		r.Get(yamlPath, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if err := docGen.AnalyzeRoutes(r.routes); err != nil {
				r.logger.Error("Failed to analyze routes", logger.Error(err))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			yamlData, err := docGen.GetOpenAPIYAML()
			if err != nil {
				r.logger.Error("Failed to generate OpenAPI YAML", logger.Error(err))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			w.Write(yamlData)
		})
	}
}

// setupPostmanEndpoint sets up Postman collection endpoint
func (r *router) setupPostmanEndpoint(config DocumentationConfig, docGen *DocumentationGenerator) {
	r.Get(config.PostmanPath, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Re-analyze routes to get latest state
		if err := docGen.AnalyzeRoutes(r.routes); err != nil {
			r.logger.Error("Failed to analyze routes", logger.Error(err))
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		postmanCollection, err := docGen.GetPostmanCollection()
		if err != nil {
			r.logger.Error("Failed to generate Postman collection", logger.Error(err))
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		jsonData, err := json.MarshalIndent(postmanCollection, "", "  ")
		if err != nil {
			r.logger.Error("Failed to marshal Postman collection", logger.Error(err))
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Write(jsonData)
	})
}

// Enhanced router methods for better documentation
func (r *router) DocGet(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	r.Get(pattern, handler)
	r.addRouteDocumentation(http.MethodGet, pattern, doc)
	return r
}

func (r *router) DocPost(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	r.Post(pattern, handler)
	r.addRouteDocumentation(http.MethodPost, pattern, doc)
	return r
}

func (r *router) DocPut(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	r.Put(pattern, handler)
	r.addRouteDocumentation(http.MethodPut, pattern, doc)
	return r
}

func (r *router) DocPatch(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	r.Patch(pattern, handler)
	r.addRouteDocumentation(http.MethodPatch, pattern, doc)
	return r
}

func (r *router) DocDelete(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	r.Delete(pattern, handler)
	r.addRouteDocumentation(http.MethodDelete, pattern, doc)
	return r
}

func (r *router) DocHead(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	r.Head(pattern, handler)
	r.addRouteDocumentation(http.MethodHead, pattern, doc)
	return r
}

func (r *router) DocOptions(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	r.Options(pattern, handler)
	r.addRouteDocumentation(http.MethodOptions, pattern, doc)
	return r
}

// DocWebSocket documents WebSocket endpoints
func (r *router) DocWebSocket(pattern string, handler WebSocketHandler, doc AsyncRouteDocumentation) Router {
	r.WebSocket(pattern, handler)
	r.addAsyncRouteDocumentation("websocket", pattern, doc)
	return r
}

// DocSSE documents Server-Sent Events endpoints
func (r *router) DocSSE(pattern string, handler SSEHandler, doc AsyncRouteDocumentation) Router {
	r.SSE(pattern, handler)
	r.addAsyncRouteDocumentation("sse", pattern, doc)
	return r
}

// addRouteDocumentation adds documentation to a route
func (r *router) addRouteDocumentation(method, pattern string, doc RouteDocumentation) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Find the route and add documentation
	for i, route := range r.routes {
		if route.Method == method && route.Pattern == pattern {
			if route.Metadata == nil {
				route.Metadata = make(map[string]interface{})
			}

			// Add documentation metadata
			route.Metadata["summary"] = doc.Summary
			route.Metadata["description"] = doc.Description
			route.Metadata["tags"] = doc.Tags
			route.Metadata["deprecated"] = doc.Deprecated
			route.Metadata["openapi"] = doc.OpenAPI
			route.Metadata["request_body"] = doc.RequestBody
			route.Metadata["responses"] = doc.Responses
			route.Metadata["parameters"] = doc.Parameters
			route.Metadata["security"] = doc.Security
			route.Metadata["examples"] = doc.Examples

			r.routes[i] = route
			break
		}
	}
}

// addAsyncRouteDocumentation adds documentation to async routes
func (r *router) addAsyncRouteDocumentation(protocol, pattern string, doc AsyncRouteDocumentation) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Find the route and add documentation
	for i, route := range r.routes {
		if route.Pattern == pattern {
			if route.Metadata == nil {
				route.Metadata = make(map[string]interface{})
			}

			// Add async documentation metadata
			route.Metadata["protocol"] = protocol
			route.Metadata["summary"] = doc.Summary
			route.Metadata["description"] = doc.Description
			route.Metadata["tags"] = doc.Tags
			route.Metadata["messages"] = doc.Messages
			route.Metadata["bindings"] = doc.Bindings
			route.Metadata["security"] = doc.Security
			route.Metadata["examples"] = doc.Examples

			r.routes[i] = route
			break
		}
	}
}

// Group documentation methods
func (g *group) DocGet(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	g.Get(pattern, handler)
	g.addRouteDocumentation(http.MethodGet, pattern, doc)
	return g
}

func (g *group) DocPost(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	g.Post(pattern, handler)
	g.addRouteDocumentation(http.MethodPost, pattern, doc)
	return g
}

func (g *group) DocPut(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	g.Put(pattern, handler)
	g.addRouteDocumentation(http.MethodPut, pattern, doc)
	return g
}

func (g *group) DocPatch(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	g.Patch(pattern, handler)
	g.addRouteDocumentation(http.MethodPatch, pattern, doc)
	return g
}

func (g *group) DocDelete(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	g.Delete(pattern, handler)
	g.addRouteDocumentation(http.MethodDelete, pattern, doc)
	return g
}

func (g *group) DocHead(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	g.Head(pattern, handler)
	g.addRouteDocumentation(http.MethodHead, pattern, doc)
	return g
}

func (g *group) DocOptions(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router {
	g.Options(pattern, handler)
	g.addRouteDocumentation(http.MethodOptions, pattern, doc)
	return g
}

func (g *group) DocWebSocket(pattern string, handler WebSocketHandler, doc AsyncRouteDocumentation) Router {
	g.WebSocket(pattern, handler)
	g.addAsyncRouteDocumentation("websocket", pattern, doc)
	return g
}

func (g *group) DocSSE(pattern string, handler SSEHandler, doc AsyncRouteDocumentation) Router {
	g.SSE(pattern, handler)
	g.addAsyncRouteDocumentation("sse", pattern, doc)
	return g
}

// Group documentation methods
func (g *group) addRouteDocumentation(method, pattern string, doc RouteDocumentation) {
	g.mu.Lock()
	defer g.mu.Unlock()

	fullPattern := g.fullPath + pattern

	// Find the route and add documentation
	for i, route := range g.routes {
		if route.Method == method && route.Pattern == fullPattern {
			if route.Metadata == nil {
				route.Metadata = make(map[string]interface{})
			}

			// Add documentation metadata
			route.Metadata["summary"] = doc.Summary
			route.Metadata["description"] = doc.Description
			route.Metadata["tags"] = doc.Tags
			route.Metadata["deprecated"] = doc.Deprecated
			route.Metadata["openapi"] = doc.OpenAPI
			route.Metadata["request_body"] = doc.RequestBody
			route.Metadata["responses"] = doc.Responses
			route.Metadata["parameters"] = doc.Parameters
			route.Metadata["security"] = doc.Security
			route.Metadata["examples"] = doc.Examples

			g.routes[i] = route
			break
		}
	}
}

func (g *group) addAsyncRouteDocumentation(protocol, pattern string, doc AsyncRouteDocumentation) {
	g.mu.Lock()
	defer g.mu.Unlock()

	fullPattern := g.fullPath + pattern

	// Find the route and add documentation
	for i, route := range g.routes {
		if route.Pattern == fullPattern {
			if route.Metadata == nil {
				route.Metadata = make(map[string]interface{})
			}

			// Add async documentation metadata
			route.Metadata["protocol"] = protocol
			route.Metadata["summary"] = doc.Summary
			route.Metadata["description"] = doc.Description
			route.Metadata["tags"] = doc.Tags
			route.Metadata["messages"] = doc.Messages
			route.Metadata["bindings"] = doc.Bindings
			route.Metadata["security"] = doc.Security
			route.Metadata["examples"] = doc.Examples

			g.routes[i] = route
			break
		}
	}
}

// Documentation structures for route annotation
type RouteDocumentation struct {
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	Tags        []string               `json:"tags"`
	Deprecated  bool                   `json:"deprecated"`
	OpenAPI     *OpenAPIOperation      `json:"openapi,omitempty"`
	RequestBody *RequestBody           `json:"request_body,omitempty"`
	Responses   map[string]Response    `json:"responses"`
	Parameters  []Parameter            `json:"parameters"`
	Security    []SecurityRequirement  `json:"security"`
	Examples    map[string]interface{} `json:"examples"`
}

type AsyncRouteDocumentation struct {
	Summary     string                      `json:"summary"`
	Description string                      `json:"description"`
	Tags        []string                    `json:"tags"`
	Messages    []AsyncMessageDocumentation `json:"messages"`
	Bindings    map[string]interface{}      `json:"bindings"`
	Security    []SecurityRequirement       `json:"security"`
	Examples    map[string]interface{}      `json:"examples"`
}

type AsyncMessageDocumentation struct {
	Name        string                 `json:"name"`
	Title       string                 `json:"title"`
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	ContentType string                 `json:"content_type"`
	Headers     map[string]interface{} `json:"headers"`
	Payload     map[string]interface{} `json:"payload"`
	Examples    []interface{}          `json:"examples"`
}

// OpenAPIOperation for documentation
type OpenAPIOperation struct {
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	OperationID string                `json:"operationId,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses,omitempty"`
	Security    []SecurityRequirement `json:"security,omitempty"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
}

// Utility functions for creating documentation
func NewRouteDoc(summary, description string) RouteDocumentation {
	return RouteDocumentation{
		Summary:     summary,
		Description: description,
		Tags:        make([]string, 0),
		Responses:   make(map[string]Response),
		Parameters:  make([]Parameter, 0),
		Security:    make([]SecurityRequirement, 0),
		Examples:    make(map[string]interface{}),
	}
}

func (doc RouteDocumentation) WithTags(tags ...string) RouteDocumentation {
	doc.Tags = append(doc.Tags, tags...)
	return doc
}

func (doc RouteDocumentation) WithDeprecated(deprecated bool) RouteDocumentation {
	doc.Deprecated = deprecated
	return doc
}

func (doc RouteDocumentation) WithRequestBody(description string, schema *Schema) RouteDocumentation {
	doc.RequestBody = &RequestBody{
		Description: description,
		Content: map[string]*MediaType{
			"application/json": {
				Schema: schema,
			},
		},
		Required: true,
	}
	return doc
}

func (doc RouteDocumentation) WithResponse(code, description string, schema *Schema) RouteDocumentation {
	doc.Responses[code] = Response{
		Description: description,
		Content: map[string]*MediaType{
			"application/json": {
				Schema: schema,
			},
		},
	}
	return doc
}

func (doc RouteDocumentation) WithParameter(name, in, description string, required bool, schema *Schema) RouteDocumentation {
	param := Parameter{
		Name:        name,
		In:          in,
		Description: description,
		Required:    required,
		Schema:      schema,
	}
	doc.Parameters = append(doc.Parameters, param)
	return doc
}

func (doc RouteDocumentation) WithSecurity(security SecurityRequirement) RouteDocumentation {
	doc.Security = append(doc.Security, security)
	return doc
}

func (doc RouteDocumentation) WithExample(name string, value interface{}) RouteDocumentation {
	doc.Examples[name] = value
	return doc
}

func NewAsyncRouteDoc(summary, description string) AsyncRouteDocumentation {
	return AsyncRouteDocumentation{
		Summary:     summary,
		Description: description,
		Tags:        make([]string, 0),
		Messages:    make([]AsyncMessageDocumentation, 0),
		Bindings:    make(map[string]interface{}),
		Security:    make([]SecurityRequirement, 0),
		Examples:    make(map[string]interface{}),
	}
}

func (doc AsyncRouteDocumentation) WithTags(tags ...string) AsyncRouteDocumentation {
	doc.Tags = append(doc.Tags, tags...)
	return doc
}

func (doc AsyncRouteDocumentation) WithMessage(name, title, description string, payload map[string]interface{}) AsyncRouteDocumentation {
	message := AsyncMessageDocumentation{
		Name:        name,
		Title:       title,
		Description: description,
		ContentType: "application/json",
		Payload:     payload,
		Examples:    make([]interface{}, 0),
	}
	doc.Messages = append(doc.Messages, message)
	return doc
}

func (doc AsyncRouteDocumentation) WithBinding(protocol string, binding interface{}) AsyncRouteDocumentation {
	doc.Bindings[protocol] = binding
	return doc
}

func (doc AsyncRouteDocumentation) WithSecurity(security SecurityRequirement) AsyncRouteDocumentation {
	doc.Security = append(doc.Security, security)
	return doc
}

func (doc AsyncRouteDocumentation) WithExample(name string, value interface{}) AsyncRouteDocumentation {
	doc.Examples[name] = value
	return doc
}

// Security requirement builders
func BearerAuth() SecurityRequirement {
	return SecurityRequirement{
		"bearerAuth": []string{},
	}
}

func APIKeyAuth(name string) SecurityRequirement {
	return SecurityRequirement{
		name: []string{},
	}
}

func OAuth2Auth(scopes ...string) SecurityRequirement {
	return SecurityRequirement{
		"oauth2": scopes,
	}
}

// DocumentationMiddleware Documentation middleware for automatic annotation
func DocumentationMiddleware(config DocumentationConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add documentation context
			ctx := r.Context()

			// Extract route information
			if pattern, ok := ctx.Value("route_pattern").(string); ok {
				// Add route documentation metadata to context
				ctx = context.WithValue(ctx, "doc_pattern", pattern)
				ctx = context.WithValue(ctx, "doc_method", r.Method)
				ctx = context.WithValue(ctx, "doc_timestamp", time.Now())
			}

			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}
