package shell

import (
	"fmt"
	"strings"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/icons"
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

// navIcon maps an icon name string to its gomponents icon node.
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
	default:
		// Fallback: render a generic icon placeholder
		return html.Span(
			html.Class(fmt.Sprintf("inline-flex h-[18px] w-[18px] items-center justify-center text-xs")),
			g.Text("•"),
		)
	}
}
