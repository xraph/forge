package ui

import (
	"github.com/xraph/forgeui/components/badge"
)

// statusBadgeVariant maps a status string to a badge variant.
func statusBadgeVariant(status string) badge.Variant {
	switch status {
	case "healthy", "Healthy":
		return badge.VariantDefault
	case "degraded", "Degraded":
		return badge.VariantSecondary
	case "unhealthy", "Unhealthy":
		return badge.VariantDestructive
	default:
		return badge.VariantOutline
	}
}

// trendColorClass returns the CSS class for a trend indicator.
func trendColorClass(trendUp bool) string {
	if trendUp {
		return "text-green-600 dark:text-green-400"
	}

	return "text-red-600 dark:text-red-400"
}
