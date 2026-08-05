package dashboard

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	contractshell "github.com/xraph/forge/extensions/dashboard/contract/shell"
)

// The React shell is served from whatever BasePath a deployment configures, but
// its asset URLs come from a bundle built once, ahead of time. That build used
// to bake an absolute "/dashboard/ui/static/" into both index.html and Vite's
// chunk-preload resolver, so any deployment not mounted at the default
// /dashboard served working HTML that referenced scripts and stylesheets which
// did not exist -- every asset 404'd and the dashboard rendered blank.
//
// This walks the actual served HTML and fetches every asset it references
// through the real static handler, which is the check that would have caught it.
func TestShellAssetsRespectBasePath(t *testing.T) {
	shellFS, err := contractshell.FS()
	if err != nil {
		t.Fatalf("shell FS: %v", err)
	}

	// The default and a nested custom base. The nested one matters on its own:
	// a single-segment base could pass by accident against a bundle that had
	// hardcoded one segment.
	for _, base := range []string{"/dashboard", "/_forge/dashboard"} {
		t.Run(base, func(t *testing.T) {
			e := &Extension{config: Config{BasePath: base}}

			rec := httptest.NewRecorder()
			e.makeShellSPAHandler(shellFS)(rec, httptest.NewRequest(http.MethodGet, base+"/ui", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s/ui -> %d", base, rec.Code)
			}

			refs := regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(rec.Body.String(), -1)
			if len(refs) == 0 {
				t.Fatal("served index.html references no assets; the bundle or this regex has changed")
			}

			staticPrefix := base + "/ui/static"
			static := e.makeShellStaticHandler(shellFS, staticPrefix)

			for _, m := range refs {
				u := m[1]

				// Document-relative URLs resolve against the request path, so
				// they break on deep links like {base}/ui/metrics even when the
				// entry page happens to work.
				if !strings.HasPrefix(u, "/") {
					t.Errorf("asset URL %q is not absolute; it would break on deep links", u)

					continue
				}

				if !strings.HasPrefix(u, staticPrefix) {
					t.Errorf("asset URL %q does not sit under the configured base %q", u, staticPrefix)

					continue
				}

				w := httptest.NewRecorder()
				static(w, httptest.NewRequest(http.MethodGet, u, nil))

				if w.Code != http.StatusOK {
					t.Errorf("GET %s -> %d, want 200", u, w.Code)
				}
			}
		})
	}
}

// TestShellBundleIsRelocatable guards the other half. index.html is rewritten at
// serve time, but the lazily imported chunks are not -- they resolve against
// their own URL at runtime, which only works if the build left no absolute base
// behind. A future `base:` change in vite.config.ts would silently reintroduce
// the 404s for code-split routes only, which the test above cannot see.
func TestShellBundleIsRelocatable(t *testing.T) {
	shellFS, err := contractshell.FS()
	if err != nil {
		t.Fatalf("shell FS: %v", err)
	}

	const bakedIn = "/dashboard/ui/static/"

	scanned := 0

	err = fs.WalkDir(shellFS, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		switch strings.ToLower(filepath.Ext(path)) {
		case ".js", ".css", ".html":
		default:
			return nil
		}

		content, readErr := fs.ReadFile(shellFS, path)
		if readErr != nil {
			return readErr
		}

		scanned++

		if bytes.Contains(content, []byte(bakedIn)) {
			t.Errorf("%s hardcodes %q; the bundle is no longer relocatable and "+
				"non-default BasePath deployments will 404 on code-split chunks", path, bakedIn)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking shell assets: %v", err)
	}

	if scanned == 0 {
		t.Fatal("no shell assets scanned; the embedded dist looks empty")
	}
}
