package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for response body unwrapping.

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PageMeta struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

type PaginatedWorkspaces struct {
	Data []Workspace `json:"data"`
	Meta *PageMeta   `json:"meta,omitempty"`
}

// Response with body:"" tag - should unwrap.
type ListWorkspacesResponse struct {
	Body PaginatedWorkspaces `body:""`
}

// Response with headers and body:"" tag.
type ListWorkspacesWithHeadersResponse struct {
	CacheControl string              `header:"Cache-Control"`
	XRequestID   string              `header:"X-Request-ID"`
	Body         PaginatedWorkspaces `body:""`
}

// Traditional response - no unwrapping.
type TraditionalResponse struct {
	Data []Workspace `json:"data"`
	Meta *PageMeta   `json:"meta,omitempty"`
}

// Request type (empty for GET).
type emptyRequest struct{}

func TestResponseBodyUnwrap(t *testing.T) {
	router := NewRouter()

	err := router.GET("/workspaces", func(ctx Context, req *emptyRequest) (*ListWorkspacesResponse, error) {
		return &ListWorkspacesResponse{
			Body: PaginatedWorkspaces{
				Data: []Workspace{
					{ID: "ws-1", Name: "Workspace 1"},
					{ID: "ws-2", Name: "Workspace 2"},
				},
				Meta: &PageMeta{Page: 1, Limit: 20, Total: 2},
			},
		}, nil
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Response should be unwrapped - no "Body" wrapper
	var resp PaginatedWorkspaces
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Len(t, resp.Data, 2)
	assert.Equal(t, "ws-1", resp.Data[0].ID)
	assert.Equal(t, "Workspace 1", resp.Data[0].Name)
	assert.NotNil(t, resp.Meta)
	assert.Equal(t, 1, resp.Meta.Page)

	// Verify there's no "Body" key in the raw JSON
	var rawResp map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &rawResp)
	require.NoError(t, err)
	_, hasBody := rawResp["Body"]
	assert.False(t, hasBody, "response should not have 'Body' wrapper key")
	_, hasData := rawResp["data"]
	assert.True(t, hasData, "response should have 'data' key directly")
}

func TestResponseBodyUnwrapWithHeaders(t *testing.T) {
	router := NewRouter()

	err := router.GET("/workspaces-with-headers", func(ctx Context, req *emptyRequest) (*ListWorkspacesWithHeadersResponse, error) {
		return &ListWorkspacesWithHeadersResponse{
			CacheControl: "max-age=3600",
			XRequestID:   "req-12345",
			Body: PaginatedWorkspaces{
				Data: []Workspace{
					{ID: "ws-1", Name: "Workspace 1"},
				},
				Meta: &PageMeta{Page: 1, Limit: 10, Total: 1},
			},
		}, nil
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/workspaces-with-headers", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Check headers were set
	assert.Equal(t, "max-age=3600", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "req-12345", rec.Header().Get("X-Request-ID"))

	// Response should be unwrapped
	var resp PaginatedWorkspaces
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Len(t, resp.Data, 1)
	assert.Equal(t, "ws-1", resp.Data[0].ID)
}

func TestResponseTraditionalNoUnwrap(t *testing.T) {
	router := NewRouter()

	err := router.GET("/traditional", func(ctx Context, req *emptyRequest) (*TraditionalResponse, error) {
		return &TraditionalResponse{
			Data: []Workspace{
				{ID: "ws-1", Name: "Workspace 1"},
			},
			Meta: &PageMeta{Page: 1, Limit: 10, Total: 1},
		}, nil
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/traditional", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Response should NOT be unwrapped - traditional struct
	var resp TraditionalResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Len(t, resp.Data, 1)
	assert.Equal(t, "ws-1", resp.Data[0].ID)
}

// Test with POST handler (combined pattern).
type CreateWorkspaceRequest struct {
	Name string `json:"name"`
}

type CreateWorkspaceResponse struct {
	XCreatedAt string    `header:"X-Created-At"`
	Body       Workspace `body:""`
}

func TestResponseBodyUnwrapPOST(t *testing.T) {
	router := NewRouter()

	err := router.POST("/workspaces", func(ctx Context, req *CreateWorkspaceRequest) (*CreateWorkspaceResponse, error) {
		return &CreateWorkspaceResponse{
			XCreatedAt: "2024-01-01T00:00:00Z",
			Body: Workspace{
				ID:   "ws-new",
				Name: req.Name,
			},
		}, nil
	})
	require.NoError(t, err)

	reqBody := CreateWorkspaceRequest{Name: "New Workspace"}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/workspaces", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Check header was set
	assert.Equal(t, "2024-01-01T00:00:00Z", rec.Header().Get("X-Created-At"))

	// Response should be unwrapped
	var resp Workspace
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "ws-new", resp.ID)
	assert.Equal(t, "New Workspace", resp.Name)

	// Verify there's no "Body" key in the raw JSON
	var rawResp map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &rawResp)
	require.NoError(t, err)
	_, hasBody := rawResp["Body"]
	assert.False(t, hasBody, "response should not have 'Body' wrapper key")
}

// Test that empty header values are not set.
type ResponseWithOptionalHeader struct {
	XOptional string              `header:"X-Optional"`
	Body      PaginatedWorkspaces `body:""`
}

func TestResponseOptionalHeaderNotSet(t *testing.T) {
	router := NewRouter()

	err := router.GET("/optional-header", func(ctx Context, req *emptyRequest) (*ResponseWithOptionalHeader, error) {
		return &ResponseWithOptionalHeader{
			XOptional: "", // Empty - should not be set
			Body: PaginatedWorkspaces{
				Data: []Workspace{{ID: "ws-1", Name: "Test"}},
			},
		}, nil
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/optional-header", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Empty header should not be set
	assert.Empty(t, rec.Header().Get("X-Optional"))

	// Body should still be unwrapped
	var resp PaginatedWorkspaces
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp.Data, 1)
}

// Test ctx.JSON() directly with body unwrapping (context handler pattern).
func TestContextJSON_BodyUnwrap(t *testing.T) {
	router := NewRouter()

	err := router.GET("/ctx-json", func(ctx Context) error {
		// Using ctx.JSON() directly with a response struct
		return ctx.JSON(200, &ListWorkspacesResponse{
			Body: PaginatedWorkspaces{
				Data: []Workspace{
					{ID: "ws-1", Name: "Workspace 1"},
				},
				Meta: &PageMeta{Page: 1, Limit: 10, Total: 1},
			},
		})
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/ctx-json", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Response should be unwrapped - no "Body" wrapper
	var resp PaginatedWorkspaces
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Len(t, resp.Data, 1)
	assert.Equal(t, "ws-1", resp.Data[0].ID)

	// Verify there's no "Body" key in the raw JSON
	var rawResp map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &rawResp)
	require.NoError(t, err)
	_, hasBody := rawResp["Body"]
	assert.False(t, hasBody, "response should not have 'Body' wrapper key")
}

// Test ctx.JSON() with headers and body unwrapping.
func TestContextJSON_HeadersAndBodyUnwrap(t *testing.T) {
	router := NewRouter()

	err := router.GET("/ctx-json-headers", func(ctx Context) error {
		return ctx.JSON(200, &ListWorkspacesWithHeadersResponse{
			CacheControl: "max-age=3600",
			XRequestID:   "req-abc123",
			Body: PaginatedWorkspaces{
				Data: []Workspace{
					{ID: "ws-1", Name: "Test"},
				},
			},
		})
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/ctx-json-headers", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Check headers were set
	assert.Equal(t, "max-age=3600", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "req-abc123", rec.Header().Get("X-Request-ID"))

	// Response should be unwrapped
	var resp PaginatedWorkspaces
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp.Data, 1)
}
