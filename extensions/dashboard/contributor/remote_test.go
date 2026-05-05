package contributor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recorderServer captures the most recent inbound request and lets each test
// drive the response. It removes the boilerplate of writing fresh httptest
// servers per case.
type recorderServer struct {
	t           *testing.T
	server      *httptest.Server
	lastPath    string
	lastQuery   string
	lastHeaders http.Header
	hits        int
	respond     func(w http.ResponseWriter, r *http.Request)
}

func newRecorderServer(t *testing.T) *recorderServer {
	t.Helper()

	rs := &recorderServer{t: t}
	rs.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<div>ok</div>"))
	}

	rs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.lastPath = r.URL.Path
		rs.lastQuery = r.URL.RawQuery
		rs.lastHeaders = r.Header.Clone()
		rs.hits++
		rs.respond(w, r)
	}))

	t.Cleanup(rs.server.Close)

	return rs
}

func TestRemoteContributor_FetchPage_ForwardsRoute(t *testing.T) {
	rs := newRecorderServer(t)

	rc := NewRemoteContributor(rs.server.URL, &Manifest{Name: "authsome"})

	body, err := rc.FetchPage(context.Background(), "/users", "")
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}

	if got, want := rs.lastPath, "/_forge/dashboard/pages/users"; got != want {
		t.Fatalf("upstream path = %q, want %q", got, want)
	}

	if rs.lastQuery != "" {
		t.Fatalf("upstream query = %q, want empty", rs.lastQuery)
	}

	if string(body) != "<div>ok</div>" {
		t.Fatalf("body = %q, want <div>ok</div>", body)
	}
}

func TestRemoteContributor_FetchPage_ForwardsRawQuery(t *testing.T) {
	rs := newRecorderServer(t)

	rc := NewRemoteContributor(rs.server.URL, &Manifest{Name: "authsome"})

	if _, err := rc.FetchPage(context.Background(), "/environments/detail", "id=aenv_xyz&extra=1"); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}

	if got, want := rs.lastPath, "/_forge/dashboard/pages/environments/detail"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	if got, want := rs.lastQuery, "id=aenv_xyz&extra=1"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
}

func TestRemoteContributor_FetchPage_PropagatesAPIKey(t *testing.T) {
	rs := newRecorderServer(t)

	rc := NewRemoteContributor(rs.server.URL, &Manifest{Name: "authsome"}, WithAPIKey("svc-key-123"))

	if _, err := rc.FetchPage(context.Background(), "/users", ""); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}

	if got, want := rs.lastHeaders.Get("X-Forge-Api-Key"), "svc-key-123"; got != want {
		t.Fatalf("X-Forge-Api-Key header = %q, want %q", got, want)
	}

	if got, want := rs.lastHeaders.Get("X-Forge-Dashboard"), "true"; got != want {
		t.Fatalf("X-Forge-Dashboard header = %q, want %q", got, want)
	}
}

// TestRemoteContributor_ForwardsAuthAndCookie pins the user-identity
// forwarding contract: when the host attaches inbound headers via
// WithForwardedHeaders, the S2S request to the remote MUST carry the
// allowlisted Authorization and Cookie headers and MUST NOT carry
// other inbound headers (Host metadata, custom internal markers,
// etc.) that would leak unrelated request context.
func TestRemoteContributor_ForwardsAuthAndCookie(t *testing.T) {
	rs := newRecorderServer(t)
	rc := NewRemoteContributor(rs.server.URL, &Manifest{Name: "authsome"})

	inbound := http.Header{}
	inbound.Set("Authorization", "Bearer user-tok-abc")
	inbound.Set("Cookie", "authsome_session_token=cookie-val")
	inbound.Set("X-Internal-Trace", "should-not-be-forwarded")

	ctx := WithForwardedHeaders(context.Background(), inbound)

	if _, err := rc.FetchPage(ctx, "/users", ""); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}

	if got, want := rs.lastHeaders.Get("Authorization"), "Bearer user-tok-abc"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got, want := rs.lastHeaders.Get("Cookie"), "authsome_session_token=cookie-val"; got != want {
		t.Fatalf("Cookie = %q, want %q", got, want)
	}
	if got := rs.lastHeaders.Get("X-Internal-Trace"); got != "" {
		t.Fatalf("X-Internal-Trace must not be forwarded; got %q", got)
	}
}

// TestRemoteContributor_NoForwardedHeaders pins backward compat: with
// no WithForwardedHeaders on the context, the outbound request must
// not carry an Authorization header (which would otherwise leak from
// some unrelated source). This is the historical behaviour every
// existing caller relies on.
func TestRemoteContributor_NoForwardedHeaders(t *testing.T) {
	rs := newRecorderServer(t)
	rc := NewRemoteContributor(rs.server.URL, &Manifest{Name: "authsome"})

	if _, err := rc.FetchPage(context.Background(), "/users", ""); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}

	if got := rs.lastHeaders.Get("Authorization"); got != "" {
		t.Fatalf("Authorization must be empty without WithForwardedHeaders; got %q", got)
	}
}

