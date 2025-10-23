package persistence

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	streaming "github.com/xraph/forge/v0/pkg/streaming/core"
)

// ReplayStrategy defines how messages should be replayed
type ReplayStrategy string

const (
	ReplayStrategyAll       ReplayStrategy = "all"        // Replay all available messages
	ReplayStrategyRecent    ReplayStrategy = "recent"     // Replay recent messages only
	ReplayStrategyTimeRange ReplayStrategy = "time_range" // Replay messages in time range
	ReplayStrategyCount     ReplayStrategy = "count"      // Replay last N messages
	ReplayStrategySince     ReplayStrategy = "since"      // Replay messages since timestamp
)

// ReplayRequest represents a request to replay messages
type ReplayRequest struct {
	RoomID          string                 `json:"room_id"`
	UserID          string                 `json:"user_id,omitempty"`
	ConnectionID    string                 `json:"connection_id,omitempty"`
	Strategy        ReplayStrategy         `json:"strategy"`
	Parameters      map[string]interface{} `json:"parameters"`
	Filter          *MessageFilter         `json:"filter,omitempty"`
	RealTime        bool                   `json:"real_time"`                  // If true, replay at original intervals
	SpeedMultiplier float64                `json:"speed_multiplier,omitempty"` // Speed up/slow down replay
}

