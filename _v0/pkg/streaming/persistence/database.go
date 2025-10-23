package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	streaming "github.com/xraph/forge/v0/pkg/streaming/core"
	"gorm.io/gorm"
)

// DatabaseMessageStore implements MessageStore using a SQL database
type DatabaseMessageStore struct {
	db            *gorm.DB
	config        MessageStoreConfig
	logger        common.Logger
	metrics       common.Metrics
	cache         map[string]*streaming.Message
	cacheMu       sync.RWMutex
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
}

// MessageRecord represents a message record in the database
type MessageRecord struct {
	ID        string     `gorm:"primaryKey;type:varchar(255)" json:"id"`
	Type      string     `gorm:"type:varchar(50);index" json:"type"`
	From      string     `gorm:"type:varchar(255);index" json:"from"`
	To        string     `gorm:"type:varchar(255);index" json:"to"`
	RoomID    string     `gorm:"type:varchar(255);index" json:"room_id"`
	Data      string     `gorm:"type:text" json:"data"`
	Metadata  string     `gorm:"type:text" json:"metadata"`
	Timestamp time.Time  `gorm:"index" json:"timestamp"`
	TTL       int64      `gorm:"default:0" json:"ttl"` // TTL in seconds
	StoredAt  time.Time  `gorm:"autoCreateTime" json:"stored_at"`
	Size      int        `gorm:"default:0" json:"size"`
	Checksum  string     `gorm:"type:varchar(64)" json:"checksum"`
	ExpiresAt *time.Time `gorm:"index" json:"expires_at"`
}

// TableName returns the table name for MessageRecord
func (MessageRecord) TableName() string {
	return "streaming_messages"
}

// NewDatabaseMessageStore creates a new database message store
func NewDatabaseMessageStore(
	db *gorm.DB,
	config MessageStoreConfig,
	logger common.Logger,
	metrics common.Metrics,
) (MessageStore, error) {
	store := &DatabaseMessageStore{
		db:      db,
		config:  config,
		logger:  logger,
		metrics: metrics,
		cache:   make(map[string]*streaming.Message),
		stopCh:  make(chan struct{}),
	}

	// Migrate the schema
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database schema: %w", err)
	}

	// Start cleanup routine
	store.startCleanup()

	return store, nil
}

