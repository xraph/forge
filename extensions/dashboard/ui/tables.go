package ui

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/components/card"
	"github.com/xraph/forgeui/icons"
)

// HealthChecksTable renders the health checks table with Alpine.js integration.
func HealthChecksTable() g.Node {
	return card.Card(
		card.Header(
			html.Div(
				html.Class("flex items-center justify-between"),
				html.Div(
					html.Class("text-lg font-semibold"),
					g.Text("Health Checks"),
				),
				html.Span(
					html.Class("text-xs text-muted-foreground"),
					g.Attr("x-data", "{}"),
					g.Attr("x-text", "'Last checked: ' + ($store.dashboard.data?.last_check ? new Date($store.dashboard.data.last_check).toLocaleTimeString() : 'Never')"),
				),
			),
		),
		card.Content(
			html.Div(
				g.Attr("x-data", "{}"),
				html.Class("relative overflow-x-auto"),

				// Loading state
				html.Div(
					g.Attr("x-show", "$store.dashboard.loading"),
					LoadingSpinner("Loading health checks..."),
				),

				// Table
				html.Div(
					g.Attr("x-show", "!$store.dashboard.loading && $store.dashboard.healthData"),
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
								html.Th(html.Class("pb-3 font-medium text-muted-foreground"), g.Text("Checked At")),
							),
						),
						html.TBody(
							html.Class("divide-y"),
							// Rendered by Alpine.js
							html.TBody(
								g.Attr("x-html", `
Object.entries($store.dashboard.healthData?.services || {}).map(([name, service]) => {
	const statusClass = service.status.toLowerCase() === 'healthy' ? 'bg-green-100 text-green-800 border-green-200' :
		service.status.toLowerCase() === 'degraded' ? 'bg-yellow-100 text-yellow-800 border-yellow-200' :
		service.status.toLowerCase() === 'unhealthy' ? 'bg-red-100 text-red-800 border-red-200' :
		'bg-gray-100 text-gray-800 border-gray-200';
	return '<tr class="hover:bg-muted/50 transition-colors">' +
		'<td class="py-3 font-medium">' + name + '</td>' +
		'<td class="py-3"><span class="inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold ' + statusClass + '">' + service.status + '</span></td>' +
		'<td class="py-3 text-muted-foreground max-w-xs truncate">' + (service.message || '-') + '</td>' +
		'<td class="py-3 text-muted-foreground">' + formatDuration(service.duration) + '</td>' +
		'<td class="py-3 text-muted-foreground">' + (service.timestamp ? new Date(service.timestamp).toLocaleTimeString() : '-') + '</td>' +
		'</tr>';
}).join('')
								`),
							),
						),
					),
				),

				// Empty state
				html.Div(
					g.Attr("x-show", "!$store.dashboard.loading && (!$store.dashboard.healthData || Object.keys($store.dashboard.healthData?.services || {}).length === 0)"),
					html.Div(
						html.Class("py-8"),
						EmptyState(
							icons.Activity(icons.WithSize(32)),
							"No Health Checks",
							"No health check data available.",
						),
					),
				),
			),
		),
	)
}

