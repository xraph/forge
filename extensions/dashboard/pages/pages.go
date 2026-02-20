package dashpages

import (
	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/router"
	g "maragu.dev/gomponents"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
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
	EnableAuth     bool
	DefaultAccess  string // "public", "protected", "partial"
	LoginPath      string // relative login path (e.g. "/auth/login")
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
	// Resolve the default access level middleware for core pages
	defaultMW := pm.defaultAccessMiddleware()

	// Core dashboard pages (inherit default layout = "dashboard")
	pm.fuiApp.Page("/").Handler(pm.OverviewPage).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/health").Handler(pm.HealthPage).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/metrics").Handler(pm.MetricsPage).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/services").Handler(pm.ServicesPage).Middleware(defaultMW...).Register()

	// Contributor extension pages — auth middleware is applied dynamically
	// inside ContributorPage based on the NavItem.Access field.
	pm.fuiApp.Page("/ext/:name/pages/*filepath").Handler(pm.ContributorPage).Register()

	// Widget fragments (HTMX auto-refresh) — always public (they are embedded)
	pm.fuiApp.Page("/ext/:name/widgets/:id").Handler(pm.WidgetFragment).Register()

	// Remote contributor pages and widgets (fetched via fragment proxy).
	if pm.fragmentProxy != nil {
		pm.fuiApp.Page("/remote/:name/pages/*filepath").Handler(pm.RemotePage).Middleware(defaultMW...).Register()
		pm.fuiApp.Page("/remote/:name/widgets/:id").Handler(pm.RemoteWidget).Register()
	}

	return nil
}

// defaultAccessMiddleware returns ForgeUI middleware based on the configured
// default access level. Returns nil (no middleware) when auth is disabled.
func (pm *PagesManager) defaultAccessMiddleware() []router.Middleware {
	if !pm.config.EnableAuth {
		return nil
	}

	level := dashauth.ParseAccessLevel(pm.config.DefaultAccess)

	loginPath := pm.config.LoginPath
	if loginPath == "" {
		loginPath = "/auth/login"
	}

	return pm.accessMiddleware(level, loginPath)
}

// accessMiddleware returns ForgeUI middleware for the given access level.
func (pm *PagesManager) accessMiddleware(level dashauth.AccessLevel, loginPath string) []router.Middleware {
	if level == dashauth.AccessPublic {
		return nil
	}

	return []router.Middleware{dashauth.PageMiddleware(level, pm.config.BasePath+loginPath)}
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
// When auth is enabled, it dynamically applies the access level from the
// contributor's NavItem.Access field.
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

	// Check auth access level for this specific page
	if pm.config.EnableAuth {
		accessLevel := pm.resolveContributorAccess(local.Manifest(), route)
		if accessLevel == dashauth.AccessProtected {
			user := dashauth.UserFromContext(ctx.Context())
			if !user.Authenticated() {
				loginPath := pm.config.BasePath + pm.config.LoginPath
				// HTMX-aware redirect
				isHTMX := ctx.Request.Header.Get("Hx-Request") != ""
				if isHTMX {
					ctx.ResponseWriter.Header().Set("Hx-Redirect", loginPath+"?redirect="+ctx.Request.URL.Path)
					ctx.ResponseWriter.WriteHeader(401)

					return g.Raw(""), nil
				}

				return g.Raw(""), nil // PageMiddleware would have redirected; fallback
			}
		}
	}

	return local.RenderPage(ctx.Context(), route, contributor.Params{
		Route:       route,
		PathParams:  map[string]string{},
		QueryParams: map[string]string{},
	})
}

// resolveContributorAccess looks up the access level for a specific route
// within a contributor's manifest. Falls back to the dashboard default.
func (pm *PagesManager) resolveContributorAccess(manifest *contributor.Manifest, route string) dashauth.AccessLevel {
	if manifest == nil {
		return dashauth.ParseAccessLevel(pm.config.DefaultAccess)
	}

	for _, nav := range manifest.Nav {
		if nav.Path == route && nav.Access != "" {
			return dashauth.ParseAccessLevel(nav.Access)
		}
		// Check children
		for _, child := range nav.Children {
			if child.Path == route && child.Access != "" {
				return dashauth.ParseAccessLevel(child.Access)
			}
		}
	}

	// Fall back to dashboard default
	return dashauth.ParseAccessLevel(pm.config.DefaultAccess)
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
		return ui.WidgetErrorFragment("Widget unavailable"), nil //nolint:nilerr // render error fragment instead of propagating
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
		return uipages.ErrorPage(502, "Remote Unavailable", //nolint:nilerr // render error page instead of propagating
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
		return ui.WidgetErrorFragment("Remote widget unavailable"), nil //nolint:nilerr // render error fragment instead of propagating
	}

	return g.Raw(string(data)), nil
}
