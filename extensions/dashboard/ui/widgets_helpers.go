package ui

import (
	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

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

// widgetContent returns the content component for a widget, falling back to a placeholder.
func widgetContent(w contributor.ResolvedWidget, contents map[string]templ.Component) templ.Component {
	if content, ok := contents[w.ID]; ok {
		return content
	}

	return widgetPlaceholder(w.Title)
}
