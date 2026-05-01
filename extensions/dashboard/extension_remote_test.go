package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// newTestDashboardExt builds a minimal *Extension wired with just enough
// state to exercise the remote-watcher code path: a registry, a logger, and
// a sane ProxyTimeout. We don't need the full lifecycle (no app, no router)
// because the tests drive WatchRemoteContributor directly.
func newTestDashboardExt(t *testing.T) *Extension {
	t.Helper()

	cfg := DefaultConfig()
	cfg.ProxyTimeout = 1 * time.Second

	base := forge.NewBaseExtension("dashboard", "test", "test")
	base.SetLogger(forge.NewNoopLogger())

	e := &Extension{
		BaseExtension: base,
		config:        cfg,
		registry:      contributor.NewContributorRegistry("/dashboard"),
	}

	return e
}

func writeManifest(w http.ResponseWriter, m *contributor.Manifest) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m) //nolint:errchkjson // test-only encoder write
}

// waitFor polls cond every 10ms until it returns true or the deadline
// elapses. Returns true on success. Used so tests don't have to sleep for a
// fixed duration that might be too long (slow CI) or too short (flaky).
func waitFor(t *testing.T, deadline time.Duration, cond func() bool) bool {
	t.Helper()

	start := time.Now()

	for time.Since(start) < deadline {
		if cond() {
			return true
		}

		time.Sleep(10 * time.Millisecond)
	}

	return cond()
}

func TestWatchRemoteContributor_RegistersOnFirstSuccess(t *testing.T) {
	manifest := &contributor.Manifest{
		Name:        "authsome",
		DisplayName: "Authsome",
		Layout:      "extension",
		Nav: []contributor.NavItem{
			{Label: "Overview", Path: "/", Group: "Authsome"},
			{Label: "Plans", Path: "/plans", Group: "Billing"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeManifest(w, manifest)
	}))
	defer server.Close()

	e := newTestDashboardExt(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.WatchRemoteContributor(ctx, server.URL, "",
		WithInitialBackoff(20*time.Millisecond),
		WithMaxBackoff(50*time.Millisecond),
	); err != nil {
		t.Fatalf("WatchRemoteContributor: %v", err)
	}

	ok := waitFor(t, 2*time.Second, func() bool {
		_, found := e.registry.GetManifest("authsome")

		return found
	})

	if !ok {
		t.Fatalf("watcher never registered authsome — registry contributor names: %v",
			e.registry.ContributorNames())
	}

	if !e.registry.IsRemote("authsome") {
		t.Fatalf("authsome must register as remote, not local")
	}
}

func TestWatchRemoteContributor_RetriesUntilUpstreamReady(t *testing.T) {
	var hits int64

	manifest := &contributor.Manifest{
		Name:   "authsome",
		Layout: "extension",
		Nav:    []contributor.NavItem{{Label: "Overview", Path: "/"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Fail the first 3 requests, then start serving.
		if atomic.AddInt64(&hits, 1) <= 3 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)

			return
		}

		writeManifest(w, manifest)
	}))
	defer server.Close()

	e := newTestDashboardExt(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.WatchRemoteContributor(ctx, server.URL, "",
		WithInitialBackoff(20*time.Millisecond),
		WithMaxBackoff(50*time.Millisecond),
	); err != nil {
		t.Fatalf("WatchRemoteContributor: %v", err)
	}

	ok := waitFor(t, 3*time.Second, func() bool {
		_, found := e.registry.GetManifest("authsome")

		return found
	})

	if !ok {
		t.Fatalf("watcher gave up before upstream became ready (hits=%d)", atomic.LoadInt64(&hits))
	}

	if got := atomic.LoadInt64(&hits); got < 4 {
		t.Errorf("expected at least 4 upstream hits (3 failures + 1 success), got %d", got)
	}
}

