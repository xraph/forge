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
	h := NewCapabilitiesHandler(reg, []string{"v1"}, nil)
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
	// A nil status func is the no-status-registered case for every
	// contributor, so the permissive default applies.
	if !got.Contributors[0].Configured {
		t.Errorf("configured = false with a nil status func, want true")
	}
	_ = contract.IntentKindQuery
}

// registerContributor adds another contributor to reg so the status test has
// one contributor with a registered status and one without.
func registerContributor(t *testing.T, reg contract.Registry, name string) {
	t.Helper()
	src := `
schemaVersion: 1
contributor: { name: ` + name + `, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: ` + name + `.list, kind: query, version: 1, capability: read }
`
	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&m); err != nil {
		t.Fatal(err)
	}
}

// capabilitiesByName serves GET /capabilities and returns each contributor's
// raw JSON object keyed by name. Raw JSON, not the decoded struct, because the
// absence of a field and a field set to false decode identically — and
// `configured` must never be absent.
func capabilitiesByName(t *testing.T, h http.Handler) map[string]map[string]json.RawMessage {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/capabilities", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var raw struct {
		Contributors []map[string]json.RawMessage `json:"contributors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal %s: %v", w.Body.Bytes(), err)
	}
	out := make(map[string]map[string]json.RawMessage, len(raw.Contributors))
	for _, c := range raw.Contributors {
		var name string
		if err := json.Unmarshal(c["name"], &name); err != nil {
			t.Fatal(err)
		}
		out[name] = c
	}
	return out
}

// TestCapabilities_ReportsContributorStatus pins the two facts the plugin host
// resolves a plugin's state from: the extension's version (matched against the
// plugin's `requires` range) and whether it is configured (false renders the
// setup guide instead of the plugin).
func TestCapabilities_ReportsContributorStatus(t *testing.T) {
	reg, _ := setupRegistry(t)
	registerContributor(t, reg, "billing")

	// "users" reports a status; "billing" reports none.
	status := func(name string) (ContributorStatus, bool) {
		if name == "users" {
			return ContributorStatus{Version: "2.1.0", Configured: false, Message: "set SMTP host"}, true
		}
		return ContributorStatus{}, false
	}
	got := capabilitiesByName(t, NewCapabilitiesHandler(reg, []string{"v1"}, status))

	users, ok := got["users"]
	if !ok {
		t.Fatalf("no users contributor in %v", got)
	}
	if v := string(users["version"]); v != `"2.1.0"` {
		t.Errorf("users version = %s, want \"2.1.0\"", v)
	}
	if v := string(users["configured"]); v != "false" {
		t.Errorf("users configured = %q, want false — a contributor that reports "+
			"Configured=false must serialise false, and the field must not be "+
			"elided by omitempty", v)
	}
	if v := string(users["message"]); v != `"set SMTP host"` {
		t.Errorf("users message = %s, want \"set SMTP host\"", v)
	}

	billing, ok := got["billing"]
	if !ok {
		t.Fatalf("no billing contributor in %v", got)
	}
	if v := string(billing["configured"]); v != "true" {
		t.Errorf("billing configured = %q, want true — a contributor with no "+
			"registered status defaults to configured, not to bool's zero value", v)
	}
	if _, present := billing["version"]; present {
		t.Errorf("billing version = %s, want omitted", billing["version"])
	}
	if _, present := billing["message"]; present {
		t.Errorf("billing message = %s, want omitted", billing["message"])
	}
}
