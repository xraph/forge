package dashboard

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// overviewPage renders the streaming overview page with stat cards.
func overviewPage(ctx context.Context, manager internal.Manager) (templ.Component, error) {
	stats, err := manager.GetStats(ctx)
	if err != nil {
		stats = &internal.ManagerStats{}
	}

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`<div class="space-y-6">`+
				`<div class="flex items-center justify-between">`+
				`<div>`+
				`<h2 class="text-2xl font-bold tracking-tight">Streaming Overview</h2>`+
				`<p class="text-muted-foreground">Real-time connection and messaging statistics</p>`+
				`</div>`+
				`</div>`+

				// Stat cards grid
				`<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">`+
				statCard("Active Connections", strconv.Itoa(stats.TotalConnections), "plug", "Currently connected WebSocket/SSE clients")+
				statCard("Rooms", strconv.Itoa(stats.TotalRooms), "door-open", "Total active rooms")+
				statCard("Channels", strconv.Itoa(stats.TotalChannels), "hash", "Total pub/sub channels")+
				statCard("Online Users", strconv.Itoa(stats.OnlineUsers), "users", "Users with active presence")+
				`</div>`+

				// Second row
				`<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">`+
				statCard("Messages/sec", fmt.Sprintf("%.1f", stats.MessagesPerSec), "zap", "Current message throughput")+
				statCard("Total Messages", formatInt64(stats.TotalMessages), "message-square", "Messages processed since start")+
				statCard("Uptime", formatDuration(stats.Uptime), "clock", "Time since manager started")+
				`</div>`+
				`</div>`)
		return err
	}), nil
}

// connectionsPage renders the active connections table.
func connectionsPage(ctx context.Context, manager internal.Manager) (templ.Component, error) {
	conns := manager.GetAllConnections()

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		html := `<div class="space-y-6">` +
			`<div>` +
			`<h2 class="text-2xl font-bold tracking-tight">Connections</h2>` +
			`<p class="text-muted-foreground">Active WebSocket and SSE connections</p>` +
			`</div>` +

			`<div class="rounded-md border">` +
			`<table class="w-full">` +
			`<thead>` +
			`<tr class="border-b bg-muted/50">` +
			`<th class="p-3 text-left text-sm font-medium">Connection ID</th>` +
			`<th class="p-3 text-left text-sm font-medium">User ID</th>` +
			`<th class="p-3 text-left text-sm font-medium">Rooms</th>` +
			`<th class="p-3 text-left text-sm font-medium">Subscriptions</th>` +
			`<th class="p-3 text-left text-sm font-medium">Last Activity</th>` +
			`<th class="p-3 text-left text-sm font-medium">Status</th>` +
			`</tr>` +
			`</thead>` +
			`<tbody>`

		if len(conns) == 0 {
			html += `<tr><td colspan="6" class="p-8 text-center text-muted-foreground">No active connections</td></tr>`
		}

		for _, conn := range conns {
			userID := conn.GetUserID()
			if userID == "" {
				userID = "anonymous"
			}

			rooms := conn.GetJoinedRooms()
			subs := conn.GetSubscriptions()
			lastAct := conn.GetLastActivity()
			status := "active"

			if conn.IsClosed() {
				status = "closed"
			}

			statusBadge := `<span class="inline-flex items-center rounded-full px-2 py-1 text-xs font-medium bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400">` + templ.EscapeString(status) + `</span>`
			if status == "closed" {
				statusBadge = `<span class="inline-flex items-center rounded-full px-2 py-1 text-xs font-medium bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400">` + templ.EscapeString(status) + `</span>`
			}

			html += `<tr class="border-b">` +
				`<td class="p-3 text-sm font-mono">` + templ.EscapeString(truncateID(conn.ID())) + `</td>` +
				`<td class="p-3 text-sm">` + templ.EscapeString(userID) + `</td>` +
				`<td class="p-3 text-sm">` + strconv.Itoa(len(rooms)) + `</td>` +
				`<td class="p-3 text-sm">` + strconv.Itoa(len(subs)) + `</td>` +
				`<td class="p-3 text-sm text-muted-foreground">` + templ.EscapeString(formatTimeAgo(lastAct)) + `</td>` +
				`<td class="p-3 text-sm">` + statusBadge + `</td>` +
				`</tr>`
		}

		html += `</tbody></table></div></div>`

		_, err := io.WriteString(w, html)
		return err
	}), nil
}

