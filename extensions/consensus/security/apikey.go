package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/xraph/forge"
)

// APIKeyMiddleware rejects requests that do not present key, either as
// "Authorization: Bearer <key>" or in the X-API-Key header. Both values are
// hashed before a constant-time comparison, so neither the key's contents nor
// its length leak through timing. A rejected request gets 401 and the wrapped
// handler is not called.
func APIKeyMiddleware(key string) forge.Middleware {
	want := sha256.Sum256([]byte(key))

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			provided := presentedKey(ctx.Request())
			got := sha256.Sum256([]byte(provided))

			if key == "" || provided == "" || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				ctx.Response().Header().Set("WWW-Authenticate", `Bearer realm="consensus-admin"`)

				return ctx.JSON(http.StatusUnauthorized, map[string]any{
					"error": "missing or invalid API key",
				})
			}

			return next(ctx)
		}
	}
}

func presentedKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if token, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return strings.TrimSpace(token)
		}
	}

	return r.Header.Get("X-Api-Key")
}
