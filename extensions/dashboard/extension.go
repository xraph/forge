package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/xraph/forge"

	dashassets "github.com/xraph/forge/extensions/dashboard/assets"
	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contributor"
	dashboarddiscovery "github.com/xraph/forge/extensions/dashboard/discovery"
	"github.com/xraph/forge/extensions/dashboard/handlers"
	"github.com/xraph/forge/extensions/dashboard/layouts"
	dashpages "github.com/xraph/forge/extensions/dashboard/pages"
	"github.com/xraph/forge/extensions/dashboard/proxy"
	"github.com/xraph/forge/extensions/dashboard/recovery"
	"github.com/xraph/forge/extensions/dashboard/search"
	"github.com/xraph/forge/extensions/dashboard/security"
	"github.com/xraph/forge/extensions/dashboard/settings"
	"github.com/xraph/forge/extensions/dashboard/sse"
	dashtheme "github.com/xraph/forge/extensions/dashboard/theme"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/bridge"
	"github.com/xraph/forgeui/router"
	"github.com/xraph/forgeui/theme"
	"github.com/xraph/forgeui/utils"
)

// Extension implements the extensible dashboard micro-frontend shell.
// Contributors (local or remote) register pages, widgets, and settings
// that are merged into a unified admin dashboard.
type Extension struct {
	*forge.BaseExtension

	config           Config
	app              forge.App
	fuiApp           *forgeui.App
	layoutMgr        *layouts.LayoutManager
	pagesMgr         *dashpages.PagesManager
	registry         *contributor.ContributorRegistry
	collector        *collector.DataCollector
	history          *collector.DataHistory
	dashBridge       *DashboardBridge
	sseBroker        *sse.Broker
	fragmentProxy    *proxy.FragmentProxy
	discoverySvc     dashboarddiscovery.DiscoveryService
	discoveryInteg   *dashboarddiscovery.Integration
	recoveryMgr      *recovery.Manager
	searcher         *search.FederatedSearch
	settingsAgg      *settings.Aggregator
	sanitizer        *security.Sanitizer
	csrfMgr          *security.CSRFManager
	themeMgr         *dashtheme.Manager
	authChecker      dashauth.AuthChecker
	authPageProv     dashauth.AuthPageProvider
	routesRegistered bool
}

