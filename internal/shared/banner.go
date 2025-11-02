package shared

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
)

// BannerConfig configures the startup banner
type BannerConfig struct {
	AppName     string
	Version     string
	Environment string
	HTTPAddress string
	StartTime   time.Time

	// Optional paths
	OpenAPISpec string
	OpenAPIUI   string
	AsyncAPIUI  string
	HealthPath  string
	MetricsPath string
}

// PrintStartupBanner prints a styled startup banner to stdout
func PrintStartupBanner(cfg BannerConfig) {
	// Color definitions (avoiding CLI package import)
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()
	boldCyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	boldGreen := color.New(color.FgGreen, color.Bold).SprintFunc()

	// Build the banner
	var banner strings.Builder

	// Top border
	banner.WriteString("\n")
	banner.WriteString(gray(strings.Repeat("━", 80)))
	banner.WriteString("\n\n")

	// Logo ASCII art (inspired by the Forge SVG logo - geometric F shape)
	logo := []string{
		"  ███████╗  ██████╗  ██████╗   ██████╗  ███████╗",
		"  ██╔════╝ ██╔═══██╗ ██╔══██╗ ██╔════╝  ██╔════╝",
		"  █████╗   ██║   ██║ ██████╔╝ ██║  ███╗ █████╗  ",
		"  ██╔══╝   ██║   ██║ ██╔══██╗ ██║   ██║ ██╔══╝  ",
		"  ██║      ╚██████╔╝ ██║  ██║ ╚██████╔╝ ███████╗",
		"  ╚═╝       ╚═════╝  ╚═╝  ╚═╝  ╚═════╝  ╚══════╝",
	}

	for _, line := range logo {
		banner.WriteString(cyan(line))
		banner.WriteString("\n")
	}

	banner.WriteString("\n")

	// Application info
	banner.WriteString(fmt.Sprintf("  %s %s\n", bold("Application:"), boldCyan(cfg.AppName)))
	banner.WriteString(fmt.Sprintf("  %s %s\n", bold("Version:"), green(cfg.Version)))
	banner.WriteString(fmt.Sprintf("  %s %s\n", bold("Environment:"), getEnvColor(cfg.Environment)(cfg.Environment)))
	banner.WriteString("\n")

	// Server info
	banner.WriteString(fmt.Sprintf("  %s %s\n", bold("Server:"), boldGreen(cfg.HTTPAddress)))
	banner.WriteString(fmt.Sprintf("  %s %s\n", bold("Started:"), gray(cfg.StartTime.Format("2006-01-02 15:04:05 MST"))))
	banner.WriteString("\n")

	// API Documentation (if available)
	hasAPIDocs := cfg.OpenAPISpec != "" || cfg.OpenAPIUI != "" || cfg.AsyncAPIUI != ""
	if hasAPIDocs {
		banner.WriteString(fmt.Sprintf("  %s\n", bold("API Documentation:")))
		
		if cfg.OpenAPISpec != "" {
			banner.WriteString(fmt.Sprintf("    %s %s\n", gray("├─"), formatEndpoint("OpenAPI Spec", cfg.OpenAPISpec, yellow)))
		}
		if cfg.OpenAPIUI != "" {
			banner.WriteString(fmt.Sprintf("    %s %s\n", gray("├─"), formatEndpoint("Swagger UI", cfg.OpenAPIUI, yellow)))
		}
		if cfg.AsyncAPIUI != "" {
			banner.WriteString(fmt.Sprintf("    %s %s\n", gray("└─"), formatEndpoint("AsyncAPI UI", cfg.AsyncAPIUI, yellow)))
		}
		banner.WriteString("\n")
	}

	// Observability endpoints (if available)
	hasObservability := cfg.HealthPath != "" || cfg.MetricsPath != ""
	if hasObservability {
		banner.WriteString(fmt.Sprintf("  %s\n", bold("Observability:")))
		
		if cfg.HealthPath != "" {
			if cfg.MetricsPath != "" {
				banner.WriteString(fmt.Sprintf("    %s %s\n", gray("├─"), formatEndpoint("Health", cfg.HealthPath, green)))
			} else {
				banner.WriteString(fmt.Sprintf("    %s %s\n", gray("└─"), formatEndpoint("Health", cfg.HealthPath, green)))
			}
		}
		if cfg.MetricsPath != "" {
			banner.WriteString(fmt.Sprintf("    %s %s\n", gray("└─"), formatEndpoint("Metrics", cfg.MetricsPath, green)))
		}
		banner.WriteString("\n")
	}

	// Bottom border with helpful message
	banner.WriteString(gray(strings.Repeat("━", 80)))
	banner.WriteString("\n")
	banner.WriteString(fmt.Sprintf("  %s\n", gray("Press Ctrl+C to shutdown gracefully")))
	banner.WriteString(gray(strings.Repeat("━", 80)))
	banner.WriteString("\n\n")

	// Print to stdout
	fmt.Print(banner.String())
}

// formatEndpoint formats an endpoint line with name and URL
func formatEndpoint(name, path string, colorFunc func(...any) string) string {
	// Pad name to align URLs
	paddedName := fmt.Sprintf("%-14s", name+":")
	return fmt.Sprintf("%s %s", paddedName, colorFunc(path))
}

// getEnvColor returns the appropriate color function for the environment
func getEnvColor(env string) func(...any) string {
	switch env {
	case "production":
		return color.New(color.FgRed, color.Bold).SprintFunc()
	case "staging":
		return color.New(color.FgYellow, color.Bold).SprintFunc()
	case "development":
		return color.New(color.FgCyan).SprintFunc()
	default:
		return color.New(color.FgWhite).SprintFunc()
	}
}


