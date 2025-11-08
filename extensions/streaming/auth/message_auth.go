package auth

import (
	"context"
	"fmt"
)

// messageAuthorizer implements MessageAuthorizer.
type messageAuthorizer struct {
	roomAuth     RoomAuthorizer
	messageStore MessageStore
}

// NewMessageAuthorizer creates a new message authorizer.
func NewMessageAuthorizer(roomAuth RoomAuthorizer, messageStore MessageStore) MessageAuthorizer {
	return &messageAuthorizer{
		roomAuth:     roomAuth,
		messageStore: messageStore,
	}
}

// CanSend checks if user can send message to room/channel.
func (ma *messageAuthorizer) CanSend(ctx context.Context, userID, targetID string, targetType TargetType) (bool, error) {
	switch targetType {
	case TargetTypeRoom:
		// Must be a member of the room
		return ma.roomAuth.CanJoin(ctx, userID, targetID)
	case TargetTypeChannel:
		// For now, assume channels are open
		return true, nil
	case TargetTypeDirect:
		// Direct messages allowed to anyone
		return true, nil
	default:
		return false, fmt.Errorf("unknown target type: %s", targetType)
	}
}

// CanDelete checks if user can delete message.
func (ma *messageAuthorizer) CanDelete(ctx context.Context, userID, messageID string) (bool, error) {
	// Get message
	msg, err := ma.messageStore.Get(ctx, messageID)
	if err != nil {
		return false, err
	}

	// User can delete their own messages
	if msg.UserID == userID {
		return true, nil
	}

	// Check if user is moderator/admin in room
	if msg.RoomID != "" {
		canModerate, err := ma.roomAuth.CanModerate(ctx, userID, msg.RoomID, ActionDelete)
		if err != nil {
			return false, err
		}

		return canModerate, nil
	}

	return false, nil
}

// CanEdit checks if user can edit message.
func (ma *messageAuthorizer) CanEdit(ctx context.Context, userID, messageID string) (bool, error) {
	// Get message
	msg, err := ma.messageStore.Get(ctx, messageID)
	if err != nil {
		return false, err
	}

	// Only message author can edit
	return msg.UserID == userID, nil
}

// CanReact checks if user can react to message.
func (ma *messageAuthorizer) CanReact(ctx context.Context, userID, messageID string) (bool, error) {
	// Get message
	msg, err := ma.messageStore.Get(ctx, messageID)
	if err != nil {
		return false, err
	}

	// Must be a member of the room
	if msg.RoomID != "" {
		return ma.roomAuth.CanJoin(ctx, userID, msg.RoomID)
	}

	// For non-room messages, allow reactions
	return true, nil
}
