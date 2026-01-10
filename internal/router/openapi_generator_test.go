package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/di"
)

func TestNewOpenAPIGenerator(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	assert.NotNil(t, gen)
	assert.Equal(t, "Test API", gen.config.Title)
	assert.Equal(t, "3.1.0", gen.config.OpenAPIVersion)   // Default
	assert.Equal(t, "/swagger", gen.config.UIPath)        // Default
	assert.Equal(t, "/openapi.json", gen.config.SpecPath) // Default
}

func TestNewOpenAPIGenerator_WithDefaults(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	config := OpenAPIConfig{
		Title:          "Test API",
		Version:        "1.0.0",
		OpenAPIVersion: "3.0.0", // Override default
		UIPath:         "/docs",
		SpecPath:       "/spec.json",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	assert.NotNil(t, gen)
	assert.Equal(t, "3.0.0", gen.config.OpenAPIVersion)
	assert.Equal(t, "/docs", gen.config.UIPath)
	assert.Equal(t, "/spec.json", gen.config.SpecPath)
}

func TestOpenAPIGenerator_Generate(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Add some routes
	router.GET("/users", func(ctx Context) error {
		return nil
	}, WithSummary("List users"), WithTags("users"))

	router.GET("/users/:id", func(ctx Context) error {
		return nil
	}, WithSummary("Get user"), WithTags("users"))

	config := OpenAPIConfig{
		Title:       "Test API",
		Description: "Test API Description",
		Version:     "1.0.0",
		Servers: []OpenAPIServer{
			{
				URL:         "https://api.example.com",
				Description: "Production",
			},
		},
		Tags: []OpenAPITag{
			{Name: "users", Description: "User operations"},
		},
	}

	gen := newOpenAPIGenerator(config, router, nil)
	spec, _ := gen.Generate()

	require.NotNil(t, spec)
	assert.Equal(t, "3.1.0", spec.OpenAPI)
	assert.Equal(t, "Test API", spec.Info.Title)
	assert.Equal(t, "Test API Description", spec.Info.Description)
	assert.Equal(t, "1.0.0", spec.Info.Version)
	assert.Len(t, spec.Servers, 1)
	assert.Len(t, spec.Tags, 1)
	assert.NotNil(t, spec.Components)
	assert.NotNil(t, spec.Paths)
}

func TestOpenAPIGenerator_ProcessRoute(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	router.GET("/users", func(ctx Context) error {
		return nil
	},
		WithSummary("List users"),
		WithDescription("Get all users"),
		WithTags("users", "admin"),
	)

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	spec, _ := gen.Generate()

	require.NotNil(t, spec.Paths["/users"])
	pathItem := spec.Paths["/users"]
	require.NotNil(t, pathItem.Get)

	operation := pathItem.Get
	assert.Equal(t, "List users", operation.Summary)
	assert.Equal(t, "Get all users", operation.Description)
	assert.Len(t, operation.Tags, 2)
	assert.Contains(t, operation.Tags, "users")
	assert.Contains(t, operation.Tags, "admin")
}

func TestOpenAPIGenerator_SetOperation(t *testing.T) {
	operation := &Operation{Summary: "Test"}

	tests := []struct {
		method string
		check  func(*PathItem) *Operation
	}{
		{"GET", func(p *PathItem) *Operation { return p.Get }},
		{"POST", func(p *PathItem) *Operation { return p.Post }},
		{"PUT", func(p *PathItem) *Operation { return p.Put }},
		{"DELETE", func(p *PathItem) *Operation { return p.Delete }},
		{"PATCH", func(p *PathItem) *Operation { return p.Patch }},
		{"OPTIONS", func(p *PathItem) *Operation { return p.Options }},
		{"HEAD", func(p *PathItem) *Operation { return p.Head }},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			pathItem := &PathItem{}
			container := di.NewContainer()
			router := NewRouter(WithContainer(container))
			config := OpenAPIConfig{Title: "Test", Version: "1.0.0"}
			gen := newOpenAPIGenerator(config, router, nil)

			gen.setOperation(pathItem, tt.method, operation)
			assert.NotNil(t, tt.check(pathItem))
			assert.Equal(t, "Test", tt.check(pathItem).Summary)
		})
	}
}

