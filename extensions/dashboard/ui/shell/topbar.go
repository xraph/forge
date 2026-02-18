package shell

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/components/button"
	"github.com/xraph/forgeui/icons"
)

// TopBar renders the top bar with breadcrumbs, search trigger, notifications, and theme toggle.
func TopBar(title string, breadcrumbs []Breadcrumb, basePath string) g.Node {
	return html.Header(
		html.Class("sticky top-0 z-40 flex h-14 items-center gap-4 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60"),

		// Mobile menu toggle
		html.Div(
			html.Class("lg:hidden"),
			button.Button(
				g.Group([]g.Node{
					icons.Menu(icons.WithSize(20)),
				}),
				button.WithVariant(forgeui.VariantGhost),
				button.WithSize(forgeui.SizeIcon),
				button.WithAttrs(
					g.Attr("@click", "$store.dashboard.toggleMobileSidebar()"),
					g.Attr("aria-label", "Toggle sidebar"),
				),
			),
		),

		// Desktop sidebar toggle
		html.Div(
			html.Class("hidden lg:block"),
			button.Button(
				g.Group([]g.Node{
					icons.Menu(icons.WithSize(18)),
				}),
				button.WithVariant(forgeui.VariantGhost),
				button.WithSize(forgeui.SizeIcon),
				button.WithAttrs(
					g.Attr("@click", "$store.dashboard.toggleSidebar()"),
					g.Attr("aria-label", "Toggle sidebar"),
				),
			),
		),

		// Breadcrumbs
		html.Div(
			html.Class("flex-1"),
			Breadcrumbs(breadcrumbs, basePath),
		),

		// Right-side actions
		html.Div(
			html.Class("flex items-center gap-2"),

			// Search trigger (Cmd+K)
			searchTrigger(),

			// Connection status indicator
			connectionIndicator(),

			// Notification bell
			notificationBell(basePath),

			// Theme toggle
			themeToggle(),
		),
	)
}

// searchTrigger renders the Cmd+K search button placeholder.
func searchTrigger() g.Node {
	return button.Button(
		g.Group([]g.Node{
			icons.Search(icons.WithSize(16)),
			html.Span(
				html.Class("hidden md:inline-block text-sm text-muted-foreground"),
				g.Text("Search..."),
			),
			html.Kbd(
				html.Class("pointer-events-none hidden h-5 select-none items-center gap-1 rounded border bg-muted px-1.5 font-mono text-[10px] font-medium opacity-100 md:inline-flex"),
				g.Text("⌘K"),
			),
		}),
		button.WithVariant(forgeui.VariantOutline),
		button.WithSize(forgeui.SizeSM),
		button.WithClass("w-auto justify-start gap-2 text-muted-foreground md:w-48"),
		button.WithAttrs(
			g.Attr("aria-label", "Search"),
		),
	)
}

// connectionIndicator shows SSE connection status.
func connectionIndicator() g.Node {
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

// notificationBell renders the notification bell with unread count.
func notificationBell(basePath string) g.Node {
	return html.Div(
		g.Attr("x-data", ""),
		html.Class("relative"),
		button.Button(
			g.Group([]g.Node{
				icons.Bell(icons.WithSize(18)),
			}),
			button.WithVariant(forgeui.VariantGhost),
			button.WithSize(forgeui.SizeIcon),
			button.WithAttrs(
				g.Attr("aria-label", "Notifications"),
			),
		),
		// Unread count badge
		html.Span(
			html.Class("absolute -top-1 -right-1 flex h-4 w-4 items-center justify-center rounded-full bg-destructive text-[10px] font-bold text-destructive-foreground"),
			g.Attr("x-show", "$store.dashboard.unreadCount > 0"),
			g.Attr("x-text", "$store.dashboard.unreadCount"),
			g.Attr("x-cloak", ""),
		),
	)
}

// themeToggle renders a dark/light mode toggle button.
func themeToggle() g.Node {
	return html.Div(
		g.Attr("x-data", `{
			isDark: document.documentElement.classList.contains('dark'),
			toggle() {
				this.isDark = !this.isDark;
				document.documentElement.classList.toggle('dark', this.isDark);
				localStorage.setItem('theme', this.isDark ? 'dark' : 'light');
			}
		}`),
		button.Button(
			g.Group([]g.Node{
				html.Span(
					g.Attr("x-show", "!isDark"),
					icons.Moon(icons.WithSize(18)),
				),
				html.Span(
					g.Attr("x-show", "isDark"),
					g.Attr("x-cloak", ""),
					icons.Sun(icons.WithSize(18)),
				),
			}),
			button.WithVariant(forgeui.VariantGhost),
			button.WithSize(forgeui.SizeIcon),
			button.WithAttrs(
				g.Attr("@click", "toggle()"),
				g.Attr("aria-label", "Toggle theme"),
			),
		),
	)
}