// migrate creates or updates the database schema
func (s *DatabaseMessageStore) migrate() error {
	if err := s.db.AutoMigrate(&MessageRecord{}); err != nil {
		return err
	}

	// Create indexes for better query performance
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_messages_room_timestamp ON streaming_messages(room_id, timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_messages_from_timestamp ON streaming_messages(from_user, timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_messages_type_timestamp ON streaming_messages(type, timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_messages_expires_at ON streaming_messages(expires_at) WHERE expires_at IS NOT NULL",
	}

	for _, indexSQL := range indexes {
		if err := s.db.Exec(indexSQL).Error; err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to create index",
					logger.String("sql", indexSQL),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

// StoreMessage stores a message in the database
func (s *DatabaseMessageStore) StoreMessage(ctx context.Context, message *streaming.Message) error {
	record, err := s.messageToRecord(message)
	if err != nil {
		return NewMessageStoreError("store", "database", err)
	}

	// Store in database
	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		if s.metrics != nil {
			s.metrics.Counter("streaming.persistence.store_errors").Inc()
		}
		return NewMessageStoreError("store", "database", err)
	}

	// Cache the message if caching is enabled
	if s.config.EnableCache {
		s.setCachedMessage(message.ID, message)
	}

	// Update metrics
	if s.metrics != nil {
		s.metrics.Counter("streaming.persistence.messages_stored").Inc()
		s.metrics.Histogram("streaming.persistence.message_size").Observe(float64(record.Size))
	}

	if s.logger != nil {
		s.logger.Debug("message stored",
			logger.String("message_id", message.ID),
			logger.String("room_id", message.RoomID),
			logger.String("type", string(message.Type)),
			logger.Int("size", record.Size),
		)
	}

	return nil
}

// GetMessage retrieves a message by ID
func (s *DatabaseMessageStore) GetMessage(ctx context.Context, messageID string) (*streaming.Message, error) {
	// Check cache first
	if s.config.EnableCache {
		if cached := s.getCachedMessage(messageID); cached != nil {
			if s.metrics != nil {
				s.metrics.Counter("streaming.persistence.cache_hits").Inc()
			}
			return cached, nil
		}
	}

	// Query database
	var record MessageRecord
	if err := s.db.WithContext(ctx).Where("id = ?", messageID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, common.ErrServiceNotFound(messageID)
		}
		return nil, NewMessageStoreError("get", "database", err)
	}

	message, err := s.recordToMessage(&record)
	if err != nil {
		return nil, NewMessageStoreError("deserialize", "database", err)
	}

	// Cache the result
	if s.config.EnableCache {
		s.setCachedMessage(messageID, message)
		if s.metrics != nil {
			s.metrics.Counter("streaming.persistence.cache_misses").Inc()
		}
	}

	return message, nil
}

// DeleteMessage deletes a message by ID
func (s *DatabaseMessageStore) DeleteMessage(ctx context.Context, messageID string) error {
	result := s.db.WithContext(ctx).Where("id = ?", messageID).Delete(&MessageRecord{})
	if result.Error != nil {
		return NewMessageStoreError("delete", "database", result.Error)
	}

	if result.RowsAffected == 0 {
		return common.ErrServiceNotFound(messageID)
	}

	// Remove from cache
	if s.config.EnableCache {
		s.removeCachedMessage(messageID)
	}

	if s.metrics != nil {
		s.metrics.Counter("streaming.persistence.messages_deleted").Inc()
	}

	return nil
}

// GetRoomMessages retrieves messages for a specific room
func (s *DatabaseMessageStore) GetRoomMessages(ctx context.Context, roomID string, filter *MessageFilter) ([]*streaming.Message, error) {
	query := s.db.WithContext(ctx).Where("room_id = ?", roomID)

	// Apply filters
	query = s.applyMessageFilter(query, filter)

	// Apply sorting
	if filter != nil && filter.SortBy != "" {
		order := "ASC"
		if filter.SortOrder == SortDesc {
			order = "DESC"
		}
		query = query.Order(fmt.Sprintf("%s %s", filter.SortBy, order))
	} else {
		query = query.Order("timestamp DESC")
	}

	// Apply pagination
	if filter != nil {
		if filter.Offset > 0 {
			query = query.Offset(filter.Offset)
		}
		if filter.Limit > 0 {
			query = query.Limit(filter.Limit)
		} else {
			query = query.Limit(100) // Default limit
		}
	}

	var records []MessageRecord
	if err := query.Find(&records).Error; err != nil {
		return nil, NewMessageStoreError("query", "database", err)
	}

	messages := make([]*streaming.Message, len(records))
	for i, record := range records {
		message, err := s.recordToMessage(&record)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to deserialize message",
					logger.String("message_id", record.ID),
					logger.Error(err),
				)
			}
			continue
		}
		messages[i] = message
	}

	if s.metrics != nil {
		s.metrics.Counter("streaming.persistence.room_queries").Inc()
		s.metrics.Histogram("streaming.persistence.query_results").Observe(float64(len(messages)))
	}

	return messages, nil
}

// GetRoomMessageCount returns the number of messages in a room
func (s *DatabaseMessageStore) GetRoomMessageCount(ctx context.Context, roomID string) (int64, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&MessageRecord{}).Where("room_id = ?", roomID).Count(&count).Error; err != nil {
		return 0, NewMessageStoreError("count", "database", err)
	}
	return count, nil
}

