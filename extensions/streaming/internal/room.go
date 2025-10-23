package internal

import (
	"context"
	"time"
)

// Room represents a chat room or group for organizing conversations.
type Room interface {
	// Identity
	GetID() string
	GetName() string
	GetDescription() string
	GetOwner() string
	GetCreated() time.Time
	GetUpdated() time.Time
	GetMetadata() map[string]any

	// Membership
	Join(ctx context.Context, userID, role string) error
	Leave(ctx context.Context, userID string) error
	IsMember(ctx context.Context, userID string) (bool, error)
	GetMembers(ctx context.Context) ([]Member, error)
	GetMember(ctx context.Context, userID string) (Member, error)
	MemberCount(ctx context.Context) (int, error)

	// Advanced Membership Management
	GetMembersByRole(ctx context.Context, role string) ([]Member, error)
	UpdateMemberRole(ctx context.Context, userID, newRole string) error
	TransferOwnership(ctx context.Context, newOwnerID string) error
	BanMember(ctx context.Context, userID string, reason string, until *time.Time) error
	UnbanMember(ctx context.Context, userID string) error
	IsBanned(ctx context.Context, userID string) (bool, error)
	GetBannedMembers(ctx context.Context) ([]RoomBan, error)
	MuteMember(ctx context.Context, userID string, duration time.Duration) error
	UnmuteMember(ctx context.Context, userID string) error
	IsMuted(ctx context.Context, userID string) (bool, error)

	// Invitations
	CreateInvite(ctx context.Context, opts InviteOptions) (*Invite, error)
	RevokeInvite(ctx context.Context, inviteCode string) error
	ValidateInvite(ctx context.Context, inviteCode string) (bool, error)
	GetInvites(ctx context.Context) ([]*Invite, error)
	JoinWithInvite(ctx context.Context, userID, inviteCode string) error

	// Permissions
	HasPermission(ctx context.Context, userID, permission string) (bool, error)
	GrantPermission(ctx context.Context, userID, permission string) error
	RevokePermission(ctx context.Context, userID, permission string) error

	// Room Settings
	IsPrivate() bool
	SetPrivate(ctx context.Context, private bool) error
	GetMaxMembers() int
	SetMaxMembers(ctx context.Context, max int) error
	IsArchived() bool
	Archive(ctx context.Context) error
	Unarchive(ctx context.Context) error
	IsLocked() bool
	Lock(ctx context.Context, reason string) error
	Unlock(ctx context.Context) error

	// Moderation
	GetModerationLog(ctx context.Context, limit int) ([]ModerationEvent, error)
	SetSlowMode(ctx context.Context, intervalSeconds int) error
	GetSlowMode(ctx context.Context) int

	// Pinned Messages
	PinMessage(ctx context.Context, messageID string) error
	UnpinMessage(ctx context.Context, messageID string) error
	GetPinnedMessages(ctx context.Context) ([]string, error)

	// Categories/Tags
	AddTag(ctx context.Context, tag string) error
	RemoveTag(ctx context.Context, tag string) error
	GetTags() []string
	SetCategory(ctx context.Context, category string) error
	GetCategory() string

	// Statistics
	GetMessageCount(ctx context.Context) (int64, error)
	GetActiveMembers(ctx context.Context, since time.Duration) ([]Member, error)

	// Messaging
	Broadcast(ctx context.Context, message *Message) error
	BroadcastExcept(ctx context.Context, message *Message, excludeUserIDs []string) error
	BroadcastToRole(ctx context.Context, message *Message, role string) error

	// Read Receipts
	MarkAsRead(ctx context.Context, userID, messageID string) error
	GetUnreadCount(ctx context.Context, userID string, since time.Time) (int, error)
	GetLastReadMessage(ctx context.Context, userID string) (string, error)

	// Lifecycle
	Update(ctx context.Context, updates map[string]any) error
	Delete(ctx context.Context) error
}

