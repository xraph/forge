package dashboard

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/shellassets"
)

// shellBootstrap is the object the SPA handler injects into index.html as
// window.__FORGE_DASHBOARD__. The shell bundle cannot know at build time where
// it is mounted: a deployment may call WithBasePath("/_forge/dashboard") or sit
// behind a reverse proxy, and the same bytes have to work either way. So the
// mount-dependent values are handed to it at request time.
//
// It is marshalled through encoding/json rather than concatenated, so a base
// path containing a quote or an angle bracket is escaped instead of breaking
// out of the script tag.
//
// The client reads contractBase for its endpoint and basePath for its router
// basename. Two more fields once lived here, loginContributor and loginOp,
// hardcoded to authsome's values with a note that deployments could override
// them later. Later never came, and nothing consumed them by the time the old
// shell was deleted. They are deliberately absent: whether the bootstrap is
// the right place to name a login contributor is a question for whoever builds
// the login UI, not something to declare in advance and leave unread.
type shellBootstrap struct {
	BasePath     string `json:"basePath"`
	ContractBase string `json:"contractBase"`
	ShellBase    string `json:"shellBase"`
	AuthEnabled  bool   `json:"authEnabled"`
	LoginPath    string `json:"loginPath"`
}

// shellEnabled reports whether the shell should be mounted at {BasePath}/ui.
//
// The zero value of ShellSource is "", not ShellEmbedded, and a Config built
// as a struct literal never passes through DefaultConfig(). Reading "" as
// ShellEmbedded here is what keeps those deployments from silently losing
// their dashboard.
func (c Config) shellEnabled() bool {
	return c.ShellSource == ShellEmbedded || c.ShellSource == ""
}

// shellBootstrapFor builds the bootstrap object for a config.
func shellBootstrapFor(cfg Config) shellBootstrap {
	return shellBootstrap{
		BasePath:     cfg.BasePath,
		ContractBase: cfg.BasePath + "/api/dashboard/v1",
		ShellBase:    cfg.BasePath + "/ui",
		AuthEnabled:  cfg.EnableAuth,
		LoginPath:    cfg.BasePath + cfg.LoginPath,
	}
}

// newShellSPAHandler serves index.html for every path under {base}/ui. The
// shell's own client-side router takes it from there, so a deep link and the
// entry page get the same bytes.
//
// Two rewrites happen on those bytes before they go out.
//
// The first injects the window.__FORGE_DASHBOARD__ bootstrap just before
// </head>, or prepends it to the document when there is no </head> at all,
// which Vite output always has but a hand-written placeholder might not.
// Either way it runs ahead of any module script.
//
// The second rewrites Vite's document-relative asset URLs. The shell's build
// sets base: "./" on purpose: an absolute base is baked into both the asset
// URLs in index.html and the preload resolver for lazy chunks, and both are
// then correct only at the default mount. Relative base fixes the chunks,
// which resolve against their own importer's URL. It does not fix index.html,
// because the same HTML answers every path under the shell prefix, so
// "./assets/" resolves against {base}/ui on the entry page and against
// {base}/ui/metrics on a deep link. Rewriting it to one absolute URL is the
// fix, and it has to happen here on the bytes: the browser resolves src and
// href while parsing the document, long before any script of ours could run,
// so by the time the bootstrap executes the wrong requests are already out.
func newShellSPAHandler(shellFS fs.FS, cfg Config) http.HandlerFunc {
	cfgJSON, err := json.Marshal(shellBootstrapFor(cfg))
	if err != nil {
		// Cannot happen: every field is a string or a bool. Fail loudly
		// rather than serving a page with no bootstrap at all.
		panic("dashboard: marshalling the shell bootstrap: " + err.Error())
	}

	bootstrap := []byte("<script>window.__FORGE_DASHBOARD__=" + string(cfgJSON) + ";</script>")
	assetsFrom := []byte(`"./assets/`)
	assetsTo := []byte(`"` + cfg.BasePath + `/ui/static/assets/`)

	return func(w http.ResponseWriter, r *http.Request) {
		raw, readErr := fs.ReadFile(shellFS, "index.html")
		if readErr != nil {
			http.Error(w,
				"dashboard shell index.html is missing from the embedded assets; see extensions/dashboard/shellassets/dist/README.md",
				http.StatusInternalServerError)

			return
		}

		out := bytes.ReplaceAll(raw, assetsFrom, assetsTo)

		if idx := bytes.Index(out, []byte("</head>")); idx >= 0 {
			out = append(out[:idx:idx], append(bootstrap, out[idx:]...)...)
		} else {
			out = append(append([]byte(nil), bootstrap...), out...)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(out)
	}
}

// newShellStaticHandler serves the shell's files at {base}/ui/static/*, which
// is where the SPA handler's rewrite points.
//
// Anything under /assets/ is cached for a year and marked immutable, because
// those filenames are content-hashed and a new build produces new names.
// Everything else gets no-cache so a deploy lands immediately.
//
// stripPrefix is the URL prefix the handler is mounted at; paths beneath it
// resolve against the embedded FS. The request and its URL are cloned before
// the path is trimmed, so nothing downstream sees a mutated original.
func newShellStaticHandler(shellFS fs.FS, stripPrefix string) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(shellFS))

	return func(w http.ResponseWriter, r *http.Request) {
		trimmed := strings.TrimPrefix(r.URL.Path, stripPrefix)
		if trimmed == "" {
			trimmed = "/"
		}

		if strings.Contains(trimmed, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		r2 := r.Clone(r.Context())
		r2.URL = cloneURLWithPath(r.URL, trimmed)

		fileServer.ServeHTTP(w, r2)
	}
}