// ReplayResponse represents the result of a replay operation
type ReplayResponse struct {
	RequestID    string            `json:"request_id"`
	Status       ReplayStatus      `json:"status"`
	MessageCount int               `json:"message_count"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	Duration     time.Duration     `json:"duration"`
	Error        string            `json:"error,omitempty"`
	Statistics   *ReplayStatistics `json:"statistics,omitempty"`
}

// ReplayStatus represents the status of a replay operation
type ReplayStatus string

const (
	ReplayStatusPending    ReplayStatus = "pending"
	ReplayStatusInProgress ReplayStatus = "in_progress"
	ReplayStatusCompleted  ReplayStatus = "completed"
	ReplayStatusFailed     ReplayStatus = "failed"
	ReplayStatusCancelled  ReplayStatus = "cancelled"
)

// ReplayStatistics contains statistics about the replay operation
type ReplayStatistics struct {
	TotalMessages     int           `json:"total_messages"`
	ProcessedMessages int           `json:"processed_messages"`
	FailedMessages    int           `json:"failed_messages"`
	AverageLatency    time.Duration `json:"average_latency"`
	BytesTransferred  int64         `json:"bytes_transferred"`
	StartedAt         time.Time     `json:"started_at"`
	CompletedAt       time.Time     `json:"completed_at"`
}

// ReplayProgressCallback is called during replay to report progress
type ReplayProgressCallback func(progress ReplayProgress)

// ReplayProgress represents progress of an ongoing replay
type ReplayProgress struct {
	RequestID         string        `json:"request_id"`
	ProcessedMessages int           `json:"processed_messages"`
	TotalMessages     int           `json:"total_messages"`
	CurrentMessage    int           `json:"current_message"`
	Progress          float64       `json:"progress"` // 0.0 to 1.0
	EstimatedTimeLeft time.Duration `json:"estimated_time_left"`
}

// MessageReplayer handles message replay operations
type MessageReplayer interface {
	// Replay messages based on request
	ReplayMessages(ctx context.Context, request *ReplayRequest) (*ReplayResponse, error)

	// Stream replay with progress callback
	StreamReplay(ctx context.Context, request *ReplayRequest, progressCallback ReplayProgressCallback) (*ReplayResponse, error)

	// Get replay status
	GetReplayStatus(requestID string) (*ReplayResponse, error)

	// Cancel ongoing replay
	CancelReplay(requestID string) error

	// Get active replays
	GetActiveReplays() []*ReplayResponse

	// Historical data queries
	GetMessageRange(ctx context.Context, roomID string, start, end time.Time) ([]*streaming.Message, error)
	GetRecentMessages(ctx context.Context, roomID string, count int) ([]*streaming.Message, error)
	GetMessagesSince(ctx context.Context, roomID string, since time.Time) ([]*streaming.Message, error)

	// Statistics
	GetReplayStatistics() ReplayServiceStatistics
}

// ReplayServiceStatistics contains statistics about the replay service
type ReplayServiceStatistics struct {
	TotalReplays      int64         `json:"total_replays"`
	ActiveReplays     int           `json:"active_replays"`
	CompletedReplays  int64         `json:"completed_replays"`
	FailedReplays     int64         `json:"failed_replays"`
	TotalMessages     int64         `json:"total_messages_replayed"`
	AverageReplayTime time.Duration `json:"average_replay_time"`
	LastReplay        time.Time     `json:"last_replay"`
}

// DefaultMessageReplayer implements MessageReplayer
type DefaultMessageReplayer struct {
	store            MessageStore
	streamingManager streaming.StreamingManager
	config           ReplayConfig
	logger           common.Logger
	metrics          common.Metrics

	// Active replays tracking
	activeReplays map[string]*activeReplay
	replaysMu     sync.RWMutex

	// Statistics
	stats   ReplayServiceStatistics
	statsMu sync.RWMutex
}

// activeReplay tracks an ongoing replay operation
type activeReplay struct {
	request   *ReplayRequest
	response  *ReplayResponse
	cancelFn  context.CancelFunc
	startedAt time.Time
	messages  []*streaming.Message
	progress  ReplayProgress
	callback  ReplayProgressCallback
	mu        sync.RWMutex
}

// ReplayConfig contains configuration for the replay service
type ReplayConfig struct {
	MaxConcurrentReplays int           `yaml:"max_concurrent_replays" default:"10"`
	DefaultPageSize      int           `yaml:"default_page_size" default:"100"`
	MaxReplayMessages    int           `yaml:"max_replay_messages" default:"10000"`
	ReplayTimeout        time.Duration `yaml:"replay_timeout" default:"5m"`
	EnableRealTimeReplay bool          `yaml:"enable_real_time_replay" default:"true"`
	BufferSize           int           `yaml:"buffer_size" default:"1000"`
	ProgressInterval     time.Duration `yaml:"progress_interval" default:"1s"`
}

// DefaultReplayConfig returns default replay configuration
func DefaultReplayConfig() ReplayConfig {
	return ReplayConfig{
		MaxConcurrentReplays: 10,
		DefaultPageSize:      100,
		MaxReplayMessages:    10000,
		ReplayTimeout:        5 * time.Minute,
		EnableRealTimeReplay: true,
		BufferSize:           1000,
		ProgressInterval:     1 * time.Second,
	}
}

// NewDefaultMessageReplayer creates a new default message replayer
func NewDefaultMessageReplayer(
	store MessageStore,
	streamingManager streaming.StreamingManager,
	config ReplayConfig,
	logger common.Logger,
	metrics common.Metrics,
) MessageReplayer {
	return &DefaultMessageReplayer{
		store:            store,
		streamingManager: streamingManager,
		config:           config,
		logger:           logger,
		metrics:          metrics,
		activeReplays:    make(map[string]*activeReplay),
	}
}

// ReplayMessages replays messages based on the request
func (r *DefaultMessageReplayer) ReplayMessages(ctx context.Context, request *ReplayRequest) (*ReplayResponse, error) {
	// Validate request
	if err := r.validateReplayRequest(request); err != nil {
		return nil, NewMessageStoreError("validate", "replay", err)
	}

	// Check concurrent replay limit
	if r.getActiveReplayCount() >= r.config.MaxConcurrentReplays {
		return nil, NewMessageStoreError("limit", "replay", fmt.Errorf("maximum concurrent replays reached: %d", r.config.MaxConcurrentReplays))
	}

	// Generate request ID
	requestID := generateUniqueID("replay")

	// Create response
	response := &ReplayResponse{
		RequestID: requestID,
		Status:    ReplayStatusPending,
		StartTime: time.Now(),
	}

	// Create context with timeout
	replayCtx, cancel := context.WithTimeout(ctx, r.config.ReplayTimeout)

	// Create active replay tracking
	activeReplay := &activeReplay{
		request:   request,
		response:  response,
		cancelFn:  cancel,
		startedAt: time.Now(),
		progress: ReplayProgress{
			RequestID: requestID,
		},
	}

	// Store active replay
	r.replaysMu.Lock()
	r.activeReplays[requestID] = activeReplay
	r.replaysMu.Unlock()

	// Start replay in background
	go r.executeReplay(replayCtx, activeReplay)

	// Update statistics
	r.updateStats(func(stats *ReplayServiceStatistics) {
		stats.TotalReplays++
		stats.LastReplay = time.Now()
	})

	if r.logger != nil {
		r.logger.Info("replay started",
			logger.String("request_id", requestID),
			logger.String("room_id", request.RoomID),
			logger.String("strategy", string(request.Strategy)),
			logger.String("user_id", request.UserID),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.replay.started").Inc()
	}

	return response, nil
}

// StreamReplay replays messages with progress updates
func (r *DefaultMessageReplayer) StreamReplay(ctx context.Context, request *ReplayRequest, progressCallback ReplayProgressCallback) (*ReplayResponse, error) {
	// Set progress callback
	response, err := r.ReplayMessages(ctx, request)
	if err != nil {
		return nil, err
	}

	// Update active replay with callback
	r.replaysMu.Lock()
	if activeReplay, exists := r.activeReplays[response.RequestID]; exists {
		activeReplay.callback = progressCallback
	}
	r.replaysMu.Unlock()

	return response, nil
}

// executeReplay executes the actual replay operation
func (r *DefaultMessageReplayer) executeReplay(ctx context.Context, activeReplay *activeReplay) {
	defer func() {
		// Clean up active replay
		r.replaysMu.Lock()
		delete(r.activeReplays, activeReplay.response.RequestID)
		r.replaysMu.Unlock()

		// Cancel context
		activeReplay.cancelFn()
	}()

	// Update status to in progress
	activeReplay.mu.Lock()
	activeReplay.response.Status = ReplayStatusInProgress
	activeReplay.mu.Unlock()

	// Fetch messages based on strategy
	messages, err := r.fetchMessagesForReplay(ctx, activeReplay.request)
	if err != nil {
		r.completeReplayWithError(activeReplay, err)
		return
	}

	// Store messages in active replay
	activeReplay.mu.Lock()
	activeReplay.messages = messages
	activeReplay.progress.TotalMessages = len(messages)
	activeReplay.mu.Unlock()

	if len(messages) == 0 {
		r.completeReplay(activeReplay, &ReplayStatistics{
			StartedAt:   activeReplay.startedAt,
			CompletedAt: time.Now(),
		})
		return
	}

	// Execute replay based on real-time setting
	var stats *ReplayStatistics
	if activeReplay.request.RealTime {
		stats = r.executeRealTimeReplay(ctx, activeReplay, messages)
	} else {
		stats = r.executeBatchReplay(ctx, activeReplay, messages)
	}

	// Complete replay
	r.completeReplay(activeReplay, stats)
}

// fetchMessagesForReplay fetches messages based on the replay strategy
func (r *DefaultMessageReplayer) fetchMessagesForReplay(ctx context.Context, request *ReplayRequest) ([]*streaming.Message, error) {
	switch request.Strategy {
	case ReplayStrategyAll:
		return r.store.GetRoomMessages(ctx, request.RoomID, request.Filter)

	case ReplayStrategyRecent:
		count := r.config.DefaultPageSize
		if c, ok := request.Parameters["count"].(int); ok {
			count = c
		}
		return r.GetRecentMessages(ctx, request.RoomID, count)

	case ReplayStrategyTimeRange:
		start, ok1 := request.Parameters["start"].(time.Time)
		end, ok2 := request.Parameters["end"].(time.Time)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("time_range strategy requires 'start' and 'end' parameters")
		}
		return r.store.GetMessagesByTimeRange(ctx, request.RoomID, start, end)

	case ReplayStrategyCount:
		count, ok := request.Parameters["count"].(int)
		if !ok {
			return nil, fmt.Errorf("count strategy requires 'count' parameter")
		}
		return r.GetRecentMessages(ctx, request.RoomID, count)

	case ReplayStrategySince:
		since, ok := request.Parameters["since"].(time.Time)
		if !ok {
			return nil, fmt.Errorf("since strategy requires 'since' parameter")
		}
		return r.GetMessagesSince(ctx, request.RoomID, since)

	default:
		return nil, fmt.Errorf("unsupported replay strategy: %s", request.Strategy)
	}
}

// executeBatchReplay sends all messages as quickly as possible
func (r *DefaultMessageReplayer) executeBatchReplay(ctx context.Context, activeReplay *activeReplay, messages []*streaming.Message) *ReplayStatistics {
	stats := &ReplayStatistics{
		TotalMessages: len(messages),
		StartedAt:     time.Now(),
	}

	var totalLatency time.Duration
	progressTicker := time.NewTicker(r.config.ProgressInterval)
	defer progressTicker.Stop()

	for i, message := range messages {
		select {
		case <-ctx.Done():
			stats.CompletedAt = time.Now()
			return stats
		default:
		}

		// Send message
		start := time.Now()
		err := r.sendReplayMessage(ctx, activeReplay.request, message)
		latency := time.Since(start)

		stats.ProcessedMessages++
		if err != nil {
			stats.FailedMessages++
			if r.logger != nil {
				r.logger.Warn("failed to send replay message",
					logger.String("message_id", message.ID),
					logger.Error(err),
				)
			}
		} else {
			totalLatency += latency
			if messageSize := r.estimateMessageSize(message); messageSize > 0 {
				stats.BytesTransferred += int64(messageSize)
			}
		}

		// Update progress
		select {
		case <-progressTicker.C:
			r.updateReplayProgress(activeReplay, i+1, len(messages))
		default:
		}
	}

	stats.CompletedAt = time.Now()
	if stats.ProcessedMessages > 0 {
		stats.AverageLatency = totalLatency / time.Duration(stats.ProcessedMessages)
	}

	return stats
}

// executeRealTimeReplay replays messages with original timing
func (r *DefaultMessageReplayer) executeRealTimeReplay(ctx context.Context, activeReplay *activeReplay, messages []*streaming.Message) *ReplayStatistics {
	stats := &ReplayStatistics{
		TotalMessages: len(messages),
		StartedAt:     time.Now(),
	}

	if len(messages) == 0 {
		stats.CompletedAt = time.Now()
		return stats
	}

	// Sort messages by timestamp
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	baseTime := messages[0].Timestamp
	startTime := time.Now()
	speedMultiplier := activeReplay.request.SpeedMultiplier
	if speedMultiplier <= 0 {
		speedMultiplier = 1.0
	}

	var totalLatency time.Duration
	progressTicker := time.NewTicker(r.config.ProgressInterval)
	defer progressTicker.Stop()

	for i, message := range messages {
		select {
		case <-ctx.Done():
			stats.CompletedAt = time.Now()
			return stats
		default:
		}

		// Calculate when to send this message
		originalDelay := message.Timestamp.Sub(baseTime)
		adjustedDelay := time.Duration(float64(originalDelay) / speedMultiplier)
		sendTime := startTime.Add(adjustedDelay)

		// Wait until it's time to send
		if waitTime := time.Until(sendTime); waitTime > 0 {
			select {
			case <-time.After(waitTime):
			case <-ctx.Done():
				stats.CompletedAt = time.Now()
				return stats
			}
		}

		// Send message
		start := time.Now()
		err := r.sendReplayMessage(ctx, activeReplay.request, message)
		latency := time.Since(start)

		stats.ProcessedMessages++
		if err != nil {
			stats.FailedMessages++
			if r.logger != nil {
				r.logger.Warn("failed to send real-time replay message",
					logger.String("message_id", message.ID),
					logger.Error(err),
				)
			}
		} else {
			totalLatency += latency
			if messageSize := r.estimateMessageSize(message); messageSize > 0 {
				stats.BytesTransferred += int64(messageSize)
			}
		}

		// Update progress
		select {
		case <-progressTicker.C:
			r.updateReplayProgress(activeReplay, i+1, len(messages))
		default:
		}
	}

	stats.CompletedAt = time.Now()
	if stats.ProcessedMessages > 0 {
		stats.AverageLatency = totalLatency / time.Duration(stats.ProcessedMessages)
	}

	return stats
}

// sendReplayMessage sends a single message during replay
func (r *DefaultMessageReplayer) sendReplayMessage(ctx context.Context, request *ReplayRequest, message *streaming.Message) error {
	// Create a copy of the message with replay metadata
	replayMessage := &streaming.Message{
		ID:        message.ID,
		Type:      message.Type,
		From:      message.From,
		To:        message.To,
		RoomID:    message.RoomID,
		Data:      message.Data,
		Metadata:  make(map[string]interface{}),
		Timestamp: message.Timestamp,
		TTL:       message.TTL,
	}

	// Copy original metadata
	for k, v := range message.Metadata {
		replayMessage.Metadata[k] = v
	}

	// Add replay metadata
	replayMessage.Metadata["replay"] = true
	replayMessage.Metadata["replay_request_id"] = request.RoomID
	replayMessage.Metadata["original_timestamp"] = message.Timestamp
	replayMessage.Metadata["replay_timestamp"] = time.Now()

	// Send to specific connection, user, or room
	if request.ConnectionID != "" {
		return r.streamingManager.SendToConnection(ctx, request.ConnectionID, replayMessage)
	} else if request.UserID != "" {
		return r.streamingManager.BroadcastToUser(ctx, request.UserID, replayMessage)
	} else {
		return r.streamingManager.BroadcastToRoom(ctx, request.RoomID, replayMessage)
	}
}

// updateReplayProgress updates the progress of an active replay
func (r *DefaultMessageReplayer) updateReplayProgress(activeReplay *activeReplay, processed, total int) {
	activeReplay.mu.Lock()
	activeReplay.progress.ProcessedMessages = processed
	activeReplay.progress.TotalMessages = total
	activeReplay.progress.CurrentMessage = processed
	activeReplay.progress.Progress = float64(processed) / float64(total)

	// Estimate time left
	elapsed := time.Since(activeReplay.startedAt)
	if processed > 0 {
		estimatedTotal := time.Duration(float64(elapsed) * float64(total) / float64(processed))
		activeReplay.progress.EstimatedTimeLeft = estimatedTotal - elapsed
	}

	callback := activeReplay.callback
	progress := activeReplay.progress
	activeReplay.mu.Unlock()

	// Call progress callback if set
	if callback != nil {
		go callback(progress)
	}
}

// completeReplay marks a replay as completed
func (r *DefaultMessageReplayer) completeReplay(activeReplay *activeReplay, stats *ReplayStatistics) {
	activeReplay.mu.Lock()
	activeReplay.response.Status = ReplayStatusCompleted
	activeReplay.response.EndTime = time.Now()
	activeReplay.response.Duration = time.Since(activeReplay.startedAt)
	activeReplay.response.MessageCount = stats.ProcessedMessages
	activeReplay.response.Statistics = stats
	activeReplay.mu.Unlock()

	// Update service statistics
	r.updateStats(func(serviceStats *ReplayServiceStatistics) {
		serviceStats.CompletedReplays++
		serviceStats.TotalMessages += int64(stats.ProcessedMessages)
		if serviceStats.CompletedReplays > 0 {
			totalDuration := time.Duration(serviceStats.CompletedReplays) * serviceStats.AverageReplayTime
			totalDuration += activeReplay.response.Duration
			serviceStats.AverageReplayTime = totalDuration / time.Duration(serviceStats.CompletedReplays)
		} else {
			serviceStats.AverageReplayTime = activeReplay.response.Duration
		}
	})

	if r.logger != nil {
		r.logger.Info("replay completed",
			logger.String("request_id", activeReplay.response.RequestID),
			logger.Int("messages", stats.ProcessedMessages),
			logger.Duration("duration", activeReplay.response.Duration),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.replay.completed").Inc()
		r.metrics.Histogram("streaming.replay.duration").Observe(activeReplay.response.Duration.Seconds())
		r.metrics.Histogram("streaming.replay.message_count").Observe(float64(stats.ProcessedMessages))
	}
}

// completeReplayWithError marks a replay as failed
func (r *DefaultMessageReplayer) completeReplayWithError(activeReplay *activeReplay, err error) {
	activeReplay.mu.Lock()
	activeReplay.response.Status = ReplayStatusFailed
	activeReplay.response.EndTime = time.Now()
	activeReplay.response.Duration = time.Since(activeReplay.startedAt)
	activeReplay.response.Error = err.Error()
	activeReplay.mu.Unlock()

	// Update service statistics
	r.updateStats(func(stats *ReplayServiceStatistics) {
		stats.FailedReplays++
	})

	if r.logger != nil {
		r.logger.Error("replay failed",
			logger.String("request_id", activeReplay.response.RequestID),
			logger.Error(err),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.replay.failed").Inc()
	}
}

// GetReplayStatus returns the status of a replay
func (r *DefaultMessageReplayer) GetReplayStatus(requestID string) (*ReplayResponse, error) {
	r.replaysMu.RLock()
	activeReplay, exists := r.activeReplays[requestID]
	r.replaysMu.RUnlock()

	if !exists {
		return nil, common.ErrServiceNotFound(requestID)
	}

	activeReplay.mu.RLock()
	defer activeReplay.mu.RUnlock()

	// Return a copy to avoid race conditions
	response := *activeReplay.response
	return &response, nil
}

// CancelReplay cancels an ongoing replay
func (r *DefaultMessageReplayer) CancelReplay(requestID string) error {
	r.replaysMu.Lock()
	activeReplay, exists := r.activeReplays[requestID]
	if exists {
		delete(r.activeReplays, requestID)
	}
	r.replaysMu.Unlock()

	if !exists {
		return common.ErrServiceNotFound(requestID)
	}

	// Cancel the context
	activeReplay.cancelFn()

	// Mark as cancelled
	activeReplay.mu.Lock()
	activeReplay.response.Status = ReplayStatusCancelled
	activeReplay.response.EndTime = time.Now()
	activeReplay.response.Duration = time.Since(activeReplay.startedAt)
	activeReplay.mu.Unlock()

	if r.logger != nil {
		r.logger.Info("replay cancelled", logger.String("request_id", requestID))
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.replay.cancelled").Inc()
	}

	return nil
}

// GetActiveReplays returns all active replays
func (r *DefaultMessageReplayer) GetActiveReplays() []*ReplayResponse {
	r.replaysMu.RLock()
	defer r.replaysMu.RUnlock()

	responses := make([]*ReplayResponse, 0, len(r.activeReplays))
	for _, activeReplay := range r.activeReplays {
		activeReplay.mu.RLock()
		response := *activeReplay.response
		activeReplay.mu.RUnlock()
		responses = append(responses, &response)
	}

	return responses
}

// GetMessageRange retrieves messages in a time range
func (r *DefaultMessageReplayer) GetMessageRange(ctx context.Context, roomID string, start, end time.Time) ([]*streaming.Message, error) {
	return r.store.GetMessagesByTimeRange(ctx, roomID, start, end)
}

// GetRecentMessages retrieves the most recent messages
func (r *DefaultMessageReplayer) GetRecentMessages(ctx context.Context, roomID string, count int) ([]*streaming.Message, error) {
	if count > r.config.MaxReplayMessages {
		count = r.config.MaxReplayMessages
	}

	filter := &MessageFilter{
		Limit: count,
	}

	return r.store.GetRoomMessages(ctx, roomID, filter)
}

// GetMessagesSince retrieves messages since a timestamp
func (r *DefaultMessageReplayer) GetMessagesSince(ctx context.Context, roomID string, since time.Time) ([]*streaming.Message, error) {
	filter := &MessageFilter{
		Since: &since,
		Limit: r.config.MaxReplayMessages,
	}

	return r.store.GetRoomMessages(ctx, roomID, filter)
}

// GetReplayStatistics returns replay service statistics
func (r *DefaultMessageReplayer) GetReplayStatistics() ReplayServiceStatistics {
	r.statsMu.RLock()
	defer r.statsMu.RUnlock()

	stats := r.stats
	stats.ActiveReplays = r.getActiveReplayCount()
	return stats
}

// Helper methods

// validateReplayRequest validates a replay request
func (r *DefaultMessageReplayer) validateReplayRequest(request *ReplayRequest) error {
	if request.RoomID == "" {
		return fmt.Errorf("room_id is required")
	}

	if !request.Strategy.IsValid() {
		return fmt.Errorf("invalid replay strategy: %s", request.Strategy)
	}

	// Validate strategy-specific parameters
	switch request.Strategy {
	case ReplayStrategyTimeRange:
		if _, ok1 := request.Parameters["start"]; !ok1 {
			return fmt.Errorf("time_range strategy requires 'start' parameter")
		}
		if _, ok2 := request.Parameters["end"]; !ok2 {
			return fmt.Errorf("time_range strategy requires 'end' parameter")
		}
	case ReplayStrategyCount:
		if count, ok := request.Parameters["count"].(int); !ok || count <= 0 {
			return fmt.Errorf("count strategy requires positive 'count' parameter")
		}
	case ReplayStrategySince:
		if _, ok := request.Parameters["since"]; !ok {
			return fmt.Errorf("since strategy requires 'since' parameter")
		}
	}

	return nil
}

// getActiveReplayCount returns the number of active replays
func (r *DefaultMessageReplayer) getActiveReplayCount() int {
	r.replaysMu.RLock()
	defer r.replaysMu.RUnlock()
	return len(r.activeReplays)
}

// updateStats safely updates service statistics
func (r *DefaultMessageReplayer) updateStats(updater func(*ReplayServiceStatistics)) {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	updater(&r.stats)
}

// estimateMessageSize estimates the size of a message in bytes
func (r *DefaultMessageReplayer) estimateMessageSize(message *streaming.Message) int {
	// This is a rough estimation - in practice, you might want to serialize the message
	size := len(message.ID) + len(message.From) + len(message.To) + len(message.RoomID)

	// Estimate data size (rough approximation)
	if message.Data != nil {
		// This is very rough - ideally serialize to get accurate size
		size += 100 // rough estimate for data
	}

	// Estimate metadata size
	size += len(message.Metadata) * 50 // rough estimate per metadata entry

	return size
}

// ReplayStrategy helpers

// IsValid checks if the replay strategy is valid
func (rs ReplayStrategy) IsValid() bool {
	switch rs {
	case ReplayStrategyAll, ReplayStrategyRecent, ReplayStrategyTimeRange, ReplayStrategyCount, ReplayStrategySince:
		return true
	default:
		return false
	}
}

// String returns the string representation of the replay strategy
func (rs ReplayStrategy) String() string {
	return string(rs)
}

// ReplayStatus helpers

// IsTerminal returns true if the replay status is terminal
func (rs ReplayStatus) IsTerminal() bool {
	return rs == ReplayStatusCompleted || rs == ReplayStatusFailed || rs == ReplayStatusCancelled
}

// IsActive returns true if the replay is currently active
func (rs ReplayStatus) IsActive() bool {
	return rs == ReplayStatusPending || rs == ReplayStatusInProgress
}

// String returns the string representation of the replay status
func (rs ReplayStatus) String() string {
	return string(rs)
}

func generateUniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixNano(), uuid.New().String())
}
