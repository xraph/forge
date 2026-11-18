package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"net/http"
	"strings"

	"github.com/xraph/forge"
)

// APIKeyMiddleware rejects requests that do not present key, either as
// "Authorization: Bearer <key>" or in the X-API-Key header. Both values are
// MACed under a random per-middleware secret before a constant-time
// comparison, so neither the key's contents nor its length leak through
// timing. A rejected request gets 401 and the wrapped handler is not called.
func APIKeyMiddleware(key string) forge.Middleware {
	secret := make([]byte, sha256.Size)
	if _, err := rand.Read(secret); err != nil {
		panic("consensus security: cannot read random bytes: " + err.Error())
	}

	mac := func(v string) []byte {
		h := hmac.New(sha256.New, secret)
		h.Write([]byte(v))

		return h.Sum(nil)
	}

	want := mac(key)

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			provided := presentedKey(ctx.Request())
			if key == "" || provided == "" || !hmac.Equal(mac(provided), want) {
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