// NewExtension creates a new dashboard extension.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension(
		"dashboard",
		"3.0.0",
		"Extensible dashboard micro-frontend shell",
	)

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// Register registers the dashboard extension.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	e.app = app

	// Load config from ConfigManager with dual-key support
	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("dashboard", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("dashboard: failed to load required config: %w", err)
		}

		e.Logger().Warn("dashboard: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("dashboard config validation failed: %w", err)
	}

	// Initialize contributor registry
	e.registry = contributor.NewContributorRegistry()

	// Initialize data history
	e.history = collector.NewDataHistory(e.config.MaxDataPoints, e.config.HistoryDuration)

	// Initialize data collector
	e.collector = collector.NewDataCollector(
		app.HealthManager(),
		app.Metrics(),
		app.Container(),
		app.Logger(),
		e.history,
	)

	// Initialize SSE broker (if real-time is enabled)
	if e.config.EnableRealtime {
		e.sseBroker = sse.NewBroker(e.config.SSEKeepAlive, e.Logger())
		e.Logger().Debug("SSE broker initialized",
			forge.F("keep_alive", e.config.SSEKeepAlive.String()),
		)
	}

	// Initialize fragment proxy for remote contributors
	e.fragmentProxy = proxy.NewFragmentProxy(
		e.registry,
		e.config.CacheMaxSize,
		e.config.CacheTTL,
		e.config.ProxyTimeout,
		e.Logger(),
	)

	// Initialize ForgeUI app (creates bridge internally).
	// Must be called after fragmentProxy is initialized since PagesManager
	// needs the proxy for remote contributor pages.
	e.initializeForgeUI()

	// Use the forgeui-managed bridge for dashboard functions
	if e.config.EnableBridge {
		e.dashBridge = NewDashboardBridgeWithBridge(e.fuiApp.Bridge(), e.collector, e.history)
		e.Logger().Debug("bridge function system initialized",
			forge.F("functions", e.dashBridge.Bridge().FunctionCount()),
		)
	}

	// Initialize recovery manager for remote contributors
	e.recoveryMgr = recovery.NewManager(e.Logger())

	// Wire recovery state changes to SSE broker for real-time UI updates
	if e.sseBroker != nil {
		e.recoveryMgr.SetOnStateChange(func(name string, oldState, newState recovery.HealthState) {
			e.sseBroker.BroadcastJSON("contributor-health", map[string]any{
				"contributor": name,
				"old_state":   string(oldState),
				"new_state":   string(newState),
			})
		})
	}

	// Initialize federated search (if enabled)
	if e.config.EnableSearch {
		e.searcher = search.NewFederatedSearch(e.registry, e.config.BasePath, e.Logger())
	}

	// Initialize settings aggregator (if enabled)
	if e.config.EnableSettings {
		e.settingsAgg = settings.NewAggregator(e.registry)
	}

	// Initialize security components
	e.sanitizer = security.NewSanitizer()

	if e.config.EnableCSRF {
		e.csrfMgr = security.NewCSRFManager()
		e.Logger().Debug("CSRF protection initialized")
	}

	// Initialize theme manager
	e.themeMgr = dashtheme.NewManager(dashtheme.ThemeConfig{
		Mode:      e.config.Theme,
		CustomCSS: e.config.CustomCSS,
	})

	// Register built-in core contributor
	core := NewCoreContributor(e.collector, e.history)
	if err := e.registry.RegisterLocal(core); err != nil {
		return fmt.Errorf("failed to register core contributor: %w", err)
	}

	// Rebuild search index after core contributor is registered
	if e.searcher != nil {
		e.searcher.RebuildIndex()
	}

	// Register dashboard extension with DI container
	if err := forge.RegisterValue[*Extension](app.Container(), "dashboard", e); err != nil {
		return fmt.Errorf("failed to register dashboard service: %w", err)
	}

	e.Logger().Info("dashboard extension registered",
		forge.F("base_path", e.config.BasePath),
		forge.F("realtime", e.config.EnableRealtime),
		forge.F("export", e.config.EnableExport),
		forge.F("search", e.config.EnableSearch),
		forge.F("settings", e.config.EnableSettings),
		forge.F("discovery", e.config.EnableDiscovery),
		forge.F("bridge", e.config.EnableBridge),
	)

	return nil
}

// Start starts the dashboard extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting dashboard extension")

	// Auto-discover DashboardAware and BridgeAware extensions
	e.discoverExtensionContributors(ctx)

	// Register routes (only once)
	if !e.routesRegistered {
		e.registerRoutes()
		e.routesRegistered = true
	}

	// Start data collection
	go e.collector.Start(ctx, e.config.RefreshInterval)

	// Start discovery integration (if enabled)
	if e.config.EnableDiscovery {
		e.startDiscoveryIntegration(ctx)
	}

	e.MarkStarted()
	e.Logger().Info("dashboard extension started",
		forge.F("base_path", e.config.BasePath),
		forge.F("contributors", e.registry.ContributorCount()),
		forge.F("realtime", e.sseBroker != nil),
		forge.F("discovery", e.discoveryInteg != nil),
	)

	return nil
}

