package ui

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/alpine"
	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/theme"
)

// DashboardPage renders the complete dashboard page
func DashboardPage(title, basePath string, enableRealtime, enableExport bool) g.Node {
	return html.HTML(
		html.Lang("en"),
		html.Head(
			theme.HeadContent(theme.DefaultLight(), theme.DefaultDark()),
			html.TitleEl(g.Text(title)),
			html.Script(html.Src("https://cdn.tailwindcss.com")),
			theme.TailwindConfigScript(),
			alpine.CloakCSS(),
			theme.StyleTag(theme.DefaultLight(), theme.DefaultDark()),
			html.StyleEl(g.Raw(`
				@layer base {
					* {
						@apply border-border;
					}
				}
			`)),
			ApexChartsScript(),
			ChartInitializationScript(),
		),
		html.Body(
			html.Class("min-h-screen bg-background text-foreground antialiased"),
			g.Attr("x-data", "{}"),

			// Initialize Alpine store
			html.Script(
				g.Raw(alpineStoreScript(basePath, enableRealtime)),
			),

			// Dark mode toggle observer
			theme.DarkModeScript(),
			html.Script(
				g.Raw(`
// Watch for dark mode changes and update charts
const observer = new MutationObserver(() => {
	if (window.updateChartThemes) {
		window.updateChartThemes();
	}
});
observer.observe(document.documentElement, {
	attributes: true,
	attributeFilter: ['class']
});
				`),
			),

			// Header
			DashboardHeader(title, basePath),

			// Main Content
			html.Main(
				html.Class("container py-6 md:py-8"),

				// Tabs
				html.Div(
					g.Attr("x-data", `{ activeTab: 'overview' }`),
					html.Class("space-y-6"),

					// Tab Navigation
					html.Div(
						html.Class("border-b"),
						html.Nav(
							html.Class("-mb-px flex space-x-8"),
							tabButton("overview", "Overview", icons.LayoutDashboard(icons.WithSize(16))),
							tabButton("metrics", "Metrics Report", icons.ChartBar(icons.WithSize(16))),
						),
					),

					// Overview Tab
					html.Div(
						g.Attr("x-show", "activeTab === 'overview'"),
						g.Attr("x-transition", ""),
						html.Class("space-y-6"),

						// Metrics Overview
						OverviewMetrics(basePath),

						// Charts
						ChartsSection(),

						// Health Checks Table
						HealthChecksTable(),

						// Services Section
						html.Div(
							html.Class("space-y-4"),
							html.Div(
								html.Class("flex items-center justify-between"),
								SectionHeader("Registered Services", "All services in the system"),
								g.If(enableExport, html.Div(
									html.Class("flex gap-2"),
									exportButton(basePath+"/export/json", "JSON", icons.FileCode(icons.WithSize(14))),
									exportButton(basePath+"/export/csv", "CSV", icons.FileText(icons.WithSize(14))),
									exportButton(basePath+"/export/prometheus", "Prometheus", icons.Activity(icons.WithSize(14))),
								)),
							),
							ServicesGrid(basePath),
						),
					),

					// Metrics Report Tab
					html.Div(
						g.Attr("x-show", "activeTab === 'metrics'"),
						g.Attr("x-transition", ""),
						g.Attr("x-init", "if (activeTab === 'metrics' && !$store.dashboard.metricsReport) { $store.dashboard.fetchMetricsReport(); }"),
						html.Class("space-y-6"),
						MetricsReportTable(),
						MetricsTypeDistributionChart(),
					),
				),
			),

			// Service Detail Modal
			ServiceDetailModal(),

			// Footer
			html.Footer(
				html.Class("mt-12 border-t py-6 text-center text-sm text-muted-foreground"),
				html.P(
					g.Text("Powered by Forge Dashboard | Last updated: "),
					html.Span(
						g.Attr("x-text", "$store.dashboard.data?.timestamp ? new Date($store.dashboard.data.timestamp).toLocaleString() : 'Never'"),
					),
				),
			),

			// Alpine.js scripts
			alpine.Scripts(),
		),
	)
}

// Helper functions for rendering UI elements

func tabButton(id, label string, icon g.Node) g.Node {
	return html.Button(
		g.Attr("@click", "activeTab = '"+id+"'"),
		g.Attr("x-bind:class", `activeTab === '`+id+`' ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground hover:border-border'`),
		html.Class("inline-flex items-center gap-2 border-b-2 py-4 px-1 text-sm font-medium transition-colors"),
		icon,
		g.Text(label),
	)
}

func exportButton(href, label string, icon g.Node) g.Node {
	return html.A(
		html.Href(href),
		html.Class("inline-flex items-center gap-1 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90"),
		icon,
		g.Text(label),
	)
}

