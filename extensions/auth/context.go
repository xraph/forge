package auth

import (
	"context"

	"github.com/xraph/forge"
)

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

// GetAuthContext retrieves the AuthContext from forge.Context.
// Returns the auth context and true if found, nil and false otherwise.
func GetAuthContext(ctx forge.Context) (*AuthContext, bool) {
	authCtx, ok := ctx.Get("auth_context").(*AuthContext)
	return authCtx, ok
}

// MustGetAuthContext retrieves the AuthContext from forge.Context or panics.
// Use this only when you're certain the context contains auth information
// (e.g., after auth middleware has run).
func MustGetAuthContext(ctx forge.Context) *AuthContext {
	authCtx, ok := GetAuthContext(ctx)
	if !ok {
		panic("auth context not found in forge context")
	}
	return authCtx
}

// GetAuthContextFromStdContext retrieves AuthContext from standard context.Context.
// This is for backward compatibility with code that uses standard context.
// For new code using forge.Context, use GetAuthContext instead.
func GetAuthContextFromStdContext(ctx context.Context) (*AuthContext, bool) {
	return FromContext(ctx)
}