// roomsPage renders the rooms listing.
func roomsPage(ctx context.Context, manager internal.Manager, basePath string) (templ.Component, error) {
	rooms, err := manager.ListRooms(ctx)
	if err != nil {
		rooms = nil
	}

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		html := `<div class="space-y-6">` +
			`<div>` +
			`<h2 class="text-2xl font-bold tracking-tight">Rooms</h2>` +
			`<p class="text-muted-foreground">Active streaming rooms and their status</p>` +
			`</div>`

		if len(rooms) == 0 {
			html += `<div class="rounded-md border p-8 text-center text-muted-foreground">` +
				`<p>No rooms created yet</p>` +
				`<p class="text-sm mt-1">Create rooms via the Playground or API</p>` +
				`</div>`
		} else {
			html += `<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">`
			for _, room := range rooms {
				memberCount, _ := room.MemberCount(ctx)
				roomID := templ.EscapeString(room.GetID())
				roomName := templ.EscapeString(room.GetName())
				if roomName == "" {
					roomName = roomID
				}
				roomDesc := templ.EscapeString(room.GetDescription())
				created := room.GetCreated().Format("Jan 2, 2006")
				privacyBadge := `<span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400">public</span>`
				if room.IsPrivate() {
					privacyBadge = `<span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">private</span>`
				}

				archivedBadge := ""
				if room.IsArchived() {
					archivedBadge = ` <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400">archived</span>`
				}

				html += `<a href="` + templ.EscapeString(basePath) + `/rooms/` + roomID + `" class="block rounded-lg border p-4 hover:bg-muted/50 transition-colors">` +
					`<div class="flex items-center justify-between mb-2">` +
					`<h3 class="font-semibold truncate">` + roomName + `</h3>` +
					privacyBadge + archivedBadge +
					`</div>` +
					`<p class="text-sm text-muted-foreground mb-3 line-clamp-2">` + roomDesc + `</p>` +
					`<div class="flex items-center gap-4 text-xs text-muted-foreground">` +
					`<span>` + strconv.Itoa(memberCount) + ` members</span>` +
					`<span>Created ` + templ.EscapeString(created) + `</span>` +
					`</div>` +
					`</a>`
			}
			html += `</div>`
		}

		html += `</div>`

		_, err := io.WriteString(w, html)
		return err
	}), nil
}

