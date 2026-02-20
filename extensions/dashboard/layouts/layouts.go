package layouts

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/alpine"
	"github.com/xraph/forgeui/bridge"
	"github.com/xraph/forgeui/components/sidebar"
	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/layout"
	"github.com/xraph/forgeui/router"
	"github.com/xraph/forgeui/theme"

	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/forge/extensions/dashboard/ui/shell"
)

// Layout name constants used for registration and route assignment.
const (
	LayoutRoot      = "root"
	LayoutDashboard = "dashboard"
	LayoutSettings  = "settings"
	LayoutBase      = "base"
	LayoutFull      = "full"
	LayoutAuth      = "auth"
)

// LayoutConfig holds configuration for all dashboard layouts.
type LayoutConfig struct {
	Title          string
	CustomCSS      string
	BridgeEndpoint string
	EnableBridge   bool
	EnableSearch   bool
	EnableRealtime bool
	EnableAuth     bool
	LoginPath      string
	LogoutPath     string
}

// LayoutManager registers and manages all dashboard layouts with forgeui.
type LayoutManager struct {
	fuiApp   *forgeui.App
	basePath string
	registry *contributor.ContributorRegistry
	config   LayoutConfig
}

// NewLayoutManager creates a LayoutManager and registers all five layouts
// with the forgeui application. The "dashboard" layout is set as the default.
func NewLayoutManager(fuiApp *forgeui.App, basePath string, registry *contributor.ContributorRegistry, config LayoutConfig) *LayoutManager {
	lm := &LayoutManager{
		fuiApp:   fuiApp,
		basePath: basePath,
		registry: registry,
		config:   config,
	}

	lm.registerLayouts()

	return lm
}

// registerLayouts registers all five layouts with the forgeui application.
//
// Hierarchy:
//
//	root (full HTML document)
//	  dashboard (sidebar + topbar + content) [default]
//	    settings (adds settings sub-nav)
//	  base (topbar only, no sidebar)
//	  full (no chrome, just content)
func (lm *LayoutManager) registerLayouts() {
	// Root layout: full HTML document shell.
	lm.fuiApp.RegisterLayout(LayoutRoot, lm.rootLayout)

	// Dashboard layout: sidebar + topbar + main content area.
	// Parent is root so it gets wrapped in the HTML document.
	lm.fuiApp.RegisterLayout(LayoutDashboard, lm.dashboardLayout,
		router.WithParentLayout(LayoutRoot),
	)

	// Settings layout: dashboard sidebar + settings sub-navigation panel.
	// Parent is dashboard so it inherits the sidebar chrome.
	lm.fuiApp.RegisterLayout(LayoutSettings, lm.settingsLayout,
		router.WithParentLayout(LayoutDashboard),
	)

	// Base layout: minimal topbar + content, no sidebar.
	// Parent is root so it gets the HTML document wrapper.
	lm.fuiApp.RegisterLayout(LayoutBase, lm.baseLayout,
		router.WithParentLayout(LayoutRoot),
	)

	// Full layout: no chrome, just content in a minimal wrapper.
	// Parent is root so it gets the HTML document wrapper.
	lm.fuiApp.RegisterLayout(LayoutFull, lm.fullLayout,
		router.WithParentLayout(LayoutRoot),
	)

	// Auth layout: centered card for login/register pages, no sidebar/topbar.
	// Parent is root so it gets the HTML document wrapper.
	lm.fuiApp.RegisterLayout(LayoutAuth, lm.authLayout,
		router.WithParentLayout(LayoutRoot),
	)
}

// ---------------------------------------------------------------------------
// Root Layout
// ---------------------------------------------------------------------------