// DeleteRoomMessages deletes messages in a room older than the specified time
func (s *DatabaseMessageStore) DeleteRoomMessages(ctx context.Context, roomID string, olderThan *time.Time) error {
	query := s.db.WithContext(ctx).Where("room_id = ?", roomID)

	if olderThan != nil {
		query = query.Where("timestamp < ?", *olderThan)
	}

	result := query.Delete(&MessageRecord{})
	if result.Error != nil {
		return NewMessageStoreError("delete_room_messages", "database", result.Error)
	}

	if s.metrics != nil {
		s.metrics.Counter("streaming.persistence.bulk_deletes").Inc()
		s.metrics.Histogram("streaming.persistence.deleted_count").Observe(float64(result.RowsAffected))
	}

	if s.logger != nil {
		s.logger.Info("room messages deleted",
			logger.String("room_id", roomID),
			logger.Int64("deleted_count", result.RowsAffected),
		)
	}

	return nil
}

// GetUserMessages retrieves messages for a specific user
func (s *DatabaseMessageStore) GetUserMessages(ctx context.Context, userID string, filter *MessageFilter) ([]*streaming.Message, error) {
	query := s.db.WithContext(ctx).Where("from_user = ? OR to_user = ?", userID, userID)

	// Apply filters
	query = s.applyMessageFilter(query, filter)

	// Apply sorting and pagination
	if filter != nil && filter.SortBy != "" {
		order := "ASC"
		if filter.SortOrder == SortDesc {
			order = "DESC"
		}
		query = query.Order(fmt.Sprintf("%s %s", filter.SortBy, order))
	} else {
		query = query.Order("timestamp DESC")
	}

	if filter != nil {
		if filter.Offset > 0 {
			query = query.Offset(filter.Offset)
		}
		if filter.Limit > 0 {
			query = query.Limit(filter.Limit)
		} else {
			query = query.Limit(100)
		}
	}

	var records []MessageRecord
	if err := query.Find(&records).Error; err != nil {
		return nil, NewMessageStoreError("query", "database", err)
	}

	messages := make([]*streaming.Message, len(records))
	for i, record := range records {
		message, err := s.recordToMessage(&record)
		if err != nil {
			continue
		}
		messages[i] = message
	}

	return messages, nil
}

// GetUserMessageCount returns the number of messages for a user
func (s *DatabaseMessageStore) GetUserMessageCount(ctx context.Context, userID string) (int64, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&MessageRecord{}).
		Where("from_user = ? OR to_user = ?", userID, userID).
		Count(&count).Error; err != nil {
		return 0, NewMessageStoreError("count", "database", err)
	}
	return count, nil
}

// GetMessageHistory retrieves message history for a room
func (s *DatabaseMessageStore) GetMessageHistory(ctx context.Context, roomID string, since *time.Time, limit int) ([]*streaming.Message, error) {
	query := s.db.WithContext(ctx).Where("room_id = ?", roomID)

	if since != nil {
		query = query.Where("timestamp >= ?", *since)
	}

	query = query.Order("timestamp ASC")

	if limit > 0 {
		query = query.Limit(limit)
	} else {
		query = query.Limit(100)
	}

	var records []MessageRecord
	if err := query.Find(&records).Error; err != nil {
		return nil, NewMessageStoreError("history", "database", err)
	}

	messages := make([]*streaming.Message, len(records))
	for i, record := range records {
		message, err := s.recordToMessage(&record)
		if err != nil {
			continue
		}
		messages[i] = message
	}

	return messages, nil
}

// GetMessagesByTimeRange retrieves messages within a time range
func (s *DatabaseMessageStore) GetMessagesByTimeRange(ctx context.Context, roomID string, start, end time.Time) ([]*streaming.Message, error) {
	query := s.db.WithContext(ctx).
		Where("room_id = ? AND timestamp >= ? AND timestamp <= ?", roomID, start, end).
		Order("timestamp ASC")

	var records []MessageRecord
	if err := query.Find(&records).Error; err != nil {
		return nil, NewMessageStoreError("time_range", "database", err)
	}

	messages := make([]*streaming.Message, len(records))
	for i, record := range records {
		message, err := s.recordToMessage(&record)
		if err != nil {
			continue
		}
		messages[i] = message
	}

	return messages, nil
}