// roomDetailPage renders a single room's detail view.
func roomDetailPage(ctx context.Context, manager internal.Manager, roomID, basePath string) (templ.Component, error) {
	room, err := manager.GetRoom(ctx, roomID)
	if err != nil {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			_, err := io.WriteString(w,
				`<div class="space-y-4">`+
					`<a href="`+templ.EscapeString(basePath)+`/rooms" class="text-sm text-primary hover:underline">&larr; Back to rooms</a>`+
					`<div class="rounded-md border p-8 text-center text-muted-foreground">`+
					`<p>Room not found: `+templ.EscapeString(roomID)+`</p>`+
					`</div></div>`)
			return err
		}), nil
	}

	members, _ := room.GetMembers(ctx)
	memberCount, _ := room.MemberCount(ctx)
	msgCount, _ := room.GetMessageCount(ctx)
	modLog, _ := room.GetModerationLog(ctx, 10)
	tags := room.GetTags()

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		roomName := templ.EscapeString(room.GetName())
		if roomName == "" {
			roomName = templ.EscapeString(room.GetID())
		}

		html := `<div class="space-y-6">` +
			`<div class="flex items-center gap-2">` +
			`<a href="` + templ.EscapeString(basePath) + `/rooms" class="text-sm text-primary hover:underline">&larr; Back</a>` +
			`</div>` +

			// Room header
			`<div class="flex items-center justify-between">` +
			`<div>` +
			`<h2 class="text-2xl font-bold tracking-tight">` + roomName + `</h2>` +
			`<p class="text-muted-foreground">` + templ.EscapeString(room.GetDescription()) + `</p>` +
			`</div>` +
			`</div>` +

			// Room info grid
			`<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">` +
			statCard("Members", strconv.Itoa(memberCount), "users", "Total room members") +
			statCard("Messages", formatInt64(msgCount), "message-square", "Total messages in room") +
			statCard("Owner", templ.EscapeString(room.GetOwner()), "crown", "Room owner") +
			statCard("Created", templ.EscapeString(room.GetCreated().Format("Jan 2, 2006")), "calendar", "Room creation date") +
			`</div>`

		// Tags
		if len(tags) > 0 {
			html += `<div class="flex items-center gap-2 flex-wrap">`
			for _, tag := range tags {
				html += `<span class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium bg-primary/10 text-primary">` + templ.EscapeString(tag) + `</span>`
			}
			html += `</div>`
		}

		// Members table
		html += `<div>` +
			`<h3 class="text-lg font-semibold mb-3">Members</h3>` +
			`<div class="rounded-md border">` +
			`<table class="w-full">` +
			`<thead><tr class="border-b bg-muted/50">` +
			`<th class="p-3 text-left text-sm font-medium">User ID</th>` +
			`<th class="p-3 text-left text-sm font-medium">Role</th>` +
			`<th class="p-3 text-left text-sm font-medium">Joined</th>` +
			`</tr></thead><tbody>`

		if len(members) == 0 {
			html += `<tr><td colspan="3" class="p-4 text-center text-muted-foreground">No members</td></tr>`
		}

		for _, m := range members {
			roleBadge := roleBadgeHTML(m.GetRole())
			html += `<tr class="border-b">` +
				`<td class="p-3 text-sm">` + templ.EscapeString(m.GetUserID()) + `</td>` +
				`<td class="p-3 text-sm">` + roleBadge + `</td>` +
				`<td class="p-3 text-sm text-muted-foreground">` + templ.EscapeString(m.GetJoinedAt().Format("Jan 2, 15:04")) + `</td>` +
				`</tr>`
		}

		html += `</tbody></table></div></div>`

		// Moderation log
		if len(modLog) > 0 {
			html += `<div>` +
				`<h3 class="text-lg font-semibold mb-3">Moderation Log</h3>` +
				`<div class="rounded-md border">` +
				`<table class="w-full">` +
				`<thead><tr class="border-b bg-muted/50">` +
				`<th class="p-3 text-left text-sm font-medium">Action</th>` +
				`<th class="p-3 text-left text-sm font-medium">Target</th>` +
				`<th class="p-3 text-left text-sm font-medium">Moderator</th>` +
				`<th class="p-3 text-left text-sm font-medium">Reason</th>` +
				`<th class="p-3 text-left text-sm font-medium">Time</th>` +
				`</tr></thead><tbody>`

			for _, evt := range modLog {
				html += `<tr class="border-b">` +
					`<td class="p-3 text-sm font-medium">` + templ.EscapeString(evt.Type) + `</td>` +
					`<td class="p-3 text-sm">` + templ.EscapeString(evt.TargetID) + `</td>` +
					`<td class="p-3 text-sm">` + templ.EscapeString(evt.ModeratorID) + `</td>` +
					`<td class="p-3 text-sm text-muted-foreground">` + templ.EscapeString(evt.Reason) + `</td>` +
					`<td class="p-3 text-sm text-muted-foreground">` + templ.EscapeString(evt.Timestamp.Format("Jan 2, 15:04")) + `</td>` +
					`</tr>`
			}

			html += `</tbody></table></div></div>`
		}

		html += `</div>`

		_, err := io.WriteString(w, html)
		return err
	}), nil
}

