package settings

import (
	"fmt"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// HandleSettingsIndex returns a handler that renders the settings overview page.
// This lists all available settings grouped by contributor.
func HandleSettingsIndex(
	aggregator *Aggregator,
	registry *contributor.ContributorRegistry,
	basePath string,
) forge.Handler {
	return func(ctx forge.Context) error {
		groups := aggregator.GetAllGrouped()

		content := settingsIndexPage(groups, basePath)

		ctx.SetHeader("Content-Type", "text/html; charset=utf-8")

		return content.Render(ctx.Response())
	}
}

// HandleSettingsForm returns a handler that renders a specific contributor's settings form.
// GET {base}/ext/:name/settings/:id.
func HandleSettingsForm(registry *contributor.ContributorRegistry, basePath string) forge.Handler {
	return func(ctx forge.Context) error {
		name := ctx.Param("name")
		settingID := ctx.Param("id")

		local, ok := registry.FindLocalContributor(name)
		if !ok {
			return settingsError(ctx, fmt.Sprintf("Extension '%s' not found", name))
		}

		content, err := local.RenderSettings(ctx.Context(), settingID)
		if err != nil {
			return settingsError(ctx, fmt.Sprintf("Setting '%s' not available: %s", settingID, err.Error()))
		}

		ctx.SetHeader("Content-Type", "text/html; charset=utf-8")

		return content.Render(ctx.Response())
	}
}

// HandleSettingsSubmit returns a handler that processes a settings form submission.
// POST {base}/ext/:name/settings/:id.
func HandleSettingsSubmit(registry *contributor.ContributorRegistry, basePath string) forge.Handler {
	return func(ctx forge.Context) error {
		name := ctx.Param("name")
		settingID := ctx.Param("id")

		local, ok := registry.FindLocalContributor(name)
		if !ok {
			return ctx.JSON(404, map[string]string{
				"error": fmt.Sprintf("extension '%s' not found", name),
			})
		}

		// Re-render the settings form after submission
		// Contributors handle their own form data parsing from ctx
		content, err := local.RenderSettings(ctx.Context(), settingID)
		if err != nil {
			return ctx.JSON(500, map[string]string{
				"error": "failed to process settings: " + err.Error(),
			})
		}

		ctx.SetHeader("Content-Type", "text/html; charset=utf-8")

		return content.Render(ctx.Response())
	}
}

// settingsError renders a settings error message.
func settingsError(ctx forge.Context, message string) error {
	node := html.Div(
		html.Class("p-6"),
		html.Div(
			html.Class("rounded-lg border border-destructive/50 bg-destructive/10 p-4"),
			html.P(
				html.Class("text-sm text-destructive"),
				g.Text(message),
			),
		),
	)

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")

	return node.Render(ctx.Response())
}

// settingsIndexPage renders the settings overview with all groups.
func settingsIndexPage(groups []GroupedSettings, basePath string) g.Node {
	if len(groups) == 0 {
		return html.Div(
			html.Class("p-6"),
			html.Div(
				html.Class("text-center py-12"),
				html.P(
					html.Class("text-muted-foreground"),
					g.Text("No settings available. Extensions can contribute settings panels to appear here."),
				),
			),
		)
	}

	var sections []g.Node

	sections = append(sections, html.H2(
		html.Class("text-2xl font-bold mb-6"),
		g.Text("Settings"),
	))

	for _, group := range groups {
		var items []g.Node

		for _, s := range group.Settings {
			settingsPath := basePath + "/ext/" + s.Contributor + "/settings/" + s.ID
			items = append(items, settingsCard(s, settingsPath))
		}

		sections = append(sections,
			html.Div(
				html.Class("mb-8"),
				html.H3(
					html.Class("text-lg font-semibold mb-3 text-muted-foreground"),
					g.Text(group.Group),
				),
				html.Div(
					html.Class("grid gap-3"),
					g.Group(items),
				),
			),
		)
	}

	return html.Div(
		html.Class("p-6 max-w-4xl"),
		g.Group(sections),
	)
}

// settingsCard renders a single settings entry as a clickable card.
func settingsCard(s contributor.ResolvedSetting, path string) g.Node {
	return html.A(
		html.Href(path),
		g.Attr("hx-get", path),
		g.Attr("hx-target", "#content"),
		g.Attr("hx-swap", "innerHTML"),
		g.Attr("hx-push-url", "true"),
		html.Class("block rounded-lg border bg-card p-4 hover:bg-accent/50 transition-colors"),
		html.Div(
			html.Class("flex items-start gap-3"),
			html.Div(
				html.Class("flex-1"),
				html.H4(
					html.Class("font-medium"),
					g.Text(s.Title),
				),
				g.If(s.Description != "",
					html.P(
						html.Class("text-sm text-muted-foreground mt-1"),
						g.Text(s.Description),
					),
				),
			),
			html.Span(
				html.Class("text-xs text-muted-foreground bg-muted px-2 py-1 rounded"),
				g.Text(s.Contributor),
			),
		),
	)
}
