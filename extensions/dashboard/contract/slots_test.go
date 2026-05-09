// slots_test.go
package contract

import "testing"

func TestSlotCatalog_PageShell(t *testing.T) {
	def, ok := DefaultSlotCatalog["page.shell"]
	if !ok {
		t.Fatal("page.shell missing from default slot catalog")
	}
	if def.Slots["main"].Cardinality != CardinalityMany {
		t.Errorf("main cardinality = %v", def.Slots["main"].Cardinality)
	}
}

func TestValidateSlotFills_AcceptCheck(t *testing.T) {
	// page.shell.main accepts resource.list, dashboard.grid; rejects unknown
	parent := DefaultSlotCatalog["page.shell"]
	cases := []struct {
		child   string
		wantErr bool
	}{
		{"resource.list", false},
		{"dashboard.grid", false},
		{"action.button", true}, // not allowed in main
	}
	for _, c := range cases {
		err := validateSlotAccepts(parent.Slots["main"], c.child)
		if (err != nil) != c.wantErr {
			t.Errorf("child=%s err=%v wantErr=%v", c.child, err, c.wantErr)
		}
	}
}

func TestSlotDepth_ExceedsMax(t *testing.T) {
	// build a graph of depth 9 (root + 8 nested slots)
	leaf := GraphNode{Intent: "metric.counter"}
	cur := leaf
	for i := 0; i < 9; i++ {
		cur = GraphNode{Intent: "page.shell", Slots: map[string][]GraphNode{"main": {cur}}}
	}
	if err := checkDepth([]GraphNode{cur}, 0); err == nil {
		t.Error("expected depth-exceeded error")
	}
}

func TestSlotCycle_DetectedAtRegistration(t *testing.T) {
	// A node referencing its own intent through a slot is a cycle
	root := GraphNode{
		Intent: "self",
		Slots: map[string][]GraphNode{
			"main": {{Intent: "self"}},
		},
	}
	if err := checkCycle([]GraphNode{root}, map[string]bool{}); err == nil {
		t.Error("expected cycle error")
	}
}

func TestApplyExtensions_NonExtensibleSlot_Rejected(t *testing.T) {
	// users.page.shell has a 'main' slot (not extensible by default)
	r := NewRegistry()
	usersM := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - { route: /users, intent: page.shell, slots: { main: [{ intent: resource.list }] } }
`)
	_ = r.Register(usersM)

	authM := mustManifest(t, `
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents: []
extends:
  - target: { contributor: users, intent: page.shell, route: /users }
    slot: main
    add:
      - { intent: action.button }
`)
	if err := r.Register(authM); err == nil {
		t.Error("expected non-extensible-slot rejection")
	}
}

func TestApplyExtensions_Extensible_Merges(t *testing.T) {
	r := NewRegistry()
	usersM := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - { route: /users, intent: page.shell, slots: {
        main: [{
          intent: resource.list,
          slots: { detailDrawer: [{ intent: form.edit }] }
        }]
      } }
`)
	_ = r.Register(usersM)

	authM := mustManifest(t, `
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents: []
extends:
  - target: { contributor: users, intent: page.shell, route: /users }
    slot: main.detailDrawer.fields
    add:
      - { intent: form.field }
`)
	if err := r.Register(authM); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	// After registration, the merged graph should include the extension
	merged, ok := r.MergedGraph("users", "/users")
	if !ok {
		t.Fatal("merged graph not found")
	}
	fields := merged.Slots["main"][0].Slots["detailDrawer"][0].Slots["fields"]
	if len(fields) != 1 || fields[0].Intent != "form.field" {
		t.Errorf("extension not applied: %+v", fields)
	}

	// Verify original manifest's graph is NOT mutated
	origFields := usersM.Graph[0].Slots["main"][0].Slots["detailDrawer"][0].Slots["fields"]
	if len(origFields) != 0 {
		t.Errorf("original manifest mutated; got fields=%+v", origFields)
	}
}