// channelsPage renders the channels listing.
func channelsPage(ctx context.Context, manager internal.Manager) (templ.Component, error) {
	channels, err := manager.ListChannels(ctx)
	if err != nil {
		channels = nil
	}

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		html := `<div class="space-y-6">` +
			`<div>` +
			`<h2 class="text-2xl font-bold tracking-tight">Channels</h2>` +
			`<p class="text-muted-foreground">Pub/sub channels and subscriber counts</p>` +
			`</div>` +

			`<div class="rounded-md border">` +
			`<table class="w-full">` +
			`<thead>` +
			`<tr class="border-b bg-muted/50">` +
			`<th class="p-3 text-left text-sm font-medium">Channel ID</th>` +
			`<th class="p-3 text-left text-sm font-medium">Name</th>` +
			`<th class="p-3 text-left text-sm font-medium">Subscribers</th>` +
			`<th class="p-3 text-left text-sm font-medium">Messages</th>` +
			`<th class="p-3 text-left text-sm font-medium">Created</th>` +
			`</tr>` +
			`</thead>` +
			`<tbody>`

		if len(channels) == 0 {
			html += `<tr><td colspan="5" class="p-8 text-center text-muted-foreground">No channels created</td></tr>`
		}

		for _, ch := range channels {
			subCount, _ := ch.GetSubscriberCount(ctx)
			html += `<tr class="border-b">` +
				`<td class="p-3 text-sm font-mono">` + templ.EscapeString(ch.GetID()) + `</td>` +
				`<td class="p-3 text-sm font-medium">` + templ.EscapeString(ch.GetName()) + `</td>` +
				`<td class="p-3 text-sm">` + strconv.Itoa(subCount) + `</td>` +
				`<td class="p-3 text-sm">` + formatInt64(ch.GetMessageCount()) + `</td>` +
				`<td class="p-3 text-sm text-muted-foreground">` + templ.EscapeString(ch.GetCreated().Format("Jan 2, 2006")) + `</td>` +
				`</tr>`
		}

		html += `</tbody></table></div></div>`

		_, err := io.WriteString(w, html)
		return err
	}), nil
}

// presencePage renders the online users presence grid.
func presencePage(ctx context.Context, manager internal.Manager) (templ.Component, error) {
	conns := manager.GetAllConnections()

	// Build a map of unique users with their presence
	type userPresenceInfo struct {
		userID string
		status string
		rooms  int
		subs   int
	}

	seen := make(map[string]bool)
	var users []userPresenceInfo

	for _, conn := range conns {
		uid := conn.GetUserID()
		if uid == "" || seen[uid] {
			continue
		}

		seen[uid] = true

		presence, _ := manager.GetPresence(ctx, uid)
		status := "online"
		if presence != nil {
			status = presence.Status
		}

		users = append(users, userPresenceInfo{
			userID: uid,
			status: status,
			rooms:  len(conn.GetJoinedRooms()),
			subs:   len(conn.GetSubscriptions()),
		})
	}

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		html := `<div class="space-y-6">` +
			`<div>` +
			`<h2 class="text-2xl font-bold tracking-tight">Presence</h2>` +
			`<p class="text-muted-foreground">Online users and their status</p>` +
			`</div>`

		if len(users) == 0 {
			html += `<div class="rounded-md border p-8 text-center text-muted-foreground">` +
				`<p>No users online</p></div>`
		} else {
			html += `<div class="grid gap-3 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">`
			for _, u := range users {
				statusColor := presenceStatusColor(u.status)
				html += `<div class="rounded-lg border p-4">` +
					`<div class="flex items-center gap-3">` +
					`<div class="relative">` +
					`<div class="h-10 w-10 rounded-full bg-muted flex items-center justify-center text-sm font-medium">` + templ.EscapeString(string([]rune(u.userID)[:1])) + `</div>` +
					`<div class="absolute bottom-0 right-0 h-3 w-3 rounded-full border-2 border-background ` + statusColor + `"></div>` +
					`</div>` +
					`<div class="min-w-0">` +
					`<p class="text-sm font-medium truncate">` + templ.EscapeString(u.userID) + `</p>` +
					`<p class="text-xs text-muted-foreground">` + templ.EscapeString(u.status) + `</p>` +
					`</div>` +
					`</div>` +
					`<div class="mt-3 flex items-center gap-3 text-xs text-muted-foreground">` +
					`<span>` + strconv.Itoa(u.rooms) + ` rooms</span>` +
					`<span>` + strconv.Itoa(u.subs) + ` channels</span>` +
					`</div>` +
					`</div>`
			}
			html += `</div>`
		}

		html += `</div>`

		_, err := io.WriteString(w, html)
		return err
	}), nil
}

