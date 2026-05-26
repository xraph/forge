package components

import (
	"sort"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// TestRegistry_everyBuilderEmitsKnownIntent exercises every public builder
// constructor in this package and asserts the resulting GraphNode's Intent
// string matches one of the catalog constants. Protects against:
//   - intent string typos in builder constructors
//   - silent renames that drift the React shell out of sync
//   - new builders that forget to add an entry to Intents()
func TestRegistry_everyBuilderEmitsKnownIntent(t *testing.T) {
	known := IntentSet()

	// Construct every builder with minimal arguments; assert its Build()
	// returns a node whose Intent is one of the known catalog strings.
	cases := []struct {
		name string
		node contract.GraphNode
	}{
		// Layout
		{"Row", Row().Build()},
		{"Column", Column().Build()},
		{"Grid", Grid().Build()},
		{"Stack", Stack().Build()},
		{"Container", Container().Build()},
		{"Section", Section().Build()},
		{"Card", Card().Build()},
		{"Tabs", Tabs().Build()},
		{"Accordion", Accordion().Build()},
		{"Split", Split().Build()},
		{"Divider", Divider().Build()},
		{"Spacer", Spacer().Build()},
		{"Page", Page("").Build()},

		// Atoms
		{"Button", Button("").Build()},
		{"IconButton", IconButton("x").Build()},
		{"Badge", Badge("").Build()},
		{"Label", Label("").Build()},
		{"Text", Text("").Build()},
		{"Heading", Heading("").Build()},
		{"Link", Link("", "").Build()},
		{"Icon", Icon("x").Build()},
		{"Avatar", Avatar().Build()},
		{"Image", Image("").Build()},
		{"Separator", Separator().Build()},
		{"Skeleton", Skeleton().Build()},
		{"Spinner", Spinner().Build()},
		{"Progress", Progress().Build()},
		{"Input", Input("x").Build()},
		{"Textarea", Textarea("x").Build()},
		{"Checkbox", Checkbox("x").Build()},
		{"Radio", Radio("x").Build()},
		{"Switch", Switch("x").Build()},
		{"Slider", Slider("x").Build()},
		{"Select", Select("x").Build()},
		{"Kbd", Kbd("x").Build()},
		{"Code", Code("").Build()},
		{"Tooltip", Tooltip("").Build()},

		// Molecules
		{"Field", Field("").Build()},
		{"StatCard", StatCard("").Build()},
		{"Alert", Alert().Build()},
		{"SearchBar", SearchBar().Build()},
		{"EmptyState", EmptyState("").Build()},
		{"ErrorState", ErrorState("").Build()},
		{"LoadingState", LoadingState().Build()},
		{"Breadcrumb", Breadcrumb().Build()},
		{"Pagination", Pagination().Build()},
		{"TagInput", TagInput("x").Build()},
		{"FileUpload", FileUpload("x").Build()},
		{"DatePicker", DatePicker("x").Build()},
		{"Combobox", Combobox("x").Build()},
		{"CommandPalette", CommandPalette().Build()},
		{"DropdownMenu", DropdownMenu().Build()},
		{"ListItem", ListItem("").Build()},
		{"Chip", Chip("").Build()},
		{"Rating", Rating().Build()},

		// Organisms
		{"DataGrid", DataGrid().Build()},
		{"DynamicForm", DynamicForm().Build()},
		{"Kanban", Kanban().Build()},
		{"Calendar", Calendar().Build()},
		{"Timeline", Timeline().Build()},
		{"TreeView", TreeView().Build()},
		{"Stepper", Stepper().Build()},
		{"Chart", Chart(ChartLine).Build()},
		{"CodeEditor", CodeEditor().Build()},
		{"NotificationCenter", NotificationCenter().Build()},
		{"Comments", Comments().Build()},

		// Auth
		{"SignInForm", SignInForm().Build()},
		{"SignUpForm", SignUpForm().Build()},
		{"AuthTabs", AuthTabs().Build()},
		{"MagicLinkForm", MagicLinkForm().Build()},
		{"OAuthButtons", OAuthButtons().Build()},
		{"UserButton", UserButton().Build()},
		{"AccountMenu", AccountMenu().Build()},
		{"OrgSwitcher", OrgSwitcher().Build()},
		{"OrgProfile", OrgProfile().Build()},
		{"TwoFactorSetup", TwoFactorSetup().Build()},
		{"PasskeyPrompt", PasskeyPrompt().Build()},
		{"SessionList", SessionList().Build()},
	}

	seen := make(map[string]struct{}, len(cases))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.node.Intent == "" {
				t.Fatalf("%s: empty intent", c.name)
			}
			if _, ok := known[c.node.Intent]; !ok {
				t.Errorf("%s: emitted intent %q is not in the catalog", c.name, c.node.Intent)
			}
			seen[c.node.Intent] = struct{}{}
		})
	}

	// Every catalog entry should have been emitted by at least one builder.
	// Catches catalog entries with no corresponding builder.
	missing := []string{}
	for name := range known {
		if _, ok := seen[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("catalog entries with no builder coverage: %v", missing)
	}
}

// TestCatalog_count is a tripwire that catches accidental additions/removals
// from Intents() without test coverage. Update the expected count when
// intentionally adding or removing intents.
func TestCatalog_count(t *testing.T) {
	got := len(Intents())
	// 13 layout + 24 atoms + 18 molecules + 11 organisms + 12 auth.
	want := 78
	if got != want {
		t.Errorf("intent count: want %d, got %d — update this test when adding/removing intents", want, got)
	}
}

// TestCatalog_unique guarantees no duplicate strings slipped into Intents().
func TestCatalog_unique(t *testing.T) {
	seen := make(map[string]struct{})
	for _, n := range Intents() {
		if _, dup := seen[n]; dup {
			t.Errorf("duplicate intent %q in catalog", n)
		}
		seen[n] = struct{}{}
	}
}

// TestCatalog_namingConvention enforces the "tier.thing" convention every
// intent must follow (layout/atom/molecule/organism/auth).
func TestCatalog_namingConvention(t *testing.T) {
	prefixes := map[string]bool{
		"layout.":   true,
		"atom.":     true,
		"molecule.": true,
		"organism.": true,
		"auth.":     true,
	}
	for _, n := range Intents() {
		ok := false
		for p := range prefixes {
			if len(n) > len(p) && n[:len(p)] == p {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("intent %q violates tier.thing naming convention", n)
		}
	}
}