// rootLayout renders the full HTML document shell. It is the ONLY layout that
// produces a complete <!DOCTYPE html> document.
//
// HTMX optimization: when the HX-Request header is present and HX-Boosted is
// NOT present, this is a partial HTMX navigation request. In that case the
// full HTML shell is skipped and only the inner content is returned so that
// HTMX can swap it into the existing page.
func (lm *LayoutManager) rootLayout(ctx *router.PageContext, content g.Node) g.Node {
	// Detect HTMX partial request.
	isHTMX := ctx.Request.Header.Get("Hx-Request") != ""
	isBoosted := ctx.Request.Header.Get("Hx-Boosted") != ""

	if isHTMX && !isBoosted {
		// Partial navigation: return only the content fragment.
		return content
	}

	lightTheme := theme.DefaultLight()
	darkTheme := theme.DefaultDark()

	return layout.Build(
		// <head>
		layout.Head(
			// Theme CSS variables, meta tags, and Tailwind config.
			theme.HeadContent(lightTheme, darkTheme),
			theme.StyleTag(lightTheme, darkTheme),
			theme.TailwindConfigScript(),

			// Page title.
			layout.Title(lm.config.Title),

			// Viewport.
			layout.Viewport("width=device-width, initial-scale=1"),

			// Dark mode flash prevention.
			theme.DarkModeScript(),

			// Alpine.js cloak CSS (hides [x-cloak] until Alpine loads).
			alpine.CloakCSS(),

			// Tailwind CDN.
			layout.Script("https://cdn.tailwindcss.com"),

			// HTMX CDN.
			shell.HTMXScript(),

			// Bridge client scripts (conditional).
			lm.bridgeHeadScripts(),

			// Custom CSS (conditional).
			g.If(lm.config.CustomCSS != "",
				html.StyleEl(g.Raw(lm.config.CustomCSS)),
			),

			// Dashboard helper scripts (formatDuration, formatBytes, etc.).
			shell.HelperScripts(""),
		),

		// <body>
		layout.Body(
			html.Class("min-h-screen bg-background text-foreground antialiased"),

			// Alpine store for dashboard state (sidebar, notifications, etc.).
			shell.DashboardStoreScript(lm.basePath, ""),

			// HTMX configuration (head-support, afterSettle, etc.).
			shell.HTMXConfigScript(""),

			// Auth redirect script (handles HTMX 401 → login redirect).
			g.If(lm.config.EnableAuth, shell.AuthRedirectScript("")),

			// The inner layout content.
			content,

			// Alpine.js (deferred, loaded last).
			alpine.Scripts(alpine.PluginCollapse, alpine.PluginMorph),
		),
	)
}

// bridgeHeadScripts returns bridge client scripts if bridge is enabled,
// or an empty node group otherwise.
func (lm *LayoutManager) bridgeHeadScripts() g.Node {
	if !lm.config.EnableBridge || lm.config.BridgeEndpoint == "" {
		return g.Group(nil)
	}

	return bridge.BridgeScripts(bridge.ScriptConfig{
		Endpoint:      lm.config.BridgeEndpoint,
		IncludeAlpine: true,
	})
}

// ---------------------------------------------------------------------------
// Dashboard Layout
// ---------------------------------------------------------------------------

// dashboardLayout renders the primary dashboard chrome: a collapsible sidebar
// with navigation groups on the left and a SidebarInset (topbar + content
// area) on the right.
func (lm *LayoutManager) dashboardLayout(ctx *router.PageContext, content g.Node) g.Node {
	groups := lm.registry.GetNavGroups()
	activePath := ctx.Request.URL.Path

	return sidebar.SidebarLayout(
		// Left sidebar with navigation.
		lm.buildSidebar(groups, activePath),

		// Right inset: topbar + content.
		sidebar.SidebarInset(
			// Topbar header.
			sidebar.SidebarInsetHeader(
				// Mobile sidebar trigger.
				sidebar.SidebarTrigger(),

				// Desktop sidebar trigger.
				sidebar.SidebarTriggerDesktop(),

				// Vertical separator.
				html.Div(html.Class("mx-2 h-4 w-px bg-border hidden md:block")),

				// Breadcrumbs (fills remaining space).
				html.Div(
					html.Class("flex-1"),
					lm.buildBreadcrumbs(ctx),
				),

				// Right-side action buttons.
				html.Div(
					html.Class("flex items-center gap-2"),
					lm.searchTrigger(),
					lm.connectionIndicator(),
					lm.notificationBell(),
					lm.themeToggle(),
					lm.userMenu(ctx),
				),
			),

			// Main content area with HTMX target.
			html.Main(
				html.ID("content"),
				html.Class("flex-1 overflow-y-auto p-4 md:p-6"),
				content,
			),
		),
	)
}

