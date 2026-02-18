package contributor

import (
	"context"
	"fmt"
	"sync"
	"testing"

	g "maragu.dev/gomponents"
)

// stubLocal is a minimal LocalContributor for testing.
type stubLocal struct {
	manifest *Manifest
}

func (s *stubLocal) Manifest() *Manifest { return s.manifest }
func (s *stubLocal) RenderPage(_ context.Context, _ string, _ Params) (g.Node, error) {
	return nil, nil
}
func (s *stubLocal) RenderWidget(_ context.Context, _ string) (g.Node, error)   { return nil, nil }
func (s *stubLocal) RenderSettings(_ context.Context, _ string) (g.Node, error) { return nil, nil }

// stubRemote is a minimal DashboardContributor for testing.
type stubRemote struct {
	manifest *Manifest
}

func (s *stubRemote) Manifest() *Manifest { return s.manifest }

func newStub(name string, navItems []NavItem, widgets []WidgetDescriptor, settings []SettingsDescriptor) *stubLocal {
	return &stubLocal{
		manifest: &Manifest{
			Name:     name,
			Nav:      navItems,
			Widgets:  widgets,
			Settings: settings,
		},
	}
}

func TestNewContributorRegistry(t *testing.T) {
	r := NewContributorRegistry()
	if r == nil {
		t.Fatal("NewContributorRegistry returned nil")
	}
	if r.ContributorCount() != 0 {
		t.Errorf("expected 0 contributors, got %d", r.ContributorCount())
	}
	names := r.ContributorNames()
	if len(names) != 0 {
		t.Errorf("expected empty names, got %v", names)
	}
}

func TestRegisterLocal(t *testing.T) {
	r := NewContributorRegistry()
	c := newStub("auth", nil, nil, nil)

	err := r.RegisterLocal(c)
	if err != nil {
		t.Fatalf("RegisterLocal failed: %v", err)
	}

	if r.ContributorCount() != 1 {
		t.Errorf("expected 1 contributor, got %d", r.ContributorCount())
	}
	if !r.IsLocal("auth") {
		t.Error("expected auth to be local")
	}
	if r.IsRemote("auth") {
		t.Error("expected auth to NOT be remote")
	}
}

func TestRegisterRemote(t *testing.T) {
	r := NewContributorRegistry()
	c := &stubRemote{manifest: &Manifest{Name: "remote-svc"}}

	err := r.RegisterRemote(c)
	if err != nil {
		t.Fatalf("RegisterRemote failed: %v", err)
	}

	if !r.IsRemote("remote-svc") {
		t.Error("expected remote-svc to be remote")
	}
	if r.IsLocal("remote-svc") {
		t.Error("expected remote-svc to NOT be local")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := NewContributorRegistry()

	c1 := newStub("auth", nil, nil, nil)
	c2 := newStub("auth", nil, nil, nil)
	remote := &stubRemote{manifest: &Manifest{Name: "auth"}}

	if err := r.RegisterLocal(c1); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	// Duplicate local
	err := r.RegisterLocal(c2)
	if err == nil {
		t.Error("expected error for duplicate local registration")
	}

	// Local exists, register as remote
	err = r.RegisterRemote(remote)
	if err == nil {
		t.Error("expected error for remote registration when local exists")
	}
}

func TestRegisterRemoteDuplicate(t *testing.T) {
	r := NewContributorRegistry()

	r1 := &stubRemote{manifest: &Manifest{Name: "svc"}}
	r2 := &stubRemote{manifest: &Manifest{Name: "svc"}}
	local := newStub("svc", nil, nil, nil)

	if err := r.RegisterRemote(r1); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	// Duplicate remote
	err := r.RegisterRemote(r2)
	if err == nil {
		t.Error("expected error for duplicate remote registration")
	}

	// Remote exists, register as local
	err = r.RegisterLocal(local)
	if err == nil {
		t.Error("expected error for local registration when remote exists")
	}
}

func TestRegisterNilManifest(t *testing.T) {
	r := NewContributorRegistry()

	err := r.RegisterLocal(&stubLocal{manifest: nil})
	if err == nil {
		t.Error("expected error for nil manifest")
	}

	err = r.RegisterRemote(&stubRemote{manifest: nil})
	if err == nil {
		t.Error("expected error for nil manifest on remote")
	}
}

func TestUnregister(t *testing.T) {
	r := NewContributorRegistry()
	c := newStub("auth", nil, nil, nil)

	_ = r.RegisterLocal(c)
	if r.ContributorCount() != 1 {
		t.Fatal("expected 1 contributor after register")
	}

	err := r.Unregister("auth")
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
	if r.ContributorCount() != 0 {
		t.Errorf("expected 0 contributors after unregister, got %d", r.ContributorCount())
	}

	// Unregister again should fail
	err = r.Unregister("auth")
	if err == nil {
		t.Error("expected error for unregistering non-existent contributor")
	}
}

func TestUnregisterRemote(t *testing.T) {
	r := NewContributorRegistry()
	rc := &stubRemote{manifest: &Manifest{Name: "remote-svc"}}

	_ = r.RegisterRemote(rc)

	err := r.Unregister("remote-svc")
	if err != nil {
		t.Fatalf("Unregister remote failed: %v", err)
	}
	if r.ContributorCount() != 0 {
		t.Errorf("expected 0 after unregister, got %d", r.ContributorCount())
	}
}

func TestFindContributor(t *testing.T) {
	r := NewContributorRegistry()
	local := newStub("local-ext", nil, nil, nil)
	remote := &stubRemote{manifest: &Manifest{Name: "remote-ext"}}

	_ = r.RegisterLocal(local)
	_ = r.RegisterRemote(remote)

	c, ok := r.FindContributor("local-ext")
	if !ok || c == nil {
		t.Error("expected to find local-ext")
	}

	c, ok = r.FindContributor("remote-ext")
	if !ok || c == nil {
		t.Error("expected to find remote-ext")
	}

	_, ok = r.FindContributor("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent")
	}
}

