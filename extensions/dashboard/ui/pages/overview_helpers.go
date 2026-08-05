package pages

import (
	"fmt"
	"time"
)

// formatDuration formats a duration into a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}

	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}

	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
	}

	days := hours / 24
	remainingHours := hours % 24

	return fmt.Sprintf("%dd %dh", days, remainingHours)
}

// formatTime formats a time for display.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "Never"
	}

	return t.Format("15:04:05")
}

// boolPtr returns a pointer to the given bool value (helper for chart props).
func boolPtr(v bool) *bool {
	return &v
}
