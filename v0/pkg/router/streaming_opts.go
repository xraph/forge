package router

import (
	"reflect"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// =============================================================================
// STREAMING HANDLER OPTIONS
// =============================================================================

// WithStreamingTags sets tags for streaming handlers
func WithStreamingTags(tags ...string) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		info.Tags = append(info.Tags, tags...)
	}
}

// WithStreamingProtocol sets the protocol for the streaming handler
func WithStreamingProtocol(protocol string) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		info.Protocol = protocol
	}
}

// WithStreamingSummary sets the summary for streaming handlers
func WithStreamingSummary(summary string) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		info.Summary = summary
	}
}

// WithStreamingDescription sets the description for streaming handlers
func WithStreamingDescription(description string) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		info.Description = description
	}
}

// WithStreamingAuth enables authentication for streaming handlers
func WithStreamingAuth(requireAuth bool) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		info.Authentication = requireAuth
	}
}

// WithStreamingConnectionLimit sets the connection limit for streaming handlers
func WithStreamingConnectionLimit(limit int) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		info.ConnectionLimit = limit
	}
}

// WithStreamingHeartbeat sets the heartbeat interval for streaming handlers
func WithStreamingHeartbeat(interval time.Duration) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		info.HeartbeatInterval = interval
	}
}

// WithWebSocketMessageTypes sets the message types for WebSocket handlers
func WithWebSocketMessageTypes(messageTypes map[string]reflect.Type) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		if info.MessageTypes == nil {
			info.MessageTypes = make(map[string]reflect.Type)
		}
		for name, msgType := range messageTypes {
			info.MessageTypes[name] = msgType
		}
	}
}

// WithSSEEventTypes sets the event types for SSE handlers
func WithSSEEventTypes(eventTypes map[string]reflect.Type) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		if info.EventTypes == nil {
			info.EventTypes = make(map[string]reflect.Type)
		}
		for name, eventType := range eventTypes {
			info.EventTypes[name] = eventType
		}
	}
}

// WithWebSocketMessageType adds a single message type for WebSocket handlers
func WithWebSocketMessageType(name string, msgType reflect.Type) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		if info.MessageTypes == nil {
			info.MessageTypes = make(map[string]reflect.Type)
		}
		info.MessageTypes[name] = msgType
	}
}

// WithSSEEventType adds a single event type for SSE handlers
func WithSSEEventType(name string, eventType reflect.Type) common.StreamingHandlerOption {
	return func(info *common.StreamingHandlerInfo) {
		if info.EventTypes == nil {
			info.EventTypes = make(map[string]reflect.Type)
		}
		info.EventTypes[name] = eventType
	}
}

// =============================================================================
// PREDEFINED STREAMING CONFIGURATIONS
// =============================================================================

// ChatWebSocketHandler creates options for a chat WebSocket handler
func ChatWebSocketHandler() []common.StreamingHandlerOption {
	return []common.StreamingHandlerOption{
		WithStreamingTags("chat", "websocket"),
		WithStreamingSummary("Real-time chat WebSocket"),
		WithStreamingDescription("WebSocket endpoint for real-time chat messages"),
		WithStreamingAuth(true),
		WithStreamingConnectionLimit(1000),
		WithStreamingHeartbeat(30 * time.Second),
		WithWebSocketMessageTypes(map[string]reflect.Type{
			"chat_message": reflect.TypeOf(ChatMessage{}),
			"user_joined":  reflect.TypeOf(UserJoinedMessage{}),
			"user_left":    reflect.TypeOf(UserLeftMessage{}),
			"typing_start": reflect.TypeOf(TypingMessage{}),
			"typing_stop":  reflect.TypeOf(TypingMessage{}),
		}),
	}
}

// NotificationSSEHandler creates options for a notification SSE handler
func NotificationSSEHandler() []common.StreamingHandlerOption {
	return []common.StreamingHandlerOption{
		WithStreamingTags("notifications", "sse"),
		WithStreamingSummary("Real-time notifications via SSE"),
		WithStreamingDescription("Server-Sent Events endpoint for real-time notifications"),
		WithStreamingAuth(true),
		WithStreamingConnectionLimit(500),
		WithSSEEventTypes(map[string]reflect.Type{
			"notification": reflect.TypeOf(NotificationEvent{}),
			"alert":        reflect.TypeOf(AlertEvent{}),
			"system":       reflect.TypeOf(SystemEvent{}),
		}),
	}
}