// discoverExtensionContributors scans all registered extensions for DashboardAware
// and BridgeAware interfaces, registering their contributors and bridge functions.
func (e *Extension) discoverExtensionContributors(ctx context.Context) {
	extensions := e.app.Extensions()
	for _, ext := range extensions {
		// Skip ourselves
		if ext.Name() == e.Name() {
			continue
		}

		// Auto-register DashboardAware contributors
		if aware, ok := ext.(DashboardAware); ok {
			c := aware.DashboardContributor()
			if c == nil {
				e.Logger().Warn("extension returned nil DashboardContributor",
					forge.F("extension", ext.Name()),
				)

				continue
			}

			// Check if it's an SSR contributor that needs lifecycle management
			if ssrC, ok := c.(*contributor.SSRContributor); ok {
				if err := e.registry.RegisterSSR(ssrC); err != nil {
					e.Logger().Error("failed to register SSR contributor",
						forge.F("extension", ext.Name()),
						forge.F("error", err.Error()),
					)

					continue
				}
				// Start the SSR sidecar
				if err := ssrC.Start(ctx); err != nil {
					e.Logger().Error("failed to start SSR contributor",
						forge.F("extension", ext.Name()),
						forge.F("error", err.Error()),
					)
				}
			} else {
				// Standard LocalContributor (including EmbeddedContributor)
				if err := e.registry.RegisterLocal(c); err != nil {
					e.Logger().Error("failed to register contributor",
						forge.F("extension", ext.Name()),
						forge.F("error", err.Error()),
					)

					continue
				}
			}

			e.Logger().Info("auto-discovered dashboard contributor",
				forge.F("extension", ext.Name()),
				forge.F("contributor", c.Manifest().Name),
			)
		}

		// Auto-register BridgeAware bridge functions
		if bridgeAware, ok := ext.(BridgeAware); ok && e.dashBridge != nil {
			if err := bridgeAware.RegisterDashboardBridge(e.dashBridge.Bridge()); err != nil {
				e.Logger().Error("failed to register bridge functions",
					forge.F("extension", ext.Name()),
					forge.F("error", err.Error()),
				)
			} else {
				e.Logger().Info("auto-discovered bridge functions",
					forge.F("extension", ext.Name()),
				)
			}
		}
	}

	// Rebuild search index after auto-discovery
	if e.searcher != nil {
		e.searcher.RebuildIndex()
	}
}

// Stop stops the dashboard extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping dashboard extension")

	// Stop SSR contributors
	for _, name := range e.registry.SSRContributorNames() {
		if ssrC, ok := e.registry.FindSSRContributor(name); ok {
			if err := ssrC.Stop(); err != nil {
				e.Logger().Error("failed to stop SSR contributor",
					forge.F("contributor", name),
					forge.F("error", err.Error()),
				)
			}
		}
	}

	// Stop discovery integration
	if e.discoveryInteg != nil {
		e.discoveryInteg.Stop()
	}

	// Close SSE broker (disconnects all clients)
	if e.sseBroker != nil {
		e.sseBroker.Close()
	}

	// Stop data collector
	if e.collector != nil {
		e.collector.Stop()
	}

	e.MarkStopped()
	e.Logger().Info("dashboard extension stopped")

	return nil
}

// Health checks if the dashboard is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if e.collector == nil {
		return ErrCollectorNotInitialized
	}

	return nil
}

// Dependencies returns extension dependencies.
func (e *Extension) Dependencies() []string {
	return []string{} // No hard dependencies
}

// Registry returns the contributor registry.
func (e *Extension) Registry() *contributor.ContributorRegistry {
	return e.registry
}

// Collector returns the data collector instance.
func (e *Extension) Collector() *collector.DataCollector {
	return e.collector
}

// History returns the data history instance.
func (e *Extension) History() *collector.DataHistory {
	return e.history
}

// RegisterContributor registers a local contributor with the dashboard.
// This is the primary API for extensions to contribute UI to the dashboard.
func (e *Extension) RegisterContributor(c contributor.LocalContributor) error {
	return e.registry.RegisterLocal(c)
}

// DashboardBridge returns the dashboard bridge instance for registering custom functions.
// Returns nil if the bridge is not enabled.
func (e *Extension) DashboardBridge() *DashboardBridge {
	return e.dashBridge
}

// RegisterBridgeFunction registers a custom bridge function callable from the dashboard UI.
// This is a convenience method — callers can also use DashboardBridge().Register() directly.
// Returns an error if the bridge is not enabled.
func (e *Extension) RegisterBridgeFunction(name string, handler any, opts ...bridge.FunctionOption) error {
	if e.dashBridge == nil {
		return errors.New("dashboard bridge is not enabled")
	}

	return e.dashBridge.Register(name, handler, opts...)
}

