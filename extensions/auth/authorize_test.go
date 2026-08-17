package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDefaultAuthorizerAllowsWhenRequirementEmpty(t *testing.T) {
	a := NewDefaultAuthorizer()

	if err := a.Authorize(context.Background(), nil, Requirement{}); err != nil {
		t.Errorf("empty requirement denied: %v", err)
	}
}

func TestDefaultAuthorizerRolesAreAnyOf(t *testing.T) {
	a := NewDefaultAuthorizer()
	authCtx := &AuthContext{Roles: []string{"editor"}}

	err := a.Authorize(context.Background(), authCtx, Requirement{
		Roles: []string{"admin", "editor"},
	})
	if err != nil {
		t.Errorf("holding one of two roles denied: %v", err)
	}

	err = a.Authorize(context.Background(), authCtx, Requirement{
		Roles: []string{"admin", "owner"},
	})
	if !errors.Is(err, ErrAuthorizationFailed) {
		t.Errorf("holding neither role: err = %v, want ErrAuthorizationFailed", err)
	}
}

func TestDefaultAuthorizerPermissionsAreAllOf(t *testing.T) {
	a := NewDefaultAuthorizer()
	authCtx := &AuthContext{Permissions: []string{"users:read"}}

	err := a.Authorize(context.Background(), authCtx, Requirement{
		Permissions: []string{"users:read", "users:write"},
	})
	if !errors.Is(err, ErrAuthorizationFailed) {
		t.Errorf("missing one permission: err = %v, want ErrAuthorizationFailed", err)
	}

	authCtx.Permissions = append(authCtx.Permissions, "users:write")

	if err := a.Authorize(context.Background(), authCtx, Requirement{
		Permissions: []string{"users:read", "users:write"},
	}); err != nil {
		t.Errorf("holding both permissions denied: %v", err)
	}
}

func TestDefaultAuthorizerScopesAreAllOf(t *testing.T) {
	a := NewDefaultAuthorizer()
	authCtx := &AuthContext{Scopes: []string{"write:users"}}

	err := a.Authorize(context.Background(), authCtx, Requirement{
		Scopes: []string{"write:users", "admin"},
	})
	if !errors.Is(err, ErrInsufficientScopes) {
		t.Errorf("missing scope: err = %v, want ErrInsufficientScopes", err)
	}
}

// The denial message has to name what was missing. warden will return its own
// reasons and Forge's default should set the same expectation.
func TestDefaultAuthorizerNamesTheMissingRequirement(t *testing.T) {
	a := NewDefaultAuthorizer()

	err := a.Authorize(context.Background(), &AuthContext{}, Requirement{
		Roles: []string{"admin"},
	})
	if err == nil {
		t.Fatal("expected denial")
	}

	if got := err.Error(); !strings.Contains(got, "admin") {
		t.Errorf("error %q does not name the missing role", got)
	}
}

func TestRequirementIsEmpty(t *testing.T) {
	if !(Requirement{}).IsEmpty() {
		t.Error("zero Requirement is not empty")
	}

	if (Requirement{Roles: []string{"admin"}}).IsEmpty() {
		t.Error("Requirement with a role reports empty")
	}

	// Providers alone is authentication, not authorization, so it does not
	// make the requirement non-empty for the authorizer's purposes.
	if !(Requirement{Providers: []string{"jwt"}}).IsEmpty() {
		t.Error("Requirement with only providers should be empty for authz")
	}
}
