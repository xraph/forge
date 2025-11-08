package auth

import (
	"context"
	"fmt"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// roomAuthorizer implements RoomAuthorizer with role-based access control.
type roomAuthorizer struct {
	roomStore streaming.RoomStore
}

// NewRoomAuthorizer creates a new room authorizer.
func NewRoomAuthorizer(roomStore streaming.RoomStore) RoomAuthorizer {
	return &roomAuthorizer{
		roomStore: roomStore,
	}
}

// CanJoin checks if user can join room.
func (ra *roomAuthorizer) CanJoin(ctx context.Context, userID, roomID string) (bool, error) {
	// Check if already a member
	isMember, err := ra.roomStore.IsMember(ctx, roomID, userID)
	if err != nil {
		return false, err
	}

	if isMember {
		return true, nil // Already a member
	}

	// Check if room exists
	room, err := ra.roomStore.Get(ctx, roomID)
	if err != nil {
		return false, err
	}

	// Check if room is public
	if !room.IsPrivate() {
		return true, nil // Public rooms allow anyone
	}

	// Private rooms require invitation
	return false, nil
}

// CanLeave checks if user can leave room.
func (ra *roomAuthorizer) CanLeave(ctx context.Context, userID, roomID string) (bool, error) {
	// Check if member
	isMember, err := ra.roomStore.IsMember(ctx, roomID, userID)
	if err != nil {
		return false, err
	}

	if !isMember {
		return false, errors.New("not a member of room")
	}

	// Check if owner
	room, err := ra.roomStore.Get(ctx, roomID)
	if err != nil {
		return false, err
	}

	// Owners cannot leave their own rooms (must transfer ownership first)
	if room.GetOwner() == userID {
		return false, errors.New("owner cannot leave room without transferring ownership")
	}

	return true, nil
}

// CanInvite checks if user can invite others.
func (ra *roomAuthorizer) CanInvite(ctx context.Context, userID, roomID string) (bool, error) {
	role, err := ra.GetUserRole(ctx, userID, roomID)
	if err != nil {
		return false, err
	}

	// Owner, admin, and moderators can invite
	switch role {
	case streaming.RoleOwner, streaming.RoleAdmin:
		return true, nil
	case streaming.RoleMember:
		// Check if member has invite permission
		member, err := ra.roomStore.GetMember(ctx, roomID, userID)
		if err != nil {
			return false, err
		}

		return member.HasPermission(streaming.PermissionInviteMembers), nil
	default:
		return false, nil
	}
}

// CanModerate checks moderation permissions.
func (ra *roomAuthorizer) CanModerate(ctx context.Context, userID, roomID string, action string) (bool, error) {
	role, err := ra.GetUserRole(ctx, userID, roomID)
	if err != nil {
		return false, err
	}

	// Only owner and admins can moderate
	switch role {
	case streaming.RoleOwner, streaming.RoleAdmin:
		return true, nil
	default:
		// Check if user has manage_room permission
		member, err := ra.roomStore.GetMember(ctx, roomID, userID)
		if err != nil {
			return false, err
		}

		return member.HasPermission(streaming.PermissionManageRoom), nil
	}
}

// GetUserRole returns user's role in room.
func (ra *roomAuthorizer) GetUserRole(ctx context.Context, userID, roomID string) (string, error) {
	// Check if owner
	room, err := ra.roomStore.Get(ctx, roomID)
	if err != nil {
		return "", err
	}

	if room.GetOwner() == userID {
		return streaming.RoleOwner, nil
	}

	// Get member info
	member, err := ra.roomStore.GetMember(ctx, roomID, userID)
	if err != nil {
		return "", err
	}

	return member.GetRole(), nil
}
