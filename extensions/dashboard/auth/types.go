// Package dashauth provides authentication and authorization abstractions for
// the dashboard extension. It is intentionally decoupled from the auth extension
// so the dashboard has no hard dependency on any specific auth provider.
package dashauth

import (
	"context"
	"net/http"
	"slices"
	"strings"
)

// AccessLevel defines the protection level for a dashboard page or route.
type AccessLevel int

const (
	// AccessPublic means the page is always accessible without authentication.
	AccessPublic AccessLevel = iota

	// AccessProtected means authentication is required; unauthenticated users
	// are redirected to the login page.
	AccessProtected

	// AccessPartial means the page is always accessible but may render
	// differently depending on authentication state. The page handler should
	// check UserFromContext to adapt its output.
	AccessPartial
)

// String returns the string representation of the access level.
func (a AccessLevel) String() string {
	switch a {
	case AccessPublic:
		return "public"
	case AccessProtected:
		return "protected"
	case AccessPartial:
		return "partial"
	default:
		return "public"
	}
}

// ParseAccessLevel parses a string into an AccessLevel.
// Accepted values: "public", "protected", "partial".
// Returns AccessPublic for unrecognized values.
func ParseAccessLevel(s string) AccessLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "protected":
		return AccessProtected
	case "partial":
		return AccessPartial
	case "public":
		return AccessPublic
	default:
		return AccessPublic
	}
}

// UserInfo represents an authenticated dashboard user. This type is decoupled
// from any specific auth provider — adapters convert provider-specific auth
// contexts into UserInfo.
type UserInfo struct {
	// Subject is the unique user identifier (e.g. user ID, email, or sub claim).
	Subject string

	// DisplayName is the user's display name.
	DisplayName string

	// Email is the user's email address.
	Email string

	// AvatarURL is the URL of the user's avatar image.
	AvatarURL string

	// Roles holds the user's roles (e.g. "admin", "editor").
	Roles []string

	// Scopes holds the user's OAuth2 scopes or permission strings.
	Scopes []string

	// ProviderName identifies which auth provider authenticated this user.
	ProviderName string

	// Claims holds additional authentication claims.
	Claims map[string]any

	// Metadata holds provider-specific metadata.
	Metadata map[string]any
}

// Authenticated returns true if the user has a non-empty Subject.
func (u *UserInfo) Authenticated() bool {
	return u != nil && u.Subject != ""
}

// HasRole checks if the user has a specific role.
func (u *UserInfo) HasRole(role string) bool {
	if u == nil {
		return false
	}

	return slices.Contains(u.Roles, role)
}

// HasScope checks if the user has a specific scope.
func (u *UserInfo) HasScope(scope string) bool {
	if u == nil {
		return false
	}

	return slices.Contains(u.Scopes, scope)
}

// HasAnyRole checks if the user has any of the specified roles.
func (u *UserInfo) HasAnyRole(roles ...string) bool {
	if u == nil {
		return false
	}

	for _, role := range roles {
		if slices.Contains(u.Roles, role) {
			return true
		}
	}

	return false
}

// GetClaim retrieves a claim by key.
func (u *UserInfo) GetClaim(key string) (any, bool) {
	if u == nil || u.Claims == nil {
		return nil, false
	}

	val, ok := u.Claims[key]

	return val, ok
}

// Initials returns the user's initials (up to 2 characters) for avatar fallback.
func (u *UserInfo) Initials() string {
	if u == nil || u.DisplayName == "" {
		if u != nil && u.Email != "" {
			return strings.ToUpper(u.Email[:1])
		}

		return "?"
	}

	parts := strings.Fields(u.DisplayName)
	if len(parts) == 1 {
		return strings.ToUpper(parts[0][:1])
	}

	return strings.ToUpper(string(parts[0][0]) + string(parts[len(parts)-1][0]))
}

// AuthChecker validates authentication state from HTTP requests. It is the
// primary abstraction the dashboard uses to check whether a request is
// authenticated. Implementations typically delegate to an auth extension's
// registry or a custom authentication mechanism.
type AuthChecker interface {
	// CheckAuth inspects the request and returns a UserInfo if authenticated.
	// Returns nil (not an error) when the request is unauthenticated.
	// Returns an error only for infrastructure failures (e.g. token validation
	// service unreachable).
	CheckAuth(ctx context.Context, r *http.Request) (*UserInfo, error)
}

// AuthCheckerFunc is a function adapter for AuthChecker.
type AuthCheckerFunc func(ctx context.Context, r *http.Request) (*UserInfo, error)

// CheckAuth implements AuthChecker.
func (f AuthCheckerFunc) CheckAuth(ctx context.Context, r *http.Request) (*UserInfo, error) {
	return f(ctx, r)
}
