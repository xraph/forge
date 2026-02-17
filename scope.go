package forge

import (
	"context"
)

// scopeContextKey is the context key for storing Scope in stdlib context.Context.
type scopeContextKey struct{}

// forgeScopeKey is the key used to store Scope in forge.Context's value map.
const forgeScopeKey = "forge:scope"

// ScopeLevel indicates the isolation level.
type ScopeLevel int

const (
	// ScopeApp indicates app-level operations: platform config, plans, global policies.
	ScopeApp ScopeLevel = iota
	// ScopeOrg indicates org-level operations: customer data, usage, isolation.
	ScopeOrganization
)

// String returns a human-readable representation of the scope level.
func (l ScopeLevel) String() string {
	switch l {
	case ScopeApp:
		return "app"
	case ScopeOrganization:
		return "organization"
	default:
		return "unknown"
	}
}

// Scope identifies the current execution context — who is making this request.
// It carries both App and Org identity. Org may be empty for app-level operations.
//
// Scope is intentionally a value type (not a pointer) so it's safe to copy,
// compare, and use as a map key.
type Scope struct {
	appID string // always present (e.g. "app_01h9a1b2c3")
	orgID string // empty for app-level operations (e.g. "org_01h9a1b2c4")
}

// NewAppScope creates a scope for app-level operations (no org).
func NewAppScope(appID string) Scope {
	return Scope{appID: appID}
}

// NewOrgScope creates a scope for org-level operations.
func NewOrgScope(appID, orgID string) Scope {
	return Scope{appID: appID, orgID: orgID}
}

// AppID returns the app identifier. Always present.
func (s Scope) AppID() string { return s.appID }

// OrgID returns the org identifier. Empty for app-level scopes.
func (s Scope) OrgID() string { return s.orgID }

// Level returns whether this scope is app-level or org-level.
func (s Scope) Level() ScopeLevel {
	if s.orgID == "" {
		return ScopeApp
	}
	return ScopeOrganization
}

// HasOrg returns true if this scope includes an organization.
func (s Scope) HasOrg() bool { return s.orgID != "" }

// IsZero returns true if the scope is unset.
func (s Scope) IsZero() bool { return s.appID == "" }

// String returns a human-readable representation.
// "app_01h9a1b2c3" or "app_01h9a1b2c3/org_01h9a1b2c4"
func (s Scope) String() string {
	if s.orgID == "" {
		return s.appID
	}
	return s.appID + "/" + s.orgID
}

// Key returns the scoping key for the given level.
// Extensions use this to decide what to scope by.
//
//	scope.Key(ScopeApp) → "app_01h9a1b2c3"  (always works)
//	scope.Key(ScopeOrganization) → "org_01h9a1b2c4"  (panics if no org)
func (s Scope) Key(level ScopeLevel) string {
	switch level {
	case ScopeApp:
		return s.appID
	case ScopeOrganization:
		if s.orgID == "" {
			panic("forge: Scope.Key(ScopeOrganization) called but no org in scope")
		}
		return s.orgID
	default:
		panic("forge: invalid scope level")
	}
}

// ---------------------------------------------------------------------------
// stdlib context.Context accessors
// ---------------------------------------------------------------------------

// WithScope attaches a Scope to a stdlib context.Context.
// Use this for background jobs, gRPC handlers, or any non-HTTP path.
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, s)
}

// ScopeFrom extracts the Scope from a stdlib context.Context.
// Returns the scope and true if found, zero Scope and false otherwise.
func ScopeFrom(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(scopeContextKey{}).(Scope)
	return s, ok
}

// MustScope extracts the Scope or panics. Use after auth middleware.
func MustScope(ctx context.Context) Scope {
	s, ok := ScopeFrom(ctx)
	if !ok {
		panic("forge: no scope in context")
	}
	return s
}

// AppIDFrom is a convenience that extracts just the AppID from context.
func AppIDFrom(ctx context.Context) string {
	s, ok := ScopeFrom(ctx)
	if !ok {
		return ""
	}
	return s.AppID()
}

// OrganizationIDFrom is a convenience that extracts just the OrgID from context.
func OrganizationIDFrom(ctx context.Context) string {
	s, ok := ScopeFrom(ctx)
	if !ok {
		return ""
	}
	return s.OrgID()
}

// ---------------------------------------------------------------------------
// forge.Context accessors
// ---------------------------------------------------------------------------

// SetScope stores the Scope in a forge.Context.
// Also propagates to the underlying request context so ScopeFrom works on ctx.Context().
func SetScope(ctx Context, s Scope) {
	ctx.Set(forgeScopeKey, s)
	ctx.WithContext(context.WithValue(ctx.Context(), scopeContextKey{}, s))
}

// GetScope retrieves the Scope from a forge.Context.
// Checks the forge context map first, then falls back to the stdlib request context.
func GetScope(ctx Context) (Scope, bool) {
	if v := ctx.Get(forgeScopeKey); v != nil {
		if s, ok := v.(Scope); ok {
			return s, true
		}
	}
	return ScopeFrom(ctx.Context())
}

// MustGetScope retrieves the Scope from forge.Context or panics.
func MustGetScope(ctx Context) Scope {
	s, ok := GetScope(ctx)
	if !ok {
		panic("forge: no scope in forge context")
	}
	return s
}
