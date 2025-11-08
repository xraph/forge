package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
)

// mockRegistry implements auth.Registry for testing.
type mockRegistry struct {
	providers map[string]auth.AuthProvider
}

func (m *mockRegistry) Register(provider auth.AuthProvider) error {
	m.providers[provider.Name()] = provider

	return nil
}

func (m *mockRegistry) Unregister(name string) error {
	delete(m.providers, name)

	return nil
}

func (m *mockRegistry) Get(name string) (auth.AuthProvider, error) {
	if p, ok := m.providers[name]; ok {
		return p, nil
	}

	return nil, auth.ErrProviderNotFound
}

func (m *mockRegistry) List() []string {
	names := make([]string, 0, len(m.providers))
	for name := range m.providers {
		names = append(names, name)
	}

	return names
}

func (m *mockRegistry) Has(name string) bool {
	_, ok := m.providers[name]

	return ok
}

func (m *mockRegistry) Middleware(providerNames ...string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			return nil
		}
	}
}

func (m *mockRegistry) MiddlewareAnd(providerNames ...string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			return nil
		}
	}
}

func (m *mockRegistry) MiddlewareWithScopes(providerName string, scopes ...string) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			return nil
		}
	}
}

func (m *mockRegistry) OpenAPISchemes() map[string]auth.SecurityScheme {
	schemes := make(map[string]auth.SecurityScheme)
	for name, provider := range m.providers {
		schemes[name] = provider.OpenAPIScheme()
	}

	return schemes
}

// mockProvider implements auth.AuthProvider for testing.
type mockProvider struct {
	shouldSucceed bool
	authContext   *auth.AuthContext
}

func (m *mockProvider) Authenticate(ctx context.Context, r *http.Request) (*auth.AuthContext, error) {
	if m.shouldSucceed {
		return m.authContext, nil
	}

	return nil, auth.ErrAuthenticationFailed
}

func (m *mockProvider) Name() string {
	return "mock"
}

func (m *mockProvider) Type() auth.SecuritySchemeType {
	return auth.SecurityTypeHTTP
}

func (m *mockProvider) OpenAPIScheme() auth.SecurityScheme {
	return auth.SecurityScheme{
		Type:        string(auth.SecurityTypeHTTP),
		Description: "Mock auth",
	}
}

func (m *mockProvider) Middleware() forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return next
	}
}

func TestConnectionAuthenticator_AuthenticateConnection(t *testing.T) {
	tests := []struct {
		name          string
		providers     []string
		setupRegistry func() *mockRegistry
		wantErr       bool
	}{
		{
			name:      "successful authentication with jwt",
			providers: []string{"mock"},
			setupRegistry: func() *mockRegistry {
				reg := &mockRegistry{providers: make(map[string]auth.AuthProvider)}
				reg.Register(&mockProvider{
					shouldSucceed: true,
					authContext: &auth.AuthContext{
						Subject: "user123",
						Scopes:  []string{"read", "write"},
					},
				})

				return reg
			},
			wantErr: false,
		},
		{
			name:      "failed authentication",
			providers: []string{"mock"},
			setupRegistry: func() *mockRegistry {
				reg := &mockRegistry{providers: make(map[string]auth.AuthProvider)}
				reg.Register(&mockProvider{
					shouldSucceed: false,
				})

				return reg
			},
			wantErr: true,
		},
		{
			name:      "no providers configured",
			providers: []string{},
			setupRegistry: func() *mockRegistry {
				return &mockRegistry{providers: make(map[string]auth.AuthProvider)}
			},
			wantErr: true,
		},
		{
			name:      "provider not found",
			providers: []string{"nonexistent"},
			setupRegistry: func() *mockRegistry {
				return &mockRegistry{providers: make(map[string]auth.AuthProvider)}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := tt.setupRegistry()
			authenticator := NewConnectionAuthenticator(registry, tt.providers)

			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			ctx := context.Background()

			_, err := authenticator.AuthenticateConnection(ctx, req)
			if (err != nil) != tt.wantErr {
				t.Errorf("AuthenticateConnection() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConnectionAuthenticator_RequireScopes(t *testing.T) {
	registry := &mockRegistry{providers: make(map[string]auth.AuthProvider)}
	registry.Register(&mockProvider{
		shouldSucceed: true,
		authContext: &auth.AuthContext{
			Subject: "user123",
			Scopes:  []string{"read", "write"},
		},
	})

	authenticator := NewConnectionAuthenticator(registry, []string{"mock"})

	tests := []struct {
		name    string
		scopes  []string
		wantErr bool
	}{
		{
			name:    "has required scopes",
			scopes:  []string{"read", "write"},
			wantErr: false,
		},
		{
			name:    "missing scope",
			scopes:  []string{"read", "write", "admin"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authenticator.RequireScopes(tt.scopes...)
			if err != nil {
				t.Errorf("RequireScopes() error = %v", err)

				return
			}

			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			ctx := context.Background()

			authCtx, err := authenticator.AuthenticateConnection(ctx, req)
			hasScopes := authCtx != nil && authCtx.HasScopes(tt.scopes...)

			if hasScopes == tt.wantErr {
				t.Errorf("scope check failed: got %v, wantErr %v", hasScopes, tt.wantErr)
			}
		})
	}
}

func BenchmarkConnectionAuthenticator_AuthenticateConnection(b *testing.B) {
	registry := &mockRegistry{providers: make(map[string]auth.AuthProvider)}
	registry.Register(&mockProvider{
		shouldSucceed: true,
		authContext: &auth.AuthContext{
			Subject: "user123",
			Scopes:  []string{"read", "write"},
		},
	})

	authenticator := NewConnectionAuthenticator(registry, []string{"mock"})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	ctx := context.Background()

	for b.Loop() {
		_, _ = authenticator.AuthenticateConnection(ctx, req)
	}
}
