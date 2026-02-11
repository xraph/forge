package ui

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"
)

// GatewayHeader renders the gateway dashboard header.
func GatewayHeader(title string) g.Node {
	return html.Header(
		html.Class("sticky top-0 z-50 w-full border-b bg-background/95 backdrop-blur"),
		html.Div(
			html.Class("container flex h-14 items-center justify-between"),
			html.Div(
				html.Class("flex items-center gap-3"),
				// Gateway icon
				html.Div(
					html.Class("flex items-center justify-center w-8 h-8 rounded-md bg-primary text-primary-foreground"),
					g.Raw(`<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>`),
				),
				html.H1(
					html.Class("text-lg font-semibold"),
					g.Text(title),
				),
			),
			html.Div(
				html.Class("flex items-center gap-4 text-sm text-muted-foreground"),
				html.Span(
					g.Attr("x-text", "$store.gw.stats ? $store.gw.stats.totalRoutes + ' routes' : '—'"),
				),
				html.Span(g.Text("|")),
				html.Span(
					g.Attr("x-text", "$store.gw.stats ? $store.gw.stats.healthyUpstreams + '/' + $store.gw.stats.totalUpstreams + ' healthy' : '—'"),
				),
			),
		),
	)
}

// StatCard renders a statistics card.
func StatCard(label, xText string) g.Node {
	return html.Div(
		html.Class("rounded-lg border bg-card p-4 shadow-sm"),
		html.Div(
			html.Class("text-sm text-muted-foreground"),
			g.Text(label),
		),
		html.Div(
			html.Class("text-2xl font-bold mt-1"),
			g.Attr("x-text", xText),
		),
	)
}

// StatusBadge renders a status badge.
func StatusBadge(xCondition string) g.Node {
	return html.Span(
		g.Attr(":class", xCondition+" ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' : 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'"),
		html.Class("px-2 py-1 rounded text-xs font-medium"),
		g.Attr("x-text", xCondition+" ? 'Healthy' : 'Unhealthy'"),
	)
}

// ProtocolBadge renders a protocol type badge.
func ProtocolBadge() g.Node {
	return html.Span(
		html.Class("px-2 py-1 rounded text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200"),
		g.Attr("x-text", "route.protocol"),
	)
}

// OverviewPanel renders the overview tab panel.
func OverviewPanel() g.Node {
	return html.Div(
		g.Attr("x-show", "activeTab === 'overview'"),
		g.Attr("x-cloak", ""),
		html.Class("space-y-6"),

		// Stat cards
		html.Div(
			html.Class("grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4"),
			StatCard("Total Requests", "$store.gw.stats ? $store.gw.stats.totalRequests.toLocaleString() : '—'"),
			StatCard("Error Rate", "$store.gw.stats && $store.gw.stats.totalRequests ? (($store.gw.stats.totalErrors / $store.gw.stats.totalRequests) * 100).toFixed(2) + '%' : '0%'"),
			StatCard("Avg Latency", "$store.gw.stats ? $store.gw.formatLatency($store.gw.stats.avgLatencyMs) : '—'"),
			StatCard("Healthy Upstreams", "$store.gw.stats ? $store.gw.stats.healthyUpstreams + '/' + $store.gw.stats.totalUpstreams : '—'"),
		),

		// Secondary stats
		html.Div(
			html.Class("grid grid-cols-1 md:grid-cols-3 gap-4"),
			StatCard("Cache Hits", "$store.gw.stats ? $store.gw.stats.cacheHits.toLocaleString() : '0'"),
			StatCard("Rate Limited", "$store.gw.stats ? $store.gw.stats.rateLimited.toLocaleString() : '0'"),
			StatCard("Circuit Breaks", "$store.gw.stats ? $store.gw.stats.circuitBreaks.toLocaleString() : '0'"),
		),
	)
}

