package internal

import (
	"errors"
	"fmt"
)

// Common errors.
var (
	// Connection errors.
	ErrConnectionNotFound     = errors.New("connection not found")
	ErrConnectionClosed       = errors.New("connection closed")
	ErrConnectionLimitReached = errors.New("connection limit reached")
	ErrInvalidConnection      = errors.New("invalid connection")

	// Room errors.
	ErrRoomNotFound      = errors.New("room not found")
	ErrRoomAlreadyExists = errors.New("room already exists")
	ErrRoomFull          = errors.New("room is full")
	ErrNotRoomMember     = errors.New("not a member of the room")
	ErrAlreadyRoomMember = errors.New("already a member of the room")
	ErrInvalidRoom       = errors.New("invalid room")
	ErrRoomLimitReached  = errors.New("room limit reached")

	// Channel errors.
	ErrChannelNotFound      = errors.New("channel not found")
	ErrChannelAlreadyExists = errors.New("channel already exists")
	ErrNotSubscribed        = errors.New("not subscribed to channel")
	ErrAlreadySubscribed    = errors.New("already subscribed to channel")
	ErrInvalidChannel       = errors.New("invalid channel")
	ErrChannelLimitReached  = errors.New("channel limit reached")

	// Permission errors.
	ErrPermissionDenied  = errors.New("permission denied")
	ErrInvalidPermission = errors.New("invalid permission")
	ErrInsufficientRole  = errors.New("insufficient role")

	// Authorization errors. Distinct from ErrPermissionDenied so a caller can
	// tell "you may not enter this room" from "you may not perform this action",
	// and so a client can render the two differently.
	ErrRoomAccessDenied    = errors.New("room access denied")
	ErrChannelAccessDenied = errors.New("channel access denied")
	ErrSendDenied          = errors.New("not permitted to send to this target")
	ErrUserMuted           = errors.New("user is muted in this room")
	ErrUserBanned          = errors.New("user is banned from this room")
	ErrSessionNotOwned     = errors.New("session belongs to a different user")

	// Feature-flag errors. Returned when an operation targets a subsystem the
	// configuration has switched off. Typed rather than ad-hoc strings so a
	// caller can branch on "disabled" instead of matching error text.
	ErrRoomsDisabled    = errors.New("rooms are disabled")
	ErrChannelsDisabled = errors.New("channels are disabled")
	ErrHistoryDisabled  = errors.New("message history is disabled")
	ErrPresenceDisabled = errors.New("presence tracking is disabled")
	ErrTypingDisabled   = errors.New("typing indicators are disabled")

	// Rate limiting.
	ErrRateLimitExceeded = errors.New("rate limit exceeded")

	// Message errors.
	ErrMessageTooLarge = errors.New("message too large")
	ErrInvalidMessage  = errors.New("invalid message")
	ErrMessageNotFound = errors.New("message not found")

	// Presence errors.
	ErrPresenceNotFound = errors.New("presence not found")
	ErrInvalidStatus    = errors.New("invalid status")

	// Invite errors.
	ErrInviteNotFound = errors.New("invite not found")
	ErrInviteExpired  = errors.New("invite expired")

	// Backend errors.
	ErrBackendNotConnected = errors.New("backend not connected")
	ErrBackendTimeout      = errors.New("backend operation timeout")
	ErrBackendUnavailable  = errors.New("backend unavailable")

	// Configuration errors.
	ErrInvalidConfig = errors.New("invalid configuration")
	ErrMissingConfig = errors.New("missing required configuration")

	// Distributed errors.
	ErrNodeNotFound          = errors.New("node not found")
	ErrLockAcquisitionFailed = errors.New("failed to acquire lock")
	ErrLockNotHeld           = errors.New("lock not held")
)

// ConnectionError wraps connection-related errors with context.
type ConnectionError struct {
	ConnID string
	Op     string
	Err    error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("connection %s: %s: %v", e.ConnID, e.Op, e.Err)
}

func (e *ConnectionError) Unwrap() error {
	return e.Err
}

// RoomError wraps room-related errors with context.
type RoomError struct {
	RoomID string
	UserID string
	Op     string
	Err    error
}

func (e *RoomError) Error() string {
	if e.UserID != "" {
		return fmt.Sprintf("room %s (user %s): %s: %v", e.RoomID, e.UserID, e.Op, e.Err)
	}

	return fmt.Sprintf("room %s: %s: %v", e.RoomID, e.Op, e.Err)
}

func (e *RoomError) Unwrap() error {
	return e.Err
}

// ChannelError wraps channel-related errors with context.
type ChannelError struct {
	ChannelID string
	Op        string
	Err       error
}

func (e *ChannelError) Error() string {
	return fmt.Sprintf("channel %s: %s: %v", e.ChannelID, e.Op, e.Err)
}

func (e *ChannelError) Unwrap() error {
	return e.Err
}

// MessageError wraps message-related errors with context.
type MessageError struct {
	MessageID string
	Op        string
	Err       error
}

func (e *MessageError) Error() string {
	return fmt.Sprintf("message %s: %s: %v", e.MessageID, e.Op, e.Err)
}

func (e *MessageError) Unwrap() error {
	return e.Err
}

// BackendError wraps backend-related errors with context.
type BackendError struct {
	Backend string
	Op      string
	Err     error
}

func (e *BackendError) Error() string {
	return fmt.Sprintf("backend %s: %s: %v", e.Backend, e.Op, e.Err)
}

func (e *BackendError) Unwrap() error {
	return e.Err
}

// Helper functions for creating errors

func NewConnectionError(connID, op string, err error) error {
	return &ConnectionError{ConnID: connID, Op: op, Err: err}
}

func NewRoomError(roomID, userID, op string, err error) error {
	return &RoomError{RoomID: roomID, UserID: userID, Op: op, Err: err}
}

func NewChannelError(channelID, op string, err error) error {
	return &ChannelError{ChannelID: channelID, Op: op, Err: err}
}

func NewMessageError(messageID, op string, err error) error {
	return &MessageError{MessageID: messageID, Op: op, Err: err}
}

func NewBackendError(backend, op string, err error) error {
	return &BackendError{Backend: backend, Op: op, Err: err}
}