// SSEBroker returns the SSE event broker. Returns nil if real-time is disabled.
func (e *Extension) SSEBroker() *sse.Broker {
	return e.sseBroker
}

// FragmentProxy returns the fragment proxy for remote contributors.
func (e *Extension) FragmentProxy() *proxy.FragmentProxy {
	return e.fragmentProxy
}

// RecoveryManager returns the recovery manager for remote contributors.
func (e *Extension) RecoveryManager() *recovery.Manager {
	return e.recoveryMgr
}

// Searcher returns the federated search engine. Returns nil if search is disabled.
func (e *Extension) Searcher() *search.FederatedSearch {
	return e.searcher
}

// SettingsAggregator returns the settings aggregator. Returns nil if settings is disabled.
func (e *Extension) SettingsAggregator() *settings.Aggregator {
	return e.settingsAgg
}

// Sanitizer returns the HTML sanitizer for remote fragments.
func (e *Extension) Sanitizer() *security.Sanitizer {
	return e.sanitizer
}

// CSRFManager returns the CSRF token manager. Returns nil if CSRF is disabled.
func (e *Extension) CSRFManager() *security.CSRFManager {
	return e.csrfMgr
}

// ThemeManager returns the theme manager.
func (e *Extension) ThemeManager() *dashtheme.Manager {
	return e.themeMgr
}

// ForgeUIApp returns the forgeui application instance.
func (e *Extension) ForgeUIApp() *forgeui.App {
	return e.fuiApp
}

// SetAuthChecker configures the authentication checker used to validate
// requests. Call this after Register() and before Start(). When auth is enabled,
// the checker is invoked on every request to populate the user context.
//
// Example using the adapter for the forge auth extension:
//
//	checker := dashauth.NewAuthExtensionChecker(authRegistry, "oidc")
//	dashExt.SetAuthChecker(checker)
func (e *Extension) SetAuthChecker(checker dashauth.AuthChecker) {
	e.authChecker = checker
}

// AuthChecker returns the configured authentication checker. Returns nil if none is set.
func (e *Extension) AuthChecker() dashauth.AuthChecker {
	return e.authChecker
}

// SetAuthPageProvider configures the provider that contributes authentication
// pages (login, logout, register, etc.) to the dashboard. Call this after
// Register() and before Start().
func (e *Extension) SetAuthPageProvider(provider dashauth.AuthPageProvider) {
	e.authPageProv = provider
}

// AuthPageProvider returns the configured auth page provider. Returns nil if none is set.
func (e *Extension) AuthPageProvider() dashauth.AuthPageProvider {
	return e.authPageProv
}

