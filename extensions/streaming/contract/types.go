package contract

import "time"

// StatsResponse is the wire shape for the stats query intent.
type StatsResponse struct {
	TotalConnections int     `json:"totalConnections"`
	TotalRooms       int     `json:"totalRooms"`
	TotalChannels    int     `json:"totalChannels"`
	TotalMessages    int64   `json:"totalMessages"`
	OnlineUsers      int     `json:"onlineUsers"`
	MessagesPerSec   float64 `json:"messagesPerSec"`
	UptimeSeconds    int64   `json:"uptimeSeconds"`
	MemoryBytes      int64   `json:"memoryBytes"`
}

// ConnectionInfo is the wire shape for one row in connections.list.
type ConnectionInfo struct {
	ConnID         string    `json:"connID"`
	UserID         string    `json:"userID"`
	Transport      string    `json:"transport"`
	JoinedRooms    []string  `json:"joinedRooms"`
	Subscriptions  []string  `json:"subscriptions"`
	LastActivity   time.Time `json:"lastActivity"`
	Status         string    `json:"status"`
}

// ConnectionsList wraps a slice of ConnectionInfo for resource.list compatibility.
type ConnectionsList struct {
	Connections []ConnectionInfo `json:"connections"`
}

// RoomInfo is the wire shape for rooms.list rows.
type RoomInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Owner       string    `json:"owner"`
	Members     int       `json:"members"`
	Private     bool      `json:"private"`
	Archived    bool      `json:"archived"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

type RoomsList struct {
	Rooms []RoomInfo `json:"rooms"`
}

// MemberInfo is one row of rooms.members.
type MemberInfo struct {
	UserID      string    `json:"userID"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
	Permissions []string  `json:"permissions"`
}

type MembersList struct {
	Members []MemberInfo `json:"members"`
}

// ModerationEntry is one row of rooms.moderation.
type ModerationEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Action    string         `json:"action"`
	ActorID   string         `json:"actorID"`
	TargetID  string         `json:"targetID"`
	Reason    string         `json:"reason"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ModerationLog struct {
	Entries []ModerationEntry `json:"entries"`
}

// ChannelInfo is one row of channels.list.
type ChannelInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	SubscriberCount int    `json:"subscriberCount"`
	MessageCount    int64  `json:"messageCount"`
}

type ChannelsList struct {
	Channels []ChannelInfo `json:"channels"`
}

// PresenceInfo is one row of presence.list.
type PresenceInfo struct {
	UserID       string    `json:"userID"`
	Status       string    `json:"status"`
	CustomStatus string    `json:"customStatus,omitempty"`
	LastSeen     time.Time `json:"lastSeen"`
	Rooms        []string  `json:"rooms"`
}

type PresenceList struct {
	Presence []PresenceInfo `json:"presence"`
}

// ConfigSummary is the wire shape for the config query intent. Slice (f)'s
// settings page is read-only; it surfaces the most relevant fields without
// echoing every internal toggle.
type ConfigSummary struct {
	BackendType   string         `json:"backendType,omitempty"`
	Distributed   bool           `json:"distributed"`
	NodeID        string         `json:"nodeID,omitempty"`
	Features      map[string]any `json:"features"`
	Limits        map[string]any `json:"limits"`
	Timeouts      map[string]any `json:"timeouts"`
}

// --- Mutation request payloads ---

type CreateRoomInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
	Private     bool   `json:"private"`
}

type DeleteRoomInput struct {
	ID string `json:"id"`
}

type SendMessageInput struct {
	RoomID  string `json:"roomID"`
	UserID  string `json:"userID"`
	Content string `json:"content"`
}

type SetPresenceInput struct {
	UserID string `json:"userID"`
	Status string `json:"status"`
}

type KickConnectionInput struct {
	ConnID string `json:"connID"`
	Reason string `json:"reason"`
}

// CommandResult is the uniform payload returned by every mutation. Empty when
// the command produced no body but succeeded.
type CommandResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	// ID is set on creation commands (e.g. rooms.create) so the React shell
	// can navigate to the new resource.
	ID string `json:"id,omitempty"`
}

// RoomDetailInput is the input for rooms.detail.
type RoomDetailInput struct {
	ID string `json:"id"`
}
