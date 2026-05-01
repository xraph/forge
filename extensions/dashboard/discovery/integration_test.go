package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// fakeDiscovery is a deterministic DiscoveryService used to drive reconcile.
type fakeDiscovery struct {
	mu        sync.Mutex
	services  []string
	instances map[string][]*ServiceInstance // keyed by service name
}

func newFakeDiscovery() *fakeDiscovery {
	return &fakeDiscovery{
		instances: map[string][]*ServiceInstance{},
	}
}

func (f *fakeDiscovery) set(serviceName string, instances ...*ServiceInstance) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.instances[serviceName]; !ok {
		f.services = append(f.services, serviceName)
	}

	f.instances[serviceName] = instances
}

func (f *fakeDiscovery) clear(serviceName string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.instances, serviceName)

	for i, name := range f.services {
		if name == serviceName {
			f.services = append(f.services[:i], f.services[i+1:]...)

			break
		}
	}
}

func (f *fakeDiscovery) ListServices(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, len(f.services))
	copy(out, f.services)

	return out, nil
}

func (f *fakeDiscovery) DiscoverWithTags(_ context.Context, name string, tags []string) ([]*ServiceInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	insts := f.instances[name]
	if len(tags) == 0 {
		return insts, nil
	}

	out := make([]*ServiceInstance, 0, len(insts))

	for _, inst := range insts {
		if hasAllTags(inst.Tags, tags) {
			out = append(out, inst)
		}
	}

	return out, nil
}

func hasAllTags(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}

	return true
}

// manifestServer serves a swappable manifest fixture and counts requests so
// tests can assert refresh frequency.
type manifestServer struct {
	server *httptest.Server
	hits   int64

	mu       sync.Mutex
	manifest *contributor.Manifest
}

func newManifestServer(t *testing.T, initial *contributor.Manifest) *manifestServer {
	t.Helper()

	ms := &manifestServer{manifest: initial}
	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&ms.hits, 1)

		ms.mu.Lock()
		manifest := ms.manifest
		ms.mu.Unlock()

		switch r.URL.Path {
		case "/_forge/dashboard/manifest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(manifest)
		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(ms.server.Close)

	return ms
}

func (ms *manifestServer) setManifest(m *contributor.Manifest) {
	ms.mu.Lock()
	ms.manifest = m
	ms.mu.Unlock()
}

func (ms *manifestServer) hitCount() int64 {
	return atomic.LoadInt64(&ms.hits)
}

// instanceFor splits a server URL into host/port and builds a ServiceInstance.
func instanceFor(t *testing.T, ms *manifestServer, id string, healthy bool, tags ...string) *ServiceInstance {
	t.Helper()

	u, err := url.Parse(ms.server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	host := u.Hostname()
	portStr := u.Port()

	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}

	status := "passing"
	if !healthy {
		status = "critical"
	}

	if len(tags) == 0 {
		tags = []string{"forge-dashboard-contributor"}
	}

	return &ServiceInstance{
		ID:      id,
		Name:    "identity",
		Address: host,
		Port:    port,
		Tags:    tags,
		Status:  status,
	}
}

func newTestIntegration(t *testing.T) (*Integration, *contributor.ContributorRegistry, *fakeDiscovery) {
	t.Helper()

	registry := contributor.NewContributorRegistry("/dashboard")
	disc := newFakeDiscovery()

	integ := NewIntegration(
		disc,
		registry,
		"forge-dashboard-contributor",
		60*time.Second,
		2*time.Second,
		forge.NewNoopLogger(),
	)

	return integ, registry, disc
}

func TestIntegration_RegistersServiceWithDashboardTag(t *testing.T) {
	manifest := &contributor.Manifest{
		Name:        "authsome",
		DisplayName: "Authsome",
		Layout:      "extension",
		Nav: []contributor.NavItem{
			{Label: "Overview", Path: "/", Group: "Authsome"},
			{Label: "Organizations", Path: "/organizations", Group: "Authentication"},
		},
	}

	ms := newManifestServer(t, manifest)
	integ, registry, disc := newTestIntegration(t)
	disc.set("identity", instanceFor(t, ms, "identity-1", true))

	integ.reconcile(context.Background())

	got, ok := registry.GetManifest("authsome")
	if !ok {
		t.Fatalf("expected authsome in registry after reconcile")
	}

	if !registry.IsRemote("authsome") {
		t.Fatalf("authsome should be registered as remote")
	}

	if got.DisplayName != "Authsome" {
		t.Errorf("manifest display name = %q, want Authsome", got.DisplayName)
	}

	groups := registry.GetExtensionNavGroups("authsome")
	if len(groups) == 0 {
		t.Fatalf("expected non-empty extension nav groups for remote authsome")
	}
}

func TestIntegration_UnregistersDepartedService(t *testing.T) {
	ms := newManifestServer(t, &contributor.Manifest{Name: "authsome", Layout: "extension"})
	integ, registry, disc := newTestIntegration(t)
	disc.set("identity", instanceFor(t, ms, "identity-1", true))

	integ.reconcile(context.Background())

	if _, ok := registry.GetManifest("authsome"); !ok {
		t.Fatalf("setup: expected authsome registered before churn")
	}

	disc.clear("identity")
	integ.reconcile(context.Background())

	if _, ok := registry.GetManifest("authsome"); ok {
		t.Fatalf("authsome should be unregistered after instance disappears")
	}
}