// initializeForgeUI creates the forgeui.App instance and initializes the
// layout manager and pages manager. It must be called in Register() after
// the registry, collector, and history are initialized but before bridge setup.
func (e *Extension) initializeForgeUI() {
	lightTheme := theme.DefaultLight()
	darkTheme := theme.DefaultDark()

	// Configure Geist variable fonts with preload links.
	fontConfig := theme.GeistFontConfig(e.config.BasePath + "/static/fonts")

	fuiApp := forgeui.New(
		forgeui.WithBasePath(e.config.BasePath),
		forgeui.WithBridge(
			bridge.WithTimeout(15*time.Second),
			bridge.WithCSRF(false),
		),
		forgeui.WithThemes(&lightTheme, &darkTheme),
		forgeui.WithFonts(&fontConfig),
		forgeui.WithDefaultLayout(layouts.LayoutDashboard),
		// Asset configuration for compiled CSS
		forgeui.WithEmbedFS(dashassets.Assets),
		forgeui.WithAssetOutputDir("extensions/dashboard/assets"),
		forgeui.WithInputCSS("extensions/dashboard/assets/css/input.css"),
	)

	// Initialize ForgeUI — builds CSS from themes if Tailwind CLI is available.
	// Non-fatal: falls back to CDN mode if CLI not found.
	if err := fuiApp.Initialize(context.Background()); err != nil {
		e.Logger().Warn("forgeui initialization warning", forge.F("error", err.Error()))
	}

	e.fuiApp = fuiApp

	// Override ScriptURL so component Script() tags resolve to the dashboard's
	// static asset path. Components emit "/assets/js/X.min.js"; remap to
	// "{basePath}/static/js/X.min.js" which the forgeui asset handler serves.
	staticPrefix := e.config.BasePath + "/static"
	utils.ScriptURL = func(path string) string {
		p := strings.TrimPrefix(path, "/assets")
		return staticPrefix + p + "?v=" + utils.ScriptVersion
	}

	// Layout manager — registers all layouts with forgeui in its constructor.
	bridgeEndpoint := ""
	if e.config.EnableBridge {
		bridgeEndpoint = e.config.BasePath + "/bridge/call"
	}

	e.layoutMgr = layouts.NewLayoutManager(fuiApp, e.config.BasePath, e.registry, layouts.LayoutConfig{
		Title:          e.config.Title,
		CustomCSS:      e.config.CustomCSS,
		BridgeEndpoint: bridgeEndpoint,
		EnableBridge:   e.config.EnableBridge,
		EnableSearch:   e.config.EnableSearch,
		EnableRealtime: e.config.EnableRealtime,
		EnableAuth:     e.config.EnableAuth,
		LoginPath:      e.config.LoginPath,
		LogoutPath:     e.config.LogoutPath,
	})

	// Pages manager — pages are registered later in registerRoutes().
	// Note: fragmentProxy may be nil at this point; it's set before Register() returns.
	e.pagesMgr = dashpages.NewPagesManager(fuiApp, e.config.BasePath, e.registry, e.collector, e.history, e.fragmentProxy, dashpages.PagesConfig{
		EnableSettings: e.config.EnableSettings,
		EnableSearch:   e.config.EnableSearch,
		BasePath:       e.config.BasePath,
		EnableAuth:     e.config.EnableAuth,
		DefaultAccess:  e.config.DefaultAccess,
		LoginPath:      e.config.LoginPath,
	})
}

// SetDiscoveryService configures the discovery service used to auto-discover
// remote dashboard contributors. Call this before Start() if discovery is enabled.
//
// The discovery service must implement dashboarddiscovery.DiscoveryService
// (ListServices + DiscoverWithTags). The forge extensions/discovery.Service
// type satisfies this interface.
//
// Example:
//
//	dashExt.SetDiscoveryService(discoveryExtension.Service())
func (e *Extension) SetDiscoveryService(svc dashboarddiscovery.DiscoveryService) {
	e.discoverySvc = svc
}

// startDiscoveryIntegration starts polling for dashboard contributors if a
// discovery service has been configured via SetDiscoveryService().
func (e *Extension) startDiscoveryIntegration(ctx context.Context) {
	if e.discoverySvc == nil {
		e.Logger().Warn("discovery integration: no discovery service configured, skipping",
			forge.F("hint", "call SetDiscoveryService() before Start()"),
		)

		return
	}

	e.discoveryInteg = dashboarddiscovery.NewIntegration(
		e.discoverySvc,
		e.registry,
		e.config.DiscoveryTag,
		e.config.DiscoveryPollInterval,
		e.config.ProxyTimeout,
		e.Logger(),
	)
	e.discoveryInteg.Start(ctx)

	e.Logger().Info("discovery integration started",
		forge.F("tag", e.config.DiscoveryTag),
		forge.F("poll_interval", e.config.DiscoveryPollInterval.String()),
	)
}

