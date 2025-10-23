package collectors

import (
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/logger"
	metrics "github.com/xraph/forge/v0/pkg/metrics/core"
	"github.com/xraph/forge/v0/pkg/streaming"
)

// =============================================================================
// STREAMING COLLECTOR
// =============================================================================

// StreamingCollector collects streaming metrics from a streaming system
type StreamingCollector struct {
	name               string
	interval           time.Duration
	streamingManager   streaming.StreamingManager
	logger             logger.Logger
	metrics            map[string]interface{}
	enabled            bool
	mu                 sync.RWMutex
	lastCollectionTime time.Time
	connectionStats    map[string]*ConnectionStats
	roomStats          map[string]*RoomStats
	messageStats       map[string]*MessageStats
	presenceStats      map[string]*PresenceStats
}

// StreamingCollectorConfig contains configuration for the streaming collector
type StreamingCollectorConfig struct {
	Interval               time.Duration `yaml:"interval" json:"interval"`
	CollectConnectionStats bool          `yaml:"collect_connection_stats" json:"collect_connection_stats"`
	CollectRoomStats       bool          `yaml:"collect_room_stats" json:"collect_room_stats"`
	CollectMessageStats    bool          `yaml:"collect_message_stats" json:"collect_message_stats"`
	CollectPresenceStats   bool          `yaml:"collect_presence_stats" json:"collect_presence_stats"`
	CollectProtocolStats   bool          `yaml:"collect_protocol_stats" json:"collect_protocol_stats"`
	TrackMessageTypes      bool          `yaml:"track_message_types" json:"track_message_types"`
	TrackUserActivity      bool          `yaml:"track_user_activity" json:"track_user_activity"`
	MaxConnectionHistory   int           `yaml:"max_connection_history" json:"max_connection_history"`
	SlowMessageThreshold   time.Duration `yaml:"slow_message_threshold" json:"slow_message_threshold"`
}

// ConnectionStats represents connection statistics
type ConnectionStats struct {
	Protocol            string        `json:"protocol"`
	TotalConnections    int64         `json:"total_connections"`
	ActiveConnections   int64         `json:"active_connections"`
	ConnectionErrors    int64         `json:"connection_errors"`
	ConnectionDuration  time.Duration `json:"connection_duration"`
	AverageSessionTime  time.Duration `json:"average_session_time"`
	MessagesSent        int64         `json:"messages_sent"`
	MessagesReceived    int64         `json:"messages_received"`
	BytesSent           int64         `json:"bytes_sent"`
	BytesReceived       int64         `json:"bytes_received"`
	LastConnection      time.Time     `json:"last_connection"`
	LastDisconnection   time.Time     `json:"last_disconnection"`
	DisconnectionReason string        `json:"disconnection_reason"`
}

// RoomStats represents room statistics
type RoomStats struct {
	RoomID             string        `json:"room_id"`
	ActiveConnections  int           `json:"active_connections"`
	TotalJoins         int64         `json:"total_joins"`
	TotalLeaves        int64         `json:"total_leaves"`
	MessagesExchanged  int64         `json:"messages_exchanged"`
	BroadcastMessages  int64         `json:"broadcast_messages"`
	DirectMessages     int64         `json:"direct_messages"`
	AverageMessageSize float64       `json:"average_message_size"`
	LastActivity       time.Time     `json:"last_activity"`
	CreatedAt          time.Time     `json:"created_at"`
	PeakConnections    int           `json:"peak_connections"`
	PeakConnectionTime time.Time     `json:"peak_connection_time"`
	MessageProcessTime time.Duration `json:"message_process_time"`
	ErrorCount         int64         `json:"error_count"`
}

