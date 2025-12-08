package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAPIConfig(t *testing.T) {
	config := OpenAPIConfig{
		Title:       "Test API",
		Description: "Test API Description",
		Version:     "1.0.0",
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
		UIEnabled:   true,
		SpecEnabled: true,
		PrettyJSON:  true,
	}

	assert.Equal(t, "Test API", config.Title)
	assert.Equal(t, "1.0.0", config.Version)
	assert.Len(t, config.Servers, 1)
	assert.Len(t, config.Security, 1)
	assert.Len(t, config.Tags, 1)
	assert.NotNil(t, config.Contact)
	assert.NotNil(t, config.License)
}

func TestSecurityScheme_HTTP(t *testing.T) {
	scheme := SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "JWT authentication",
	}

	assert.Equal(t, "http", scheme.Type)
	assert.Equal(t, "bearer", scheme.Scheme)
	assert.Equal(t, "JWT", scheme.BearerFormat)
}

func TestSecurityScheme_APIKey(t *testing.T) {
	scheme := SecurityScheme{
		Type:        "apiKey",
		Name:        "X-API-Key",
		In:          "header",
		Description: "API key authentication",
	}

	assert.Equal(t, "apiKey", scheme.Type)
	assert.Equal(t, "X-API-Key", scheme.Name)
	assert.Equal(t, "header", scheme.In)
}

func TestSecurityScheme_OAuth2(t *testing.T) {
	scheme := SecurityScheme{
		Type:        "oauth2",
		Description: "OAuth 2.0 authentication",
		Flows: &OAuthFlows{
			AuthorizationCode: &OAuthFlow{
				AuthorizationURL: "https://example.com/oauth/authorize",
				TokenURL:         "https://example.com/oauth/token",
				Scopes: map[string]string{
					"read":  "Read access",
					"write": "Write access",
				},
			},
		},
	}

	assert.Equal(t, "oauth2", scheme.Type)
	assert.NotNil(t, scheme.Flows)
	assert.NotNil(t, scheme.Flows.AuthorizationCode)
	assert.Len(t, scheme.Flows.AuthorizationCode.Scopes, 2)
}

func TestOpenAPISpec_Structure(t *testing.T) {
	spec := &OpenAPISpec{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Paths: make(map[string]*PathItem),
		Components: &Components{
			Schemas: make(map[string]*Schema),
		},
	}

	assert.Equal(t, "3.1.0", spec.OpenAPI)
	assert.Equal(t, "Test API", spec.Info.Title)
	assert.NotNil(t, spec.Paths)
	assert.NotNil(t, spec.Components)
}

func TestSchema_StringType(t *testing.T) {
	schema := &Schema{
		Type:        "string",
		Format:      "email",
		Description: "User email",
		Example:     "user@example.com",
		MinLength:   5,
		MaxLength:   100,
	}

	assert.Equal(t, "string", schema.Type)
	assert.Equal(t, "email", schema.Format)
	assert.Equal(t, 5, schema.MinLength)
	assert.Equal(t, 100, schema.MaxLength)
}

func TestSchema_NumberType(t *testing.T) {
	schema := &Schema{
		Type:             "number",
		Minimum:          0,
		Maximum:          100,
		ExclusiveMinimum: true,
		MultipleOf:       0.5,
	}

	assert.Equal(t, "number", schema.Type)
	assert.Equal(t, 0.0, schema.Minimum)
	assert.Equal(t, 100.0, schema.Maximum)
	assert.True(t, schema.ExclusiveMinimum)
}

func TestSchema_ArrayType(t *testing.T) {
	schema := &Schema{
		Type: "array",
		Items: &Schema{
			Type: "string",
		},
		MinItems:    1,
		MaxItems:    10,
		UniqueItems: true,
	}

	assert.Equal(t, "array", schema.Type)
	assert.NotNil(t, schema.Items)
	assert.Equal(t, 1, schema.MinItems)
	assert.Equal(t, 10, schema.MaxItems)
	assert.True(t, schema.UniqueItems)
}

