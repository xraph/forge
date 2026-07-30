package shared

import (
	"net/http"
	"sync/atomic"
)

// exposeInternalErrors controls whether server-error detail is echoed to
// clients. Set once at app construction; see SetExposeInternalErrors.
var exposeInternalErrors atomic.Bool

// SetExposeInternalErrors controls whether 5xx responses carry internal error
// detail. The app enables it outside production so local debugging is not
// degraded, and leaves it off in production.
//
// A 5xx body built from a wrapped error commonly embeds whatever the error
// carried — driver output, SQL fragments, file paths, internal hostnames — and
// anyone able to trigger the error can read it. Detail always reaches the logs
// regardless of this setting.
func SetExposeInternalErrors(expose bool) {
	exposeInternalErrors.Store(expose)
}

// ExposeInternalErrors reports the current setting.
func ExposeInternalErrors() bool {
	return exposeInternalErrors.Load()
}

// SanitizeErrorBody returns the body to send for an error response.
//
// For 5xx it replaces the error's own body with a minimal envelope unless
// exposure is enabled: a server error is by definition not something the client
// can act on, so the detail is pure disclosure. 4xx bodies pass through
// untouched — validation messages and field errors are exactly what the client
// needs in order to correct the request.
func SanitizeErrorBody(status int, body any) any {
	if status < http.StatusInternalServerError || exposeInternalErrors.Load() {
		return body
	}

	return map[string]any{
		"code":  status,
		"error": http.StatusText(status),
	}
}