// MessageStats represents message statistics
type MessageStats struct {
	MessageType        string        `json:"message_type"`
	TotalMessages      int64         `json:"total_messages"`
	TotalBytes         int64         `json:"total_bytes"`
	AverageSize        float64       `json:"average_size"`
	ProcessingTime     time.Duration `json:"processing_time"`
	AverageProcessTime time.Duration `json:"average_process_time"`
	ErrorCount         int64         `json:"error_count"`
	LastMessage        time.Time     `json:"last_message"`
	LargestMessage     int64         `json:"largest_message"`
	SmallestMessage    int64         `json:"smallest_message"`
	SlowMessageCount   int64         `json:"slow_message_count"`
	FilteredMessages   int64         `json:"filtered_messages"`
}

// PresenceStats represents presence statistics
type PresenceStats struct {
	TotalUsers        int           `json:"total_users"`
	OnlineUsers       int           `json:"online_users"`
	OfflineUsers      int           `json:"offline_users"`
	AwayUsers         int           `json:"away_users"`
	BusyUsers         int           `json:"busy_users"`
	TotalJoins        int64         `json:"total_joins"`
	TotalLeaves       int64         `json:"total_leaves"`
	TotalUpdates      int64         `json:"total_updates"`
	AverageOnlineTime time.Duration `json:"average_online_time"`
	LastActivity      time.Time     `json:"last_activity"`
	PeakOnlineUsers   int           `json:"peak_online_users"`
	PeakOnlineTime    time.Time     `json:"peak_online_time"`
}

// DefaultStreamingCollectorConfig returns default configuration
func DefaultStreamingCollectorConfig() *StreamingCollectorConfig {
	return &StreamingCollectorConfig{
		Interval:               time.Second * 10,
		CollectConnectionStats: true,
		CollectRoomStats:       true,
		CollectMessageStats:    true,
		CollectPresenceStats:   true,
		CollectProtocolStats:   true,
		TrackMessageTypes:      true,
		TrackUserActivity:      true,
		MaxConnectionHistory:   1000,
		SlowMessageThreshold:   time.Millisecond * 100,
	}
}

// NewStreamingCollector creates a new streaming collector
func NewStreamingCollector(streamingManager streaming.StreamingManager, logger logger.Logger) metrics.CustomCollector {
	return NewStreamingCollectorWithConfig(streamingManager, DefaultStreamingCollectorConfig(), logger)
}

// NewStreamingCollectorWithConfig creates a new streaming collector with configuration
func NewStreamingCollectorWithConfig(streamingManager streaming.StreamingManager, config *StreamingCollectorConfig, logger logger.Logger) metrics.CustomCollector {
	return &StreamingCollector{
		name:             "streaming",
		interval:         config.Interval,
		streamingManager: streamingManager,
		logger:           logger,
		metrics:          make(map[string]interface{}),
		enabled:          true,
		connectionStats:  make(map[string]*ConnectionStats),
		roomStats:        make(map[string]*RoomStats),
		messageStats:     make(map[string]*MessageStats),
		presenceStats:    make(map[string]*PresenceStats),
	}
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name
func (sc *StreamingCollector) Name() string {
	return sc.name
}

// Collect collects streaming metrics
func (sc *StreamingCollector) Collect() map[string]interface{} {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return sc.metrics
	}

	now := time.Now()

	// Only collect if enough time has passed
	if !sc.lastCollectionTime.IsZero() && now.Sub(sc.lastCollectionTime) < sc.interval {
		return sc.metrics
	}

	sc.lastCollectionTime = now

	// Clear previous metrics
	sc.metrics = make(map[string]interface{})

	// Collect connection statistics
	if err := sc.collectConnectionStats(); err != nil && sc.logger != nil {
		sc.logger.Error("failed to collect connection stats",
			logger.Error(err),
		)
	}

	// Collect room statistics
	if err := sc.collectRoomStats(); err != nil && sc.logger != nil {
		sc.logger.Error("failed to collect room stats",
			logger.Error(err),
		)
	}

	// Collect message statistics
	if err := sc.collectMessageStats(); err != nil && sc.logger != nil {
		sc.logger.Error("failed to collect message stats",
			logger.Error(err),
		)
	}

	// Collect presence statistics
	if err := sc.collectPresenceStats(); err != nil && sc.logger != nil {
		sc.logger.Error("failed to collect presence stats",
			logger.Error(err),
		)
	}

	// Collect protocol statistics
	if err := sc.collectProtocolStats(); err != nil && sc.logger != nil {
		sc.logger.Error("failed to collect protocol stats",
			logger.Error(err),
		)
	}

	return sc.metrics
}

