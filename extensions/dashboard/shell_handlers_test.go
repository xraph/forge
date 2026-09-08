package dashboard

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/xraph/forge"
)

// fakeShellFS stands in for a real Vite build. It carries the two things the
// handlers actually care about, a </head> to inject before and a
// document-relative asset reference to rewrite, so none of these tests depend
// on the committed placeholder's exact bytes, which are free to change.
func fakeShellFS() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(
			`<!doctype html><html><head>` +
				`<script type="module" crossorigin src="./assets/index-abc123.js"></script>` +
				`<link rel="stylesheet" href="./assets/index-def456.css">` +
				`</head><body><div id="root"></div></body></html>`,
		)},
		"assets/index-abc123.js":  &fstest.MapFile{Data: []byte("console.log('shell')\n")},
		"assets/index-def456.css": &fstest.MapFile{Data: []byte(":root{}\n")},
		"favicon.ico":             &fstest.MapFile{Data: []byte("\x00\x00\x01\x00")},
		// Not something the shell build produces. It exists only so the cache
		// rule can be shown to test a prefix rather than a substring.
		"vendor/assets/x.js": &fstest.MapFile{Data: []byte("// vendored\n")},
	}
}

// shellTestConfig returns a config mounted somewhere other than the default,
// which is the only mount that would catch a hardcoded "/dashboard".
func shellTestConfig() Config {
	cfg := DefaultConfig()
	cfg.BasePath = "/_forge/dashboard"

	return cfg
}

// newShellTestRouter registers the shell routes on a real forge router, so the
// tests exercise the actual trie rather than calling handlers directly. That is
// what makes the route-ordering assertions meaningful: a handler called by hand
// can never tell you which route the router would have picked.
func newShellTestRouter(t *testing.T, cfg Config) forge.Router {
	t.Helper()

	r := forge.NewRouter()

	mountShellRoutes(r, cfg.BasePath, fakeShellFS(), cfg, func(err error) {
		t.Helper()

		if err != nil {
			t.Fatalf("registering shell routes: %v", err)
		}
	})

	return r
}

func shellGet(t *testing.T, r forge.Router, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	return rec
}

// extractBootstrap pulls the JSON out of the injected
// <script>window.__FORGE_DASHBOARD__={...};</script> and unmarshals it, so the
// assertions are on parsed values. Asserting that the body merely contains
// "window.__FORGE_DASHBOARD__" would pass on a bootstrap carrying entirely the
// wrong base path.
func extractBootstrap(t *testing.T, body string) map[string]any {
	t.Helper()

	const marker = "window.__FORGE_DASHBOARD__="

	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("no %q in the served document:\n%s", marker, body)
	}

	rest := body[start+len(marker):]

	end := strings.Index(rest, ";</script>")
	if end < 0 {
		t.Fatalf("bootstrap script is not terminated with ;</script>:\n%s", rest)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(rest[:end]), &got); err != nil {
		t.Fatalf("bootstrap is not valid JSON: %v\nraw: %s", err, rest[:end])
	}

	return got
}

func TestShellSPABootstrap(t *testing.T) {
	cfg := shellTestConfig()
	cfg.EnableAuth = true

	r := newShellTestRouter(t, cfg)

	// Both the entry path and a deep link serve the same document, so both
	// have to carry a correct bootstrap.
	for _, path := range []string{"/_forge/dashboard/ui", "/_forge/dashboard/ui/metrics/http"} {
		t.Run(path, func(t *testing.T) {
			rec := shellGet(t, r, path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, rec.Code)
			}

			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}

			got := extractBootstrap(t, rec.Body.String())

			want := map[string]any{
				"basePath":     "/_forge/dashboard",
				"contractBase": "/_forge/dashboard/api/dashboard/v1",
				"shellBase":    "/_forge/dashboard/ui",
				"authEnabled":  true,
				"loginPath":    "/_forge/dashboard/login",
			}

			for key, wantVal := range want {
				if got[key] != wantVal {
					t.Errorf("bootstrap[%q] = %#v, want %#v", key, got[key], wantVal)
				}
			}

			// W5 owns the login UI and decides whether these belong in the
			// bootstrap at all. Until then they must not reappear as
			// hardcoded values nothing reads.
			for _, key := range []string{"loginContributor", "loginOp"} {
				if _, present := got[key]; present {
					t.Errorf("bootstrap carries %q = %#v; it was deliberately omitted", key, got[key])
				}
			}
		})
	}
}

