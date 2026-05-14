package dashpages

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/templ"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/router"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/forge/extensions/dashboard/proxy"
	"github.com/xraph/forge/extensions/dashboard/settings"
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
// Page handlers return templ components and delegate rendering to the contributor system.
// Layout wrapping is handled automatically by forgeui.
type PagesManager struct {
	fuiApp        *forgeui.App
	basePath      string
	registry      *contributor.ContributorRegistry
	collector     *collector.DataCollector
	history       *collector.DataHistory
	fragmentProxy *proxy.FragmentProxy
	settingsAgg   *settings.Aggregator
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
	settingsAgg *settings.Aggregator,
	config PagesConfig,
) *PagesManager {
	return &PagesManager{
		fuiApp:        fuiApp,
		basePath:      basePath,
		registry:      registry,
		collector:     collector,
		history:       history,
		fragmentProxy: fragmentProxy,
		settingsAgg:   settingsAgg,
		config:        config,
	}
}

// SetAuthEnabled updates the auth configuration at runtime. This is used
// for late auth registration when an auth provider registers after the
// pages manager has already been constructed.
func (pm *PagesManager) SetAuthEnabled(enabled bool, defaultAccess, loginPath string) {
	pm.config.EnableAuth = enabled
	pm.config.DefaultAccess = defaultAccess
	pm.config.LoginPath = loginPath
}

