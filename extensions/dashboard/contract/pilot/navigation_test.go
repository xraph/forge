package pilot

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

func loadPilotIntoRegistry(t *testing.T) contract.Registry {
	t.Helper()
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()
	m, err := loader.Load(strings.NewReader(string(manifestYAML)), "pilot/manifest.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := reg.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg
}

func TestNavigationHandler_GroupsAndSorts(t *testing.T) {
	reg := loadPilotIntoRegistry(t)
	got, err := navigationHandler(reg)(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("navigation: %v", err)
	}
	if len(got.Groups) == 0 {
		t.Fatal("expected at least one group")
	}
	// Overview should come before Operations (priority 0 vs 6).
	overviewIdx, opsIdx := -1, -1
	for i, g := range got.Groups {
		if g.Group == "Overview" {
			overviewIdx = i
		}
		if g.Group == "Operations" {
			opsIdx = i
		}
	}
	if overviewIdx < 0 || opsIdx < 0 {
		t.Fatalf("expected Overview + Operations groups, got %+v", got.Groups)
	}
	if overviewIdx >= opsIdx {
		t.Errorf("Overview (%d) should come before Operations (%d)", overviewIdx, opsIdx)
	}
	// Within Overview, items should be priority-sorted.
	for _, g := range got.Groups {
		if g.Group != "Overview" {
			continue
		}
		for i := 1; i < len(g.Items); i++ {
			if g.Items[i].Priority < g.Items[i-1].Priority {
				t.Errorf("Overview items not priority-sorted: %+v", g.Items)
			}
		}
	}
}

func TestNavigationHandler_SkipsRoutesWithoutNav(t *testing.T) {
	reg := loadPilotIntoRegistry(t)
	got, _ := navigationHandler(reg)(context.Background(), struct{}{}, contract.Principal{})
	// /traces/:id has no Nav (slice j detail route) — make sure it doesn't appear.
	for _, g := range got.Groups {
		for _, item := range g.Items {
			if item.Href == "/traces/:id" {
				t.Errorf("nav included :id detail route: %+v", item)
			}
		}
	}
}

// TestNavigationHandler_PrefixesHrefWithAppSlug guards the URL
// namespacing contract: NavItem.Href returned to the React shell is
// /@<slug><route> when the owning contributor opts into the app
// switcher, and stays unprefixed for library contributors.
func TestNavigationHandler_PrefixesHrefWithAppSlug(t *testing.T) {
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()

	src := `
schemaVersion: 1
contributor:
  name: auth
  envelope: { supports: [v1], preferred: v1 }
  app: { displayName: Authsome, slug: authsome, icon: shield, priority: 10, home: /users }
intents:
  - { name: auth.home, kind: graph, version: 1, capability: render }
graph:
  - route: /users
    intent: page.shell
    title: Users
    nav: { group: People, icon: users, priority: 0 }
  - route: /
    intent: page.shell
    title: Home
    nav: { group: Overview, icon: home, priority: 0 }
`
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

	resp, err := navigationHandler(reg)(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("navigation: %v", err)
	}
	wantHref := map[string]string{
		"/users": "/@authsome/users",
		"/":      "/@authsome", // "/" maps to /@<slug> with no trailing slash
	}
	for _, g := range resp.Groups {
		for _, item := range g.Items {
			// The label "Users" came from the /users route; "Home" from "/".
			// Find which by checking the unprefixed half of the href.
			if want, ok := wantHref[stripSlug(item.Href, "authsome")]; ok {
				if item.Href != want {
					t.Errorf("href = %q, want %q", item.Href, want)
				}
			}
		}
	}
}

// stripSlug is a test helper that undoes /@<slug> prefixing so tests can
// look up expectations by the unprefixed route.
func stripSlug(href, slug string) string {
	prefix := "/@" + slug
	if href == prefix {
		return "/"
	}
	if len(href) > len(prefix) && href[:len(prefix)] == prefix {
		return href[len(prefix):]
	}
	return href
}

func TestNavigationHandler_NilRegistryUnavailable(t *testing.T) {
	_, err := navigationHandler(nil)(context.Background(), struct{}{}, contract.Principal{})
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}

// TestNavigationHandler_DeterministicAcrossRuns guards against
// map-iteration randomness in registry.All(). Multiple contributors with
// identical (group, priority, label) blocks would previously render in
// non-deterministic order because reg.All() returns map values in Go's
// randomised iteration order, and sort.SliceStable preserves that order
// for ties.
//
// The fix sorts manifests by contributor name before iterating and adds
// contributor as the final tiebreaker in the item sort comparator. This
// test invokes the handler 25 times and asserts byte-identical output —
// flaky if either guard regresses.
func TestNavigationHandler_DeterministicAcrossRuns(t *testing.T) {
	reg := contract.NewRegistry()
	wreg := contract.NewWardenRegistry()

	// Three contributors, each contributing one nav item under the same
	// group "Overview" at priority 0 with the same label "Home". The only
	// distinguishing field is the contributor itself — exactly the
	// scenario the tiebreaker has to resolve.
	srcs := []string{
		`
schemaVersion: 1
contributor: { name: alpha, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: alpha.home, kind: graph, version: 1, capability: render }
graph:
  - route: /alpha
    intent: page.shell
    title: Home
    nav: { group: Overview, icon: home, priority: 0 }
`,
		`
schemaVersion: 1
contributor: { name: bravo, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: bravo.home, kind: graph, version: 1, capability: render }
graph:
  - route: /bravo
    intent: page.shell
    title: Home
    nav: { group: Overview, icon: home, priority: 0 }
`,
		`
schemaVersion: 1
contributor: { name: charlie, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: charlie.home, kind: graph, version: 1, capability: render }
graph:
  - route: /charlie
    intent: page.shell
    title: Home
    nav: { group: Overview, icon: home, priority: 0 }
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

	first, err := navigationHandler(reg)(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("navigation: %v", err)
	}
	// Sanity: three items in Overview, ordered alpha → bravo → charlie
	// (the contributor-name tiebreaker).
	var overview *NavGroup
	for i := range first.Groups {
		if first.Groups[i].Group == "Overview" {
			overview = &first.Groups[i]
			break
		}
	}
	if overview == nil {
		t.Fatal("expected Overview group in first response")
	}
	if len(overview.Items) != 3 {
		t.Fatalf("Overview items = %d, want 3", len(overview.Items))
	}
	wantOrder := []string{"alpha", "bravo", "charlie"}
	for i, w := range wantOrder {
		if overview.Items[i].Contributor != w {
			t.Errorf("Overview[%d].Contributor = %q, want %q (full: %+v)",
				i, overview.Items[i].Contributor, w, overview.Items)
		}
	}

	// 25 more calls should produce byte-identical output. If they don't,
	// either reg.All() ordering leaked through or a comparator returned
	// non-deterministic results for equal keys.
	for i := 0; i < 25; i++ {
		next, err := navigationHandler(reg)(context.Background(), struct{}{}, contract.Principal{})
		if err != nil {
			t.Fatalf("navigation iter %d: %v", i, err)
		}
		if len(next.Groups) != len(first.Groups) {
			t.Fatalf("iter %d: group count drifted (%d vs %d)", i, len(next.Groups), len(first.Groups))
		}
		for gi := range next.Groups {
			if next.Groups[gi].Group != first.Groups[gi].Group {
				t.Fatalf("iter %d: group[%d] = %q, want %q", i, gi, next.Groups[gi].Group, first.Groups[gi].Group)
			}
			if len(next.Groups[gi].Items) != len(first.Groups[gi].Items) {
				t.Fatalf("iter %d: group[%d] item count drift", i, gi)
			}
			for ii := range next.Groups[gi].Items {
				if next.Groups[gi].Items[ii].Contributor != first.Groups[gi].Items[ii].Contributor {
					t.Fatalf("iter %d: group[%d].items[%d].contributor = %q, want %q",
						i, gi, ii, next.Groups[gi].Items[ii].Contributor, first.Groups[gi].Items[ii].Contributor)
				}
			}
		}
	}
}