// TestShellSPABootstrapEscapesBasePath pins that the bootstrap goes through
// encoding/json rather than string concatenation. A base path carrying a quote
// or a closing script tag must not break out of the inline script, and must
// still round-trip to the exact string the deployment configured.
func TestShellSPABootstrapEscapesBasePath(t *testing.T) {
	cfg := shellTestConfig()
	cfg.BasePath = `/a"</script><script>alert(1)</script>b`

	rec := httptest.NewRecorder()
	newShellSPAHandler(fakeShellFS(), cfg)(rec, httptest.NewRequest(http.MethodGet, "/ui", nil))

	body := rec.Body.String()

	// Scope the assertion to the injected script. The asset-URL rewrite also
	// interpolates the base path, into an HTML attribute rather than into
	// JavaScript, and that is not what this test is about.
	const marker = "<script>window.__FORGE_DASHBOARD__="

	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("no bootstrap in the served document:\n%s", body)
	}

	end := strings.Index(body[start:], "</script>")
	if end < 0 {
		t.Fatalf("bootstrap script is never closed:\n%s", body[start:])
	}

	script := body[start : start+end]
	for _, forbidden := range []string{"</script", "<script>alert"} {
		if strings.Contains(script[len(marker):], forbidden) {
			t.Fatalf("base path broke out of the inline script (%q appears inside it):\n%s", forbidden, script)
		}
	}

	got := extractBootstrap(t, body)
	if got["basePath"] != cfg.BasePath {
		t.Errorf("bootstrap[basePath] = %#v, want %#v", got["basePath"], cfg.BasePath)
	}
}

// TestShellSPARewritesAssetURLs is the guard on the one rewrite that cannot be
// moved into the bootstrap script. The browser resolves src and href while
// parsing the document, so by the time any script of ours runs the requests
// for "./assets/*" are already out and already 404ing on a non-default mount.
func TestShellSPARewritesAssetURLs(t *testing.T) {
	cfg := shellTestConfig()
	r := newShellTestRouter(t, cfg)

	rec := shellGet(t, r, "/_forge/dashboard/ui")
	body := rec.Body.String()

	const want = `"/_forge/dashboard/ui/static/assets/`
	if !strings.Contains(body, want) {
		t.Errorf("document does not contain rewritten asset prefix %q:\n%s", want, body)
	}

	if strings.Contains(body, `"./assets/`) {
		t.Errorf(`document still contains document-relative "./assets/ references:`+"\n%s", body)
	}
}