// RegisterPages registers all dashboard page routes with forgeui's router.
// Core dashboard pages inherit the default layout (typically "dashboard").
// Settings pages use the "settings" layout (nested under dashboard).
func (pm *PagesManager) RegisterPages() error {
	// Resolve the default access level middleware for core pages
	defaultMW := pm.defaultAccessMiddleware()

	// Slice (i): legacy CoreContributor templ pages are retired. The contract
	// React shell at {basePath}/contract/app/* now serves Overview / Health /
	// Metrics / Traces / Services / Extensions. Old paths 302 to the shell so
	// existing bookmarks keep working. /metrics/all, /metrics/collectors/:name
	// and /metrics/detail/*name collapse onto /contract/app/metrics — slice (j)
	// adds proper deep-link routes when those pages get rebuilt on the React
	// side.
	shellBase := pm.basePath + "/contract/app"
	pm.fuiApp.Page("/").Handler(redirectTo(shellBase + "/")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/health").Handler(redirectTo(shellBase + "/health")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/metrics").Handler(redirectTo(shellBase + "/metrics")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/metrics/all").Handler(redirectTo(shellBase + "/metrics")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/metrics/collectors/:name").Handler(redirectTo(shellBase + "/metrics")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/metrics/detail/*name").Handler(redirectTo(shellBase + "/metrics")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/services").Handler(redirectTo(shellBase + "/services")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/extensions").Handler(redirectTo(shellBase + "/extensions")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/traces").Handler(redirectTo(shellBase + "/traces")).Middleware(defaultMW...).Register()
	pm.fuiApp.Page("/traces/:id").Handler(redirectTraceDetail(shellBase)).Middleware(defaultMW...).Register()

	// Settings pages (use "settings" layout = settings sub-nav → dashboard → root)
	if pm.config.EnableSettings && pm.settingsAgg != nil {
		pm.fuiApp.Page("/settings").Handler(pm.SettingsPage).Layout("settings").Middleware(defaultMW...).Register()
		pm.fuiApp.Page("/ext/:name/settings/:id").Handler(pm.SettingsFormPage).Layout("settings").Middleware(defaultMW...).Register()
		pm.fuiApp.Page("/ext/:name/settings/:id").Method("POST").Handler(pm.SettingsSubmitPage).Layout("settings").Middleware(defaultMW...).Register()
	}

	// Extension-layout contributor pages (standalone layout, registered first
	// so they take precedence over the generic catch-all route below).
	pm.registerExtensionLayoutPages()

	// Contributor extension pages — auth middleware is applied dynamically
	// inside ContributorPage based on the NavItem.Access field.
	// Register exact path first (wildcard requires at least one char after /pages/).
	pm.fuiApp.Page("/ext/:name/pages").Handler(pm.ContributorPage).Register()
	pm.fuiApp.Page("/ext/:name/pages/*filepath").Handler(pm.ContributorPage).Register()
	// POST routes for contributor page form submissions.
	pm.fuiApp.Page("/ext/:name/pages").Method("POST").Handler(pm.ContributorPage).Register()
	pm.fuiApp.Page("/ext/:name/pages/*filepath").Method("POST").Handler(pm.ContributorPage).Register()

	// Widget fragments (HTMX auto-refresh) — always public (they are embedded)
	pm.fuiApp.Page("/ext/:name/widgets/:id").Handler(pm.WidgetFragment).Register()

	// Remote contributor pages and widgets (fetched via fragment proxy).
	// Routes are pre-registered under the extension layout so late-discovered
	// extension-layout remotes (e.g. authsome via service discovery) render
	// with the contributor's own topbar/sidebar shell. The extension layout
	// already falls back to a topbar-only chrome via ShowSidebarOrDefault when
	// the contributor's manifest opts out, so dashboard-layout remotes still
	// work — they just lose the dashboard sidebar, which they were never part
	// of anyway (the registry's nav merge skips Layout=="extension" remotes
	// and the catch-all wildcard never carried a remote contributor's group
	// nav into the dashboard sidebar in the first place). POST routes are
	// registered too so contributor forms (?action=…) round-trip.
	if pm.fragmentProxy != nil {
		pm.fuiApp.Page("/remote/:name/pages").Handler(pm.RemotePage).Layout("extension").Middleware(defaultMW...).Register()
		pm.fuiApp.Page("/remote/:name/pages/*filepath").Handler(pm.RemotePage).Layout("extension").Middleware(defaultMW...).Register()
		pm.fuiApp.Page("/remote/:name/pages").Method("POST").Handler(pm.RemotePage).Layout("extension").Middleware(defaultMW...).Register()
		pm.fuiApp.Page("/remote/:name/pages/*filepath").Method("POST").Handler(pm.RemotePage).Layout("extension").Middleware(defaultMW...).Register()
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
		loginPath = "/login"
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

// registerExtensionLayoutPages registers dedicated page routes for contributors
// that use the "extension" layout. These are registered with the extension layout
// so they get the standalone topbar + app grid navigator (and optional extension
// sidebar) instead of the default sidebar-based dashboard layout.
//
// Both local and remote contributors are handled here. For local contributors the
// handler delegates to the in-process LocalContributor.RenderPage. For remote
// contributors it proxies the page HTML via the fragment proxy. Forms (POST) are
// supported for both. The per-name routes (e.g. /remote/authsome/pages) win over
// the generic /remote/:name/pages wildcard registered later in RegisterPages
// because forgeui's router prefers static segments over parameter segments.
func (pm *PagesManager) registerExtensionLayoutPages() {
	for _, name := range pm.registry.ContributorNames() {
		m, ok := pm.registry.GetManifest(name)
		if !ok || m.Layout != "extension" {
			continue
		}

		var (
			handler    router.PageHandler
			pathPrefix string
		)

		if pm.registry.IsRemote(name) {
			if pm.fragmentProxy == nil {
				continue
			}

			handler = pm.remoteExtensionHandler(name)
			pathPrefix = "/remote/" + name + "/pages"
		} else {
			handler = pm.localExtensionHandler(name)
			pathPrefix = "/ext/" + name + "/pages"
		}

		pm.fuiApp.Page(pathPrefix).Handler(handler).Layout("extension").Register()
		pm.fuiApp.Page(pathPrefix + "/*filepath").Handler(handler).Layout("extension").Register()
		// POST routes for extension-layout form submissions.
		pm.fuiApp.Page(pathPrefix).Method("POST").Handler(handler).Layout("extension").Register()
		pm.fuiApp.Page(pathPrefix + "/*filepath").Method("POST").Handler(handler).Layout("extension").Register()
	}
}

// localExtensionHandler returns the handler for an extension-layout LocalContributor.
func (pm *PagesManager) localExtensionHandler(contribName string) router.PageHandler {
	return func(ctx *router.PageContext) (templ.Component, error) {
		route := extensionRouteFromCtx(ctx)

		local, ok := pm.registry.FindLocalContributor(contribName)
		if !ok {
			return uipages.ErrorPage(404, "Not Found",
				"Extension '"+contribName+"' not found", pm.basePath), nil
		}

		pageBase := fmt.Sprintf("%s/ext/%s/pages", pm.basePath, contribName)
		ctx.Request = ctx.Request.WithContext(contributor.WithPageBase(ctx.Context(), pageBase))

		// Enrich context for layout rendering (sidebar/topbar content slots).
		if cp, ok := local.(contributor.ContextPreparer); ok {
			enrichedCtx := cp.PrepareContext(ctx.Context(), route)
			ctx.Request = ctx.Request.WithContext(enrichedCtx)
		}

		if blocked, comp := pm.enforceContributorAccess(ctx, local.Manifest(), route); blocked {
			return comp, nil
		}

		return local.RenderPage(ctx.Context(), route, contributor.Params{
			Route:       route,
			BasePath:    pm.basePath,
			PageBase:    pageBase,
			QueryParams: queryParamsFromCtx(ctx),
			FormData:    formDataFromCtx(ctx),
		})
	}
}

// remoteExtensionHandler returns the handler for an extension-layout remote
// contributor — proxies pages via the fragment proxy and emits the HTML inside
// the dashboard's extension layout shell.
func (pm *PagesManager) remoteExtensionHandler(contribName string) router.PageHandler {
	return func(ctx *router.PageContext) (templ.Component, error) {
		route := extensionRouteFromCtx(ctx)

		manifest, ok := pm.registry.GetManifest(contribName)
		if !ok {
			return uipages.ErrorPage(404, "Not Found",
				"Extension '"+contribName+"' not found", pm.basePath), nil
		}

		if blocked, comp := pm.enforceContributorAccess(ctx, manifest, route); blocked {
			return comp, nil
		}

		pageBase := fmt.Sprintf("%s/remote/%s/pages", pm.basePath, contribName)
		fetchQuery := buildProxyFetchQuery(ctx.Request.URL.RawQuery, pm.basePath, pageBase)

		data, err := proxyToRemote(ctx, pm.fragmentProxy, contribName, route, fetchQuery)
		if err != nil {
			return uipages.ErrorPage(502, "Remote Unavailable", //nolint:nilerr // surface as page rather than propagating
				"Failed to load page from remote extension '"+contribName+"': "+err.Error(),
				pm.basePath), nil
		}

		return templ.Raw(string(data)), nil
	}
}

// proxyToRemote forwards a host page request (GET or POST) to a remote
// contributor via the FragmentProxy. POSTs route through PostPage so the
// inbound body + content-type reach the upstream's form handler; GETs use
// the cached FetchPage.
//
// The inbound request's Authorization and Cookie headers are forwarded
// to the remote via contributor.WithForwardedHeaders so handlers there
// can identify the end user (the registered remote is already a
// trusted target — the host registered it explicitly via
// WatchRemoteContributor / AddRemoteContributor).
func proxyToRemote(ctx *router.PageContext, fp *proxy.FragmentProxy, name, route, query string) ([]byte, error) {
	fwdCtx := contributor.WithForwardedHeaders(ctx.Context(), ctx.Request.Header)

	if ctx.Request.Method == http.MethodPost {
		contentType := ctx.Request.Header.Get("Content-Type")
		// We pass the raw body through. The upstream's POST handler is
		// responsible for parsing the form (whether url-encoded or
		// multipart). Closing of the body is handled by the http.Request
		// lifecycle on the host side.
		return fp.PostPage(fwdCtx, name, route, query, ctx.Request.Body, contentType)
	}

	return fp.FetchPage(fwdCtx, name, route, query)
}

// buildProxyFetchQuery merges the user's incoming query string with the
// reserved bp (basePath) and pb (pageBase) parameters that contributor
// protocol handlers consume to render correctly-prefixed links back to the
// consumer's URL space. User-supplied bp/pb are overwritten — they're
// reserved.
func buildProxyFetchQuery(userRawQuery, basePath, pageBase string) string {
	values, err := url.ParseQuery(userRawQuery)
	if err != nil {
		// Fall back to a fresh query — the caller's malformed input shouldn't
		// abort the proxy fetch entirely.
		values = url.Values{}
	}

	if basePath != "" {
		values.Set("bp", basePath)
	}

	if pageBase != "" {
		values.Set("pb", pageBase)
	}

	return values.Encode()
}

// extensionRouteFromCtx resolves the contributor-relative route from the
// :filepath wildcard, defaulting to "/" when no subpath is present.
func extensionRouteFromCtx(ctx *router.PageContext) string {
	filepath := ctx.Param("filepath")
	if filepath == "" {
		return "/"
	}

	return "/" + filepath
}

// queryParamsFromCtx flattens URL query parameters into a single-value map.
func queryParamsFromCtx(ctx *router.PageContext) map[string]string {
	qp := make(map[string]string)

	for k, v := range ctx.Request.URL.Query() {
		if len(v) > 0 {
			qp[k] = v[0]
		}
	}

	return qp
}

// formDataFromCtx parses POST form values into a single-value map.
func formDataFromCtx(ctx *router.PageContext) map[string]string {
	fd := make(map[string]string)
	if ctx.Request.Method != http.MethodPost {
		return fd
	}

	if err := ctx.Request.ParseForm(); err == nil {
		for k, v := range ctx.Request.PostForm {
			if len(v) > 0 {
				fd[k] = v[0]
			}
		}
	}

	return fd
}

// enforceContributorAccess applies the auth access level for a specific
// contributor route. Returns (true, component) when the request was blocked
// (component is the response to render) and (false, nil) when access is
// permitted and the caller should continue.
func (pm *PagesManager) enforceContributorAccess(ctx *router.PageContext, manifest *contributor.Manifest, route string) (bool, templ.Component) {
	if !pm.config.EnableAuth {
		return false, nil
	}

	if pm.resolveContributorAccess(manifest, route) != dashauth.AccessProtected {
		return false, nil
	}

	user := dashauth.UserFromContext(ctx.Context())
	if user.Authenticated() {
		return false, nil
	}

	loginPath := pm.config.BasePath + pm.config.LoginPath
	if ctx.Request.Header.Get("Hx-Request") != "" {
		ctx.ResponseWriter.Header().Set("Hx-Redirect", loginPath+"?redirect="+ctx.Request.URL.Path)
		ctx.ResponseWriter.WriteHeader(http.StatusUnauthorized)
	}

	return true, templ.Raw("")
}

// redirectTo returns a forgeui PageHandler that emits a 302 to the given target.
// Used by slice (i) to forward legacy templ paths to the React shell.
func redirectTo(target string) router.PageHandler {
	return func(ctx *router.PageContext) (templ.Component, error) {
		http.Redirect(ctx.ResponseWriter, ctx.Request, target, http.StatusFound)
		return templ.Raw(""), nil
	}
}

// redirectTraceDetail forwards /traces/:id to {shellBase}/traces/<id>. Slice (j)
// added the matching /traces/:id route to the React shell + pilot manifest, so
// we can use a clean path-style redirect (it was a ?id= query string under
// slice (i) before the shell knew the route).
func redirectTraceDetail(shellBase string) router.PageHandler {
	return func(ctx *router.PageContext) (templ.Component, error) {
		id := ctx.Param("id")
		target := shellBase + "/traces"
		if id != "" {
			target += "/" + url.PathEscape(id)
		}
		http.Redirect(ctx.ResponseWriter, ctx.Request, target, http.StatusFound)
		return templ.Raw(""), nil
	}
}

// SettingsPage renders the settings index page listing all available settings.
func (pm *PagesManager) SettingsPage(ctx *router.PageContext) (templ.Component, error) {
	groups := pm.settingsAgg.GetAllGrouped()
	return settings.SettingsIndexPage(groups, pm.basePath), nil
}

// SettingsFormPage renders a contributor's settings form.
func (pm *PagesManager) SettingsFormPage(ctx *router.PageContext) (templ.Component, error) {
	name := ctx.Param("name")
	settingID := ctx.Param("id")

	local, ok := pm.registry.FindLocalContributor(name)
	if !ok {
		return uipages.ErrorPage(404, "Not Found", "Extension '"+name+"' not found", pm.basePath), nil
	}

	content, err := local.RenderSettings(ctx.Context(), settingID)
	if err != nil {
		return uipages.ErrorPage(500, "Error", "Setting unavailable: "+err.Error(), pm.basePath), nil
	}

	return content, nil
}

// SettingsSubmitPage processes a settings form submission and re-renders the form.
func (pm *PagesManager) SettingsSubmitPage(ctx *router.PageContext) (templ.Component, error) {
	return pm.SettingsFormPage(ctx)
}

// ContributorPage renders a page from a named contributor extension.
// The contributor name and file path are extracted from route parameters.
// When auth is enabled, it dynamically applies the access level from the
// contributor's NavItem.Access field.
func (pm *PagesManager) ContributorPage(ctx *router.PageContext) (templ.Component, error) {
	name := ctx.Param("name")
	filepath := ctx.Param("filepath")

	route := "/" + filepath
	if filepath == "" {
		route = "/"
	}

	local, ok := pm.registry.FindLocalContributor(name)
	if !ok {
		// Fall back to /remote/<name>/... when the same contributor was
		// discovered as remote (typical when authsome moves from in-process
		// to a discovered identity service). Avoids 404s on stale bookmarks.
		if pm.registry.IsRemote(name) {
			return redirectToPrefix(ctx, pm.basePath, "remote", name, route), nil
		}

		return uipages.ErrorPage(404, "Not Found", "Extension '"+name+"' not found", pm.basePath), nil
	}

	pageBase := fmt.Sprintf("%s/ext/%s/pages", pm.basePath, name)
	ctx.Request = ctx.Request.WithContext(contributor.WithPageBase(ctx.Context(), pageBase))

	// Enrich context for layout rendering (sidebar/topbar content slots).
	if cp, ok := local.(contributor.ContextPreparer); ok {
		enrichedCtx := cp.PrepareContext(ctx.Context(), route)
		ctx.Request = ctx.Request.WithContext(enrichedCtx)
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

					return templ.Raw(""), nil
				}

				return templ.Raw(""), nil // PageMiddleware would have redirected; fallback
			}
		}
	}

	// Build query params from request URL.
	qp := make(map[string]string)
	for k, v := range ctx.Request.URL.Query() {
		if len(v) > 0 {
			qp[k] = v[0]
		}
	}

	// Build form data from POST body.
	fd := make(map[string]string)
	if ctx.Request.Method == http.MethodPost {
		if err := ctx.Request.ParseForm(); err == nil {
			for k, v := range ctx.Request.PostForm {
				if len(v) > 0 {
					fd[k] = v[0]
				}
			}
		}
	}

	return local.RenderPage(ctx.Context(), route, contributor.Params{
		Route:       route,
		BasePath:    pm.basePath,
		PageBase:    pageBase,
		PathParams:  map[string]string{},
		QueryParams: qp,
		FormData:    fd,
	})
}

// redirectToPrefix returns a templ component that redirects the browser to the
// given contributor prefix ("ext" for local, "remote" for discovered). For HTMX
// requests it sets Hx-Redirect; for plain navigation it emits a tiny HTML
// fragment with a meta-refresh + JS fallback so the user lands on the new URL.
// Used to soften the transition when a contributor moves between local and
// remote registration without breaking stale bookmarks.
func redirectToPrefix(ctx *router.PageContext, basePath, prefix, name, route string) templ.Component {
	target := fmt.Sprintf("%s/%s/%s/pages%s", basePath, prefix, name, route)
	if ctx.Request.URL.RawQuery != "" {
		target += "?" + ctx.Request.URL.RawQuery
	}

	if ctx.Request.Header.Get("Hx-Request") != "" {
		ctx.ResponseWriter.Header().Set("Hx-Redirect", target)
		ctx.ResponseWriter.WriteHeader(http.StatusOK)

		return templ.Raw("")
	}

	ctx.ResponseWriter.Header().Set("Location", target)
	ctx.ResponseWriter.WriteHeader(http.StatusSeeOther)

	return templ.Raw(fmt.Sprintf(
		`<!doctype html><meta http-equiv="refresh" content="0;url=%s"><script>location.replace(%q)</script>`,
		htmlAttrEscape(target), target,
	))
}

// htmlAttrEscape escapes the minimal characters needed to safely embed a URL in
// an HTML attribute value (we control the inputs but stay defensive in case
// route or query strings ever carry quotes or angle brackets).
func htmlAttrEscape(s string) string {
	return htmlEscaper.Replace(s)
}

var htmlEscaper = strings.NewReplacer(
	`&`, "&amp;",
	`"`, "&quot;",
	`'`, "&#39;",
	`<`, "&lt;",
	`>`, "&gt;",
)

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
func (pm *PagesManager) WidgetFragment(ctx *router.PageContext) (templ.Component, error) {
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
func (pm *PagesManager) RemotePage(ctx *router.PageContext) (templ.Component, error) {
	name := ctx.Param("name")
	filepath := ctx.Param("filepath")

	route := "/" + filepath
	if filepath == "" {
		route = "/"
	}

	if !pm.registry.IsRemote(name) {
		// Symmetric fallback: bookmarks for /remote/<name>/... after the
		// contributor flips back to local-only registration should land on
		// /ext/<name>/... instead of a 502.
		if _, ok := pm.registry.FindLocalContributor(name); ok {
			return redirectToPrefix(ctx, pm.basePath, "ext", name, route), nil
		}

		return uipages.ErrorPage(404, "Not Found",
			"Extension '"+name+"' not found", pm.basePath), nil
	}

	pageBase := fmt.Sprintf("%s/remote/%s/pages", pm.basePath, name)
	fetchQuery := buildProxyFetchQuery(ctx.Request.URL.RawQuery, pm.basePath, pageBase)

	data, err := proxyToRemote(ctx, pm.fragmentProxy, name, route, fetchQuery)
	if err != nil {
		return uipages.ErrorPage(502, "Remote Unavailable", //nolint:nilerr // render error page instead of propagating
			"Failed to load page from remote extension '"+name+"': "+err.Error(),
			pm.basePath), nil
	}

	return templ.Raw(string(data)), nil
}

// RemoteWidget fetches a widget from a remote contributor via the fragment proxy.
func (pm *PagesManager) RemoteWidget(ctx *router.PageContext) (templ.Component, error) {
	name := ctx.Param("name")
	widgetID := ctx.Param("id")

	data, err := pm.fragmentProxy.FetchWidget(ctx.Context(), name, widgetID)
	if err != nil {
		return ui.WidgetErrorFragment("Remote widget unavailable"), nil //nolint:nilerr // render error fragment instead of propagating
	}

	return templ.Raw(string(data)), nil
}
