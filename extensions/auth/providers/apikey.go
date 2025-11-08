package providers

import (
	"context"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
)

// APIKeyProvider implements API key authentication.
// API keys can be provided in headers, query parameters, or cookies.
type APIKeyProvider struct {
	name        string
	description string
	headerName  string
	queryParam  string
	cookieName  string
	validator   APIKeyValidator
	container   forge.Container
}

// APIKeyValidator validates an API key and returns the auth context.
// The validator has access to the DI container via the provider and can
// retrieve services like databases, caches, etc. for validation.
type APIKeyValidator func(ctx context.Context, apiKey string) (*auth.AuthContext, error)

// NewAPIKeyProvider creates a new API key auth provider.
// By default, it looks for the API key in the "X-API-Key" header.
func NewAPIKeyProvider(name string, opts ...APIKeyOption) auth.AuthProvider {
	p := &APIKeyProvider{
		name:        name,
		description: "API Key Authentication",
		headerName:  "X-API-Key",
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

type APIKeyOption func(*APIKeyProvider)

// WithAPIKeyHeader sets the header name to look for the API key.
func WithAPIKeyHeader(name string) APIKeyOption {
	return func(p *APIKeyProvider) { p.headerName = name }
}

// WithAPIKeyQuery sets the query parameter name to look for the API key.
func WithAPIKeyQuery(param string) APIKeyOption {
	return func(p *APIKeyProvider) { p.queryParam = param }
}

// WithAPIKeyCookie sets the cookie name to look for the API key.
func WithAPIKeyCookie(name string) APIKeyOption {
	return func(p *APIKeyProvider) { p.cookieName = name }
}

// WithAPIKeyValidator sets the validator function.
func WithAPIKeyValidator(validator APIKeyValidator) APIKeyOption {
	return func(p *APIKeyProvider) { p.validator = validator }
}

// WithAPIKeyDescription sets the OpenAPI description.
func WithAPIKeyDescription(desc string) APIKeyOption {
	return func(p *APIKeyProvider) { p.description = desc }
}

// WithAPIKeyContainer sets the DI container (for accessing services).
func WithAPIKeyContainer(container forge.Container) APIKeyOption {
	return func(p *APIKeyProvider) { p.container = container }
}

func (p *APIKeyProvider) Name() string {
	return p.name
}

func (p *APIKeyProvider) Type() auth.SecuritySchemeType {
	return auth.SecurityTypeAPIKey
}

func (p *APIKeyProvider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
	// Extract API key from header, query, or cookie (in that order)
	var apiKey string

	if p.headerName != "" {
		apiKey = r.Header.Get(p.headerName)
	}

	if apiKey == "" && p.queryParam != "" {
		apiKey = r.URL.Query().Get(p.queryParam)
	}

	if apiKey == "" && p.cookieName != "" {
		if cookie, err := r.Cookie(p.cookieName); err == nil {
			apiKey = cookie.Value
		}
	}

	if apiKey == "" {
		return nil, auth.ErrMissingCredentials
	}

	// Validate using the provided validator
	if p.validator == nil {
		return nil, auth.ErrInvalidConfiguration
	}

	return p.validator(ctx, apiKey)
}

func (p *APIKeyProvider) OpenAPIScheme() auth.SecurityScheme {
	// Determine location and name based on configuration
	in := "header"
	name := p.headerName

	if p.queryParam != "" {
		in = "query"
		name = p.queryParam
	} else if p.cookieName != "" {
		in = "cookie"
		name = p.cookieName
	}

	return auth.SecurityScheme{
		Type:        string(auth.SecurityTypeAPIKey),
		Description: p.description,
		Name:        name,
		In:          in,
	}
}

func (p *APIKeyProvider) Middleware() forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			authCtx, err := p.Authenticate(ctx.Context(), ctx.Request())
			if err != nil {
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			ctx.Set("auth_context", authCtx)

			return next(ctx)
		}
	}
}