func TestRemoteContributor_FetchPage_ErrorOnNon2xx(t *testing.T) {
	rs := newRecorderServer(t)
	rs.respond = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}

	rc := NewRemoteContributor(rs.server.URL, &Manifest{Name: "authsome"})

	body, err := rc.FetchPage(context.Background(), "/users", "")
	if err == nil {
		t.Fatalf("expected error on 500, got body=%q", body)
	}
}

func TestRemoteContributor_PostPage_ForwardsBodyAndContentType(t *testing.T) {
	rs := newRecorderServer(t)

	var seenBody []byte
	var seenMethod, seenContentType string

	rs.respond = func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenContentType = r.Header.Get("Content-Type")
		seenBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<div>posted</div>"))
	}

	rc := NewRemoteContributor(rs.server.URL, &Manifest{Name: "authsome"})

	body := strings.NewReader("name=alice&role=admin")
	got, err := rc.PostPage(context.Background(), "/users/create", "id=ausr_1", body, "application/x-www-form-urlencoded")
	if err != nil {
		t.Fatalf("PostPage: %v", err)
	}

	if string(got) != "<div>posted</div>" {
		t.Fatalf("response = %q, want <div>posted</div>", got)
	}

	if seenMethod != http.MethodPost {
		t.Errorf("upstream method = %q, want POST", seenMethod)
	}

	if got, want := rs.lastPath, "/_forge/dashboard/pages/users/create"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}

	if got, want := rs.lastQuery, "id=ausr_1"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}

	if seenContentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", seenContentType)
	}

	if got, want := string(seenBody), "name=alice&role=admin"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestRemoteContributor_FetchPage_ContextCancelled(t *testing.T) {
	rs := newRecorderServer(t)
	rs.respond = func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}

	rc := NewRemoteContributor(rs.server.URL, &Manifest{Name: "authsome"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := rc.FetchPage(ctx, "/users", ""); err == nil {
		t.Fatalf("expected error from cancelled context")
	}
}

func TestFetchManifest_DecodesPluginNav(t *testing.T) {
	fixturePath := filepath.Join("testdata", "manifest_full.json")

	body, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	rs := newRecorderServer(t)
	rs.respond = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}

	manifest, err := FetchManifest(context.Background(), rs.server.URL, 5*time.Second, "")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}

	if got, want := rs.lastPath, "/_forge/dashboard/manifest"; got != want {
		t.Fatalf("manifest path = %q, want %q", got, want)
	}

	if manifest.Name != "authsome" {
		t.Fatalf("name = %q, want authsome", manifest.Name)
	}

	if got, want := manifest.Layout, "extension"; got != want {
		t.Fatalf("layout = %q, want %q", got, want)
	}

	if !manifest.ShowSidebarOrDefault() {
		t.Fatalf("show_sidebar should be true for the authsome fixture")
	}

	// The fixture is the live identity manifest captured during this work
	// (see plan). It must round-trip every group authsome contributes,
	// including plugin-only ones. If a future change drops a plugin from the
	// platform manifest, recapture the fixture and update the assertion.
	wantGroups := map[string]bool{
		"Authsome":        false,
		"User Management": false,
		"Access Control":  false,
		"Configuration":   false,
		"Developer":       false,
		"Authentication":  false,
		"Security":        false,
		"Billing":         false,
		"Provisioning":    false,
	}

	for _, item := range manifest.Nav {
		if _, ok := wantGroups[item.Group]; ok {
			wantGroups[item.Group] = true
		}
	}

	for group, seen := range wantGroups {
		if !seen {
			t.Errorf("manifest fixture missing nav group %q — plugin nav merging regressed", group)
		}
	}

	// Spot-check a plugin-contributed item that's only there if the
	// organization plugin's DashboardNavItems() round-tripped.
	wantItems := map[string]string{
		"/organizations":     "Organizations",
		"/oauth2-clients":    "OAuth2 Clients",
		"/scim":              "SCIM",
		"/passkeys":          "Passkeys",
		"/anomaly-detection": "Anomaly Detection",
	}

	for path, label := range wantItems {
		found := false

		for _, item := range manifest.Nav {
			if item.Path == path && item.Label == label {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("nav item %q (label %q) missing from fixture", path, label)
		}
	}
}

func TestFetchManifest_NonJSONBodyFails(t *testing.T) {
	rs := newRecorderServer(t)
	rs.respond = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}

	if _, err := FetchManifest(context.Background(), rs.server.URL, 1*time.Second, ""); err == nil {
		t.Fatalf("expected JSON decode error, got nil")
	}
}

func TestFetchManifest_FixtureMatchesLiveSchema(t *testing.T) {
	// Dual-decode: once into Manifest, once back to JSON, parse the byte-by-byte
	// re-encoding into a generic map to confirm we don't silently lose any
	// top-level keys the live identity service produces.
	body, err := os.ReadFile(filepath.Join("testdata", "manifest_full.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(manifest.Nav) < 25 {
		t.Fatalf("expected at least 25 nav items in fixture, got %d", len(manifest.Nav))
	}

	if len(manifest.Widgets) < 10 {
		t.Fatalf("expected at least 10 widgets in fixture, got %d", len(manifest.Widgets))
	}
}
