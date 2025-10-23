package common

import (
	"context"
	"reflect"
	"time"
)

// =============================================================================
// STREAMING MESSAGE TYPES
// =============================================================================

// StreamingMessage represents a generic streaming message
type StreamingMessage struct {
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data"`
	ID        string                 `json:"id,omitempty"`
	Timestamp time.Time              `json:"timestamp,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// StreamingEvent represents a Server-Sent Event
type StreamingEvent struct {
	Type  string      `json:"type"`
	Data  interface{} `json:"data"`
	ID    string      `json:"id,omitempty"`
	Retry int         `json:"retry,omitempty"`
}

// =============================================================================
// STREAMING CONNECTION INTERFACE
// =============================================================================

// StreamingConnection represents a generic streaming connection
type StreamingConnection interface {
	// Connection management
	ID() string
	UserID() string
	IsAlive() bool
	Close() error

	// Message sending
	Send(message interface{}) error
	SendMessage(msg StreamingMessage) error
	SendEvent(event StreamingEvent) error

	// Connection metadata
	Protocol() string
	RemoteAddr() string
	ConnectedAt() time.Time
	LastActivity() time.Time

	// Context access
	Context() context.Context
	WithContext(ctx context.Context) StreamingConnection
}

// =============================================================================
// STREAMING STATISTICS
// =============================================================================

// StreamingStats represents statistics for streaming operations
type StreamingStats struct {
	TotalConnections  int64            `json:"total_connections"`
	ActiveConnections int64            `json:"active_connections"`
	MessagesSent      int64            `json:"messages_sent"`
	MessagesReceived  int64            `json:"messages_received"`
	ErrorCount        int64            `json:"error_count"`
	AverageLatency    time.Duration    `json:"average_latency"`
	ConnectionsByType map[string]int64 `json:"connections_by_type"`
	LastActivity      time.Time        `json:"last_activity"`
}

// =============================================================================
// STREAMING HANDLER INFO
// =============================================================================

// StreamingHandlerOption defines options specific to streaming handlers
type StreamingHandlerOption func(*StreamingHandlerInfo)

// StreamingHandlerInfo contains information about streaming handlers
type StreamingHandlerInfo struct {
	Path              string
	Protocol          string // "websocket", "sse", "longpolling"
	Handler           interface{}
	MessageTypes      map[string]reflect.Type
	EventTypes        map[string]reflect.Type
	Summary           string
	Description       string
	Tags              []string
	Authentication    bool
	ConnectionLimit   int
	HeartbeatInterval time.Duration
	RegisteredAt      time.Time
	ConnectionCount   int64
	MessageCount      int64
	ErrorCount        int64
	LastConnected     time.Time
	LastError         error
}

// WithProtocol sets the protocol for the streaming handler
func WithProtocol(protocol string) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.Protocol = protocol
	}
}

// WithStreamingSummary sets the summary for the streaming handler
func WithStreamingSummary(summary string) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.Summary = summary
	}
}

// WithStreamingDescription sets the description for the streaming handler
func WithStreamingDescription(description string) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.Description = description
	}
}

// WithStreamingAuth sets the authentication requirement for the streaming handler
func WithStreamingAuth(auth bool) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.Authentication = auth
	}
}

// WithStreamingConnectionLimit sets the connection limit for the streaming handler
func WithStreamingConnectionLimit(limit int) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.ConnectionLimit = limit
	}
}

// WithStreamingHeartbeat sets the heartbeat interval for the streaming handler
func WithStreamingHeartbeat(interval time.Duration) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.HeartbeatInterval = interval
	}
}