// registerRoutes registers dashboard routes with the app's router.
// ForgeUI handles HTML page routes (overview, health, metrics, services,
// contributor pages, widget fragments) via e.fuiApp.Handler().
// API, SSE, export, proxy, search, and settings routes remain on forge.Router.
func (e *Extension) registerRoutes() {
	router := e.app.Router()
	base := e.config.BasePath

	// 1. Layouts are registered by LayoutManager's constructor (NewLayoutManager).

	// 2. Register forgeui pages
	if err := e.pagesMgr.RegisterPages(); err != nil {
		panic(fmt.Sprintf("dashboard: failed to register pages: %v", err))
	}

	must := func(err error) {
		if err != nil {
			panic(fmt.Sprintf("dashboard: failed to register route: %v", err))
		}
	}

	// Register auth pages if a provider is configured
	if e.config.EnableAuth && e.authPageProv != nil {
		e.registerAuthPages()
	}

	// Build handler deps for API routes that remain on forge.Router
	deps := &handlers.Deps{
		Registry:  e.registry,
		Collector: e.collector,
		History:   e.history,
		Config: handlers.Config{
			BasePath:       e.config.BasePath,
			Title:          e.config.Title,
			Theme:          e.config.Theme,
			CustomCSS:      e.config.CustomCSS,
			EnableExport:   e.config.EnableExport,
			EnableRealtime: e.config.EnableRealtime,
			EnableSearch:   e.config.EnableSearch,
			EnableSettings: e.config.EnableSettings,
			EnableBridge:   e.config.EnableBridge,
			EnableAuth:     e.config.EnableAuth,
			ExportFormats:  e.config.ExportFormats,
		},
	}

	// 3. JSON API routes (stay on forge.Router)
	must(router.GET(base+"/api/overview", handlers.HandleAPIOverview(deps)))
	must(router.GET(base+"/api/health", handlers.HandleAPIHealth(deps)))
	must(router.GET(base+"/api/metrics", handlers.HandleAPIMetrics(deps)))
	must(router.GET(base+"/api/services", handlers.HandleAPIServices(deps)))
	must(router.GET(base+"/api/history", handlers.HandleAPIHistory(deps)))
	must(router.GET(base+"/api/service-detail", handlers.HandleAPIServiceDetail(deps)))
	must(router.GET(base+"/api/metrics-report", handlers.HandleAPIMetricsReport(deps)))

	// 4. Export endpoints (stay on forge.Router)
	if e.config.EnableExport {
		must(router.GET(base+"/export/json", handlers.HandleExportJSON(deps)))
		must(router.GET(base+"/export/csv", handlers.HandleExportCSV(deps)))
		must(router.GET(base+"/export/prometheus", handlers.HandleExportPrometheus(deps)))
	}

	// 5. SSE real-time event stream (stays on forge.Router)
	if e.config.EnableRealtime && e.sseBroker != nil {
		must(router.EventStream(base+"/sse", handlers.HandleSSEEndpoint(e.sseBroker)))
		must(router.GET(base+"/api/sse/status", handlers.HandleSSEStatus(e.sseBroker)))
	}

	// 6. Remote contributor proxy routes are now handled by forgeui via PagesManager.

	// 6b. Mount embedded contributor static assets.
	// EmbeddedContributors are registered as local contributors. We type-assert
	// to find them and mount their asset handlers.
	e.mountEmbeddedAssets(router, base, must)

	// 7. Search API endpoint (stays on forge.Router)
	if e.config.EnableSearch && e.searcher != nil {
		must(router.GET(base+"/api/search", search.HandleSearchAPI(e.searcher)))
	}

	// 8. Settings routes (stay on forge.Router -- they use forge.Handler signature)
	if e.config.EnableSettings && e.settingsAgg != nil {
		must(router.GET(base+"/settings", settings.HandleSettingsIndex(e.settingsAgg, e.registry, base)))
		must(router.GET(base+"/ext/:name/settings/:id", settings.HandleSettingsForm(e.registry, base)))
		must(router.POST(base+"/ext/:name/settings/:id", settings.HandleSettingsSubmit(e.registry, base)))
	}

	// 9. Mount forgeui handler (AFTER specific routes so they take precedence).
	// This catches all remaining HTML page requests and delegates to forgeui,
	// which also serves static assets, bridge endpoints at {basePath}/bridge/call
	// and {basePath}/bridge/stream/, and routed pages.
	// Note: No StripPrefix — forgeui's internal mux registers routes with the
	// basePath prefix, so full request paths must reach it unmodified.
	fuiHandler := func(ctx forge.Context) error {
		e.fuiApp.Handler().ServeHTTP(ctx.Response(), ctx.Request())
		return nil
	}

	// Build route options — attach auth middleware when enabled
	var routeOpts []forge.RouteOption
	if e.config.EnableAuth && e.authChecker != nil {
		routeOpts = append(routeOpts, forge.WithMiddleware(dashauth.ForgeMiddleware(e.authChecker)))
	}

	must(router.GET(base+"/*", fuiHandler, routeOpts...))
	must(router.POST(base+"/*", fuiHandler, routeOpts...))

	e.Logger().Debug("dashboard routes registered",
		forge.F("base_path", base),
		forge.F("realtime", e.config.EnableRealtime),
		forge.F("export", e.config.EnableExport),
		forge.F("search", e.config.EnableSearch),
		forge.F("settings", e.config.EnableSettings),
		forge.F("bridge", e.config.EnableBridge),
		forge.F("discovery", e.config.EnableDiscovery),
		forge.F("auth", e.config.EnableAuth),
		forge.F("core_pages", 4),
	)
}

