package pilot

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

// TestAppsListHandler_OrdersByPriorityThenName guards the switcher contract:
// apps appear in (priority, displayName, contributor) order so a refresh
// never reshuffles the dropdown.
func TestAppsListHandler_OrdersByPriorityThenName(t *testing.T) {
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()

	srcs := []string{
		// Two apps at priority 10 (Authsome + Chronicle) — name tiebreaker
		// puts Authsome before Chronicle.
		`
schemaVersion: 1
contributor:
  name: auth
  envelope: { supports: [v1], preferred: v1 }
  app: { displayName: Authsome, icon: shield, priority: 10, home: /users }
intents: []
`,
		`
schemaVersion: 1
contributor:
  name: chronicle
  envelope: { supports: [v1], preferred: v1 }
  app: { displayName: Chronicle, icon: scroll, priority: 10, home: / }
intents: []
`,
		// Pilot has priority 0 — must come first.
		`
schemaVersion: 1
contributor:
  name: core-contract
  envelope: { supports: [v1], preferred: v1 }
  app: { displayName: Forge, icon: forge, priority: 0, home: / }
intents: []
`,
		// A library contributor without app:; must NOT appear.
		`
schemaVersion: 1
contributor:
  name: lib-helper
  envelope: { supports: [v1], preferred: v1 }
intents: []
`,
	}
	for _, src := range srcs {
		m, err := loader.Load(strings.NewReader(src), "test")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if err := loader.Validate(m, wreg); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if err := reg.Register(m); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	resp, err := appsListHandler(reg)(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("apps.list: %v", err)
	}
	if len(resp.Apps) != 3 {
		t.Fatalf("apps count = %d, want 3 (lib-helper must be filtered)", len(resp.Apps))
	}
	wantOrder := []string{"core-contract", "auth", "chronicle"}
	for i, w := range wantOrder {
		if resp.Apps[i].Contributor != w {
			t.Errorf("apps[%d].Contributor = %q, want %q (full: %+v)",
				i, resp.Apps[i].Contributor, w, resp.Apps)
		}
	}
}

func TestAppsListHandler_NilRegistry(t *testing.T) {
	_, err := appsListHandler(nil)(context.Background(), struct{}{}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}
