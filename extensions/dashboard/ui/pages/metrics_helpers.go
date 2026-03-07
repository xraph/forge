package pages

import (
	"sort"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forgeui"
)

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

// recentMetricsSlice returns the most recently updated metrics, up to limit.
func recentMetricsSlice(all []collector.MetricEntry, limit int) []collector.MetricEntry {
	if len(all) <= limit {
		return all
	}

	sorted := make([]collector.MetricEntry, len(all))
	copy(sorted, all)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.After(sorted[j].Timestamp)
	})

	return sorted[:limit]
}