// cloneURLWithPath returns a copy of u with Path replaced, so the static
// handler never mutates the original request's URL.
func cloneURLWithPath(u *url.URL, path string) *url.URL {
	if u == nil {
		return &url.URL{Path: path}
	}

	clone := *u
	clone.Path = path
	clone.RawPath = ""

	return &clone
}

// mountShellRoutes registers the shell's three routes when the config asks for
// an embedded shell, and nothing at all when it asks for an external one.
//
// The concrete "static" segment is registered first, ahead of the SPA
// catch-all, so it is unambiguous which one owns an asset path. Every router
// backend forge ships today resolves this by specificity rather than by
// registration order, so all three orderings measure the same: forgemux,
// bunrouter and chi all route {base}/ui/static/assets/x.js to the static
// handler whichever way round they are registered. Keep the order anyway. A
// backend that resolved first-registered-wins would answer every asset request
// with index.html, the browser would try to parse the document as JavaScript,
// and nothing in the logs would point at route registration.
//
// TestShellStaticRoutesWinOverSPA asserts the outcome (an asset request gets
// the file, not the SPA document) rather than the ordering, so it stays honest
// if the backend ever changes.
func mountShellRoutes(router forge.Router, base string, shellFS fs.FS, cfg Config, must func(error)) {
	if !cfg.shellEnabled() {
		return
	}

	staticPrefix := base + "/ui/static"

	must(router.GET(staticPrefix+"/*filepath", newShellStaticHandler(shellFS, staticPrefix)))
	must(router.GET(base+"/ui", newShellSPAHandler(shellFS, cfg)))
	must(router.GET(base+"/ui/*filepath", newShellSPAHandler(shellFS, cfg)))
}

// mountShell resolves the embedded shell assets and mounts them, warning when
// what is embedded is the committed placeholder rather than a real build.
//
// The placeholder renders as a readable page explaining itself, but a warning
// at startup is what stops someone spending an afternoon on a dashboard that
// was never bundled in the first place.
func (e *Extension) mountShell(router forge.Router, base string, must func(error)) {
	shellFS, err := shellassets.FS()
	if err != nil {
		e.Logger().Warn("dashboard shell assets unavailable; nothing will serve at the shell path",
			forge.F("shell_path", base+"/ui"),
			forge.F("error", err.Error()))

		return
	}

	if e.config.shellEnabled() && shellassets.IsPlaceholder() {
		e.Logger().Warn("dashboard shell is the committed placeholder, not a real build; the UI will render an explanatory page instead of the dashboard",
			forge.F("shell_path", base+"/ui"),
			forge.F("fix", "see extensions/dashboard/shellassets/dist/README.md"))
	}

	mountShellRoutes(router, base, shellFS, e.config, must)
}
