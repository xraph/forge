package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

// mockProvider is a simple mock auth provider for testing.
type mockProvider struct {
	name       string
	authFunc   func(ctx context.Context, r *http.Request) (*AuthContext, error)
	schemeType SecuritySchemeType
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Type() SecuritySchemeType {
	return m.schemeType
}

func (m *mockProvider) Authenticate(ctx context.Context, r *http.Request) (*AuthContext, error) {
	if m.authFunc != nil {
		return m.authFunc(ctx, r)
	}

	return &AuthContext{Subject: "test-user"}, nil
}

func (m *mockProvider) OpenAPIScheme() SecurityScheme {
	return SecurityScheme{
		Type:        string(m.schemeType),
		Description: "Test provider",
	}
}

func (m *mockProvider) Middleware() forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return next
	}
}

func TestRegistry_Register(t *testing.T) {
	testLogger := logger.NewTestLogger()
	registry := NewRegistry(nil, testLogger)

	provider := &mockProvider{name: "test", schemeType: SecurityTypeAPIKey}

	// Test successful registration
	err := registry.Register(provider)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Test duplicate registration
	err = registry.Register(provider)
	if err == nil {
		t.Error("Expected error for duplicate registration, got nil")
	}

	// Test empty name
	emptyProvider := &mockProvider{name: "", schemeType: SecurityTypeAPIKey}

	err = registry.Register(emptyProvider)
	if err == nil {
		t.Error("Expected error for empty provider name, got nil")
	}
}

func TestRegistry_GetAndHas(t *testing.T) {
	testLogger := logger.NewTestLogger()
	registry := NewRegistry(nil, testLogger)

	provider := &mockProvider{name: "test", schemeType: SecurityTypeAPIKey}
	registry.Register(provider)

	// Test Get existing provider
	retrieved, err := registry.Get("test")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if retrieved.Name() != "test" {
		t.Errorf("Expected provider name 'test', got %s", retrieved.Name())
	}

	// Test Get non-existent provider
	_, err = registry.Get("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent provider, got nil")
	}

	// Test Has
	if !registry.Has("test") {
		t.Error("Expected registry to have 'test' provider")
	}

	if registry.Has("nonexistent") {
		t.Error("Expected registry to not have 'nonexistent' provider")
	}
}

func TestRegistry_List(t *testing.T) {
	testLogger := logger.NewTestLogger()
	registry := NewRegistry(nil, testLogger)

	provider1 := &mockProvider{name: "provider1", schemeType: SecurityTypeAPIKey}
	provider2 := &mockProvider{name: "provider2", schemeType: SecurityTypeHTTP}

	registry.Register(provider1)
	registry.Register(provider2)

	list := registry.List()
	if len(list) != 2 {
		t.Errorf("Expected 2 providers, got %d", len(list))
	}

	// Check both providers are in list
	hasProvider1 := false
	hasProvider2 := false

	for _, name := range list {
		if name == "provider1" {
			hasProvider1 = true
		}

		if name == "provider2" {
			hasProvider2 = true
		}
	}

	if !hasProvider1 {
		t.Error("Expected provider1 in list")
	}

	if !hasProvider2 {
		t.Error("Expected provider2 in list")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	testLogger := logger.NewTestLogger()
	registry := NewRegistry(nil, testLogger)

	provider := &mockProvider{name: "test", schemeType: SecurityTypeAPIKey}
	registry.Register(provider)

	// Test successful unregistration
	err := registry.Unregister("test")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify it's removed
	if registry.Has("test") {
		t.Error("Expected provider to be removed")
	}

	// Test unregistering non-existent provider
	err = registry.Unregister("nonexistent")
	if err == nil {
		t.Error("Expected error for unregistering non-existent provider, got nil")
	}
}

func TestRegistry_OpenAPISchemes(t *testing.T) {
	testLogger := logger.NewTestLogger()
	registry := NewRegistry(nil, testLogger)

	provider1 := &mockProvider{name: "api-key", schemeType: SecurityTypeAPIKey}
	provider2 := &mockProvider{name: "bearer", schemeType: SecurityTypeHTTP}

	registry.Register(provider1)
	registry.Register(provider2)

	schemes := registry.OpenAPISchemes()
	if len(schemes) != 2 {
		t.Errorf("Expected 2 schemes, got %d", len(schemes))
	}

	if _, ok := schemes["api-key"]; !ok {
		t.Error("Expected api-key scheme in schemes")
	}

	if _, ok := schemes["bearer"]; !ok {
		t.Error("Expected bearer scheme in schemes")
	}
}