// Reset resets the collector
func (sc *StreamingCollector) Reset() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.metrics = make(map[string]interface{})
	sc.connectionStats = make(map[string]*ConnectionStats)
	sc.roomStats = make(map[string]*RoomStats)
	sc.messageStats = make(map[string]*MessageStats)
	sc.presenceStats = make(map[string]*PresenceStats)
	sc.lastCollectionTime = time.Time{}
}

// =============================================================================
// CONNECTION STATISTICS COLLECTION
// =============================================================================

// collectConnectionStats collects connection statistics
func (sc *StreamingCollector) collectConnectionStats() error {
	if sc.streamingManager == nil {
		return fmt.Errorf("streaming manager not available")
	}

	// Get current streaming statistics
	stats := sc.streamingManager.GetStats()
	fmt.Printf("Streaming stats: %+v\n", stats)

	// Collect aggregate connection statistics
	totalConnections := int64(0)
	activeConnections := int64(0)
	totalErrors := int64(0)
	totalMessages := int64(0)
	totalBytes := int64(0)

	// Collect protocol-specific statistics
	protocols := map[string]*ConnectionStats{
		"websocket": {Protocol: "websocket"},
		"sse":       {Protocol: "sse"},
		"polling":   {Protocol: "polling"},
	}

	for protocolName, protocolStats := range protocols {
		// Get protocol-specific stats from streaming manager
		protocolData := sc.getProtocolStats(protocolName)
		if protocolData != nil {
			protocolStats.TotalConnections = protocolData.TotalConnections
			protocolStats.ActiveConnections = protocolData.ActiveConnections
			protocolStats.ConnectionErrors = protocolData.ConnectionErrors
			protocolStats.MessagesSent = protocolData.MessagesSent
			protocolStats.MessagesReceived = protocolData.MessagesReceived
			protocolStats.BytesSent = protocolData.BytesSent
			protocolStats.BytesReceived = protocolData.BytesReceived
		}

		sc.connectionStats[protocolName] = protocolStats

		// Add to aggregates
		totalConnections += protocolStats.TotalConnections
		activeConnections += protocolStats.ActiveConnections
		totalErrors += protocolStats.ConnectionErrors
		totalMessages += protocolStats.MessagesSent + protocolStats.MessagesReceived
		totalBytes += protocolStats.BytesSent + protocolStats.BytesReceived

		// Add metrics for this protocol
		prefix := fmt.Sprintf("streaming.connections.%s", protocolName)
		sc.metrics[prefix+".total"] = protocolStats.TotalConnections
		sc.metrics[prefix+".active"] = protocolStats.ActiveConnections
		sc.metrics[prefix+".errors"] = protocolStats.ConnectionErrors
		sc.metrics[prefix+".messages_sent"] = protocolStats.MessagesSent
		sc.metrics[prefix+".messages_received"] = protocolStats.MessagesReceived
		sc.metrics[prefix+".bytes_sent"] = protocolStats.BytesSent
		sc.metrics[prefix+".bytes_received"] = protocolStats.BytesReceived
		sc.metrics[prefix+".avg_session_time"] = protocolStats.AverageSessionTime.Seconds()
	}

	// Aggregate metrics
	sc.metrics["streaming.connections.total"] = totalConnections
	sc.metrics["streaming.connections.active"] = activeConnections
	sc.metrics["streaming.connections.errors"] = totalErrors
	sc.metrics["streaming.messages.total"] = totalMessages
	sc.metrics["streaming.bytes.total"] = totalBytes

	// Connection health metrics
	if totalConnections > 0 {
		errorRate := float64(totalErrors) / float64(totalConnections) * 100
		sc.metrics["streaming.connections.error_rate"] = errorRate
	}

	return nil
}