// ---------------------------------------------------------------------------
// Settings Layout
// ---------------------------------------------------------------------------

// settingsLayout wraps content with a settings sub-navigation panel. Because
// its parent is LayoutDashboard it already has the sidebar and topbar; this
// layout replaces only the content portion of the dashboard layout with a
// two-column flex: settings sub-nav on the left and content on the right.
func (lm *LayoutManager) settingsLayout(ctx *router.PageContext, content g.Node) g.Node {
	activePath := ctx.Request.URL.Path

	return html.Div(
		html.Class("flex flex-1 overflow-hidden"),

		// Settings sub-navigation sidebar.
		html.Aside(
			html.Class("hidden md:flex w-56 flex-col border-r bg-muted/30 p-4 overflow-y-auto"),
			lm.settingsSubNav(activePath),
		),

		// Settings content area.
		html.Main(
			html.ID("content"),
			html.Class("flex-1 overflow-y-auto p-6 max-w-4xl"),
			content,
		),
	)
}

// settingsSubNav renders the settings sub-navigation sidebar with items
// sourced from the contributor registry.
func (lm *LayoutManager) settingsSubNav(activePath string) g.Node {
	items := []g.Node{
		html.H3(
			html.Class("text-sm font-semibold text-muted-foreground mb-4 px-2"),
			g.Text("Settings"),
		),
	}

	// "General" settings entry.
	settingsBase := lm.basePath + "/settings"
	items = append(items, lm.settingsNavItem("General", settingsBase, "settings",
		activePath == settingsBase || activePath == settingsBase+"/"))

	// Settings items from contributors.
	allSettings := lm.registry.GetAllSettings()
	for _, s := range allSettings {
		settingPath := settingsBase + "/" + s.ID
		items = append(items, lm.settingsNavItem(
			s.Title, settingPath, s.Icon,
			isActivePath(settingPath, activePath, lm.basePath),
		))
	}

	return html.Nav(
		html.Class("space-y-1"),
		g.Group(items),
	)
}

// settingsNavItem renders a single settings navigation item with HTMX attrs.
func (lm *LayoutManager) settingsNavItem(label, path, icon string, active bool) g.Node {
	activeClass := "bg-accent text-accent-foreground font-medium"
	inactiveClass := "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"

	cssClass := "flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors"
	if active {
		cssClass += " " + activeClass
	} else {
		cssClass += " " + inactiveClass
	}

	children := []g.Node{
		html.Class(cssClass),
		html.Href(path),
		g.Attr("hx-get", path),
		g.Attr("hx-target", "#content"),
		g.Attr("hx-swap", "innerHTML"),
		g.Attr("hx-push-url", "true"),
	}

	if icon != "" {
		children = append(children, navIcon(icon))
	}

	children = append(children, html.Span(g.Text(label)))

	return html.A(children...)
}

// ---------------------------------------------------------------------------
// Base Layout
// ---------------------------------------------------------------------------

// baseLayout renders a minimal layout: topbar with brand link and
// breadcrumbs, followed by the main content area. No sidebar.
func (lm *LayoutManager) baseLayout(ctx *router.PageContext, content g.Node) g.Node {
	return html.Div(
		html.Class("flex min-h-screen flex-col"),

		// Topbar (no sidebar triggers).
		html.Header(
			html.Class("sticky top-0 z-40 flex h-14 items-center gap-4 border-b bg-background/95 px-4 md:px-6 backdrop-blur supports-[backdrop-filter]:bg-background/60"),

			// Brand logo / link.
			html.A(
				html.Href(lm.basePath),
				html.Class("flex items-center gap-2 text-foreground hover:text-primary transition-colors"),
				g.Attr("hx-get", lm.basePath),
				g.Attr("hx-target", "#content"),
				g.Attr("hx-swap", "innerHTML"),
				g.Attr("hx-push-url", "true"),
				icons.LayoutDashboard(icons.WithSize(20), icons.WithClass("text-primary")),
				html.Span(html.Class("font-semibold"), g.Text("Forge")),
			),

			// Breadcrumbs.
			html.Div(
				html.Class("flex-1"),
				lm.buildBreadcrumbs(ctx),
			),

			// Right-side actions.
			html.Div(
				html.Class("flex items-center gap-2"),
				lm.searchTrigger(),
				lm.themeToggle(),
			),
		),

		// Content area.
		html.Main(
			html.ID("content"),
			html.Class("flex-1 p-4 md:p-6"),
			content,
		),
	)
}