// playgroundPage renders the interactive playground with forms.
func playgroundPage(ctx context.Context, manager internal.Manager, params contributor.Params) (templ.Component, error) {
	// Check for form submissions
	formAction := params.FormData["action"]
	var resultHTML string

	switch formAction {
	case "create-room":
		roomName := params.FormData["room_name"]
		roomDesc := params.FormData["room_description"]
		ownerID := params.FormData["owner_id"]
		private := params.FormData["private"] == "on"

		if roomName != "" && ownerID != "" {
			room := local.NewRoom(internal.RoomOptions{
				ID:          uuid.New().String(),
				Name:        roomName,
				Description: roomDesc,
				Owner:       ownerID,
				Private:     private,
			})
			if err := manager.CreateRoom(ctx, room); err != nil {
				resultHTML = alertHTML("error", "Failed to create room: "+err.Error())
			} else {
				resultHTML = alertHTML("success", "Room '"+templ.EscapeString(roomName)+"' created successfully (ID: "+templ.EscapeString(room.GetID())+")")
			}
		} else {
			resultHTML = alertHTML("error", "Room name and owner ID are required")
		}

	case "delete-room":
		deleteRoomID := params.FormData["room_id"]
		if deleteRoomID != "" {
			if err := manager.DeleteRoom(ctx, deleteRoomID); err != nil {
				resultHTML = alertHTML("error", "Failed to delete room: "+err.Error())
			} else {
				resultHTML = alertHTML("success", "Room '"+templ.EscapeString(deleteRoomID)+"' deleted")
			}
		}

	case "send-message":
		msgRoomID := params.FormData["room_id"]
		msgUserID := params.FormData["user_id"]
		msgContent := params.FormData["message"]

		if msgRoomID != "" && msgContent != "" {
			msg := &internal.Message{
				Type:      internal.MessageTypeMessage,
				RoomID:    msgRoomID,
				UserID:    msgUserID,
				Data:      msgContent,
				Timestamp: time.Now(),
			}
			if err := manager.BroadcastToRoom(ctx, msgRoomID, msg); err != nil {
				resultHTML = alertHTML("error", "Failed to send message: "+err.Error())
			} else {
				resultHTML = alertHTML("success", "Message sent to room '"+templ.EscapeString(msgRoomID)+"'")
			}
		} else {
			resultHTML = alertHTML("error", "Room ID and message content are required")
		}

	case "set-presence":
		presUserID := params.FormData["user_id"]
		presStatus := params.FormData["status"]

		if presUserID != "" && presStatus != "" {
			if err := manager.SetPresence(ctx, presUserID, presStatus); err != nil {
				resultHTML = alertHTML("error", "Failed to set presence: "+err.Error())
			} else {
				resultHTML = alertHTML("success", "Presence set to '"+templ.EscapeString(presStatus)+"' for user '"+templ.EscapeString(presUserID)+"'")
			}
		}

	case "kick-connection":
		kickConnID := params.FormData["conn_id"]
		kickReason := params.FormData["reason"]

		if kickConnID != "" {
			if err := manager.KickConnection(ctx, kickConnID, kickReason); err != nil {
				resultHTML = alertHTML("error", "Failed to kick connection: "+err.Error())
			} else {
				resultHTML = alertHTML("success", "Connection '"+templ.EscapeString(truncateID(kickConnID))+"' kicked")
			}
		}
	}

	// List current rooms for select dropdowns
	rooms, _ := manager.ListRooms(ctx)

	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		formBasePath := templ.EscapeString(params.BasePath + "/playground")

		html := `<div class="space-y-6">` +
			`<div>` +
			`<h2 class="text-2xl font-bold tracking-tight">Playground</h2>` +
			`<p class="text-muted-foreground">Test streaming features interactively</p>` +
			`</div>`

		// Show result if any
		if resultHTML != "" {
			html += resultHTML
		}

		html += `<div class="grid gap-6 md:grid-cols-2">`

		// Create Room form
		html += formCard("Create Room", formBasePath,
			hiddenInput("action", "create-room")+
				textInput("room_name", "Room Name", "Enter room name", true)+
				textInput("room_description", "Description", "Optional description", false)+
				textInput("owner_id", "Owner ID", "User ID of room owner", true)+
				checkboxInput("private", "Private Room")+
				submitButton("Create Room"))

		// Delete Room form
		html += formCard("Delete Room", formBasePath,
			hiddenInput("action", "delete-room")+
				roomSelectInput("room_id", "Room", rooms)+
				submitButton("Delete Room"))

		// Send Message form
		html += formCard("Send Message", formBasePath,
			hiddenInput("action", "send-message")+
				roomSelectInput("room_id", "Target Room", rooms)+
				textInput("user_id", "Sender User ID", "User ID of sender", false)+
				textareaInput("message", "Message", "Enter message content", true)+
				submitButton("Send Message"))

		// Set Presence form
		html += formCard("Set Presence", formBasePath,
			hiddenInput("action", "set-presence")+
				textInput("user_id", "User ID", "User to update", true)+
				selectInput("status", "Status", []selectOption{
					{Value: "online", Label: "Online"},
					{Value: "away", Label: "Away"},
					{Value: "busy", Label: "Busy"},
					{Value: "offline", Label: "Offline"},
				})+
				submitButton("Set Presence"))

		// Kick Connection form
		html += formCard("Kick Connection", formBasePath,
			hiddenInput("action", "kick-connection")+
				textInput("conn_id", "Connection ID", "Connection to kick", true)+
				textInput("reason", "Reason", "Optional reason", false)+
				submitButton("Kick Connection"))

		html += `</div></div>`

		_, err := io.WriteString(w, html)
		return err
	}), nil
}