func alpineStoreScript(basePath string, enableRealtime bool) string {
	wsConnection := "null"
	if enableRealtime {
		wsConnection = `
	connectWebSocket() {
		if (this.ws) return;
		
		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const wsUrl = protocol + '//' + window.location.host + '` + basePath + `/ws';
		
		this.ws = new WebSocket(wsUrl);
		
		this.ws.onopen = () => {
			console.log('WebSocket connected');
			this.connected = true;
			this.reconnectAttempts = 0;
		};
		
		this.ws.onmessage = (event) => {
			try {
				const message = JSON.parse(event.data);
				this.handleWSMessage(message);
			} catch (error) {
				console.error('Failed to parse WebSocket message:', error);
			}
		};
		
		this.ws.onerror = (error) => {
			console.error('WebSocket error:', error);
			this.connected = false;
		};
		
		this.ws.onclose = () => {
			console.log('WebSocket closed');
			this.connected = false;
			this.ws = null;
			this.reconnectWebSocket();
		};
	},
	
	reconnectWebSocket() {
		if (this.reconnectAttempts >= this.maxReconnectAttempts) {
			console.log('Max reconnection attempts reached');
			return;
		}
		
		this.reconnectAttempts++;
		const delay = this.reconnectDelay * this.reconnectAttempts;
		
		setTimeout(() => {
			console.log('Reconnecting WebSocket (attempt ' + this.reconnectAttempts + ')...');
			this.connectWebSocket();
		}, delay);
	},
	
	handleWSMessage(message) {
		switch (message.type) {
			case 'overview':
				this.data = message.data;
				break;
			case 'health':
				this.healthData = message.data;
				break;
			case 'metrics':
				// Update metrics if needed
				break;
			case 'services':
				if (this.data) {
					this.data.services = message.data;
				}
				break;
		}
	},`
	} else {
		wsConnection = `
	// WebSocket disabled
	connectWebSocket() {},
	reconnectWebSocket() {},
	handleWSMessage() {},`
	}

	return `
document.addEventListener('alpine:init', () => {
	Alpine.store('dashboard', {
		data: null,
		healthData: null,
		metricsReport: null,
		selectedService: null,
		loading: true,
		connected: false,
		ws: null,
		reconnectAttempts: 0,
		maxReconnectAttempts: 5,
		reconnectDelay: 2000,
		refreshInterval: null,
		
		init() {
			this.fetch();
			` + (func() string {
		if enableRealtime {
			return "this.connectWebSocket();"
		}
		return "// Start polling\nthis.startPolling();"
	})() + `
		},
		
		async fetch() {
			try {
				this.loading = true;
				const [overview, health, history] = await Promise.all([
					fetch('` + basePath + `/api/overview').then(r => r.json()),
					fetch('` + basePath + `/api/health').then(r => r.json()),
					fetch('` + basePath + `/api/history').then(r => r.json())
				]);
				
				this.data = overview;
				this.healthData = health;
				
				// Update charts
				if (window.updateCharts) {
					window.updateCharts(history);
				}
			} catch (error) {
				console.error('Failed to fetch dashboard data:', error);
			} finally {
				this.loading = false;
			}
		},
		
		async fetchMetricsReport() {
			try {
				const report = await fetch('` + basePath + `/api/metrics-report').then(r => r.json());
				this.metricsReport = report;
				
				// Update metrics type chart
				if (window.updateMetricsTypeChart) {
					window.updateMetricsTypeChart(report);
				}
			} catch (error) {
				console.error('Failed to fetch metrics report:', error);
			}
		},
		
		async showServiceDetail(serviceName) {
			try {
				const detail = await fetch('` + basePath + `/api/service-detail?name=' + encodeURIComponent(serviceName)).then(r => r.json());
				this.selectedService = detail;
			} catch (error) {
				console.error('Failed to fetch service detail:', error);
			}
		},
		
		hideServiceDetail() {
			this.selectedService = null;
		},
		
		refresh() {
			this.fetch();
		},
		
		startPolling() {
			// Poll every 30 seconds
			this.refreshInterval = setInterval(() => {
				this.fetch();
			}, 30000);
		},
		` + wsConnection + `
	});
});

// Helper function for duration formatting
window.formatDuration = function(ns) {
	if (!ns) return '--';
	const ms = ns / 1000000;
	if (ms < 1000) return ms.toFixed(0) + 'ms';
	const seconds = ms / 1000;
	if (seconds < 60) return seconds.toFixed(1) + 's';
	const minutes = Math.floor(seconds / 60);
	const hours = Math.floor(minutes / 60);
	const days = Math.floor(hours / 24);
	if (days > 0) return days + 'd ' + (hours % 24) + 'h';
	if (hours > 0) return hours + 'h ' + (minutes % 60) + 'm';
	return minutes + 'm';
};
	`
}