// getProtocolStats returns protocol-specific statistics (placeholder)
func (sc *StreamingCollector) getProtocolStats(protocol string) *ConnectionStats {
	// This would be implemented based on the actual streaming.Manager interface
	// For now, return placeholder data
	return &ConnectionStats{
		Protocol:           protocol,
		TotalConnections:   100,
		ActiveConnections:  50,
		ConnectionErrors:   2,
		MessagesSent:       1000,
		MessagesReceived:   950,
		BytesSent:          50000,
		BytesReceived:      48000,
		AverageSessionTime: time.Minute * 10,
	}
}

// RecordConnection records a connection event
func (sc *StreamingCollector) RecordConnection(protocol string, isConnect bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return
	}

	stats, exists := sc.connectionStats[protocol]
	if !exists {
		stats = &ConnectionStats{
			Protocol: protocol,
		}
		sc.connectionStats[protocol] = stats
	}

	if isConnect {
		stats.TotalConnections++
		stats.ActiveConnections++
		stats.LastConnection = time.Now()
	} else {
		stats.ActiveConnections--
		stats.LastDisconnection = time.Now()
	}
}

// RecordConnectionError records a connection error
func (sc *StreamingCollector) RecordConnectionError(protocol string, reason string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return
	}

	stats, exists := sc.connectionStats[protocol]
	if !exists {
		stats = &ConnectionStats{
			Protocol: protocol,
		}
		sc.connectionStats[protocol] = stats
	}

	stats.ConnectionErrors++
	stats.DisconnectionReason = reason
}

// =============================================================================
// ROOM STATISTICS COLLECTION
// =============================================================================

// collectRoomStats collects room statistics
func (sc *StreamingCollector) collectRoomStats() error {
	if sc.streamingManager == nil {
		return fmt.Errorf("streaming manager not available")
	}

	// Get all rooms from the streaming manager
	rooms := sc.getRooms()

	totalRooms := len(rooms)
	totalActiveConnections := 0
	totalMessages := int64(0)
	totalErrors := int64(0)

	for roomID, room := range rooms {
		if err := sc.collectRoomStatsForRoom(roomID, room); err != nil {
			if sc.logger != nil {
				sc.logger.Error("failed to collect room stats",
					logger.String("room_id", roomID),
					logger.Error(err),
				)
			}
			continue
		}

		// Add to aggregates
		if stats, exists := sc.roomStats[roomID]; exists {
			totalActiveConnections += stats.ActiveConnections
			totalMessages += stats.MessagesExchanged
			totalErrors += stats.ErrorCount
		}
	}

	// Aggregate metrics
	sc.metrics["streaming.rooms.total"] = totalRooms
	sc.metrics["streaming.rooms.active_connections"] = totalActiveConnections
	sc.metrics["streaming.rooms.messages_exchanged"] = totalMessages
	sc.metrics["streaming.rooms.errors"] = totalErrors

	// Room health metrics
	if totalRooms > 0 {
		avgConnectionsPerRoom := float64(totalActiveConnections) / float64(totalRooms)
		sc.metrics["streaming.rooms.avg_connections_per_room"] = avgConnectionsPerRoom
	}

	return nil
}

// getRooms returns all rooms (placeholder implementation)
func (sc *StreamingCollector) getRooms() map[string]interface{} {
	// This would be implemented based on the actual streaming.Manager interface
	return map[string]interface{}{
		"room1": nil,
		"room2": nil,
		"room3": nil,
	}
}

