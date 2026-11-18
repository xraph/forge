package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/xraph/forge"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// authGate is the middleware every dashboard route runs when EnableAuth is
// on. It resolves the caller through the AuthChecker, the same way
// dashauth.ForgeMiddleware does, and then refuses the request outright when
// nobody is signed in (401) or when RequiredRoles is set and the caller holds
// none of them (403).
//
// The gate exists because the per-intent Requires predicate cannot carry
// this on its own: an intent that declares no predicate allows everyone, and
// the built-in intents declare none. Without a route-level gate, turning auth
// on only changed what the principal endpoint said, while every query,
// stream and export still answered an anonymous caller.
//
// A user already placed in the context by middleware upstream of the
// dashboard counts, so deployments that authenticate before the request
// reaches the dashboard keep working without an AuthChecker. With neither,
// every request is refused, which is the safe reading of "auth is on".
func authGate(checker dashauth.AuthChecker, requiredRoles []string, loginPath string, logger forge.Logger) forge.Middleware {
	roles := append([]string(nil), requiredRoles...)

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			user := dashauth.UserFromContext(ctx.Context())

			if checker != nil {
				checked, err := checker.CheckAuth(ctx.Context(), ctx.Request())
				if err != nil && logger != nil {
					// An auth backend failure must not open the door, so it
					// is treated as "nobody is signed in" below.
					logger.Warn("dashboard: auth check failed, treating request as unauthenticated",
						forge.F("path", ctx.Request().URL.Path),
						forge.F("error", err.Error()),
					)
				}

				if checked != nil {
					user = checked
					ctx.WithContext(dashauth.WithUser(ctx.Context(), user))
				}
			}

			if !user.Authenticated() {
				writeGateError(ctx.Response(), http.StatusUnauthorized, &contract.Error{
					Code:    contract.CodeUnauthenticated,
					Message: "sign in to use the dashboard",
					Details: map[string]any{"loginPath": loginPath},
				})

				return nil
			}

			if len(roles) > 0 && !user.HasAnyRole(roles...) {
				writeGateError(ctx.Response(), http.StatusForbidden, &contract.Error{
					Code:    contract.CodePermissionDenied,
					Message: "your account does not have a role required to access this dashboard",
					Details: map[string]any{"requiredRoles": roles},
				})

				return nil
			}

			return next(ctx)
		}
	}
}

// writeGateError writes the contract's error envelope, so clients that
// already decode envelope errors from the dispatch endpoint handle a refusal
// from the gate the same way.
func writeGateError(w http.ResponseWriter, status int, e *contract.Error) {
	body, err := json.Marshal(contract.ErrorResponse{OK: false, Envelope: "v1", Error: e})
	if err != nil {
		http.Error(w, http.StatusText(status), status)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
