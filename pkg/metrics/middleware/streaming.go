package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
)

// =============================================================================
// STREAMING METRICS MIDDLEWARE
// =============================================================================

// StreamingMetricsMiddleware provides automatic streaming metrics collection
type StreamingMetricsMiddleware struct {
	name      string
	collector metrics.MetricsCollector
	config    *StreamingMetricsConfig
	logger    logger.Logger
	started   bool
	mu        sync.RWMutex

	// Connection metrics
	connectionsActive     metrics.Gauge
	connectionsTotal      metrics.Counter
	connectionDuration    metrics.Histogram
	connectionErrors      metrics.Counter
	connectionUpgrades    metrics.Counter
	connectionsByProtocol map[string]metrics.Counter
	connectionsByRoom     map[string]metrics.Gauge

	// Message metrics
	messagesTotal    metrics.Counter
	messageSize      metrics.Histogram
	messageLatency   metrics.Histogram
	messageErrors    metrics.Counter
	messagesSent     metrics.Counter
	messagesReceived metrics.Counter
	messagesByType   map[string]metrics.Counter
	messagesByRoom   map[string]metrics.Counter

	// Room metrics
	roomsActive  metrics.Gauge
	roomsTotal   metrics.Counter
	roomMembers  metrics.Histogram
	roomMessages metrics.Counter
	roomJoins    metrics.Counter
	roomLeaves   metrics.Counter

	// Presence metrics
	presenceUpdates  metrics.Counter
	presenceUsers    metrics.Gauge
	presenceByStatus map[string]metrics.Gauge

	// Performance metrics
	broadcastLatency    metrics.Histogram
	broadcastSize       metrics.Histogram
	subscriptionLatency metrics.Histogram
	heartbeatLatency    metrics.Histogram
	heartbeatMissed     metrics.Counter
}

// StreamingMetricsConfig contains configuration for streaming metrics middleware
type StreamingMetricsConfig struct {
	Enabled                 bool          `yaml:"enabled" json:"enabled"`
	CollectConnectionStats  bool          `yaml:"collect_connection_stats" json:"collect_connection_stats"`
	CollectMessageStats     bool          `yaml:"collect_message_stats" json:"collect_message_stats"`
	CollectRoomStats        bool          `yaml:"collect_room_stats" json:"collect_room_stats"`
	CollectPresenceStats    bool          `yaml:"collect_presence_stats" json:"collect_presence_stats"`
	CollectPerformanceStats bool          `yaml:"collect_performance_stats" json:"collect_performance_stats"`
	GroupByProtocol         bool          `yaml:"group_by_protocol" json:"group_by_protocol"`
	GroupByRoom             bool          `yaml:"group_by_room" json:"group_by_room"`
	GroupByMessageType      bool          `yaml:"group_by_message_type" json:"group_by_message_type"`
	GroupByPresenceStatus   bool          `yaml:"group_by_presence_status" json:"group_by_presence_status"`
	MaxRooms                int           `yaml:"max_rooms" json:"max_rooms"`
	MaxMessageTypes         int           `yaml:"max_message_types" json:"max_message_types"`
	SlowMessageThreshold    time.Duration `yaml:"slow_message_threshold" json:"slow_message_threshold"`
	LargeMessageThreshold   int64         `yaml:"large_message_threshold" json:"large_message_threshold"`
	LogSlowMessages         bool          `yaml:"log_slow_messages" json:"log_slow_messages"`
	LogLargeMessages        bool          `yaml:"log_large_messages" json:"log_large_messages"`
	StatsInterval           time.Duration `yaml:"stats_interval" json:"stats_interval"`
}

// DefaultStreamingMetricsConfig returns default configuration
func DefaultStreamingMetricsConfig() *StreamingMetricsConfig {
	return &StreamingMetricsConfig{
		Enabled:                 true,
		CollectConnectionStats:  true,
		CollectMessageStats:     true,
		CollectRoomStats:        true,
		CollectPresenceStats:    true,
		CollectPerformanceStats: true,
		GroupByProtocol:         true,
		GroupByRoom:             true,
		GroupByMessageType:      true,
		GroupByPresenceStatus:   true,
		MaxRooms:                1000,
		MaxMessageTypes:         50,
		SlowMessageThreshold:    time.Millisecond * 100,
		LargeMessageThreshold:   1024 * 1024, // 1MB
		LogSlowMessages:         true,
		LogLargeMessages:        true,
		StatsInterval:           time.Second * 30,
	}
}