// ---------------------------------------------------------------------------
// Full Layout
// ---------------------------------------------------------------------------

// fullLayout renders content with no chrome at all -- just a minimal
// background wrapper and an HTMX-targetable main element.
func (lm *LayoutManager) fullLayout(_ *router.PageContext, content g.Node) g.Node {
	return html.Div(
		html.Class("min-h-screen bg-background text-foreground"),
		html.Main(
			html.ID("content"),
			content,
		),
	)
}

// ---------------------------------------------------------------------------
// Sidebar builder
// ---------------------------------------------------------------------------

// buildSidebar constructs the full forgeui collapsible sidebar component from
// the contributor nav groups. This mirrors the pattern in shell.buildForgeUISidebar
// but sources data from the LayoutManager directly.
func (lm *LayoutManager) buildSidebar(groups []contributor.NavGroup, activePath string) g.Node {
	return sidebar.SidebarWithOptions(
		[]sidebar.SidebarOption{
			sidebar.WithCollapsible(true),
			sidebar.WithCollapsibleMode(sidebar.CollapsibleOffcanvas),
			sidebar.WithKeyboardShortcut("b"),
			sidebar.WithPersistState(true),
			sidebar.WithStorageKey("forge_dashboard_sidebar"),
		},

		// Header with brand link.
		sidebar.SidebarHeader(
			html.A(
				html.Href(lm.basePath),
				html.Class("flex items-center gap-2 text-sidebar-foreground hover:text-sidebar-primary transition-colors"),
				g.Attr("hx-get", lm.basePath),
				g.Attr("hx-target", "#content"),
				g.Attr("hx-swap", "innerHTML"),
				g.Attr("hx-push-url", "true"),
				icons.LayoutDashboard(icons.WithSize(22), icons.WithClass("text-sidebar-primary")),
				html.Span(
					html.Class("text-lg font-semibold"),
					g.Attr("x-show", "$store.sidebar && (!$store.sidebar.collapsed || $store.sidebar.isMobile)"),
					g.Text("Forge"),
				),
			),
		),

		// Toggle button on sidebar edge.
		sidebar.SidebarToggle(),

		// Rail for hover-to-expand.
		sidebar.SidebarRail(),

		// Navigation content.
		sidebar.SidebarContent(
			lm.buildSidebarNavGroups(groups, activePath)...,
		),

		// Footer.
		sidebar.SidebarFooter(
			html.Div(
				html.Class("flex items-center gap-2 text-xs text-sidebar-foreground/50"),
				icons.Settings(icons.WithSize(14)),
				html.Span(
					g.Attr("x-show", "$store.sidebar && (!$store.sidebar.collapsed || $store.sidebar.isMobile)"),
					g.Text("Forge Dashboard v3"),
				),
			),
		),
	)
}

// buildSidebarNavGroups converts contributor NavGroups into forgeui sidebar
// collapsible group components.
func (lm *LayoutManager) buildSidebarNavGroups(groups []contributor.NavGroup, activePath string) []g.Node {
	nodes := make([]g.Node, 0, len(groups))

	for _, group := range groups {
		groupKey := sanitizeAlpineKey(group.Name)

		items := make([]g.Node, 0, len(group.Items))
		for _, item := range group.Items {
			items = append(items, lm.buildSidebarMenuItem(item, activePath))
		}

		nodes = append(nodes,
			sidebar.SidebarGroupCollapsible(
				[]sidebar.SidebarGroupOption{
					sidebar.WithGroupKey(groupKey),
					sidebar.WithGroupDefaultOpen(true),
				},
				sidebar.SidebarGroupLabelCollapsible(groupKey, group.Name, nil),
				sidebar.SidebarGroupContent(groupKey,
					sidebar.SidebarMenu(items...),
				),
			),
		)
	}

	return nodes
}

