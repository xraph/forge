package shell

import (
	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui/icons"
)

// Breadcrumb represents a single breadcrumb navigation item.
type Breadcrumb struct {
	Label string
	Path  string // empty = current page (no link)
}

// Breadcrumbs renders a breadcrumb navigation trail.
func Breadcrumbs(crumbs []Breadcrumb, basePath string) g.Node {
	if len(crumbs) == 0 {
		return g.Group(nil)
	}

	items := make([]g.Node, 0, len(crumbs)*2)

	for i, crumb := range crumbs {
		// Add separator between items
		if i > 0 {
			items = append(items, html.Span(
				html.Class("mx-1.5 text-muted-foreground"),
				icons.ChevronRight(icons.WithSize(14)),
			))
		}

		if crumb.Path != "" && i < len(crumbs)-1 {
			// Linked breadcrumb (not the last one)
			items = append(items, html.A(
				html.Href(basePath+crumb.Path),
				html.Class("text-sm text-muted-foreground hover:text-foreground transition-colors"),
				g.Attr("hx-get", basePath+crumb.Path),
				g.Attr("hx-target", "#content"),
				g.Attr("hx-swap", "innerHTML"),
				g.Attr("hx-push-url", "true"),
				g.Text(crumb.Label),
			))
		} else {
			// Current page breadcrumb (last one, no link)
			items = append(items, html.Span(
				html.Class("text-sm font-medium text-foreground"),
				g.Text(crumb.Label),
			))
		}
	}

	return html.Nav(
		g.Attr("aria-label", "Breadcrumb"),
		html.Ol(
			html.Class("flex items-center"),
			g.Group(items),
		),
	)
}
