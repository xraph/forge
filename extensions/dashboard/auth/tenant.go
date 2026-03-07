package dashauth

import (
	"context"
	"net/http"

	"github.com/xraph/forge"
)

// TenantType identifies the type of tenant scope.
type TenantType string

const (
	// TenantTypeApp indicates an application-level tenant.
	TenantTypeApp TenantType = "app"
	// TenantTypeOrg indicates an organization-level tenant.
	TenantTypeOrg TenantType = "org"
)

// TenantInfo represents the current tenant context for the dashboard.
// It bridges forge.Scope into a richer dashboard-specific type with
// display metadata.
type TenantInfo struct {
	// ID is the tenant identifier (AppID or OrgID depending on Type).
	ID string

	// Name is the human-readable display name for this tenant.
	Name string

	// Type indicates whether this is an app-level or org-level tenant.
	Type TenantType

	// AppID is always present — the application identifier.
	AppID string

	// OrgID is present only for org-level tenants.
	OrgID string

	// LogoURL is an optional URL for the tenant's logo/branding.
	LogoURL string

	// Metadata holds additional tenant-specific data.
	Metadata map[string]any
}

// IsOrg returns true if this is an organization-level tenant.
func (t *TenantInfo) IsOrg() bool {
	return t != nil && t.Type == TenantTypeOrg
}

// IsApp returns true if this is an app-level tenant.
func (t *TenantInfo) IsApp() bool {
	return t != nil && t.Type == TenantTypeApp
}

// HasTenant returns true if the tenant info is non-nil and has a non-empty ID.
func (t *TenantInfo) HasTenant() bool {
	return t != nil && t.ID != ""
}

// TenantResolver resolves the current tenant from a request context.
// Implementations typically read forge.Scope from context and enrich it
// with display name, logo, etc.
type TenantResolver interface {
	ResolveTenant(ctx context.Context, r *http.Request) (*TenantInfo, error)
}

// TenantResolverFunc is a function adapter for TenantResolver.
type TenantResolverFunc func(ctx context.Context, r *http.Request) (*TenantInfo, error)

// ResolveTenant implements TenantResolver.
func (f TenantResolverFunc) ResolveTenant(ctx context.Context, r *http.Request) (*TenantInfo, error) {
	return f(ctx, r)
}

// ScopeTenantResolver is a default TenantResolver that reads forge.Scope
// from the request context and converts it to TenantInfo.
// This works out of the box when forge's scope middleware is in the chain.
type ScopeTenantResolver struct{}

// ResolveTenant reads forge.Scope from context and converts to TenantInfo.
func (s ScopeTenantResolver) ResolveTenant(ctx context.Context, _ *http.Request) (*TenantInfo, error) {
	scope, ok := forge.ScopeFrom(ctx)
	if !ok || scope.IsZero() {
		return nil, nil // no scope, no tenant
	}

	tenantType := TenantTypeApp
	id := scope.AppID()

	if scope.HasOrg() {
		tenantType = TenantTypeOrg
		id = scope.OrgID()
	}

	return &TenantInfo{
		ID:    id,
		Name:  id, // default name is the ID; enrichment can happen via custom resolver
		Type:  tenantType,
		AppID: scope.AppID(),
		OrgID: scope.OrgID(),
	}, nil
}