// mountEmbeddedAssets iterates local contributors and mounts static asset handlers
// for any EmbeddedContributor instances. This allows embedded dashboard UIs to serve
// their CSS, JS, and image files.
func (e *Extension) mountEmbeddedAssets(router forge.Router, base string, must func(error)) {
	for _, name := range e.registry.ContributorNames() {
		lc, ok := e.registry.FindLocalContributor(name)
		if !ok {
			continue
		}

		ec, ok := lc.(*contributor.EmbeddedContributor)
		if !ok {
			continue
		}

		assetsPrefix := base + "/ext/" + name + "/assets/"
		handler := ec.AssetsHandler()
		assetHandler := func(ctx forge.Context) error {
			http.StripPrefix(assetsPrefix, handler).ServeHTTP(ctx.Response(), ctx.Request())

			return nil
		}
		must(router.GET(assetsPrefix+"*", assetHandler))

		e.Logger().Debug("mounted embedded contributor assets",
			forge.F("contributor", name),
			forge.F("path", assetsPrefix),
		)
	}
}

// registerAuthPages registers auth page routes (login, logout, register, etc.)
// with ForgeUI using the LayoutAuth layout. Auth pages are always AccessPublic
// so unauthenticated users can reach the login page.
func (e *Extension) registerAuthPages() {
	if e.authPageProv == nil {
		return
	}

	pages := e.authPageProv.AuthPages()
	if len(pages) == 0 {
		return
	}

	provider := e.authPageProv

	for _, page := range pages {
		pageType := page.Type
		pagePath := "/auth" + page.Path

		// GET handler — render the auth page form
		getHandler := func(ctx *router.PageContext) (templ.Component, error) {
			return provider.RenderAuthPage(ctx, pageType)
		}

		// POST handler — handle form submission
		postHandler := func(ctx *router.PageContext) (templ.Component, error) {
			redirectURL, errComponent, err := provider.HandleAuthAction(ctx, pageType)
			if err != nil {
				return nil, err
			}

			if redirectURL != "" {
				http.Redirect(ctx.ResponseWriter, ctx.Request, redirectURL, http.StatusFound)

				return templ.Raw(""), nil
			}

			if errComponent != nil {
				return errComponent, nil
			}

			// Fallback: re-render the page
			return provider.RenderAuthPage(ctx, pageType)
		}

		// Register GET and POST with LayoutAuth layout
		e.fuiApp.Page(pagePath).
			Handler(getHandler).
			Layout(layouts.LayoutAuth).
			Register()

		e.fuiApp.Page(pagePath).
			Handler(postHandler).
			Method("POST").
			Layout(layouts.LayoutAuth).
			Register()
	}

	e.Logger().Debug("auth pages registered",
		forge.F("count", len(pages)),
	)
}
