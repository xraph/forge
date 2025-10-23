package filters

import (
	"context"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// PermissionChecker checks if recipient has permission to receive message.
type PermissionChecker interface {
	// HasPermission checks if recipient can receive message
	HasPermission(ctx context.Context, recipient streaming.EnhancedConnection, msg *streaming.Message) (bool, error)
}

// permissionFilter filters based on permissions.
type permissionFilter struct {
	checker PermissionChecker
}

// NewPermissionFilter creates a permission filter.
func NewPermissionFilter(checker PermissionChecker) MessageFilter {
	return &permissionFilter{
		checker: checker,
	}
}

func (pf *permissionFilter) Name() string {
	return "permission_filter"
}

func (pf *permissionFilter) Priority() int {
	return 5 // Very early (before content)
}

func (pf *permissionFilter) Filter(ctx context.Context, msg *streaming.Message, recipient streaming.EnhancedConnection) (*streaming.Message, error) {
	// Check if recipient has permission
	hasPermission, err := pf.checker.HasPermission(ctx, recipient, msg)
	if err != nil {
		return nil, err
	}

	if !hasPermission {
		return nil, nil // Block message
	}

	return msg, nil
}

// defaultPermissionChecker is a basic permission checker.
type defaultPermissionChecker struct {
	roomStore streaming.RoomStore
}

// NewDefaultPermissionChecker creates a basic permission checker.
func NewDefaultPermissionChecker(roomStore streaming.RoomStore) PermissionChecker {
	return &defaultPermissionChecker{
		roomStore: roomStore,
	}
}

func (dpc *defaultPermissionChecker) HasPermission(ctx context.Context, recipient streaming.EnhancedConnection, msg *streaming.Message) (bool, error) {
	// Check room membership
	if msg.RoomID != "" {
		userID := recipient.GetUserID()
		if userID == "" {
			return false, nil // Anonymous users can't receive room messages
		}

		isMember, err := dpc.roomStore.IsMember(ctx, msg.RoomID, userID)
		if err != nil {
			return false, err
		}

		if !isMember {
			return false, nil // Not a member
		}

		// Check if user is muted (via metadata)
		member, err := dpc.roomStore.GetMember(ctx, msg.RoomID, userID)
		if err != nil {
			return false, err
		}

		// Check if user has muted status in metadata
		if muted, ok := member.GetMetadata()["muted"].(bool); ok && muted {
			return false, nil // User is muted
		}

		// Check if sender is blocked
		// This would require a blocked users list
		// For now, allow the message
	}

	return true, nil
}
