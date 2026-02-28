package shell

import (
	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// Breadcrumb represents a single breadcrumb navigation item.
type Breadcrumb struct {
	Label string
	Path  string // empty = current page (no link)
}

// SidebarConfig holds all data needed to render the dashboard sidebar.
type SidebarConfig struct {
	Groups     []contributor.NavGroup
	ActivePath string
	BasePath   string

	// Auth-related fields for the sidebar footer user dropdown.
	EnableAuth bool
	User       *dashauth.UserInfo
	LogoutPath string // full path, e.g. "/dashboard/auth/logout"
}
