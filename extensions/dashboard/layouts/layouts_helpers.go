package layouts

import (
	"fmt"
	"strings"

	"github.com/a-h/templ"

	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/router"

	"github.com/xraph/forge/extensions/dashboard/contributor"
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
	default:
		return navIconFallback()
	}
}

// navIconFallback renders a generic dot placeholder.
func navIconFallback() templ.Component {
	return templ.Raw(`<span class="inline-flex h-4 w-4 items-center justify-center text-xs">&#8226;</span>`)
}

// ---------------------------------------------------------------------------
// Extension layout helpers
// ---------------------------------------------------------------------------

// resolvedTopbarConfig is the resolved topbar configuration for the current
// extension page, with defaults applied.
type resolvedTopbarConfig struct {
	Title           string
	LogoURL         string
	LogoIcon        string
	LogoComponent   templ.Component // custom icon component (highest priority)
	AccentColor     string
	Actions         []contributor.TopbarAction
	ShowSearch      bool
	ShowThemeToggle *bool
	ShowUserMenu    *bool
	ExtraContent    templ.Component // custom content rendered after search trigger
}

// ShowThemeToggleOrDefault returns the ShowThemeToggle value, defaulting to true.
func (r resolvedTopbarConfig) ShowThemeToggleOrDefault() bool {
	if r.ShowThemeToggle == nil {
		return true
	}

	return *r.ShowThemeToggle
}

// ShowUserMenuOrDefault returns the ShowUserMenu value, defaulting to true.
func (r resolvedTopbarConfig) ShowUserMenuOrDefault() bool {
	if r.ShowUserMenu == nil {
		return true
	}

	return *r.ShowUserMenu
}

// resolveTopbarConfig extracts the topbar configuration for the current page's
// contributor. It examines the URL path to determine which contributor owns
// the page and reads its TopbarConfig from the manifest.
func (lm *LayoutManager) resolveTopbarConfig(ctx *router.PageContext) resolvedTopbarConfig {
	path := ctx.Request.URL.Path
	contribName := extractContributorName(path, lm.basePath)

	if contribName == "" {
		return resolvedTopbarConfig{Title: lm.config.Title}
	}

	m, ok := lm.registry.GetManifest(contribName)
	if !ok || m.TopbarConfig == nil {
		displayName := contribName
		icon := ""
		if ok {
			if m.DisplayName != "" {
				displayName = m.DisplayName
			}
			icon = m.Icon
		}

		cfg := resolvedTopbarConfig{
			Title:    displayName,
			LogoIcon: icon,
		}
		if ok {
			cfg.LogoComponent = m.IconComponent
			cfg.ExtraContent = m.TopbarExtraContent
		}

		return cfg
	}

	tc := m.TopbarConfig
	title := tc.Title
	if title == "" {
		title = m.DisplayName
		if title == "" {
			title = contribName
		}
	}

	logoIcon := tc.LogoIcon
	if logoIcon == "" {
		logoIcon = m.Icon
	}

	return resolvedTopbarConfig{
		Title:           title,
		LogoURL:         tc.LogoURL,
		LogoIcon:        logoIcon,
		LogoComponent:   m.IconComponent,
		AccentColor:     tc.AccentColor,
		Actions:         tc.Actions,
		ShowSearch:      tc.ShowSearch,
		ShowThemeToggle: tc.ShowThemeToggle,
		ShowUserMenu:    tc.ShowUserMenu,
		ExtraContent:    m.TopbarExtraContent,
	}
}

// extractContributorName extracts the contributor name from a dashboard URL path.
// e.g. "/dashboard/ext/authsome/pages/users" -> "authsome"
func extractContributorName(path, basePath string) string {
	trimmed := strings.TrimPrefix(path, basePath)
	trimmed = strings.TrimPrefix(trimmed, "/")

	if !strings.HasPrefix(trimmed, "ext/") {
		return ""
	}

	parts := strings.SplitN(trimmed[4:], "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}

	return parts[0]
}

// appGridEntryClass returns CSS classes for an app grid entry.
func appGridEntryClass(entryPath, activePath string) string {
	base := "group flex flex-col items-center justify-center rounded-lg p-2.5 text-center transition-all cursor-pointer"
	if strings.HasPrefix(activePath, entryPath) {
		return base + " bg-accent ring-1 ring-ring/20"
	}

	return base + " hover:bg-accent/50"
}

// appGridIconClass returns CSS classes for an app grid entry icon container.
func appGridIconClass(entryPath, activePath string) string {
	base := "flex h-10 w-10 items-center justify-center rounded-lg transition-colors"
	if strings.HasPrefix(activePath, entryPath) {
		return base + " bg-primary/10 text-primary"
	}

	return base + " bg-muted text-muted-foreground group-hover:bg-accent group-hover:text-accent-foreground"
}

// extensionActionClass returns CSS classes for an extension action button.
func extensionActionClass(variant string) string {
	base := "inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors"

	switch variant {
	case "outline":
		return base + " border border-input hover:bg-accent hover:text-accent-foreground"
	case "ghost":
		return base + " hover:bg-accent hover:text-accent-foreground"
	default:
		return base + " bg-primary text-primary-foreground hover:bg-primary/90"
	}
}

// searchComponentJS returns a JavaScript object string for a local Alpine x-data
// component that manages search state inside the ForgeUI dialog.
func searchComponentJS(basePath string) string {
	return fmt.Sprintf(`{
        query: '',
        results: [],
        loading: false,
        _timer: null,
        search() {
            clearTimeout(this._timer);
            if (!this.query || this.query.length < 2) {
                this.results = [];
                this.loading = false;
                return;
            }
            this.loading = true;
            var self = this;
            this._timer = setTimeout(function() {
                fetch('%s/api/search?q=' + encodeURIComponent(self.query) + '&limit=20')
                    .then(function(r) { return r.json(); })
                    .then(function(data) {
                        self.results = data.results || [];
                        self.loading = false;
                    })
                    .catch(function() {
                        self.results = [];
                        self.loading = false;
                    });
            }, 250);
        },
        selectResult() {
            window.tui.dialog.close('forge-search');
        }
    }`, basePath)
}
