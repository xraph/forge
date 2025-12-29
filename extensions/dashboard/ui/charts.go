package ui

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/components/card"
)

// ChartPlaceholder renders a placeholder for charts with ApexCharts integration
func ChartPlaceholder(id, title, description string) g.Node {
	return card.Card(
		card.Header(
			card.Title(title),
			g.If(description != "", card.Description(description)),
		),
		card.Content(
			html.Div(
				html.ID(id),
				html.Class("h-64 md:h-80"),
				g.Attr("x-data", "{}"),
				g.Attr("x-init", "initChart('"+id+"')"),
			),
		),
	)
}

// HealthHistoryChart renders a health history chart
func HealthHistoryChart() g.Node {
	return ChartPlaceholder(
		"health-chart",
		"Service Health History",
		"Historical health check trends",
	)
}

// ServicesCountChart renders a services count chart
func ServicesCountChart() g.Node {
	return ChartPlaceholder(
		"services-chart",
		"Service Count History",
		"Total and healthy services over time",
	)
}

// MetricsTypeDistributionChart renders a metrics type distribution pie chart
func MetricsTypeDistributionChart() g.Node {
	return card.Card(
		card.Header(
			card.Title("Metrics by Type"),
			card.Description("Distribution of metric types"),
		),
		card.Content(
			html.Div(
				html.ID("metrics-type-chart"),
				html.Class("h-64"),
				g.Attr("x-data", "{}"),
				g.Attr("x-init", "initMetricsTypeChart()"),
			),
		),
	)
}

// ChartsSection renders a grid of charts
func ChartsSection() g.Node {
	return html.Div(
		html.Class("grid gap-6 md:grid-cols-2"),
		HealthHistoryChart(),
		ServicesCountChart(),
	)
}

// ApexChartsScript returns the script tag for ApexCharts
func ApexChartsScript() g.Node {
	return html.Script(
		html.Src("https://cdn.jsdelivr.net/npm/apexcharts@3.45.0/dist/apexcharts.min.js"),
	)
}

// ChartInitializationScript returns the Alpine.js chart initialization logic
func ChartInitializationScript() g.Node {
	return html.Script(
		g.Raw(`
// Chart instances
window.charts = {
	health: null,
	services: null,
	metricsType: null
};

// Initialize charts
window.initChart = function(chartId) {
	const isDark = document.documentElement.classList.contains('dark');
	const theme = isDark ? 'dark' : 'light';
	
	const commonOptions = {
		chart: {
			type: 'line',
			height: 320,
			toolbar: { show: false },
			animations: { enabled: true }
		},
		theme: { mode: theme },
		stroke: { curve: 'smooth', width: 2 },
		grid: { borderColor: isDark ? '#374151' : '#e5e7eb' },
		xaxis: {
			type: 'datetime',
			labels: { style: { colors: isDark ? '#9ca3af' : '#6b7280' } }
		},
		yaxis: {
			labels: { style: { colors: isDark ? '#9ca3af' : '#6b7280' } }
		},
		tooltip: { theme: theme }
	};
	
	if (chartId === 'health-chart') {
		const options = {
			...commonOptions,
			series: [
				{ name: 'Healthy', data: [] },
				{ name: 'Degraded', data: [] },
				{ name: 'Unhealthy', data: [] }
			],
			colors: ['#10b981', '#f59e0b', '#ef4444']
		};
		window.charts.health = new ApexCharts(document.querySelector('#' + chartId), options);
		window.charts.health.render();
	} else if (chartId === 'services-chart') {
		const options = {
			...commonOptions,
			series: [
				{ name: 'Total Services', data: [] },
				{ name: 'Healthy Services', data: [] }
			],
			colors: ['#3b82f6', '#10b981']
		};
		window.charts.services = new ApexCharts(document.querySelector('#' + chartId), options);
		window.charts.services.render();
	}
};

window.initMetricsTypeChart = function() {
	const isDark = document.documentElement.classList.contains('dark');
	const options = {
		chart: {
			type: 'pie',
			height: 256
		},
		theme: { mode: isDark ? 'dark' : 'light' },
		series: [],
		labels: [],
		colors: ['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6']
	};
	window.charts.metricsType = new ApexCharts(document.querySelector('#metrics-type-chart'), options);
	window.charts.metricsType.render();
};

// Update charts with new data
window.updateCharts = function(historyData) {
	if (!historyData || !historyData.series) return;
	
	// Update services chart
	const serviceSeries = historyData.series.filter(s => 
		s.name === 'service_count' || s.name === 'healthy_services'
	);
	
	if (serviceSeries.length > 0 && window.charts.services) {
		const seriesData = serviceSeries.map(series => ({
			name: series.name === 'service_count' ? 'Total Services' : 'Healthy Services',
			data: series.points.map(p => ({ x: new Date(p.timestamp), y: p.value }))
		}));
		window.charts.services.updateSeries(seriesData);
	}
};

window.updateMetricsTypeChart = function(metricsReport) {
	if (!metricsReport || !metricsReport.metrics_by_type || !window.charts.metricsType) return;
	
	const data = metricsReport.metrics_by_type;
	window.charts.metricsType.updateOptions({
		series: Object.values(data),
		labels: Object.keys(data)
	});
};

// Update theme on all charts
window.updateChartThemes = function() {
	const isDark = document.documentElement.classList.contains('dark');
	const theme = isDark ? 'dark' : 'light';
	
	Object.values(window.charts).forEach(chart => {
		if (chart) {
			chart.updateOptions({ theme: { mode: theme } });
		}
	});
};
		`),
	)
}