// LiveDataSSEHandler creates options for live data streaming via SSE
func LiveDataSSEHandler() []common.StreamingHandlerOption {
	return []common.StreamingHandlerOption{
		WithStreamingTags("data", "streaming", "sse"),
		WithStreamingSummary("Live data streaming via SSE"),
		WithStreamingDescription("Server-Sent Events endpoint for live data updates"),
		WithStreamingAuth(false), // Public data stream
		WithStreamingConnectionLimit(200),
		WithSSEEventTypes(map[string]reflect.Type{
			"data_update": reflect.TypeOf(DataUpdateEvent{}),
			"heartbeat":   reflect.TypeOf(HeartbeatEvent{}),
		}),
	}
}

// GameWebSocketHandler creates options for a gaming WebSocket handler
func GameWebSocketHandler() []common.StreamingHandlerOption {
	return []common.StreamingHandlerOption{
		WithStreamingTags("game", "realtime", "websocket"),
		WithStreamingSummary("Real-time game WebSocket"),
		WithStreamingDescription("WebSocket endpoint for real-time game interactions"),
		WithStreamingAuth(true),
		WithStreamingConnectionLimit(100),
		WithStreamingHeartbeat(10 * time.Second), // Fast heartbeat for games
		WithWebSocketMessageTypes(map[string]reflect.Type{
			"player_move":   reflect.TypeOf(PlayerMoveMessage{}),
			"game_state":    reflect.TypeOf(GameStateMessage{}),
			"player_action": reflect.TypeOf(PlayerActionMessage{}),
			"game_event":    reflect.TypeOf(GameEventMessage{}),
		}),
	}
}

// MonitoringSSEHandler creates options for system monitoring via SSE
func MonitoringSSEHandler() []common.StreamingHandlerOption {
	return []common.StreamingHandlerOption{
		WithStreamingTags("monitoring", "metrics", "sse"),
		WithStreamingSummary("System monitoring via SSE"),
		WithStreamingDescription("Server-Sent Events endpoint for system metrics and monitoring data"),
		WithStreamingAuth(true), // Requires admin auth typically
		WithStreamingConnectionLimit(50),
		WithSSEEventTypes(map[string]reflect.Type{
			"cpu_usage":    reflect.TypeOf(CPUUsageEvent{}),
			"memory_usage": reflect.TypeOf(MemoryUsageEvent{}),
			"disk_usage":   reflect.TypeOf(DiskUsageEvent{}),
			"network_io":   reflect.TypeOf(NetworkIOEvent{}),
			"alert":        reflect.TypeOf(SystemAlertEvent{}),
		}),
	}
}

// =============================================================================
// STREAMING MESSAGE TYPES
// =============================================================================

// Chat-related message types
type ChatMessage struct {
	UserID    string    `json:"user_id" description:"ID of the user sending the message"`
	Username  string    `json:"username" description:"Username of the sender"`
	Content   string    `json:"content" description:"Message content"`
	Timestamp time.Time `json:"timestamp" description:"Message timestamp"`
	RoomID    string    `json:"room_id,omitempty" description:"Chat room ID"`
}

type UserJoinedMessage struct {
	UserID   string `json:"user_id" description:"ID of the user who joined"`
	Username string `json:"username" description:"Username of the user who joined"`
	RoomID   string `json:"room_id,omitempty" description:"Chat room ID"`
}

type UserLeftMessage struct {
	UserID   string `json:"user_id" description:"ID of the user who left"`
	Username string `json:"username" description:"Username of the user who left"`
	RoomID   string `json:"room_id,omitempty" description:"Chat room ID"`
}

type TypingMessage struct {
	UserID   string `json:"user_id" description:"ID of the user who is typing"`
	Username string `json:"username" description:"Username of the user who is typing"`
	RoomID   string `json:"room_id,omitempty" description:"Chat room ID"`
}