// RoutesPanel renders the routes tab panel.
func RoutesPanel() g.Node {
	return html.Div(
		g.Attr("x-show", "activeTab === 'routes'"),
		g.Attr("x-cloak", ""),

		html.Div(
			html.Class("rounded-lg border bg-card shadow-sm overflow-hidden"),
			html.Table(
				html.Class("min-w-full divide-y divide-border"),
				html.THead(
					html.Class("bg-muted/50"),
					html.Tr(
						tableHeader("Path"),
						tableHeader("Methods"),
						tableHeader("Protocol"),
						tableHeader("Source"),
						tableHeader("Targets"),
						tableHeader("Status"),
					),
				),
				html.TBody(
					html.Class("divide-y divide-border"),
					html.Template(
						g.Attr("x-for", "route in $store.gw.routes"),
						g.Attr(":key", "route.id"),
						html.Tr(
							html.Class("hover:bg-muted/50"),
							html.Td(html.Class("px-4 py-3 text-sm font-mono"), g.Attr("x-text", "route.path")),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "route.methods ? route.methods.join(', ') : 'ALL'")),
							html.Td(html.Class("px-4 py-3 text-sm"), ProtocolBadge()),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "route.source")),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "route.targets ? route.targets.length : 0")),
							html.Td(html.Class("px-4 py-3 text-sm"),
								html.Span(
									g.Attr(":class", "route.enabled ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' : 'bg-muted text-muted-foreground'"),
									html.Class("px-2 py-1 rounded text-xs font-medium"),
									g.Attr("x-text", "route.enabled ? 'Active' : 'Disabled'"),
								),
							),
						),
					),
				),
			),
		),
	)
}

// UpstreamsPanel renders the upstreams tab panel.
func UpstreamsPanel() g.Node {
	return html.Div(
		g.Attr("x-show", "activeTab === 'upstreams'"),
		g.Attr("x-cloak", ""),

		html.Div(
			html.Class("rounded-lg border bg-card shadow-sm overflow-hidden"),
			html.Table(
				html.Class("min-w-full divide-y divide-border"),
				html.THead(
					html.Class("bg-muted/50"),
					html.Tr(
						tableHeader("URL"),
						tableHeader("Route"),
						tableHeader("Health"),
						tableHeader("Circuit"),
						tableHeader("Conns"),
						tableHeader("Requests"),
						tableHeader("Errors"),
						tableHeader("Latency"),
					),
				),
				html.TBody(
					html.Class("divide-y divide-border"),
					html.Template(
						g.Attr("x-for", "u in $store.gw.upstreams"),
						g.Attr(":key", "u.id"),
						html.Tr(
							html.Class("hover:bg-muted/50"),
							html.Td(html.Class("px-4 py-3 text-sm font-mono"), g.Attr("x-text", "u.url")),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "u.routePath")),
							html.Td(html.Class("px-4 py-3 text-sm"), StatusBadge("u.healthy")),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "u.circuitState")),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "u.activeConns")),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "u.totalRequests.toLocaleString()")),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "u.totalErrors.toLocaleString()")),
							html.Td(html.Class("px-4 py-3 text-sm"), g.Attr("x-text", "$store.gw.formatLatency(u.avgLatencyMs)")),
						),
					),
				),
			),
		),
	)
}

// ServicesPanel renders the discovered services tab panel.
func ServicesPanel() g.Node {
	return html.Div(
		g.Attr("x-show", "activeTab === 'services'"),
		g.Attr("x-cloak", ""),
		html.Class("space-y-4"),

		html.Div(
			html.Class("grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4"),
			html.Template(
				g.Attr("x-for", "svc in $store.gw.services"),
				g.Attr(":key", "svc.name"),
				html.Div(
					html.Class("rounded-lg border bg-card p-4 shadow-sm"),
					html.Div(
						html.Class("flex items-center justify-between mb-3"),
						html.H3(html.Class("text-lg font-semibold"), g.Attr("x-text", "svc.name")),
						StatusBadge("svc.healthy"),
					),
					html.Div(
						html.Class("space-y-1 text-sm text-muted-foreground"),
						serviceInfoRow("Version", "svc.version || '—'"),
						serviceInfoRow("Address", "svc.address + ':' + svc.port"),
						serviceInfoRow("Protocols", "svc.protocols ? svc.protocols.join(', ') : '—'"),
						serviceInfoRow("Routes", "svc.routeCount"),
					),
				),
			),
		),

		// Empty state
		html.Div(
			g.Attr("x-show", "$store.gw.services.length === 0"),
			html.Class("text-center py-12 text-muted-foreground"),
			html.P(g.Text("No discovered services")),
			html.P(html.Class("text-sm"), g.Text("Enable FARP discovery to auto-detect services")),
		),
	)
}

func tableHeader(label string) g.Node {
	return html.Th(
		html.Class("px-4 py-3 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider"),
		g.Text(label),
	)
}

func serviceInfoRow(label, xText string) g.Node {
	return html.Div(
		g.Text(label+": "),
		html.Span(
			html.Class("text-foreground"),
			g.Attr("x-text", xText),
		),
	)
}
