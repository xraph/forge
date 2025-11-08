package streaming

import (
	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// Core interfaces.
type Manager = internal.Manager
type Room = internal.Room
type Channel = internal.Channel
type Member = internal.Member
type UserPresence = internal.UserPresence
type Connection = internal.EnhancedConnection

// Store interfaces.
type RoomStore = internal.RoomStore
type ChannelStore = internal.ChannelStore
type MessageStore = internal.MessageStore
type PresenceStore = internal.PresenceStore
type TypingStore = internal.TypingStore

// Tracker interfaces.
type PresenceTracker = internal.PresenceTracker
type TypingTracker = internal.TypingTracker

// Distributed backend.
type DistributedBackend = internal.DistributedBackend
type Lock = internal.Lock
type NodeInfo = internal.NodeInfo
type NodeChangeHandler = internal.NodeChangeHandler
type MessageHandler = internal.MessageHandler
type NodeChangeEvent = internal.NodeChangeEvent

// Message types.
type Message = internal.Message
type MessageReaction = internal.MessageReaction
type MessageEdit = internal.MessageEdit

// Room types.
type Invite = internal.Invite
type InviteOptions = internal.InviteOptions
type RoomBan = internal.RoomBan
type ModerationEvent = internal.ModerationEvent
type RoomEvent = internal.RoomEvent
type RoomOptions = internal.RoomOptions
type MemberOptions = internal.MemberOptions

// Connection types.
type ConnectionInfo = internal.ConnectionInfo

// Statistics types.
type RoomStats = internal.RoomStats

// Room creation (local backend).
type LocalRoom = *local.LocalRoom
type UserStats = internal.UserStats
type ManagerStats = internal.ManagerStats
type OnlineStats = internal.OnlineStats
type UserPresenceStats = internal.UserPresenceStats

// Presence types.
type PresenceEvent = internal.PresenceEvent
type PresenceOptions = internal.PresenceOptions
type PresenceFilters = internal.PresenceFilters
type DeviceInfo = internal.DeviceInfo
type ActivityInfo = internal.ActivityInfo
type Availability = internal.Availability

// Rate limiting.
type RateLimitStatus = internal.RateLimitStatus

// Query types.
type HistoryQuery = internal.HistoryQuery
type MessageSearchQuery = internal.MessageSearchQuery
type AnalyticsQuery = internal.AnalyticsQuery
type FileQuery = internal.FileQuery

// Analytics types.
type AnalyticsResult = internal.AnalyticsResult
type AnalyticsEvent = internal.AnalyticsEvent

// File types.
type FileUpload = internal.FileUpload
type FileInfo = internal.FileInfo

// Webhook types.
type WebhookConfig = internal.WebhookConfig

// Moderation types.
type ModerationStatus = internal.ModerationStatus

// Configuration.
type Config = internal.Config
type ConfigOption = internal.ConfigOption
type DistributedBackendOptions = internal.DistributedBackendOptions

// Typing.
type TypingOptions = internal.TypingOptions

// Backend error.
type BackendError = internal.BackendError

var ErrInvalidConfig = internal.ErrInvalidConfig
var ErrBackendNotFound = internal.ErrBackendNotConnected
var ErrBackendTimeout = internal.ErrBackendTimeout
var ErrBackendUnavailable = internal.ErrBackendUnavailable
var ErrLockAcquisitionFailed = internal.ErrLockAcquisitionFailed
var ErrLockNotHeld = internal.ErrLockNotHeld
var ErrNodeNotFound = internal.ErrNodeNotFound
var ErrInvalidChannel = internal.ErrInvalidChannel
var ErrInvalidRoom = internal.ErrInvalidRoom
var ErrInvalidMessage = internal.ErrInvalidMessage
var ErrPresenceNotFound = internal.ErrPresenceNotFound
var ErrInvalidStatus = internal.ErrInvalidStatus
var ErrInvalidPermission = internal.ErrInvalidPermission
var ErrInsufficientRole = internal.ErrInsufficientRole
var ErrMessageTooLarge = internal.ErrMessageTooLarge
var ErrMessageNotFound = internal.ErrMessageNotFound

// Connection errors.
var ErrConnectionNotFound = internal.ErrConnectionNotFound
var ErrConnectionClosed = internal.ErrConnectionClosed
var ErrConnectionLimitReached = internal.ErrConnectionLimitReached
var ErrInvalidConnection = internal.ErrInvalidConnection

// Room errors.
var ErrRoomNotFound = internal.ErrRoomNotFound
var ErrRoomAlreadyExists = internal.ErrRoomAlreadyExists
var ErrRoomFull = internal.ErrRoomFull
var ErrNotRoomMember = internal.ErrNotRoomMember
var ErrAlreadyRoomMember = internal.ErrAlreadyRoomMember
var ErrRoomLimitReached = internal.ErrRoomLimitReached

// Channel errors.
var ErrChannelNotFound = internal.ErrChannelNotFound
var ErrChannelAlreadyExists = internal.ErrChannelAlreadyExists
var ErrNotSubscribed = internal.ErrNotSubscribed
var ErrAlreadySubscribed = internal.ErrAlreadySubscribed

// Permission errors.
var ErrPermissionDenied = internal.ErrPermissionDenied

// Invite errors.
var ErrInviteNotFound = internal.ErrInviteNotFound
var ErrInviteExpired = internal.ErrInviteExpired

// Error constructors.
var NewConnectionError = internal.NewConnectionError
var NewRoomError = internal.NewRoomError
var NewChannelError = internal.NewChannelError
var NewMessageError = internal.NewMessageError
var NewBackendError = internal.NewBackendError

// Room creation.
func NewLocalRoom(opts RoomOptions) *local.LocalRoom {
	return local.NewRoom(opts)
}

// Status constants.
const StatusOnline = internal.StatusOnline
const StatusAway = internal.StatusAway
const StatusBusy = internal.StatusBusy
const StatusOffline = internal.StatusOffline

// Message type constants.
const MessageTypeMessage = internal.MessageTypeMessage
const MessageTypeJoin = internal.MessageTypeJoin
const MessageTypeLeave = internal.MessageTypeLeave
const MessageTypeTyping = internal.MessageTypeTyping
const MessageTypePresence = internal.MessageTypePresence

// Default option functions.
var DefaultPresenceOptions = internal.DefaultPresenceOptions
var DefaultTypingOptions = internal.DefaultTypingOptions
