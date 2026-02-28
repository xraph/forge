package ui

// healthChecksTableRowsJS returns the Alpine.js x-html expression for health check table rows.
func healthChecksTableRowsJS() string {
	return `
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
}).join('')`
}

// serviceStatusClassBindJS returns the Alpine.js x-bind:class expression for service status styling.
func serviceStatusClassBindJS() string {
	return `{
	'bg-green-100 text-green-800 border-green-200': $store.dashboard.selectedService?.status?.toLowerCase() === 'healthy',
	'bg-yellow-100 text-yellow-800 border-yellow-200': $store.dashboard.selectedService?.status?.toLowerCase() === 'degraded',
	'bg-red-100 text-red-800 border-red-200': $store.dashboard.selectedService?.status?.toLowerCase() === 'unhealthy'
}`
}

// serviceMetricsListJS returns the Alpine.js x-html expression for service metrics list.
func serviceMetricsListJS() string {
	return `
Object.entries($store.dashboard.selectedService?.metrics || {}).map(([key, value]) => {
	return '<div class="mb-2 flex items-center justify-between rounded-lg bg-muted p-3">' +
		'<span class="text-sm font-mono font-medium">' + key + '</span>' +
		'<span class="text-sm font-semibold">' + JSON.stringify(value) + '</span></div>';
}).join('')`
}

// metricsStatCardsJS returns the Alpine.js x-html expression for metrics stat cards.
func metricsStatCardsJS() string {
	return `
[{label: 'Total Metrics', key: 'total_metrics'}, {label: 'Counters', key: 'counter'}, {label: 'Gauges', key: 'gauge'}, {label: 'Histograms', key: 'histogram'}].map(stat => {
	const value = stat.key === 'total_metrics' ? ($store.dashboard.metricsReport?.total_metrics || 0) : ($store.dashboard.metricsReport?.metrics_by_type?.[stat.key] || 0);
	return '<div class="text-center space-y-1 rounded-lg bg-muted p-4">' +
		'<p class="text-sm text-muted-foreground">' + stat.label + '</p>' +
		'<p class="text-2xl font-bold">' + value + '</p></div>';
}).join('')`
}

// collectorsTableRowsJS returns the Alpine.js x-html expression for collector table rows.
func collectorsTableRowsJS() string {
	return `
($store.dashboard.metricsReport?.collectors || []).map(collector => {
	return '<tr class="hover:bg-muted/50">' +
		'<td class="py-3">' + collector.name + '</td>' +
		'<td class="py-3 text-muted-foreground">' + collector.type + '</td>' +
		'<td class="py-3 text-muted-foreground">' + collector.metrics_count + '</td>' +
		'<td class="py-3"><span class="inline-flex items-center rounded-full bg-green-100 px-2.5 py-0.5 text-xs font-semibold text-green-800">' + collector.status + '</span></td>' +
		'</tr>';
}).join('')`
}

// topMetricsListJS returns the Alpine.js x-html expression for top metrics list.
func topMetricsListJS() string {
	return `
($store.dashboard.metricsReport?.top_metrics || []).slice(0, 10).map(metric => {
	return '<div class="flex items-center justify-between rounded-lg bg-muted p-3">' +
		'<div><span class="text-sm font-medium">' + metric.name + '</span>' +
		'<span class="ml-2 text-xs text-muted-foreground">(' + metric.type + ')</span></div>' +
		'<span class="font-mono text-sm font-semibold">' + JSON.stringify(metric.value) + '</span></div>';
}).join('')`
}