// Notification-related event types
type NotificationEvent struct {
	ID       string                 `json:"id" description:"Notification ID"`
	Type     string                 `json:"type" description:"Notification type"`
	Title    string                 `json:"title" description:"Notification title"`
	Message  string                 `json:"message" description:"Notification message"`
	UserID   string                 `json:"user_id" description:"Target user ID"`
	Data     map[string]interface{} `json:"data,omitempty" description:"Additional notification data"`
	Priority string                 `json:"priority" description:"Notification priority (low, normal, high, urgent)"`
}

type AlertEvent struct {
	ID       string                 `json:"id" description:"Alert ID"`
	Level    string                 `json:"level" description:"Alert level (info, warning, error, critical)"`
	Message  string                 `json:"message" description:"Alert message"`
	Source   string                 `json:"source" description:"Alert source system"`
	Data     map[string]interface{} `json:"data,omitempty" description:"Additional alert data"`
	Resolved bool                   `json:"resolved" description:"Whether the alert is resolved"`
}

type SystemEvent struct {
	Type      string                 `json:"type" description:"System event type"`
	Component string                 `json:"component" description:"System component"`
	Message   string                 `json:"message" description:"System event message"`
	Data      map[string]interface{} `json:"data,omitempty" description:"Additional system data"`
	Severity  string                 `json:"severity" description:"Event severity"`
}

// Data streaming event types
type DataUpdateEvent struct {
	Source    string      `json:"source" description:"Data source identifier"`
	Type      string      `json:"type" description:"Data type"`
	Data      interface{} `json:"data" description:"Updated data payload"`
	Timestamp time.Time   `json:"timestamp" description:"Update timestamp"`
	Version   int64       `json:"version" description:"Data version number"`
}

type HeartbeatEvent struct {
	Timestamp time.Time `json:"timestamp" description:"Heartbeat timestamp"`
	Status    string    `json:"status" description:"System status"`
	Uptime    int64     `json:"uptime" description:"System uptime in seconds"`
}

// Game-related message types
type PlayerMoveMessage struct {
	PlayerID  string  `json:"player_id" description:"Player identifier"`
	X         float64 `json:"x" description:"X coordinate"`
	Y         float64 `json:"y" description:"Y coordinate"`
	Z         float64 `json:"z,omitempty" description:"Z coordinate (for 3D games)"`
	Timestamp int64   `json:"timestamp" description:"Move timestamp"`
}

type GameStateMessage struct {
	GameID    string      `json:"game_id" description:"Game identifier"`
	State     string      `json:"state" description:"Game state (waiting, playing, paused, ended)"`
	Players   []Player    `json:"players" description:"List of players"`
	Score     interface{} `json:"score,omitempty" description:"Current score"`
	Timestamp int64       `json:"timestamp" description:"State timestamp"`
}

type PlayerActionMessage struct {
	PlayerID  string                 `json:"player_id" description:"Player identifier"`
	Action    string                 `json:"action" description:"Action type"`
	Data      map[string]interface{} `json:"data,omitempty" description:"Action data"`
	Timestamp int64                  `json:"timestamp" description:"Action timestamp"`
}

type GameEventMessage struct {
	EventID   string                 `json:"event_id" description:"Event identifier"`
	Type      string                 `json:"type" description:"Event type"`
	GameID    string                 `json:"game_id" description:"Game identifier"`
	Data      map[string]interface{} `json:"data,omitempty" description:"Event data"`
	Timestamp int64                  `json:"timestamp" description:"Event timestamp"`
}

type Player struct {
	ID       string `json:"id" description:"Player identifier"`
	Username string `json:"username" description:"Player username"`
	Score    int    `json:"score" description:"Player score"`
	Status   string `json:"status" description:"Player status (active, inactive, disconnected)"`
}

// Monitoring event types
type CPUUsageEvent struct {
	Usage     float64   `json:"usage" description:"CPU usage percentage"`
	Cores     int       `json:"cores" description:"Number of CPU cores"`
	LoadAvg   []float64 `json:"load_avg" description:"Load average (1, 5, 15 minute intervals)"`
	Timestamp time.Time `json:"timestamp" description:"Measurement timestamp"`
}

