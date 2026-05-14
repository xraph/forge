package dashauth

import (
	"context"
	"net/http"
)

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

const (
	respWriterContextKey contextKey = "forge:dashboard:resp"
	requestContextKey    contextKey = "forge:dashboard:req"
)

// WithHTTP stashes the live ResponseWriter and Request on ctx so contract
// command handlers that legitimately need to touch HTTP — e.g. the auth
// extension's auth.login handler that issues a Set-Cookie — can reach them.
// The contract transport calls this before dispatching commands.
//
// Most contract handlers are pure data and should NOT pull these out;
// reaching for the response writer is an escape hatch for the small set of
// concerns where the cookie/header IS the contract (auth, downloads).
func WithHTTP(ctx context.Context, w http.ResponseWriter, r *http.Request) context.Context {
	ctx = context.WithValue(ctx, respWriterContextKey, w)
	ctx = context.WithValue(ctx, requestContextKey, r)
	return ctx
}

// ResponseWriterFromContext returns the ResponseWriter previously stashed by
// WithHTTP, or nil when called outside an HTTP-served dispatch (background
// jobs, tests).
func ResponseWriterFromContext(ctx context.Context) http.ResponseWriter {
	w, _ := ctx.Value(respWriterContextKey).(http.ResponseWriter)
	return w
}

// RequestFromContext returns the Request previously stashed by WithHTTP, or
// nil outside an HTTP-served dispatch.
func RequestFromContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestContextKey).(*http.Request)
	return r
}
