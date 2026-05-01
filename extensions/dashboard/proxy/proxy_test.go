package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

func newTestRegistryWithRemote(t *testing.T, name, baseURL string) *contributor.ContributorRegistry {
	t.Helper()

	reg := contributor.NewContributorRegistry("/dashboard")
	rc := contributor.NewRemoteContributor(baseURL, &contributor.Manifest{
		Name:   name,
		Layout: "extension",
	})

	if err := reg.RegisterRemote(rc); err != nil {
		t.Fatalf("RegisterRemote: %v", err)
	}

	return reg
}

func newProxyForTest(t *testing.T, registry *contributor.ContributorRegistry) *FragmentProxy {
	t.Helper()

	return NewFragmentProxy(
		registry,
		32,                   // cache max size
		200*time.Millisecond, // cache TTL — short so stale tests are fast
		2*time.Second,        // upstream timeout
		forge.NewNoopLogger(),
	)
}

func TestFragmentProxy_FetchPage_ForwardsQuery(t *testing.T) {
	var (
		seenPath  atomic.Value
		seenQuery atomic.Value
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath.Store(r.URL.Path)
		seenQuery.Store(r.URL.RawQuery)

		_, _ = w.Write([]byte("<div>detail</div>"))
	}))
	defer server.Close()

	reg := newTestRegistryWithRemote(t, "authsome", server.URL)
	p := newProxyForTest(t, reg)

	body, err := p.FetchPage(context.Background(), "authsome", "/users/detail", "id=ausr_1&extra=2")
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}

	if got, want := string(body), "<div>detail</div>"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	if got, want := seenPath.Load().(string), "/_forge/dashboard/pages/users/detail"; got != want {
		t.Fatalf("upstream path = %q, want %q", got, want)
	}

	if got, want := seenQuery.Load().(string), "id=ausr_1&extra=2"; got != want {
		t.Fatalf("upstream query = %q, want %q", got, want)
	}
}

func TestFragmentProxy_FetchPage_CachePerQuery(t *testing.T) {
	var hits int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)

		_, _ = w.Write([]byte("hit:" + r.URL.RawQuery))
	}))
	defer server.Close()

	reg := newTestRegistryWithRemote(t, "authsome", server.URL)
	p := newProxyForTest(t, reg)

	// Same route+query twice → 1 upstream hit (second from cache).
	if _, err := p.FetchPage(context.Background(), "authsome", "/users/detail", "id=A"); err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	if _, err := p.FetchPage(context.Background(), "authsome", "/users/detail", "id=A"); err != nil {
		t.Fatalf("second fetch: %v", err)
	}

	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("upstream hits after duplicate fetch = %d, want 1 (cache miss expected)", got)
	}

	// Different query → second cache key, so another upstream hit.
	if _, err := p.FetchPage(context.Background(), "authsome", "/users/detail", "id=B"); err != nil {
		t.Fatalf("third fetch: %v", err)
	}

	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Fatalf("upstream hits after distinct query = %d, want 2 (id=A and id=B should be cached separately)", got)
	}
}

func TestFragmentProxy_FetchPage_StaleOnFailure(t *testing.T) {
	var (
		hits int64
		fail atomic.Bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)

		if fail.Load() {
			http.Error(w, "boom", http.StatusBadGateway)

			return
		}

		_, _ = w.Write([]byte("fresh"))
	}))
	defer server.Close()

	reg := newTestRegistryWithRemote(t, "authsome", server.URL)
	p := newProxyForTest(t, reg)

	if _, err := p.FetchPage(context.Background(), "authsome", "/users", ""); err != nil {
		t.Fatalf("warm-up fetch: %v", err)
	}

	// Wait until the cache entry is stale (> TTL) but the entry is still present
	// for stale fall-through. The cache implementation keeps stale entries
	// available via GetStale until they age out completely; this delay is well
	// inside that window.
	time.Sleep(250 * time.Millisecond)

	// Force the upstream to fail — proxy must serve stale from cache.
	fail.Store(true)

	body, err := p.FetchPage(context.Background(), "authsome", "/users", "")
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}

	if got := string(body); got != "fresh" {
		t.Fatalf("stale body = %q, want %q", got, "fresh")
	}
}

func TestFragmentProxy_FetchPage_UnknownContributor(t *testing.T) {
	reg := contributor.NewContributorRegistry("/dashboard")
	p := newProxyForTest(t, reg)

	if _, err := p.FetchPage(context.Background(), "nope", "/", ""); err == nil {
		t.Fatalf("expected error for unknown contributor, got nil")
	}
}

func TestRouteWithQuery_PrependsQuestionMark(t *testing.T) {
	cases := []struct {
		route, query, want string
	}{
		{"/users", "", "/users"},
		{"/users", "id=1", "/users?id=1"},
		{"/users/detail", "id=1&x=2", "/users/detail?id=1&x=2"},
	}

	for _, tc := range cases {
		got := routeWithQuery(tc.route, tc.query)
		if got != tc.want {
			t.Errorf("routeWithQuery(%q, %q) = %q, want %q", tc.route, tc.query, got, tc.want)
		}
	}
}
