package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// The mounted token endpoint binds the token to the signed-in caller and
// advertises the expiry validation actually enforces.
func TestCSRFRoute_TokenBoundToCallerAndExpiryHonest(t *testing.T) {
	app, ext := startGatedDashboard(t, WithEnableAuth(true))

	before := time.Now()
	rec := serveGate(t, app, gateCall{"csrf", http.MethodGet, "/dashboard/api/dashboard/v1/csrf", ""}, "alice", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}

	var tok transport.CSRFTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}

	mgr := ext.CSRFManager()
	if !mgr.ValidateTokenFor(tok.Token, "alice") {
		t.Error("token from the route does not validate for the caller")
	}

	if mgr.ValidateTokenFor(tok.Token, "mallory") || mgr.ValidateToken(tok.Token) {
		t.Error("token from the route validates for someone else")
	}

	if tok.ExpiresAt.After(before.Add(mgr.MaxAge()).Add(time.Second)) {
		t.Errorf("advertised expiry %v exceeds validation window %v", tok.ExpiresAt.Sub(before), mgr.MaxAge())
	}
}
