package auth

import (
	"context"
	"net/http"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/internal/shared"
)

// AuthProvider defines the interface for authentication providers.
// Implementations can access the DI container through closures or context
// to retrieve services needed for authentication (database, cache, etc.).
type AuthProvider interface {
	// Name returns the unique name/ID of this auth provider.
	// This name is used to reference the provider in route/group options.
	Name() string

	// Type returns the OpenAPI security scheme type
	Type() SecuritySchemeType

	// Authenticate validates the request and returns the authenticated subject.
	// Returns the auth context (user, claims, etc.) or an error.
	// The provider can access services from the DI container via closures.
	Authenticate(ctx context.Context, r *http.Request) (*AuthContext, error)

	// OpenAPIScheme returns the OpenAPI security scheme definition.
	// This is used to automatically generate OpenAPI security documentation.
	OpenAPIScheme() SecurityScheme

	// Middleware returns HTTP middleware for this provider.
	// The middleware is automatically applied when the provider is used.
	Middleware() forge.Middleware
}

// AuthContext holds authenticated user/service information.
// This is stored in the request context after successful authentication.
type AuthContext struct {
	// Subject is the authenticated entity (user ID, service ID, etc.)
	Subject string

	// Claims holds additional authentication claims (roles, permissions, etc.)
	Claims map[string]interface{}

	// Scopes holds OAuth2 scopes or permission strings
	Scopes []string

	// Metadata holds provider-specific metadata
	Metadata map[string]interface{}

	// Data holds additional data from the authenticated provider
	Data any

	// ProviderName identifies which auth provider authenticated this request
	ProviderName string
}

// SecuritySchemeType represents OpenAPI 3.1 security scheme types
type SecuritySchemeType string

const (
	SecurityTypeAPIKey        SecuritySchemeType = "apiKey"
	SecurityTypeHTTP          SecuritySchemeType = "http"
	SecurityTypeOAuth2        SecuritySchemeType = "oauth2"
	SecurityTypeOpenIDConnect SecuritySchemeType = "openIdConnect"
	SecurityTypeMutualTLS     SecuritySchemeType = "mutualTLS"
)

// SecurityScheme represents an OpenAPI security scheme definition
// We use the shared type to ensure compatibility with OpenAPI generation
type SecurityScheme = shared.SecurityScheme

// OAuthFlows defines OAuth 2.0 flows
type OAuthFlows = shared.OAuthFlows

// OAuthFlow defines a single OAuth 2.0 flow
type OAuthFlow = shared.OAuthFlow

// ProviderFunc is a function adapter for simple auth providers
type ProviderFunc func(ctx context.Context, r *http.Request) (*AuthContext, error)

// HasScope checks if the auth context has a specific scope
func (a *AuthContext) HasScope(scope string) bool {
	for _, s := range a.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// HasScopes checks if the auth context has all specified scopes
func (a *AuthContext) HasScopes(scopes ...string) bool {
	for _, scope := range scopes {
		if !a.HasScope(scope) {
			return false
		}
	}
	return true
}

// GetClaim retrieves a claim by key
func (a *AuthContext) GetClaim(key string) (interface{}, bool) {
	if a.Claims == nil {
		return nil, false
	}
	val, ok := a.Claims[key]
	return val, ok
}

// GetClaimString retrieves a string claim
func (a *AuthContext) GetClaimString(key string) (string, bool) {
	val, ok := a.GetClaim(key)
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}