func TestFindLocalContributor(t *testing.T) {
	r := NewContributorRegistry()
	local := newStub("local-ext", nil, nil, nil)
	remote := &stubRemote{manifest: &Manifest{Name: "remote-ext"}}

	_ = r.RegisterLocal(local)
	_ = r.RegisterRemote(remote)

	lc, ok := r.FindLocalContributor("local-ext")
	if !ok || lc == nil {
		t.Error("expected to find local contributor")
	}

	_, ok = r.FindLocalContributor("remote-ext")
	if ok {
		t.Error("expected NOT to find remote as local contributor")
	}
}

func TestContributorNames(t *testing.T) {
	r := NewContributorRegistry()
	_ = r.RegisterLocal(newStub("beta", nil, nil, nil))
	_ = r.RegisterLocal(newStub("alpha", nil, nil, nil))
	_ = r.RegisterRemote(&stubRemote{manifest: &Manifest{Name: "gamma"}})

	names := r.ContributorNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	// Names should be sorted
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("expected sorted names [alpha beta gamma], got %v", names)
	}
}

func TestGetManifest(t *testing.T) {
	r := NewContributorRegistry()
	c := newStub("auth", nil, nil, nil)
	c.manifest.DisplayName = "Authentication"

	_ = r.RegisterLocal(c)

	m, ok := r.GetManifest("auth")
	if !ok || m == nil {
		t.Fatal("expected to get manifest for auth")
	}
	if m.DisplayName != "Authentication" {
		t.Errorf("expected DisplayName 'Authentication', got %q", m.DisplayName)
	}

	_, ok = r.GetManifest("nonexistent")
	if ok {
		t.Error("expected not to find manifest for nonexistent")
	}
}

func TestNavMergeWithGroups(t *testing.T) {
	r := NewContributorRegistry()

	// Contributor 1: Overview group
	core := newStub("core", []NavItem{
		{Label: "Overview", Path: "/", Icon: "home", Group: "Overview", Priority: 0},
		{Label: "Health", Path: "/health", Icon: "heart", Group: "Overview", Priority: 1},
	}, nil, nil)

	// Contributor 2: Platform group
	auth := newStub("auth", []NavItem{
		{Label: "Users", Path: "/users", Icon: "users", Group: "Identity", Priority: 0},
		{Label: "Roles", Path: "/roles", Icon: "shield", Group: "Identity", Priority: 1},
	}, nil, nil)

	// Contributor 3: Operations group
	ops := newStub("ops", []NavItem{
		{Label: "Logs", Path: "/logs", Icon: "file-text", Group: "Operations", Priority: 0},
		{Label: "Alerts", Path: "/alerts", Icon: "bell", Group: "Operations", Priority: 1},
	}, nil, nil)

	_ = r.RegisterLocal(core)
	_ = r.RegisterLocal(auth)
	_ = r.RegisterLocal(ops)

	groups := r.GetNavGroups()

	if len(groups) != 3 {
		t.Fatalf("expected 3 nav groups, got %d", len(groups))
	}

	// Verify group ordering: Overview (0) < Identity (1) < Operations (4)
	if groups[0].Name != "Overview" {
		t.Errorf("expected first group to be Overview, got %q", groups[0].Name)
	}
	if groups[1].Name != "Identity" {
		t.Errorf("expected second group to be Identity, got %q", groups[1].Name)
	}
	if groups[2].Name != "Operations" {
		t.Errorf("expected third group to be Operations, got %q", groups[2].Name)
	}

	// Verify items within Overview group
	if len(groups[0].Items) != 2 {
		t.Fatalf("expected 2 items in Overview, got %d", len(groups[0].Items))
	}
	if groups[0].Items[0].Label != "Overview" {
		t.Errorf("expected first item in Overview to be 'Overview', got %q", groups[0].Items[0].Label)
	}
	if groups[0].Items[1].Label != "Health" {
		t.Errorf("expected second item in Overview to be 'Health', got %q", groups[0].Items[1].Label)
	}
}

