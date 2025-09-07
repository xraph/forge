package core

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// PresenceStatus represents user presence status
type PresenceStatus string

const (
	PresenceOnline    PresenceStatus = "online"
	PresenceOffline   PresenceStatus = "offline"
	PresenceAway      PresenceStatus = "away"
	PresenceBusy      PresenceStatus = "busy"
	PresenceInvisible PresenceStatus = "invisible"
)

// MessageType represents the type of message being sent
type MessageType string

const (
	MessageTypeText      MessageType = "text"
	MessageTypeEvent     MessageType = "event"
	MessageTypePresence  MessageType = "presence"
	MessageTypeSystem    MessageType = "system"
	MessageTypeBroadcast MessageType = "broadcast"
	MessageTypePrivate   MessageType = "private"
	MessageTypeTyping    MessageType = "typing"
	MessageTypeError     MessageType = "error"
)

// Event represents a streaming message
type Event interface {
	ToMessage() *Message
}

// Message represents a streaming message
type Message struct {
	ID        string                 `json:"id" description:"Unique message identifier"`
	Type      MessageType            `json:"type" description:"Message type"`
	From      string                 `json:"from,omitempty"`
	To        string                 `json:"to,omitempty"`
	RoomID    string                 `json:"roomId" description:"Streaming room identifier"`
	Data      interface{}            `json:"data"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp" description:"Message timestamp"`
	TTL       time.Duration          `json:"ttl,omitempty"`
	Priority  MessagePriority        `json:"priority,omitempty"`
}

func (m *Message) ToMessage() *Message {
	return NormalizeMessage(m)
}

func NormalizeMessage(m *Message) *Message {
	if m.ID == "" {
		m.ID = generateMessageID()
	}

	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}

	if m.TTL < 0 {
		m.TTL = 0
	}

	if m.Priority == 0 {
		m.Priority = PriorityNormal
	}

	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}

	if m.Data == nil {
		m.Data = make(map[string]interface{})
	}

	if m.Type == "" {
		m.Type = MessageTypeText
	}

	return m
}

// MessageFilter represents criteria for filtering messages
type MessageFilter struct {
	Types     []MessageType `json:"types,omitempty"`
	FromUsers []string      `json:"from_users,omitempty"`
	ToUsers   []string      `json:"to_users,omitempty"`
	Rooms     []string      `json:"rooms,omitempty"`
	Since     *time.Time    `json:"since,omitempty"`
	Until     *time.Time    `json:"until,omitempty"`
	Limit     int           `json:"limit,omitempty"`
}

