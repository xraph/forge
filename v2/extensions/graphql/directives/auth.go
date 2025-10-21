package directives

import (
	"context"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
)

// Auth directive validates authentication and authorization
// Usage: @auth(requires: "admin")
func Auth(ctx context.Context, obj interface{}, next graphql.Resolver, requires *string) (interface{}, error) {
	// Check if user is authenticated
	user := ctx.Value("user")
	if user == nil {
		return nil, fmt.Errorf("unauthorized: authentication required")
	}

	// If requires is set, check authorization
	if requires != nil && *requires != "" {
		// Get user roles from context
		roles, ok := ctx.Value("roles").([]string)
		if !ok {
			return nil, fmt.Errorf("unauthorized: role information not found")
		}

		// Check if user has required role
		hasRole := false
		for _, role := range roles {
			if role == *requires {
				hasRole = true
				break
			}
		}

		if !hasRole {
			return nil, fmt.Errorf("forbidden: requires role '%s'", *requires)
		}
	}

	// User is authorized, proceed to resolver
	return next(ctx)
}