func TestNavMergeFullPath(t *testing.T) {
	r := NewContributorRegistry()

	c := newStub("authsome", []NavItem{
		{Label: "Users", Path: "/users", Group: "Identity"},
	}, nil, nil)

	_ = r.RegisterLocal(c)

	groups := r.GetNavGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(groups[0].Items))
	}

	expected := "/dashboard/ext/authsome/pages/users"
	if groups[0].Items[0].FullPath != expected {
		t.Errorf("expected FullPath %q, got %q", expected, groups[0].Items[0].FullPath)
	}
}

func TestNavMergeDefaultGroup(t *testing.T) {
	r := NewContributorRegistry()

	// NavItem with no Group should default to "Platform"
	c := newStub("plugin", []NavItem{
		{Label: "Settings", Path: "/settings"},
	}, nil, nil)

	_ = r.RegisterLocal(c)

	groups := r.GetNavGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "Platform" {
		t.Errorf("expected default group 'Platform', got %q", groups[0].Name)
	}
}

func TestNavMergePriorityWithinGroup(t *testing.T) {
	r := NewContributorRegistry()

	// Two contributors adding items to the same group with different priorities
	c1 := newStub("ext-a", []NavItem{
		{Label: "Beta", Path: "/beta", Group: "Platform", Priority: 10},
	}, nil, nil)
	c2 := newStub("ext-b", []NavItem{
		{Label: "Alpha", Path: "/alpha", Group: "Platform", Priority: 1},
	}, nil, nil)

	_ = r.RegisterLocal(c1)
	_ = r.RegisterLocal(c2)

	groups := r.GetNavGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	// Alpha (priority 1) should come before Beta (priority 10)
	if groups[0].Items[0].Label != "Alpha" {
		t.Errorf("expected Alpha first (priority 1), got %q", groups[0].Items[0].Label)
	}
	if groups[0].Items[1].Label != "Beta" {
		t.Errorf("expected Beta second (priority 10), got %q", groups[0].Items[1].Label)
	}
}

func TestNavMergeCustomGroupLast(t *testing.T) {
	r := NewContributorRegistry()

	// Custom group (not in predefined order) should appear last
	c1 := newStub("core", []NavItem{
		{Label: "Home", Path: "/", Group: "Overview"},
	}, nil, nil)
	c2 := newStub("custom", []NavItem{
		{Label: "Custom Page", Path: "/custom", Group: "MyCustomGroup"},
	}, nil, nil)

	_ = r.RegisterLocal(c1)
	_ = r.RegisterLocal(c2)

	groups := r.GetNavGroups()
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// Overview should be first, custom group last
	if groups[0].Name != "Overview" {
		t.Errorf("expected Overview first, got %q", groups[0].Name)
	}
	if groups[1].Name != "MyCustomGroup" {
		t.Errorf("expected MyCustomGroup last, got %q", groups[1].Name)
	}
}

func TestGetAllWidgets(t *testing.T) {
	r := NewContributorRegistry()

	c1 := newStub("core", nil, []WidgetDescriptor{
		{ID: "health", Title: "Health", Priority: 1},
		{ID: "metrics", Title: "Metrics", Priority: 3},
	}, nil)
	c2 := newStub("ext", nil, []WidgetDescriptor{
		{ID: "custom", Title: "Custom Widget", Priority: 2},
	}, nil)

	_ = r.RegisterLocal(c1)
	_ = r.RegisterLocal(c2)

	widgets := r.GetAllWidgets()
	if len(widgets) != 3 {
		t.Fatalf("expected 3 widgets, got %d", len(widgets))
	}

	// Sorted by priority: 1, 2, 3
	if widgets[0].ID != "health" {
		t.Errorf("expected first widget 'health', got %q", widgets[0].ID)
	}
	if widgets[1].ID != "custom" {
		t.Errorf("expected second widget 'custom', got %q", widgets[1].ID)
	}
	if widgets[2].ID != "metrics" {
		t.Errorf("expected third widget 'metrics', got %q", widgets[2].ID)
	}

	// Verify contributor assignment
	if widgets[0].Contributor != "core" {
		t.Errorf("expected contributor 'core' for health widget, got %q", widgets[0].Contributor)
	}
	if widgets[1].Contributor != "ext" {
		t.Errorf("expected contributor 'ext' for custom widget, got %q", widgets[1].Contributor)
	}
}

