package shell

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/components/sidebar"
	"github.com/xraph/forgeui/icons"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// ---- ForgeUI Sidebar Builder ----
// These functions build the sidebar using forgeui's sidebar component
// which provides Alpine.js state management, collapsible modes, tooltips,
// keyboard shortcuts (Cmd+B), and state persistence.

// BuildForgeUISidebar builds a full forgeui sidebar component from nav groups.
func BuildForgeUISidebar(groups []contributor.NavGroup, activePath, basePath string) g.Node {
	return sidebar.SidebarWithOptions(
		[]sidebar.SidebarOption{
			sidebar.WithCollapsible(true),
			sidebar.WithCollapsibleMode(sidebar.CollapsibleOffcanvas),
			sidebar.WithKeyboardShortcut("b"),
			sidebar.WithPersistState(true),
			sidebar.WithStorageKey("forge_dashboard_sidebar"),
		},

		// Header
		sidebar.SidebarHeader(
			html.A(
				html.Href(basePath),
				html.Class("flex items-center gap-2 text-sidebar-foreground hover:text-sidebar-primary transition-colors"),
				g.Attr("hx-get", basePath),
				g.Attr("hx-target", "#content"),
				g.Attr("hx-swap", "innerHTML"),
				g.Attr("hx-push-url", "true"),
				icons.LayoutDashboard(icons.WithSize(22), icons.WithClass("text-sidebar-primary")),
				html.Span(
					html.Class("text-lg font-semibold"),
					g.Attr("x-show", "$store.sidebar && (!$store.sidebar.collapsed || $store.sidebar.isMobile)"),
					g.Text("Forge"),
				),
			),
		),

		// Toggle button on the sidebar edge
		sidebar.SidebarToggle(),

		// Rail for easy hover-to-expand
		sidebar.SidebarRail(),

		// Nav content
		sidebar.SidebarContent(
			BuildSidebarNavGroups(groups, activePath, basePath)...,
		),

		// Footer
		sidebar.SidebarFooter(
			html.Div(
				html.Class("flex items-center gap-2 text-xs text-sidebar-foreground/50"),
				icons.Settings(icons.WithSize(14)),
				html.Span(
					g.Attr("x-show", "$store.sidebar && (!$store.sidebar.collapsed || $store.sidebar.isMobile)"),
					g.Text("Forge Dashboard v3"),
				),
			),
		),
	)
}

// BuildSidebarNavGroups converts contributor NavGroups into forgeui sidebar groups.
func BuildSidebarNavGroups(groups []contributor.NavGroup, activePath, basePath string) []g.Node {
	nodes := make([]g.Node, 0, len(groups))

	for _, group := range groups {
		groupKey := SanitizeAlpineKey(group.Name)

		items := make([]g.Node, 0, len(group.Items))
		for _, item := range group.Items {
			items = append(items, BuildSidebarMenuItem(item, activePath, basePath))
		}

		nodes = append(nodes,
			sidebar.SidebarGroupCollapsible(
				[]sidebar.SidebarGroupOption{
					sidebar.WithGroupKey(groupKey),
					sidebar.WithGroupDefaultOpen(true),
				},
				sidebar.SidebarGroupLabelCollapsible(groupKey, group.Name, nil),
				sidebar.SidebarGroupContent(groupKey,
					sidebar.SidebarMenu(items...),
				),
			),
		)
	}

	return nodes
}

// BuildSidebarMenuItem creates a forgeui sidebar menu button with HTMX navigation.
func BuildSidebarMenuItem(item contributor.ResolvedNav, activePath, basePath string) g.Node {
	isActive := isActivePath(item.FullPath, activePath, basePath)

	opts := []sidebar.SidebarMenuButtonOption{
		sidebar.WithMenuHref(item.FullPath),
		sidebar.WithActive(isActive),
		sidebar.WithMenuTooltip(item.Label),
		sidebar.WithMenuAttrs(
			g.Attr("hx-get", item.FullPath),
			g.Attr("hx-target", "#content"),
			g.Attr("hx-swap", "innerHTML"),
			g.Attr("hx-push-url", "true"),
		),
	}

	if item.Icon != "" {
		opts = append(opts, sidebar.WithMenuIcon(navIcon(item.Icon)))
	}

	if item.Badge != "" {
		opts = append(opts, sidebar.WithMenuBadge(
			sidebar.SidebarMenuBadge(item.Badge),
		))
	}

	return sidebar.SidebarMenuItem(
		sidebar.SidebarMenuButton(item.Label, opts...),
	)
}

// SanitizeAlpineKey converts a group name to a valid Alpine.js variable name.
func SanitizeAlpineKey(name string) string {
	result := ""
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			result += string(c)
		}
	}

	if result == "" {
		return "group"
	}

	// Ensure it doesn't start with a digit
	if result[0] >= '0' && result[0] <= '9' {
		result = "g" + result
	}

	return result
}
