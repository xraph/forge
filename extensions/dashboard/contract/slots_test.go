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

// TestSlotCatalog_NewIntentKinds spot-checks the new entries registered when
// the slot catalog expanded for the authsome migration. Each case asserts the
// intent kind is present and its declared slots have the expected cardinality
// and extensibility — the bare minimum to catch accidental removals.
func TestSlotCatalog_NewIntentKinds(t *testing.T) {
	cases := []struct {
		intent      string
		slot        string
		want        Cardinality
		extensible  bool
		acceptsKind string // one intent that must appear in Accepts
	}{
		{"resource.detail", "header", CardinalityOne, false, "detail.header"},
		{"resource.detail", "sections", CardinalityMany, true, "detail.section"},
		{"resource.detail", "actions", CardinalityMany, true, "action.button"},
		{"resource.detail", "extensions", CardinalityMany, true, "detail.section"},
		{"resource.list", "filters", CardinalityMany, true, "filter.search"},
		{"resource.list", "bulkActions", CardinalityMany, true, "action.button"},
		{"resource.list", "pagination", CardinalityOne, false, "pagination.cursor"},
		{"form.edit", "actions", CardinalityMany, true, "action.button"},
		{"form.edit", "fields", CardinalityMany, true, "field.json"},
		{"dashboard.grid", "widgets", CardinalityMany, true, "dashboard.stat"},
		{"layout.tabs", "panels", CardinalityMany, true, "layout.section"},
		{"layout.card", "content", CardinalityMany, true, "resource.list"},
		{"layout.section", "content", CardinalityMany, true, "form.edit"},
		{"detail.section", "content", CardinalityMany, true, "resource.list"},
		{"confirm.dialog", "trigger", CardinalityOne, false, "action.button"},
		{"confirm.dialog", "body", CardinalityMany, false, "form.edit"},
	}
	for _, c := range cases {
		def, ok := DefaultSlotCatalog[c.intent]
		if !ok {
			t.Errorf("intent %q missing from catalog", c.intent)
			continue
		}
		slot, ok := def.Slots[c.slot]
		if !ok {
			t.Errorf("intent %q has no slot %q", c.intent, c.slot)
			continue
		}
		if slot.Cardinality != c.want {
			t.Errorf("intent %q slot %q cardinality = %v, want %v", c.intent, c.slot, slot.Cardinality, c.want)
		}
		if slot.Extensible != c.extensible {
			t.Errorf("intent %q slot %q extensible = %v, want %v", c.intent, c.slot, slot.Extensible, c.extensible)
		}
		if err := validateSlotAccepts(slot, c.acceptsKind); err != nil {
			t.Errorf("intent %q slot %q should accept %q: %v", c.intent, c.slot, c.acceptsKind, err)
		}
	}
}

// TestSlotCatalog_LeafIntentsRegistered checks that the new leaf intents
// (auth pages, editors, filters, dashboard widgets, form field extensions)
// are present in the catalog so the validator knows about them, even though
// they have no slots of their own. Manifests using them as children will
// have their Accepts checked by the parent slot; if the parent forgets them
// they'll be silently rejected.
func TestSlotCatalog_LeafIntentsRegistered(t *testing.T) {
	leaves := []string{
		"auth.signin-form", "auth.signup-form",
		"auth.forgot-password-form", "auth.reset-password-form",
		"auth.setup-form", "auth.dynamic-signup-form",
		"auth.magic-link-form", "auth.oauth-buttons",
		"detail.header",
		"editor.code", "editor.formBuilder",
		"metric.gauge", "dashboard.stat", "dashboard.recentlist",
		"filter.search", "filter.select", "filter.date",
		"pagination.cursor", "pagination.offset",
		"field.json", "field.permissions",
		"settings.tabs", "settings.panel",
	}
	for _, name := range leaves {
		if _, ok := DefaultSlotCatalog[name]; !ok {
			t.Errorf("leaf intent %q missing from catalog", name)
		}
	}
}