// RoomStore provides backend storage for rooms.
type RoomStore interface {
	// CRUD operations
	Create(ctx context.Context, room Room) error
	Get(ctx context.Context, roomID string) (Room, error)
	Update(ctx context.Context, roomID string, updates map[string]any) error
	Delete(ctx context.Context, roomID string) error
	List(ctx context.Context, filters map[string]any) ([]Room, error)
	Exists(ctx context.Context, roomID string) (bool, error)

	// Bulk operations
	CreateMany(ctx context.Context, rooms []Room) error
	DeleteMany(ctx context.Context, roomIDs []string) error

	// Membership operations
	AddMember(ctx context.Context, roomID string, member Member) error
	RemoveMember(ctx context.Context, roomID, userID string) error
	GetMembers(ctx context.Context, roomID string) ([]Member, error)
	GetMember(ctx context.Context, roomID, userID string) (Member, error)
	IsMember(ctx context.Context, roomID, userID string) (bool, error)
	MemberCount(ctx context.Context, roomID string) (int, error)

	// User's rooms
	GetUserRooms(ctx context.Context, userID string) ([]Room, error)
	GetUserRoomsByRole(ctx context.Context, userID, role string) ([]Room, error)
	GetCommonRooms(ctx context.Context, userID1, userID2 string) ([]Room, error)

	// Advanced queries
	Search(ctx context.Context, query string, filters map[string]any) ([]Room, error)
	FindByTag(ctx context.Context, tag string) ([]Room, error)
	FindByCategory(ctx context.Context, category string) ([]Room, error)
	GetPublicRooms(ctx context.Context, limit int) ([]Room, error)
	GetArchivedRooms(ctx context.Context, userID string) ([]Room, error)

	// Statistics
	GetRoomCount(ctx context.Context) (int, error)
	GetTotalMembers(ctx context.Context) (int, error)

	// Bans and invites
	BanMember(ctx context.Context, roomID, userID string, ban RoomBan) error
	UnbanMember(ctx context.Context, roomID, userID string) error
	GetBans(ctx context.Context, roomID string) ([]RoomBan, error)
	IsBanned(ctx context.Context, roomID, userID string) (bool, error)
	SaveInvite(ctx context.Context, roomID string, invite *Invite) error
	GetInvite(ctx context.Context, inviteCode string) (*Invite, error)
	DeleteInvite(ctx context.Context, inviteCode string) error
	ListInvites(ctx context.Context, roomID string) ([]*Invite, error)

	// Lifecycle
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
}

// Member represents a room member with role and permissions.
type Member interface {
	GetUserID() string
	GetRole() string
	GetJoinedAt() time.Time
	GetPermissions() []string

	SetRole(role string)
	HasPermission(permission string) bool
	GrantPermission(permission string)
	RevokePermission(permission string)
	GetMetadata() map[string]any
	SetMetadata(key string, value any)
}

// Common room roles
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleGuest  = "guest"
)

// Common permissions
const (
	PermissionSendMessage   = "send_message"
	PermissionDeleteMessage = "delete_message"
	PermissionInviteMembers = "invite_members"
	PermissionRemoveMembers = "remove_members"
	PermissionManageRoom    = "manage_room"
	PermissionManageRoles   = "manage_roles"
)

// RoomOptions contains configuration for creating a room.
type RoomOptions struct {
	ID          string         `json:"id,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Owner       string         `json:"owner"`
	Private     bool           `json:"private"`
	MaxMembers  int            `json:"max_members,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// MemberOptions contains configuration for adding a member.
type MemberOptions struct {
	UserID      string         `json:"user_id"`
	Role        string         `json:"role"`
	Permissions []string       `json:"permissions,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// RoomEvent represents an event that occurs in a room.
type RoomEvent struct {
	Type      string         `json:"type"` // "created", "updated", "deleted", "member_joined", "member_left"
	RoomID    string         `json:"room_id"`
	UserID    string         `json:"user_id,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// Room event types
const (
	RoomEventCreated      = "created"
	RoomEventUpdated      = "updated"
	RoomEventDeleted      = "deleted"
	RoomEventMemberJoined = "member_joined"
	RoomEventMemberLeft   = "member_left"
	RoomEventMemberKicked = "member_kicked"
	RoomEventMemberBanned = "member_banned"
)

// Invite represents a room invitation.
type Invite struct {
	Code      string         `json:"code"`
	RoomID    string         `json:"room_id"`
	CreatedBy string         `json:"created_by"`
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt *time.Time     `json:"expires_at,omitempty"`
	MaxUses   int            `json:"max_uses"`
	UsedCount int            `json:"used_count"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// InviteOptions contains configuration for creating a room invitation.
type InviteOptions struct {
	Duration time.Duration  `json:"duration,omitempty"`
	MaxUses  int            `json:"max_uses,omitempty"`
	Role     string         `json:"role,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// RoomBan represents a banned user in a room.
type RoomBan struct {
	UserID    string         `json:"user_id"`
	RoomID    string         `json:"room_id"`
	Reason    string         `json:"reason"`
	BannedBy  string         `json:"banned_by"`
	BannedAt  time.Time      `json:"banned_at"`
	ExpiresAt *time.Time     `json:"expires_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ModerationEvent represents a moderation action in a room.
type ModerationEvent struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"` // "ban", "unban", "mute", "kick", "warn", "slow_mode"
	RoomID      string         `json:"room_id"`
	TargetID    string         `json:"target_id,omitempty"`
	ModeratorID string         `json:"moderator_id"`
	Reason      string         `json:"reason,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Moderation event types
const (
	ModerationEventBan      = "ban"
	ModerationEventUnban    = "unban"
	ModerationEventMute     = "mute"
	ModerationEventUnmute   = "unmute"
	ModerationEventKick     = "kick"
	ModerationEventWarn     = "warn"
	ModerationEventSlowMode = "slow_mode"
	ModerationEventLock     = "lock"
	ModerationEventUnlock   = "unlock"
)
