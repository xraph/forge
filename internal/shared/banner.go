package shared

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fatih/color"
)

const minimumBannerWidth = 62

// BannerConfig configures the startup banner.
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
	PprofPath   string
}

type bannerRow struct {
	plain    string
	rendered string
	divider  bool
}

// PrintStartupBanner prints a compact startup summary to stdout.
func PrintStartupBanner(cfg BannerConfig) {
	cyan := color.New(color.FgCyan).SprintFunc()
	gray := color.New(color.FgHiBlack).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()
	boldCyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	boldGreen := color.New(color.FgGreen, color.Bold).SprintFunc()

	version := cfg.Version
	if version != "" && !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	appDetails := cfg.AppName
	renderedAppDetails := bold(cfg.AppName)

	if cfg.Environment != "" {
		appDetails += " · " + cfg.Environment
		renderedAppDetails += gray(" · ") + getEnvColor(cfg.Environment)(cfg.Environment)
	}

	elapsed := time.Since(cfg.StartTime).Round(time.Millisecond)
	if cfg.StartTime.IsZero() || elapsed < 0 {
		elapsed = 0
	}

	readyText := "● Ready in " + elapsed.String()
	serverURL := formatServerURL(cfg.HTTPAddress)

	rows := []bannerRow{
		{plain: "    " + appDetails, rendered: "    " + renderedAppDetails},
		{divider: true},
	}

	apiRows := endpointRows("API", []endpoint{
		{name: "Swagger", path: cfg.OpenAPIUI},
		{name: "OpenAPI", path: cfg.OpenAPISpec},
		{name: "AsyncAPI", path: cfg.AsyncAPIUI},
	}, gray, bold, cyan)
	systemRows := endpointRows("SYSTEM", []endpoint{
		{name: "Health", path: cfg.HealthPath},
		{name: "Metrics", path: cfg.MetricsPath},
		{name: "Profiling", path: cfg.PprofPath},
	}, gray, bold, cyan)

	width := minimumBannerWidth
	for _, row := range append(append([]bannerRow{}, apiRows...), systemRows...) {
		width = max(width, utf8.RuneCountInString(row.plain)+2)
	}

	width = max(width, utf8.RuneCountInString("  "+readyText)+utf8.RuneCountInString(serverURL)+5)
	width = max(width, utf8.RuneCountInString("  ◆ FORGE")+utf8.RuneCountInString(version)+3)

	headerPlain, headerRendered := pairedRow(
		"  ◆ FORGE", "  "+boldCyan("◆ FORGE"),
		version, gray(version),
		width-2,
	)
	readyPlain, readyRendered := pairedRow(
		"  "+readyText, "  "+boldGreen(readyText),
		serverURL, cyan(serverURL),
		width-2,
	)

	rows = append([]bannerRow{{plain: headerPlain, rendered: headerRendered}}, rows...)
	rows = append(rows, bannerRow{plain: readyPlain, rendered: readyRendered})

	if len(apiRows) > 0 {
		rows = append(rows, bannerRow{})
		rows = append(rows, apiRows...)
	}

	if len(systemRows) > 0 {
		rows = append(rows, bannerRow{})
		rows = append(rows, systemRows...)
	}

	rows = append(rows,
		bannerRow{divider: true},
		bannerRow{plain: "  Ctrl+C to stop", rendered: "  " + gray("Ctrl+C to stop")},
	)

	var banner strings.Builder
	banner.WriteString("\n")
	banner.WriteString(gray("╭" + strings.Repeat("─", width) + "╮"))
	banner.WriteString("\n")

	for _, row := range rows {
		if row.divider {
			banner.WriteString(gray("├" + strings.Repeat("─", width) + "┤"))
			banner.WriteString("\n")

			continue
		}

		padding := width - utf8.RuneCountInString(row.plain)

		banner.WriteString(gray("│"))
		banner.WriteString(row.rendered)
		banner.WriteString(strings.Repeat(" ", max(0, padding)))
		banner.WriteString(gray("│"))
		banner.WriteString("\n")
	}

	banner.WriteString(gray("╰" + strings.Repeat("─", width) + "╯"))
	banner.WriteString("\n\n")

	fmt.Fprint(os.Stdout, banner.String())
}

type endpoint struct {
	name string
	path string
}

func endpointRows(
	group string,
	endpoints []endpoint,
	muted func(...any) string,
	emphasis func(...any) string,
	link func(...any) string,
) []bannerRow {
	rows := make([]bannerRow, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.path == "" {
			continue
		}

		rowGroup := ""
		if len(rows) == 0 {
			rowGroup = group
		}

		plain := fmt.Sprintf("  %-10s %-13s %s", rowGroup, endpoint.name, endpoint.path)
		rendered := fmt.Sprintf("  %-10s %-13s %s", muted(rowGroup), emphasis(endpoint.name), link(endpoint.path))
		rows = append(rows, bannerRow{plain: plain, rendered: rendered})
	}

	return rows
}

func pairedRow(leftPlain, leftRendered, rightPlain, rightRendered string, width int) (string, string) {
	gap := max(3, width-utf8.RuneCountInString(leftPlain)-utf8.RuneCountInString(rightPlain))
	spacing := strings.Repeat(" ", gap)

	return leftPlain + spacing + rightPlain, leftRendered + spacing + rightRendered
}

func formatServerURL(address string) string {
	if address == "" || strings.Contains(address, "://") {
		return address
	}

	if strings.HasPrefix(address, ":") {
		return "http://localhost" + address
	}

	if port, ok := strings.CutPrefix(address, "0.0.0.0:"); ok {
		return "http://localhost:" + port
	}

	if port, ok := strings.CutPrefix(address, "[::]:"); ok {
		return "http://localhost:" + port
	}

	return "http://" + address
}

// getEnvColor returns the appropriate color function for the environment.
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