// NewStreamingMetricsMiddleware creates a new streaming metrics middleware
func NewStreamingMetricsMiddleware(collector metrics.MetricsCollector) *StreamingMetricsMiddleware {
	return NewStreamingMetricsMiddlewareWithConfig(collector, DefaultStreamingMetricsConfig())
}

// NewStreamingMetricsMiddlewareWithConfig creates a new streaming metrics middleware with configuration
func NewStreamingMetricsMiddlewareWithConfig(collector metrics.MetricsCollector, config *StreamingMetricsConfig) *StreamingMetricsMiddleware {
	return &StreamingMetricsMiddleware{
		name:                  "streaming-metrics",
		collector:             collector,
		config:                config,
		connectionsByProtocol: make(map[string]metrics.Counter),
		connectionsByRoom:     make(map[string]metrics.Gauge),
		messagesByType:        make(map[string]metrics.Counter),
		messagesByRoom:        make(map[string]metrics.Counter),
		presenceByStatus:      make(map[string]metrics.Gauge),
	}
}

// =============================================================================
// SERVICE IMPLEMENTATION
// =============================================================================

// Name returns the middleware name
func (m *StreamingMetricsMiddleware) Name() string {
	return m.name
}

// Dependencies returns the middleware dependencies
func (m *StreamingMetricsMiddleware) Dependencies() []string {
	return []string{}
}

// OnStart is called when the middleware starts
func (m *StreamingMetricsMiddleware) OnStart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrServiceAlreadyExists(m.name)
	}

	// Initialize metrics
	m.initializeMetrics()

	m.started = true

	if m.logger != nil {
		m.logger.Info("Streaming metrics middleware started",
			logger.String("name", m.name),
			logger.Bool("enabled", m.config.Enabled),
			logger.Bool("collect_connection_stats", m.config.CollectConnectionStats),
			logger.Bool("collect_message_stats", m.config.CollectMessageStats),
			logger.Bool("collect_room_stats", m.config.CollectRoomStats),
			logger.Bool("collect_presence_stats", m.config.CollectPresenceStats),
			logger.Duration("slow_message_threshold", m.config.SlowMessageThreshold),
			logger.Int64("large_message_threshold", m.config.LargeMessageThreshold),
		)
	}

	return nil
}

// OnStop is called when the middleware stops
func (m *StreamingMetricsMiddleware) OnStop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return common.ErrServiceNotFound(m.name)
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("Streaming metrics middleware stopped", logger.String("name", m.name))
	}

	return nil
}

// OnHealthCheck is called to check middleware health
func (m *StreamingMetricsMiddleware) OnHealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return common.ErrHealthCheckFailed(m.name, fmt.Errorf("middleware not started"))
	}

	return nil
}

// =============================================================================
// METRICS COLLECTION METHODS
// =============================================================================

