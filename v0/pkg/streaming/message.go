package streaming

import (
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

// PresenceStatus represents user presence status
type PresenceStatus = streamingcore.PresenceStatus

const (
	PresenceOnline    = streamingcore.PresenceOnline
	PresenceOffline   = streamingcore.PresenceOffline
	PresenceAway      = streamingcore.PresenceAway
	PresenceBusy      = streamingcore.PresenceBusy
	PresenceInvisible = streamingcore.PresenceInvisible
)

// MessageType represents the type of message being sent
type MessageType = streamingcore.MessageType

const (
	MessageTypeText      = streamingcore.MessageTypeText
	MessageTypeEvent     = streamingcore.MessageTypeEvent
	MessageTypePresence  = streamingcore.MessageTypePresence
	MessageTypeSystem    = streamingcore.MessageTypeSystem
	MessageTypeBroadcast = streamingcore.MessageTypeBroadcast
	MessageTypePrivate   = streamingcore.MessageTypePrivate
	MessageTypeTyping    = streamingcore.MessageTypeTyping
	MessageTypeError     = streamingcore.MessageTypeError
)

// Message represents a streaming message
type Message = streamingcore.Message

// MessageFilter represents criteria for filtering messages
type MessageFilter = streamingcore.MessageFilter

// SystemMessage represents system-generated messages
type SystemMessage = streamingcore.SystemMessage

// PresenceMessage represents presence update messages
type PresenceMessage = streamingcore.PresenceMessage

// TypingMessage represents typing indicator messages
type TypingMessage = streamingcore.TypingMessage

// ErrorMessage represents error messages
type ErrorMessage = streamingcore.ErrorMessage

// MessageBuilder helps construct messages
type MessageBuilder = streamingcore.MessageBuilder

// NewMessageBuilder creates a new message builder
func NewMessageBuilder() *MessageBuilder {
	return streamingcore.NewMessageBuilder()
}

// MessageSerializer handles message serialization/deserialization
type MessageSerializer = streamingcore.MessageSerializer

// JSONMessageSerializer implements JSON serialization
type JSONMessageSerializer = streamingcore.JSONMessageSerializer

// NewJSONMessageSerializer creates a new JSON message serializer
func NewJSONMessageSerializer() MessageSerializer {
	return streamingcore.NewJSONMessageSerializer()
}

// MessageValidator validates messages
type MessageValidator = streamingcore.MessageValidator

// DefaultMessageValidator implements basic message validation
type DefaultMessageValidator = streamingcore.DefaultMessageValidator

// NewDefaultMessageValidator creates a new default message validator
func NewDefaultMessageValidator() MessageValidator {
	return streamingcore.NewDefaultMessageValidator()
}

// MessagePriority represents message priority levels
type MessagePriority = streamingcore.MessagePriority

const (
	PriorityLow      = streamingcore.PriorityLow
	PriorityNormal   = streamingcore.PriorityNormal
	PriorityHigh     = streamingcore.PriorityHigh
	PriorityCritical = streamingcore.PriorityCritical
)

// PriorityMessage wraps a message with priority information
type PriorityMessage = streamingcore.PriorityMessage

// MessageQueue represents a priority message queue
type MessageQueue = streamingcore.MessageQueue

// Helper functions for creating specific message types

// NewTextMessage creates a new text message
func NewTextMessage(roomID, from, text string) *Message {
	return streamingcore.NewTextMessage(roomID, from, text)
}

// NewSystemMessage creates a new system message
func NewSystemMessage(code, message string, details map[string]interface{}) *Message {
	return streamingcore.NewSystemMessage(code, message, details)
}

// NewPresenceMessage creates a new presence message
func NewPresenceMessage(roomID, userID string, status PresenceStatus, metadata map[string]interface{}) *Message {
	return streamingcore.NewPresenceMessage(roomID, userID, status, metadata)
}

// NewTypingMessage creates a new typing indicator message
func NewTypingMessage(roomID, userID string, typing bool) *Message {
	return streamingcore.NewTypingMessage(roomID, userID, typing)
}

// NewErrorMessage creates a new error message
func NewErrorMessage(code, message, details string) *Message {
	return streamingcore.NewErrorMessage(code, message, details)
}

// MessageStats represents message statistics
type MessageStats = streamingcore.MessageStats

// MessageStatsCollector collects message statistics
type MessageStatsCollector = streamingcore.MessageStatsCollector

// NewMessageStatsCollector creates a new message stats collector
func NewMessageStatsCollector() *MessageStatsCollector {
	return streamingcore.NewMessageStatsCollector()
}

func NormalizeMessage(m *Message) *Message {
	return streamingcore.NormalizeMessage(m)
}