// StoreMessages stores multiple messages in a batch
func (s *DatabaseMessageStore) StoreMessages(ctx context.Context, messages []*streaming.Message) error {
	if len(messages) == 0 {
		return nil
	}

	records := make([]MessageRecord, len(messages))
	for i, message := range messages {
		record, err := s.messageToRecord(message)
		if err != nil {
			return NewMessageStoreError("batch_serialize", "database", err)
		}
		records[i] = *record
	}

	// Use batch insert
	if err := s.db.WithContext(ctx).CreateInBatches(records, s.config.BatchSize).Error; err != nil {
		if s.metrics != nil {
			s.metrics.Counter("streaming.persistence.batch_errors").Inc()
		}
		return NewMessageStoreError("batch_store", "database", err)
	}

	// Cache messages if enabled
	if s.config.EnableCache {
		for _, message := range messages {
			s.setCachedMessage(message.ID, message)
		}
	}

	if s.metrics != nil {
		s.metrics.Counter("streaming.persistence.batch_stores").Inc()
		s.metrics.Histogram("streaming.persistence.batch_size").Observe(float64(len(messages)))
	}

	return nil
}

// DeleteMessages deletes multiple messages
func (s *DatabaseMessageStore) DeleteMessages(ctx context.Context, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}

	result := s.db.WithContext(ctx).Where("id IN ?", messageIDs).Delete(&MessageRecord{})
	if result.Error != nil {
		return NewMessageStoreError("batch_delete", "database", result.Error)
	}

	// Remove from cache
	if s.config.EnableCache {
		for _, id := range messageIDs {
			s.removeCachedMessage(id)
		}
	}

	if s.metrics != nil {
		s.metrics.Counter("streaming.persistence.batch_deletes").Inc()
		s.metrics.Histogram("streaming.persistence.deleted_count").Observe(float64(result.RowsAffected))
	}

	return nil
}

// GetMessageStats returns message statistics for a room
func (s *DatabaseMessageStore) GetMessageStats(ctx context.Context, roomID string) (*MessageStats, error) {
	stats := &MessageStats{
		RoomID:         roomID,
		MessagesByType: make(map[streaming.MessageType]int64),
		MessagesByUser: make(map[string]int64),
		MessagesByHour: make([]int64, 24),
	}

	// Get total count
	if err := s.db.WithContext(ctx).Model(&MessageRecord{}).
		Where("room_id = ?", roomID).
		Count(&stats.TotalMessages).Error; err != nil {
		return nil, NewMessageStoreError("stats", "database", err)
	}

	// Get messages by type
	var typeStats []struct {
		Type  string
		Count int64
	}
	if err := s.db.WithContext(ctx).Model(&MessageRecord{}).
		Select("type, COUNT(*) as count").
		Where("room_id = ?", roomID).
		Group("type").
		Scan(&typeStats).Error; err != nil {
		return nil, NewMessageStoreError("type_stats", "database", err)
	}

	for _, stat := range typeStats {
		stats.MessagesByType[streaming.MessageType(stat.Type)] = stat.Count
	}

	// Get first and last message times
	var firstMessage, lastMessage MessageRecord
	if err := s.db.WithContext(ctx).Where("room_id = ?", roomID).
		Order("timestamp ASC").First(&firstMessage).Error; err == nil {
		stats.FirstMessage = &firstMessage.Timestamp
	}

	if err := s.db.WithContext(ctx).Where("room_id = ?", roomID).
		Order("timestamp DESC").First(&lastMessage).Error; err == nil {
		stats.LastMessage = &lastMessage.Timestamp
	}

	return stats, nil
}

