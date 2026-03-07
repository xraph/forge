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
	case "grid-3x3", "grid":
		return icons.Grid3x3(size)
	case "building":
		return icons.Building(size)
	case "box":
		return icons.Box(size)
	case "globe":
		return icons.Globe(size)
	case "database":
		return icons.Database(size)
	case "mail":
		return icons.Mail(size)
	case "message-square":
		return icons.MessageSquare(size)
	case "shield-check":
		return icons.ShieldCheck(size)
	case "key-round":
		return icons.KeyRound(size)
	case "building-2":
		return icons.Building2(size)
	case "monitor-smartphone":
		return icons.MonitorSmartphone(size)
	case "webhook":
		return icons.Webhook(size)
	case "user-plus":
		return icons.UserPlus(size)
	case "fingerprint":
		return icons.FingerprintPattern(size)

	// Forgery extension icons
	case "alert-triangle":
		return icons.TriangleAlert(size)
	case "bot":
		return icons.Bot(size)
	case "boxes":
		return icons.Boxes(size)
	case "brain":
		return icons.Brain(size)
	case "briefcase":
		return icons.Briefcase(size)
	case "clock":
		return icons.Clock(size)
	case "cpu":
		return icons.Cpu(size)
	case "file-edit":
		return icons.FilePen(size)
	case "file-lock":
		return icons.FileLock(size)
	case "git-branch":
		return icons.GitBranch(size)
	case "layers":
		return icons.Layers(size)
	case "list":
		return icons.List(size)
	case "list-tree":
		return icons.ListTree(size)
	case "play":
		return icons.Play(size)
	case "puzzle":
		return icons.Puzzle(size)
	case "scroll-text":
		return icons.ScrollText(size)
	case "send":
		return icons.Send(size)
	case "sliders-horizontal":
		return icons.SlidersHorizontal(size)
	case "sparkles":
		return icons.Sparkles(size)
	case "terminal":
		return icons.Terminal(size)
	case "user-check":
		return icons.UserCheck(size)
	case "user-circle":
		return icons.CircleUser(size)
	case "wrench":
		return icons.Wrench(size)
	case "zap":
		return icons.Zap(size)
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

// isSettingsActive returns true if the current path is a settings page,
// used to auto-open the collapsible settings nav item.
func isSettingsActive(settingsBasePath string, items []contributor.ResolvedSetting, activePath string) bool {
	if activePath == settingsBasePath || activePath == settingsBasePath+"/" {
		return true
	}
	if strings.HasPrefix(activePath, settingsBasePath+"/") {
		return true
	}
	for _, s := range items {
		if activePath == settingsBasePath+"/"+s.ID {
			return true
		}
	}
	return false
}
