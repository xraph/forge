package transport

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/xraph/forge/extensions/dashboard/security"
)

// CSRFTokenResponse is the wire shape for GET /api/dashboard/v1/csrf.
type CSRFTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// NewCSRFTokenHandler returns the GET /csrf handler. ttl is the token validity
// window the response surfaces to the client; the underlying manager is the
// authority on validation.
func NewCSRFTokenHandler(mgr *security.CSRFManager, ttl time.Duration) http.Handler {
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
	tok := h.mgr.GenerateToken()
	resp := CSRFTokenResponse{
		Token:     tok,
		ExpiresAt: time.Now().Add(h.ttl),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
