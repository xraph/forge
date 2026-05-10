package handlers

import (
	"encoding/json"
	"net/http"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

// PrincipalOptions configures the principal endpoint. AuthEnabled toggles
// between two response shapes:
//
//   - false: always 200 with `{authenticated:false}` (auth disabled, the shell
//     skips the login gate and renders the layout for an anonymous user).
//   - true:  200 with the populated principal when a user is in context, or
//     401 with `{code:"UNAUTHENTICATED",loginPath:...}` when not — the shell
//     interprets that as "render the LoginScreen".
//
// LoginPath is the absolute URL the shell should send users to for sign-in
// (e.g. /<basePath>/login). Only included in the 401 envelope; ignored when
// auth is disabled.
type PrincipalOptions struct {
	AuthEnabled bool
	LoginPath   string
}

// principalResponse is the wire shape for GET /api/dashboard/v1/principal.
type principalResponse struct {
	// Authenticated is the canonical signal the shell reads. When false the
	// rest of the fields are zero-valued.
	Authenticated bool     `json:"authenticated"`
	Subject       string   `json:"subject,omitempty"`
	DisplayName   string   `json:"displayName,omitempty"`
	Email         string   `json:"email,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
}

type unauthenticatedResponse struct {
	Code      string `json:"code"`
	LoginPath string `json:"loginPath,omitempty"`
}

// NewPrincipalHandler returns the GET /api/dashboard/v1/principal handler
// configured for a given dashboard. Slice (l) replaced the static handler so
// the React shell can distinguish "auth disabled" from "auth required, not
// signed in".
func NewPrincipalHandler(opts PrincipalOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := dashauth.UserFromContext(r.Context())
		if user == nil {
			if !opts.AuthEnabled {
				writeJSON(w, http.StatusOK, principalResponse{Authenticated: false})
				return
			}
			writeJSON(w, http.StatusUnauthorized, unauthenticatedResponse{
				Code:      "UNAUTHENTICATED",
				LoginPath: opts.LoginPath,
			})
			return
		}
		resp := principalResponse{
			Authenticated: true,
			Subject:       user.Subject,
			DisplayName:   user.DisplayName,
			Email:         user.Email,
			Roles:         append([]string{}, user.Roles...),
			Scopes:        append([]string{}, user.Scopes...),
		}
		if resp.DisplayName == "" {
			resp.DisplayName = resp.Subject
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// HandleAPIPrincipalHTTP is the auth-enabled default for callers that don't
// configure the handler explicitly. Kept for backwards compatibility with
// tests and any direct route registrations.
func HandleAPIPrincipalHTTP(w http.ResponseWriter, r *http.Request) {
	NewPrincipalHandler(PrincipalOptions{AuthEnabled: true})(w, r)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
