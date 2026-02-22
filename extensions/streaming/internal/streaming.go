package internal

import (
	"context"
	"time"

	"github.com/xraph/forge"
)

// Manager is the central interface for managing streaming connections,
// rooms, channels, presence, and message broadcasting.
type Manager interface {
	// Connection management
	Register(conn EnhancedConnection) error
	Unregister(connID string) error
	GetConnection(connID string) (EnhancedConnection, error)
	GetUserConnections(userID string) []EnhancedConnection
	GetAllConnections() []EnhancedConnection
	ConnectionCount() int

	// Advanced Connection Management
	GetConnectionsByStatus(status string) []EnhancedConnection
	KickConnection(ctx context.Context, connID string, reason string) error
	GetConnectionInfo(connID string) (*ConnectionInfo, error)
	GetIdleConnections(idleFor time.Duration) []EnhancedConnection
	CleanupIdleConnections(ctx context.Context, idleFor time.Duration) (int, error)

	// Room operations
	CreateRoom(ctx context.Context, room Room) error
	GetRoom(ctx context.Context, roomID string) (Room, error)
	DeleteRoom(ctx context.Context, roomID string) error
	JoinRoom(ctx context.Context, connID, roomID string) error
	LeaveRoom(ctx context.Context, connID, roomID string) error
	GetRoomMembers(ctx context.Context, roomID string) ([]Member, error)
	ListRooms(ctx context.Context) ([]Room, error)

	// Room Management Extended
	UpdateRoom(ctx context.Context, roomID string, updates map[string]any) error
	SearchRooms(ctx context.Context, query string, filters map[string]any) ([]Room, error)
	GetPublicRooms(ctx context.Context, limit int) ([]Room, error)
	GetUserRoomCount(ctx context.Context, userID string) (int, error)
	ArchiveRoom(ctx context.Context, roomID string) error
	RestoreRoom(ctx context.Context, roomID string) error
	TransferRoomOwnership(ctx context.Context, roomID, newOwnerID string) error

	// Channel operations
	CreateChannel(ctx context.Context, channel Channel) error
	GetChannel(ctx context.Context, channelID string) (Channel, error)
	DeleteChannel(ctx context.Context, channelID string) error
	Subscribe(ctx context.Context, connID, channelID string, filters map[string]any) error
	Unsubscribe(ctx context.Context, connID, channelID string) error
	ListChannels(ctx context.Context) ([]Channel, error)

	// Channel Management Extended
	UpdateChannel(ctx context.Context, channelID string, updates map[string]any) error
	GetChannelSubscribers(ctx context.Context, channelID string) ([]string, error)
	GetUserChannels(ctx context.Context, userID string) ([]Channel, error)

	// Message broadcasting
	Broadcast(ctx context.Context, message *Message) error
	BroadcastToRoom(ctx context.Context, roomID string, message *Message) error
	BroadcastToChannel(ctx context.Context, channelID string, message *Message) error
	SendToUser(ctx context.Context, userID string, message *Message) error
	SendToConnection(ctx context.Context, connID string, message *Message) error

	// Bulk Broadcasting
	BroadcastToUsers(ctx context.Context, userIDs []string, message *Message) error
	BroadcastToRooms(ctx context.Context, roomIDs []string, message *Message) error
	BroadcastExcept(ctx context.Context, message *Message, excludeUserIDs []string) error
	BulkJoinRoom(ctx context.Context, connIDs []string, roomID string) error

	// Presence operations
	SetPresence(ctx context.Context, userID, status string) error
	GetPresence(ctx context.Context, userID string) (*UserPresence, error)
	GetOnlineUsers(ctx context.Context, roomID string) ([]string, error)
	TrackActivity(ctx context.Context, userID string) error

	// Presence Extended
	GetPresenceForUsers(ctx context.Context, userIDs []string) ([]*UserPresence, error)
	SetCustomStatus(ctx context.Context, userID, customStatus string) error
	GetOnlineCount(ctx context.Context) (int, error)
	GetPresenceInRooms(ctx context.Context, roomIDs []string) (map[string][]string, error)

	// Typing operations
	StartTyping(ctx context.Context, userID, roomID string) error
	StopTyping(ctx context.Context, userID, roomID string) error
	GetTypingUsers(ctx context.Context, roomID string) ([]string, error)

	// Typing Extended
	GetTypingUsersInChannels(ctx context.Context, channelIDs []string) (map[string][]string, error)
	IsTyping(ctx context.Context, userID, roomID string) (bool, error)
	ClearTyping(ctx context.Context, userID string) error

	// Message history
	SaveMessage(ctx context.Context, message *Message) error
	GetHistory(ctx context.Context, roomID string, query HistoryQuery) ([]*Message, error)

	// Message History Extended
	GetThreadHistory(ctx context.Context, roomID, threadID string, query HistoryQuery) ([]*Message, error)
	GetUserMessages(ctx context.Context, userID string, query HistoryQuery) ([]*Message, error)
	SearchMessages(ctx context.Context, roomID, searchTerm string, query HistoryQuery) ([]*Message, error)
	DeleteMessage(ctx context.Context, messageID string) error
	EditMessage(ctx context.Context, messageID string, newContent any) error

	// Reactions/Interactions
	AddReaction(ctx context.Context, messageID, userID, emoji string) error
	RemoveReaction(ctx context.Context, messageID, userID, emoji string) error
	GetReactions(ctx context.Context, messageID string) (map[string][]string, error)

	// Moderation
	MuteUser(ctx context.Context, userID, roomID string, duration time.Duration) error
	UnmuteUser(ctx context.Context, userID, roomID string) error
	BanUser(ctx context.Context, userID, roomID string, reason string, until *time.Time) error
	UnbanUser(ctx context.Context, userID, roomID string) error
	GetModerationLog(ctx context.Context, roomID string, limit int) ([]ModerationEvent, error)

	// Rate Limiting
	CheckRateLimit(ctx context.Context, userID string, action string) (bool, error)
	GetRateLimitStatus(ctx context.Context, userID string) (*RateLimitStatus, error)

	// Statistics & Monitoring
	GetStats(ctx context.Context) (*ManagerStats, error)
	GetRoomStats(ctx context.Context, roomID string) (*RoomStats, error)
	GetUserStats(ctx context.Context, userID string) (*UserStats, error)
	GetActiveRooms(ctx context.Context, since time.Duration) ([]Room, error)

	// Direct Messaging
	CreateDirectMessage(ctx context.Context, fromUserID, toUserID string) (string, error)
	GetDirectMessages(ctx context.Context, userID string) ([]Room, error)
	IsDirectMessage(ctx context.Context, roomID string) (bool, error)

	// Session Resumption
	ResumeSession(ctx context.Context, connID, sessionID string) (bool, error)

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health(ctx context.Context) error
}