// collectRoomStatsForRoom collects statistics for a specific room
func (sc *StreamingCollector) collectRoomStatsForRoom(roomID string, room interface{}) error {
	// Get or create room stats
	stats, exists := sc.roomStats[roomID]
	if !exists {
		stats = &RoomStats{
			RoomID:    roomID,
			CreatedAt: time.Now(),
		}
		sc.roomStats[roomID] = stats
	}

	// Update room statistics
	stats.ActiveConnections = sc.getRoomConnectionCount(room)
	stats.LastActivity = time.Now()

	// Update peak connections
	if stats.ActiveConnections > stats.PeakConnections {
		stats.PeakConnections = stats.ActiveConnections
		stats.PeakConnectionTime = time.Now()
	}

	// Add metrics for this room
	prefix := fmt.Sprintf("streaming.rooms.%s", roomID)
	sc.metrics[prefix+".active_connections"] = stats.ActiveConnections
	sc.metrics[prefix+".total_joins"] = stats.TotalJoins
	sc.metrics[prefix+".total_leaves"] = stats.TotalLeaves
	sc.metrics[prefix+".messages_exchanged"] = stats.MessagesExchanged
	sc.metrics[prefix+".broadcast_messages"] = stats.BroadcastMessages
	sc.metrics[prefix+".direct_messages"] = stats.DirectMessages
	sc.metrics[prefix+".average_message_size"] = stats.AverageMessageSize
	sc.metrics[prefix+".peak_connections"] = stats.PeakConnections
	sc.metrics[prefix+".message_process_time"] = stats.MessageProcessTime.Seconds()
	sc.metrics[prefix+".error_count"] = stats.ErrorCount

	return nil
}

// getRoomConnectionCount returns the number of connections in a room (placeholder)
func (sc *StreamingCollector) getRoomConnectionCount(room interface{}) int {
	// This would be implemented based on the actual streaming.Room interface
	return 10
}

// RecordRoomJoin records a room join event
func (sc *StreamingCollector) RecordRoomJoin(roomID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return
	}

	stats, exists := sc.roomStats[roomID]
	if !exists {
		stats = &RoomStats{
			RoomID:    roomID,
			CreatedAt: time.Now(),
		}
		sc.roomStats[roomID] = stats
	}

	stats.TotalJoins++
	stats.ActiveConnections++
	stats.LastActivity = time.Now()
}

// RecordRoomLeave records a room leave event
func (sc *StreamingCollector) RecordRoomLeave(roomID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return
	}

	stats, exists := sc.roomStats[roomID]
	if !exists {
		return
	}

	stats.TotalLeaves++
	stats.ActiveConnections--
	stats.LastActivity = time.Now()
}

// RecordRoomMessage records a room message event
func (sc *StreamingCollector) RecordRoomMessage(roomID string, messageSize int, processingTime time.Duration, isBroadcast bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return
	}

	stats, exists := sc.roomStats[roomID]
	if !exists {
		stats = &RoomStats{
			RoomID:    roomID,
			CreatedAt: time.Now(),
		}
		sc.roomStats[roomID] = stats
	}

	stats.MessagesExchanged++
	stats.LastActivity = time.Now()
	stats.MessageProcessTime += processingTime

	if isBroadcast {
		stats.BroadcastMessages++
	} else {
		stats.DirectMessages++
	}

	// Update average message size
	if stats.MessagesExchanged > 0 {
		stats.AverageMessageSize = (stats.AverageMessageSize*float64(stats.MessagesExchanged-1) + float64(messageSize)) / float64(stats.MessagesExchanged)
	}
}

// =============================================================================
// MESSAGE STATISTICS COLLECTION
// =============================================================================