// SystemMessage represents system-generated messages
type SystemMessage struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// PresenceMessage represents presence update messages
type PresenceMessage struct {
	UserID   string                 `json:"user_id"`
	Status   PresenceStatus         `json:"status"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TypingMessage represents typing indicator messages
type TypingMessage struct {
	UserID  string `json:"user_id"`
	Typing  bool   `json:"typing"`
	RoomID  string `json:"room_id"`
	Timeout int    `json:"timeout,omitempty"` // seconds
}

// ErrorMessage represents error messages
type ErrorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// MessageBuilder helps construct messages
type MessageBuilder struct {
	message *Message
}

// NewMessageBuilder creates a new message builder
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		message: &Message{
			ID:        generateMessageID(),
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		},
	}
}

// Type sets the message type
func (mb *MessageBuilder) Type(msgType MessageType) *MessageBuilder {
	mb.message.Type = msgType
	return mb
}

// From sets the sender
func (mb *MessageBuilder) From(userID string) *MessageBuilder {
	mb.message.From = userID
	return mb
}

// To sets the recipient (for private messages)
func (mb *MessageBuilder) To(userID string) *MessageBuilder {
	mb.message.To = userID
	return mb
}

// Room sets the room ID
func (mb *MessageBuilder) Room(roomID string) *MessageBuilder {
	mb.message.RoomID = roomID
	return mb
}

// Data sets the message data
func (mb *MessageBuilder) Data(data interface{}) *MessageBuilder {
	mb.message.Data = data
	return mb
}

// Metadata adds metadata to the message
func (mb *MessageBuilder) Metadata(key string, value interface{}) *MessageBuilder {
	mb.message.Metadata[key] = value
	return mb
}

// TTL sets the time-to-live for the message
func (mb *MessageBuilder) TTL(ttl time.Duration) *MessageBuilder {
	mb.message.TTL = ttl
	return mb
}

// Build returns the constructed message
func (mb *MessageBuilder) Build() *Message {
	return mb.message
}

// MessageSerializer handles message serialization/deserialization
type MessageSerializer interface {
	Serialize(message *Message) ([]byte, error)
	Deserialize(data []byte) (*Message, error)
	ContentType() string
}

// JSONMessageSerializer implements JSON serialization
type JSONMessageSerializer struct{}

// NewJSONMessageSerializer creates a new JSON message serializer
func NewJSONMessageSerializer() MessageSerializer {
	return &JSONMessageSerializer{}
}

// Serialize serializes a message to JSON
func (s *JSONMessageSerializer) Serialize(message *Message) ([]byte, error) {
	return json.Marshal(message)
}

// Deserialize deserializes JSON data to a message
func (s *JSONMessageSerializer) Deserialize(data []byte) (*Message, error) {
	var message Message
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, err
	}
	return &message, nil
}

// ContentType returns the content type
func (s *JSONMessageSerializer) ContentType() string {
	return "application/json"
}

// MessageValidator validates messages
type MessageValidator interface {
	Validate(message *Message) error
}

// DefaultMessageValidator implements basic message validation
type DefaultMessageValidator struct {
	maxDataSize  int
	maxMetaSize  int
	allowedTypes map[MessageType]bool
}

// NewDefaultMessageValidator creates a new default message validator
func NewDefaultMessageValidator() MessageValidator {
	return &DefaultMessageValidator{
		maxDataSize: 64 * 1024, // 64KB
		maxMetaSize: 8 * 1024,  // 8KB
		allowedTypes: map[MessageType]bool{
			MessageTypeText:      true,
			MessageTypeEvent:     true,
			MessageTypePresence:  true,
			MessageTypeSystem:    true,
			MessageTypeBroadcast: true,
			MessageTypePrivate:   true,
			MessageTypeTyping:    true,
			MessageTypeError:     true,
		},
	}
}

// Validate validates a message
func (v *DefaultMessageValidator) Validate(message *Message) error {
	if message == nil {
		return common.ErrValidationError("message", fmt.Errorf("message cannot be nil"))
	}

	if message.ID == "" {
		return common.ErrValidationError("message.id", fmt.Errorf("message ID cannot be empty"))
	}

	if !v.allowedTypes[message.Type] {
		return common.ErrValidationError("message.type", fmt.Errorf("invalid message type: %s", message.Type))
	}

	if message.RoomID == "" && message.Type != MessageTypeSystem {
		return common.ErrValidationError("message.room_id", fmt.Errorf("room ID is required for non-system messages"))
	}

	// Validate data size
	if message.Data != nil {
		if dataBytes, err := json.Marshal(message.Data); err != nil {
			return common.ErrValidationError("message.data", fmt.Errorf("failed to serialize message data: %w", err))
		} else if len(dataBytes) > v.maxDataSize {
			return common.ErrValidationError("message.data", fmt.Errorf("message data too large: %d bytes (max: %d)", len(dataBytes), v.maxDataSize))
		}
	}

	// Validate metadata size
	if message.Metadata != nil {
		if metaBytes, err := json.Marshal(message.Metadata); err != nil {
			return common.ErrValidationError("message.metadata", fmt.Errorf("failed to serialize message metadata: %w", err))
		} else if len(metaBytes) > v.maxMetaSize {
			return common.ErrValidationError("message.metadata", fmt.Errorf("message metadata too large: %d bytes (max: %d)", len(metaBytes), v.maxMetaSize))
		}
	}

	// Validate TTL
	if message.TTL < 0 {
		return common.ErrValidationError("message.ttl", fmt.Errorf("TTL cannot be negative"))
	}

	// Validate timestamp
	if message.Timestamp.IsZero() {
		return common.ErrValidationError("message.timestamp", fmt.Errorf("timestamp cannot be zero"))
	}

	return nil
}

// MessagePriority represents message priority levels
type MessagePriority int

const (
	PriorityLow MessagePriority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// PriorityMessage wraps a message with priority information
type PriorityMessage struct {
	Message  *Message        `json:"message"`
	Priority MessagePriority `json:"priority"`
}

// MessageQueue represents a priority message queue
type MessageQueue interface {
	Push(message *PriorityMessage) error
	Pop() (*PriorityMessage, error)
	Peek() (*PriorityMessage, error)
	Size() int
	Clear()
}

// generateMessageID generates a unique message ID
func generateMessageID() string {
	return generateUniqueID("msg")
}

// generateUniqueID generates a unique ID with prefix
func generateUniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixNano(), randomString(8))
}

// randomString generates a random string of given length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// Helper functions for creating specific message types

// NewTextMessage creates a new text message
func NewTextMessage(roomID, from, text string) *Message {
	return NewMessageBuilder().
		Type(MessageTypeText).
		Room(roomID).
		From(from).
		Data(map[string]interface{}{"text": text}).
		Build()
}

// NewSystemMessage creates a new system message
func NewSystemMessage(code, message string, details map[string]interface{}) *Message {
	return NewMessageBuilder().
		Type(MessageTypeSystem).
		Data(SystemMessage{
			Code:    code,
			Message: message,
			Details: details,
		}).
		Build()
}

// NewPresenceMessage creates a new presence message
func NewPresenceMessage(roomID, userID string, status PresenceStatus, metadata map[string]interface{}) *Message {
	return NewMessageBuilder().
		Type(MessageTypePresence).
		Room(roomID).
		From(userID).
		Data(PresenceMessage{
			UserID:   userID,
			Status:   status,
			Metadata: metadata,
		}).
		Build()
}

// NewTypingMessage creates a new typing indicator message
func NewTypingMessage(roomID, userID string, typing bool) *Message {
	return NewMessageBuilder().
		Type(MessageTypeTyping).
		Room(roomID).
		From(userID).
		Data(TypingMessage{
			UserID: userID,
			Typing: typing,
			RoomID: roomID,
		}).
		TTL(5 * time.Second). // Typing indicators expire quickly
		Build()
}

// NewErrorMessage creates a new error message
func NewErrorMessage(code, message, details string) *Message {
	return NewMessageBuilder().
		Type(MessageTypeError).
		Data(ErrorMessage{
			Code:    code,
			Message: message,
			Details: details,
		}).
		Build()
}

// MessageStats represents message statistics
type MessageStats struct {
	TotalMessages   int64                 `json:"total_messages"`
	MessagesByType  map[MessageType]int64 `json:"messages_by_type"`
	MessagesByRoom  map[string]int64      `json:"messages_by_room"`
	MessagesPerHour []int64               `json:"messages_per_hour"`
	AverageSize     float64               `json:"average_size"`
	ErrorRate       float64               `json:"error_rate"`
	LastMessageTime time.Time             `json:"last_message_time"`
}

// MessageStatsCollector collects message statistics
type MessageStatsCollector struct {
	stats *MessageStats
	mu    sync.RWMutex
}

// NewMessageStatsCollector creates a new message stats collector
func NewMessageStatsCollector() *MessageStatsCollector {
	return &MessageStatsCollector{
		stats: &MessageStats{
			MessagesByType:  make(map[MessageType]int64),
			MessagesByRoom:  make(map[string]int64),
			MessagesPerHour: make([]int64, 24),
		},
	}
}

// RecordMessage records a message for statistics
func (c *MessageStatsCollector) RecordMessage(message *Message, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.TotalMessages++
	c.stats.MessagesByType[message.Type]++
	c.stats.MessagesByRoom[message.RoomID]++
	c.stats.LastMessageTime = time.Now()

	// Update average size
	if c.stats.TotalMessages == 1 {
		c.stats.AverageSize = float64(size)
	} else {
		c.stats.AverageSize = (c.stats.AverageSize*float64(c.stats.TotalMessages-1) + float64(size)) / float64(c.stats.TotalMessages)
	}

	// Update hourly stats
	hour := time.Now().Hour()
	c.stats.MessagesPerHour[hour]++
}

// GetStats returns the current statistics
func (c *MessageStatsCollector) GetStats() MessageStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create a copy to avoid race conditions
	stats := MessageStats{
		TotalMessages:   c.stats.TotalMessages,
		MessagesByType:  make(map[MessageType]int64),
		MessagesByRoom:  make(map[string]int64),
		MessagesPerHour: make([]int64, 24),
		AverageSize:     c.stats.AverageSize,
		ErrorRate:       c.stats.ErrorRate,
		LastMessageTime: c.stats.LastMessageTime,
	}

	for k, v := range c.stats.MessagesByType {
		stats.MessagesByType[k] = v
	}
	for k, v := range c.stats.MessagesByRoom {
		stats.MessagesByRoom[k] = v
	}
	copy(stats.MessagesPerHour, c.stats.MessagesPerHour)

	return stats
}
