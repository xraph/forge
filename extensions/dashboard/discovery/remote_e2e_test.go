package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// TestRemoteSidebar_FromAuthsomeManifest is the headline regression check the
// user asked for: an extension-layout remote contributor whose manifest was
// dynamically built (plugins added items at runtime) must surface every nav
// group through the dashboard registry's GetExtensionEntries /
// GetExtensionNavGroups APIs, with /remote/<name>/pages prefixes — no items
// dropped, no /ext/ leaks.
func TestRemoteSidebar_FromAuthsomeManifest(t *testing.T) {
	manifestPath := filepath.Join("..", "contributor", "testdata", "manifest_full.json")

	manifestBody, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read fixture (%s): %v", manifestPath, err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_forge/dashboard/manifest" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write(manifestBody)
	}))
	defer upstream.Close()

	registry := contributor.NewContributorRegistry("/dashboard")

	disc := newFakeDiscovery()
	disc.set("identity", instanceFromURL(t, "id-1", upstream.URL))

	integ := NewIntegration(
		disc,
		registry,
		"forge-dashboard-contributor",
		60*time.Second,
		2*time.Second,
		forge.NewNoopLogger(),
	)

	integ.reconcile(context.Background())

	if !registry.IsRemote("authsome") {
		t.Fatalf("authsome should be registered as remote after discovery reconcile")
	}

	// 1. App grid entries — authsome appears with /remote/ prefix.
	entries := registry.GetExtensionEntries("/dashboard")

	var authsomeEntry *contributor.ExtensionEntry

	for i := range entries {
		if entries[i].Name == "authsome" {
			authsomeEntry = &entries[i]

			break
		}
	}

	if authsomeEntry == nil {
		t.Fatalf("authsome missing from GetExtensionEntries: %+v", entries)
	}

	if !strings.HasPrefix(authsomeEntry.BasePath, "/dashboard/remote/authsome/pages") {
		t.Fatalf("entry BasePath = %q, want /dashboard/remote/authsome/pages prefix", authsomeEntry.BasePath)
	}

	// 2. Sidebar groups — every group from the live manifest must surface, and
	//    every resolved nav path must use /remote/.
	groups := registry.GetExtensionNavGroups("authsome")
	if len(groups) == 0 {
		t.Fatalf("GetExtensionNavGroups returned no groups for remote authsome")
	}

	wantGroups := []string{
		"Authsome",
		"User Management",
		"Access Control",
		"Configuration",
		"Developer",
		"Authentication",
		"Security",
		"Billing",
		"Provisioning",
	}

	have := map[string]bool{}
	for _, g := range groups {
		have[g.Name] = true

		for _, item := range g.Items {
			if !strings.HasPrefix(item.FullPath, "/dashboard/remote/authsome/pages/") {
				t.Errorf("nav item %q resolved to %q — must use /remote/ prefix", item.Label, item.FullPath)
			}
		}
	}

	for _, g := range wantGroups {
		if !have[g] {
			t.Errorf("missing nav group %q (have %v) — plugin nav merging broke at the discovery boundary", g, mapKeys(have))
		}
	}

	// 3. Spot-check plugin-contributed items.
	wantItems := map[string]string{
		"Organizations":     "/dashboard/remote/authsome/pages/organizations",
		"OAuth2 Clients":    "/dashboard/remote/authsome/pages/oauth2-clients",
		"SCIM":              "/dashboard/remote/authsome/pages/scim",
		"Plans":             "/dashboard/remote/authsome/pages/plans",
		"Anomaly Detection": "/dashboard/remote/authsome/pages/anomaly-detection",
		"Passkeys":          "/dashboard/remote/authsome/pages/passkeys",
	}

	for label, wantPath := range wantItems {
		found := false

		for _, g := range groups {
			for _, item := range g.Items {
				if item.Label == label {
					found = true

					if item.FullPath != wantPath {
						t.Errorf("item %q full path = %q, want %q", label, item.FullPath, wantPath)
					}

					break
				}
			}
		}

		if !found {
			t.Errorf("plugin-contributed item %q not present in sidebar groups", label)
		}
	}
}

func TestRemoteSidebar_RefreshAfterPluginAddedRemotely(t *testing.T) {
	v1 := `{"name":"authsome","display_name":"Authsome","layout":"extension","show_sidebar":true,"nav":[
		{"label":"Overview","path":"/","group":"Authsome"}
	],"widgets":[],"settings":[]}`

	v2 := `{"name":"authsome","display_name":"Authsome","layout":"extension","show_sidebar":true,"nav":[
		{"label":"Overview","path":"/","group":"Authsome"},
		{"label":"Organizations","path":"/organizations","group":"Authentication"}
	],"widgets":[],"settings":[]}`

	body := v1

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()

	registry := contributor.NewContributorRegistry("/dashboard")

	disc := newFakeDiscovery()
	disc.set("identity", instanceFromURL(t, "id-1", upstream.URL))

	integ := NewIntegration(
		disc,
		registry,
		"forge-dashboard-contributor",
		60*time.Second,
		2*time.Second,
		forge.NewNoopLogger(),
	)

	integ.reconcile(context.Background())

	if got := len(registry.GetExtensionNavGroups("authsome")); got != 1 {
		t.Fatalf("after first reconcile: %d groups, want 1", got)
	}

	body = v2

	integ.reconcile(context.Background())

	groups := registry.GetExtensionNavGroups("authsome")
	if len(groups) != 2 {
		t.Fatalf("after refresh: %d groups, want 2 (Authsome + Authentication)", len(groups))
	}

	authentication := false

	for _, g := range groups {
		if g.Name == "Authentication" {
			authentication = true

			for _, item := range g.Items {
				if !strings.HasPrefix(item.FullPath, "/dashboard/remote/authsome/pages/") {
					t.Errorf("Authentication item %q has prefix %q, want /remote/", item.Label, item.FullPath)
				}
			}
		}
	}

	if !authentication {
		t.Fatalf("Authentication group missing after refresh — manifest refresh path didn't propagate plugin nav")
	}
}

func instanceFromURL(t *testing.T, id, raw string) *ServiceInstance {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	return &ServiceInstance{
		ID:      id,
		Name:    "identity",
		Address: u.Hostname(),
		Port:    port,
		Tags:    []string{"forge-dashboard-contributor"},
		Status:  "passing",
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