// collectMessageStats collects message statistics
func (sc *StreamingCollector) collectMessageStats() error {
	// Collect aggregate message statistics
	totalMessages := int64(0)
	totalBytes := int64(0)
	totalErrors := int64(0)
	totalProcessTime := time.Duration(0)

	for messageType, stats := range sc.messageStats {
		totalMessages += stats.TotalMessages
		totalBytes += stats.TotalBytes
		totalErrors += stats.ErrorCount
		totalProcessTime += stats.ProcessingTime

		// Add metrics for this message type
		prefix := fmt.Sprintf("streaming.messages.%s", messageType)
		sc.metrics[prefix+".total"] = stats.TotalMessages
		sc.metrics[prefix+".total_bytes"] = stats.TotalBytes
		sc.metrics[prefix+".average_size"] = stats.AverageSize
		sc.metrics[prefix+".processing_time"] = stats.ProcessingTime.Seconds()
		sc.metrics[prefix+".avg_process_time"] = stats.AverageProcessTime.Seconds()
		sc.metrics[prefix+".error_count"] = stats.ErrorCount
		sc.metrics[prefix+".largest_message"] = stats.LargestMessage
		sc.metrics[prefix+".smallest_message"] = stats.SmallestMessage
		sc.metrics[prefix+".slow_messages"] = stats.SlowMessageCount
		sc.metrics[prefix+".filtered_messages"] = stats.FilteredMessages

		// Calculate error rate
		if stats.TotalMessages > 0 {
			errorRate := float64(stats.ErrorCount) / float64(stats.TotalMessages) * 100
			sc.metrics[prefix+".error_rate"] = errorRate
		}
	}

	// Aggregate metrics
	sc.metrics["streaming.messages.total"] = totalMessages
	sc.metrics["streaming.messages.total_bytes"] = totalBytes
	sc.metrics["streaming.messages.errors"] = totalErrors
	sc.metrics["streaming.messages.total_processing_time"] = totalProcessTime.Seconds()

	// Message health metrics
	if totalMessages > 0 {
		avgProcessTime := totalProcessTime.Seconds() / float64(totalMessages)
		sc.metrics["streaming.messages.avg_processing_time"] = avgProcessTime

		errorRate := float64(totalErrors) / float64(totalMessages) * 100
		sc.metrics["streaming.messages.error_rate"] = errorRate
	}

	return nil
}

// RecordMessage records a message event
func (sc *StreamingCollector) RecordMessage(messageType string, messageSize int, processingTime time.Duration, err error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return
	}

	stats, exists := sc.messageStats[messageType]
	if !exists {
		stats = &MessageStats{
			MessageType:     messageType,
			SmallestMessage: int64(messageSize), // Initialize with first message size
		}
		sc.messageStats[messageType] = stats
	}

	stats.TotalMessages++
	stats.TotalBytes += int64(messageSize)
	stats.ProcessingTime += processingTime
	stats.LastMessage = time.Now()

	// Update average size
	stats.AverageSize = float64(stats.TotalBytes) / float64(stats.TotalMessages)

	// Update average processing time
	stats.AverageProcessTime = stats.ProcessingTime / time.Duration(stats.TotalMessages)

	// Update largest/smallest message
	if int64(messageSize) > stats.LargestMessage {
		stats.LargestMessage = int64(messageSize)
	}
	if int64(messageSize) < stats.SmallestMessage {
		stats.SmallestMessage = int64(messageSize)
	}

	// Record error if present
	if err != nil {
		stats.ErrorCount++
	}

	// Record slow message
	if processingTime > time.Millisecond*100 {
		stats.SlowMessageCount++
	}
}

// =============================================================================
// PRESENCE STATISTICS COLLECTION
// =============================================================================

// collectPresenceStats collects presence statistics
func (sc *StreamingCollector) collectPresenceStats() error {
	if sc.streamingManager == nil {
		return fmt.Errorf("streaming manager not available")
	}

	// Get presence data from streaming manager
	presenceData := sc.getPresenceData()

	// Update presence statistics
	stats := &PresenceStats{
		TotalUsers:        presenceData.TotalUsers,
		OnlineUsers:       presenceData.OnlineUsers,
		OfflineUsers:      presenceData.OfflineUsers,
		AwayUsers:         presenceData.AwayUsers,
		BusyUsers:         presenceData.BusyUsers,
		LastActivity:      time.Now(),
		AverageOnlineTime: presenceData.AverageOnlineTime,
	}

	// Update peak online users
	if stats.OnlineUsers > stats.PeakOnlineUsers {
		stats.PeakOnlineUsers = stats.OnlineUsers
		stats.PeakOnlineTime = time.Now()
	}

	sc.presenceStats["global"] = stats

	// Add metrics
	prefix := "streaming.presence"
	sc.metrics[prefix+".total_users"] = stats.TotalUsers
	sc.metrics[prefix+".online_users"] = stats.OnlineUsers
	sc.metrics[prefix+".offline_users"] = stats.OfflineUsers
	sc.metrics[prefix+".away_users"] = stats.AwayUsers
	sc.metrics[prefix+".busy_users"] = stats.BusyUsers
	sc.metrics[prefix+".total_joins"] = stats.TotalJoins
	sc.metrics[prefix+".total_leaves"] = stats.TotalLeaves
	sc.metrics[prefix+".total_updates"] = stats.TotalUpdates
	sc.metrics[prefix+".avg_online_time"] = stats.AverageOnlineTime.Seconds()
	sc.metrics[prefix+".peak_online_users"] = stats.PeakOnlineUsers

	// Presence health metrics
	if stats.TotalUsers > 0 {
		onlineRate := float64(stats.OnlineUsers) / float64(stats.TotalUsers) * 100
		sc.metrics[prefix+".online_rate"] = onlineRate
	}

	return nil
}

