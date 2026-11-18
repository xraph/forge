package dashauth

import (
	"github.com/xraph/forge"
)

// ForgeMiddleware returns forge.Middleware that runs the AuthChecker and stores
// the resulting UserInfo in the request context. The dashboard attaches it to
// the contract routes, so the dispatcher and the principal endpoint see the
// caller.
//
// It does NOT block unauthenticated requests; it only populates the context.
// The dashboard extension uses it on the principal endpoint alone. Every other
// dashboard route runs a blocking gate when auth is on, because an intent that
// declares no Requires predicate allows everyone and so cannot be relied on to
// keep anonymous callers out.
func ForgeMiddleware(checker AuthChecker) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if checker == nil {
				return next(ctx)
			}

			user, err := checker.CheckAuth(ctx.Context(), ctx.Request())
			if err != nil {
				// Log but don't block — auth infrastructure errors shouldn't
				// fail requests that do not need a user.
				_ = err
			}

			if user != nil {
				ctx.WithContext(WithUser(ctx.Context(), user))
			}

			return next(ctx)
		}
	}
}

// TenantMiddleware returns forge.Middleware that runs the TenantResolver and
// stores the resulting TenantInfo in the request context. Like ForgeMiddleware
// for auth, it does NOT block requests without a tenant — it only populates
// the context so downstream handlers can access tenant info.
func TenantMiddleware(resolver TenantResolver) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if resolver == nil {
				return next(ctx)
			}

			tenant, err := resolver.ResolveTenant(ctx.Context(), ctx.Request())
			if err != nil {
				// Log but don't block — tenant resolution errors shouldn't
				// fail the request.
				_ = err
			}

			if tenant != nil {
				ctx.WithContext(WithTenant(ctx.Context(), tenant))
			}

			return next(ctx)
		}
	}
}
