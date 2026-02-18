package pages

import (
	"context"
	"fmt"
	"time"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/components/card"
	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/primitives"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/ui"
)

// OverviewPage renders the dashboard overview page with stats, widgets, and charts.
func OverviewPage(ctx context.Context, c *collector.DataCollector, h *collector.DataHistory) (g.Node, error) {
	overview := c.CollectOverview(ctx)
	health := c.CollectHealth(ctx)

	return html.Div(
		html.Class("space-y-6"),

		// Page header
		ui.SectionHeader("Overview", "System health and performance at a glance"),

		// Stat cards row
		overviewStatCards(overview),

		// Health summary + charts
		html.Div(
			html.Class("grid gap-6 lg:grid-cols-2"),
			healthSummaryCard(health),
			servicesChartCard(),
		),
	), nil
}

// overviewStatCards renders the four main stat cards.
func overviewStatCards(overview *collector.OverviewData) g.Node {
	healthColor := healthStatusColor(overview.OverallHealth)

	return html.Div(
		html.Class("grid gap-4 md:grid-cols-2 lg:grid-cols-4"),

		// Overall Health
		ui.StatCard(
			icons.Activity(icons.WithSize(20)),
			overview.OverallHealth,
			"Overall Health",
			"",
			false,
		),

		// Services
		ui.StatCard(
			icons.Server(icons.WithSize(20)),
			fmt.Sprintf("%d / %d", overview.HealthyServices, overview.TotalServices),
			"Healthy Services",
			"",
			false,
		),

		// Metrics
		ui.StatCard(
			icons.ChartBar(icons.WithSize(20)),
			fmt.Sprintf("%d", overview.TotalMetrics),
			"Total Metrics",
			"",
			false,
		),

		// Uptime
		ui.StatCard(
			icons.Clock(icons.WithSize(20)),
			formatDuration(overview.Uptime),
			"System Uptime",
			"",
			false,
		),

		// Hidden element for health color reference (to avoid unused variable)
		g.If(false, html.Span(html.Class(healthColor))),
	)
}

// healthSummaryCard renders a health summary card.
func healthSummaryCard(health *collector.HealthData) g.Node {
	return card.Card(
		card.Header(
			html.Div(
				html.Class("flex items-center justify-between"),
				card.Title("Health Summary"),
				ui.StatusBadge(health.OverallStatus),
			),
		),
		card.Content(
			html.Div(
				html.Class("space-y-4"),

				// Status counts
				html.Div(
					html.Class("grid grid-cols-4 gap-4 text-center"),
					healthCountItem("Healthy", health.Summary.Healthy, "text-green-600 dark:text-green-400"),
					healthCountItem("Degraded", health.Summary.Degraded, "text-yellow-600 dark:text-yellow-400"),
					healthCountItem("Unhealthy", health.Summary.Unhealthy, "text-red-600 dark:text-red-400"),
					healthCountItem("Unknown", health.Summary.Unknown, "text-muted-foreground"),
				),

				// Last check time
				html.Div(
					html.Class("flex items-center justify-between pt-2 border-t text-xs text-muted-foreground"),
					html.Span(g.Text("Last checked")),
					html.Span(g.Text(formatTime(health.CheckedAt))),
				),
			),
		),
	)
}

// healthCountItem renders a single health count item.
func healthCountItem(label string, count int, colorClass string) g.Node {
	return html.Div(
		html.Class("space-y-1"),
		primitives.Text(
			primitives.TextSize("text-2xl"),
			primitives.TextWeight("font-bold"),
			primitives.TextColor(colorClass),
			primitives.TextChildren(g.Text(fmt.Sprintf("%d", count))),
		),
		primitives.Text(
			primitives.TextSize("text-xs"),
			primitives.TextColor("text-muted-foreground"),
			primitives.TextChildren(g.Text(label)),
		),
	)
}

// servicesChartCard renders a placeholder chart card for services.
func servicesChartCard() g.Node {
	return ui.ChartPlaceholder(
		"services-chart",
		"Service Count History",
		"Total and healthy services over time",
	)
}

// healthStatusColor returns a CSS color class for a health status string.
func healthStatusColor(status string) string {
	switch status {
	case "healthy", "Healthy":
		return "text-green-600 dark:text-green-400"
	case "degraded", "Degraded":
		return "text-yellow-600 dark:text-yellow-400"
	case "unhealthy", "Unhealthy":
		return "text-red-600 dark:text-red-400"
	default:
		return "text-muted-foreground"
	}
}

// formatDuration formats a duration into a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}

	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}

	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
	}

	days := hours / 24
	remainingHours := hours % 24

	return fmt.Sprintf("%dd %dh", days, remainingHours)
}

// formatTime formats a time for display.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "Never"
	}

	return t.Format("15:04:05")
}