type MemoryUsageEvent struct {
	Total     int64     `json:"total" description:"Total memory in bytes"`
	Used      int64     `json:"used" description:"Used memory in bytes"`
	Free      int64     `json:"free" description:"Free memory in bytes"`
	Usage     float64   `json:"usage" description:"Memory usage percentage"`
	Timestamp time.Time `json:"timestamp" description:"Measurement timestamp"`
}

type DiskUsageEvent struct {
	Path      string    `json:"path" description:"Disk path"`
	Total     int64     `json:"total" description:"Total disk space in bytes"`
	Used      int64     `json:"used" description:"Used disk space in bytes"`
	Free      int64     `json:"free" description:"Free disk space in bytes"`
	Usage     float64   `json:"usage" description:"Disk usage percentage"`
	Timestamp time.Time `json:"timestamp" description:"Measurement timestamp"`
}

type NetworkIOEvent struct {
	Interface  string    `json:"interface" description:"Network interface name"`
	BytesIn    int64     `json:"bytes_in" description:"Bytes received"`
	BytesOut   int64     `json:"bytes_out" description:"Bytes sent"`
	PacketsIn  int64     `json:"packets_in" description:"Packets received"`
	PacketsOut int64     `json:"packets_out" description:"Packets sent"`
	Timestamp  time.Time `json:"timestamp" description:"Measurement timestamp"`
}

type SystemAlertEvent struct {
	ID        string                 `json:"id" description:"Alert identifier"`
	Level     string                 `json:"level" description:"Alert level (info, warning, error, critical)"`
	Component string                 `json:"component" description:"System component"`
	Message   string                 `json:"message" description:"Alert message"`
	Metric    string                 `json:"metric" description:"Related metric"`
	Value     float64                `json:"value" description:"Metric value that triggered the alert"`
	Threshold float64                `json:"threshold" description:"Alert threshold"`
	Data      map[string]interface{} `json:"data,omitempty" description:"Additional alert data"`
	Timestamp time.Time              `json:"timestamp" description:"Alert timestamp"`
}

// =============================================================================
// HELPER FUNCTIONS FOR CREATING STREAMING OPTIONS
// =============================================================================

// CreateWebSocketOptions creates streaming handler options for WebSocket with message types
func CreateWebSocketOptions(tags []string, summary, description string, requireAuth bool, messageTypes map[string]reflect.Type) []common.StreamingHandlerOption {
	options := []common.StreamingHandlerOption{
		WithStreamingTags(tags...),
		WithStreamingSummary(summary),
		WithStreamingDescription(description),
		WithStreamingAuth(requireAuth),
		WithStreamingProtocol("websocket"),
		WithStreamingHeartbeat(30 * time.Second),
	}

	if len(messageTypes) > 0 {
		options = append(options, WithWebSocketMessageTypes(messageTypes))
	}

	return options
}

// CreateSSEOptions creates streaming handler options for SSE with event types
func CreateSSEOptions(tags []string, summary, description string, requireAuth bool, eventTypes map[string]reflect.Type) []common.StreamingHandlerOption {
	options := []common.StreamingHandlerOption{
		WithStreamingTags(tags...),
		WithStreamingSummary(summary),
		WithStreamingDescription(description),
		WithStreamingAuth(requireAuth),
		WithStreamingProtocol("sse"),
	}

	if len(eventTypes) > 0 {
		options = append(options, WithSSEEventTypes(eventTypes))
	}

	return options
}

// SimpleWebSocketOptions creates basic WebSocket options with minimal configuration
func SimpleWebSocketOptions(tags ...string) []common.StreamingHandlerOption {
	return []common.StreamingHandlerOption{
		WithStreamingTags(tags...),
		WithStreamingProtocol("websocket"),
		WithStreamingAuth(false),
		WithStreamingHeartbeat(30 * time.Second),
	}
}

// SimpleSSEOptions creates basic SSE options with minimal configuration
func SimpleSSEOptions(tags ...string) []common.StreamingHandlerOption {
	return []common.StreamingHandlerOption{
		WithStreamingTags(tags...),
		WithStreamingProtocol("sse"),
		WithStreamingAuth(false),
	}
}
