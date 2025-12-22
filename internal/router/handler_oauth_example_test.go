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

// TestOAuthFlowExample demonstrates the exact OAuth use case from the issue.
// This test verifies that handlers defined with the pattern:
//
//	func(ctx forge.Context, req *Request) (*Response, error)
//
// properly bind ALL data sources: path params, query params, headers, and body.
func TestOAuthFlowExample(t *testing.T) {
	// Simulating xid.ID for this test
	type XID string

	// Request struct matching the OAuth initiate request pattern
	type InitiateOAuthRequest struct {
		// Path parameters - these MUST be bound automatically
		WorkspaceID XID    `path:"workspace_id" validate:"required"`
		ProviderID  string `path:"provider_id" validate:"required"`

		// Body fields - these are bound from JSON body
		ConnectionID        XID      `json:"connectionId" validate:"required"`
		Scopes              []string `json:"scopes,omitempty" optional:"true"`
		FrontendCallbackURL string   `json:"frontendCallbackUrl,omitempty" optional:"true"`
		RedirectURL         string   `json:"redirectUrl,omitempty" optional:"true"` // deprecated
	}

	type InitiateOAuthResponse struct {
		AuthorizationURL string `json:"authorizationUrl"`
		State            string `json:"state"`
		WorkspaceID      XID    `json:"workspaceId"`
		ProviderID       string `json:"providerId"`
		ConnectionID     XID    `json:"connectionId"`
	}

	// Handler - note that it does NOT call ctx.BindRequest explicitly
	// The framework should handle this automatically
	handler := func(ctx Context, req *InitiateOAuthRequest) (*InitiateOAuthResponse, error) {
		// At this point, ALL fields should be populated:
		// - WorkspaceID and ProviderID from path params
		// - ConnectionID, Scopes, etc. from body

		return &InitiateOAuthResponse{
			AuthorizationURL: "https://github.com/login/oauth/authorize?client_id=xxx&state=yyy",
			State:            "random-state-token",
			WorkspaceID:      req.WorkspaceID,
			ProviderID:       req.ProviderID,
			ConnectionID:     req.ConnectionID,
		}, nil
	}

	// Create router and register the OAuth endpoint
	router := NewRouter()
	err := router.POST("/workspaces/:workspace_id/auth/oauth/:provider_id/initiate", handler)
	require.NoError(t, err)

	// Create request body
	bodyData := map[string]any{
		"connectionId":        "conn_abc123",
		"scopes":              []string{"repo", "user:email"},
		"frontendCallbackUrl": "https://app.example.com/oauth/complete",
	}
	bodyBytes, _ := json.Marshal(bodyData)

	// Make request with path params in URL
	req := httptest.NewRequest(
		"POST",
		"/workspaces/ws_xyz789/auth/oauth/github/initiate",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code, "Should return 200 OK")

	var resp InitiateOAuthResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err, "Response should be valid JSON")

	// Verify ALL data was bound correctly WITHOUT calling ctx.BindRequest
	assert.Equal(t, XID("ws_xyz789"), resp.WorkspaceID, "Path param workspace_id should be bound automatically")
	assert.Equal(t, "github", resp.ProviderID, "Path param provider_id should be bound automatically")
	assert.Equal(t, XID("conn_abc123"), resp.ConnectionID, "Body field connectionId should be bound automatically")
	assert.Contains(t, resp.AuthorizationURL, "github.com", "Should generate GitHub OAuth URL")
}

// TestOAuthFlowWithMissingPathParam verifies validation works for missing required path params.
func TestOAuthFlowWithMissingPathParam(t *testing.T) {
	type XID string

	type InitiateOAuthRequest struct {
		WorkspaceID  XID    `path:"workspace_id" validate:"required"`
		ProviderID   string `path:"provider_id" validate:"required"`
		ConnectionID XID    `json:"connectionId" validate:"required"`
	}

	type InitiateOAuthResponse struct {
		Success bool `json:"success"`
	}

	handler := func(ctx Context, req *InitiateOAuthRequest) (*InitiateOAuthResponse, error) {
		return &InitiateOAuthResponse{Success: true}, nil
	}

	router := NewRouter()
	// Register with only one path param (missing provider_id in path pattern)
	err := router.POST("/workspaces/:workspace_id/auth/oauth/initiate", handler)
	require.NoError(t, err)

	bodyData := map[string]any{
		"connectionId": "conn_abc123",
	}
	bodyBytes, _ := json.Marshal(bodyData)

	req := httptest.NewRequest(
		"POST",
		"/workspaces/ws_xyz789/auth/oauth/initiate",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return bad request because provider_id is required but not in the path
	assert.Equal(t, http.StatusBadRequest, w.Code, "Should return 400 when required path param is missing")
}

// TestOAuthFlowWithMissingBodyField verifies validation works for missing required body fields.
func TestOAuthFlowWithMissingBodyField(t *testing.T) {
	type XID string

	type InitiateOAuthRequest struct {
		WorkspaceID  XID    `path:"workspace_id" validate:"required"`
		ProviderID   string `path:"provider_id" validate:"required"`
		ConnectionID XID    `json:"connectionId" validate:"required"`
	}

	type InitiateOAuthResponse struct {
		Success bool `json:"success"`
	}

	handler := func(ctx Context, req *InitiateOAuthRequest) (*InitiateOAuthResponse, error) {
		return &InitiateOAuthResponse{Success: true}, nil
	}

	router := NewRouter()
	err := router.POST("/workspaces/:workspace_id/auth/oauth/:provider_id/initiate", handler)
	require.NoError(t, err)

	// Missing required connectionId in body
	bodyData := map[string]any{
		"scopes": []string{"repo"},
	}
	bodyBytes, _ := json.Marshal(bodyData)

	req := httptest.NewRequest(
		"POST",
		"/workspaces/ws_xyz789/auth/oauth/github/initiate",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return bad request because connectionId is required
	assert.Equal(t, http.StatusBadRequest, w.Code, "Should return 400 when required body field is missing")
}
