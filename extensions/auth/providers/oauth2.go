package providers

import (
	"context"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
)

// OAuth2Provider implements OAuth 2.0 authentication.
// It validates OAuth2 access tokens and extracts scopes and permissions.
type OAuth2Provider struct {
	name        string
	description string
	flows       *auth.OAuthFlows
	validator   OAuth2TokenValidator
	container   forge.Container
}

// OAuth2TokenValidator validates an OAuth2 token and returns the auth context.
// The validator should verify the token with the OAuth2 authorization server
// and extract claims, scopes, etc.
type OAuth2TokenValidator func(ctx context.Context, token string) (*auth.AuthContext, error)

// NewOAuth2Provider creates a new OAuth2 auth provider.
func NewOAuth2Provider(name string, flows *auth.OAuthFlows, opts ...OAuth2Option) auth.AuthProvider {
	p := &OAuth2Provider{
		name:        name,
		description: "OAuth 2.0 Authentication",
		flows:       flows,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

type OAuth2Option func(*OAuth2Provider)

// WithOAuth2Validator sets the validator function.
func WithOAuth2Validator(validator OAuth2TokenValidator) OAuth2Option {
	return func(p *OAuth2Provider) { p.validator = validator }
}

// WithOAuth2Description sets the OpenAPI description.
func WithOAuth2Description(desc string) OAuth2Option {
	return func(p *OAuth2Provider) { p.description = desc }
}

// WithOAuth2Container sets the DI container (for accessing services).
func WithOAuth2Container(container forge.Container) OAuth2Option {
	return func(p *OAuth2Provider) { p.container = container }
}

func (p *OAuth2Provider) Name() string {
	return p.name
}

func (p *OAuth2Provider) Type() auth.SecuritySchemeType {
	return auth.SecurityTypeOAuth2
}

func (p *OAuth2Provider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
	// OAuth2 typically uses bearer tokens
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

func (p *OAuth2Provider) OpenAPIScheme() auth.SecurityScheme {
	return auth.SecurityScheme{
		Type:        string(auth.SecurityTypeOAuth2),
		Description: p.description,
		Flows:       p.flows,
	}
}

func (p *OAuth2Provider) Middleware() forge.Middleware {
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