// ---- HTML helpers ----

func statCard(title, value, icon, description string) string {
	return `<div class="rounded-lg border bg-card p-4">` +
		`<div class="flex items-center justify-between mb-2">` +
		`<span class="text-sm font-medium text-muted-foreground">` + templ.EscapeString(title) + `</span>` +
		lucideIcon(icon, 16) +
		`</div>` +
		`<div class="text-2xl font-bold">` + templ.EscapeString(value) + `</div>` +
		`<p class="text-xs text-muted-foreground mt-1">` + templ.EscapeString(description) + `</p>` +
		`</div>`
}

func alertHTML(alertType, message string) string {
	colorClass := "bg-green-50 text-green-800 border-green-200 dark:bg-green-900/20 dark:text-green-400 dark:border-green-800"
	if alertType == "error" {
		colorClass = "bg-red-50 text-red-800 border-red-200 dark:bg-red-900/20 dark:text-red-400 dark:border-red-800"
	}

	return `<div class="rounded-md border p-4 ` + colorClass + `">` +
		`<p class="text-sm font-medium">` + message + `</p>` +
		`</div>`
}

func formCard(title, action, fields string) string {
	return `<div class="rounded-lg border p-4">` +
		`<h3 class="font-semibold mb-4">` + templ.EscapeString(title) + `</h3>` +
		`<form method="POST" action="` + action + `" class="space-y-3">` +
		fields +
		`</form>` +
		`</div>`
}

func hiddenInput(name, value string) string {
	return `<input type="hidden" name="` + templ.EscapeString(name) + `" value="` + templ.EscapeString(value) + `">`
}

func textInput(name, label, placeholder string, required bool) string {
	req := ""
	if required {
		req = ` required`
	}

	return `<div>` +
		`<label class="text-sm font-medium block mb-1" for="` + templ.EscapeString(name) + `">` + templ.EscapeString(label) + `</label>` +
		`<input type="text" id="` + templ.EscapeString(name) + `" name="` + templ.EscapeString(name) + `" placeholder="` + templ.EscapeString(placeholder) + `" class="w-full rounded-md border px-3 py-2 text-sm bg-background"` + req + `>` +
		`</div>`
}

