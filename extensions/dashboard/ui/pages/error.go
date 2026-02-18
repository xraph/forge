package pages

import (
	"fmt"
	"strconv"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forgeui"
	"github.com/xraph/forgeui/components/button"
	"github.com/xraph/forgeui/icons"
	"github.com/xraph/forgeui/primitives"
)

// ErrorPage renders an error page with appropriate styling for the given status code.
func ErrorPage(code int, title, message, retryURL string) g.Node {
	return html.Div(
		html.Class("flex flex-col items-center justify-center min-h-[60vh] text-center px-4"),
		errorIcon(code),
		html.Div(
			html.Class("mt-6 space-y-2"),
			primitives.Text(
				primitives.TextSize("text-4xl"),
				primitives.TextWeight("font-bold"),
				primitives.TextColor("text-foreground"),
				primitives.TextChildren(g.Text(strconv.Itoa(code))),
			),
			primitives.Text(
				primitives.TextSize("text-xl"),
				primitives.TextWeight("font-semibold"),
				primitives.TextColor("text-foreground"),
				primitives.TextChildren(g.Text(title)),
			),
			primitives.Text(
				primitives.TextSize("text-sm"),
				primitives.TextColor("text-muted-foreground"),
				primitives.TextChildren(g.Text(message)),
			),
		),
		errorActions(code, retryURL),
	)
}

// NotFoundPage renders a 404 error page.
func NotFoundPage(basePath string) g.Node {
	return ErrorPage(404, "Page Not Found", "The page you're looking for doesn't exist or has been moved.", basePath)
}

// InternalErrorPage renders a 500 error page.
func InternalErrorPage(detail string) g.Node {
	msg := "Something went wrong on our end. Please try again later."
	if detail != "" {
		msg = detail
	}

	return ErrorPage(500, "Internal Server Error", msg, "")
}

// ServiceUnavailablePage renders a 503 error page with optional auto-retry.
func ServiceUnavailablePage(retryURL string) g.Node {
	return html.Div(
		html.Class("flex flex-col items-center justify-center min-h-[60vh] text-center px-4"),
		errorIcon(503),
		html.Div(
			html.Class("mt-6 space-y-2"),
			primitives.Text(
				primitives.TextSize("text-4xl"),
				primitives.TextWeight("font-bold"),
				primitives.TextColor("text-foreground"),
				primitives.TextChildren(g.Text("503")),
			),
			primitives.Text(
				primitives.TextSize("text-xl"),
				primitives.TextWeight("font-semibold"),
				primitives.TextColor("text-foreground"),
				primitives.TextChildren(g.Text("Service Unavailable")),
			),
			primitives.Text(
				primitives.TextSize("text-sm"),
				primitives.TextColor("text-muted-foreground"),
				primitives.TextChildren(g.Text("The service is temporarily unavailable. Retrying automatically...")),
			),
		),
		// Auto-retry countdown
		html.Div(
			html.Class("mt-8"),
			g.Attr("x-data", "{ countdown: 10 }"),
			g.Attr("x-init", `
				let interval = setInterval(() => {
					countdown--;
					if (countdown <= 0) {
						clearInterval(interval);
						if (window.htmx) {
							htmx.ajax('GET', $el.dataset.retryUrl, {target: '#content', swap: 'innerHTML'});
						} else {
							window.location.reload();
						}
					}
				}, 1000)
			`),
			g.Attr("data-retry-url", retryURL),
			html.Div(
				html.Class("flex flex-col items-center gap-3"),
				html.Div(
					html.Class("flex items-center gap-2 text-sm text-muted-foreground"),
					icons.RefreshCw(icons.WithSize(16), icons.WithClass("animate-spin")),
					html.Span(
						g.Text("Retrying in "),
						html.Span(g.Attr("x-text", "countdown")),
						g.Text(" seconds..."),
					),
				),
				button.Button(
					g.Group([]g.Node{
						icons.RefreshCw(icons.WithSize(16)),
						g.Text("Retry Now"),
					}),
					button.WithVariant(forgeui.VariantOutline),
					button.WithSize(forgeui.SizeSM),
					button.WithAttrs(
						g.Attr("hx-get", retryURL),
						g.Attr("hx-target", "#content"),
						g.Attr("hx-swap", "innerHTML"),
					),
				),
			),
		),
	)
}

// errorIcon returns an icon appropriate for the error code.
func errorIcon(code int) g.Node {
	iconSize := icons.WithSize(48)
	iconClass := icons.WithClass("text-muted-foreground")

	var icon g.Node

	switch {
	case code == 404:
		icon = icons.FileQuestionMark(iconSize, iconClass)
	case code == 503:
		icon = icons.ServerCrash(iconSize, iconClass)
	default:
		icon = icons.TriangleAlert(iconSize, iconClass)
	}

	return html.Div(
		html.Class("flex h-24 w-24 items-center justify-center rounded-full bg-muted"),
		icon,
	)
}

// errorActions renders action buttons based on error type.
func errorActions(code int, retryURL string) g.Node {
	buttons := make([]g.Node, 0, 2)

	// Back button (always)
	buttons = append(buttons, button.Button(
		g.Group([]g.Node{
			icons.ArrowLeft(icons.WithSize(16)),
			g.Text("Go Back"),
		}),
		button.WithVariant(forgeui.VariantOutline),
		button.WithSize(forgeui.SizeSM),
		button.WithAttrs(
			g.Attr("onclick", "history.back()"),
		),
	))

	// Retry button (for server errors)
	if code >= 500 && retryURL != "" {
		buttons = append(buttons, button.Button(
			g.Group([]g.Node{
				icons.RefreshCw(icons.WithSize(16)),
				g.Text("Try Again"),
			}),
			button.WithVariant(forgeui.VariantDefault),
			button.WithSize(forgeui.SizeSM),
			button.WithAttrs(
				g.Attr("hx-get", retryURL),
				g.Attr("hx-target", "#content"),
				g.Attr("hx-swap", "innerHTML"),
			),
		))
	}

	// Home button for 404
	if code == 404 && retryURL != "" {
		buttons = append(buttons, button.Button(
			g.Group([]g.Node{
				icons.Home(icons.WithSize(16)),
				g.Text("Dashboard"),
			}),
			button.WithVariant(forgeui.VariantDefault),
			button.WithSize(forgeui.SizeSM),
			button.WithAttrs(
				html.Href(retryURL),
				g.Attr("hx-get", retryURL),
				g.Attr("hx-target", "#content"),
				g.Attr("hx-swap", "innerHTML"),
				g.Attr("hx-push-url", "true"),
			),
		))
	}

	return html.Div(
		html.Class(fmt.Sprintf("mt-8 flex items-center gap-3")),
		g.Group(buttons),
	)
}
