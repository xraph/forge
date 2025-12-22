package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/di"
)

// TestOpinionatedHandlerBindsAllSources verifies that opinionated handlers
// properly bind data from all sources: path, query, header, and body using the full router.
func TestOpinionatedHandlerBindsAllSources(t *testing.T) {
	type CompleteRequest struct {
		// Path parameters
		WorkspaceID string `path:"workspace_id" validate:"required"`
		ProviderID  string `path:"provider_id" validate:"required"`

		// Query parameters
		Page  int  `query:"page" default:"1"`
		Limit int  `query:"limit"`
		Debug bool `query:"debug,omitempty"`

		// Headers
		APIKey    string `header:"X-API-Key" validate:"required"`
		UserAgent string `header:"User-Agent,omitempty"`

		// Body fields
		Name        string   `json:"name" validate:"required"`
		Email       string   `json:"email"`
		Tags        []string `json:"tags,omitempty"`
		Description string   `json:"description,omitempty"`
	}

	type CompleteResponse struct {
		Message     string   `json:"message"`
		WorkspaceID string   `json:"workspaceId"`
		ProviderID  string   `json:"providerId"`
		Page        int      `json:"page"`
		Limit       int      `json:"limit"`
		Debug       bool     `json:"debug"`
		APIKey      string   `json:"apiKey"`
		UserAgent   string   `json:"userAgent"`
		Name        string   `json:"name"`
		Email       string   `json:"email"`
		Tags        []string `json:"tags"`
	}

	// Handler that echoes all bound data
	handler := func(ctx Context, req *CompleteRequest) (*CompleteResponse, error) {
		return &CompleteResponse{
			Message:     "all sources bound",
			WorkspaceID: req.WorkspaceID,
			ProviderID:  req.ProviderID,
			Page:        req.Page,
			Limit:       req.Limit,
			Debug:       req.Debug,
			APIKey:      req.APIKey,
			UserAgent:   req.UserAgent,
			Name:        req.Name,
			Email:       req.Email,
			Tags:        req.Tags,
		}, nil
	}

	// Create router and register handler
	router := NewRouter()
	err := router.POST("/workspaces/:workspace_id/providers/:provider_id", handler)
	require.NoError(t, err)

	// Create request with data from all sources
	bodyData := map[string]any{
		"name":  "Test User",
		"email": "test@example.com",
		"tags":  []string{"golang", "testing"},
	}
	bodyBytes, _ := json.Marshal(bodyData)

	req := httptest.NewRequest("POST", "/workspaces/ws123/providers/github?page=2&limit=50&debug=true", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret-key-123")
	req.Header.Set("User-Agent", "TestClient/1.0")

	// Create response recorder
	w := httptest.NewRecorder()

	// Execute request through router
	router.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var resp CompleteResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	// Verify all data was bound correctly
	assert.Equal(t, "all sources bound", resp.Message)
	assert.Equal(t, "ws123", resp.WorkspaceID, "path param workspace_id should be bound")
	assert.Equal(t, "github", resp.ProviderID, "path param provider_id should be bound")
	assert.Equal(t, 2, resp.Page, "query param page should be bound")
	assert.Equal(t, 50, resp.Limit, "query param limit should be bound")
	assert.Equal(t, true, resp.Debug, "query param debug should be bound")
	assert.Equal(t, "secret-key-123", resp.APIKey, "header X-API-Key should be bound")
	assert.Equal(t, "TestClient/1.0", resp.UserAgent, "header User-Agent should be bound")
	assert.Equal(t, "Test User", resp.Name, "body field name should be bound")
	assert.Equal(t, "test@example.com", resp.Email, "body field email should be bound")
	assert.Equal(t, []string{"golang", "testing"}, resp.Tags, "body field tags should be bound")
}

// TestOpinionatedHandlerPathParamsOnly verifies that handlers work with only path params.
func TestOpinionatedHandlerPathParamsOnly(t *testing.T) {
	type PathOnlyRequest struct {
		UserID string `path:"user_id" validate:"required"`
		ItemID string `path:"item_id" validate:"required"`
	}

	type SimpleResponse struct {
		UserID string `json:"userId"`
		ItemID string `json:"itemId"`
	}

	handler := func(ctx Context, req *PathOnlyRequest) (*SimpleResponse, error) {
		return &SimpleResponse{
			UserID: req.UserID,
			ItemID: req.ItemID,
		}, nil
	}

	router := NewRouter()
	err := router.GET("/users/:user_id/items/:item_id", handler)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/users/user123/items/item456", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SimpleResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "user123", resp.UserID)
	assert.Equal(t, "item456", resp.ItemID)
}

// TestOpinionatedHandlerValidationError verifies validation errors work correctly.
func TestOpinionatedHandlerValidationError(t *testing.T) {
	type ValidatedRequest struct {
		WorkspaceID string `path:"workspace_id" validate:"required"`
		Name        string `json:"name" validate:"required"`
		Email       string `json:"email" validate:"required"`
	}

	type ValidatedResponse struct {
		Success bool `json:"success"`
	}

	handler := func(ctx Context, req *ValidatedRequest) (*ValidatedResponse, error) {
		return &ValidatedResponse{Success: true}, nil
	}

	router := NewRouter()
	err := router.POST("/workspaces/:workspace_id", handler)
	require.NoError(t, err)

	// Missing required name field in body
	bodyData := map[string]any{
		"email": "test@example.com",
	}
	bodyBytes, _ := json.Marshal(bodyData)

	req := httptest.NewRequest("POST", "/workspaces/ws123", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return bad request due to missing required field
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCombinedHandlerBindsAllSources verifies combined pattern (ctx, svc, req) binds correctly.
func TestCombinedHandlerBindsAllSources(t *testing.T) {
	type TestBindingService struct {
		Name string
	}

	type CombinedRequest struct {
		TenantID string `path:"tenant_id" validate:"required"`
		UserID   string `json:"userId" validate:"required"`
	}

	type CombinedResponse struct {
		Service  string `json:"service"`
		TenantID string `json:"tenantId"`
		UserID   string `json:"userId"`
	}

	handler := func(ctx Context, svc *TestBindingService, req *CombinedRequest) (*CombinedResponse, error) {
		return &CombinedResponse{
			Service:  svc.Name,
			TenantID: req.TenantID,
			UserID:   req.UserID,
		}, nil
	}

	// Register service
	container := di.NewContainer()
	err := di.RegisterSingleton(container, "github.com/xraph/forge/internal/router.TestBindingService", func(c di.Container) (*TestBindingService, error) {
		return &TestBindingService{Name: "TestServiceInstance"}, nil
	})
	require.NoError(t, err)

	router := NewRouter(WithContainer(container))
	err = router.POST("/tenants/:tenant_id", handler)
	require.NoError(t, err)

	bodyData := map[string]any{
		"userId": "user456",
	}
	bodyBytes, _ := json.Marshal(bodyData)

	req := httptest.NewRequest("POST", "/tenants/tenant123", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CombinedResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "TestServiceInstance", resp.Service, "service should be injected")
	assert.Equal(t, "tenant123", resp.TenantID, "path param should be bound")
	assert.Equal(t, "user456", resp.UserID, "body field should be bound")
}

// TestOpinionatedHandlerGETWithQueryOnly verifies GET requests with query params only.
func TestOpinionatedHandlerGETWithQueryOnly(t *testing.T) {
	type SearchRequest struct {
		Q      string `query:"q" validate:"required"`
		Page   int    `query:"page" default:"1"`
		Limit  int    `query:"limit" default:"10"`
		Format string `query:"format,omitempty"`
	}

	type SearchResponse struct {
		Query  string `json:"query"`
		Page   int    `json:"page"`
		Limit  int    `json:"limit"`
		Format string `json:"format"`
	}

	handler := func(ctx Context, req *SearchRequest) (*SearchResponse, error) {
		return &SearchResponse{
			Query:  req.Q,
			Page:   req.Page,
			Limit:  req.Limit,
			Format: req.Format,
		}, nil
	}

	router := NewRouter()
	err := router.GET("/search", handler)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/search?q=golang&page=3&limit=25&format=json", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SearchResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "golang", resp.Query)
	assert.Equal(t, 3, resp.Page)
	assert.Equal(t, 25, resp.Limit)
	assert.Equal(t, "json", resp.Format)
}