// TestShellSPAPrependsWhenNoHead covers the fallback the note calls out: Vite
// output always has a </head>, a hand-written or minified document might not,
// and the bootstrap still has to run before any module script.
func TestShellSPAPrependsWhenNoHead(t *testing.T) {
	noHead := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<div id="root"></div>`)},
	}

	rec := httptest.NewRecorder()
	newShellSPAHandler(noHead, shellTestConfig())(rec, httptest.NewRequest(http.MethodGet, "/ui", nil))

	body := rec.Body.String()
	if !strings.HasPrefix(body, "<script>window.__FORGE_DASHBOARD__=") {
		t.Fatalf("bootstrap was not prepended to a document with no </head>:\n%s", body)
	}
}

// TestShellStaticRoutesWinOverSPA pins that an asset request is answered with
// the asset and not with the SPA document, and that the two get different cache
// headers.
//
// It asserts the outcome, not the registration order. Every router backend
// forge ships resolves {base}/ui/static/* to the concrete segment by
// specificity, so swapping the registration order in mountShellRoutes does not
// change what this measures. A backend that resolved first-registered-wins
// would break it, which is the point: the failure mode is that the browser gets
// index.html where it asked for JavaScript, and nothing in the logs says so.
func TestShellStaticRoutesWinOverSPA(t *testing.T) {
	cfg := shellTestConfig()
	r := newShellTestRouter(t, cfg)

	tests := []struct {
		name      string
		path      string
		wantCache string
		wantBody  string
	}{
		{
			name: "hashed asset is immutable",
			path: "/_forge/dashboard/ui/static/assets/index-abc123.js",
			// Content-hashed filenames never change meaning, so a year is safe.
			wantCache: "public, max-age=31536000, immutable",
			wantBody:  "console.log('shell')",
		},
		{
			name:      "hashed stylesheet is immutable",
			path:      "/_forge/dashboard/ui/static/assets/index-def456.css",
			wantCache: "public, max-age=31536000, immutable",
			wantBody:  ":root{}",
		},
		{
			name: "unhashed static file is not cached",
			path: "/_forge/dashboard/ui/static/favicon.ico",
			// Not content-hashed, so a deploy has to land immediately.
			wantCache: "no-cache",
			wantBody:  "",
		},
		{
			// strings.Contains(path, "/assets/") would call this immutable.
			// It is not a shell asset and carries no hash, so a year of
			// caching would be a promise nothing can keep.
			name:      "assets nested deeper is not treated as a shell asset",
			path:      "/_forge/dashboard/ui/static/vendor/assets/x.js",
			wantCache: "no-cache",
			wantBody:  "// vendored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := shellGet(t, r, tt.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, rec.Code)
			}

			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}

			body := rec.Body.String()

			// The ordering check. If the SPA catch-all won, this asset request
			// is answered with the shell document instead of the file.
			if strings.Contains(body, "window.__FORGE_DASHBOARD__") {
				t.Fatalf("asset request was answered with the SPA document; the SPA catch-all is winning over the static route:\n%s", body)
			}

			if tt.wantBody != "" && !strings.Contains(body, tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", body, tt.wantBody)
			}
		})
	}

	// The SPA document itself is never cached, so a new build lands on the
	// next navigation.
	rec := shellGet(t, r, "/_forge/dashboard/ui")
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("SPA document Cache-Control = %q, want %q", got, "no-cache")
	}
}

func TestShellSourceMounting(t *testing.T) {
	tests := []struct {
		name        string
		source      ShellSource
		wantMounted bool
	}{
		{
			name:        "explicit embedded mounts",
			source:      ShellEmbedded,
			wantMounted: true,
		},
		{
			// The zero value of a string type is "", not ShellEmbedded, and a
			// Config built as a struct literal never passes through
			// DefaultConfig(). Reading "" as embedded is what keeps those
			// deployments from silently losing their dashboard.
			name:        "zero value mounts",
			source:      "",
			wantMounted: true,
		},
		{
			name:        "external mounts nothing",
			source:      ShellExternal,
			wantMounted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A struct literal, deliberately: DefaultConfig() would paper over
			// exactly the zero value this is testing.
			cfg := Config{BasePath: "/_forge/dashboard", ShellSource: tt.source}
			r := newShellTestRouter(t, cfg)

			for _, path := range []string{
				"/_forge/dashboard/ui",
				"/_forge/dashboard/ui/metrics",
				"/_forge/dashboard/ui/static/assets/index-abc123.js",
			} {
				rec := shellGet(t, r, path)

				mounted := rec.Code == http.StatusOK
				if mounted != tt.wantMounted {
					t.Errorf("GET %s = %d; mounted=%v, want mounted=%v",
						path, rec.Code, mounted, tt.wantMounted)
				}
			}
		})
	}
}

// TestShellStaticMissingAssetIsNotCached pins the contract that an error
// response never carries the immutable directive, so a mistyped hashed asset
// cannot be cached by an intermediary as permanently missing.
//
// It is a contract test, not a regression test for cacheOnSuccess. The stdlib
// already strips Cache-Control on a file-server error -- through the
// unexported net/http.serveError in fs.go, not through http.Error, and only
// while GODEBUG=httpservecontentkeepheaders is unset -- so this would pass
// with the header set up front too, on a default toolchain. The point is that
// the guarantee holds whichever way the handler is written, and keeps holding
// if either of those two stdlib details changes.
func TestShellStaticMissingAssetIsNotCached(t *testing.T) {
	cfg := shellTestConfig()
	r := newShellTestRouter(t, cfg)

	for _, path := range []string{
		"/_forge/dashboard/ui/static/assets/index-typo999.js",
		"/_forge/dashboard/ui/static/nope.txt",
	} {
		t.Run(path, func(t *testing.T) {
			rec := shellGet(t, r, path)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s = %d, want 404", path, rec.Code)
			}

			if got := rec.Header().Get("Cache-Control"); strings.Contains(got, "immutable") {
				t.Errorf("404 carries Cache-Control %q; a missing asset must not be cached as permanent", got)
			}
		})
	}
}

// TestShellStaticNotModifiedKeepsCacheControl pins the 304 case in
// cacheOnSuccess. A revalidation response has to carry the same freshness
// directives the 200 would have, or a client that revalidates ends up worse off
// than one that never asked.
//
// The handler is driven directly here. http.ServeContent cannot produce a 304
// over an embed.FS, whose files have a zero modtime and so no Last-Modified to
// revalidate against, so a fake writer is what exercises the branch.
func TestShellStaticNotModifiedKeepsCacheControl(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &cacheOnSuccess{ResponseWriter: rec, value: "public, max-age=31536000, immutable"}

	w.WriteHeader(http.StatusNotModified)

	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("304 Cache-Control = %q, want the directive the 200 would have carried", got)
	}

	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusMovedPermanently} {
		rec := httptest.NewRecorder()
		w := &cacheOnSuccess{ResponseWriter: rec, value: "public, max-age=31536000, immutable"}

		w.WriteHeader(status)

		if got := rec.Header().Get("Cache-Control"); got != "" {
			t.Errorf("status %d carries Cache-Control %q, want none", status, got)
		}
	}
}

// TestShellSourceValidation pins that a typo is rejected at config time.
//
// The serving path treats everything that is not ShellEmbedded or "" as "do not
// mount", so `shell_source: embeded` in a YAML file would otherwise cost a
// deployment its dashboard with nothing in the logs to explain it.
func TestShellSourceValidation(t *testing.T) {
	tests := []struct {
		name    string
		source  ShellSource
		wantErr bool
	}{
		{name: "embedded", source: ShellEmbedded},
		{name: "external", source: ShellExternal},
		{name: "empty means embedded", source: ""},
		{name: "typo is rejected", source: "embeded", wantErr: true},
		{name: "unknown value is rejected", source: "cdn", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.ShellSource = tt.source

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}

			if tt.wantErr && !strings.Contains(err.Error(), "shell_source") {
				t.Errorf("error %q does not name the offending field", err)
			}
		})
	}
}

// TestShellStaticHandlerDoesNotMutateRequest pins the clone. The static handler
// trims the mount prefix off the path before handing the request to the file
// server; doing that in place would leave every middleware and logger
// downstream reporting a path the client never asked for.
func TestShellStaticHandlerDoesNotMutateRequest(t *testing.T) {
	const path = "/_forge/dashboard/ui/static/assets/index-abc123.js"

	req := httptest.NewRequest(http.MethodGet, path, nil)
	originalURL := req.URL

	newShellStaticHandler(fakeShellFS(), "/_forge/dashboard/ui/static")(httptest.NewRecorder(), req)

	if req.URL != originalURL {
		t.Errorf("handler replaced the request's URL pointer")
	}

	if req.URL.Path != path {
		t.Errorf("handler mutated the request URL path to %q, want %q", req.URL.Path, path)
	}
}