// EnhancedConnection wraps the core forge.Connection with streaming metadata.
type EnhancedConnection interface {
	forge.Connection

	// Metadata
	GetUserID() string
	SetUserID(userID string)
	GetSessionID() string
	SetSessionID(sessionID string)
	GetMetadata(key string) (any, bool)
	SetMetadata(key string, value any)

	// Tracking
	GetJoinedRooms() []string
	AddRoom(roomID string)
	RemoveRoom(roomID string)
	IsInRoom(roomID string) bool

	GetSubscriptions() []string
	AddSubscription(channelID string)
	RemoveSubscription(channelID string)
	IsSubscribed(channelID string) bool

	GetLastActivity() time.Time
	UpdateActivity()

	// State
	IsClosed() bool
	MarkClosed()
}

// Message represents a streaming message with rich metadata.
type Message struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"` // "message", "presence", "typing", "system", "join", "leave", "error"
	Event     string         `json:"event,omitempty"`
	RoomID    string         `json:"room_id,omitempty"`
	ChannelID string         `json:"channel_id,omitempty"`
	UserID    string         `json:"user_id"`
	Data      any            `json:"data"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	ThreadID  string         `json:"thread_id,omitempty"` // For threaded conversations
}

// Message types.
const (
	MessageTypeMessage  = "message"
	MessageTypePresence = "presence"
	MessageTypeTyping   = "typing"
	MessageTypeSystem   = "system"
	MessageTypeJoin     = "join"
	MessageTypeLeave    = "leave"
	MessageTypeError    = "error"
)

// HistoryQuery defines parameters for retrieving message history.
type HistoryQuery struct {
	Limit      int       // Maximum number of messages
	Before     time.Time // Messages before this timestamp
	After      time.Time // Messages after this timestamp
	ThreadID   string    // Filter by thread
	UserID     string    // Filter by user
	SearchTerm string    // Full-text search
}

// UserPresence represents a user's online presence status.
type UserPresence struct {
	UserID       string         `json:"user_id"`
	Status       string         `json:"status"` // "online", "away", "busy", "offline"
	LastSeen     time.Time      `json:"last_seen"`
	Connections  []string       `json:"connections"`
	CustomStatus string         `json:"custom_status,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Status constants.
