package auth

import "context"

type contextKey string

const authContextKey contextKey = "forge:auth:context"

// FromContext retrieves the AuthContext from the request context.
// Returns the auth context and true if found, nil and false otherwise.
func FromContext(ctx context.Context) (*AuthContext, bool) {
	authCtx, ok := ctx.Value(authContextKey).(*AuthContext)
	return authCtx, ok
}

// MustFromContext retrieves the AuthContext or panics.
// Use this only when you're certain the context contains auth information
// (e.g., after auth middleware has run).
func MustFromContext(ctx context.Context) *AuthContext {
	authCtx, ok := FromContext(ctx)
	if !ok {
		panic("auth context not found in request context")
	}
	return authCtx
}

// WithContext adds the AuthContext to a context.
func WithContext(ctx context.Context, authCtx *AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey, authCtx)
}
