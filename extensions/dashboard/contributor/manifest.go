package contributor

import "github.com/a-h/templ"

// Manifest describes a contributor's capabilities, navigation, widgets, settings, and more.
type Manifest struct {
	// IconComponent is a custom templ.Component icon provided by DashboardIconProvider.
	// Takes priority over Icon and ExtensionIcon when rendering in the app grid,
	// sidebar header, and topbar branding.
	IconComponent templ.Component `json:"-"`

	Name            string               `json:"name"`
	DisplayName     string               `json:"display_name"`
	Icon            string               `json:"icon"`
	Version         string               `json:"version"`
	Layout          string               `json:"layout,omitempty"`       // "dashboard", "settings", "base", "full", "extension" — default "dashboard"
	Root            bool                 `json:"root,omitempty"`         // true = nav paths resolve at dashboard root (e.g. /health), not /ext/{name}/pages/...
	ShowSidebar     *bool                `json:"show_sidebar,omitempty"` // extension layout only: show sidebar with extension nav items (default: false)
	Nav             []NavItem            `json:"nav"`
	Widgets         []WidgetDescriptor   `json:"widgets"`
	Settings        []SettingsDescriptor `json:"settings"`
	SearchProviders []SearchProviderDef  `json:"search_providers,omitempty"`
	Notifications   []NotificationDef    `json:"notifications,omitempty"`
	Capabilities    []string             `json:"capabilities,omitempty"`
	AuthPages       []AuthPageDef        `json:"auth_pages,omitempty"`
	TopbarConfig    *TopbarConfig        `json:"topbar_config,omitempty"`  // per-extension topbar customization (extension layout only)
	ExtensionIcon   string               `json:"extension_icon,omitempty"` // icon/logo URL for the app grid navigator

	// SidebarHeaderContent is custom content rendered in the extension sidebar
	// header below the branding. Used for app switchers, status indicators, etc.
	SidebarHeaderContent templ.Component `json:"-"`

	// SidebarFooterContent is custom content rendered in the extension sidebar
	// footer above the user dropdown. Used for links like API Docs, support, etc.
	SidebarFooterContent templ.Component `json:"-"`

	// TopbarExtraContent is custom content rendered in the extension topbar
	// after the search trigger. Used for environment switchers, context indicators, etc.
	TopbarExtraContent templ.Component `json:"-"`
}

// ShowSidebarOrDefault returns the ShowSidebar value, defaulting to false.
func (m *Manifest) ShowSidebarOrDefault() bool {
	if m == nil || m.ShowSidebar == nil {
		return false
	}
	return *m.ShowSidebar
}

// TopbarConfig defines per-extension topbar customization for the standalone
// extension layout. Only used when Layout is "extension".
type TopbarConfig struct {
	// Title shown in the topbar (overrides extension DisplayName).
	Title string `json:"title,omitempty"`

	// LogoURL is the URL for the extension logo shown in the topbar.
	LogoURL string `json:"logo_url,omitempty"`

	// LogoIcon is a lucide icon name to use as the logo when LogoURL is empty.
	LogoIcon string `json:"logo_icon,omitempty"`

	// AccentColor is a CSS color value for the topbar accent (e.g. brand color).
	AccentColor string `json:"accent_color,omitempty"`

	// Actions are custom action buttons shown in the topbar (right side).
	Actions []TopbarAction `json:"actions,omitempty"`

	// ShowSearch controls whether the search trigger appears in this extension's topbar.
	ShowSearch bool `json:"show_search,omitempty"`

	// ShowThemeToggle controls whether the theme toggle appears. Default: true.
	ShowThemeToggle *bool `json:"show_theme_toggle,omitempty"`

	// ShowUserMenu controls whether the user menu appears. Default: true.
	ShowUserMenu *bool `json:"show_user_menu,omitempty"`
}

// ShowThemeToggleOrDefault returns the ShowThemeToggle value, defaulting to true.
func (tc *TopbarConfig) ShowThemeToggleOrDefault() bool {
	if tc == nil || tc.ShowThemeToggle == nil {
		return true
	}

	return *tc.ShowThemeToggle
}

// ShowUserMenuOrDefault returns the ShowUserMenu value, defaulting to true.
func (tc *TopbarConfig) ShowUserMenuOrDefault() bool {
	if tc == nil || tc.ShowUserMenu == nil {
		return true
	}

	return *tc.ShowUserMenu
}

// TopbarAction defines a custom button/link in the extension topbar.
type TopbarAction struct {
	// Label is the button text (hidden on small screens if Icon is set).
	Label string `json:"label"`

	// Icon is the lucide icon name for the action button.
	Icon string `json:"icon,omitempty"`

	// Href is the navigation target. Can be a full URL or a dashboard path.
	Href string `json:"href"`

	// Tooltip is shown on hover.
	Tooltip string `json:"tooltip,omitempty"`

	// Variant controls button styling: "default", "outline", "ghost".
	Variant string `json:"variant,omitempty"`
}

// NavItem represents a navigation entry in the dashboard sidebar.
type NavItem struct {
	Label    string    `json:"label"`
	Path     string    `json:"path"`
	Icon     string    `json:"icon"`
	Badge    string    `json:"badge,omitempty"`
	Group    string    `json:"group,omitempty"`
	Children []NavItem `json:"children,omitempty"`
	Priority int       `json:"priority,omitempty"`
	Access   string    `json:"access,omitempty"` // "public", "protected", "partial" (empty = dashboard default)
}

// WidgetDescriptor describes a widget that can be rendered on the dashboard.
type WidgetDescriptor struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Size        string `json:"size"`        // "sm", "md", "lg"
	RefreshSec  int    `json:"refresh_sec"` // 0 = static
	Group       string `json:"group"`
	Priority    int    `json:"priority"`
}

// SettingsDescriptor describes a settings panel contributed by an extension.
type SettingsDescriptor struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Group       string `json:"group"`
	Icon        string `json:"icon"`
	Priority    int    `json:"priority"`
}

// SearchProviderDef describes a search provider contributed by an extension.
type SearchProviderDef struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Prefix      string   `json:"prefix,omitempty"`       // e.g., "user:" filters to this provider
	ResultTypes []string `json:"result_types,omitempty"` // "page", "entity", "action"
}

// NotificationDef describes a notification type an extension can emit.
type NotificationDef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Severity string `json:"severity"` // "info", "warning", "error", "critical"
}

// SearchResult represents a single search result from a contributor.
type SearchResult struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	URL         string  `json:"url"`
	Icon        string  `json:"icon"`
	Source      string  `json:"source"` // contributor name
	Category    string  `json:"category"`
	Score       float64 `json:"score"`
}

// Notification represents a real-time notification from a contributor.
type Notification struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	URL      string `json:"url,omitempty"`
	Time     int64  `json:"time"` // unix timestamp
}

// AuthPageDef describes an authentication page contributed by an extension.
type AuthPageDef struct {
	Type     string `json:"type"`     // "login", "logout", "register", "forgot-password", etc.
	Path     string `json:"path"`     // relative path under auth base (e.g. "/login")
	Title    string `json:"title"`    // page title
	Icon     string `json:"icon"`     // optional icon name
	Priority int    `json:"priority"` // sort order
}
