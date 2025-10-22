package providers

import (
	"context"
	"net/http"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/auth"
)

// OIDCProvider implements OpenID Connect authentication.
// It validates OIDC ID tokens and access tokens.
type OIDCProvider struct {
	name             string
	description      string
	openIdConnectUrl string
	validator        OIDCTokenValidator
	container        forge.Container
}

// OIDCTokenValidator validates an OIDC token and returns the auth context.
// The validator should verify the token with the OIDC provider and extract
// claims (sub, email, name, etc.).
type OIDCTokenValidator func(ctx context.Context, token string) (*auth.AuthContext, error)

// NewOIDCProvider creates a new OpenID Connect auth provider.
func NewOIDCProvider(name string, openIdConnectUrl string, opts ...OIDCOption) auth.AuthProvider {
	p := &OIDCProvider{
		name:             name,
		description:      "OpenID Connect Authentication",
		openIdConnectUrl: openIdConnectUrl,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

type OIDCOption func(*OIDCProvider)

// WithOIDCValidator sets the validator function
func WithOIDCValidator(validator OIDCTokenValidator) OIDCOption {
	return func(p *OIDCProvider) { p.validator = validator }
}

// WithOIDCDescription sets the OpenAPI description
func WithOIDCDescription(desc string) OIDCOption {
	return func(p *OIDCProvider) { p.description = desc }
}

// WithOIDCContainer sets the DI container (for accessing services)
func WithOIDCContainer(container forge.Container) OIDCOption {
	return func(p *OIDCProvider) { p.container = container }
}

func (p *OIDCProvider) Name() string {
	return p.name
}

func (p *OIDCProvider) Type() auth.SecuritySchemeType {
	return auth.SecurityTypeOpenIDConnect
}

func (p *OIDCProvider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
	// OIDC typically uses bearer tokens (ID tokens or access tokens)
	bearer := &BearerTokenProvider{
		validator: func(ctx context.Context, token string) (*auth.AuthContext, error) {
			if p.validator != nil {
				return p.validator(ctx, token)
			}
			return nil, auth.ErrInvalidConfiguration
		},
	}
	return bearer.Authenticate(ctx, r)
}

func (p *OIDCProvider) OpenAPIScheme() auth.SecurityScheme {
	return auth.SecurityScheme{
		Type:             string(auth.SecurityTypeOpenIDConnect),
		Description:      p.description,
		OpenIdConnectUrl: p.openIdConnectUrl,
	}
}

func (p *OIDCProvider) Middleware() forge.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCtx, err := p.Authenticate(r.Context(), r)
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := auth.WithContext(r.Context(), authCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
