package ui

import (
	"fmt"
	"time"
)

// overviewHealthCardJS returns the Alpine.js x-html expression for the overview health card.
func overviewHealthCardJS() string {
	return `
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
})()`
}

// servicesGridCardsJS returns the Alpine.js x-html expression for service grid cards.
func servicesGridCardsJS() string {
	return `
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
}).join('')`
}

// getStatusColorClass returns the border color class for a given status.
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

// formatRelativeTime formats a time as a human-readable relative string.
func formatRelativeTime(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	}

	if duration < time.Hour {
		return fmt.Sprintf("%d min ago", int(duration.Minutes()))
	}

	if duration < 24*time.Hour {
		return fmt.Sprintf("%d hr ago", int(duration.Hours()))
	}

	return fmt.Sprintf("%d days ago", int(duration.Hours()/24))
}
