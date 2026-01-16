package ui

import (
	"fmt"
	"time"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/components/card"
	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/primitives"
)

// MetricCard renders a single metric card with icon, value, and description.
func MetricCard(title, value, description string, icon g.Node, trendValue string, trendUp bool) g.Node {
	var (
		trendIcon  g.Node
		trendColor string
	)

	if trendValue != "" {
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
				html.Class("flex items-center justify-between pb-2"),
				html.Div(
					html.Class("text-sm font-medium"),
					g.Text(title),
				),
				html.Div(
					html.Class("text-muted-foreground"),
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
				g.If(trendValue != "", html.Div(
					html.Class("flex items-center gap-1 text-xs "+trendColor),
					trendIcon,
					html.Span(g.Text(trendValue)),
					g.If(description != "", html.Span(
						html.Class("text-muted-foreground ml-1"),
						g.Text(description),
					)),
				)),
				g.If(trendValue == "" && description != "", primitives.Text(
					primitives.TextSize("text-xs"),
					primitives.TextColor("text-muted-foreground"),
					primitives.TextChildren(g.Text(description)),
				)),
			),
		),
	)
}

// OverviewMetrics renders the main overview metrics grid.
func OverviewMetrics(basePath string) g.Node {
	return html.Div(
		g.Attr("x-data", "{}"),
		html.Class("grid gap-4 md:grid-cols-2 lg:grid-cols-4"),

		// Overall Health Card
		html.Div(
			g.Attr("x-show", "$store.dashboard.data"),
			MetricCard(
				"Overall Health",
				"",
				"System status",
				icons.Activity(icons.WithSize(16)),
				"",
				false,
			),
			// Use Alpine to bind the value dynamically
			g.Attr("x-html", `
				(() => {
					const data = $store.dashboard.data;
					if (!data) return '';
					const statusColors = {
						'healthy': 'text-green-600 dark:text-green-400',
						'degraded': 'text-yellow-600 dark:text-yellow-400',
						'unhealthy': 'text-red-600 dark:text-red-400'
					};
					const status = (data.overall_health || 'unknown').toLowerCase();
					return '<div class="text-2xl font-bold ' + (statusColors[status] || '') + '">' + 
						(data.overall_health || 'Unknown') + '</div>';
				})()
			`),
		),

		// Services Card - bound to Alpine store
		html.Div(
			g.Attr("x-data", "{}"),
			card.Card(
				card.Header(
					html.Div(
						html.Class("flex items-center justify-between pb-2"),
						html.Div(
							html.Class("text-sm font-medium"),
							g.Text("Services"),
						),
						html.Div(
							html.Class("text-muted-foreground"),
							icons.Layers(icons.WithSize(16)),
						),
					),
				),
				card.Content(
					html.Div(
						html.Class("space-y-1"),
						html.Div(
							html.Class("text-2xl md:text-3xl font-bold"),
							g.Attr("x-text", "($store.dashboard.data?.healthy_services || 0) + ' / ' + ($store.dashboard.data?.total_services || 0)"),
						),
						primitives.Text(
							primitives.TextSize("text-xs"),
							primitives.TextColor("text-muted-foreground"),
							primitives.TextChildren(g.Text("Healthy services")),
						),
					),
				),
			),
		),

		// Metrics Card
		html.Div(
			g.Attr("x-data", "{}"),
			card.Card(
				card.Header(
					html.Div(
						html.Class("flex items-center justify-between pb-2"),
						html.Div(
							html.Class("text-sm font-medium"),
							g.Text("Metrics"),
						),
						html.Div(
							html.Class("text-muted-foreground"),
							icons.ChartBar(icons.WithSize(16)),
						),
					),
				),
				card.Content(
					html.Div(
						html.Class("space-y-1"),
						html.Div(
							html.Class("text-2xl md:text-3xl font-bold"),
							g.Attr("x-text", "$store.dashboard.data?.total_metrics || 0"),
						),
						primitives.Text(
							primitives.TextSize("text-xs"),
							primitives.TextColor("text-muted-foreground"),
							primitives.TextChildren(g.Text("Total metrics tracked")),
						),
					),
				),
			),
		),

		// Uptime Card
		html.Div(
			g.Attr("x-data", "{}"),
			card.Card(
				card.Header(
					html.Div(
						html.Class("flex items-center justify-between pb-2"),
						html.Div(
							html.Class("text-sm font-medium"),
							g.Text("Uptime"),
						),
						html.Div(
							html.Class("text-muted-foreground"),
							icons.Clock(icons.WithSize(16)),
						),
					),
				),
				card.Content(
					html.Div(
						html.Class("space-y-1"),
						html.Div(
							html.Class("text-2xl md:text-3xl font-bold"),
							g.Attr("x-text", "formatDuration($store.dashboard.data?.uptime || 0)"),
						),
						primitives.Text(
							primitives.TextSize("text-xs"),
							primitives.TextColor("text-muted-foreground"),
							primitives.TextChildren(g.Text("System uptime")),
						),
					),
				),
			),
		),
	)
}