func TestSchema_ObjectType(t *testing.T) {
	schema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"id": {
				Type:   "string",
				Format: "uuid",
			},
			"name": {
				Type: "string",
			},
		},
		Required: []string{"id", "name"},
	}

	assert.Equal(t, "object", schema.Type)
	assert.Len(t, schema.Properties, 2)
	assert.Len(t, schema.Required, 2)
}

func TestOperation(t *testing.T) {
	operation := &Operation{
		Summary:     "Get user by ID",
		Description: "Retrieves a single user",
		OperationID: "getUser",
		Tags:        []string{"users"},
		Parameters: []Parameter{
			{
				Name:        "id",
				In:          "path",
				Description: "User ID",
				Required:    true,
				Schema: &Schema{
					Type: "string",
				},
			},
		},
		Responses: map[string]*Response{
			"200": {
				Description: "Success",
				Content: map[string]*MediaType{
					"application/json": {
						Schema: &Schema{
							Type: "object",
						},
					},
				},
			},
		},
	}

	assert.Equal(t, "Get user by ID", operation.Summary)
	assert.Equal(t, "getUser", operation.OperationID)
	assert.Len(t, operation.Tags, 1)
	assert.Len(t, operation.Parameters, 1)
	assert.Len(t, operation.Responses, 1)
}

func TestRequestBody(t *testing.T) {
	requestBody := &RequestBody{
		Description: "User data",
		Required:    true,
		Content: map[string]*MediaType{
			"application/json": {
				Schema: &Schema{
					Type: "object",
					Properties: map[string]*Schema{
						"name":  {Type: "string"},
						"email": {Type: "string"},
					},
				},
			},
		},
	}

	assert.Equal(t, "User data", requestBody.Description)
	assert.True(t, requestBody.Required)
	assert.Len(t, requestBody.Content, 1)
}

func TestDiscriminator(t *testing.T) {
	discriminator := &Discriminator{
		PropertyName: "type",
		Mapping: map[string]string{
			"dog": "#/components/schemas/Dog",
			"cat": "#/components/schemas/Cat",
		},
	}

	assert.Equal(t, "type", discriminator.PropertyName)
	assert.Len(t, discriminator.Mapping, 2)
}

func TestWithOpenAPI(t *testing.T) {
	config := OpenAPIConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	opt := WithOpenAPI(config)
	assert.NotNil(t, opt)

	cfg := &routerConfig{}
	opt.Apply(cfg)
	assert.NotNil(t, cfg.openAPIConfig)
	assert.Equal(t, "Test API", cfg.openAPIConfig.Title)
}

func TestConvertPathToOpenAPIFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no params",
			input:    "/api/users",
			expected: "/api/users",
		},
		{
			name:     "single colon param",
			input:    "/api/users/:id",
			expected: "/api/users/{id}",
		},
		{
			name:     "multiple colon params",
			input:    "/api/workspaces/:workspace_id/users/:user_id",
			expected: "/api/workspaces/{workspace_id}/users/{user_id}",
		},
		{
			name:     "already curly brace format",
			input:    "/api/users/{id}",
			expected: "/api/users/{id}",
		},
		{
			name:     "mixed formats",
			input:    "/api/workspaces/:workspace_id/users/{user_id}/provisions/:provision_id",
			expected: "/api/workspaces/{workspace_id}/users/{user_id}/provisions/{provision_id}",
		},
		{
			name:     "complex nested path",
			input:    "/api/workspaces/:workspace_id/users/provisions/:provision_id/delete",
			expected: "/api/workspaces/{workspace_id}/users/provisions/{provision_id}/delete",
		},
		{
			name:     "root path",
			input:    "/",
			expected: "/",
		},
		{
			name:     "empty path",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertPathToOpenAPIFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOpenAPIGenerator_PathConversion(t *testing.T) {
	// Create a router with OpenAPI enabled
	r := NewRouter(
		WithOpenAPI(OpenAPIConfig{
			Title:       "Test API",
			Version:     "1.0.0",
			SpecEnabled: true,
		}),
	)

	// Register routes with colon-style path params
	_ = r.GET("/api/workspaces/:workspace_id/users", func(ctx Context) error {
		return ctx.JSON(200, map[string]string{"status": "ok"})
	})

	_ = r.DELETE("/api/workspaces/:workspace_id/users/provisions/:provision_id/delete", func(ctx Context) error {
		return ctx.JSON(200, map[string]string{"status": "deleted"})
	})

	// Also register a route with curly brace style
	_ = r.GET("/api/users/{user_id}", func(ctx Context) error {
		return ctx.JSON(200, map[string]string{"status": "ok"})
	})

	// Generate OpenAPI spec
	spec := r.OpenAPISpec()
	require.NotNil(t, spec, "OpenAPI spec should be generated")

	// Verify paths are converted to OpenAPI format
	_, hasColonPath := spec.Paths["/api/workspaces/:workspace_id/users"]
	assert.False(t, hasColonPath, "Colon-style path should NOT be in OpenAPI spec")

	_, hasOpenAPIPath := spec.Paths["/api/workspaces/{workspace_id}/users"]
	assert.True(t, hasOpenAPIPath, "OpenAPI-style path should be in OpenAPI spec")

	_, hasDeletePath := spec.Paths["/api/workspaces/{workspace_id}/users/provisions/{provision_id}/delete"]
	assert.True(t, hasDeletePath, "Complex nested path should be converted to OpenAPI format")

	_, hasCurlyPath := spec.Paths["/api/users/{user_id}"]
	assert.True(t, hasCurlyPath, "Curly brace path should remain unchanged")
}

func TestRouteOptionsOpenAPI(t *testing.T) {
	t.Run("WithSecurity", func(t *testing.T) {
		opt := WithSecurity("bearerAuth", "apiKey")
		config := &RouteConfig{
			Metadata: make(map[string]any),
		}
		opt.Apply(config)

		security, ok := config.Metadata["security"].([]string)
		require.True(t, ok)
		assert.Len(t, security, 2)
		assert.Equal(t, "bearerAuth", security[0])
	})

	t.Run("WithResponse", func(t *testing.T) {
		opt := WithResponse(200, "Success", map[string]string{"id": "123"})
		config := &RouteConfig{
			Metadata: make(map[string]any),
		}
		opt.Apply(config)

		responses, ok := config.Metadata["responses"].(map[int]*ResponseDef)
		require.True(t, ok)
		assert.Len(t, responses, 1)
		assert.Equal(t, "Success", responses[200].Description)
	})

	t.Run("WithRequestBody", func(t *testing.T) {
		example := map[string]string{"name": "John"}
		opt := WithRequestBody("User data", true, example)
		config := &RouteConfig{
			Metadata: make(map[string]any),
		}
		opt.Apply(config)

		body, ok := config.Metadata["requestBody"].(*RequestBodyDef)
		require.True(t, ok)
		assert.Equal(t, "User data", body.Description)
		assert.True(t, body.Required)
		assert.NotNil(t, body.Example)
	})

	t.Run("WithParameter", func(t *testing.T) {
		opt := WithParameter("id", "path", "User ID", true, "123")
		config := &RouteConfig{
			Metadata: make(map[string]any),
		}
		opt.Apply(config)

		params, ok := config.Metadata["parameters"].([]ParameterDef)
		require.True(t, ok)
		assert.Len(t, params, 1)
		assert.Equal(t, "id", params[0].Name)
		assert.Equal(t, "path", params[0].In)
		assert.True(t, params[0].Required)
	})

	t.Run("WithExternalDocs", func(t *testing.T) {
		opt := WithExternalDocs("More info", "https://docs.example.com")
		config := &RouteConfig{
			Metadata: make(map[string]any),
		}
		opt.Apply(config)

		docs, ok := config.Metadata["externalDocs"].(*ExternalDocsDef)
		require.True(t, ok)
		assert.Equal(t, "More info", docs.Description)
		assert.Equal(t, "https://docs.example.com", docs.URL)
	})
}