// buildSidebarMenuItem creates a sidebar menu button with HTMX navigation.
func (lm *LayoutManager) buildSidebarMenuItem(item contributor.ResolvedNav, activePath string) g.Node {
	active := isActivePath(item.FullPath, activePath, lm.basePath)

	opts := []sidebar.SidebarMenuButtonOption{
		sidebar.WithMenuHref(item.FullPath),
		sidebar.WithActive(active),
		sidebar.WithMenuTooltip(item.Label),
		sidebar.WithMenuAttrs(
			g.Attr("hx-get", item.FullPath),
			g.Attr("hx-target", "#content"),
			g.Attr("hx-swap", "innerHTML"),
			g.Attr("hx-push-url", "true"),
		),
	}

	if item.Icon != "" {
		opts = append(opts, sidebar.WithMenuIcon(navIcon(item.Icon)))
	}

	if item.Badge != "" {
		opts = append(opts, sidebar.WithMenuBadge(
			sidebar.SidebarMenuBadge(item.Badge),
		))
	}

	return sidebar.SidebarMenuItem(
		sidebar.SidebarMenuButton(item.Label, opts...),
	)
}

// ---------------------------------------------------------------------------
// Topbar action components
// ---------------------------------------------------------------------------

// searchTrigger renders the Cmd+K search button.
func (lm *LayoutManager) searchTrigger() g.Node {
	if !lm.config.EnableSearch {
		return g.Group(nil)
	}

	return html.Button(
		html.Class("inline-flex items-center justify-start gap-2 whitespace-nowrap rounded-md border border-input bg-background px-3 py-1.5 text-sm text-muted-foreground shadow-sm hover:bg-accent hover:text-accent-foreground w-auto md:w-48"),
		g.Attr("aria-label", "Search"),
		icons.Search(icons.WithSize(16)),
		html.Span(
			html.Class("hidden md:inline-block text-sm text-muted-foreground"),
			g.Text("Search..."),
		),
		html.Kbd(
			html.Class("pointer-events-none hidden h-5 select-none items-center gap-1 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium opacity-100 md:inline-flex"),
			g.Text("\u2318K"),
		),
	)
}

// connectionIndicator shows SSE/realtime connection status via Alpine.
func (lm *LayoutManager) connectionIndicator() g.Node {
	if !lm.config.EnableRealtime {
		return g.Group(nil)
	}

	return html.Div(
		html.Class("flex items-center gap-1.5"),
		g.Attr("x-data", ""),
		html.Div(
			html.Class("h-2 w-2 rounded-full"),
			g.Attr(":class", "$store.dashboard.connected ? 'bg-green-500' : 'bg-muted-foreground/30'"),
		),
		html.Span(
			html.Class("hidden sm:inline-block text-xs text-muted-foreground"),
			g.Attr("x-text", "$store.dashboard.connected ? 'Live' : ''"),
		),
	)
}

// notificationBell renders the notification bell icon with unread count badge.
func (lm *LayoutManager) notificationBell() g.Node {
	return html.Div(
		g.Attr("x-data", ""),
		html.Class("relative"),
		html.Button(
			html.Class("inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground h-9 w-9"),
			g.Attr("aria-label", "Notifications"),
			icons.Bell(icons.WithSize(18)),
		),
		// Unread count badge.
		html.Span(
			html.Class("absolute -top-1 -right-1 flex h-4 w-4 items-center justify-center rounded-full bg-destructive text-[10px] font-bold text-destructive-foreground"),
			g.Attr("x-show", "$store.dashboard.unreadCount > 0"),
			g.Attr("x-text", "$store.dashboard.unreadCount"),
			g.Attr("x-cloak", ""),
		),
	)
}

// themeToggle renders a dark/light mode toggle button using Alpine.js.
func (lm *LayoutManager) themeToggle() g.Node {
	return html.Div(
		g.Attr("x-data", `{
			isDark: document.documentElement.classList.contains('dark'),
			toggle() {
				this.isDark = !this.isDark;
				document.documentElement.classList.toggle('dark', this.isDark);
				localStorage.setItem('theme', this.isDark ? 'dark' : 'light');
			}
		}`),
		html.Button(
			html.Class("inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground h-9 w-9"),
			g.Attr("@click", "toggle()"),
			g.Attr("aria-label", "Toggle theme"),
			html.Span(
				g.Attr("x-show", "!isDark"),
				icons.Moon(icons.WithSize(18)),
			),
			html.Span(
				g.Attr("x-show", "isDark"),
				g.Attr("x-cloak", ""),
				icons.Sun(icons.WithSize(18)),
			),
		),
	)
}

