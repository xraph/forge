package pages

import (
	"context"
	"fmt"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/components/badge"
	"github.com/xraph/forgeui/components/card"
	"github.com/xraph/forgeui/icons"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/ui"
)

// MetricsPage renders the metrics report page.
func MetricsPage(ctx context.Context, c *collector.DataCollector) (g.Node, error) {
	report := c.CollectMetricsReport(ctx)

	return html.Div(
		html.Class("space-y-6"),

		// Page header
		ui.SectionHeader("Metrics", "Comprehensive metrics overview and statistics"),

		// Stats row
		metricsStatsRow(report),

		// Metrics by type + chart
		html.Div(
			html.Class("grid gap-6 lg:grid-cols-2"),
			metricsByTypeCard(report),
			ui.MetricsTypeDistributionChart(),
		),

		// Active collectors
		collectorsTable(report),

		// Top metrics
		topMetricsCard(report),
	), nil
}

// metricsStatsRow renders the top-level metrics stats.
func metricsStatsRow(report *collector.MetricsReport) g.Node {
	return html.Div(
		html.Class("grid gap-4 md:grid-cols-4"),

		ui.StatCard(
			icons.ChartBar(icons.WithSize(20)),
			fmt.Sprintf("%d", report.TotalMetrics),
			"Total Metrics",
			"",
			false,
		),
		ui.StatCard(
			icons.Activity(icons.WithSize(20)),
			fmt.Sprintf("%d", len(report.Collectors)),
			"Active Collectors",
			"",
			false,
		),
		ui.StatCard(
			icons.RefreshCw(icons.WithSize(20)),
			fmt.Sprintf("%d", report.Stats.TotalCollections),
			"Total Collections",
			"",
			false,
		),
		ui.StatCard(
			icons.TriangleAlert(icons.WithSize(20)),
			fmt.Sprintf("%d", report.Stats.ErrorCount),
			"Collection Errors",
			"",
			false,
		),
	)
}

// metricsByTypeCard renders the metrics breakdown by type.
func metricsByTypeCard(report *collector.MetricsReport) g.Node {
	rows := make([]g.Node, 0, len(report.MetricsByType))

	for typeName, count := range report.MetricsByType {
		rows = append(rows, html.Div(
			html.Class("flex items-center justify-between rounded-lg bg-muted p-3"),
			html.Span(
				html.Class("text-sm font-medium capitalize"),
				g.Text(typeName),
			),
			badge.Badge(fmt.Sprintf("%d", count), badge.WithVariant(forgeui.VariantSecondary)),
		))
	}

	if len(rows) == 0 {
		rows = append(rows, html.Div(
			html.Class("text-center text-sm text-muted-foreground py-4"),
			g.Text("No metrics collected yet"),
		))
	}

	return card.Card(
		card.Header(card.Title("Metrics by Type")),
		card.Content(
			html.Div(
				html.Class("space-y-2"),
				g.Group(rows),
			),
		),
	)
}

// collectorsTable renders the active collectors table.
func collectorsTable(report *collector.MetricsReport) g.Node {
	if len(report.Collectors) == 0 {
		return card.Card(
			card.Header(card.Title("Active Collectors")),
			card.Content(
				ui.EmptyState(
					icons.Server(icons.WithSize(32)),
					"No Collectors",
					"No active metrics collectors found.",
				),
			),
		)
	}

	rows := make([]g.Node, 0, len(report.Collectors))
	for _, col := range report.Collectors {
		rows = append(rows, html.Tr(
			html.Class("hover:bg-muted/50 transition-colors"),
			html.Td(html.Class("py-3 font-medium"), g.Text(col.Name)),
			html.Td(html.Class("py-3 text-muted-foreground"), g.Text(col.Type)),
			html.Td(html.Class("py-3 text-muted-foreground"), g.Text(fmt.Sprintf("%d", col.MetricsCount))),
			html.Td(
				html.Class("py-3"),
				badge.Badge(col.Status, badge.WithVariant(collectorStatusVariant(col.Status))),
			),
			html.Td(
				html.Class("py-3 text-muted-foreground text-xs"),
				g.Text(formatTime(col.LastCollection)),
			),
		))
	}

	return card.Card(
		card.Header(card.Title("Active Collectors")),
		card.Content(
			html.Div(
				html.Class("overflow-x-auto"),
				html.Table(
					html.Class("w-full text-sm"),
					html.THead(
						html.Class("border-b"),
						html.Tr(
							html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Name")),
							html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Type")),
							html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Metrics")),
							html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Status")),
							html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Last Collection")),
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

// collectorStatusVariant returns a badge variant for a collector status.
func collectorStatusVariant(status string) forgeui.Variant {
	switch status {
	case "healthy", "Healthy", "active", "Active":
		return forgeui.VariantDefault
	case "degraded", "Degraded":
		return forgeui.VariantSecondary
	case "unhealthy", "Unhealthy", "error", "Error":
		return forgeui.VariantDestructive
	default:
		return forgeui.VariantOutline
	}
}

// topMetricsCard renders the top metrics list.
func topMetricsCard(report *collector.MetricsReport) g.Node {
	if len(report.TopMetrics) == 0 {
		return g.Group(nil)
	}

	items := make([]g.Node, 0, len(report.TopMetrics))
	limit := len(report.TopMetrics)

	if limit > 10 {
		limit = 10
	}

	for _, metric := range report.TopMetrics[:limit] {
		items = append(items, html.Div(
			html.Class("flex items-center justify-between rounded-lg bg-muted p-3"),
			html.Div(
				html.Class("flex items-center gap-2"),
				html.Span(html.Class("text-sm font-mono font-medium"), g.Text(metric.Name)),
				badge.Badge(metric.Type, badge.WithVariant(forgeui.VariantOutline)),
			),
			html.Span(
				html.Class("font-mono text-sm font-semibold"),
				g.Text(fmt.Sprintf("%v", metric.Value)),
			),
		))
	}

	return card.Card(
		card.Header(card.Title("Top Metrics")),
		card.Content(
			html.Div(
				html.Class("space-y-2"),
				g.Group(items),
			),
		),
	)
}
