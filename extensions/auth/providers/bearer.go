package providers

import (
	"context"
	"net/http"
	"strings"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
)

// BearerTokenProvider implements Bearer token authentication (JWT, OAuth2, etc.).
// It extracts tokens from the Authorization header using the "Bearer" scheme.
type BearerTokenProvider struct {
	name         string
	description  string
	bearerFormat string // "JWT", "token", etc.
	validator    BearerTokenValidator
	container    forge.Container
}

// BearerTokenValidator validates a bearer token and returns the auth context.
// The validator can access services from the DI container for JWT verification,
// token introspection, etc.
type BearerTokenValidator func(ctx context.Context, token string) (*auth.AuthContext, error)

// NewBearerTokenProvider creates a new bearer token auth provider.
// By default, it expects JWT tokens.
func NewBearerTokenProvider(name string, opts ...BearerTokenOption) auth.AuthProvider {
	p := &BearerTokenProvider{
		name:         name,
		description:  "Bearer Token Authentication",
		bearerFormat: "JWT",
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

type BearerTokenOption func(*BearerTokenProvider)

// WithBearerFormat sets the bearer token format (e.g., "JWT", "token").
func WithBearerFormat(format string) BearerTokenOption {
	return func(p *BearerTokenProvider) { p.bearerFormat = format }
}

// WithBearerValidator sets the validator function.
func WithBearerValidator(validator BearerTokenValidator) BearerTokenOption {
	return func(p *BearerTokenProvider) { p.validator = validator }
}

// WithBearerDescription sets the OpenAPI description.
func WithBearerDescription(desc string) BearerTokenOption {
	return func(p *BearerTokenProvider) { p.description = desc }
}

// WithBearerContainer sets the DI container (for accessing services).
func WithBearerContainer(container forge.Container) BearerTokenOption {
	return func(p *BearerTokenProvider) { p.container = container }
}

func (p *BearerTokenProvider) Name() string {
	return p.name
}

func (p *BearerTokenProvider) Type() auth.SecuritySchemeType {
	return auth.SecurityTypeHTTP
}

func (p *BearerTokenProvider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, auth.ErrMissingCredentials
	}

	// Parse "Bearer <token>" format
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return nil, auth.ErrInvalidCredentials
	}

	token := parts[1]
	if token == "" {
		return nil, auth.ErrMissingCredentials
	}

	// Validate using the provided validator
	if p.validator == nil {
		return nil, auth.ErrInvalidConfiguration
	}

	return p.validator(ctx, token)
}

func (p *BearerTokenProvider) OpenAPIScheme() auth.SecurityScheme {
	return auth.SecurityScheme{
		Type:         string(auth.SecurityTypeHTTP),
		Description:  p.description,
		Scheme:       "bearer",
		BearerFormat: p.bearerFormat,
	}
}

func (p *BearerTokenProvider) Middleware() forge.Middleware {
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