// ---------------------------------------------------------------------------
// Auth Layout
// ---------------------------------------------------------------------------

// authLayout renders a centered card layout for authentication pages (login,
// register, etc.). No sidebar or topbar — just a clean centered container
// with a brand link and theme toggle.
func (lm *LayoutManager) authLayout(_ *router.PageContext, content g.Node) g.Node {
	return html.Div(
		html.Class("flex min-h-screen flex-col items-center justify-center bg-muted/30 p-4"),

		// Brand link at the top
		html.A(
			html.Href(lm.basePath),
			html.Class("mb-8 flex items-center gap-2 text-foreground hover:text-primary transition-colors"),
			icons.LayoutDashboard(icons.WithSize(28), icons.WithClass("text-primary")),
			html.Span(html.Class("text-2xl font-bold"), g.Text("Forge")),
		),

		// Centered card container
		html.Div(
			html.Class("w-full max-w-md"),
			html.Div(
				html.Class("rounded-lg border bg-card text-card-foreground shadow-sm"),
				html.Main(
					html.ID("content"),
					html.Class("p-6"),
					content,
				),
			),
		),

		// Theme toggle at the bottom
		html.Div(
			html.Class("mt-6"),
			lm.themeToggle(),
		),
	)
}

// ---------------------------------------------------------------------------
// User Menu
// ---------------------------------------------------------------------------

// userMenu renders an auth-aware user dropdown in the topbar.
// When authenticated: shows user avatar/initials + dropdown with sign-out.
// When not authenticated + auth enabled: shows a "Sign in" link.
// When auth is disabled: renders nothing.
func (lm *LayoutManager) userMenu(ctx *router.PageContext) g.Node {
	if !lm.config.EnableAuth {
		return g.Group(nil)
	}

	user := dashauth.UserFromContext(ctx.Context())

	if !user.Authenticated() {
		// Unauthenticated — show "Sign in" link
		loginPath := lm.basePath + lm.config.LoginPath

		return html.A(
			html.Href(loginPath),
			html.Class("inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"),
			g.Attr("hx-get", loginPath),
			g.Attr("hx-target", "#content"),
			g.Attr("hx-swap", "innerHTML"),
			g.Attr("hx-push-url", "true"),
			icons.User(icons.WithSize(16)),
			html.Span(html.Class("hidden sm:inline-block"), g.Text("Sign in")),
		)
	}

	// Authenticated — show user avatar/initials with dropdown
	logoutPath := lm.basePath + lm.config.LogoutPath

	return html.Div(
		g.Attr("x-data", "{ open: false }"),
		html.Class("relative"),

		// Avatar button trigger
		html.Button(
			html.Class("inline-flex items-center justify-center rounded-full h-8 w-8 bg-primary text-primary-foreground text-xs font-semibold hover:opacity-90 transition-opacity"),
			g.Attr("@click", "open = !open"),
			g.Attr("@click.outside", "open = false"),
			g.Attr("aria-label", "User menu"),
			g.If(user.AvatarURL != "",
				html.Img(
					html.Class("h-8 w-8 rounded-full object-cover"),
					g.Attr("src", user.AvatarURL),
					g.Attr("alt", user.DisplayName),
				),
			),
			g.If(user.AvatarURL == "",
				g.Text(user.Initials()),
			),
		),

		// Dropdown menu
		html.Div(
			html.Class("absolute right-0 mt-2 w-56 rounded-md border bg-popover text-popover-foreground shadow-lg z-50"),
			g.Attr("x-show", "open"),
			g.Attr("x-cloak", ""),
			g.Attr("x-transition:enter", "transition ease-out duration-100"),
			g.Attr("x-transition:enter-start", "transform opacity-0 scale-95"),
			g.Attr("x-transition:enter-end", "transform opacity-100 scale-100"),
			g.Attr("x-transition:leave", "transition ease-in duration-75"),
			g.Attr("x-transition:leave-start", "transform opacity-100 scale-100"),
			g.Attr("x-transition:leave-end", "transform opacity-0 scale-95"),

			// User info header
			html.Div(
				html.Class("px-4 py-3 border-b"),
				html.P(html.Class("text-sm font-medium"), g.Text(user.DisplayName)),
				g.If(user.Email != "",
					html.P(html.Class("text-xs text-muted-foreground"), g.Text(user.Email)),
				),
			),

			// Menu items
			html.Div(
				html.Class("py-1"),
				// Sign out link
				html.A(
					html.Href(logoutPath),
					html.Class("flex items-center gap-2 px-4 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"),
					icons.LogOut(icons.WithSize(14)),
					g.Text("Sign out"),
				),
			),
		),
	)
}

