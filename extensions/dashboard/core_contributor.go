package dashboard

import (
	"context"
	"fmt"
	"strconv"
	"time"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/icons"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/forge/extensions/dashboard/ui/pages"
)

// CoreContributor is the built-in contributor providing Overview, Health, Metrics, and Services pages.
// It implements contributor.LocalContributor.
type CoreContributor struct {
	collector *collector.DataCollector
	history   *collector.DataHistory
}

// NewCoreContributor creates a new CoreContributor.
func NewCoreContributor(c *collector.DataCollector, h *collector.DataHistory) *CoreContributor {
	return &CoreContributor{
		collector: c,
		history:   h,
	}
}

// Manifest returns the core contributor's manifest.
func (c *CoreContributor) Manifest() *contributor.Manifest {
	return &contributor.Manifest{
		Name:        "core",
		DisplayName: "Dashboard Core",
		Icon:        "layout-dashboard",
		Version:     "3.0.0",
		Nav: []contributor.NavItem{
			{Label: "Overview", Path: "/", Icon: "home", Group: "Overview", Priority: 0},
			{Label: "Health", Path: "/health", Icon: "heart-pulse", Group: "Overview", Priority: 1},
			{Label: "Metrics", Path: "/metrics", Icon: "chart-bar", Group: "Overview", Priority: 2},
			{Label: "Services", Path: "/services", Icon: "server", Group: "Overview", Priority: 3},
		},
		Widgets: []contributor.WidgetDescriptor{
			{ID: "health-summary", Title: "Health Summary", Description: "Current health status overview", Size: "sm", RefreshSec: 30, Group: "Overview", Priority: 0},
			{ID: "metrics-overview", Title: "Metrics Overview", Description: "Key metrics at a glance", Size: "sm", RefreshSec: 30, Group: "Overview", Priority: 1},
			{ID: "services-status", Title: "Services Status", Description: "Service availability", Size: "sm", RefreshSec: 30, Group: "Overview", Priority: 2},
			{ID: "uptime", Title: "Uptime", Description: "System uptime", Size: "sm", RefreshSec: 0, Group: "Overview", Priority: 3},
		},
	}
}

// RenderPage renders a page for the given route.
func (c *CoreContributor) RenderPage(ctx context.Context, route string, params contributor.Params) (g.Node, error) {
	switch route {
	case "/":
		return pages.OverviewPage(ctx, c.collector, c.history)
	case "/health":
		return pages.HealthPage(ctx, c.collector)
	case "/metrics":
		return pages.MetricsPage(ctx, c.collector)
	case "/services":
		return pages.ServicesPage(ctx, c.collector)
	default:
		return nil, ErrPageNotFound
	}
}

// RenderWidget renders a specific widget by ID.
func (c *CoreContributor) RenderWidget(ctx context.Context, widgetID string) (g.Node, error) {
	switch widgetID {
	case "health-summary":
		return c.renderHealthSummaryWidget(ctx)
	case "metrics-overview":
		return c.renderMetricsOverviewWidget(ctx)
	case "services-status":
		return c.renderServicesStatusWidget(ctx)
	case "uptime":
		return c.renderUptimeWidget(ctx)
	default:
		return nil, ErrWidgetNotFound
	}
}

// RenderSettings renders a settings panel. Core has no settings.
func (c *CoreContributor) RenderSettings(_ context.Context, _ string) (g.Node, error) {
	return nil, ErrSettingNotFound
}

// renderHealthSummaryWidget renders the health summary widget content.
func (c *CoreContributor) renderHealthSummaryWidget(ctx context.Context) (g.Node, error) {
	health := c.collector.CollectHealth(ctx)

	return html.Div(
		html.Class("space-y-3"),
		html.Div(
			html.Class("flex items-center gap-2"),
			icons.Activity(icons.WithSize(16)),
			html.Span(html.Class("text-sm font-medium"), g.Text(health.OverallStatus)),
		),
		html.Div(
			html.Class("grid grid-cols-3 gap-2 text-center text-xs"),
			widgetCountItem("Healthy", health.Summary.Healthy, "text-green-600"),
			widgetCountItem("Degraded", health.Summary.Degraded, "text-yellow-600"),
			widgetCountItem("Unhealthy", health.Summary.Unhealthy, "text-red-600"),
		),
	), nil
}

// renderMetricsOverviewWidget renders the metrics overview widget content.
func (c *CoreContributor) renderMetricsOverviewWidget(ctx context.Context) (g.Node, error) {
	metrics := c.collector.CollectMetrics(ctx)

	return html.Div(
		html.Class("space-y-3"),
		html.Div(
			html.Class("flex items-center gap-2"),
			icons.ChartBar(icons.WithSize(16)),
			html.Span(html.Class("text-2xl font-bold"), g.Text(strconv.Itoa(metrics.Stats.TotalMetrics))),
		),
		html.Div(
			html.Class("grid grid-cols-3 gap-2 text-center text-xs"),
			widgetCountItem("Counters", metrics.Stats.Counters, "text-blue-600"),
			widgetCountItem("Gauges", metrics.Stats.Gauges, "text-green-600"),
			widgetCountItem("Histograms", metrics.Stats.Histograms, "text-purple-600"),
		),
	), nil
}

// renderServicesStatusWidget renders the services status widget content.
func (c *CoreContributor) renderServicesStatusWidget(ctx context.Context) (g.Node, error) {
	overview := c.collector.CollectOverview(ctx)

	return html.Div(
		html.Class("space-y-3"),
		html.Div(
			html.Class("flex items-center gap-2"),
			icons.Server(icons.WithSize(16)),
			html.Span(html.Class("text-2xl font-bold"), g.Text(fmt.Sprintf("%d / %d", overview.HealthyServices, overview.TotalServices))),
		),
		html.Div(
			html.Class("text-xs text-muted-foreground"),
			g.Text("Healthy / Total services"),
		),
	), nil
}

// renderUptimeWidget renders the uptime widget content.
func (c *CoreContributor) renderUptimeWidget(ctx context.Context) (g.Node, error) {
	overview := c.collector.CollectOverview(ctx)

	return html.Div(
		html.Class("space-y-3"),
		html.Div(
			html.Class("flex items-center gap-2"),
			icons.Clock(icons.WithSize(16)),
			html.Span(html.Class("text-2xl font-bold"), g.Text(formatUptimeShort(overview.Uptime))),
		),
		html.Div(
			html.Class("text-xs text-muted-foreground"),
			g.Text("System uptime"),
		),
	), nil
}

// widgetCountItem renders a count item for widget grids.
func widgetCountItem(label string, count int, colorClass string) g.Node {
	return html.Div(
		html.Div(
			html.Class("font-bold "+colorClass),
			g.Text(strconv.Itoa(count)),
		),
		html.Div(
			html.Class("text-muted-foreground"),
			g.Text(label),
		),
	)
}

// formatUptimeShort returns a short human-readable uptime string.
func formatUptimeShort(d time.Duration) string {
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

	return fmt.Sprintf("%dd %dh", days, hours%24)
}
