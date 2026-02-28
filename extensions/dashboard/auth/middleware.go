package dashauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/a-h/templ"

	"github.com/xraph/forge"

	"github.com/xraph/forgeui/router"
)

// ForgeMiddleware returns forge.Middleware that runs the AuthChecker and stores
// the resulting UserInfo in the request context. This middleware runs on the
// forge.Router catch-all route, BEFORE the request reaches ForgeUI.
//
// It does NOT block unauthenticated requests — it only populates the context.
// Access enforcement is handled by PageMiddleware at the ForgeUI layer.
func ForgeMiddleware(checker AuthChecker) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if checker == nil {
				return next(ctx)
			}

			user, err := checker.CheckAuth(ctx.Context(), ctx.Request())
			if err != nil {
				// Log but don't block — auth infrastructure errors shouldn't
				// prevent public pages from rendering.
				_ = err
			}

			if user != nil {
				ctx.WithContext(WithUser(ctx.Context(), user))
			}

			return next(ctx)
		}
	}
}

// PageMiddleware returns ForgeUI router.Middleware that enforces an AccessLevel
// on a specific page. It reads the UserInfo from the request context (placed
// there by ForgeMiddleware) and decides whether to allow, redirect, or pass through.
//
//   - AccessPublic: always passes through.
//   - AccessPartial: always passes through (the page handler checks auth itself).
//   - AccessProtected: checks UserFromContext; redirects to loginPath if nil.
//
// The loginPath should be the full dashboard path (e.g. "/dashboard/auth/login").
func PageMiddleware(level AccessLevel, loginPath string) router.Middleware {
	return func(next router.PageHandler) router.PageHandler {
		return func(ctx *router.PageContext) (templ.Component, error) {
			switch level {
			case AccessPublic, AccessPartial:
				return next(ctx)

			case AccessProtected:
				user := UserFromContext(ctx.Context())
				if user.Authenticated() {
					return next(ctx)
				}

				// Unauthenticated — redirect to login
				handleUnauthorized(ctx, loginPath)

				return nil, nil

			default:
				return next(ctx)
			}
		}
	}
}

// RequireRole returns ForgeUI middleware that checks the user has a specific role.
// It assumes PageMiddleware(AccessProtected, ...) already ran, so the user is
// authenticated. If the user lacks the role, a 403 Forbidden response is returned.
func RequireRole(role string) router.Middleware {
	return func(next router.PageHandler) router.PageHandler {
		return func(ctx *router.PageContext) (templ.Component, error) {
			user := UserFromContext(ctx.Context())
			if !user.Authenticated() {
				ctx.ResponseWriter.WriteHeader(http.StatusUnauthorized)

				return templ.Raw("401 - Unauthorized"), nil
			}

			if !user.HasRole(role) {
				ctx.ResponseWriter.WriteHeader(http.StatusForbidden)

				return accessDeniedComponent(fmt.Sprintf("You need the %q role to access this page.", role)), nil
			}

			return next(ctx)
		}
	}
}

// RequireScope returns ForgeUI middleware that checks the user has a specific scope.
func RequireScope(scope string) router.Middleware {
	return func(next router.PageHandler) router.PageHandler {
		return func(ctx *router.PageContext) (templ.Component, error) {
			user := UserFromContext(ctx.Context())
			if !user.Authenticated() {
				ctx.ResponseWriter.WriteHeader(http.StatusUnauthorized)

				return templ.Raw("401 - Unauthorized"), nil
			}

			if !user.HasScope(scope) {
				ctx.ResponseWriter.WriteHeader(http.StatusForbidden)

				return accessDeniedComponent(fmt.Sprintf("You need the %q scope to access this page.", scope)), nil
			}

			return next(ctx)
		}
	}
}

// accessDeniedComponent returns a templ.Component that renders an "Access Denied" message.
func accessDeniedComponent(message string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<div class="p-6 text-center">`+
			`<h2 class="text-xl font-semibold text-destructive mb-2">Access Denied</h2>`+
			`<p class="text-muted-foreground">`+templ.EscapeString(message)+`</p>`+
			`</div>`)
		return err
	})
}

// handleUnauthorized sends an HTMX-aware redirect to the login page.
// For HTMX partial requests it uses the HX-Redirect header so the browser
// JS (AuthRedirectScript) can perform a full-page redirect.
// For normal requests it sends a standard HTTP 302 redirect.
func handleUnauthorized(ctx *router.PageContext, loginPath string) {
	currentPath := ctx.Request.URL.Path

	redirectURL := loginPath
	if currentPath != "" && currentPath != loginPath {
		redirectURL += "?redirect=" + url.QueryEscape(currentPath)
	}

	isHTMX := ctx.Request.Header.Get("Hx-Request") != ""

	if isHTMX {
		// HTMX partial request: use HX-Redirect header for client-side redirect.
		ctx.ResponseWriter.Header().Set("Hx-Redirect", redirectURL)
		ctx.ResponseWriter.WriteHeader(http.StatusUnauthorized)

		return
	}

	// Standard HTTP redirect.
	http.Redirect(ctx.ResponseWriter, ctx.Request, redirectURL, http.StatusFound)
}
