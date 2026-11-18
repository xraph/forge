package transport

import (
	"net/http"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/security"
)

// CSRFTokenResponse is the wire shape for GET /api/dashboard/v1/csrf.
type CSRFTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// NewCSRFTokenHandler returns the GET /csrf handler. The token is bound to
// the caller resolved by the auth middleware (see CSRFIdentity), so it only
// validates on that caller's own commands.
//
// ttl is the validity window the response reports to the client. The manager
// is the authority on validation, so a ttl of zero or one longer than the
// manager's MaxAge is reported as MaxAge: advertising a longer life than
// validation honours leaves clients holding tokens that are already refused.
func NewCSRFTokenHandler(mgr *security.CSRFManager, ttl time.Duration) http.Handler {
	if ttl <= 0 || ttl > mgr.MaxAge() {
		ttl = mgr.MaxAge()
	}

	return &csrfHandler{mgr: mgr, ttl: ttl}
}

type csrfHandler struct {
	mgr *security.CSRFManager
	ttl time.Duration
}

func (h *csrfHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)

		return
	}

	now := time.Now()
	tok := h.mgr.GenerateTokenFor(CSRFIdentity(dashauth.UserFromContext(r.Context())))
	resp := CSRFTokenResponse{
		Token:     tok,
		ExpiresAt: now.Add(h.ttl),
	}

	// A token is a per-caller secret; no cache should keep it.
	w.Header().Set("Cache-Control", "no-store")
	writePlainJSON(w, resp)
}

// CSRFIdentity is the identity a CSRF token is bound to: the signed-in
// user's subject, or empty for an anonymous caller. The token endpoint and
// the command handler both derive it from the request context, so a token
// only validates for the caller it was minted for.
func CSRFIdentity(user *dashauth.UserInfo) string {
	if !user.Authenticated() {
		return ""
	}

	return user.Subject
}
