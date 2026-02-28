package settings

import (
	"context"
	"fmt"
	"io"

	"github.com/a-h/templ"

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

		return content.Render(ctx.Context(), ctx.Response())
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

		return content.Render(ctx.Context(), ctx.Response())
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

		return content.Render(ctx.Context(), ctx.Response())
	}
}

// settingsError renders a settings error message.
func settingsError(ctx forge.Context, message string) error {
	component := templ.ComponentFunc(func(tctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="p-6">`+
				`<div class="rounded-lg border border-destructive/50 bg-destructive/10 p-4">`+
				`<p class="text-sm text-destructive">`+templ.EscapeString(message)+`</p>`+
				`</div>`+
				`</div>`)
		return err
	})

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")

	return component.Render(ctx.Context(), ctx.Response())
}

// settingsIndexPage renders the settings overview with all groups.
func settingsIndexPage(groups []GroupedSettings, basePath string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(groups) == 0 {
			_, err := io.WriteString(w,
				`<div class="p-6">`+
					`<div class="text-center py-12">`+
					`<p class="text-muted-foreground">No settings available. Extensions can contribute settings panels to appear here.</p>`+
					`</div>`+
					`</div>`)
			return err
		}

		if _, err := io.WriteString(w, `<div class="p-6 max-w-4xl">`); err != nil {
			return err
		}

		if _, err := io.WriteString(w, `<h2 class="text-2xl font-bold mb-6">Settings</h2>`); err != nil {
			return err
		}

		for _, group := range groups {
			if _, err := io.WriteString(w,
				`<div class="mb-8">`+
					`<h3 class="text-lg font-semibold mb-3 text-muted-foreground">`+templ.EscapeString(group.Group)+`</h3>`+
					`<div class="grid gap-3">`); err != nil {
				return err
			}

			for _, s := range group.Settings {
				settingsPath := basePath + "/ext/" + s.Contributor + "/settings/" + s.ID
				if err := writeSettingsCard(w, s, settingsPath); err != nil {
					return err
				}
			}

			if _, err := io.WriteString(w, `</div></div>`); err != nil {
				return err
			}
		}

		_, err := io.WriteString(w, `</div>`)
		return err
	})
}

// writeSettingsCard writes a single settings entry as a clickable card.
func writeSettingsCard(w io.Writer, s contributor.ResolvedSetting, path string) error {
	escapedPath := templ.EscapeString(path)
	html := `<a href="` + escapedPath + `"` +
		` hx-get="` + escapedPath + `"` +
		` hx-target="#content"` +
		` hx-swap="innerHTML"` +
		` hx-push-url="true"` +
		` class="block rounded-lg border bg-card p-4 hover:bg-accent/50 transition-colors">` +
		`<div class="flex items-start gap-3">` +
		`<div class="flex-1">` +
		`<h4 class="font-medium">` + templ.EscapeString(s.Title) + `</h4>`

	if s.Description != "" {
		html += `<p class="text-sm text-muted-foreground mt-1">` + templ.EscapeString(s.Description) + `</p>`
	}

	html += `</div>` +
		`<span class="text-xs text-muted-foreground bg-muted px-2 py-1 rounded">` + templ.EscapeString(s.Contributor) + `</span>` +
		`</div>` +
		`</a>`

	_, err := io.WriteString(w, html)
	return err
}
