package contributor

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/a-h/templ"
)

// EmbeddedContributor serves dashboard pages, widgets, and settings from an
// embedded fs.FS (typically embed.FS). It is framework-agnostic — the FS can
// contain output from Astro, Next.js, or any other static site generator.
//
// Pages are looked up as HTML files in the pages directory, widgets in the
// widgets directory, and settings in the settings directory. Full HTML
// documents are automatically converted to fragments via ExtractBodyFragment.
//
// Static assets (CSS, JS, images) are served via AssetsHandler().
type EmbeddedContributor struct {
	manifest    *Manifest
	fsys        fs.FS
	pagesDir    string
	widgetsDir  string
	settingsDir string
	assetsDir   string
}

// EmbeddedOption configures an EmbeddedContributor.
type EmbeddedOption func(*EmbeddedContributor)

// WithPagesDir sets the directory within the FS where page HTML files are stored.
// Default: "pages".
func WithPagesDir(dir string) EmbeddedOption {
	return func(ec *EmbeddedContributor) {
		ec.pagesDir = dir
	}
}

// WithWidgetsDir sets the directory within the FS where widget HTML files are stored.
// Default: "widgets".
func WithWidgetsDir(dir string) EmbeddedOption {
	return func(ec *EmbeddedContributor) {
		ec.widgetsDir = dir
	}
}

// WithSettingsDir sets the directory within the FS where settings HTML files are stored.
// Default: "settings".
func WithSettingsDir(dir string) EmbeddedOption {
	return func(ec *EmbeddedContributor) {
		ec.settingsDir = dir
	}
}

// WithAssetsDir sets the directory within the FS where static assets are stored.
// Default: "assets".
func WithAssetsDir(dir string) EmbeddedOption {
	return func(ec *EmbeddedContributor) {
		ec.assetsDir = dir
	}
}

// NewEmbeddedContributor creates an EmbeddedContributor that reads HTML
// fragments from the provided filesystem.
func NewEmbeddedContributor(manifest *Manifest, fsys fs.FS, opts ...EmbeddedOption) *EmbeddedContributor {
	ec := &EmbeddedContributor{
		manifest:    manifest,
		fsys:        fsys,
		pagesDir:    "pages",
		widgetsDir:  "widgets",
		settingsDir: "settings",
		assetsDir:   "assets",
	}
	for _, opt := range opts {
		opt(ec)
	}

	return ec
}

// Manifest returns the contributor's manifest.
func (ec *EmbeddedContributor) Manifest() *Manifest {
	return ec.manifest
}

// RenderPage renders a page fragment for the given route.
// Route "/" looks for pages/index.html, route "/sessions" looks for
// pages/sessions/index.html or pages/sessions.html.
func (ec *EmbeddedContributor) RenderPage(_ context.Context, route string, _ Params) (templ.Component, error) {
	clean := strings.TrimPrefix(filepath.Clean(route), "/")

	candidates := []string{
		filepath.Join(ec.pagesDir, clean, "index.html"),
	}
	if clean != "" {
		candidates = append(candidates, filepath.Join(ec.pagesDir, clean+".html"))
	} else {
		// Root route: also try pages/index.html directly
		candidates = append(candidates, filepath.Join(ec.pagesDir, "index.html"))
	}

	for _, path := range candidates {
		content, err := fs.ReadFile(ec.fsys, filepath.ToSlash(path))
		if err != nil {
			continue
		}

		fragment := ExtractBodyFragment(content)

		return templ.Raw(string(fragment)), nil
	}

	return nil, fmt.Errorf("%w: page %q not found in embedded FS", ErrPageNotFound, route)
}

// RenderWidget renders a widget fragment by ID.
// Widget "active-sessions" looks for widgets/active-sessions.html.
func (ec *EmbeddedContributor) RenderWidget(_ context.Context, widgetID string) (templ.Component, error) {
	path := filepath.Join(ec.widgetsDir, widgetID+".html")

	content, err := fs.ReadFile(ec.fsys, filepath.ToSlash(path))
	if err != nil {
		return nil, fmt.Errorf("%w: widget %q not found in embedded FS", ErrWidgetNotFound, widgetID)
	}

	fragment := ExtractBodyFragment(content)

	return templ.Raw(string(fragment)), nil
}

// RenderSettings renders a settings panel fragment by ID.
func (ec *EmbeddedContributor) RenderSettings(_ context.Context, settingID string) (templ.Component, error) {
	path := filepath.Join(ec.settingsDir, settingID+".html")

	content, err := fs.ReadFile(ec.fsys, filepath.ToSlash(path))
	if err != nil {
		return nil, fmt.Errorf("%w: setting %q not found in embedded FS", ErrSettingNotFound, settingID)
	}

	fragment := ExtractBodyFragment(content)

	return templ.Raw(string(fragment)), nil
}

// AssetsHandler returns an http.Handler that serves static assets (CSS, JS,
// images) from the embedded filesystem. Mount this at the appropriate path
// in the dashboard routes.
func (ec *EmbeddedContributor) AssetsHandler() http.Handler {
	sub, err := fs.Sub(ec.fsys, ec.assetsDir)
	if err != nil {
		// If assets dir doesn't exist, return a handler that always 404s.
		return http.NotFoundHandler()
	}

	return http.FileServer(http.FS(sub))
}

// FS returns the underlying filesystem for direct access if needed.
func (ec *EmbeddedContributor) FS() fs.FS {
	return ec.fsys
}
