package pages

import (
	"context"
	"strconv"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/components/badge"
	"github.com/xraph/forgeui/components/card"
	"github.com/xraph/forgeui/icons"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/ui"
)

// HealthPage renders the health checks page with service statuses.
func HealthPage(ctx context.Context, c *collector.DataCollector) (g.Node, error) {
	health := c.CollectHealth(ctx)

	return html.Div(
		html.Class("space-y-6"),

		// Page header
		html.Div(
			html.Class("flex items-center justify-between"),
			ui.SectionHeader("Health Checks", "Service health status and diagnostics"),
			ui.StatusBadge(health.OverallStatus),
		),

		// Summary row
		healthSummaryRow(health.Summary),

		// Health checks table
		healthChecksTable(health),
	), nil
}

// healthSummaryRow renders the summary stats row.
func healthSummaryRow(summary collector.HealthSummary) g.Node {
	return html.Div(
		html.Class("grid gap-4 md:grid-cols-4"),

		healthStatCard("Healthy", summary.Healthy, "bg-green-500/10 text-green-600 dark:text-green-400"),
		healthStatCard("Degraded", summary.Degraded, "bg-yellow-500/10 text-yellow-600 dark:text-yellow-400"),
		healthStatCard("Unhealthy", summary.Unhealthy, "bg-red-500/10 text-red-600 dark:text-red-400"),
		healthStatCard("Total", summary.Total, "bg-primary/10 text-primary"),
	)
}

// healthStatCard renders a small stat card for health counts.
func healthStatCard(label string, count int, colorClass string) g.Node {
	return card.Card(
		card.Content(
			html.Div(
				html.Class("flex items-center gap-4"),
				html.Div(
					html.Class("flex h-10 w-10 items-center justify-center rounded-lg "+colorClass),
					html.Span(
						html.Class("text-lg font-bold"),
						g.Text(strconv.Itoa(count)),
					),
				),
				html.Span(
					html.Class("text-sm text-muted-foreground"),
					g.Text(label),
				),
			),
		),
	)
}

// healthChecksTable renders the full health checks table with server-rendered data.
func healthChecksTable(health *collector.HealthData) g.Node {
	if len(health.Services) == 0 {
		return card.Card(
			card.Content(
				ui.EmptyState(
					icons.Activity(icons.WithSize(32)),
					"No Health Checks",
					"No health check data available yet.",
				),
			),
		)
	}

	rows := make([]g.Node, 0, len(health.Services))

	for name, svc := range health.Services {
		rows = append(rows, healthRow(name, svc))
	}

	return card.Card(
		card.Header(
			html.Div(
				html.Class("flex items-center justify-between"),
				card.Title("Service Health Details"),
				html.Span(
					html.Class("text-xs text-muted-foreground"),
					g.Text("Last checked: "+formatTime(health.CheckedAt)),
				),
			),
		),
		card.Content(
			html.Div(
				html.Class("overflow-x-auto"),
				html.Table(
					html.Class("w-full text-sm"),
					html.THead(
						html.Class("border-b"),
						html.Tr(
							html.Class("text-left"),
							html.Th(html.Class("pb-3 font-medium text-muted-foreground"), g.Text("Service")),
							html.Th(html.Class("pb-3 font-medium text-muted-foreground"), g.Text("Status")),
							html.Th(html.Class("pb-3 font-medium text-muted-foreground"), g.Text("Message")),
							html.Th(html.Class("pb-3 font-medium text-muted-foreground"), g.Text("Duration")),
							html.Th(html.Class("pb-3 font-medium text-muted-foreground"), g.Text("Critical")),
						),
					),
					html.TBody(
						html.Class("divide-y"),
						g.Group(rows),
					),
				),
			),
		),
	)
}

// healthRow renders a single health check table row.
func healthRow(name string, svc collector.ServiceHealth) g.Node {
	statusVariant := statusBadgeVariant(svc.Status)

	return html.Tr(
		html.Class("hover:bg-muted/50 transition-colors"),
		html.Td(
			html.Class("py-3 font-medium"),
			g.Text(name),
		),
		html.Td(
			html.Class("py-3"),
			badge.Badge(svc.Status, badge.WithVariant(statusVariant)),
		),
		html.Td(
			html.Class("py-3 text-muted-foreground max-w-xs truncate"),
			g.Text(defaultText(svc.Message, "-")),
		),
		html.Td(
			html.Class("py-3 text-muted-foreground"),
			g.Text(svc.Duration.String()),
		),
		html.Td(
			html.Class("py-3"),
			g.If(svc.Critical,
				badge.Badge("Critical", badge.WithVariant(forgeui.VariantDestructive)),
			),
			g.If(!svc.Critical,
				html.Span(html.Class("text-muted-foreground"), g.Text("-")),
			),
		),
	)
}

// statusBadgeVariant returns the badge variant for a health status.
func statusBadgeVariant(status string) forgeui.Variant {
	switch status {
	case "healthy", "Healthy":
		return forgeui.VariantDefault
	case "degraded", "Degraded":
		return forgeui.VariantSecondary
	case "unhealthy", "Unhealthy":
		return forgeui.VariantDestructive
	default:
		return forgeui.VariantOutline
	}
}

// defaultText returns the value if non-empty, else the fallback.
func defaultText(val, fallback string) string {
	if val == "" {
		return fallback
	}

	return val
}
