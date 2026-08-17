package auth

import (
	"context"
	"fmt"
	"strings"
)

// Requirement is the static authorization requirement a route declares.
//
// It is a value rather than a closure on purpose: a value can be written into
// an OpenAPI document and read back into a generated client, and a closure
// cannot. That is the constraint that keeps v1 free of resource-scoped checks
// (can this user edit document 42), which stay inside handlers.
type Requirement struct {
	// Providers are the auth providers that may satisfy authentication.
	Providers []string

	// Scopes must all be held. Existing WithRequiredAuth behaviour.
	Scopes []string

	// Roles: holding any one of them satisfies the requirement.
	Roles []string

	// Permissions must all be held.
	Permissions []string

	// Mode is "or" or "and", and applies to Providers only.
	Mode string
}

// IsEmpty reports whether this requirement asks anything of the subject
// beyond being authenticated.
//
// Providers are excluded: naming a provider is an authentication requirement,
// which the middleware has already enforced by the time an authorizer runs.
func (r Requirement) IsEmpty() bool {
	return len(r.Scopes) == 0 && len(r.Roles) == 0 && len(r.Permissions) == 0
}

// Authorizer decides whether an authenticated subject satisfies a route's
// declared requirement.
//
// Forge ships one implementation doing plain set membership. warden replaces
// it wholesale through Registry.SetAuthorizer. Forge itself never grows role
// hierarchies, permission inheritance or wildcard matching: that is policy,
// and policy belongs to the authorizer.
//
// Authorize returns an error rather than a bool so an implementation can say
// why it refused. A nil return allows. An error wrapping
// ErrAuthorizationFailed or ErrInsufficientScopes maps to 403.
type Authorizer interface {
	Name() string
	Authorize(ctx context.Context, authCtx *AuthContext, req Requirement) error
}

// defaultAuthorizer does set membership and nothing else.
type defaultAuthorizer struct{}

// NewDefaultAuthorizer returns the built-in set-membership authorizer.
func NewDefaultAuthorizer() Authorizer {
	return defaultAuthorizer{}
}

func (defaultAuthorizer) Name() string { return "default" }

func (defaultAuthorizer) Authorize(_ context.Context, authCtx *AuthContext, req Requirement) error {
	if req.IsEmpty() {
		return nil
	}

	// Scopes first, and with their own sentinel, because callers already
	// distinguish ErrInsufficientScopes and changing that would be a silent
	// behaviour change for anyone matching on it.
	if !authCtx.HasScopes(req.Scopes...) {
		return fmt.Errorf("%w: requires scopes %s",
			ErrInsufficientScopes, strings.Join(req.Scopes, ", "))
	}

	if !authCtx.HasAnyRole(req.Roles...) {
		return fmt.Errorf("%w: requires any of roles %s",
			ErrAuthorizationFailed, strings.Join(req.Roles, ", "))
	}

	if !authCtx.HasAllPermissions(req.Permissions...) {
		return fmt.Errorf("%w: requires permissions %s",
			ErrAuthorizationFailed, strings.Join(req.Permissions, ", "))
	}

	return nil
}
