package ui

import (
	"fmt"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/components/card"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// WidgetCard renders a widget inside a card wrapper with optional HTMX auto-refresh.
func WidgetCard(w contributor.ResolvedWidget, content g.Node, basePath string) g.Node {
	children := []g.Node{
		card.Header(
			html.Div(
				html.Class("flex items-center justify-between"),
				card.Title(w.Title),
				g.If(w.Description != "",
					card.Description(w.Description),
				),
			),
		),
	}

	// Content wrapper — if dynamic, use HTMX auto-refresh
	if w.RefreshSec > 0 {
		children = append(children, html.Div(
			g.Attr("hx-get", fmt.Sprintf("%s/ext/%s/widgets/%s", basePath, w.Contributor, w.ID)),
			g.Attr("hx-trigger", fmt.Sprintf("load, every %ds", w.RefreshSec)),
			g.Attr("hx-swap", "innerHTML"),
			card.Content(content),
		))
	} else {
		children = append(children, card.Content(content))
	}

	return card.Card(children...)
}

// WidgetGrid renders a responsive grid of widgets.
func WidgetGrid(widgets []contributor.ResolvedWidget, contents map[string]g.Node, basePath string) g.Node {
	if len(widgets) == 0 {
		return g.Group(nil)
	}

	items := make([]g.Node, 0, len(widgets))

	for _, w := range widgets {
		content, ok := contents[w.ID]
		if !ok {
			content = widgetPlaceholder(w.Title)
		}

		colSpan := widgetColSpan(w.Size)
		items = append(items, html.Div(
			html.Class(colSpan),
			WidgetCard(w, content, basePath),
		))
	}

	return html.Div(
		html.Class("grid gap-4 md:grid-cols-2 lg:grid-cols-4"),
		g.Group(items),
	)
}

// widgetColSpan returns the CSS grid column span class based on widget size.
func widgetColSpan(size string) string {
	switch size {
	case "sm":
		return "col-span-1"
	case "md":
		return "md:col-span-2"
	case "lg":
		return "md:col-span-2 lg:col-span-4"
	default:
		return "col-span-1"
	}
}

// widgetPlaceholder renders a loading placeholder for a widget.
func widgetPlaceholder(title string) g.Node {
	return html.Div(
		html.Class("flex items-center justify-center py-8 text-sm text-muted-foreground"),
		g.Text(fmt.Sprintf("Loading %s...", title)),
	)
}

// WidgetErrorFragment renders an error message fragment for a failed widget.
func WidgetErrorFragment(message string) g.Node {
	return html.Div(
		html.Class("flex items-center justify-center py-4 text-sm text-destructive"),
		g.Text(message),
	)
}
