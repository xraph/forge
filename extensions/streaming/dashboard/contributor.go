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
type StreamingContributor struct {
	manager internal.Manager
	config  internal.Config
}

// NewStreamingContributor creates a new streaming dashboard contributor.
func NewStreamingContributor(manager internal.Manager, config internal.Config) *StreamingContributor {
	return &StreamingContributor{
		manager: manager,
		config:  config,
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
	switch route {
	case "/":
		return overviewPage(ctx, c.manager)
	case "/connections":
		return connectionsPage(ctx, c.manager)
	case "/rooms":
		return roomsPage(ctx, c.manager, params.BasePath)
	case "/channels":
		return channelsPage(ctx, c.manager)
	case "/presence":
		return presencePage(ctx, c.manager)
	case "/playground":
		return playgroundPage(ctx, c.manager, params)
	default:
		if strings.HasPrefix(route, "/rooms/") {
			roomID := strings.TrimPrefix(route, "/rooms/")
			return roomDetailPage(ctx, c.manager, roomID, params.BasePath)
		}

		return nil, fmt.Errorf("dashboard: page not found")
	}
}

// RenderWidget renders a specific widget by ID.
func (c *StreamingContributor) RenderWidget(ctx context.Context, widgetID string) (templ.Component, error) {
	switch widgetID {
	case "connections":
		return connectionsWidget(ctx, c.manager)
	case "rooms":
		return roomsWidget(ctx, c.manager)
	case "online-users":
		return onlineUsersWidget(ctx, c.manager)
	case "messages":
		return messagesWidget(ctx, c.manager)
	default:
		return nil, fmt.Errorf("dashboard: widget not found")
	}
}

// RenderSettings renders a settings panel for the given setting ID.
func (c *StreamingContributor) RenderSettings(_ context.Context, settingID string) (templ.Component, error) {
	switch settingID {
	case "config":
		return configSettings(c.config), nil
	default:
		return nil, fmt.Errorf("dashboard: setting not found")
	}
}