const (
	StatusOnline  = "online"
	StatusAway    = "away"
	StatusBusy    = "busy"
	StatusOffline = "offline"
)

// ConnectionInfo provides detailed information about a connection.
type ConnectionInfo struct {
	ID            string         `json:"id"`
	UserID        string         `json:"user_id"`
	SessionID     string         `json:"session_id"`
	ConnectedAt   time.Time      `json:"connected_at"`
	LastActivity  time.Time      `json:"last_activity"`
	RoomsJoined   []string       `json:"rooms_joined"`
	Subscriptions []string       `json:"subscriptions"`
	IPAddress     string         `json:"ip_address,omitempty"`
	UserAgent     string         `json:"user_agent,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// RateLimitStatus represents rate limiting status for a user.
type RateLimitStatus struct {
	Allowed    bool          `json:"allowed"`
	Remaining  int           `json:"remaining"`
	ResetAt    time.Time     `json:"reset_at"`
	RetryAfter time.Duration `json:"retry_after"`
}

// ManagerStats provides overall streaming statistics.
type ManagerStats struct {
	TotalConnections int           `json:"total_connections"`
	TotalRooms       int           `json:"total_rooms"`
	TotalChannels    int           `json:"total_channels"`
	TotalMessages    int64         `json:"total_messages"`
	OnlineUsers      int           `json:"online_users"`
	MessagesPerSec   float64       `json:"messages_per_sec"`
	Uptime           time.Duration `json:"uptime"`
	MemoryUsage      int64         `json:"memory_usage,omitempty"`
}

// RoomStats provides statistics for a specific room.
type RoomStats struct {
	TotalMessages   int64     `json:"total_messages"`
	TotalMembers    int       `json:"total_members"`
	ActiveMembers   int       `json:"active_members"`
	MessagesToday   int64     `json:"messages_today"`
	AverageMessages float64   `json:"average_messages"`
	PeakOnline      int       `json:"peak_online"`
	CreatedAt       time.Time `json:"created_at"`
	LastActivity    time.Time `json:"last_activity"`
}

// UserStats provides statistics for a specific user.
type UserStats struct {
	MessagesSent    int64         `json:"messages_sent"`
	RoomsJoined     int           `json:"rooms_joined"`
	OnlineTime      time.Duration `json:"online_time"`
	LastSeen        time.Time     `json:"last_seen"`
	AverageActivity float64       `json:"average_activity"`
}

// MessageReaction represents a reaction to a message.
type MessageReaction struct {
	MessageID string    `json:"message_id"`
	UserID    string    `json:"user_id"`
	Emoji     string    `json:"emoji"`
	Timestamp time.Time `json:"timestamp"`
}

// MessageEdit represents an edit to a message.
type MessageEdit struct {
	MessageID   string    `json:"message_id"`
	NewContent  any       `json:"new_content"`
	EditedBy    string    `json:"edited_by"`
	EditedAt    time.Time `json:"edited_at"`
	PrevContent any       `json:"prev_content,omitempty"`
}

// ModerationStatus represents the status of a moderation action.
type ModerationStatus string

const (
	ModerationStatusPending  ModerationStatus = "pending"
	ModerationStatusApproved ModerationStatus = "approved"
	ModerationStatusRejected ModerationStatus = "rejected"
	ModerationStatusActive   ModerationStatus = "active"
	ModerationStatusExpired  ModerationStatus = "expired"
)

// MessageSearchQuery represents a query for searching messages.
type MessageSearchQuery struct {
	Query   string         `json:"query"`
	RoomID  string         `json:"room_id,omitempty"`
	UserID  string         `json:"user_id,omitempty"`
	Before  *time.Time     `json:"before,omitempty"`
	After   *time.Time     `json:"after,omitempty"`
	Limit   int            `json:"limit,omitempty"`
	Offset  int            `json:"offset,omitempty"`
	Filters map[string]any `json:"filters,omitempty"`
}

// AnalyticsQuery represents a query for analytics data.
type AnalyticsQuery struct {
	Metric    string         `json:"metric"`
	StartTime time.Time      `json:"start_time"`
	EndTime   time.Time      `json:"end_time"`
	GroupBy   string         `json:"group_by,omitempty"`
	Filters   map[string]any `json:"filters,omitempty"`
	Limit     int            `json:"limit,omitempty"`
}

// AnalyticsResult represents the result of an analytics query.
type AnalyticsResult struct {
	Metric    string            `json:"metric"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// AnalyticsEvent represents an event for analytics tracking.
type AnalyticsEvent struct {
	Type      string         `json:"type"`
	UserID    string         `json:"user_id,omitempty"`
	RoomID    string         `json:"room_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// FileUpload represents a file upload request.
type FileUpload struct {
	ID          string         `json:"id"`
	Filename    string         `json:"filename"`
	ContentType string         `json:"content_type"`
	Size        int64          `json:"size"`
	UserID      string         `json:"user_id"`
	RoomID      string         `json:"room_id,omitempty"`
	Data        []byte         `json:"data,omitempty"`
	URL         string         `json:"url,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	UploadedAt  time.Time      `json:"uploaded_at"`
}

// FileInfo represents information about an uploaded file.
type FileInfo struct {
	ID           string         `json:"id"`
	Filename     string         `json:"filename"`
	ContentType  string         `json:"content_type"`
	Size         int64          `json:"size"`
	UserID       string         `json:"user_id"`
	RoomID       string         `json:"room_id,omitempty"`
	URL          string         `json:"url"`
	ThumbnailURL string         `json:"thumbnail_url,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	UploadedAt   time.Time      `json:"uploaded_at"`
	ExpiresAt    *time.Time     `json:"expires_at,omitempty"`
}

// FileQuery represents a query for searching files.
type FileQuery struct {
	UserID      string         `json:"user_id,omitempty"`
	RoomID      string         `json:"room_id,omitempty"`
	ContentType string         `json:"content_type,omitempty"`
	Before      *time.Time     `json:"before,omitempty"`
	After       *time.Time     `json:"after,omitempty"`
	Limit       int            `json:"limit,omitempty"`
	Offset      int            `json:"offset,omitempty"`
	Filters     map[string]any `json:"filters,omitempty"`
}

// WebhookConfig represents webhook configuration.
type WebhookConfig struct {
	ID         string            `json:"id"`
	URL        string            `json:"url"`
	Events     []string          `json:"events"`
	Secret     string            `json:"secret,omitempty"`
	Active     bool              `json:"active"`
	Headers    map[string]string `json:"headers,omitempty"`
	Timeout    time.Duration     `json:"timeout,omitempty"`
	RetryCount int               `json:"retry_count,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}
