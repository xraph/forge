package pages

import (
	"context"
	"fmt"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/components/badge"
	"github.com/xraph/forgeui/components/card"
	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/primitives"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/ui"
)

// ServicesPage renders the services list page.
func ServicesPage(ctx context.Context, c *collector.DataCollector) (g.Node, error) {
	services := c.CollectServices(ctx)

	return html.Div(
		html.Class("space-y-6"),

		// Page header
		html.Div(
			html.Class("flex items-center justify-between"),
			ui.SectionHeader("Services", "Registered services and their status"),
			html.Span(
				html.Class("text-sm text-muted-foreground"),
				g.Text(fmt.Sprintf("%d services", len(services))),
			),
		),

		// Services content
		g.If(len(services) == 0,
			card.Card(
				card.Content(
					ui.EmptyState(
						icons.Server(icons.WithSize(32)),
						"No Services",
						"No services are currently registered.",
					),
				),
			),
		),

		g.If(len(services) > 0,
			servicesGrid(services),
		),
	), nil
}

// servicesGrid renders a responsive grid of service cards.
func servicesGrid(services []collector.ServiceInfo) g.Node {
	cards := make([]g.Node, 0, len(services))

	for _, svc := range services {
		cards = append(cards, serviceCard(svc))
	}

	return html.Div(
		html.Class("grid gap-4 md:grid-cols-2 lg:grid-cols-3"),
		g.Group(cards),
	)
}

// serviceCard renders a single service card.
func serviceCard(svc collector.ServiceInfo) g.Node {
	borderColor := serviceStatusBorder(svc.Status)

	return card.CardWithOptions(
		[]card.Option{card.WithClass("border-l-4 " + borderColor)},
		card.Header(
			html.Div(
				html.Class("flex items-center justify-between"),
				html.Div(
					html.Class("flex items-center gap-2"),
					icons.Server(icons.WithSize(18), icons.WithClass("text-muted-foreground")),
					primitives.Text(
						primitives.TextSize("text-base"),
						primitives.TextWeight("font-semibold"),
						primitives.TextChildren(g.Text(svc.Name)),
					),
				),
				badge.Badge(svc.Type, badge.WithVariant(forgeui.VariantSecondary)),
			),
		),
		card.Content(
			html.Div(
				html.Class("flex items-center justify-between"),
				html.Div(
					html.Class("flex items-center gap-2"),
					serviceStatusDot(svc.Status),
					html.Span(
						html.Class("text-sm"),
						g.Text(svc.Status),
					),
				),
				g.If(!svc.RegisteredAt.IsZero(),
					html.Span(
						html.Class("text-xs text-muted-foreground"),
						g.Text("since "+svc.RegisteredAt.Format("Jan 2 15:04")),
					),
				),
			),
		),
	)
}

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

// serviceStatusDot renders a small colored dot for service status.
func serviceStatusDot(status string) g.Node {
	colorClass := "bg-muted-foreground"

	switch status {
	case "healthy", "Healthy":
		colorClass = "bg-green-500"
	case "degraded", "Degraded":
		colorClass = "bg-yellow-500"
	case "unhealthy", "Unhealthy":
		colorClass = "bg-red-500"
	}

	return html.Div(
		html.Class("h-2.5 w-2.5 rounded-full " + colorClass),
	)
}