// ServiceDetailModal renders a modal with detailed service information.
func ServiceDetailModal() g.Node {
	return html.Div(
		g.Attr("x-data", "{}"),
		g.Attr("x-show", "$store.dashboard.selectedService !== null"),
		g.Attr("x-transition:enter", "transition ease-out duration-200"),
		g.Attr("x-transition:enter-start", "opacity-0"),
		g.Attr("x-transition:enter-end", "opacity-100"),
		g.Attr("x-transition:leave", "transition ease-in duration-150"),
		g.Attr("x-transition:leave-start", "opacity-100"),
		g.Attr("x-transition:leave-end", "opacity-0"),
		html.Class("fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"),
		g.Attr("@click.self", "$store.dashboard.hideServiceDetail()"),

		// Modal content
		html.Div(
			html.Class("relative w-full max-w-2xl max-h-[90vh] overflow-y-auto rounded-lg border bg-background p-0 shadow-lg"),
			g.Attr("x-show", "$store.dashboard.selectedService"),
			g.Attr("x-transition:enter", "transition ease-out duration-200"),
			g.Attr("x-transition:enter-start", "opacity-0 scale-95"),
			g.Attr("x-transition:enter-end", "opacity-100 scale-100"),
			g.Attr("x-transition:leave", "transition ease-in duration-150"),
			g.Attr("x-transition:leave-start", "opacity-100 scale-100"),
			g.Attr("x-transition:leave-end", "opacity-0 scale-95"),
			g.Attr("@click.stop", ""),

			// Header
			html.Div(
				html.Class("sticky top-0 z-10 flex items-center justify-between border-b bg-background px-6 py-4"),
				html.H2(
					html.Class("text-xl font-semibold"),
					g.Attr("x-text", "$store.dashboard.selectedService?.name || 'Service Details'"),
				),
				html.Button(
					html.Class("rounded-md p-2 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"),
					g.Attr("@click", "$store.dashboard.hideServiceDetail()"),
					icons.X(icons.WithSize(20)),
				),
			),

			// Body
			html.Div(
				html.Class("p-6 space-y-6"),

				// Status section
				html.Div(
					html.Class("space-y-2"),
					html.H3(
						html.Class("text-sm font-medium text-muted-foreground"),
						g.Text("Status"),
					),
					html.Div(
						html.Class("flex items-center gap-2"),
						html.Span(
							html.Class("inline-flex items-center rounded-full border px-3 py-1 text-sm font-semibold"),
							g.Attr("x-bind:class", `{
								'bg-green-100 text-green-800 border-green-200': $store.dashboard.selectedService?.status?.toLowerCase() === 'healthy',
								'bg-yellow-100 text-yellow-800 border-yellow-200': $store.dashboard.selectedService?.status?.toLowerCase() === 'degraded',
								'bg-red-100 text-red-800 border-red-200': $store.dashboard.selectedService?.status?.toLowerCase() === 'unhealthy'
							}`),
							g.Attr("x-text", "$store.dashboard.selectedService?.status || 'Unknown'"),
						),
					),
				),

				// Type section
				html.Div(
					html.Class("space-y-2"),
					html.H3(
						html.Class("text-sm font-medium text-muted-foreground"),
						g.Text("Type"),
					),
					html.P(
						html.Class("text-lg font-semibold"),
						g.Attr("x-text", "$store.dashboard.selectedService?.type || 'N/A'"),
					),
				),

				// Last health check
				html.Div(
					html.Class("space-y-2"),
					html.H3(
						html.Class("text-sm font-medium text-muted-foreground"),
						g.Text("Last Health Check"),
					),
					html.P(
						html.Class("text-lg font-semibold"),
						g.Attr("x-text", "$store.dashboard.selectedService?.last_health_check ? new Date($store.dashboard.selectedService.last_health_check).toLocaleString() : 'N/A'"),
					),
				),

				// Health details
				html.Div(
					g.Attr("x-show", "$store.dashboard.selectedService?.health"),
					html.Class("space-y-2"),
					html.H3(
						html.Class("text-sm font-medium text-muted-foreground"),
						g.Text("Health Details"),
					),
					html.Div(
						html.Class("rounded-lg bg-muted p-4"),
						html.P(
							html.Class("text-sm whitespace-pre-wrap"),
							g.Attr("x-text", "$store.dashboard.selectedService?.health?.message || 'No message'"),
						),
					),
				),

				// Metrics section
				html.Div(
					html.Class("space-y-2"),
					html.H3(
						html.Class("text-sm font-medium text-muted-foreground"),
						g.Attr("x-text", "'Metrics (' + (Object.keys($store.dashboard.selectedService?.metrics || {}).length) + ')'"),
					),
					html.Div(
						g.Attr("x-show", "Object.keys($store.dashboard.selectedService?.metrics || {}).length > 0"),
						html.Class("max-h-96 overflow-y-auto"),
						html.Div(
							g.Attr("x-html", `
Object.entries($store.dashboard.selectedService?.metrics || {}).map(([key, value]) => {
	return '<div class="mb-2 flex items-center justify-between rounded-lg bg-muted p-3">' +
		'<span class="text-sm font-mono font-medium">' + key + '</span>' +
		'<span class="text-sm font-semibold">' + JSON.stringify(value) + '</span></div>';
}).join('')
							`),
						),
					),
					html.P(
						g.Attr("x-show", "Object.keys($store.dashboard.selectedService?.metrics || {}).length === 0"),
						html.Class("text-sm text-muted-foreground text-center py-4"),
						g.Text("No metrics available for this service"),
					),
				),
			),
		),
	)
}