// GetGlobalStats returns global message statistics
func (s *DatabaseMessageStore) GetGlobalStats(ctx context.Context) (*GlobalMessageStats, error) {
	stats := &GlobalMessageStats{
		MessagesByType: make(map[streaming.MessageType]int64),
		MessagesPerDay: make([]int64, 7),
	}

	// Get total message count
	if err := s.db.WithContext(ctx).Model(&MessageRecord{}).Count(&stats.TotalMessages).Error; err != nil {
		return nil, NewMessageStoreError("global_stats", "database", err)
	}

	// Get total rooms
	if err := s.db.WithContext(ctx).Model(&MessageRecord{}).
		Distinct("room_id").Count(&stats.TotalRooms).Error; err != nil {
		return nil, NewMessageStoreError("room_count", "database", err)
	}

	// Get total users
	if err := s.db.WithContext(ctx).Model(&MessageRecord{}).
		Distinct("from_user").Count(&stats.TotalUsers).Error; err != nil {
		return nil, NewMessageStoreError("user_count", "database", err)
	}

	return stats, nil
}

// CleanupExpiredMessages removes expired messages
func (s *DatabaseMessageStore) CleanupExpiredMessages(ctx context.Context, ttl time.Duration) (int64, error) {
	expiryTime := time.Now().Add(-ttl)

	result := s.db.WithContext(ctx).
		Where("expires_at IS NOT NULL AND expires_at < ?", time.Now()).
		Or("expires_at IS NULL AND timestamp < ?", expiryTime).
		Delete(&MessageRecord{})

	if result.Error != nil {
		return 0, NewMessageStoreError("cleanup", "database", result.Error)
	}

	if s.metrics != nil {
		s.metrics.Counter("streaming.persistence.cleanups").Inc()
		s.metrics.Histogram("streaming.persistence.cleaned_count").Observe(float64(result.RowsAffected))
	}

	if s.logger != nil && result.RowsAffected > 0 {
		s.logger.Info("expired messages cleaned up",
			logger.Int64("deleted_count", result.RowsAffected),
			logger.Duration("ttl", ttl),
		)
	}

	return result.RowsAffected, nil
}

// Vacuum performs database maintenance
func (s *DatabaseMessageStore) Vacuum(ctx context.Context) error {
	// This is database-specific, implement based on the database type
	if err := s.db.WithContext(ctx).Exec("VACUUM").Error; err != nil {
		// Some databases don't support VACUUM, so we'll try ANALYZE instead
		if err := s.db.WithContext(ctx).Exec("ANALYZE").Error; err != nil {
			return NewMessageStoreError("vacuum", "database", err)
		}
	}

	if s.logger != nil {
		s.logger.Info("database vacuum completed")
	}

	return nil
}

