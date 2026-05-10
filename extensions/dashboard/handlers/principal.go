package handlers

import (
	"encoding/json"
	"net/http"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

// principalResponse is the wire shape for GET /api/dashboard/v1/principal.
type principalResponse struct {
	Subject     string   `json:"subject"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email,omitempty"`
	Roles       []string `json:"roles"`
	Scopes      []string `json:"scopes"`
}

// HandleAPIPrincipalHTTP returns the current user's principal info as JSON.
// 401 when no user is in context.
func HandleAPIPrincipalHTTP(w http.ResponseWriter, r *http.Request) {
	user := dashauth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	resp := principalResponse{
		Subject:     user.Subject,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Roles:       append([]string{}, user.Roles...),
		Scopes:      append([]string{}, user.Scopes...),
	}
	if resp.DisplayName == "" {
		resp.DisplayName = resp.Subject
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
