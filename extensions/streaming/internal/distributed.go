package internal

import (
	"context"
	"time"
)

// DistributedBackend provides cross-node coordination for distributed streaming.
type DistributedBackend interface {
	// Node coordination
	RegisterNode(ctx context.Context, nodeID string, metadata map[string]any) error
	UnregisterNode(ctx context.Context, nodeID string) error
	GetNodes(ctx context.Context) ([]NodeInfo, error)
	GetNode(ctx context.Context, nodeID string) (*NodeInfo, error)
	Heartbeat(ctx context.Context, nodeID string) error

	// Message broadcasting across nodes
	Publish(ctx context.Context, channel string, message *Message) error
	Subscribe(ctx context.Context, channel string, handler MessageHandler) error
	Unsubscribe(ctx context.Context, channel string) error

	// Distributed presence
	SetPresence(ctx context.Context, userID, status string, ttl time.Duration) error
	GetPresence(ctx context.Context, userID string) (*UserPresence, error)
	GetOnlineUsers(ctx context.Context) ([]string, error)

	// Distributed locks (for exclusive operations)
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (Lock, error)
	ReleaseLock(ctx context.Context, lock Lock) error

	// Distributed counters
	Increment(ctx context.Context, key string) (int64, error)
	Decrement(ctx context.Context, key string) (int64, error)
	GetCounter(ctx context.Context, key string) (int64, error)

	// Node discovery
	DiscoverNodes(ctx context.Context) ([]NodeInfo, error)
	WatchNodes(ctx context.Context, handler NodeChangeHandler) error

	// Health check
	Ping(ctx context.Context) error

	// Lifecycle
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
}

// Lock represents a distributed lock.
type Lock interface {
	GetKey() string
	GetOwner() string
	GetExpiry() time.Time
	Renew(ctx context.Context, ttl time.Duration) error
	IsHeld() bool
}

// NodeInfo contains information about a node in the cluster.
type NodeInfo struct {
	ID          string         `json:"id"`
	Address     string         `json:"address"`
	Started     time.Time      `json:"started"`
	LastSeen    time.Time      `json:"last_seen"`
	Connections int            `json:"connections"`
	Rooms       int            `json:"rooms"`
	Channels    int            `json:"channels"`
	Version     string         `json:"version"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// NodeChangeHandler is called when nodes join or leave the cluster.
type NodeChangeHandler func(event NodeChangeEvent)

// NodeChangeEvent represents a node joining or leaving.
type NodeChangeEvent struct {
	Type      string    `json:"type"` // "joined", "left", "updated"
	Node      NodeInfo  `json:"node"`
	Timestamp time.Time `json:"timestamp"`
}

// Node change event types
const (
	NodeEventJoined  = "joined"
	NodeEventLeft    = "left"
	NodeEventUpdated = "updated"
)

// MessageHandler processes messages from distributed channels.
type MessageHandler func(ctx context.Context, message *Message) error

// DistributedBackendOptions contains configuration for distributed backend.
type DistributedBackendOptions struct {
	// Backend type: "redis", "nats", ""
	Type string

	// Connection URLs
	URLs []string

	// Node ID (unique identifier for this instance)
	NodeID string

	// Heartbeat interval
	HeartbeatInterval time.Duration

	// Node timeout (consider dead after this)
	NodeTimeout time.Duration

	// Reconnect settings
	MaxReconnectAttempts int
	ReconnectBackoff     time.Duration

	// TLS configuration
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string

	// Authentication
	Username string
	Password string

	// Additional options
	Options map[string]any
}

// DefaultDistributedBackendOptions returns default distributed backend options.
func DefaultDistributedBackendOptions() DistributedBackendOptions {
	return DistributedBackendOptions{
		Type:                 "redis",
		URLs:                 []string{"redis://localhost:6379"},
		NodeID:               "", // Auto-generated if empty
		HeartbeatInterval:    10 * time.Second,
		NodeTimeout:          30 * time.Second,
		MaxReconnectAttempts: 10,
		ReconnectBackoff:     5 * time.Second,
		TLSEnabled:           false,
		Options:              make(map[string]any),
	}
}

// ClusterStats provides statistics about the streaming cluster.
type ClusterStats struct {
	TotalNodes        int            `json:"total_nodes"`
	ActiveNodes       int            `json:"active_nodes"`
	TotalConnections  int            `json:"total_connections"`
	TotalRooms        int            `json:"total_rooms"`
	TotalChannels     int            `json:"total_channels"`
	MessagesPerSecond float64        `json:"messages_per_second"`
	Nodes             []NodeInfo     `json:"nodes"`
	Uptime            time.Duration  `json:"uptime"`
	Extra             map[string]any `json:"extra,omitempty"`
}