func textareaInput(name, label, placeholder string, required bool) string {
	req := ""
	if required {
		req = ` required`
	}

	return `<div>` +
		`<label class="text-sm font-medium block mb-1" for="` + templ.EscapeString(name) + `">` + templ.EscapeString(label) + `</label>` +
		`<textarea id="` + templ.EscapeString(name) + `" name="` + templ.EscapeString(name) + `" placeholder="` + templ.EscapeString(placeholder) + `" rows="3" class="w-full rounded-md border px-3 py-2 text-sm bg-background"` + req + `></textarea>` +
		`</div>`
}

func checkboxInput(name, label string) string {
	return `<div class="flex items-center gap-2">` +
		`<input type="checkbox" id="` + templ.EscapeString(name) + `" name="` + templ.EscapeString(name) + `" class="rounded border">` +
		`<label class="text-sm" for="` + templ.EscapeString(name) + `">` + templ.EscapeString(label) + `</label>` +
		`</div>`
}

type selectOption struct {
	Value string
	Label string
}

func selectInput(name, label string, options []selectOption) string {
	html := `<div>` +
		`<label class="text-sm font-medium block mb-1" for="` + templ.EscapeString(name) + `">` + templ.EscapeString(label) + `</label>` +
		`<select id="` + templ.EscapeString(name) + `" name="` + templ.EscapeString(name) + `" class="w-full rounded-md border px-3 py-2 text-sm bg-background">`

	for _, opt := range options {
		html += `<option value="` + templ.EscapeString(opt.Value) + `">` + templ.EscapeString(opt.Label) + `</option>`
	}

	html += `</select></div>`
	return html
}

func roomSelectInput(name, label string, rooms []internal.Room) string {
	html := `<div>` +
		`<label class="text-sm font-medium block mb-1" for="` + templ.EscapeString(name) + `">` + templ.EscapeString(label) + `</label>` +
		`<select id="` + templ.EscapeString(name) + `" name="` + templ.EscapeString(name) + `" class="w-full rounded-md border px-3 py-2 text-sm bg-background">`

	if len(rooms) == 0 {
		html += `<option value="">No rooms available</option>`
	}

	for _, r := range rooms {
		rName := r.GetName()
		if rName == "" {
			rName = r.GetID()
		}

		html += `<option value="` + templ.EscapeString(r.GetID()) + `">` + templ.EscapeString(rName) + `</option>`
	}

	html += `</select></div>`
	return html
}

func submitButton(label string) string {
	return `<button type="submit" class="inline-flex items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors">` +
		templ.EscapeString(label) +
		`</button>`
}

func roleBadgeHTML(role string) string {
	colorClass := "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300"

	switch role {
	case "owner":
		colorClass = "bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
	case "admin":
		colorClass = "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400"
	case "member":
		colorClass = "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400"
	}

	return `<span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ` + colorClass + `">` + templ.EscapeString(role) + `</span>`
}

func presenceStatusColor(status string) string {
	switch status {
	case "online":
		return "bg-green-500"
	case "away":
		return "bg-yellow-500"
	case "busy":
		return "bg-red-500"
	default:
		return "bg-gray-400"
	}
}

func lucideIcon(name string, size int) string {
	sizeStr := strconv.Itoa(size)
	// Use a simple icon representation with Lucide icon name in data attribute.
	// The dashboard layout injects the Lucide icon library which replaces these.
	return `<i data-lucide="` + templ.EscapeString(name) + `" class="h-` + sizeStr + ` w-` + sizeStr + `"></i>`
}

func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12] + "..."
	}

	return id
}

func formatInt64(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}

	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}

	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}

	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}

	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
	}

	days := hours / 24
	return fmt.Sprintf("%dd %dh", days, hours%24)
}

func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	d := time.Since(t)

	if d < time.Minute {
		return "just now"
	}

	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}

	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}

	return t.Format("Jan 2, 15:04")
}
