package contributor

import (
	"context"

	g "maragu.dev/gomponents"
)

// Params holds route parameters and query strings extracted from the request.
type Params struct {
	Route       string
	PathParams  map[string]string
	QueryParams map[string]string
	FormData    map[string]string
}

// DashboardContributor is the base interface every dashboard extension implements.
// It provides a manifest describing the contributor's capabilities.
type DashboardContributor interface {
	// Manifest returns the contributor's manifest describing its pages, widgets, settings, etc.
	Manifest() *Manifest
}

// LocalContributor runs in-process and renders gomponents.Node directly.
// This is the primary interface for extensions that contribute UI to the dashboard.
type LocalContributor interface {
	DashboardContributor

	// RenderPage renders a page for the given route.
	RenderPage(ctx context.Context, route string, params Params) (g.Node, error)

	// RenderWidget renders a specific widget by ID.
	RenderWidget(ctx context.Context, widgetID string) (g.Node, error)

	// RenderSettings renders a settings panel for the given setting ID.
	RenderSettings(ctx context.Context, settingID string) (g.Node, error)
}

// SearchableContributor optionally adds search capability to a contributor.
type SearchableContributor interface {
	DashboardContributor

	// Search performs a search query and returns matching results.
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// NotifiableContributor optionally streams notifications from a contributor.
type NotifiableContributor interface {
	DashboardContributor

	// Notifications returns a channel that emits notifications.
	// The channel should be closed when the context is cancelled.
	Notifications(ctx context.Context) (<-chan Notification, error)
}

// AuthPageContributor optionally contributes authentication pages (login, logout,
// register, etc.) to the dashboard. Extensions implementing this interface
// provide the auth page UI that integrates with the dashboard shell.
type AuthPageContributor interface {
	DashboardContributor

	// RenderAuthPage renders an authentication page by type (e.g. "login", "register").
	RenderAuthPage(ctx context.Context, pageType string, params Params) (g.Node, error)

	// HandleAuthAction handles form submissions for authentication pages.
	// Returns a redirect URL on success, or a gomponents node to re-render on failure.
	HandleAuthAction(ctx context.Context, pageType string, params Params) (redirectURL string, node g.Node, err error)
}
