package pages

import "github.com/xraph/forgeui"

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