// RecordConnection records connection metrics
func (m *StreamingMetricsMiddleware) RecordConnection(conn ConnectionInfo) {
	if !m.config.Enabled || !m.config.CollectConnectionStats {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch conn.Event {
	case "connect":
		m.connectionsActive.Inc()
		m.connectionsTotal.Inc()
		m.connectionUpgrades.Inc()

		if m.config.GroupByProtocol {
			m.recordProtocolConnection(conn.Protocol)
		}

		if m.config.GroupByRoom && conn.Room != "" {
			m.recordRoomConnection(conn.Room, 1)
		}

	case "disconnect":
		m.connectionsActive.Dec()
		m.connectionDuration.Observe(conn.Duration.Seconds())

		if m.config.GroupByRoom && conn.Room != "" {
			m.recordRoomConnection(conn.Room, -1)
		}

	case "error":
		m.connectionErrors.Inc()
	}

	if m.logger != nil {
		m.logger.Debug("Connection event recorded",
			logger.String("event", conn.Event),
			logger.String("protocol", conn.Protocol),
			logger.String("room", conn.Room),
			logger.String("user_id", conn.UserID),
			logger.Duration("duration", conn.Duration),
		)
	}
}

// RecordMessage records message metrics
func (m *StreamingMetricsMiddleware) RecordMessage(msg MessageInfo) {
	if !m.config.Enabled || !m.config.CollectMessageStats {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Record basic message metrics
	m.messagesTotal.Inc()
	m.messageSize.Observe(float64(msg.Size))

	if msg.Latency > 0 {
		m.messageLatency.Observe(msg.Latency.Seconds())
	}

	if msg.Direction == "sent" {
		m.messagesSent.Inc()
	} else if msg.Direction == "received" {
		m.messagesReceived.Inc()
	}

	// Record error if present
	if msg.Error != nil {
		m.messageErrors.Inc()
	}

	// Check for slow messages
	if m.config.LogSlowMessages && msg.Latency > m.config.SlowMessageThreshold {
		if m.logger != nil {
			m.logger.Warn("Slow message detected",
				logger.String("room", msg.Room),
				logger.String("type", msg.Type),
				logger.String("direction", msg.Direction),
				logger.Duration("latency", msg.Latency),
				logger.Int64("size", msg.Size),
			)
		}
	}

	// Check for large messages
	if m.config.LogLargeMessages && msg.Size > m.config.LargeMessageThreshold {
		if m.logger != nil {
			m.logger.Warn("Large message detected",
				logger.String("room", msg.Room),
				logger.String("type", msg.Type),
				logger.String("direction", msg.Direction),
				logger.Int64("size", msg.Size),
			)
		}
	}

	// Record grouped metrics
	if m.config.GroupByMessageType {
		m.recordMessageType(msg.Type)
	}

	if m.config.GroupByRoom && msg.Room != "" {
		m.recordRoomMessage(msg.Room)
	}

	if m.logger != nil {
		m.logger.Debug("Message event recorded",
			logger.String("room", msg.Room),
			logger.String("type", msg.Type),
			logger.String("direction", msg.Direction),
			logger.Duration("latency", msg.Latency),
			logger.Int64("size", msg.Size),
			logger.Bool("error", msg.Error != nil),
		)
	}
}

// RecordRoom records room metrics
func (m *StreamingMetricsMiddleware) RecordRoom(room RoomInfo) {
	if !m.config.Enabled || !m.config.CollectRoomStats {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch room.Event {
	case "create":
		m.roomsActive.Inc()
		m.roomsTotal.Inc()

	case "destroy":
		m.roomsActive.Dec()

	case "join":
		m.roomJoins.Inc()
		m.roomMembers.Observe(float64(room.MemberCount))

	case "leave":
		m.roomLeaves.Inc()
		m.roomMembers.Observe(float64(room.MemberCount))
	}

	if m.logger != nil {
		m.logger.Debug("Room event recorded",
			logger.String("event", room.Event),
			logger.String("room", room.Name),
			logger.Int("member_count", room.MemberCount),
		)
	}
}

// RecordPresence records presence metrics
func (m *StreamingMetricsMiddleware) RecordPresence(presence PresenceInfo) {
	if !m.config.Enabled || !m.config.CollectPresenceStats {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.presenceUpdates.Inc()
	m.presenceUsers.Set(float64(presence.TotalUsers))

	if m.config.GroupByPresenceStatus {
		m.recordPresenceStatus(presence.Status, presence.UserCount)
	}

	if m.logger != nil {
		m.logger.Debug("Presence event recorded",
			logger.String("status", presence.Status),
			logger.Int("user_count", presence.UserCount),
			logger.Int("total_users", presence.TotalUsers),
		)
	}
}

// RecordBroadcast records broadcast metrics
func (m *StreamingMetricsMiddleware) RecordBroadcast(broadcast BroadcastInfo) {
	if !m.config.Enabled || !m.config.CollectPerformanceStats {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.broadcastLatency.Observe(broadcast.Latency.Seconds())
	m.broadcastSize.Observe(float64(broadcast.Size))

	if m.logger != nil {
		m.logger.Debug("Broadcast event recorded",
			logger.String("room", broadcast.Room),
			logger.Int("recipient_count", broadcast.RecipientCount),
			logger.Duration("latency", broadcast.Latency),
			logger.Int64("size", broadcast.Size),
		)
	}
}

// RecordHeartbeat records heartbeat metrics
func (m *StreamingMetricsMiddleware) RecordHeartbeat(heartbeat HeartbeatInfo) {
	if !m.config.Enabled || !m.config.CollectPerformanceStats {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if heartbeat.Missed {
		m.heartbeatMissed.Inc()
	} else {
		m.heartbeatLatency.Observe(heartbeat.Latency.Seconds())
	}

	if m.logger != nil {
		m.logger.Debug("Heartbeat event recorded",
			logger.String("connection_id", heartbeat.ConnectionID),
			logger.Duration("latency", heartbeat.Latency),
			logger.Bool("missed", heartbeat.Missed),
		)
	}
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// initializeMetrics initializes the metrics
func (m *StreamingMetricsMiddleware) initializeMetrics() {
	// Connection metrics
	if m.config.CollectConnectionStats {
		m.connectionsActive = m.collector.Gauge("streaming_connections_active")
		m.connectionsTotal = m.collector.Counter("streaming_connections_total", "protocol", "room")
		m.connectionDuration = m.collector.Histogram("streaming_connection_duration_seconds", "protocol", "room")
		m.connectionErrors = m.collector.Counter("streaming_connection_errors_total", "protocol", "room")
		m.connectionUpgrades = m.collector.Counter("streaming_connection_upgrades_total", "protocol")
	}

	// Message metrics
	if m.config.CollectMessageStats {
		m.messagesTotal = m.collector.Counter("streaming_messages_total", "room", "type", "direction")
		m.messageSize = m.collector.Histogram("streaming_message_size_bytes", "room", "type", "direction")
		m.messageLatency = m.collector.Histogram("streaming_message_latency_seconds", "room", "type", "direction")
		m.messageErrors = m.collector.Counter("streaming_message_errors_total", "room", "type", "direction")
		m.messagesSent = m.collector.Counter("streaming_messages_sent_total", "room", "type")
		m.messagesReceived = m.collector.Counter("streaming_messages_received_total", "room", "type")
	}

	// Room metrics
	if m.config.CollectRoomStats {
		m.roomsActive = m.collector.Gauge("streaming_rooms_active")
		m.roomsTotal = m.collector.Counter("streaming_rooms_total")
		m.roomMembers = m.collector.Histogram("streaming_room_members", "room")
		m.roomMessages = m.collector.Counter("streaming_room_messages_total", "room")
		m.roomJoins = m.collector.Counter("streaming_room_joins_total", "room")
		m.roomLeaves = m.collector.Counter("streaming_room_leaves_total", "room")
	}

	// Presence metrics
	if m.config.CollectPresenceStats {
		m.presenceUpdates = m.collector.Counter("streaming_presence_updates_total", "status")
		m.presenceUsers = m.collector.Gauge("streaming_presence_users_total")
	}

	// Performance metrics
	if m.config.CollectPerformanceStats {
		m.broadcastLatency = m.collector.Histogram("streaming_broadcast_latency_seconds", "room")
		m.broadcastSize = m.collector.Histogram("streaming_broadcast_size_bytes", "room")
		m.subscriptionLatency = m.collector.Histogram("streaming_subscription_latency_seconds", "room")
		m.heartbeatLatency = m.collector.Histogram("streaming_heartbeat_latency_seconds")
		m.heartbeatMissed = m.collector.Counter("streaming_heartbeat_missed_total")
	}
}

// recordProtocolConnection records a protocol-specific connection metric
func (m *StreamingMetricsMiddleware) recordProtocolConnection(protocol string) {
	counter, exists := m.connectionsByProtocol[protocol]
	if !exists {
		counter = m.collector.Counter("streaming_connections_by_protocol", "protocol", protocol)
		m.connectionsByProtocol[protocol] = counter
	}
	counter.Inc()
}

// recordRoomConnection records a room-specific connection metric
func (m *StreamingMetricsMiddleware) recordRoomConnection(room string, delta int) {
	if len(m.connectionsByRoom) >= m.config.MaxRooms {
		return // Prevent unlimited metric creation
	}

	gauge, exists := m.connectionsByRoom[room]
	if !exists {
		gauge = m.collector.Gauge("streaming_connections_by_room", "room", room)
		m.connectionsByRoom[room] = gauge
	}

	if delta > 0 {
		gauge.Inc()
	} else {
		gauge.Dec()
	}
}

// recordMessageType records a message type specific metric
func (m *StreamingMetricsMiddleware) recordMessageType(messageType string) {
	if len(m.messagesByType) >= m.config.MaxMessageTypes {
		return // Prevent unlimited metric creation
	}

	counter, exists := m.messagesByType[messageType]
	if !exists {
		counter = m.collector.Counter("streaming_messages_by_type", "type", messageType)
		m.messagesByType[messageType] = counter
	}
	counter.Inc()
}

// recordRoomMessage records a room-specific message metric
func (m *StreamingMetricsMiddleware) recordRoomMessage(room string) {
	if len(m.messagesByRoom) >= m.config.MaxRooms {
		return // Prevent unlimited metric creation
	}

	counter, exists := m.messagesByRoom[room]
	if !exists {
		counter = m.collector.Counter("streaming_messages_by_room", "room", room)
		m.messagesByRoom[room] = counter
	}
	counter.Inc()
}

// recordPresenceStatus records a presence status specific metric
func (m *StreamingMetricsMiddleware) recordPresenceStatus(status string, userCount int) {
	gauge, exists := m.presenceByStatus[status]
	if !exists {
		gauge = m.collector.Gauge("streaming_presence_by_status", "status", status)
		m.presenceByStatus[status] = gauge
	}
	gauge.Set(float64(userCount))
}

// SetLogger sets the logger
func (m *StreamingMetricsMiddleware) SetLogger(logger logger.Logger) {
	m.logger = logger
}

// =============================================================================
// SUPPORTING TYPES
// =============================================================================

// ConnectionInfo contains information about a streaming connection
type ConnectionInfo struct {
	Event    string // "connect", "disconnect", "error"
	Protocol string // "websocket", "sse", "polling"
	Room     string
	UserID   string
	Duration time.Duration // Only for disconnect events
	Error    error         // Only for error events
}

// MessageInfo contains information about a streaming message
type MessageInfo struct {
	Room      string
	Type      string
	Direction string // "sent", "received"
	Size      int64
	Latency   time.Duration
	Error     error
}

// RoomInfo contains information about a streaming room
type RoomInfo struct {
	Event       string // "create", "destroy", "join", "leave"
	Name        string
	MemberCount int
}

// PresenceInfo contains information about presence updates
type PresenceInfo struct {
	Status     string // "online", "offline", "away", "busy"
	UserCount  int    // Users with this status
	TotalUsers int    // Total users
}

// BroadcastInfo contains information about broadcast operations
type BroadcastInfo struct {
	Room           string
	RecipientCount int
	Size           int64
	Latency        time.Duration
}

// HeartbeatInfo contains information about heartbeat operations
type HeartbeatInfo struct {
	ConnectionID string
	Latency      time.Duration
	Missed       bool
}

// =============================================================================
// MIDDLEWARE HANDLER
// =============================================================================

// StreamingMetricsHandler creates a middleware handler for streaming metrics
func (m *StreamingMetricsMiddleware) StreamingMetricsHandler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !m.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Check if this is a WebSocket upgrade request
			if r.Header.Get("Upgrade") == "websocket" {
				start := time.Now()

				// Record connection attempt
				m.RecordConnection(ConnectionInfo{
					Event:    "connect",
					Protocol: "websocket",
					Room:     r.Header.Get("X-Room-ID"),
					UserID:   r.Header.Get("X-User-ID"),
				})

				// Create response wrapper to detect upgrade success
				wrapper := &streamingResponseWrapper{
					ResponseWriter: w,
					statusCode:     http.StatusOK,
				}

				next.ServeHTTP(wrapper, r)

				// Record connection result
				if wrapper.statusCode != http.StatusSwitchingProtocols {
					m.RecordConnection(ConnectionInfo{
						Event:    "error",
						Protocol: "websocket",
						Room:     r.Header.Get("X-Room-ID"),
						UserID:   r.Header.Get("X-User-ID"),
						Duration: time.Since(start),
					})
				}
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// streamingResponseWrapper wraps http.ResponseWriter to capture metrics
type streamingResponseWrapper struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code
func (w *streamingResponseWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// CreateStreamingMetricsMiddleware creates streaming metrics middleware
func CreateStreamingMetricsMiddleware(collector metrics.MetricsCollector) *StreamingMetricsMiddleware {
	return NewStreamingMetricsMiddleware(collector)
}

// CreateStreamingMetricsMiddlewareWithConfig creates streaming metrics middleware with custom config
func CreateStreamingMetricsMiddlewareWithConfig(collector metrics.MetricsCollector, config *StreamingMetricsConfig) *StreamingMetricsMiddleware {
	return NewStreamingMetricsMiddlewareWithConfig(collector, config)
}

// MiddlewareDefinition returns the middleware definition for the router
func (m *StreamingMetricsMiddleware) MiddlewareDefinition() common.MiddlewareDefinition {
	return common.MiddlewareDefinition{
		Name:         m.name,
		Priority:     90, // Run before HTTP metrics but after other middleware
		Handler:      m.StreamingMetricsHandler(),
		Dependencies: m.Dependencies(),
		Config:       m.config,
	}
}

// RegisterStreamingMetricsMiddleware registers streaming metrics middleware with the router
func RegisterStreamingMetricsMiddleware(router common.Router, collector metrics.MetricsCollector) error {
	middleware := CreateStreamingMetricsMiddleware(collector)
	return router.Use(middleware.MiddlewareDefinition())
}

// RegisterStreamingMetricsMiddlewareWithConfig registers streaming metrics middleware with custom config
func RegisterStreamingMetricsMiddlewareWithConfig(router common.Router, collector metrics.MetricsCollector, config *StreamingMetricsConfig) error {
	middleware := CreateStreamingMetricsMiddlewareWithConfig(collector, config)
	return router.Use(middleware.MiddlewareDefinition())
}