// MetricsReportTable renders a comprehensive metrics report.
func MetricsReportTable() g.Node {
	return html.Div(
		g.Attr("x-data", "{}"),
		html.Class("space-y-6"),

		// Statistics overview
		card.Card(
			card.Header(
				html.Div(
					html.Class("text-lg font-semibold"),
					g.Text("Metrics Statistics"),
				),
			),
			card.Content(
				html.Div(
					html.Class("grid gap-4 md:grid-cols-4"),
					g.Attr("x-show", "$store.dashboard.metricsReport"),

					// Stat cards rendered by Alpine
					html.Div(
						g.Attr("x-html", `
[{label: 'Total Metrics', key: 'total_metrics'}, {label: 'Counters', key: 'counter'}, {label: 'Gauges', key: 'gauge'}, {label: 'Histograms', key: 'histogram'}].map(stat => {
	const value = stat.key === 'total_metrics' ? ($store.dashboard.metricsReport?.total_metrics || 0) : ($store.dashboard.metricsReport?.metrics_by_type?.[stat.key] || 0);
	return '<div class="text-center space-y-1 rounded-lg bg-muted p-4">' +
		'<p class="text-sm text-muted-foreground">' + stat.label + '</p>' +
		'<p class="text-2xl font-bold">' + value + '</p></div>';
}).join('')
						`),
					),
				),
			),
		),

		// Active collectors table
		card.Card(
			card.Header(
				html.Div(
					html.Class("text-lg font-semibold"),
					g.Text("Active Collectors"),
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
								html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Name")),
								html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Type")),
								html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Metrics")),
								html.Th(html.Class("pb-3 text-left font-medium text-muted-foreground"), g.Text("Status")),
							),
						),
						html.TBody(
							html.Class("divide-y"),
							html.Div(
								g.Attr("x-html", `
($store.dashboard.metricsReport?.collectors || []).map(collector => {
	return '<tr class="hover:bg-muted/50">' +
		'<td class="py-3">' + collector.name + '</td>' +
		'<td class="py-3 text-muted-foreground">' + collector.type + '</td>' +
		'<td class="py-3 text-muted-foreground">' + collector.metrics_count + '</td>' +
		'<td class="py-3"><span class="inline-flex items-center rounded-full bg-green-100 px-2.5 py-0.5 text-xs font-semibold text-green-800">' + collector.status + '</span></td>' +
		'</tr>';
}).join('')
								`),
							),
						),
					),
				),
			),
		),

		// Top metrics
		card.Card(
			card.Header(
				html.Div(
					html.Class("text-lg font-semibold"),
					g.Text("Top Metrics"),
				),
			),
			card.Content(
				html.Div(
					html.Class("space-y-2"),
					html.Div(
						g.Attr("x-html", `
($store.dashboard.metricsReport?.top_metrics || []).slice(0, 10).map(metric => {
	return '<div class="flex items-center justify-between rounded-lg bg-muted p-3">' +
		'<div><span class="text-sm font-medium">' + metric.name + '</span>' +
		'<span class="ml-2 text-xs text-muted-foreground">(' + metric.type + ')</span></div>' +
		'<span class="font-mono text-sm font-semibold">' + JSON.stringify(metric.value) + '</span></div>';
}).join('')
						`),
					),
				),
			),
		),
	)
}
