package providers

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/extensions/auth"
)

// BasicAuthProvider implements HTTP Basic Authentication.
// It extracts username and password from the Authorization header.
type BasicAuthProvider struct {
	name        string
	description string
	validator   BasicAuthValidator
	container   forge.Container
}

// BasicAuthValidator validates username and password and returns the auth context.
// The validator can access services from the DI container to verify credentials
// against a database, LDAP, etc.
type BasicAuthValidator func(ctx context.Context, username, password string) (*auth.AuthContext, error)

// NewBasicAuthProvider creates a new HTTP Basic Auth provider.
func NewBasicAuthProvider(name string, opts ...BasicAuthOption) auth.AuthProvider {
	p := &BasicAuthProvider{
		name:        name,
		description: "HTTP Basic Authentication",
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

type BasicAuthOption func(*BasicAuthProvider)

// WithBasicAuthValidator sets the validator function
func WithBasicAuthValidator(validator BasicAuthValidator) BasicAuthOption {
	return func(p *BasicAuthProvider) { p.validator = validator }
}

// WithBasicAuthDescription sets the OpenAPI description
func WithBasicAuthDescription(desc string) BasicAuthOption {
	return func(p *BasicAuthProvider) { p.description = desc }
}

// WithBasicAuthContainer sets the DI container (for accessing services)
func WithBasicAuthContainer(container forge.Container) BasicAuthOption {
	return func(p *BasicAuthProvider) { p.container = container }
}

func (p *BasicAuthProvider) Name() string {
	return p.name
}

func (p *BasicAuthProvider) Type() auth.SecuritySchemeType {
	return auth.SecurityTypeHTTP
}

func (p *BasicAuthProvider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, auth.ErrMissingCredentials
	}

	// Parse "Basic <base64>" format
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "basic" {
		return nil, auth.ErrInvalidCredentials
	}

	// Decode base64 credentials
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, auth.ErrInvalidCredentials
	}

	// Parse "username:password" format
	credentials := strings.SplitN(string(decoded), ":", 2)
	if len(credentials) != 2 {
		return nil, auth.ErrInvalidCredentials
	}

	username := credentials[0]
	password := credentials[1]

	if username == "" || password == "" {
		return nil, auth.ErrMissingCredentials
	}

	// Validate using the provided validator
	if p.validator == nil {
		return nil, auth.ErrInvalidConfiguration
	}

	return p.validator(ctx, username, password)
}

func (p *BasicAuthProvider) OpenAPIScheme() auth.SecurityScheme {
	return auth.SecurityScheme{
		Type:        string(auth.SecurityTypeHTTP),
		Description: p.description,
		Scheme:      "basic",
	}
}

func (p *BasicAuthProvider) Middleware() forge.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCtx, err := p.Authenticate(r.Context(), r)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := auth.WithContext(r.Context(), authCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
