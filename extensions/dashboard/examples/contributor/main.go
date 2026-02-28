// Package main demonstrates how to create a custom LocalContributor that
// adds pages, widgets, and settings to the dashboard.
//
// A LocalContributor runs in-process and renders templ.Component values
// directly. This is the primary mechanism for Forge extensions to
// contribute UI to the dashboard shell.
//
// NOTE: This is an illustrative stub. It requires a full Forge application
// environment to run.
package main

import (
	"context"
	"io"
	"log"

	"github.com/a-h/templ"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// ---------------------------------------------------------------------------
// UsersContributor: a custom local contributor for user management.
// ---------------------------------------------------------------------------

// UsersContributor adds user management pages and widgets to the dashboard.
type UsersContributor struct{}

// Manifest returns the contributor's manifest describing its pages, widgets, and settings.
func (c *UsersContributor) Manifest() *contributor.Manifest {
	return &contributor.Manifest{
		Name:        "users",
		DisplayName: "User Management",
		Icon:        "users",
		Version:     "1.0.0",
		Nav: []contributor.NavItem{
			{Label: "Users", Path: "/", Icon: "users", Group: "Identity", Priority: 0},
			{Label: "Roles", Path: "/roles", Icon: "shield", Group: "Identity", Priority: 1},
		},
		Widgets: []contributor.WidgetDescriptor{
			{ID: "active-users", Title: "Active Users", Size: "sm", RefreshSec: 60, Group: "Identity", Priority: 0},
		},
		Settings: []contributor.SettingsDescriptor{
			{ID: "user-defaults", Title: "User Defaults", Description: "Default settings for new users", Group: "Identity", Icon: "settings", Priority: 0},
		},
	}
}

// RenderPage renders a page for the given route.
func (c *UsersContributor) RenderPage(_ context.Context, route string, _ contributor.Params) (templ.Component, error) {
	switch route {
	case "/":
		return htmlSnippet(`<div class="p-6">
			<h2 class="text-2xl font-bold mb-4">Users</h2>
			<p class="text-muted-foreground">User management page content goes here.</p>
		</div>`), nil

	case "/roles":
		return htmlSnippet(`<div class="p-6">
			<h2 class="text-2xl font-bold mb-4">Roles</h2>
			<p class="text-muted-foreground">Role management page content goes here.</p>
		</div>`), nil

	default:
		return nil, dashboard.ErrPageNotFound
	}
}

// RenderWidget renders a specific widget by ID.
func (c *UsersContributor) RenderWidget(_ context.Context, widgetID string) (templ.Component, error) {
	switch widgetID {
	case "active-users":
		return htmlSnippet(`<div class="text-center py-4">
			<span class="text-3xl font-bold">42</span>
			<p class="text-sm text-muted-foreground">active users</p>
		</div>`), nil

	default:
		return nil, dashboard.ErrWidgetNotFound
	}
}

// RenderSettings renders a settings panel for the given setting ID.
func (c *UsersContributor) RenderSettings(_ context.Context, settingID string) (templ.Component, error) {
	switch settingID {
	case "user-defaults":
		return htmlSnippet(`<div class="p-6">
			<h3 class="text-lg font-semibold mb-2">User Defaults</h3>
			<p class="text-muted-foreground">Configure default settings for new user accounts.</p>
		</div>`), nil

	default:
		return nil, dashboard.ErrSettingNotFound
	}
}

// htmlSnippet creates a templ.Component from raw HTML. In production,
// use .templ files instead — this is for illustration only.
func htmlSnippet(html string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, html)
		return err
	})
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	app := forge.New(
		forge.WithAppName("contributor-example"),
		forge.WithAppVersion("1.0.0"),
	)

	// Create dashboard extension.
	dashExt := dashboard.NewExtension(
		dashboard.WithTitle("My App Dashboard"),
		dashboard.WithBasePath("/dashboard"),
	)

	if err := app.RegisterExtension(dashExt); err != nil {
		log.Fatalf("failed to register dashboard extension: %v", err)
	}

	// Register a custom local contributor.
	// The type assertion to *dashboard.Extension is needed to access the
	// RegisterContributor method, which is not part of the forge.Extension interface.
	typedDashExt := dashExt.(*dashboard.Extension)
	if err := typedDashExt.RegisterContributor(&UsersContributor{}); err != nil {
		log.Fatalf("failed to register users contributor: %v", err)
	}

	// After registration the dashboard sidebar will contain:
	//   Identity group:
	//     - Users    -> /dashboard/ext/users/pages/
	//     - Roles    -> /dashboard/ext/users/pages/roles
	//
	// The overview page will include the "Active Users" widget.
	if err := app.Run(); err != nil {
		log.Fatalf("application error: %v", err)
	}
}
