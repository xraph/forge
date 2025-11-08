package internal

import (
	"context"
	"time"
)

// PresenceTracker manages user presence tracking (online/offline/away status).
type PresenceTracker interface {
	// Set and get presence
	SetPresence(ctx context.Context, userID, status string) error
	GetPresence(ctx context.Context, userID string) (*UserPresence, error)
	GetPresences(ctx context.Context, userIDs []string) ([]*UserPresence, error)

	// Activity tracking
	TrackActivity(ctx context.Context, userID string) error
	GetLastSeen(ctx context.Context, userID string) (time.Time, error)

	// Online users
	GetOnlineUsers(ctx context.Context) ([]string, error)
	GetOnlineUsersInRoom(ctx context.Context, roomID string) ([]string, error)
	IsOnline(ctx context.Context, userID string) (bool, error)

	// Custom status
	SetCustomStatus(ctx context.Context, userID, customStatus string) error
	GetCustomStatus(ctx context.Context, userID string) (string, error)

	// Broadcasting
	BroadcastPresence(ctx context.Context, roomID, userID, status string) error

	// Cleanup expired presence
	CleanupExpired(ctx context.Context) error

	// Bulk Operations
	SetPresenceForUsers(ctx context.Context, updates map[string]string) error
	GetPresenceForRooms(ctx context.Context, roomIDs []string) (map[string][]*UserPresence, error)
	GetPresenceBulk(ctx context.Context, userIDs []string) (map[string]*UserPresence, error)

	// Advanced Queries
	GetUsersByStatus(ctx context.Context, status string) ([]string, error)
	GetRecentlyOnline(ctx context.Context, since time.Duration) ([]string, error)
	GetRecentlyOffline(ctx context.Context, since time.Duration) ([]string, error)
	GetAwayUsers(ctx context.Context) ([]string, error)
	GetBusyUsers(ctx context.Context) ([]string, error)

	// Presence History
	GetPresenceHistory(ctx context.Context, userID string, since time.Time) ([]*PresenceEvent, error)
	GetStatusChanges(ctx context.Context, userID string, limit int) ([]*PresenceEvent, error)

	// Device Management
	AddDevice(ctx context.Context, userID, deviceID string, deviceInfo DeviceInfo) error
	RemoveDevice(ctx context.Context, userID, deviceID string) error
	GetDevices(ctx context.Context, userID string) ([]DeviceInfo, error)
	GetActiveDevices(ctx context.Context, userID string) ([]DeviceInfo, error)

	// Rich Presence
	SetRichPresence(ctx context.Context, userID string, richData map[string]any) error
	GetRichPresence(ctx context.Context, userID string) (map[string]any, error)
	SetActivity(ctx context.Context, userID string, activity *ActivityInfo) error
	GetActivity(ctx context.Context, userID string) (*ActivityInfo, error)

	// Availability
	SetAvailability(ctx context.Context, userID string, available bool, message string) error
	IsAvailable(ctx context.Context, userID string) (bool, string, error)

	// Time-based
	GetOnlineUsersAt(ctx context.Context, timestamp time.Time) ([]string, error)
	GetPresenceDuration(ctx context.Context, userID string, status string, since time.Time) (time.Duration, error)

	// Watching/Subscriptions
	WatchUser(ctx context.Context, watcherID, watchedUserID string) error
	UnwatchUser(ctx context.Context, watcherID, watchedUserID string) error
	GetWatchers(ctx context.Context, userID string) ([]string, error)
	GetWatching(ctx context.Context, userID string) ([]string, error)

	// Statistics
	GetOnlineStats(ctx context.Context) (*OnlineStats, error)
	GetPresenceStats(ctx context.Context, userID string) (*UserPresenceStats, error)

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// PresenceStore provides backend storage for presence data.
type PresenceStore interface {
	// Set and get presence
	Set(ctx context.Context, userID string, presence *UserPresence) error
	Get(ctx context.Context, userID string) (*UserPresence, error)
	GetMultiple(ctx context.Context, userIDs []string) ([]*UserPresence, error)
	Delete(ctx context.Context, userID string) error

	// Online users
	GetOnline(ctx context.Context) ([]string, error)
	SetOnline(ctx context.Context, userID string, ttl time.Duration) error
	SetOffline(ctx context.Context, userID string) error
	IsOnline(ctx context.Context, userID string) (bool, error)

	// Activity tracking
	UpdateActivity(ctx context.Context, userID string, timestamp time.Time) error
	GetLastActivity(ctx context.Context, userID string) (time.Time, error)

	// Cleanup
	CleanupExpired(ctx context.Context, olderThan time.Duration) error

	// Bulk operations
	SetMultiple(ctx context.Context, presences map[string]*UserPresence) error
	DeleteMultiple(ctx context.Context, userIDs []string) error

	// Advanced queries
	GetByStatus(ctx context.Context, status string) ([]*UserPresence, error)
	GetRecent(ctx context.Context, status string, since time.Duration) ([]*UserPresence, error)
	GetWithFilters(ctx context.Context, filters PresenceFilters) ([]*UserPresence, error)

	// History
	SaveHistory(ctx context.Context, userID string, event *PresenceEvent) error
	GetHistory(ctx context.Context, userID string, limit int) ([]*PresenceEvent, error)
	GetHistorySince(ctx context.Context, userID string, since time.Time) ([]*PresenceEvent, error)

	// Device tracking
	SetDevice(ctx context.Context, userID, deviceID string, device DeviceInfo) error
	GetDevices(ctx context.Context, userID string) ([]DeviceInfo, error)
	RemoveDevice(ctx context.Context, userID, deviceID string) error

	// Statistics
	CountByStatus(ctx context.Context) (map[string]int, error)
	GetActiveCount(ctx context.Context, since time.Duration) (int, error)

	// Lifecycle
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
}

// PresenceOptions contains configuration for presence tracking.
type PresenceOptions struct {
	// Timeout after which user is considered offline
	OfflineTimeout time.Duration

	// Interval for cleanup of expired presence
	CleanupInterval time.Duration

	// Broadcast presence changes to rooms
	BroadcastChanges bool

	// Track activity automatically
	AutoTrackActivity bool
}

// DefaultPresenceOptions returns default presence tracking options.
func DefaultPresenceOptions() PresenceOptions {
	return PresenceOptions{
		OfflineTimeout:    5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
		BroadcastChanges:  true,
		AutoTrackActivity: true,
	}
}

// PresenceEvent represents a presence change event.
type PresenceEvent struct {
	Type      string         `json:"type"` // "online", "offline", "away", "busy"
	UserID    string         `json:"user_id"`
	Status    string         `json:"status"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// DeviceInfo represents information about a user's device.
type DeviceInfo struct {
	DeviceID string         `json:"device_id"`
	Type     string         `json:"type"` // "mobile", "desktop", "tablet", "web"
	OS       string         `json:"os,omitempty"`
	Browser  string         `json:"browser,omitempty"`
	LastSeen time.Time      `json:"last_seen"`
	IP       string         `json:"ip,omitempty"`
	Active   bool           `json:"active"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ActivityInfo represents rich activity/presence information (Discord-style).
type ActivityInfo struct {
	Type      string         `json:"type"` // "playing", "listening", "watching", "streaming", "custom"
	Name      string         `json:"name"`
	Details   string         `json:"details,omitempty"`
	State     string         `json:"state,omitempty"`
	StartTime time.Time      `json:"start_time,omitempty"`
	EndTime   *time.Time     `json:"end_time,omitempty"`
	Assets    map[string]any `json:"assets,omitempty"` // Images, icons, etc.
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Activity types.
const (
	ActivityTypePlaying   = "playing"
	ActivityTypeListening = "listening"
	ActivityTypeWatching  = "watching"
	ActivityTypeStreaming = "streaming"
	ActivityTypeCustom    = "custom"
)

// OnlineStats provides statistics about online users.
type OnlineStats struct {
	Current    int            `json:"current"`
	Peak24h    int            `json:"peak_24h"`
	Average24h float64        `json:"average_24h"`
	ByStatus   map[string]int `json:"by_status"`
	ByTimezone map[string]int `json:"by_timezone,omitempty"`
	Trend      string         `json:"trend"` // "increasing", "decreasing", "stable"
}

// UserPresenceStats provides detailed statistics for a user's presence.
type UserPresenceStats struct {
	TotalOnlineTime   time.Duration  `json:"total_online_time"`
	AverageOnlineTime time.Duration  `json:"average_online_time"`
	LastOnline        time.Time      `json:"last_online"`
	StatusChanges     int            `json:"status_changes"`
	MostCommonStatus  string         `json:"most_common_status"`
	DeviceCount       int            `json:"device_count"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// PresenceFilters defines filters for querying presence data.
type PresenceFilters struct {
	Status        []string  `json:"status,omitempty"`
	Online        bool      `json:"online,omitempty"`
	SinceActivity time.Time `json:"since_activity,omitempty"`
	RoomID        string    `json:"room_id,omitempty"`
	DeviceType    string    `json:"device_type,omitempty"`
}

// Availability represents user availability status.
type Availability struct {
	Available bool      `json:"available"`
	Message   string    `json:"message,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}
