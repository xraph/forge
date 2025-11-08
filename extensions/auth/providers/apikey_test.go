package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/extensions/auth"
	"github.com/xraph/forge/internal/errors"
)

func TestAPIKeyProvider_Authenticate_Header(t *testing.T) {
	called := false
	validator := func(ctx context.Context, apiKey string) (*auth.AuthContext, error) {
		called = true

		if apiKey == "valid-key" {
			return &auth.AuthContext{Subject: "user-123"}, nil
		}

		return nil, auth.ErrInvalidCredentials
	}

	provider := NewAPIKeyProvider("test",
		WithAPIKeyHeader("X-API-Key"),
		WithAPIKeyValidator(validator),
	)

	// Test with valid key in header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-API-Key", "valid-key")

	ctx, err := provider.Authenticate(context.Background(), req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if ctx.Subject != "user-123" {
		t.Errorf("Expected subject 'user-123', got %s", ctx.Subject)
	}

	if !called {
		t.Error("Expected validator to be called")
	}

	// Test with invalid key
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-API-Key", "invalid-key")

	_, err = provider.Authenticate(context.Background(), req)
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("Expected ErrInvalidCredentials, got %v", err)
	}

	// Test without key
	req = httptest.NewRequest(http.MethodGet, "/test", nil)

	_, err = provider.Authenticate(context.Background(), req)
	if !errors.Is(err, auth.ErrMissingCredentials) {
		t.Errorf("Expected ErrMissingCredentials, got %v", err)
	}
}

func TestAPIKeyProvider_Authenticate_Query(t *testing.T) {
	validator := func(ctx context.Context, apiKey string) (*auth.AuthContext, error) {
		if apiKey == "valid-key" {
			return &auth.AuthContext{Subject: "user-123"}, nil
		}

		return nil, auth.ErrInvalidCredentials
	}

	provider := NewAPIKeyProvider("test",
		WithAPIKeyQuery("api_key"),
		WithAPIKeyValidator(validator),
	)

	// Test with valid key in query
	req := httptest.NewRequest(http.MethodGet, "/test?api_key=valid-key", nil)

	ctx, err := provider.Authenticate(context.Background(), req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if ctx.Subject != "user-123" {
		t.Errorf("Expected subject 'user-123', got %s", ctx.Subject)
	}
}

func TestAPIKeyProvider_Authenticate_Cookie(t *testing.T) {
	validator := func(ctx context.Context, apiKey string) (*auth.AuthContext, error) {
		if apiKey == "valid-key" {
			return &auth.AuthContext{Subject: "user-123"}, nil
		}

		return nil, auth.ErrInvalidCredentials
	}

	provider := NewAPIKeyProvider("test",
		WithAPIKeyCookie("api_key"),
		WithAPIKeyValidator(validator),
	)

	// Test with valid key in cookie
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "api_key", Value: "valid-key"})

	ctx, err := provider.Authenticate(context.Background(), req)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if ctx.Subject != "user-123" {
		t.Errorf("Expected subject 'user-123', got %s", ctx.Subject)
	}
}

func TestAPIKeyProvider_OpenAPIScheme(t *testing.T) {
	// Test header scheme
	provider := NewAPIKeyProvider("test",
		WithAPIKeyHeader("X-API-Key"),
	)

	scheme := provider.OpenAPIScheme()
	if scheme.Type != string(auth.SecurityTypeAPIKey) {
		t.Errorf("Expected type apiKey, got %s", scheme.Type)
	}

	if scheme.In != "header" {
		t.Errorf("Expected in=header, got %s", scheme.In)
	}

	if scheme.Name != "X-API-Key" {
		t.Errorf("Expected name=X-API-Key, got %s", scheme.Name)
	}

	// Test query scheme
	provider = NewAPIKeyProvider("test",
		WithAPIKeyQuery("api_key"),
	)

	scheme = provider.OpenAPIScheme()
	if scheme.In != "query" {
		t.Errorf("Expected in=query, got %s", scheme.In)
	}

	if scheme.Name != "api_key" {
		t.Errorf("Expected name=api_key, got %s", scheme.Name)
	}
}
