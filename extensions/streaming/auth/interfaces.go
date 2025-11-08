package auth

import (
	"context"
	"net/http"

	"github.com/xraph/forge/extensions/auth"
)

// ConnectionAuthenticator authenticates WebSocket/SSE connections.
type ConnectionAuthenticator interface {
	// AuthenticateConnection verifies the connection on WebSocket/SSE upgrade
	AuthenticateConnection(ctx context.Context, r *http.Request) (*auth.AuthContext, error)

	// RequireAuth enforces auth providers (OR logic)
	RequireAuth(providers ...string) error

	// RequireScopes enforces scopes (AND logic)
	RequireScopes(scopes ...string) error
}

// RoomAuthorizer checks room-level permissions.
type RoomAuthorizer interface {
	// CanJoin checks if user can join room
	CanJoin(ctx context.Context, userID, roomID string) (bool, error)

	// CanLeave checks if user can leave room
	CanLeave(ctx context.Context, userID, roomID string) (bool, error)

	// CanInvite checks if user can invite others
	CanInvite(ctx context.Context, userID, roomID string) (bool, error)

	// CanModerate checks moderation permissions
	CanModerate(ctx context.Context, userID, roomID string, action string) (bool, error)

	// GetUserRole returns user's role in room
	GetUserRole(ctx context.Context, userID, roomID string) (string, error)
}

// MessageAuthorizer checks message-level permissions.
type MessageAuthorizer interface {
	// CanSend checks if user can send message to room/channel
	CanSend(ctx context.Context, userID, targetID string, targetType TargetType) (bool, error)

	// CanDelete checks if user can delete message
	CanDelete(ctx context.Context, userID, messageID string) (bool, error)

	// CanEdit checks if user can edit message
	CanEdit(ctx context.Context, userID, messageID string) (bool, error)

	// CanReact checks if user can react to message
	CanReact(ctx context.Context, userID, messageID string) (bool, error)
}

// TargetType defines message target types.
type TargetType string

const (
	TargetTypeRoom    TargetType = "room"
	TargetTypeChannel TargetType = "channel"
	TargetTypeDirect  TargetType = "direct"
)

// Moderation actions.
const (
	ActionDelete = "delete"
	ActionMute   = "mute"
	ActionBan    = "ban"
	ActionKick   = "kick"
	ActionManage = "manage"
)

// MessageStore provides message retrieval for authorization.
type MessageStore interface {
	// Get retrieves a message by ID
	Get(ctx context.Context, messageID string) (*MessageInfo, error)
}

// MessageInfo contains basic message information for authorization.
type MessageInfo struct {
	ID        string
	UserID    string
	RoomID    string
	ChannelID string
	Content   any
	Metadata  map[string]any
}
