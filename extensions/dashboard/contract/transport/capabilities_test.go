// capabilities_test.go
package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

func TestCapabilities_ReportsRegisteredContributors(t *testing.T) {
	reg, _ := setupRegistry(t)
	h := NewCapabilitiesHandler(reg, []string{"v1"})
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/capabilities", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got CapabilitiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Contributors) != 1 || got.Contributors[0].Name != "users" {
		t.Errorf("contributors = %+v", got.Contributors)
	}
	intents := got.Contributors[0].Intents
	if len(intents) != 2 {
		t.Errorf("expected 2 intents in capabilities, got %d", len(intents))
	}
	_ = contract.IntentKindQuery
}
