package layouts

import (
	"github.com/a-h/templ"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/router"

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
	LayoutExtension = "extension"
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
	fuiApp        *forgeui.App
	basePath      string
	registry      *contributor.ContributorRegistry
	config        LayoutConfig
	footerActions []shell.UserDropdownAction
}

// NewLayoutManager creates a LayoutManager and registers all layouts
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

// registerLayouts registers all layouts with the forgeui application.
//
// Hierarchy:
//
//	root (full HTML document)
//	  dashboard (sidebar + topbar + content) [default]
//	    settings (adds settings sub-nav)
//	  base (topbar only, no sidebar)
//	  full (no chrome, just content)
//	  auth (centered card for login/register)
//	  extension (customizable topbar + app grid, no sidebar)
func (lm *LayoutManager) registerLayouts() {
	// Root layout: full HTML document shell.
	lm.fuiApp.RegisterLayout(LayoutRoot, func(ctx *router.PageContext, content templ.Component) templ.Component {
		return RootLayoutTempl(lm, ctx, content)
	})

	// Dashboard layout: sidebar + topbar + main content area.
	lm.fuiApp.RegisterLayout(LayoutDashboard, func(ctx *router.PageContext, content templ.Component) templ.Component {
		return DashboardLayoutTempl(lm, ctx, content)
	}, router.WithParentLayout(LayoutRoot))

	// Settings layout: dashboard sidebar + settings sub-navigation panel.
	lm.fuiApp.RegisterLayout(LayoutSettings, func(ctx *router.PageContext, content templ.Component) templ.Component {
		return SettingsLayoutTempl(lm, ctx, content)
	}, router.WithParentLayout(LayoutDashboard))

	// Base layout: minimal topbar + content, no sidebar.
	lm.fuiApp.RegisterLayout(LayoutBase, func(ctx *router.PageContext, content templ.Component) templ.Component {
		return BaseLayoutTempl(lm, ctx, content)
	}, router.WithParentLayout(LayoutRoot))

	// Full layout: no chrome, just content in a minimal wrapper.
	lm.fuiApp.RegisterLayout(LayoutFull, func(_ *router.PageContext, content templ.Component) templ.Component {
		return FullLayoutTempl(content)
	}, router.WithParentLayout(LayoutRoot))

	// Auth layout: centered card for login/register pages.
	lm.fuiApp.RegisterLayout(LayoutAuth, func(_ *router.PageContext, content templ.Component) templ.Component {
		return AuthLayoutTempl(lm, content)
	}, router.WithParentLayout(LayoutRoot))

	// Extension layout: customizable topbar + app grid navigator, no sidebar.
	// Used by extensions that need their own standalone UI.
	lm.fuiApp.RegisterLayout(LayoutExtension, func(ctx *router.PageContext, content templ.Component) templ.Component {
		return ExtensionLayoutTempl(lm, ctx, content)
	}, router.WithParentLayout(LayoutRoot))
}

// SetAuthEnabled updates the auth configuration at runtime. This is used
// for late auth registration when an auth provider registers after the
// layout manager has already been constructed.
func (lm *LayoutManager) SetAuthEnabled(enabled bool, loginPath, logoutPath string) {
	lm.config.EnableAuth = enabled
	lm.config.LoginPath = loginPath
	lm.config.LogoutPath = logoutPath
}

// SetFooterActions configures the user dropdown actions contributed by extensions
// implementing DashboardFooterContributor. Called during dashboard Start() after
// extension discovery.
func (lm *LayoutManager) SetFooterActions(actions []shell.UserDropdownAction) {
	lm.footerActions = actions
}

// isPartialRequest returns true when the request is an HTMX partial navigation
// (HX-Request is set but HX-Boosted is not). Layouts should skip their chrome
// and return only the inner content so HTMX can swap it into #content.
func (lm *LayoutManager) isPartialRequest(ctx *router.PageContext) bool {
	isHTMX := ctx.Request.Header.Get("Hx-Request") != ""
	isBoosted := ctx.Request.Header.Get("Hx-Boosted") != ""

	return isHTMX && !isBoosted
}

// cssPath returns the URL path to the compiled CSS stylesheet.
func (lm *LayoutManager) cssPath() string {
	return lm.fuiApp.CSSPath()
}

// isCDNMode returns true when compiled CSS is not available (CDN fallback).
// Always false: ForgeUI serves pre-built CSS from embedded assets regardless
// of whether the runtime Tailwind CLI build succeeds. CDN mode uses Tailwind v3
// which is incompatible with the v4-based theme system (breaks dark/light toggle).
func (lm *LayoutManager) isCDNMode() bool {
	return false
}

// fontPreloadLinks returns a templ.Component that renders <link rel="preload">
// tags for configured fonts. Returns a nop component if no fonts are configured.
func (lm *LayoutManager) fontPreloadLinks() templ.Component {
	return lm.fuiApp.FontPreloadLinks()
}
