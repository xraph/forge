package coordinator

import (
	"context"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// StreamCoordinator coordinates streaming across multiple nodes.
type StreamCoordinator interface {
	// BroadcastToNode sends message to specific node
	BroadcastToNode(ctx context.Context, nodeID string, msg *streaming.Message) error

	// BroadcastToUser sends to user across all nodes
	BroadcastToUser(ctx context.Context, userID string, msg *streaming.Message) error

	// BroadcastToRoom sends to room across all nodes
	BroadcastToRoom(ctx context.Context, roomID string, msg *streaming.Message) error

	// BroadcastGlobal sends to all nodes
	BroadcastGlobal(ctx context.Context, msg *streaming.Message) error

	// SyncPresence synchronizes presence across nodes
	SyncPresence(ctx context.Context, presence *streaming.UserPresence) error

	// SyncRoomState synchronizes room state
	SyncRoomState(ctx context.Context, roomID string, state *RoomState) error

	// GetUserNodes returns nodes where user is connected
	GetUserNodes(ctx context.Context, userID string) ([]string, error)

	// GetRoomNodes returns nodes serving room
	GetRoomNodes(ctx context.Context, roomID string) ([]string, error)

	// RegisterNode registers this node
	RegisterNode(ctx context.Context, nodeID string, metadata map[string]any) error

	// UnregisterNode unregisters this node
	UnregisterNode(ctx context.Context, nodeID string) error

	// Subscribe subscribes to coordinator events
	Subscribe(ctx context.Context, handler MessageHandler) error

	// Start starts the coordinator
	Start(ctx context.Context) error

	// Stop stops the coordinator
	Stop(ctx context.Context) error
}

// RoomState represents room state for synchronization.
type RoomState struct {
	RoomID    string         `json:"roomId"`
	Members   []string       `json:"members"`
	Settings  map[string]any `json:"settings"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Version   int64          `json:"version"`
}

// MessageHandler handles coordinator messages.
type MessageHandler func(ctx context.Context, msg *CoordinatorMessage) error

// CoordinatorMessage represents a message in the coordination system.
type CoordinatorMessage struct {
	Type      string    `json:"type"`
	NodeID    string    `json:"nodeId"`
	UserID    string    `json:"userId"`
	RoomID    string    `json:"roomId"`
	ChannelID string    `json:"channelId"`
	Payload   any       `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

// MessageType defines coordinator message types.
const (
	MessageTypeBroadcast      = "broadcast"
	MessageTypePresenceUpdate = "presence.update"
	MessageTypeRoomStateSync  = "room.state.sync"
	MessageTypeMemberJoin     = "room.member.join"
	MessageTypeMemberLeave    = "room.member.leave"
	MessageTypeNodeRegister   = "node.register"
	MessageTypeNodeUnregister = "node.unregister"
)
