package ui

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/components/badge"
	"github.com/xraph/forgeui/components/button"
	"github.com/xraph/forgeui/components/card"
	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/primitives"
)

// StatusBadge renders a status indicator badge.
func StatusBadge(status string) g.Node {
	var variant forgeui.Variant

	switch status {
	case "healthy", "Healthy":
		variant = forgeui.VariantDefault
	case "degraded", "Degraded":
		variant = forgeui.VariantSecondary
	case "unhealthy", "Unhealthy":
		variant = forgeui.VariantDestructive
	default:
		variant = forgeui.VariantOutline
	}

	return badge.Badge(
		status,
		badge.WithVariant(variant),
	)
}

// SectionHeader renders a section header with title and optional description.
func SectionHeader(title, description string) g.Node {
	return primitives.VStack("2",
		primitives.Text(
			primitives.TextAs("h2"),
			primitives.TextSize("text-xl md:text-2xl"),
			primitives.TextWeight("font-bold"),
			primitives.TextChildren(g.Text(title)),
		),
		g.If(description != "", primitives.Text(
			primitives.TextSize("text-sm"),
			primitives.TextColor("text-muted-foreground"),
			primitives.TextChildren(g.Text(description)),
		)),
	)
}

// StatCard renders a statistics card with icon, value, label, and optional trend.
func StatCard(icon g.Node, value, label, trend string, trendUp bool) g.Node {
	var (
		trendIcon  g.Node
		trendColor string
	)

	if trend != "" {
		if trendUp {
			trendIcon = icons.TrendingUp(icons.WithSize(14))
			trendColor = "text-green-600 dark:text-green-400"
		} else {
			trendIcon = icons.TrendingDown(icons.WithSize(14))
			trendColor = "text-red-600 dark:text-red-400"
		}
	}

	return card.Card(
		card.Header(
			html.Div(
				html.Class("flex items-center justify-between"),
				html.Div(
					html.Class("flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary"),
					icon,
				),
			),
		),
		card.Content(
			primitives.VStack("1",
				primitives.Text(
					primitives.TextSize("text-2xl md:text-3xl"),
					primitives.TextWeight("font-bold"),
					primitives.TextChildren(g.Text(value)),
				),
				primitives.Text(
					primitives.TextSize("text-sm"),
					primitives.TextColor("text-muted-foreground"),
					primitives.TextChildren(g.Text(label)),
				),
				g.If(trend != "", html.Div(
					html.Class("flex items-center gap-1 text-xs "+trendColor),
					trendIcon,
					g.Text(trend),
				)),
			),
		),
	)
}

// ActionButton renders a button with an icon.
func ActionButton(label string, icon g.Node, variant forgeui.Variant, attrs ...g.Node) g.Node {
	nodes := []g.Node{
		g.Group([]g.Node{icon, g.Text(label)}),
	}

	opts := []button.Option{
		button.WithVariant(variant),
		button.WithSize(forgeui.SizeSM),
	}

	return button.Button(
		g.Group(append(nodes, attrs...)),
		opts...,
	)
}

// EmptyState renders an empty state placeholder.
func EmptyState(icon g.Node, title, description string) g.Node {
	return html.Div(
		html.Class("flex flex-col items-center justify-center py-12 text-center"),
		html.Div(
			html.Class("mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-muted text-muted-foreground"),
			icon,
		),
		primitives.VStack("2",
			primitives.Text(
				primitives.TextSize("text-lg"),
				primitives.TextWeight("font-semibold"),
				primitives.TextChildren(g.Text(title)),
			),
			primitives.Text(
				primitives.TextSize("text-sm"),
				primitives.TextColor("text-muted-foreground"),
				primitives.TextChildren(g.Text(description)),
			),
		),
	)
}

// LoadingSpinner renders a loading spinner with optional message.
func LoadingSpinner(message string) g.Node {
	return html.Div(
		html.Class("flex flex-col items-center justify-center py-12"),
		html.Div(
			html.Class("h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent"),
		),
		g.If(message != "", html.P(
			html.Class("mt-4 text-sm text-muted-foreground"),
			g.Text(message),
		)),
	)
}

// DashboardHeader renders the main dashboard header with navigation and theme toggle.
func DashboardHeader(title string, basePath string) g.Node {
	return html.Header(
		html.Class("sticky top-0 z-50 w-full border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60"),
		html.Div(
			html.Class("container flex h-16 items-center justify-between"),
			// Title
			html.Div(
				html.Class("flex items-center gap-2"),
				icons.LayoutDashboard(icons.WithSize(24), icons.WithClass("text-primary")),
				primitives.Text(
					primitives.TextSize("text-xl"),
					primitives.TextWeight("font-bold"),
					primitives.TextChildren(g.Text(title)),
				),
			),
			// Actions
			html.Div(
				html.Class("flex items-center gap-4"),
				// WebSocket status indicator
				html.Div(
					html.Class("flex items-center gap-2 text-sm"),
					g.Attr("x-data", "{}"),
					html.Div(
						html.Class("h-2 w-2 rounded-full"),
						g.Attr("x-bind:class", "$store.dashboard?.connected ? 'bg-green-500' : 'bg-gray-400'"),
					),
					html.Span(
						html.Class("text-muted-foreground"),
						g.Attr("x-text", "$store.dashboard?.connected ? 'Live' : 'Disconnected'"),
					),
				),
				// Refresh button
				button.Button(
					g.Group([]g.Node{
						g.Attr("@click", "$store.dashboard.refresh()"),
						icons.RefreshCw(icons.WithSize(16)),
					}),
					button.WithVariant(forgeui.VariantGhost),
					button.WithSize(forgeui.SizeIcon),
					button.WithClass("relative"),
				),
				// Theme toggle (will be added by theme system)
			),
		),
	)
}
