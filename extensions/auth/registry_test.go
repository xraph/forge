package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"

	"github.com/xraph/forge/internal/logger"
)

// mockProvider is a simple mock auth provider for testing.
type mockProvider struct {
	name       string
	authFunc   func(ctx context.Context, r *http.Request) (*AuthContext, error)
	authCtx    *AuthContext
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

	if m.authCtx != nil {
		return m.authCtx, nil
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

// stubAuthorizer is a minimal Authorizer used to verify that Registry defers
// to whatever authorizer is installed. It is reused by later tests (the
// guard middleware) that need to assert an authorizer was actually invoked.
type stubAuthorizer struct{ called bool }

func (s *stubAuthorizer) Name() string { return "stub" }

func (s *stubAuthorizer) Authorize(context.Context, *AuthContext, Requirement) error {
	s.called = true

	return nil
}

func TestRegistryDefaultsToTheBuiltInAuthorizer(t *testing.T) {
	testLogger := logger.NewTestLogger()
	r := NewRegistry(nil, testLogger)

	if got := r.Authorizer().Name(); got != "default" {
		t.Errorf("Authorizer().Name() = %q, want \"default\"", got)
	}
}

func TestRegistrySetAuthorizerReplacesIt(t *testing.T) {
	testLogger := logger.NewTestLogger()
	r := NewRegistry(nil, testLogger)
	stub := &stubAuthorizer{}

	r.SetAuthorizer(stub)

	if got := r.Authorizer().Name(); got != "stub" {
		t.Errorf("Authorizer().Name() = %q, want \"stub\"", got)
	}
}

// Passing nil must not blank out the authorizer and leave a nil-interface
// panic waiting for the first guarded request.
func TestRegistrySetAuthorizerIgnoresNil(t *testing.T) {
	testLogger := logger.NewTestLogger()
	r := NewRegistry(nil, testLogger)

	r.SetAuthorizer(nil)

	if r.Authorizer() == nil {
		t.Fatal("Authorizer() = nil after SetAuthorizer(nil)")
	}

	if got := r.Authorizer().Name(); got != "default" {
		t.Errorf("Authorizer().Name() = %q, want \"default\"", got)
	}
}

// embeddedContext exists only so testContext can embed forge.Context under a
// field name other than "Context": forge.Context itself has a Context()
// method, and embedding it under its own type name would make the field and
// the promoted method collide (both named "Context"), leaving *testContext
// without a usable Context() method. Embedding this alias instead gives the
// field the name "embeddedContext", so Context() promotes cleanly.
type embeddedContext = forge.Context

// testContext is a forge.Context test double. It embeds the real
// implementation from go-utils/http (the same one production handlers get,
// see internal/router/handler.go) so it satisfies the full interface without
// hand-rolling dozens of methods, and adds StatusCode() so tests can inspect
// what got written to the underlying httptest.ResponseRecorder.
type testContext struct {
	embeddedContext

	rec *httptest.ResponseRecorder
}

func (t *testContext) StatusCode() int {
	return t.rec.Code
}

func newTestContext(t *testing.T) *testContext {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	return &testContext{
		embeddedContext: forge_http.NewContext(rec, req, nil),
		rec:             rec,
	}
}

func TestMiddlewareWithRequirementDeniesOnMissingRole(t *testing.T) {
	testLogger := logger.NewTestLogger()
	r := NewRegistry(nil, testLogger)

	if err := r.Register(&mockProvider{
		name: "jwt",
		authCtx: &AuthContext{
			Subject: "u1",
			Roles:   []string{"viewer"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mw := r.MiddlewareWithRequirement(Requirement{
		Providers: []string{"jwt"},
		Roles:     []string{"admin"},
	})

	called := false
	handler := mw(func(forge.Context) error {
		called = true

		return nil
	})

	ctx := newTestContext(t) // see existing helpers in this file
	err := handler(ctx)

	if called {
		t.Error("handler ran despite a failed authorization")
	}

	if status := ctx.StatusCode(); status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}

	_ = err
}

func TestMiddlewareWithRequirementAllowsWhenSatisfied(t *testing.T) {
	testLogger := logger.NewTestLogger()
	r := NewRegistry(nil, testLogger)

	if err := r.Register(&mockProvider{
		name: "jwt",
		authCtx: &AuthContext{
			Subject: "u1",
			Roles:   []string{"viewer"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mw := r.MiddlewareWithRequirement(Requirement{
		Providers: []string{"jwt"},
		Roles:     []string{"viewer"},
	})

	called := false
	handler := mw(func(forge.Context) error {
		called = true

		return nil
	})

	if err := handler(newTestContext(t)); err != nil {
		t.Fatalf("handler returned %v, want nil", err)
	}

	if !called {
		t.Error("handler did not run despite a satisfied requirement")
	}
}

// An unguarded route still authenticates but must reach the handler without
// consulting the authorizer at all. Otherwise every existing WithAuth route
// would start paying for a decision it never asked for, and a custom
// authorizer that denies by default would break all of them.
func TestMiddlewareWithRequirementSkipsAuthorizerWhenEmpty(t *testing.T) {
	testLogger := logger.NewTestLogger()
	r := NewRegistry(nil, testLogger)

	if err := r.Register(&mockProvider{
		name:    "jwt",
		authCtx: &AuthContext{Subject: "u1"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	stub := &stubAuthorizer{}
	r.SetAuthorizer(stub)

	mw := r.MiddlewareWithRequirement(Requirement{Providers: []string{"jwt"}})

	called := false
	handler := mw(func(forge.Context) error {
		called = true

		return nil
	})

	if err := handler(newTestContext(t)); err != nil {
		t.Fatalf("handler returned %v, want nil", err)
	}

	if !called {
		t.Error("handler did not run for an unguarded route")
	}

	if stub.called {
		t.Error("authorizer consulted for a requirement that demands nothing")
	}
}

// TestMiddlewareWithRequirementPublishesSubjectRoles proves the producer side
// of Task 13's rename: MiddlewareWithRequirement must set the subject's roles
// under "auth.subject.roles", not the old "auth.roles" — that string is
// already used elsewhere (internal/router/router_auth_opts.go's WithAnyRole)
// as ROUTE metadata for the roles a route requires, a different map with the
// opposite meaning. internal/router's RequireRole/RequireAllRoles
// interceptors read this key to enforce membership without importing this
// package (which would cycle).
func TestMiddlewareWithRequirementPublishesSubjectRoles(t *testing.T) {
	testLogger := logger.NewTestLogger()
	r := NewRegistry(nil, testLogger)

	if err := r.Register(&mockProvider{
		name: "jwt",
		authCtx: &AuthContext{
			Subject: "u1",
			Roles:   []string{"admin", "editor"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// No Roles/Permissions/Scopes required — an unguarded but authenticated
	// route must still publish the subject's roles for RequireRole to read.
	mw := r.MiddlewareWithRequirement(Requirement{Providers: []string{"jwt"}})

	var gotRoles []string

	handler := mw(func(ctx forge.Context) error {
		gotRoles, _ = ctx.Get("auth.subject.roles").([]string)

		return nil
	})

	if err := handler(newTestContext(t)); err != nil {
		t.Fatalf("handler returned %v, want nil", err)
	}

	want := []string{"admin", "editor"}
	if len(gotRoles) != len(want) {
		t.Fatalf("auth.subject.roles = %v, want %v", gotRoles, want)
	}

	for i, role := range want {
		if gotRoles[i] != role {
			t.Errorf("auth.subject.roles[%d] = %q, want %q", i, gotRoles[i], role)
		}
	}
}

// TestMiddlewareWithRequirementPublishesSubjectScopes is the scope twin of the
// role test above. "auth.scopes" is route metadata for the scopes a route
// REQUIRES (WithRequiredAuth, WithGroupRequiredScopes), so the scopes the
// SUBJECT HOLDS go out under "auth.subject.scopes" instead, where
// internal/router's RequireScopes/RequireAnyScope read them without importing
// this package (which would cycle).
func TestMiddlewareWithRequirementPublishesSubjectScopes(t *testing.T) {
	testLogger := logger.NewTestLogger()
	r := NewRegistry(nil, testLogger)

	if err := r.Register(&mockProvider{
		name: "jwt",
		authCtx: &AuthContext{
			Subject: "u1",
			Scopes:  []string{"read:users", "write:users"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// No Roles/Permissions/Scopes required — an unguarded but authenticated
	// route must still publish the subject's scopes for RequireScopes to read.
	mw := r.MiddlewareWithRequirement(Requirement{Providers: []string{"jwt"}})

	var gotScopes []string

	handler := mw(func(ctx forge.Context) error {
		gotScopes, _ = ctx.Get("auth.subject.scopes").([]string)

		return nil
	})

	if err := handler(newTestContext(t)); err != nil {
		t.Fatalf("handler returned %v, want nil", err)
	}

	want := []string{"read:users", "write:users"}
	if len(gotScopes) != len(want) {
		t.Fatalf("auth.subject.scopes = %v, want %v", gotScopes, want)
	}

	for i, scope := range want {
		if gotScopes[i] != scope {
			t.Errorf("auth.subject.scopes[%d] = %q, want %q", i, gotScopes[i], scope)
		}
	}
}