// TestSlotCatalog_RichDetailComposition validates a representative composition
// matching the authsome user-detail page: page.shell hosts a resource.detail
// with a detail.header in the header slot, multiple detail.sections containing
// nested resource.list and form.edit, an action.menu in actions, and an empty
// extensions slot for plugin injection.
func TestSlotCatalog_RichDetailComposition(t *testing.T) {
	r := NewRegistry()
	authM := mustManifest(t, `
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - route: /users/:id
    intent: page.shell
    slots:
      main:
        - intent: resource.detail
          slots:
            header:
              - intent: detail.header
            sections:
              - intent: detail.section
                slots:
                  content:
                    - intent: form.edit
              - intent: detail.section
                slots:
                  content:
                    - intent: resource.list
            actions:
              - intent: action.menu
                slots:
                  items: []
            extensions: []
`)
	if err := r.Register(authM); err != nil {
		t.Fatalf("register rich detail composition: %v", err)
	}
}

// TestApplyExtensions_RichDetailExtensions verifies plugins can inject a
// detail.section into a resource.detail.extensions slot, which is the
// primary extension target for the 24 authsome plugins.
func TestApplyExtensions_RichDetailExtensions(t *testing.T) {
	r := NewRegistry()
	authM := mustManifest(t, `
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - route: /users/:id
    intent: page.shell
    slots:
      main:
        - intent: resource.detail
          slots: { extensions: [] }
`)
	if err := r.Register(authM); err != nil {
		t.Fatalf("register host: %v", err)
	}

	pluginM := mustManifest(t, `
schemaVersion: 1
contributor: { name: apikey, envelope: { supports: [v1], preferred: v1 } }
intents: []
extends:
  - target: { contributor: auth, intent: page.shell, route: /users/:id }
    slot: main.extensions
    add:
      - intent: detail.section
`)
	if err := r.Register(pluginM); err != nil {
		t.Fatalf("register plugin extension: %v", err)
	}

	merged, ok := r.MergedGraph("auth", "/users/:id")
	if !ok {
		t.Fatal("merged graph not found")
	}
	ext := merged.Slots["main"][0].Slots["extensions"]
	if len(ext) != 1 || ext[0].Intent != "detail.section" {
		t.Errorf("extension not applied: %+v", ext)
	}
}

// TestSlotCatalog_SettingsTabsComposition validates that settings.tabs can
// be dropped directly into a page.shell.main slot — the standard usage
// pattern for the global /settings page (and for plugin deep-link pages).
func TestSlotCatalog_SettingsTabsComposition(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - route: /settings
    intent: page.shell
    slots:
      main:
        - intent: settings.tabs
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register settings.tabs composition: %v", err)
	}
}

// TestApplyExtensions_DashboardWidgets verifies plugins can inject widgets
// into a dashboard.grid.widgets slot (used by the authsome overview page
// where each plugin contributes a stat tile).
func TestApplyExtensions_DashboardWidgets(t *testing.T) {
	r := NewRegistry()
	authM := mustManifest(t, `
schemaVersion: 1
contributor: { name: auth, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - route: /
    intent: page.shell
    slots:
      main:
        - intent: dashboard.grid
          slots: { widgets: [{ intent: dashboard.stat }] }
`)
	if err := r.Register(authM); err != nil {
		t.Fatalf("register host: %v", err)
	}

	pluginM := mustManifest(t, `
schemaVersion: 1
contributor: { name: mfa, envelope: { supports: [v1], preferred: v1 } }
intents: []
extends:
  - target: { contributor: auth, intent: page.shell, route: / }
    slot: main.widgets
    add:
      - intent: dashboard.stat
`)
	if err := r.Register(pluginM); err != nil {
		t.Fatalf("register plugin widget: %v", err)
	}

	merged, ok := r.MergedGraph("auth", "/")
	if !ok {
		t.Fatal("merged graph not found")
	}
	widgets := merged.Slots["main"][0].Slots["widgets"]
	if len(widgets) != 2 {
		t.Errorf("expected 2 widgets after extension, got %d", len(widgets))
	}
}
