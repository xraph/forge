package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CallbackRequest demonstrates the user's original use case.
type CallbackRequest struct {
	Provider         string `json:"-"                          path:"provider"           validate:"required"`
	State            string `json:"state"                      query:"state"             validate:"required"`
	Code             string `json:"code"                       query:"code"              validate:"required"`
	Error            string `json:"error,omitempty"            query:"error"`
	ErrorDescription string `json:"errorDescription,omitempty" query:"error_description"`
}

// TestCallbackRequest_EchoStyle tests the callback route using Echo-style :provider syntax.
func TestCallbackRequest_EchoStyle(t *testing.T) {
	router := NewRouter()

	// Register callback handler with Echo-style :provider
	err := router.GET("/callback/:provider", func(ctx Context) error {
		var req CallbackRequest
		if err := ctx.BindRequest(&req); err != nil {
			return ctx.JSON(400, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]string{
			"provider": req.Provider,
			"state":    req.State,
			"code":     req.Code,
		})
	})
	require.NoError(t, err)

	tests := []struct {
		name         string
		path         string
		expectStatus int
		expectBody   string
	}{
		{
			name:         "google provider",
			path:         "/callback/google?state=abc123&code=auth-code-123",
			expectStatus: 200,
			expectBody:   `"provider":"google"`,
		},
		{
			name:         "github provider",
			path:         "/callback/github?state=xyz789&code=gh-code-456",
			expectStatus: 200,
			expectBody:   `"provider":"github"`,
		},
		{
			name:         "missing state parameter",
			path:         "/callback/google?code=auth-code-123",
			expectStatus: 400,
			expectBody:   `error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.expectBody)
		})
	}
}

// TestCallbackRequest_ChiStyle tests the callback route using Chi-style {provider} syntax.
func TestCallbackRequest_ChiStyle(t *testing.T) {
	router := NewRouter()

	// Register callback handler with Chi/Gorilla-style {provider}
	err := router.GET("/callback/{provider}", func(ctx Context) error {
		var req CallbackRequest
		if err := ctx.BindRequest(&req); err != nil {
			return ctx.JSON(400, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]string{
			"provider": req.Provider,
			"state":    req.State,
			"code":     req.Code,
		})
	})
	require.NoError(t, err)

	tests := []struct {
		name         string
		path         string
		expectStatus int
		expectBody   string
	}{
		{
			name:         "google provider",
			path:         "/callback/google?state=abc123&code=auth-code-123",
			expectStatus: 200,
			expectBody:   `"provider":"google"`,
		},
		{
			name:         "github provider",
			path:         "/callback/github?state=xyz789&code=gh-code-456",
			expectStatus: 200,
			expectBody:   `"provider":"github"`,
		},
		{
			name:         "missing state parameter",
			path:         "/callback/google?code=auth-code-123",
			expectStatus: 400,
			expectBody:   `error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.expectBody)
		})
	}
}