// HealthCheck performs a health check on the database
func (s *DatabaseMessageStore) HealthCheck(ctx context.Context) error {
	// Simple ping test
	sqlDB, err := s.db.DB()
	if err != nil {
		return NewMessageStoreError("health_check", "database", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return NewMessageStoreError("health_check", "database", err)
	}

	// Test a simple query
	var count int64
	if err := s.db.WithContext(ctx).Model(&MessageRecord{}).Count(&count).Error; err != nil {
		return NewMessageStoreError("health_check", "database", err)
	}

	return nil
}

// Helper methods

// messageToRecord converts a streaming message to a database record
func (s *DatabaseMessageStore) messageToRecord(message *streaming.Message) (*MessageRecord, error) {
	dataBytes, err := json.Marshal(message.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message data: %w", err)
	}

	metadataBytes, err := json.Marshal(message.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message metadata: %w", err)
	}

	record := &MessageRecord{
		ID:        message.ID,
		Type:      string(message.Type),
		From:      message.From,
		To:        message.To,
		RoomID:    message.RoomID,
		Data:      string(dataBytes),
		Metadata:  string(metadataBytes),
		Timestamp: message.Timestamp,
		StoredAt:  time.Now(),
		Size:      len(dataBytes) + len(metadataBytes),
	}

	// Set TTL and expiration
	if message.TTL > 0 {
		record.TTL = int64(message.TTL.Seconds())
		expiresAt := message.Timestamp.Add(message.TTL)
		record.ExpiresAt = &expiresAt
	}

	// Generate checksum if enabled
	if s.config.EnableChecksum {
		record.Checksum = s.generateChecksum(dataBytes, metadataBytes)
	}

	return record, nil
}

// recordToMessage converts a database record to a streaming message
func (s *DatabaseMessageStore) recordToMessage(record *MessageRecord) (*streaming.Message, error) {
	var data interface{}
	if err := json.Unmarshal([]byte(record.Data), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message data: %w", err)
	}

	var metadata map[string]interface{}
	if record.Metadata != "" {
		if err := json.Unmarshal([]byte(record.Metadata), &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal message metadata: %w", err)
		}
	}

	message := &streaming.Message{
		ID:        record.ID,
		Type:      streaming.MessageType(record.Type),
		From:      record.From,
		To:        record.To,
		RoomID:    record.RoomID,
		Data:      data,
		Metadata:  metadata,
		Timestamp: record.Timestamp,
	}

	if record.TTL > 0 {
		message.TTL = time.Duration(record.TTL) * time.Second
	}

	return message, nil
}

// applyMessageFilter applies filtering to a database query
func (s *DatabaseMessageStore) applyMessageFilter(query *gorm.DB, filter *MessageFilter) *gorm.DB {
	if filter == nil {
		return query
	}

	// Filter by message types
	if len(filter.Types) > 0 {
		types := make([]string, len(filter.Types))
		for i, t := range filter.Types {
			types[i] = string(t)
		}
		query = query.Where("type IN ?", types)
	}

	// Filter by from users
	if len(filter.FromUsers) > 0 {
		query = query.Where("from_user IN ?", filter.FromUsers)
	}

	// Filter by to users
	if len(filter.ToUsers) > 0 {
		query = query.Where("to_user IN ?", filter.ToUsers)
	}

	// Filter by time range
	if filter.Since != nil {
		query = query.Where("timestamp >= ?", *filter.Since)
	}
	if filter.Until != nil {
		query = query.Where("timestamp <= ?", *filter.Until)
	}

	return query
}

// generateChecksum generates a checksum for message data
func (s *DatabaseMessageStore) generateChecksum(data, metadata []byte) string {
	// Simple checksum implementation - in production, use a proper hash function
	return fmt.Sprintf("%x", len(data)+len(metadata))
}

// Cache methods

// getCachedMessage retrieves a message from cache
func (s *DatabaseMessageStore) getCachedMessage(messageID string) *streaming.Message {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return s.cache[messageID]
}

// setCachedMessage stores a message in cache
func (s *DatabaseMessageStore) setCachedMessage(messageID string, message *streaming.Message) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Check cache size limit
	if len(s.cache) >= s.config.CacheSize {
		// Remove oldest entry (simple LRU approximation)
		for k := range s.cache {
			delete(s.cache, k)
			break
		}
	}

	s.cache[messageID] = message
}

// removeCachedMessage removes a message from cache
func (s *DatabaseMessageStore) removeCachedMessage(messageID string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	delete(s.cache, messageID)
}

// startCleanup starts the cleanup routine
func (s *DatabaseMessageStore) startCleanup() {
	s.cleanupTicker = time.NewTicker(s.config.CleanupInterval)

	go func() {
		defer s.cleanupTicker.Stop()

		for {
			select {
			case <-s.cleanupTicker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if _, err := s.CleanupExpiredMessages(ctx, s.config.DefaultTTL); err != nil {
					if s.logger != nil {
						s.logger.Error("cleanup failed", logger.Error(err))
					}
				}
				cancel()

			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop stops the database store
func (s *DatabaseMessageStore) Stop() {
	close(s.stopCh)
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}
}
