package shell

import (
	"github.com/a-h/templ"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// Breadcrumb represents a single breadcrumb navigation item.
type Breadcrumb struct {
	Label string
	Path  string // empty = current page (no link)
}

// UserDropdownAction represents a configurable action in the sidebar footer user dropdown.
// Auth providers (e.g. authsome) contribute these via the DashboardFooterContributor interface.
type UserDropdownAction struct {
	Label string // display text (e.g., "Profile")
	Icon  string // lucide icon name (e.g., "user")
	Href  string // navigation target (full path)
}

// SidebarConfig holds all data needed to render the dashboard sidebar.
type SidebarConfig struct {
	Groups     []contributor.NavGroup
	ActivePath string
	BasePath   string

	// Auth-related fields for the sidebar footer.
	EnableAuth bool
	User       *dashauth.UserInfo
	LoginPath  string // full path shown in footer when not authenticated
	LogoutPath string // full path, e.g. "/dashboard/auth/logout"

	// UserDropdownActions are additional actions shown in the user dropdown
	// between the user info and the "Sign out" item. Contributed by extensions
	// implementing DashboardFooterContributor.
	UserDropdownActions []UserDropdownAction

	// SettingsBasePath is the base path for the settings page (e.g., "/dashboard/settings").
	// When non-empty, a collapsible "Settings" nav item is rendered in the sidebar.
	SettingsBasePath string

	// SettingsItems are the registered settings from all contributors.
	SettingsItems []contributor.ResolvedSetting
}

// ExtensionSidebarConfig holds all data needed to render an extension's sidebar.
type ExtensionSidebarConfig struct {
	Groups                 []contributor.NavGroup
	ActivePath             string
	BasePath               string          // dashboard base path
	DashboardPath          string          // path to navigate back to dashboard
	ExtensionName          string          // extension display name
	ExtensionIcon          string          // lucide icon name
	ExtensionLogoURL       string          // custom logo URL (overrides ExtensionIcon)
	ExtensionIconComponent templ.Component // custom icon component (highest priority, from DashboardIconProvider)
	ExtensionBasePath      string          // extension entry point URL

	// Auth-related fields for the sidebar footer.
	EnableAuth bool
	User       *dashauth.UserInfo
	LoginPath  string
	LogoutPath string

	// UserDropdownActions are additional actions shown in the user dropdown.
	UserDropdownActions []UserDropdownAction

	// HeaderExtraContent is custom content rendered in the sidebar header
	// below the extension branding. Used for app switchers, status indicators, etc.
	HeaderExtraContent templ.Component

	// FooterExtraContent is custom content rendered in the sidebar footer
	// above the user dropdown. Used for links like API Docs, support, etc.
	FooterExtraContent templ.Component
}