func TestWatchRemoteContributor_DedupSameKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeManifest(w, &contributor.Manifest{Name: "authsome", Layout: "extension"})
	}))
	defer server.Close()

	e := newTestDashboardExt(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.WatchRemoteContributor(ctx, server.URL, ""); err != nil {
		t.Fatalf("first WatchRemoteContributor: %v", err)
	}

	// Second call with same baseURL — must be a no-op (no new watcher).
	if err := e.WatchRemoteContributor(ctx, server.URL, ""); err != nil {
		t.Fatalf("second WatchRemoteContributor: %v", err)
	}

	e.remoteWatchersMu.Lock()
	count := len(e.remoteWatchers)
	e.remoteWatchersMu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 active watcher after duplicate calls, got %d", count)
	}
}

func TestWatchRemoteContributor_RefreshPicksUpManifestChange(t *testing.T) {
	// v1 has 1 nav item, v2 has 3 — verify refresh re-registers when the
	// upstream manifest changes mid-flight.
	v1 := &contributor.Manifest{
		Name:   "authsome",
		Layout: "extension",
		Nav:    []contributor.NavItem{{Label: "Overview", Path: "/"}},
	}
	v2 := &contributor.Manifest{
		Name:   "authsome",
		Layout: "extension",
		Nav: []contributor.NavItem{
			{Label: "Overview", Path: "/"},
			{Label: "Plans", Path: "/plans"},
			{Label: "Organizations", Path: "/orgs"},
		},
	}

	var current atomic.Pointer[contributor.Manifest]
	current.Store(v1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeManifest(w, current.Load())
	}))
	defer server.Close()

	e := newTestDashboardExt(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.WatchRemoteContributor(ctx, server.URL, "",
		WithInitialBackoff(10*time.Millisecond),
		WithWatcherRefreshInterval(80*time.Millisecond),
	); err != nil {
		t.Fatalf("WatchRemoteContributor: %v", err)
	}

	ok := waitFor(t, 1*time.Second, func() bool {
		_, found := e.registry.GetManifest("authsome")

		return found
	})

	if !ok {
		t.Fatalf("watcher never made initial registration")
	}

	current.Store(v2)

	// Refresh ticker is 80ms; allow up to ~600ms for the next tick + re-register.
	ok = waitFor(t, 1*time.Second, func() bool {
		m, found := e.registry.GetManifest("authsome")

		return found && len(m.Nav) == 3
	})

	if !ok {
		m, _ := e.registry.GetManifest("authsome")

		var navCount int
		if m != nil {
			navCount = len(m.Nav)
		}

		t.Fatalf("refresh never picked up v2 — current nav count = %d, want 3", navCount)
	}
}

func TestWatchRemoteContributor_StopCancelsWatcher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "always fail", http.StatusInternalServerError)
	}))
	defer server.Close()

	e := newTestDashboardExt(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.WatchRemoteContributor(ctx, server.URL, "",
		WithInitialBackoff(10*time.Millisecond),
		WithMaxBackoff(20*time.Millisecond),
	); err != nil {
		t.Fatalf("WatchRemoteContributor: %v", err)
	}

	e.remoteWatchersMu.Lock()
	count := len(e.remoteWatchers)
	e.remoteWatchersMu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 watcher pre-stop, got %d", count)
	}

	e.stopRemoteWatchers()

	e.remoteWatchersMu.Lock()
	postCount := len(e.remoteWatchers)
	e.remoteWatchersMu.Unlock()

	if postCount != 0 {
		t.Fatalf("expected 0 watchers after stop, got %d", postCount)
	}

	// Give the goroutine a moment to observe the cancellation; a double-stop
	// should be safe even though it's not the typical pattern.
	time.Sleep(50 * time.Millisecond)
	e.stopRemoteWatchers()
}

func TestWatchRemoteContributor_EmptyBaseURLErrors(t *testing.T) {
	e := newTestDashboardExt(t)

	if err := e.WatchRemoteContributor(context.Background(), "", ""); err == nil {
		t.Fatalf("expected error for empty baseURL, got nil")
	}
}
