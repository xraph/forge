package layouts

import (
	"github.com/a-h/templ"

	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/router"

	"github.com/xraph/forge/extensions/dashboard/ui/shell"
)

// alpineGlobalStoreJS initialises the Alpine.js darkMode store before Alpine
// starts so that the dark/light toggle works immediately on page load.
const alpineGlobalStoreJS = `
document.addEventListener('alpine:init', () => {
	Alpine.store('darkMode', {
		on: document.documentElement.classList.contains('dark'),
		toggle() {
			this.on = !this.on;
			document.documentElement.classList.toggle('dark', this.on);
			document.documentElement.setAttribute('data-theme', this.on ? 'dark' : 'light');
			localStorage.setItem('theme', this.on ? 'dark' : 'light');
		}
	});
});
`

// settingsNavItemClass returns the CSS class for a settings sub-navigation
// item based on whether it is active.
func settingsNavItemClass(active bool) string {
	base := "flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors"
	if active {
		return base + " bg-accent text-accent-foreground font-medium"
	}
	return base + " text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
}

// buildBreadcrumbs constructs breadcrumbs from the current request path.
// It produces a simple path-based breadcrumb trail rooted at "Dashboard".
func buildBreadcrumbs(ctx *router.PageContext) []shell.Breadcrumb {
	return []shell.Breadcrumb{
		{Label: "Dashboard", Path: ""},
	}
}

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

// navIcon maps an icon name string to a templ.Component.
func navIcon(name string) templ.Component {
	size := icons.WithSize(16)

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
		return navIconFallback()
	}
}

// navIconFallback renders a generic dot placeholder.
func navIconFallback() templ.Component {
	return templ.Raw(`<span class="inline-flex h-4 w-4 items-center justify-center text-xs">&#8226;</span>`)
}
