package shell

import (
	"context"
	"io"
	"strings"

	"github.com/a-h/templ"

	"github.com/xraph/forgeui/icons"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// isActivePath determines if a nav item should be marked as active.
func isActivePath(itemPath, activePath, basePath string) bool {
	if activePath == "" || itemPath == "" {
		return false
	}

	// Exact match
	if itemPath == activePath {
		return true
	}

	// For the dashboard root, only match exact
	if itemPath == basePath || itemPath == basePath+"/" {
		return activePath == basePath || activePath == basePath+"/"
	}

	// Prefix match for sub-pages
	return strings.HasPrefix(activePath, itemPath+"/") || strings.HasPrefix(activePath, itemPath+"?")
}

// SanitizeAlpineKey converts a group name to a valid Alpine.js variable name.
func SanitizeAlpineKey(name string) string {
	var sb strings.Builder

	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			sb.WriteRune(c)
		}
	}

	result := sb.String()

	if result == "" {
		return "group"
	}

	// Ensure it doesn't start with a digit
	if result[0] >= '0' && result[0] <= '9' {
		result = "g" + result
	}

	return result
}

// navIcon maps an icon name string to a templ.Component.
// Used from templ templates via @navIcon("name").
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

// navIconFallback renders a generic dot placeholder as a templ component.
func navIconFallback() templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<span class="inline-flex h-4 w-4 items-center justify-center text-xs">&#8226;</span>`)
		return err
	})
}

// hasActiveChild returns true if any child nav item is currently active.
func hasActiveChild(children []contributor.ResolvedNav, activePath, basePath string) bool {
	for _, child := range children {
		if isActivePath(child.FullPath, activePath, basePath) {
			return true
		}
	}
	return false
}