// getPresenceData returns presence data (placeholder implementation)
func (sc *StreamingCollector) getPresenceData() *PresenceStats {
	// This would be implemented based on the actual streaming.Manager interface
	return &PresenceStats{
		TotalUsers:        500,
		OnlineUsers:       350,
		OfflineUsers:      150,
		AwayUsers:         50,
		BusyUsers:         25,
		AverageOnlineTime: time.Minute * 45,
	}
}

// RecordPresenceJoin records a presence join event
func (sc *StreamingCollector) RecordPresenceJoin(userID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return
	}

	stats, exists := sc.presenceStats["global"]
	if !exists {
		stats = &PresenceStats{}
		sc.presenceStats["global"] = stats
	}

	stats.TotalJoins++
	stats.OnlineUsers++
	stats.LastActivity = time.Now()
}

// RecordPresenceLeave records a presence leave event
func (sc *StreamingCollector) RecordPresenceLeave(userID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.enabled {
		return
	}

	stats, exists := sc.presenceStats["global"]
	if !exists {
		return
	}

	stats.TotalLeaves++
	stats.OnlineUsers--
	stats.LastActivity = time.Now()
}

// =============================================================================
// PROTOCOL STATISTICS COLLECTION
// =============================================================================

// collectProtocolStats collects protocol-specific statistics
func (sc *StreamingCollector) collectProtocolStats() error {
	// Collect WebSocket statistics
	sc.metrics["streaming.protocols.websocket.supported"] = true
	sc.metrics["streaming.protocols.websocket.connections"] = sc.getProtocolConnectionCount("websocket")
	sc.metrics["streaming.protocols.websocket.upgrade_success_rate"] = sc.getProtocolUpgradeSuccessRate("websocket")

	// Collect SSE statistics
	sc.metrics["streaming.protocols.sse.supported"] = true
	sc.metrics["streaming.protocols.sse.connections"] = sc.getProtocolConnectionCount("sse")
	sc.metrics["streaming.protocols.sse.heartbeat_interval"] = sc.getSSEHeartbeatInterval()

	// Collect Long Polling statistics
	sc.metrics["streaming.protocols.polling.supported"] = true
	sc.metrics["streaming.protocols.polling.connections"] = sc.getProtocolConnectionCount("polling")
	sc.metrics["streaming.protocols.polling.timeout"] = sc.getPollingTimeout()

	return nil
}

// Protocol helper methods (placeholder implementations)
func (sc *StreamingCollector) getProtocolConnectionCount(protocol string) int {
	if stats, exists := sc.connectionStats[protocol]; exists {
		return int(stats.ActiveConnections)
	}
	return 0
}

func (sc *StreamingCollector) getProtocolUpgradeSuccessRate(protocol string) float64 {
	// This would be implemented based on actual protocol statistics
	return 95.0
}

func (sc *StreamingCollector) getSSEHeartbeatInterval() float64 {
	// This would be implemented based on actual SSE configuration
	return 30.0
}