// ServicesGrid renders a grid of service cards.
func ServicesGrid(basePath string) g.Node {
	return html.Div(
		g.Attr("x-data", "{}"),
		html.Class("grid gap-4 md:grid-cols-2 lg:grid-cols-3"),

		// Loading state
		html.Div(
			g.Attr("x-show", "!$store.dashboard.data || $store.dashboard.loading"),
			html.Class("col-span-full"),
			LoadingSpinner("Loading services..."),
		),

		// Services list
		html.Div(
			g.Attr("x-show", "$store.dashboard.data && !$store.dashboard.loading"),
			html.Div(
				g.Attr("x-show", "($store.dashboard.data?.services || []).length > 0"),
				html.Class("contents"),
				// Services will be rendered by Alpine
				g.Attr("x-html", `
					($store.dashboard.data?.services || []).map(service => {
						const statusColors = {
							'healthy': 'border-green-500',
							'degraded': 'border-yellow-500',
							'unhealthy': 'border-red-500'
						};
						const borderColor = statusColors[(service.status || '').toLowerCase()] || 'border-gray-300';
						return '<div class="cursor-pointer hover:shadow-lg transition-shadow border-l-4 rounded-lg border bg-card text-card-foreground shadow-sm ' + borderColor + '" @click="$store.dashboard.showServiceDetail(\'' + service.name + '\')">' +
							'<div class="flex flex-col space-y-1.5 p-6"><div class="flex items-center justify-between"><h3 class="text-base font-semibold leading-none tracking-tight">' + service.name + '</h3>' +
							'<span class="inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors bg-secondary text-secondary-foreground">' + service.type + '</span></div></div>' +
							'<div class="p-6 pt-0"><div class="flex items-center gap-2">' +
							'<span class="inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold">' + service.status + '</span>' +
							'</div></div></div>';
					}).join('')
				`),
			),
		),

		// Empty state
		html.Div(
			g.Attr("x-show", "$store.dashboard.data && ($store.dashboard.data?.services || []).length === 0"),
			html.Class("col-span-full"),
			EmptyState(
				icons.Inbox(icons.WithSize(32)),
				"No Services",
				"No services are currently registered.",
			),
		),
	)
}

// Helper functions.
func getStatusColorClass(status string) string {
	switch status {
	case "healthy", "Healthy":
		return "border-green-500"
	case "degraded", "Degraded":
		return "border-yellow-500"
	case "unhealthy", "Unhealthy":
		return "border-red-500"
	default:
		return "border-gray-300"
	}
}

func formatRelativeTime(t time.Time) string {
	duration := time.Since(t)
	if duration < time.Minute { //nolint:gocritic // ifElseChain: range checks clearer with if-else
		return "just now"
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())

		return fmt.Sprintf("%d min ago", minutes)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())

		return fmt.Sprintf("%d hr ago", hours)
	} else {
		days := int(duration.Hours() / 24)

		return fmt.Sprintf("%d days ago", days)
	}
}