func TestIntegration_RefreshesManifestOnChange(t *testing.T) {
	v1 := &contributor.Manifest{
		Name:   "authsome",
		Layout: "extension",
		Nav: []contributor.NavItem{
			{Label: "Overview", Path: "/", Group: "Authsome"},
		},
	}

	ms := newManifestServer(t, v1)
	integ, registry, disc := newTestIntegration(t)
	disc.set("identity", instanceFor(t, ms, "identity-1", true))

	integ.reconcile(context.Background())

	if got := registry.GetExtensionNavGroups("authsome"); len(got) != 1 {
		t.Fatalf("after first reconcile, want 1 nav group, got %d", len(got))
	}

	// Plugin loads on identity → manifest now has new nav items.
	v2 := &contributor.Manifest{
		Name:   "authsome",
		Layout: "extension",
		Nav: []contributor.NavItem{
			{Label: "Overview", Path: "/", Group: "Authsome"},
			{Label: "Organizations", Path: "/organizations", Group: "Authentication"},
			{Label: "Plans", Path: "/plans", Group: "Billing"},
		},
	}
	ms.setManifest(v2)

	integ.reconcile(context.Background())

	groups := registry.GetExtensionNavGroups("authsome")
	if len(groups) != 3 {
		t.Fatalf("after refresh, want 3 nav groups, got %d (%v)", len(groups), groupNames(groups))
	}

	if !containsGroup(groups, "Authentication") {
		t.Errorf("missing Authentication group after refresh: %v", groupNames(groups))
	}

	if !containsGroup(groups, "Billing") {
		t.Errorf("missing Billing group after refresh: %v", groupNames(groups))
	}
}

func TestIntegration_NoReregisterWhenManifestUnchanged(t *testing.T) {
	manifest := &contributor.Manifest{
		Name:   "authsome",
		Layout: "extension",
		Nav: []contributor.NavItem{
			{Label: "Overview", Path: "/", Group: "Authsome"},
		},
	}

	ms := newManifestServer(t, manifest)
	integ, registry, disc := newTestIntegration(t)
	disc.set("identity", instanceFor(t, ms, "identity-1", true))

	integ.reconcile(context.Background())

	// Capture the registry's manifest pointer; if refresh re-registers
	// unnecessarily it would replace the pointer with a freshly decoded copy.
	first, _ := registry.GetManifest("authsome")

	// Two more passes with the same fixture must not change identity-equality
	// of the stored manifest pointer.
	integ.reconcile(context.Background())
	integ.reconcile(context.Background())

	second, _ := registry.GetManifest("authsome")
	if first != second {
		t.Fatalf("manifest pointer changed across no-op refresh — re-registered unnecessarily")
	}

	// Manifest fetches DO occur on every poll (correct — that's how we detect
	// changes). The hit count just needs to be ≥ 3 (initial + 2 refreshes).
	if hits := ms.hitCount(); hits < 3 {
		t.Errorf("expected ≥ 3 manifest fetches across 3 reconciles, got %d", hits)
	}
}

func TestIntegration_SkipsUnhealthyInstances(t *testing.T) {
	ms := newManifestServer(t, &contributor.Manifest{Name: "authsome", Layout: "extension"})
	integ, registry, disc := newTestIntegration(t)
	disc.set("identity", instanceFor(t, ms, "identity-1", false))

	integ.reconcile(context.Background())

	if _, ok := registry.GetManifest("authsome"); ok {
		t.Fatalf("unhealthy instance should not have been registered")
	}
}

func TestIntegration_SkipsServicesWithoutTag(t *testing.T) {
	ms := newManifestServer(t, &contributor.Manifest{Name: "authsome", Layout: "extension"})
	integ, registry, disc := newTestIntegration(t)
	disc.set("identity", instanceFor(t, ms, "identity-1", true, "some-other-tag"))

	integ.reconcile(context.Background())

	if _, ok := registry.GetManifest("authsome"); ok {
		t.Fatalf("instance without dashboard tag should not have been registered")
	}
}

func TestManifestsEqual_DiffersOnNavChange(t *testing.T) {
	a := &contributor.Manifest{Name: "x", Nav: []contributor.NavItem{{Path: "/"}}}
	b := &contributor.Manifest{Name: "x", Nav: []contributor.NavItem{{Path: "/"}}}
	c := &contributor.Manifest{Name: "x", Nav: []contributor.NavItem{{Path: "/"}, {Path: "/users"}}}

	if !manifestsEqual(a, b) {
		t.Errorf("identical manifests should be equal")
	}

	if manifestsEqual(a, c) {
		t.Errorf("manifests with different nav should not be equal")
	}

	if !manifestsEqual(nil, nil) {
		t.Errorf("nil/nil should be equal")
	}

	if manifestsEqual(a, nil) {
		t.Errorf("nil/non-nil should not be equal")
	}
}

func groupNames(groups []contributor.NavGroup) []string {
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}

	return names
}

func containsGroup(groups []contributor.NavGroup, name string) bool {
	for _, g := range groups {
		if g.Name == name {
			return true
		}
	}

	return false
}

// Force fmt import (used by httptest error paths) — keeps go vet happy if
// httptest helpers ever need fmt.Errorf during test assertions.
var _ = fmt.Sprintf