func TestOpenAPIGenerator_DefaultResponse(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	router.GET("/test", func(ctx Context) error {
		return nil
	})

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	spec, _ := gen.Generate()

	require.NotNil(t, spec.Paths["/test"])
	pathItem := spec.Paths["/test"]
	require.NotNil(t, pathItem.Get)
	require.NotNil(t, pathItem.Get.Responses)

	// Should have default 200 response
	response, ok := pathItem.Get.Responses["200"]
	require.True(t, ok)
	assert.Equal(t, "Success", response.Description)
}

func TestOpenAPIGenerator_SpecHandler(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	config := OpenAPIConfig{
		Title:      "Test API",
		Version:    "1.0.0",
		PrettyJSON: false,
	}

	gen := newOpenAPIGenerator(config, router, nil)
	handler := gen.specHandler()

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()

	// Convert handler to http.HandlerFunc
	contextFunc, ok := handler.(func(Context) error)
	require.True(t, ok)

	// Create context
	ctx := di.NewContext(w, req, nil)
	err := contextFunc(ctx)
	assert.NoError(t, err)

	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOpenAPIGenerator_UIHandler(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	handler := gen.uiHandler()

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	w := httptest.NewRecorder()

	// Convert handler to http.HandlerFunc
	contextFunc, ok := handler.(func(Context) error)
	require.True(t, ok)

	// Create context
	ctx := di.NewContext(w, req, nil)
	err := contextFunc(ctx)
	assert.NoError(t, err)

	// Check response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "swagger-ui")
	assert.Contains(t, w.Body.String(), "Test API")
}

func TestOpenAPIGenerator_SwaggerHTML(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	config := OpenAPIConfig{
		Title:    "My API",
		Version:  "1.0.0",
		SpecPath: "/api/spec.json",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	html := gen.generateSwaggerHTML()

	assert.Contains(t, html, "<!DOCTYPE html>")
	assert.Contains(t, html, "My API")
	assert.Contains(t, html, "/api/spec.json")
	assert.Contains(t, html, "swagger-ui-bundle.js")
	assert.Contains(t, html, "swagger-ui.css")
}

func TestRouter_WithOpenAPI(t *testing.T) {
	container := di.NewContainer()

	config := OpenAPIConfig{
		Title:       "Test API",
		Version:     "1.0.0",
		UIEnabled:   true,
		SpecEnabled: true,
	}

	router := NewRouter(
		WithContainer(container),
		WithOpenAPI(config),
	)

	// setupOpenAPI should be called during router creation
	// Check that routes were registered
	routes := router.Routes()

	// Should have at least the OpenAPI routes
	var hasSpec, hasUI bool

	for _, route := range routes {
		if route.Path == "/openapi.json" {
			hasSpec = true
		}

		if route.Path == "/swagger" {
			hasUI = true
		}
	}

	assert.True(t, hasSpec, "OpenAPI spec endpoint should be registered")
	assert.True(t, hasUI, "Swagger UI endpoint should be registered")
}

func TestRouter_OpenAPISpec(t *testing.T) {
	container := di.NewContainer()

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	router := NewRouter(
		WithContainer(container),
		WithOpenAPI(config),
	)

	// Add a route
	router.GET("/test", func(ctx Context) error {
		return nil
	}, WithSummary("Test route"))

	// Get OpenAPI spec
	spec := router.OpenAPISpec()
	require.NotNil(t, spec)
	assert.Equal(t, "3.1.0", spec.OpenAPI)
	assert.Equal(t, "Test API", spec.Info.Title)
}

func TestRouter_OpenAPISpec_Disabled(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Without OpenAPI config
	spec := router.OpenAPISpec()
	assert.Nil(t, spec)
}

func TestOpenAPIGenerator_RegisterEndpoints_SpecOnly(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	config := OpenAPIConfig{
		Title:       "Test API",
		Version:     "1.0.0",
		SpecEnabled: true,
		UIEnabled:   false,
		SpecPath:    "/api-spec.json",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	gen.RegisterEndpoints()

	routes := router.Routes()

	var hasSpec, hasUI bool

	for _, route := range routes {
		if route.Path == "/api-spec.json" {
			hasSpec = true
		}

		if route.Path == "/swagger" {
			hasUI = true
		}
	}

	assert.True(t, hasSpec)
	assert.False(t, hasUI)
}

func TestOpenAPIGenerator_RegisterEndpoints_UIOnly(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	config := OpenAPIConfig{
		Title:       "Test API",
		Version:     "1.0.0",
		SpecEnabled: false,
		UIEnabled:   true,
		UIPath:      "/docs",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	gen.RegisterEndpoints()

	routes := router.Routes()

	var hasSpec, hasUI bool

	for _, route := range routes {
		if route.Path == "/openapi.json" {
			hasSpec = true
		}

		if route.Path == "/docs" {
			hasUI = true
		}
	}

	assert.False(t, hasSpec)
	assert.True(t, hasUI)
}

func TestOpenAPIGenerator_CompleteSpec(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Add routes with full metadata
	router.GET("/users/:id", func(ctx Context) error {
		return nil
	},
		WithSummary("Get user by ID"),
		WithDescription("Retrieves a single user"),
		WithOperationID("getUser"),
		WithTags("users"),
		WithParameter("id", "path", "User ID", true, "123"),
		WithResponse(200, "Success", map[string]string{"id": "123"}),
		WithResponse(404, "Not found", nil),
		WithSecurity("bearerAuth"),
	)

	config := OpenAPIConfig{
		Title:       "User API",
		Description: "API for managing users",
		Version:     "2.0.0",
		Servers: []OpenAPIServer{
			{
				URL:         "https://api.example.com",
				Description: "Production server",
			},
		},
		Security: map[string]SecurityScheme{
			"bearerAuth": {
				Type:   "http",
				Scheme: "bearer",
			},
		},
		Tags: []OpenAPITag{
			{
				Name:        "users",
				Description: "User operations",
			},
		},
		Contact: &Contact{
			Name:  "API Team",
			Email: "api@example.com",
		},
		License: &License{
			Name: "MIT",
			URL:  "https://opensource.org/licenses/MIT",
		},
	}

	gen := newOpenAPIGenerator(config, router, nil)
	spec, _ := gen.Generate()

	// Verify complete spec structure
	assert.Equal(t, "3.1.0", spec.OpenAPI)
	assert.Equal(t, "User API", spec.Info.Title)
	assert.Equal(t, "API for managing users", spec.Info.Description)
	assert.Equal(t, "2.0.0", spec.Info.Version)
	assert.NotNil(t, spec.Info.Contact)
	assert.NotNil(t, spec.Info.License)
	assert.Len(t, spec.Servers, 1)
	assert.Len(t, spec.Tags, 1)
	assert.NotNil(t, spec.Components)
	assert.NotNil(t, spec.Components.SecuritySchemes)
}

func TestOpenAPIGenerator_AnyMethod(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Register a route using .Any()
	router.Any("/resource", func(ctx Context) error {
		return ctx.String(http.StatusOK, "ok")
	}, WithSummary("Handle resource"), WithTags("resource"))

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	spec, err := gen.Generate()
	require.NoError(t, err)
	require.NotNil(t, spec)

	// Verify that all HTTP methods are documented
	pathItem := spec.Paths["/resource"]
	require.NotNil(t, pathItem, "Path /resource should exist in OpenAPI spec")

	// Check that all 7 methods are documented
	assert.NotNil(t, pathItem.Get, "GET operation should be documented")
	assert.NotNil(t, pathItem.Post, "POST operation should be documented")
	assert.NotNil(t, pathItem.Put, "PUT operation should be documented")
	assert.NotNil(t, pathItem.Delete, "DELETE operation should be documented")
	assert.NotNil(t, pathItem.Patch, "PATCH operation should be documented")
	assert.NotNil(t, pathItem.Options, "OPTIONS operation should be documented")
	assert.NotNil(t, pathItem.Head, "HEAD operation should be documented")

	// Verify that all operations have the same summary and tags
	operations := []*Operation{
		pathItem.Get,
		pathItem.Post,
		pathItem.Put,
		pathItem.Delete,
		pathItem.Patch,
		pathItem.Options,
		pathItem.Head,
	}

	for i, op := range operations {
		assert.Equal(t, "Handle resource", op.Summary, "Operation %d should have correct summary", i)
		assert.Contains(t, op.Tags, "resource", "Operation %d should have 'resource' tag", i)
	}
}

func TestOpenAPIGenerator_AnyMethod_WithOpinionatedHandler(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	type TestRequest struct {
		Name string `json:"name"`
	}

	type TestResponse struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	// Register an opinionated handler using .Any()
	router.Any("/items", func(ctx Context, req *TestRequest) (*TestResponse, error) {
		return &TestResponse{
			ID:   "123",
			Name: req.Name,
		}, nil
	}, WithSummary("Handle item"), WithDescription("Create or modify item"))

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	spec, err := gen.Generate()
	require.NoError(t, err)
	require.NotNil(t, spec)

	// Verify path exists
	pathItem := spec.Paths["/items"]
	require.NotNil(t, pathItem)

	// Check POST operation (opinionated handlers typically have request bodies)
	assert.NotNil(t, pathItem.Post)
	assert.Equal(t, "Handle item", pathItem.Post.Summary)
	assert.Equal(t, "Create or modify item", pathItem.Post.Description)

	// Verify request schema is generated
	if pathItem.Post.RequestBody != nil {
		assert.NotNil(t, pathItem.Post.RequestBody.Content)

		if content, ok := pathItem.Post.RequestBody.Content["application/json"]; ok {
			assert.NotNil(t, content.Schema)
		}
	}

	// Verify response schema is generated
	assert.NotNil(t, pathItem.Post.Responses)

	if resp, ok := pathItem.Post.Responses["200"]; ok {
		assert.NotNil(t, resp.Content)

		if content, ok := resp.Content["application/json"]; ok {
			assert.NotNil(t, content.Schema)
		}
	}
}

func TestOpenAPIGenerator_AnyMethod_OnGroup(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Create a group and use .Any()
	api := router.Group("/api", WithGroupTags("api"))
	api.Any("/health", func(ctx Context) error {
		return ctx.String(http.StatusOK, "healthy")
	}, WithSummary("Health check"))

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	spec, err := gen.Generate()
	require.NoError(t, err)
	require.NotNil(t, spec)

	// Verify the group prefix is applied
	pathItem := spec.Paths["/api/health"]
	require.NotNil(t, pathItem, "Path /api/health should exist")

	// Verify GET operation
	assert.NotNil(t, pathItem.Get)
	assert.Equal(t, "Health check", pathItem.Get.Summary)
	assert.Contains(t, pathItem.Get.Tags, "api", "Should inherit group tag")

	// Verify other methods also exist and have the tag
	assert.NotNil(t, pathItem.Post)
	assert.Contains(t, pathItem.Post.Tags, "api")
}

func TestOpenAPIGenerator_AnyMethod_PureHTTPHandler(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	// Register pure http.Handler using .Any()
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pure handler"))
	})

	router.Any("/pure", handler, WithSummary("Pure HTTP handler"), WithTags("pure"))

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router, nil)
	spec, err := gen.Generate()
	require.NoError(t, err)
	require.NotNil(t, spec)

	// Verify all methods are documented even for pure handlers
	pathItem := spec.Paths["/pure"]
	require.NotNil(t, pathItem)

	assert.NotNil(t, pathItem.Get)
	assert.NotNil(t, pathItem.Post)
	assert.NotNil(t, pathItem.Put)
	assert.NotNil(t, pathItem.Delete)

	// Verify metadata is preserved
	assert.Equal(t, "Pure HTTP handler", pathItem.Get.Summary)
	assert.Contains(t, pathItem.Get.Tags, "pure")
}
