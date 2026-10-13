package streaming

import (
	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// Core interfaces.
type (
	Manager      = internal.Manager
	Room         = internal.Room
	Channel      = internal.Channel
	Member       = internal.Member
	UserPresence = internal.UserPresence
	Connection   = internal.EnhancedConnection
)

// Store interfaces.
type (
	RoomStore     = internal.RoomStore
	ChannelStore  = internal.ChannelStore
	MessageStore  = internal.MessageStore
	PresenceStore = internal.PresenceStore
	TypingStore   = internal.TypingStore
)

// Tracker interfaces.
type (
	PresenceTracker = internal.PresenceTracker
	TypingTracker   = internal.TypingTracker
)

// Distributed backend.
type (
	DistributedBackend = internal.DistributedBackend
	Lock               = internal.Lock
	NodeInfo           = internal.NodeInfo
	NodeChangeHandler  = internal.NodeChangeHandler
	MessageHandler     = internal.MessageHandler
	NodeChangeEvent    = internal.NodeChangeEvent
)

// Message types.
type (
	Message         = internal.Message
	MessageReaction = internal.MessageReaction
	MessageEdit     = internal.MessageEdit
)

// Room types.
type (
	Invite          = internal.Invite
	InviteOptions   = internal.InviteOptions
	RoomBan         = internal.RoomBan
	ModerationEvent = internal.ModerationEvent
	RoomEvent       = internal.RoomEvent
	RoomOptions     = internal.RoomOptions
	MemberOptions   = internal.MemberOptions
)

// ConnectionInfo provides detailed information about a connection.
type ConnectionInfo = internal.ConnectionInfo

// RoomStats provides statistics for a specific room.
type RoomStats = internal.RoomStats

// Room creation (local backend) and statistics types.
type (
	LocalRoom         = *local.LocalRoom
	UserStats         = internal.UserStats
	ManagerStats      = internal.ManagerStats
	OnlineStats       = internal.OnlineStats
	UserPresenceStats = internal.UserPresenceStats
)

// Presence types.
type (
	PresenceEvent   = internal.PresenceEvent
	PresenceOptions = internal.PresenceOptions
	PresenceFilters = internal.PresenceFilters
	DeviceInfo      = internal.DeviceInfo
	ActivityInfo    = internal.ActivityInfo
	Availability    = internal.Availability
)

// RateLimitStatus represents rate limiting status for a user.
type RateLimitStatus = internal.RateLimitStatus

// Query types.
type (
	HistoryQuery       = internal.HistoryQuery
	MessageSearchQuery = internal.MessageSearchQuery
	AnalyticsQuery     = internal.AnalyticsQuery
	FileQuery          = internal.FileQuery
)

// Analytics types.
type (
	AnalyticsResult = internal.AnalyticsResult
	AnalyticsEvent  = internal.AnalyticsEvent
)

// File types.
type (
	FileUpload = internal.FileUpload
	FileInfo   = internal.FileInfo
)

// WebhookConfig represents webhook configuration.
type WebhookConfig = internal.WebhookConfig

// ModerationStatus represents the status of a moderation action.
type ModerationStatus = internal.ModerationStatus

// Configuration.
type (
	Config                    = internal.Config
	ConfigOption              = internal.ConfigOption
	DistributedBackendOptions = internal.DistributedBackendOptions
)

// TypingOptions contains configuration for typing indicators.
type TypingOptions = internal.TypingOptions

// BackendError wraps backend-related errors with context.
type BackendError = internal.BackendError

// Backend, validation and lookup errors.
var (
	ErrInvalidConfig         = internal.ErrInvalidConfig
	ErrBackendNotFound       = internal.ErrBackendNotConnected
	ErrBackendTimeout        = internal.ErrBackendTimeout
	ErrBackendUnavailable    = internal.ErrBackendUnavailable
	ErrLockAcquisitionFailed = internal.ErrLockAcquisitionFailed
	ErrLockNotHeld           = internal.ErrLockNotHeld
	ErrNodeNotFound          = internal.ErrNodeNotFound
	ErrInvalidChannel        = internal.ErrInvalidChannel
	ErrInvalidRoom           = internal.ErrInvalidRoom
	ErrInvalidMessage        = internal.ErrInvalidMessage
	ErrPresenceNotFound      = internal.ErrPresenceNotFound
	ErrInvalidStatus         = internal.ErrInvalidStatus
	ErrInvalidPermission     = internal.ErrInvalidPermission
	ErrInsufficientRole      = internal.ErrInsufficientRole
	ErrMessageTooLarge       = internal.ErrMessageTooLarge
	ErrMessageNotFound       = internal.ErrMessageNotFound
)

// Connection errors.
var (
	ErrConnectionNotFound     = internal.ErrConnectionNotFound
	ErrConnectionClosed       = internal.ErrConnectionClosed
	ErrConnectionLimitReached = internal.ErrConnectionLimitReached
	ErrInvalidConnection      = internal.ErrInvalidConnection
)

// Room errors.
var (
	ErrRoomNotFound      = internal.ErrRoomNotFound
	ErrRoomAlreadyExists = internal.ErrRoomAlreadyExists
	ErrRoomFull          = internal.ErrRoomFull
	ErrNotRoomMember     = internal.ErrNotRoomMember
	ErrAlreadyRoomMember = internal.ErrAlreadyRoomMember
	ErrRoomLimitReached  = internal.ErrRoomLimitReached
)

// Channel errors.
var (
	ErrChannelNotFound      = internal.ErrChannelNotFound
	ErrChannelAlreadyExists = internal.ErrChannelAlreadyExists
	ErrNotSubscribed        = internal.ErrNotSubscribed
	ErrAlreadySubscribed    = internal.ErrAlreadySubscribed
	ErrChannelLimitReached  = internal.ErrChannelLimitReached
)

// ErrPermissionDenied is returned when the caller may not perform an action.
var ErrPermissionDenied = internal.ErrPermissionDenied

// Authorization errors.
var (
	ErrRoomAccessDenied    = internal.ErrRoomAccessDenied
	ErrChannelAccessDenied = internal.ErrChannelAccessDenied
	ErrSendDenied          = internal.ErrSendDenied
	ErrUserMuted           = internal.ErrUserMuted
	ErrUserBanned          = internal.ErrUserBanned
	ErrSessionNotOwned     = internal.ErrSessionNotOwned
)

// Feature-flag errors.
var (
	ErrRoomsDisabled    = internal.ErrRoomsDisabled
	ErrChannelsDisabled = internal.ErrChannelsDisabled
	ErrHistoryDisabled  = internal.ErrHistoryDisabled
	ErrPresenceDisabled = internal.ErrPresenceDisabled
	ErrTypingDisabled   = internal.ErrTypingDisabled
)

// ErrRateLimitExceeded is returned when a connection exceeds its rate limit.
var ErrRateLimitExceeded = internal.ErrRateLimitExceeded

// Invite errors.
var (
	ErrInviteNotFound = internal.ErrInviteNotFound
	ErrInviteExpired  = internal.ErrInviteExpired
)

// Error constructors.
var (
	NewConnectionError = internal.NewConnectionError
	NewRoomError       = internal.NewRoomError
	NewChannelError    = internal.NewChannelError
	NewMessageError    = internal.NewMessageError
	NewBackendError    = internal.NewBackendError
)

// NewLocalRoom creates a room backed by the in-process local store.
func NewLocalRoom(opts RoomOptions) *local.LocalRoom {
	return local.NewRoom(opts)
}

// Status constants.
const (
	StatusOnline  = internal.StatusOnline
	StatusAway    = internal.StatusAway
	StatusBusy    = internal.StatusBusy
	StatusOffline = internal.StatusOffline
)

// Message type constants.
//
// All seven kinds `internal` declares, deliberately: `TransportKinds` needs the
// complete set, and a reserved set that silently listed only some of them would
// be wrong in the direction that matters. A kind missing from it is a frame
// the client reports as an unknown message forever. Exporting them all also
// spares a caller outside this package spelling any value of `Message.Type` as
// a string literal.
const (
	MessageTypeMessage  = internal.MessageTypeMessage
	MessageTypeJoin     = internal.MessageTypeJoin
	MessageTypeLeave    = internal.MessageTypeLeave
	MessageTypeTyping   = internal.MessageTypeTyping
	MessageTypePresence = internal.MessageTypePresence
	MessageTypeSystem   = internal.MessageTypeSystem
	MessageTypeError    = internal.MessageTypeError
)

// Content type constants.
const (
	ContentTypeJSON     = internal.ContentTypeJSON
	ContentTypeBinary   = internal.ContentTypeBinary
	ContentTypeText     = internal.ContentTypeText
	ContentTypeMsgPack  = internal.ContentTypeMsgPack
	ContentTypeProtobuf = internal.ContentTypeProtobuf
)

// Default option functions.
var (
	DefaultPresenceOptions = internal.DefaultPresenceOptions
	DefaultTypingOptions   = internal.DefaultTypingOptions
)
