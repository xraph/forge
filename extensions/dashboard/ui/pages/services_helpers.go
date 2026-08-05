package pages

// serviceStatusBorder returns the left border color class for a service status.
func serviceStatusBorder(status string) string {
	switch status {
	case "healthy", "Healthy":
		return "border-l-green-500"
	case "degraded", "Degraded":
		return "border-l-yellow-500"
	case "unhealthy", "Unhealthy":
		return "border-l-red-500"
	default:
		return "border-l-muted-foreground"
	}
}

// serviceStatusDotColor returns the dot color class for a service status.
func serviceStatusDotColor(status string) string {
	switch status {
	case "healthy", "Healthy":
		return "bg-green-500"
	case "degraded", "Degraded":
		return "bg-yellow-500"
	case "unhealthy", "Unhealthy":
		return "bg-red-500"
	default:
		return "bg-muted-foreground"
	}
}
