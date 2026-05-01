package dashpages

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/xraph/forge"
	"github.com/xraph/forgeui/router"

	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/forge/extensions/dashboard/proxy"
)

const timeSecond = time.Second

// stubLocalContributor records the params it was rendered with so tests can
// assert that PageBase / PathParams / QueryParams flow through correctly.
type stubLocalContributor struct {
	manifest *contributor.Manifest

	mu             sync.Mutex
	renderedRoute  string
	renderedParams contributor.Params
	renderedCtx    context.Context //nolint:containedctx // intentional — tests inspect the context that was passed to RenderPage
}

func (s *stubLocalContributor) Manifest() *contributor.Manifest { return s.manifest }

func (s *stubLocalContributor) RenderPage(ctx context.Context, route string, params contributor.Params) (templ.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.renderedRoute = route
	s.renderedParams = params
	s.renderedCtx = ctx

	return templ.Raw("<div data-route=\"" + route + "\"></div>"), nil
}

func (s *stubLocalContributor) RenderWidget(_ context.Context, _ string) (templ.Component, error) {
	return templ.Raw(""), nil
}

func (s *stubLocalContributor) RenderSettings(_ context.Context, _ string) (templ.Component, error) {
	return templ.Raw(""), nil
}

// pageContextFor builds a router.PageContext that matches what forgeui
// constructs at request time. The path params (`name`, `filepath`) must be
// supplied explicitly because we are not using the router's pattern matcher.
func pageContextFor(t *testing.T, method, rawURL string, params router.Params) (*router.PageContext, *httptest.ResponseRecorder) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url %q: %v", rawURL, err)
	}

	req := httptest.NewRequest(method, parsed.String(), nil)
	req.URL = parsed

	rec := httptest.NewRecorder()

	return &router.PageContext{
		ResponseWriter: rec,
		Request:        req,
		Params:         params,
	}, rec
}

func newTestPagesManager(t *testing.T, registry *contributor.ContributorRegistry, fragmentProxy *proxy.FragmentProxy) *PagesManager {
	t.Helper()

	return &PagesManager{
		basePath:      "/dashboard",
		registry:      registry,
		fragmentProxy: fragmentProxy,
		config:        PagesConfig{BasePath: "/dashboard"},
	}
}

func TestRemotePage_ForwardsRawQueryToProxy(t *testing.T) {
	var seenQuery, seenPath string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenQuery = r.URL.RawQuery
		_, _ = w.Write([]byte("<div>detail</div>"))
	}))
	defer upstream.Close()

	registry := contributor.NewContributorRegistry("/dashboard")

	rc := contributor.NewRemoteContributor(upstream.URL, &contributor.Manifest{Name: "authsome", Layout: "extension"})
	if err := registry.RegisterRemote(rc); err != nil {
		t.Fatalf("register: %v", err)
	}

	fp := proxy.NewFragmentProxy(registry, 32, 0, 2*timeSecond, forge.NewNoopLogger())

	pm := newTestPagesManager(t, registry, fp)

	ctx, rec := pageContextFor(t, http.MethodGet,
		"/dashboard/remote/authsome/pages/environments/detail?id=aenv_1",
		router.Params{"name": "authsome", "filepath": "environments/detail"})

	comp, err := pm.RemotePage(ctx)
	if err != nil {
		t.Fatalf("RemotePage: %v", err)
	}

	body := renderToString(t, comp)
	if !strings.Contains(body, "<div>detail</div>") {
		t.Fatalf("body did not contain proxied content: %q", body)
	}

	if got, want := seenPath, "/_forge/dashboard/pages/environments/detail"; got != want {
		t.Errorf("upstream path = %q, want %q", got, want)
	}

	if got, want := seenQuery, "id=aenv_1"; got != want {
		t.Errorf("upstream query = %q, want %q (query string was dropped along the way)", got, want)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("response code = %d, want 200", rec.Code)
	}
}

func TestRemotePage_RemoteUnreachable_RendersErrorComponent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	registry := contributor.NewContributorRegistry("/dashboard")

	rc := contributor.NewRemoteContributor(upstream.URL, &contributor.Manifest{Name: "authsome", Layout: "extension"})
	if err := registry.RegisterRemote(rc); err != nil {
		t.Fatalf("register: %v", err)
	}

	fp := proxy.NewFragmentProxy(registry, 32, 0, 2*timeSecond, forge.NewNoopLogger())
	pm := newTestPagesManager(t, registry, fp)

	ctx, _ := pageContextFor(t, http.MethodGet,
		"/dashboard/remote/authsome/pages/users",
		router.Params{"name": "authsome", "filepath": "users"})

	comp, err := pm.RemotePage(ctx)
	if err != nil {
		t.Fatalf("RemotePage: %v", err)
	}

	body := renderToString(t, comp)
	if !strings.Contains(strings.ToLower(body), "remote unavailable") {
		t.Fatalf("expected error component to mention 'Remote Unavailable', got: %q", body)
	}
}

func TestContributorPage_FallbackToRemote_PlainNav(t *testing.T) {
	registry := contributor.NewContributorRegistry("/dashboard")

	rc := contributor.NewRemoteContributor("http://nowhere.invalid", &contributor.Manifest{Name: "authsome", Layout: "extension"})
	if err := registry.RegisterRemote(rc); err != nil {
		t.Fatalf("register: %v", err)
	}

	pm := newTestPagesManager(t, registry, nil)

	ctx, rec := pageContextFor(t, http.MethodGet,
		"/dashboard/ext/authsome/pages/environments/detail?id=aenv_1",
		router.Params{"name": "authsome", "filepath": "environments/detail"})

	if _, err := pm.ContributorPage(ctx); err != nil {
		t.Fatalf("ContributorPage: %v", err)
	}

	got := rec.Header().Get("Location")
	want := "/dashboard/remote/authsome/pages/environments/detail?id=aenv_1"

	if got != want {
		t.Fatalf("Location = %q, want %q (stale /ext/ should redirect to /remote/)", got, want)
	}

	if rec.Code != http.StatusSeeOther {
		t.Errorf("code = %d, want 303", rec.Code)
	}
}

