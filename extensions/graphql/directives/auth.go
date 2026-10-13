package directives

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/99designs/gqlgen/graphql"
)

// Auth directive validates authentication and authorization
// Usage: @auth(requires: "admin").
func Auth(ctx context.Context, obj any, next graphql.Resolver, requires *string) (any, error) {
	// Check if user is authenticated
	user := ctx.Value("user")
	if user == nil {
		return nil, errors.New("unauthorized: authentication required")
	}

	// If requires is set, check authorization
	if requires != nil && *requires != "" {
		// Get user roles from context
		roles, ok := ctx.Value("roles").([]string)
		if !ok {
			return nil, errors.New("unauthorized: role information not found")
		}

		if !slices.Contains(roles, *requires) {
			return nil, fmt.Errorf("forbidden: requires role '%s'", *requires)
		}
	}

	// User is authorized, proceed to resolver
	return next(ctx)
}