// TestCallbackRequest_MixedStyles tests that both styles can coexist in the same router.
func TestCallbackRequest_MixedStyles(t *testing.T) {
	router := NewRouter()

	// Echo-style route
	err := router.GET("/auth/callback/:provider", func(ctx Context) error {
		var req CallbackRequest
		if err := ctx.BindRequest(&req); err != nil {
			return ctx.JSON(400, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]string{
			"provider": req.Provider,
			"style":    "echo",
		})
	})
	require.NoError(t, err)

	// Chi-style route
	err = router.GET("/oauth/callback/{provider}", func(ctx Context) error {
		var req CallbackRequest
		if err := ctx.BindRequest(&req); err != nil {
			return ctx.JSON(400, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]string{
			"provider": req.Provider,
			"style":    "chi",
		})
	})
	require.NoError(t, err)

	// Test Echo-style route
	req1 := httptest.NewRequest(http.MethodGet, "/auth/callback/google?state=abc&code=123", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	assert.Equal(t, 200, rec1.Code)
	assert.Contains(t, rec1.Body.String(), `"style":"echo"`)
	assert.Contains(t, rec1.Body.String(), `"provider":"google"`)

	// Test Chi-style route
	req2 := httptest.NewRequest(http.MethodGet, "/oauth/callback/github?state=xyz&code=456", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	assert.Equal(t, 200, rec2.Code)
	assert.Contains(t, rec2.Body.String(), `"style":"chi"`)
	assert.Contains(t, rec2.Body.String(), `"provider":"github"`)
}

// TestPathConversion_ComplexRoutes tests various complex route patterns.
func TestPathConversion_ComplexRoutes(t *testing.T) {
	tests := []struct {
		route    string
		request  string
		expected map[string]string
	}{
		{
			route:   "/users/:userId/posts/{postId}",
			request: "/users/john/posts/42",
			expected: map[string]string{
				"userId": "john",
				"postId": "42",
			},
		},
		{
			route:   "/api/{version}/users/:id",
			request: "/api/v2/users/123",
			expected: map[string]string{
				"version": "v2",
				"id":      "123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.route, func(t *testing.T) {
			router := NewRouter()

			err := router.GET(tt.route, func(ctx Context) error {
				result := make(map[string]string)
				for key := range tt.expected {
					result[key] = ctx.Param(key)
				}

				return ctx.JSON(200, result)
			})
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodGet, tt.request, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, 200, rec.Code)

			for key, expectedValue := range tt.expected {
				assert.Contains(t, rec.Body.String(), `"`+key+`":"`+expectedValue+`"`,
					"Expected %s=%s in response", key, expectedValue)
			}
		})
	}
}

// TestPathConversion_OAuthRealWorld tests a real-world OAuth callback scenario.
func TestPathConversion_OAuthRealWorld(t *testing.T) {
	router := NewRouter()

	// Simulate OAuth callback endpoints for multiple providers
	// Using Chi-style {provider} syntax (more common in OAuth implementations)
	err := router.GET("/auth/{provider}/callback", func(ctx Context) error {
		var req CallbackRequest
		if err := ctx.BindRequest(&req); err != nil {
			return ctx.JSON(400, map[string]any{
				"error":             "validation_failed",
				"error_description": err.Error(),
			})
		}

		// Handle OAuth error response
		if req.Error != "" {
			return ctx.JSON(400, map[string]string{
				"error":             req.Error,
				"error_description": req.ErrorDescription,
			})
		}

		// Successful callback
		return ctx.JSON(200, map[string]string{
			"provider": req.Provider,
			"state":    req.State,
			"code":     req.Code,
			"message":  "OAuth callback received successfully",
		})
	})
	require.NoError(t, err)

	tests := []struct {
		name         string
		provider     string
		queryParams  string
		expectStatus int
		expectBody   []string
	}{
		{
			name:         "successful Google OAuth callback",
			provider:     "google",
			queryParams:  "state=random-state-123&code=4/0AbCD-EfGh",
			expectStatus: 200,
			expectBody:   []string{`"provider":"google"`, `"code":"4/0AbCD-EfGh"`},
		},
		{
			name:         "successful GitHub OAuth callback",
			provider:     "github",
			queryParams:  "state=gh-state-xyz&code=gho_16C7e42F292c6912E7710c838347Ae178B4a",
			expectStatus: 200,
			expectBody:   []string{`"provider":"github"`, `"message":"OAuth callback received successfully"`},
		},
		{
			name:         "OAuth error - access denied",
			provider:     "google",
			queryParams:  "error=access_denied&error_description=User+denied+access&state=abc&code=dummy",
			expectStatus: 400,
			expectBody:   []string{`"error":"access_denied"`, `"error_description":"User denied access"`},
		},
		{
			name:         "missing required state parameter",
			provider:     "google",
			queryParams:  "code=auth-code",
			expectStatus: 400,
			expectBody:   []string{`"error":"validation_failed"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/auth/" + tt.provider + "/callback"
			if tt.queryParams != "" {
				path += "?" + tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectStatus, rec.Code, "Response: %s", rec.Body.String())

			for _, expected := range tt.expectBody {
				assert.Contains(t, rec.Body.String(), expected)
			}
		})
	}
}

// TestPathConversion_EdgeCases tests edge cases in path conversion.
func TestPathConversion_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		route string
		want  string
	}{
		{
			name:  "only braces",
			route: "/{id}",
			want:  "/:id",
		},
		{
			name:  "consecutive params",
			route: "/{a}/{b}/{c}",
			want:  "/:a/:b/:c",
		},
		{
			name:  "param at end",
			route: "/api/users/{id}",
			want:  "/api/users/:id",
		},
		{
			name:  "echo style unchanged",
			route: "/api/users/:id",
			want:  "/api/users/:id",
		},
		{
			name:  "mixed in complex path",
			route: "/v1/{org}/repos/:repo/files/{fileId}",
			want:  "/v1/:org/repos/:repo/files/:fileId",
		},
		{
			name:  "wildcard with params",
			route: "/{org}/files/*",
			want:  "/:org/files/*filepath",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertPathToBunRouter(tt.route)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestPathParamValidation_Integration tests full integration with validation.
func TestPathParamValidation_Integration(t *testing.T) {
	type ResourceRequest struct {
		OrgID      string `path:"orgId"      validate:"required"`
		ResourceID string `path:"resourceId" validate:"required"`
		Action     string `query:"action"    validate:"required"`
	}

	router := NewRouter()

	// Test with Chi-style braces
	err := router.POST("/orgs/{orgId}/resources/{resourceId}", func(ctx Context) error {
		var req ResourceRequest
		if err := ctx.BindRequest(&req); err != nil {
			// Validation errors should return 400
			return ctx.JSON(400, map[string]string{"error": err.Error()})
		}

		return ctx.JSON(200, map[string]string{
			"orgId":      req.OrgID,
			"resourceId": req.ResourceID,
			"action":     req.Action,
		})
	})
	require.NoError(t, err)

	// Success case
	req := httptest.NewRequest(http.MethodPost, "/orgs/acme/resources/res-123?action=update", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), `"orgId":"acme"`)
	assert.Contains(t, rec.Body.String(), `"resourceId":"res-123"`)
	assert.Contains(t, rec.Body.String(), `"action":"update"`)
}