func TestContributorPage_FallbackToRemote_HTMX(t *testing.T) {
	registry := contributor.NewContributorRegistry("/dashboard")

	rc := contributor.NewRemoteContributor("http://nowhere.invalid", &contributor.Manifest{Name: "authsome", Layout: "extension"})
	if err := registry.RegisterRemote(rc); err != nil {
		t.Fatalf("register: %v", err)
	}

	pm := newTestPagesManager(t, registry, nil)

	ctx, rec := pageContextFor(t, http.MethodGet,
		"/dashboard/ext/authsome/pages/users?q=al",
		router.Params{"name": "authsome", "filepath": "users"})
	ctx.Request.Header.Set("Hx-Request", "true")

	if _, err := pm.ContributorPage(ctx); err != nil {
		t.Fatalf("ContributorPage: %v", err)
	}

	if got, want := rec.Header().Get("Hx-Redirect"),
		"/dashboard/remote/authsome/pages/users?q=al"; got != want {
		t.Fatalf("Hx-Redirect = %q, want %q", got, want)
	}
}

func TestRemotePage_FallbackToLocal(t *testing.T) {
	registry := contributor.NewContributorRegistry("/dashboard")

	stub := &stubLocalContributor{manifest: &contributor.Manifest{Name: "authsome", Layout: "extension"}}
	if err := registry.RegisterLocal(stub); err != nil {
		t.Fatalf("register: %v", err)
	}

	pm := newTestPagesManager(t, registry, nil)

	ctx, rec := pageContextFor(t, http.MethodGet,
		"/dashboard/remote/authsome/pages/environments/detail?id=aenv_1",
		router.Params{"name": "authsome", "filepath": "environments/detail"})

	if _, err := pm.RemotePage(ctx); err != nil {
		t.Fatalf("RemotePage: %v", err)
	}

	if got, want := rec.Header().Get("Location"),
		"/dashboard/ext/authsome/pages/environments/detail?id=aenv_1"; got != want {
		t.Fatalf("Location = %q, want %q (stale /remote/ → /ext/)", got, want)
	}
}

func TestContributorPage_InjectsPageBaseAndRunsPrepareContext(t *testing.T) {
	registry := contributor.NewContributorRegistry("/dashboard")

	stub := &stubLocalContributor{manifest: &contributor.Manifest{Name: "authsome", Layout: "extension"}}
	if err := registry.RegisterLocal(stub); err != nil {
		t.Fatalf("register: %v", err)
	}

	pm := newTestPagesManager(t, registry, nil)

	ctx, _ := pageContextFor(t, http.MethodGet,
		"/dashboard/ext/authsome/pages/environments/detail?id=aenv_1",
		router.Params{"name": "authsome", "filepath": "environments/detail"})

	if _, err := pm.ContributorPage(ctx); err != nil {
		t.Fatalf("ContributorPage: %v", err)
	}

	stub.mu.Lock()
	gotPageBase := contributor.PageBaseFromContext(stub.renderedCtx)
	gotParamsPageBase := stub.renderedParams.PageBase
	gotQuery := stub.renderedParams.QueryParams["id"]
	stub.mu.Unlock()

	if want := "/dashboard/ext/authsome/pages"; gotPageBase != want {
		t.Errorf("PageBaseFromContext = %q, want %q", gotPageBase, want)
	}

	if want := "/dashboard/ext/authsome/pages"; gotParamsPageBase != want {
		t.Errorf("Params.PageBase = %q, want %q", gotParamsPageBase, want)
	}

	if gotQuery != "aenv_1" {
		t.Errorf("Params.QueryParams[\"id\"] = %q, want aenv_1 — query was dropped before reaching contributor", gotQuery)
	}
}

func TestRemoteExtensionHandler_ForwardsQueryAndRendersFragment(t *testing.T) {
	var seenQuery string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		_, _ = w.Write([]byte("<section data-fragment=\"y\"></section>"))
	}))
	defer upstream.Close()

	registry := contributor.NewContributorRegistry("/dashboard")

	rc := contributor.NewRemoteContributor(upstream.URL, &contributor.Manifest{Name: "authsome", Layout: "extension"})
	if err := registry.RegisterRemote(rc); err != nil {
		t.Fatalf("register: %v", err)
	}

	fp := proxy.NewFragmentProxy(registry, 32, 0, 2*timeSecond, forge.NewNoopLogger())
	pm := newTestPagesManager(t, registry, fp)

	handler := pm.remoteExtensionHandler("authsome")

	ctx, _ := pageContextFor(t, http.MethodGet,
		"/dashboard/remote/authsome/pages/users/detail?id=ausr_1",
		router.Params{"filepath": "users/detail"})

	comp, err := handler(ctx)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	body := renderToString(t, comp)
	if !strings.Contains(body, "data-fragment=\"y\"") {
		t.Fatalf("expected proxied fragment, got %q", body)
	}

	if got, want := seenQuery, "id=ausr_1"; got != want {
		t.Errorf("upstream query = %q, want %q", got, want)
	}
}

// renderToString renders a templ component to a string for assertions.
func renderToString(t *testing.T, comp templ.Component) string {
	t.Helper()

	if comp == nil {
		return ""
	}

	var buf bytes.Buffer
	if err := comp.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	return buf.String()
}
