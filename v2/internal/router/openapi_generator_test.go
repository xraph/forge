package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/v2/internal/di"
)

func TestNewOpenAPIGenerator(t *testing.T) {
	container := di.NewContainer()
	router := NewRouter(WithContainer(container))

	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := newOpenAPIGenerator(config, router)
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

	gen := newOpenAPIGenerator(config, router)
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

	gen := newOpenAPIGenerator(config, router)
	spec := gen.Generate()

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

	gen := newOpenAPIGenerator(config, router)
	spec := gen.Generate()

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
			gen := newOpenAPIGenerator(config, router)

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

	gen := newOpenAPIGenerator(config, router)
	spec := gen.Generate()

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

	gen := newOpenAPIGenerator(config, router)
	handler := gen.specHandler()

	// Create test request
	req := httptest.NewRequest("GET", "/openapi.json", nil)
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

	gen := newOpenAPIGenerator(config, router)
	handler := gen.uiHandler()

	// Create test request
	req := httptest.NewRequest("GET", "/swagger", nil)
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

	gen := newOpenAPIGenerator(config, router)
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

	gen := newOpenAPIGenerator(config, router)
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

	gen := newOpenAPIGenerator(config, router)
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

	gen := newOpenAPIGenerator(config, router)
	spec := gen.Generate()

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
