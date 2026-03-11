package dashboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// StreamingContributor implements contributor.LocalContributor for the streaming extension.
// It uses resolver functions to lazily obtain the manager and config at render time,
// ensuring they are available even when the dashboard discovers this contributor
// before the streaming extension has fully initialized.
type StreamingContributor struct {
	managerFn func() internal.Manager
	configFn  func() internal.Config
}

// NewStreamingContributor creates a new streaming dashboard contributor.
// The resolver functions are called at render time, not at construction time,
// so the manager/config can be nil during initial discovery and will resolve
// correctly once the streaming extension finishes its Register/Start lifecycle.
func NewStreamingContributor(managerFn func() internal.Manager, configFn func() internal.Config) *StreamingContributor {
	return &StreamingContributor{
		managerFn: managerFn,
		configFn:  configFn,
	}
}

// Manifest returns the streaming contributor manifest.
func (c *StreamingContributor) Manifest() *contributor.Manifest {
	return &contributor.Manifest{
		Name:        "streaming",
		DisplayName: "Streaming",
		Icon:        "radio",
		Version:     "2.0.0",
		Nav: []contributor.NavItem{
			{Label: "Overview", Path: "/", Icon: "radio", Group: "Platform", Priority: 10},
			{Label: "Connections", Path: "/connections", Icon: "plug", Group: "Platform", Priority: 11},
			{Label: "Rooms", Path: "/rooms", Icon: "door-open", Group: "Platform", Priority: 12},
			{Label: "Channels", Path: "/channels", Icon: "hash", Group: "Platform", Priority: 13},
			{Label: "Presence", Path: "/presence", Icon: "users", Group: "Platform", Priority: 14},
			{Label: "Playground", Path: "/playground", Icon: "flask-conical", Group: "Platform", Priority: 15},
		},
		Widgets: []contributor.WidgetDescriptor{
			{ID: "connections", Title: "Active Connections", Description: "Current WebSocket/SSE connections", Size: "sm", RefreshSec: 10, Group: "Platform", Priority: 10},
			{ID: "rooms", Title: "Rooms", Description: "Total streaming rooms", Size: "sm", RefreshSec: 30, Group: "Platform", Priority: 11},
			{ID: "online-users", Title: "Online Users", Description: "Users currently online", Size: "sm", RefreshSec: 10, Group: "Platform", Priority: 12},
			{ID: "messages", Title: "Message Rate", Description: "Messages per second", Size: "sm", RefreshSec: 10, Group: "Platform", Priority: 13},
		},
		Settings: []contributor.SettingsDescriptor{
			{ID: "config", Title: "Streaming Configuration", Description: "Current streaming backend and feature settings", Group: "Platform", Icon: "settings", Priority: 10},
		},
	}
}

// RenderPage renders a page for the given route.
func (c *StreamingContributor) RenderPage(ctx context.Context, route string, params contributor.Params) (templ.Component, error) {
	manager := c.managerFn()
	if manager == nil {
		return nil, fmt.Errorf("streaming manager not initialized")
	}

	switch route {
	case "/":
		return overviewPage(ctx, manager)
	case "/connections":
		return connectionsPage(ctx, manager)
	case "/rooms":
		return roomsPage(ctx, manager, params.BasePath)
	case "/channels":
		return channelsPage(ctx, manager)
	case "/presence":
		return presencePage(ctx, manager)
	case "/playground":
		return playgroundPage(ctx, manager, params)
	default:
		if strings.HasPrefix(route, "/rooms/") {
			roomID := strings.TrimPrefix(route, "/rooms/")
			return roomDetailPage(ctx, manager, roomID, params.BasePath)
		}

		return nil, fmt.Errorf("dashboard: page not found")
	}
}

// RenderWidget renders a specific widget by ID.
func (c *StreamingContributor) RenderWidget(ctx context.Context, widgetID string) (templ.Component, error) {
	manager := c.managerFn()
	if manager == nil {
		return nil, fmt.Errorf("streaming manager not initialized")
	}

	switch widgetID {
	case "connections":
		return connectionsWidget(ctx, manager)
	case "rooms":
		return roomsWidget(ctx, manager)
	case "online-users":
		return onlineUsersWidget(ctx, manager)
	case "messages":
		return messagesWidget(ctx, manager)
	default:
		return nil, fmt.Errorf("dashboard: widget not found")
	}
}

// RenderSettings renders a settings panel for the given setting ID.
func (c *StreamingContributor) RenderSettings(_ context.Context, settingID string) (templ.Component, error) {
	switch settingID {
	case "config":
		return configSettings(c.configFn()), nil
	default:
		return nil, fmt.Errorf("dashboard: setting not found")
	}
}
