package dashboard

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/streaming/internal"
)

// connectionsWidget renders the active connections count widget.
func connectionsWidget(ctx context.Context, manager internal.Manager) (templ.Component, error) {
	count := manager.ConnectionCount()

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-3">`+
				`<div class="flex items-center gap-2">`+
				`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22v-5"/><path d="M9 8V2"/><path d="M15 8V2"/><path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z"/></svg>`+
				`<span class="text-2xl font-bold">`+strconv.Itoa(count)+`</span>`+
				`</div>`+
				`<div class="text-xs text-muted-foreground">WebSocket &amp; SSE connections</div>`+
				`</div>`)
		return err
	}), nil
}

// roomsWidget renders the rooms count widget.
func roomsWidget(ctx context.Context, manager internal.Manager) (templ.Component, error) {
	rooms, err := manager.ListRooms(ctx)
	if err != nil {
		rooms = nil
	}

	count := len(rooms)

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-3">`+
				`<div class="flex items-center gap-2">`+
				`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 4h3a2 2 0 0 1 2 2v14"/><path d="M2 20h3"/><path d="M13 20h9"/><path d="M10 12v.01"/><path d="M13 4.562v16.157a1 1 0 0 1-1.242.97L5 20V5.562a2 2 0 0 1 1.515-1.94l4-1A2 2 0 0 1 13 4.561Z"/></svg>`+
				`<span class="text-2xl font-bold">`+strconv.Itoa(count)+`</span>`+
				`</div>`+
				`<div class="text-xs text-muted-foreground">Active streaming rooms</div>`+
				`</div>`)
		return err
	}), nil
}

// onlineUsersWidget renders the online users count widget.
func onlineUsersWidget(ctx context.Context, manager internal.Manager) (templ.Component, error) {
	count, err := manager.GetOnlineCount(ctx)
	if err != nil {
		count = 0
	}

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-3">`+
				`<div class="flex items-center gap-2">`+
				`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>`+
				`<span class="text-2xl font-bold">`+strconv.Itoa(count)+`</span>`+
				`</div>`+
				`<div class="text-xs text-muted-foreground">Users currently online</div>`+
				`</div>`)
		return err
	}), nil
}

// messagesWidget renders the message rate widget.
func messagesWidget(ctx context.Context, manager internal.Manager) (templ.Component, error) {
	stats, err := manager.GetStats(ctx)
	if err != nil {
		stats = &internal.ManagerStats{}
	}

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-3">`+
				`<div class="flex items-center gap-2">`+
				`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon></svg>`+
				`<span class="text-2xl font-bold">`+fmt.Sprintf("%.1f", stats.MessagesPerSec)+`</span>`+
				`</div>`+
				`<div class="grid grid-cols-2 gap-2 text-center text-xs">`+
				`<div>`+
				`<div class="font-bold text-blue-600">`+formatInt64(stats.TotalMessages)+`</div>`+
				`<div class="text-muted-foreground">Total</div>`+
				`</div>`+
				`<div>`+
				`<div class="font-bold text-green-600">`+fmt.Sprintf("%.1f/s", stats.MessagesPerSec)+`</div>`+
				`<div class="text-muted-foreground">Rate</div>`+
				`</div>`+
				`</div>`+
				`</div>`)
		return err
	}), nil
}
