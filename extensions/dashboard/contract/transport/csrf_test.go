package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/security"
)

func TestCSRFTokenHandler_ReturnsTokenAndExpiry(t *testing.T) {
	mgr := security.NewCSRFManager()
	h := NewCSRFTokenHandler(mgr, time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/csrf", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var resp CSRFTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Token == "" {
		t.Error("token empty")
	}
	if !mgr.ValidateToken(resp.Token) {
		t.Error("returned token does not validate against the manager")
	}
	if resp.ExpiresAt.Before(time.Now()) {
		t.Errorf("ExpiresAt is in the past: %v", resp.ExpiresAt)
	}
}

func TestCSRFTokenHandler_RejectsNonGET(t *testing.T) {
	h := NewCSRFTokenHandler(security.NewCSRFManager(), time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1/csrf", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