// ---------------------------------------------------------------------------
// Breadcrumb builder
// ---------------------------------------------------------------------------

// buildBreadcrumbs constructs breadcrumbs from the current request path.
// It produces a simple path-based breadcrumb trail rooted at "Dashboard".
func (lm *LayoutManager) buildBreadcrumbs(ctx *router.PageContext) g.Node {
	crumbs := []shell.Breadcrumb{
		{Label: "Dashboard", Path: ""},
	}

	return shell.Breadcrumbs(crumbs, lm.basePath)
}

// ---------------------------------------------------------------------------
// Helpers (copied from shell package to avoid export churn)
// ---------------------------------------------------------------------------

// isActivePath determines if a nav item path should be highlighted as active.
func isActivePath(itemPath, activePath, basePath string) bool {
	if activePath == "" || itemPath == "" {
		return false
	}

	// Exact match.
	if itemPath == activePath {
		return true
	}

	// Dashboard root: exact match only.
	if itemPath == basePath || itemPath == basePath+"/" {
		return activePath == basePath || activePath == basePath+"/"
	}

	// Prefix match for sub-pages.
	return len(activePath) > len(itemPath) &&
		(activePath[len(itemPath)] == '/' || activePath[len(itemPath)] == '?') &&
		activePath[:len(itemPath)] == itemPath
}

// sanitizeAlpineKey converts a group name to a valid Alpine.js variable name.
func sanitizeAlpineKey(name string) string {
	result := make([]byte, 0, len(name))
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			result = append(result, byte(c))
		}
	}

	if len(result) == 0 {
		return "group"
	}

	// Ensure the key does not start with a digit.
	if result[0] >= '0' && result[0] <= '9' {
		result = append([]byte{'g'}, result...)
	}

	return string(result)
}

// navIcon maps an icon name string to a gomponents icon node.
func navIcon(name string) g.Node {
	size := icons.WithSize(18)

	switch name {
	case "home":
		return icons.Home(size)
	case "heart-pulse":
		return icons.HeartPulse(size)
	case "bar-chart", "chart-bar":
		return icons.ChartBar(size)
	case "chart-column":
		return icons.ChartColumn(size)
	case "chart-line":
		return icons.ChartLine(size)
	case "bar-chart-3":
		return icons.ChartBar(size)
	case "server":
		return icons.Server(size)
	case "settings":
		return icons.Settings(size)
	case "search":
		return icons.Search(size)
	case "bell":
		return icons.Bell(size)
	case "layout-dashboard":
		return icons.LayoutDashboard(size)
	case "layout-template":
		return icons.LayoutTemplate(size)
	case "maximize":
		return icons.Maximize(size)
	case "activity":
		return icons.Activity(size)
	case "menu":
		return icons.Menu(size)
	case "user", "users":
		return icons.User(size)
	case "shield":
		return icons.Shield(size)
	case "log-out", "logout":
		return icons.LogOut(size)
	case "log-in", "login":
		return icons.LogIn(size)
	case "key":
		return icons.Key(size)
	case "lock":
		return icons.Lock(size)
	case "credit-card":
		return icons.CreditCard(size)
	case "file-text":
		return icons.FileText(size)
	case "package":
		return icons.Box(size)
	default:
		return html.Span(
			html.Class("inline-flex h-[18px] w-[18px] items-center justify-center text-xs"),
			g.Text("\u2022"),
		)
	}
}
