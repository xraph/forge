package forge

// RequireScope is middleware that ensures a Scope is present in the context.
// Returns ErrNoScope (HTTP 401) if missing.
func RequireScope() Middleware {
	return func(next Handler) Handler {
		return func(ctx Context) error {
			if _, ok := GetScope(ctx); !ok {
				return ErrNoScope
			}

			return next(ctx)
		}
	}
}

// RequireOrg is middleware that ensures the Scope includes an organization.
// Returns ErrNoScope (HTTP 401) if no scope is present, or
// ErrNoOrg (HTTP 403) if the scope lacks an OrgID.
func RequireOrg() Middleware {
	return func(next Handler) Handler {
		return func(ctx Context) error {
			s, ok := GetScope(ctx)
			if !ok {
				return ErrNoScope
			}

			if !s.HasOrg() {
				return ErrNoOrg
			}

			return next(ctx)
		}
	}
}
