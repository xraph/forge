package pages

import "github.com/xraph/forgeui"

// statusBadgeVariant returns the badge variant for a health status.
func statusBadgeVariant(status string) forgeui.Variant {
	switch status {
	case "healthy", "Healthy":
		return forgeui.VariantDefault
	case "degraded", "Degraded":
		return forgeui.VariantSecondary
	case "unhealthy", "Unhealthy":
		return forgeui.VariantDestructive
	default:
		return forgeui.VariantOutline
	}
}

// defaultText returns the value if non-empty, else the fallback.
func defaultText(val, fallback string) string {
	if val == "" {
		return fallback
	}

	return val
}