func (sc *StreamingCollector) getPollingTimeout() float64 {
	// This would be implemented based on actual polling configuration
	return 30.0
}

// =============================================================================
// CONFIGURATION METHODS
// =============================================================================

// Enable enables the collector
func (sc *StreamingCollector) Enable() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.enabled = true
}

// Disable disables the collector
func (sc *StreamingCollector) Disable() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.enabled = false
}

// IsEnabled returns whether the collector is enabled
func (sc *StreamingCollector) IsEnabled() bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.enabled
}

// SetInterval sets the collection interval
func (sc *StreamingCollector) SetInterval(interval time.Duration) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.interval = interval
}

// GetInterval returns the collection interval
func (sc *StreamingCollector) GetInterval() time.Duration {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.interval
}

// GetStats returns collector statistics
func (sc *StreamingCollector) GetStats() map[string]interface{} {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["enabled"] = sc.enabled
	stats["interval"] = sc.interval
	stats["last_collection"] = sc.lastCollectionTime
	stats["protocols"] = len(sc.connectionStats)
	stats["rooms"] = len(sc.roomStats)
	stats["message_types"] = len(sc.messageStats)
	stats["total_connections"] = sc.getTotalConnections()
	stats["total_messages"] = sc.getTotalMessages()

	return stats
}

// getTotalConnections returns total connections across all protocols
func (sc *StreamingCollector) getTotalConnections() int64 {
	var total int64
	for _, stats := range sc.connectionStats {
		total += stats.ActiveConnections
	}
	return total
}

// getTotalMessages returns total messages across all types
func (sc *StreamingCollector) getTotalMessages() int64 {
	var total int64
	for _, stats := range sc.messageStats {
		total += stats.TotalMessages
	}
	return total
}

// =============================================================================
// METRICS INTEGRATION
// =============================================================================

// CreateMetricsWrapper creates a wrapper for metrics integration
func (sc *StreamingCollector) CreateMetricsWrapper(metricsCollector metrics.MetricsCollector) *StreamingMetricsWrapper {
	return &StreamingMetricsWrapper{
		collector:          sc,
		metricsCollector:   metricsCollector,
		activeConnections:  metricsCollector.Gauge("streaming_active_connections", "protocol"),
		messagesSent:       metricsCollector.Counter("streaming_messages_sent_total", "room_id", "message_type"),
		messageProcessTime: metricsCollector.Histogram("streaming_message_processing_duration_seconds", "message_type"),
		roomConnections:    metricsCollector.Gauge("streaming_room_connections", "room_id"),
		presenceUsers:      metricsCollector.Gauge("streaming_presence_users", "status"),
	}
}

// StreamingMetricsWrapper wraps the streaming collector with metrics integration
type StreamingMetricsWrapper struct {
	collector          *StreamingCollector
	metricsCollector   metrics.MetricsCollector
	activeConnections  metrics.Gauge
	messagesSent       metrics.Counter
	messageProcessTime metrics.Histogram
	roomConnections    metrics.Gauge
	presenceUsers      metrics.Gauge
}

// UpdateConnectionCount updates connection count metrics
func (smw *StreamingMetricsWrapper) UpdateConnectionCount(protocol string, count int) {
	smw.activeConnections.Set(float64(count))
}

// RecordMessageSent records message sent metrics
func (smw *StreamingMetricsWrapper) RecordMessageSent(roomID, messageType string, processingTime time.Duration) {
	smw.collector.RecordMessage(messageType, 0, processingTime, nil)
	smw.messagesSent.Inc()
	smw.messageProcessTime.Observe(processingTime.Seconds())
}

// UpdateRoomConnections updates room connection count
func (smw *StreamingMetricsWrapper) UpdateRoomConnections(roomID string, count int) {
	smw.roomConnections.Set(float64(count))
}

// UpdatePresenceUsers updates presence user count
func (smw *StreamingMetricsWrapper) UpdatePresenceUsers(status string, count int) {
	smw.presenceUsers.Set(float64(count))
}
