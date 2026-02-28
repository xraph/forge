package dashboard

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/a-h/templ"

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
		Root:        true,
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
func (c *CoreContributor) RenderPage(ctx context.Context, route string, params contributor.Params) (templ.Component, error) {
	switch route {
	case "/":
		return pages.OverviewPage(ctx, c.collector, c.history)
	case "/health":
		return pages.HealthPage(ctx, c.collector, c.history)
	case "/metrics":
		return pages.MetricsPage(ctx, c.collector, c.history)
	case "/services":
		return pages.ServicesPage(ctx, c.collector)
	default:
		return nil, ErrPageNotFound
	}
}

// RenderWidget renders a specific widget by ID.
func (c *CoreContributor) RenderWidget(ctx context.Context, widgetID string) (templ.Component, error) {
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
func (c *CoreContributor) RenderSettings(_ context.Context, _ string) (templ.Component, error) {
	return nil, ErrSettingNotFound
}

// renderHealthSummaryWidget renders the health summary widget content.
func (c *CoreContributor) renderHealthSummaryWidget(ctx context.Context) (templ.Component, error) {
	health := c.collector.CollectHealth(ctx)

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-3">`+
				`<div class="flex items-center gap-2">`+
				`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"></polyline></svg>`+
				`<span class="text-sm font-medium">`+templ.EscapeString(health.OverallStatus)+`</span>`+
				`</div>`+
				`<div class="grid grid-cols-3 gap-2 text-center text-xs">`+
				widgetCountItemHTML("Healthy", health.Summary.Healthy, "text-green-600")+
				widgetCountItemHTML("Degraded", health.Summary.Degraded, "text-yellow-600")+
				widgetCountItemHTML("Unhealthy", health.Summary.Unhealthy, "text-red-600")+
				`</div>`+
				`</div>`)
		return err
	}), nil
}

// renderMetricsOverviewWidget renders the metrics overview widget content.
func (c *CoreContributor) renderMetricsOverviewWidget(ctx context.Context) (templ.Component, error) {
	metrics := c.collector.CollectMetrics(ctx)

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-3">`+
				`<div class="flex items-center gap-2">`+
				`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="12" width="6" height="8"></rect><rect x="9" y="8" width="6" height="12"></rect><rect x="15" y="4" width="6" height="16"></rect></svg>`+
				`<span class="text-2xl font-bold">`+strconv.Itoa(metrics.Stats.TotalMetrics)+`</span>`+
				`</div>`+
				`<div class="grid grid-cols-3 gap-2 text-center text-xs">`+
				widgetCountItemHTML("Counters", metrics.Stats.Counters, "text-blue-600")+
				widgetCountItemHTML("Gauges", metrics.Stats.Gauges, "text-green-600")+
				widgetCountItemHTML("Histograms", metrics.Stats.Histograms, "text-purple-600")+
				`</div>`+
				`</div>`)
		return err
	}), nil
}

// renderServicesStatusWidget renders the services status widget content.
func (c *CoreContributor) renderServicesStatusWidget(ctx context.Context) (templ.Component, error) {
	overview := c.collector.CollectOverview(ctx)

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-3">`+
				`<div class="flex items-center gap-2">`+
				`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect><rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect><line x1="6" y1="6" x2="6.01" y2="6"></line><line x1="6" y1="18" x2="6.01" y2="18"></line></svg>`+
				`<span class="text-2xl font-bold">`+fmt.Sprintf("%d / %d", overview.HealthyServices, overview.TotalServices)+`</span>`+
				`</div>`+
				`<div class="text-xs text-muted-foreground">Healthy / Total services</div>`+
				`</div>`)
		return err
	}), nil
}

// renderUptimeWidget renders the uptime widget content.
func (c *CoreContributor) renderUptimeWidget(ctx context.Context) (templ.Component, error) {
	overview := c.collector.CollectOverview(ctx)

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-3">`+
				`<div class="flex items-center gap-2">`+
				`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><polyline points="12 6 12 12 16 14"></polyline></svg>`+
				`<span class="text-2xl font-bold">`+templ.EscapeString(formatUptimeShort(overview.Uptime))+`</span>`+
				`</div>`+
				`<div class="text-xs text-muted-foreground">System uptime</div>`+
				`</div>`)
		return err
	}), nil
}

// widgetCountItemHTML renders a count item for widget grids as an HTML string.
func widgetCountItemHTML(label string, count int, colorClass string) string {
	return `<div>` +
		`<div class="font-bold ` + colorClass + `">` + strconv.Itoa(count) + `</div>` +
		`<div class="text-muted-foreground">` + templ.EscapeString(label) + `</div>` +
		`</div>`
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
