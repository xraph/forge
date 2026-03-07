package dashauth

import "context"

type contextKey string

const userContextKey contextKey = "forge:dashboard:user"

// WithUser stores a UserInfo in the context.
func WithUser(ctx context.Context, user *UserInfo) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext retrieves the UserInfo from the context.
// Returns nil if no user is stored (i.e. unauthenticated request).
func UserFromContext(ctx context.Context) *UserInfo {
	user, _ := ctx.Value(userContextKey).(*UserInfo)

	return user
}

// IsAuthenticated returns true if the context contains an authenticated user.
func IsAuthenticated(ctx context.Context) bool {
	user := UserFromContext(ctx)

	return user.Authenticated()
}

const tenantContextKey contextKey = "forge:dashboard:tenant"

// WithTenant stores a TenantInfo in the context.
func WithTenant(ctx context.Context, tenant *TenantInfo) context.Context {
	return context.WithValue(ctx, tenantContextKey, tenant)
}

// TenantFromContext retrieves the TenantInfo from the context.
// Returns nil if no tenant is stored.
func TenantFromContext(ctx context.Context) *TenantInfo {
	tenant, _ := ctx.Value(tenantContextKey).(*TenantInfo)

	return tenant
}

// HasTenantInContext returns true if the context contains a tenant with a non-empty ID.
func HasTenantInContext(ctx context.Context) bool {
	t := TenantFromContext(ctx)

	return t.HasTenant()
}
