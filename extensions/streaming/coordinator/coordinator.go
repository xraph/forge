package coordinator

import (
	"context"
	"encoding/json"
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

	// MessageTypeRoomBan tells every node a user was banned from a room, so
	// each can take that user's local connections out of it. UserID and
	// RoomID name the ban; the payload is a *RoomBanEvent.
	MessageTypeRoomBan = "room.member.ban"
)

// RoomBanEvent is the payload of a MessageTypeRoomBan message.
type RoomBanEvent struct {
	Reason    string     `json:"reason,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// EventPublisher is implemented by coordinators that can send a control event
// to every other node. It is separate from StreamCoordinator so coordinators
// written against that interface keep compiling; the manager checks for it
// and does without when it is missing.
type EventPublisher interface {
	// PublishEvent sends msg to every node. NodeID and Timestamp are filled in
	// when empty, so the sending node skips its own event.
	PublishEvent(ctx context.Context, msg *CoordinatorMessage) error
}

// rawCoordinatorMessage is CoordinatorMessage with the payload left encoded.
// The outer Payload field shadows the embedded one for encoding/json.
type rawCoordinatorMessage struct {
	CoordinatorMessage

	Payload json.RawMessage `json:"payload"`
}

// DecodeMessage decodes a coordinator message off the wire.
//
// A broadcast's payload is decoded straight into a *streaming.Message. Left to
// encoding/json it became a map[string]any, which the receiving node then
// marshalled back to JSON and unmarshalled again into a Message: two extra
// passes and a map of interface values per relayed message. Other message
// types keep the generic decode, since their payloads have no single shape.
func DecodeMessage(data []byte) (*CoordinatorMessage, error) {
	var raw rawCoordinatorMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	msg := raw.CoordinatorMessage
	msg.Payload = nil

	if len(raw.Payload) == 0 || string(raw.Payload) == "null" {
		return &msg, nil
	}

	switch msg.Type {
	case MessageTypeBroadcast:
		var sm streaming.Message
		if err := json.Unmarshal(raw.Payload, &sm); err != nil {
			return nil, err
		}

		msg.Payload = &sm

		return &msg, nil

	case MessageTypeRoomBan:
		var ev RoomBanEvent
		if err := json.Unmarshal(raw.Payload, &ev); err != nil {
			return nil, err
		}

		msg.Payload = &ev

		return &msg, nil
	}

	var payload any
	if err := json.Unmarshal(raw.Payload, &payload); err != nil {
		return nil, err
	}

	msg.Payload = payload

	return &msg, nil
}