func TestGetAllSettings(t *testing.T) {
	r := NewContributorRegistry()

	c := newStub("auth", nil, nil, []SettingsDescriptor{
		{ID: "security", Title: "Security Settings", Priority: 2},
		{ID: "general", Title: "General Settings", Priority: 1},
	})

	_ = r.RegisterLocal(c)

	settings := r.GetAllSettings()
	if len(settings) != 2 {
		t.Fatalf("expected 2 settings, got %d", len(settings))
	}

	// Sorted by priority
	if settings[0].ID != "general" {
		t.Errorf("expected first setting 'general', got %q", settings[0].ID)
	}
	if settings[1].ID != "security" {
		t.Errorf("expected second setting 'security', got %q", settings[1].ID)
	}
}

func TestLazyRebuild(t *testing.T) {
	r := NewContributorRegistry()

	c := newStub("core", []NavItem{
		{Label: "Home", Path: "/", Group: "Overview"},
	}, nil, nil)
	_ = r.RegisterLocal(c)

	// First call triggers rebuild
	groups1 := r.GetNavGroups()
	if len(groups1) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups1))
	}

	// Register another → dirty again
	c2 := newStub("ext", []NavItem{
		{Label: "Custom", Path: "/custom", Group: "Platform"},
	}, nil, nil)
	_ = r.RegisterLocal(c2)

	// Next call triggers rebuild with both contributors
	groups2 := r.GetNavGroups()
	if len(groups2) != 2 {
		t.Fatalf("expected 2 groups after second register, got %d", len(groups2))
	}
}

func TestUnregisterTriggersRebuild(t *testing.T) {
	r := NewContributorRegistry()

	c1 := newStub("core", []NavItem{
		{Label: "Home", Path: "/", Group: "Overview"},
	}, nil, nil)
	c2 := newStub("ext", []NavItem{
		{Label: "Custom", Path: "/custom", Group: "Platform"},
	}, nil, nil)

	_ = r.RegisterLocal(c1)
	_ = r.RegisterLocal(c2)

	groups := r.GetNavGroups()
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	_ = r.Unregister("ext")

	groups = r.GetNavGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group after unregister, got %d", len(groups))
	}
	if groups[0].Name != "Overview" {
		t.Errorf("expected remaining group to be Overview, got %q", groups[0].Name)
	}
}

func TestEmptyRegistryNoErrors(t *testing.T) {
	r := NewContributorRegistry()

	// All getters should work fine on empty registry
	groups := r.GetNavGroups()
	if groups == nil {
		t.Error("expected non-nil slice from GetNavGroups")
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}

	widgets := r.GetAllWidgets()
	if len(widgets) != 0 {
		t.Errorf("expected 0 widgets, got %d", len(widgets))
	}

	settings := r.GetAllSettings()
	if len(settings) != 0 {
		t.Errorf("expected 0 settings, got %d", len(settings))
	}

	_, ok := r.FindContributor("nonexistent")
	if ok {
		t.Error("expected not to find contributor in empty registry")
	}

	if r.IsLocal("x") {
		t.Error("expected IsLocal to be false for empty registry")
	}
	if r.IsRemote("x") {
		t.Error("expected IsRemote to be false for empty registry")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewContributorRegistry()
	const goroutines = 10

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // register, query, unregister

	// Concurrent registrations
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("contrib-%d", idx)
			c := newStub(name, []NavItem{
				{Label: name, Path: "/" + name, Group: "Platform"},
			}, nil, nil)
			_ = r.RegisterLocal(c)
		}(i)
	}

	// Concurrent reads (may see partial state, that's fine)
	for range goroutines {
		go func() {
			defer wg.Done()
			_ = r.GetNavGroups()
			_ = r.GetAllWidgets()
			_ = r.ContributorNames()
			_ = r.ContributorCount()
		}()
	}

	// Concurrent unregistrations (some may fail, that's fine)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("contrib-%d", idx)
			_ = r.Unregister(name)
		}(i)
	}

	wg.Wait()

	// Registry should be in a consistent state after all goroutines complete
	// We can't assert exact counts because of race between register and unregister
	_ = r.GetNavGroups()
	_ = r.GetAllWidgets()
	_ = r.ContributorNames()
}

func TestExplicitRebuildNavigation(t *testing.T) {
	r := NewContributorRegistry()

	c := newStub("core", []NavItem{
		{Label: "Home", Path: "/", Group: "Overview"},
	}, nil, nil)
	_ = r.RegisterLocal(c)

	// Explicit rebuild
	r.RebuildNavigation()

	groups := r.GetNavGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
}
