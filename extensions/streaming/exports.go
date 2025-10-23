package streaming

import "github.com/xraph/forge/extensions/streaming/internal"

// Core interfaces
type Manager = internal.Manager
type Room = internal.Room
type Channel = internal.Channel
type Member = internal.Member
type UserPresence = internal.UserPresence
type Connection = internal.EnhancedConnection

// Store interfaces
type RoomStore = internal.RoomStore
type ChannelStore = internal.ChannelStore
type MessageStore = internal.MessageStore
type PresenceStore = internal.PresenceStore
type TypingStore = internal.TypingStore

// Tracker interfaces
type PresenceTracker = internal.PresenceTracker
type TypingTracker = internal.TypingTracker

// Distributed backend
type DistributedBackend = internal.DistributedBackend
type Lock = internal.Lock
type NodeInfo = internal.NodeInfo
type NodeChangeHandler = internal.NodeChangeHandler
type MessageHandler = internal.MessageHandler
type NodeChangeEvent = internal.NodeChangeEvent

// Message types
type Message = internal.Message
type MessageReaction = internal.MessageReaction
type MessageEdit = internal.MessageEdit

// Room types
type Invite = internal.Invite
type InviteOptions = internal.InviteOptions
type RoomBan = internal.RoomBan
type ModerationEvent = internal.ModerationEvent
type RoomEvent = internal.RoomEvent
type RoomOptions = internal.RoomOptions
type MemberOptions = internal.MemberOptions

// Connection types
type ConnectionInfo = internal.ConnectionInfo

// Statistics types
type RoomStats = internal.RoomStats
type UserStats = internal.UserStats
type ManagerStats = internal.ManagerStats
type OnlineStats = internal.OnlineStats
type UserPresenceStats = internal.UserPresenceStats

// Presence types
type PresenceEvent = internal.PresenceEvent
type PresenceOptions = internal.PresenceOptions
type PresenceFilters = internal.PresenceFilters
type DeviceInfo = internal.DeviceInfo
type ActivityInfo = internal.ActivityInfo
type Availability = internal.Availability

// Rate limiting
type RateLimitStatus = internal.RateLimitStatus

// Query types
type HistoryQuery = internal.HistoryQuery

// Configuration
type Config = internal.Config
type ConfigOption = internal.ConfigOption
type DistributedBackendOptions = internal.DistributedBackendOptions

// Typing
type TypingOptions = internal.TypingOptions

// Backend error
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
