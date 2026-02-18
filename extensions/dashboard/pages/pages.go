package dashpages

import (
	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/router"
	g "maragu.dev/gomponents"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/forge/extensions/dashboard/proxy"
	"github.com/xraph/forge/extensions/dashboard/ui"
	uipages "github.com/xraph/forge/extensions/dashboard/ui/pages"
)

// PagesConfig holds configuration for the PagesManager.
type PagesConfig struct {
	EnableSettings bool
	EnableSearch   bool
	BasePath       string
}

// PagesManager registers and serves dashboard pages using forgeui's routing system.
// Page handlers return gomponents nodes and delegate rendering to the contributor system.
// Layout wrapping is handled automatically by forgeui.
type PagesManager struct {
	fuiApp        *forgeui.App
	basePath      string
	registry      *contributor.ContributorRegistry
	collector     *collector.DataCollector
	history       *collector.DataHistory
	fragmentProxy *proxy.FragmentProxy
	config        PagesConfig
}

// NewPagesManager creates a new PagesManager.
func NewPagesManager(
	fuiApp *forgeui.App,
	basePath string,
	registry *contributor.ContributorRegistry,
	collector *collector.DataCollector,
	history *collector.DataHistory,
	fragmentProxy *proxy.FragmentProxy,
	config PagesConfig,
) *PagesManager {
	return &PagesManager{
		fuiApp:        fuiApp,
		basePath:      basePath,
		registry:      registry,
		collector:     collector,
		history:       history,
		fragmentProxy: fragmentProxy,
		config:        config,
	}
}

// RegisterPages registers all dashboard page routes with forgeui's router.
// Core dashboard pages inherit the default layout (typically "dashboard").
// Settings routes are not registered here — they remain on forge.Router.
func (pm *PagesManager) RegisterPages() error {
	// Core dashboard pages (inherit default layout = "dashboard")
	pm.fuiApp.Page("/").Handler(pm.OverviewPage).Register()
	pm.fuiApp.Page("/health").Handler(pm.HealthPage).Register()
	pm.fuiApp.Page("/metrics").Handler(pm.MetricsPage).Register()
	pm.fuiApp.Page("/services").Handler(pm.ServicesPage).Register()

	// Contributor extension pages
	pm.fuiApp.Page("/ext/:name/pages/*filepath").Handler(pm.ContributorPage).Register()

	// Widget fragments (HTMX auto-refresh) — no special handling needed,
	// RootLayout's HTMX check means they get rendered without the HTML shell.
	pm.fuiApp.Page("/ext/:name/widgets/:id").Handler(pm.WidgetFragment).Register()

	// Remote contributor pages and widgets (fetched via fragment proxy).
	if pm.fragmentProxy != nil {
		pm.fuiApp.Page("/remote/:name/pages/*filepath").Handler(pm.RemotePage).Register()
		pm.fuiApp.Page("/remote/:name/widgets/:id").Handler(pm.RemoteWidget).Register()
	}

	return nil
}

// OverviewPage renders the dashboard overview by delegating to the core contributor.
func (pm *PagesManager) OverviewPage(ctx *router.PageContext) (g.Node, error) {
	local, ok := pm.registry.FindLocalContributor("core")
	if !ok {
		return uipages.ErrorPage(500, "Configuration Error", "No core contributor registered", pm.basePath), nil
	}

	return local.RenderPage(ctx.Context(), "/", contributor.Params{Route: "/"})
}

// HealthPage renders the health status page by delegating to the core contributor.
func (pm *PagesManager) HealthPage(ctx *router.PageContext) (g.Node, error) {
	local, ok := pm.registry.FindLocalContributor("core")
	if !ok {
		return uipages.ErrorPage(500, "Configuration Error", "No core contributor registered", pm.basePath), nil
	}

	return local.RenderPage(ctx.Context(), "/health", contributor.Params{Route: "/health"})
}

// MetricsPage renders the metrics page by delegating to the core contributor.
func (pm *PagesManager) MetricsPage(ctx *router.PageContext) (g.Node, error) {
	local, ok := pm.registry.FindLocalContributor("core")
	if !ok {
		return uipages.ErrorPage(500, "Configuration Error", "No core contributor registered", pm.basePath), nil
	}

	return local.RenderPage(ctx.Context(), "/metrics", contributor.Params{Route: "/metrics"})
}

// ServicesPage renders the services page by delegating to the core contributor.
func (pm *PagesManager) ServicesPage(ctx *router.PageContext) (g.Node, error) {
	local, ok := pm.registry.FindLocalContributor("core")
	if !ok {
		return uipages.ErrorPage(500, "Configuration Error", "No core contributor registered", pm.basePath), nil
	}

	return local.RenderPage(ctx.Context(), "/services", contributor.Params{Route: "/services"})
}

// ContributorPage renders a page from a named contributor extension.
// The contributor name and file path are extracted from route parameters.
func (pm *PagesManager) ContributorPage(ctx *router.PageContext) (g.Node, error) {
	name := ctx.Param("name")
	filepath := ctx.Param("filepath")

	route := "/" + filepath
	if filepath == "" {
		route = "/"
	}

	local, ok := pm.registry.FindLocalContributor(name)
	if !ok {
		return uipages.ErrorPage(404, "Not Found", "Extension '"+name+"' not found", pm.basePath), nil
	}

	return local.RenderPage(ctx.Context(), route, contributor.Params{
		Route:       route,
		PathParams:  map[string]string{},
		QueryParams: map[string]string{},
	})
}

// WidgetFragment renders a single widget as an HTML fragment for HTMX auto-refresh.
func (pm *PagesManager) WidgetFragment(ctx *router.PageContext) (g.Node, error) {
	name := ctx.Param("name")
	widgetID := ctx.Param("id")

	local, ok := pm.registry.FindLocalContributor(name)
	if !ok {
		return ui.WidgetErrorFragment("Extension not found"), nil
	}

	content, err := local.RenderWidget(ctx.Context(), widgetID)
	if err != nil {
		return ui.WidgetErrorFragment("Widget unavailable"), nil
	}

	return content, nil
}

// RemotePage fetches a page from a remote contributor via the fragment proxy
// and returns the raw HTML fragment. ForgeUI wraps it in the dashboard layout.
func (pm *PagesManager) RemotePage(ctx *router.PageContext) (g.Node, error) {
	name := ctx.Param("name")
	filepath := ctx.Param("filepath")

	route := "/" + filepath
	if filepath == "" {
		route = "/"
	}

	data, err := pm.fragmentProxy.FetchPage(ctx.Context(), name, route)
	if err != nil {
		return uipages.ErrorPage(502, "Remote Unavailable",
			"Failed to load page from remote extension '"+name+"': "+err.Error(),
			pm.basePath), nil
	}

	return g.Raw(string(data)), nil
}

// RemoteWidget fetches a widget from a remote contributor via the fragment proxy.
func (pm *PagesManager) RemoteWidget(ctx *router.PageContext) (g.Node, error) {
	name := ctx.Param("name")
	widgetID := ctx.Param("id")

	data, err := pm.fragmentProxy.FetchWidget(ctx.Context(), name, widgetID)
	if err != nil {
		return ui.WidgetErrorFragment("Remote widget unavailable"), nil
	}

	return g.Raw(string(data)), nil
}
