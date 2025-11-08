package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// MessageStore implements streaming.MessageStore with Redis backend using Redis Streams.
type MessageStore struct {
	client *redis.Client
	prefix string
}

// NewMessageStore creates a new Redis message store.
func NewMessageStore(client *redis.Client, prefix string) streaming.MessageStore {
	if prefix == "" {
		prefix = "streaming:messages"
	}

	return &MessageStore{
		client: client,
		prefix: prefix,
	}
}

func (s *MessageStore) Save(ctx context.Context, message *streaming.Message) error {
	// Store in Redis Stream for the room
	streamKey := fmt.Sprintf("%s:%s", s.prefix, message.RoomID)

	// Also store full message by ID for direct access
	msgKey := fmt.Sprintf("%s:msg:%s", s.prefix, message.ID)

	dataJSON, err := json.Marshal(message)
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()

	// Store in stream
	pipe.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]any{
			"id":   message.ID,
			"data": dataJSON,
		},
	})

	// Store by ID for direct access
	pipe.Set(ctx, msgKey, dataJSON, 0)

	// Index by user
	if message.UserID != "" {
		userKey := fmt.Sprintf("%s:user:%s", s.prefix, message.UserID)
		pipe.LPush(ctx, userKey, message.ID)
	}

	// Index by thread
	if message.ThreadID != "" && message.RoomID != "" {
		threadKey := fmt.Sprintf("%s:thread:%s:%s", s.prefix, message.RoomID, message.ThreadID)
		pipe.LPush(ctx, threadKey, message.ID)
	}

	_, err = pipe.Exec(ctx)

	return err
}

func (s *MessageStore) SaveBatch(ctx context.Context, messages []*streaming.Message) error {
	pipe := s.client.Pipeline()

	for _, msg := range messages {
		streamKey := fmt.Sprintf("%s:%s", s.prefix, msg.RoomID)
		msgKey := fmt.Sprintf("%s:msg:%s", s.prefix, msg.ID)

		dataJSON, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		pipe.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			Values: map[string]any{
				"id":   msg.ID,
				"data": dataJSON,
			},
		})

		pipe.Set(ctx, msgKey, dataJSON, 0)

		if msg.UserID != "" {
			userKey := fmt.Sprintf("%s:user:%s", s.prefix, msg.UserID)
			pipe.LPush(ctx, userKey, msg.ID)
		}

		if msg.ThreadID != "" && msg.RoomID != "" {
			threadKey := fmt.Sprintf("%s:thread:%s:%s", s.prefix, msg.RoomID, msg.ThreadID)
			pipe.LPush(ctx, threadKey, msg.ID)
		}
	}

	_, err := pipe.Exec(ctx)

	return err
}

func (s *MessageStore) Get(ctx context.Context, messageID string) (*streaming.Message, error) {
	msgKey := fmt.Sprintf("%s:msg:%s", s.prefix, messageID)

	data, err := s.client.Get(ctx, msgKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, streaming.ErrMessageNotFound
	}

	if err != nil {
		return nil, err
	}

	var message streaming.Message
	if err := json.Unmarshal([]byte(data), &message); err != nil {
		return nil, err
	}

	return &message, nil
}

func (s *MessageStore) Delete(ctx context.Context, messageID string) error {
	msgKey := fmt.Sprintf("%s:msg:%s", s.prefix, messageID)

	// Get message to find room/user/thread for cleanup
	msg, err := s.Get(ctx, messageID)
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()
	pipe.Del(ctx, msgKey)

	// Remove from user index
	if msg.UserID != "" {
		userKey := fmt.Sprintf("%s:user:%s", s.prefix, msg.UserID)
		pipe.LRem(ctx, userKey, 0, messageID)
	}

	// Remove from thread index
	if msg.ThreadID != "" && msg.RoomID != "" {
		threadKey := fmt.Sprintf("%s:thread:%s:%s", s.prefix, msg.RoomID, msg.ThreadID)
		pipe.LRem(ctx, threadKey, 0, messageID)
	}

	_, err = pipe.Exec(ctx)

	return err
}

func (s *MessageStore) GetHistory(ctx context.Context, roomID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	streamKey := fmt.Sprintf("%s:%s", s.prefix, roomID)

	// Read from stream (newest first)
	count := int64(query.Limit)
	if count == 0 {
		count = 100
	}

	// Read messages from stream
	results, err := s.client.XRevRange(ctx, streamKey, "+", "-").Result()
	if err != nil {
		return nil, err
	}

	messages := make([]*streaming.Message, 0, len(results))
	for _, result := range results {
		dataStr, ok := result.Values["data"].(string)
		if !ok {
			continue
		}

		var msg streaming.Message
		if err := json.Unmarshal([]byte(dataStr), &msg); err != nil {
			continue
		}

		// Apply filters
		if !query.Before.IsZero() && msg.Timestamp.After(query.Before) {
			continue
		}

		if !query.After.IsZero() && msg.Timestamp.Before(query.After) {
			continue
		}

		if query.ThreadID != "" && msg.ThreadID != query.ThreadID {
			continue
		}

		if query.UserID != "" && msg.UserID != query.UserID {
			continue
		}

		if query.SearchTerm != "" && !s.matchesSearch(&msg, query.SearchTerm) {
			continue
		}

		messages = append(messages, &msg)

		if len(messages) >= query.Limit && query.Limit > 0 {
			break
		}
	}

	return messages, nil
}

func (s *MessageStore) GetThreadHistory(ctx context.Context, roomID, threadID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	threadKey := fmt.Sprintf("%s:thread:%s:%s", s.prefix, roomID, threadID)

	// Get message IDs from thread index
	messageIDs, err := s.client.LRange(ctx, threadKey, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	messages := make([]*streaming.Message, 0, len(messageIDs))
	for _, msgID := range messageIDs {
		msg, err := s.Get(ctx, msgID)
		if err != nil {
			continue
		}

		// Apply filters
		if !query.Before.IsZero() && msg.Timestamp.After(query.Before) {
			continue
		}

		if !query.After.IsZero() && msg.Timestamp.Before(query.After) {
			continue
		}

		if query.UserID != "" && msg.UserID != query.UserID {
			continue
		}

		messages = append(messages, msg)

		if len(messages) >= query.Limit && query.Limit > 0 {
			break
		}
	}

	return messages, nil
}

func (s *MessageStore) GetUserMessages(ctx context.Context, userID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	userKey := fmt.Sprintf("%s:user:%s", s.prefix, userID)

	messageIDs, err := s.client.LRange(ctx, userKey, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	messages := make([]*streaming.Message, 0, len(messageIDs))
	for _, msgID := range messageIDs {
		msg, err := s.Get(ctx, msgID)
		if err != nil {
			continue
		}

		// Apply filters
		if !query.Before.IsZero() && msg.Timestamp.After(query.Before) {
			continue
		}

		if !query.After.IsZero() && msg.Timestamp.Before(query.After) {
			continue
		}

		if query.ThreadID != "" && msg.ThreadID != query.ThreadID {
			continue
		}

		messages = append(messages, msg)

		if len(messages) >= query.Limit && query.Limit > 0 {
			break
		}
	}

	return messages, nil
}

func (s *MessageStore) Search(ctx context.Context, roomID, searchTerm string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	query.SearchTerm = searchTerm

	return s.GetHistory(ctx, roomID, query)
}

func (s *MessageStore) GetMessageCount(ctx context.Context, roomID string) (int64, error) {
	streamKey := fmt.Sprintf("%s:%s", s.prefix, roomID)

	info, err := s.client.XInfoStream(ctx, streamKey).Result()
	if err != nil {
		return 0, err
	}

	return info.Length, nil
}

func (s *MessageStore) GetMessageCountByUser(ctx context.Context, roomID, userID string) (int64, error) {
	userKey := fmt.Sprintf("%s:user:%s", s.prefix, userID)
	count, err := s.client.LLen(ctx, userKey).Result()

	return count, err
}

func (s *MessageStore) DeleteOld(ctx context.Context, olderThan time.Duration) error {
	// Use SCAN to find all message streams
	var cursor uint64

	cutoffTime := time.Now().Add(-olderThan)

	for {
		keys, nextCursor, err := s.client.Scan(ctx, cursor, s.prefix+":*", 100).Result()
		if err != nil {
			return err
		}

		for _, key := range keys {
			// Skip non-stream keys
			if !strings.HasSuffix(key, ":msg:") && !strings.Contains(key, ":user:") && !strings.Contains(key, ":thread:") {
				// It's a stream key, trim old messages
				// XTrim by time is not directly supported, would need custom logic
				// For now, skip (Redis Streams don't have built-in time-based trimming)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	// Note: Redis Streams trimming by time requires reading messages and deleting individually
	// or using XTRIM with MAXLEN which doesn't consider time
	// This is a simplified implementation
	_ = cutoffTime // TODO: Implement proper time-based cleanup

	return nil
}

func (s *MessageStore) DeleteByRoom(ctx context.Context, roomID string) error {
	streamKey := fmt.Sprintf("%s:%s", s.prefix, roomID)

	return s.client.Del(ctx, streamKey).Err()
}

func (s *MessageStore) DeleteByUser(ctx context.Context, userID string) error {
	userKey := fmt.Sprintf("%s:user:%s", s.prefix, userID)

	// Get all message IDs
	messageIDs, err := s.client.LRange(ctx, userKey, 0, -1).Result()
	if err != nil {
		return err
	}

	// Delete all messages
	pipe := s.client.Pipeline()
	for _, msgID := range messageIDs {
		msgKey := fmt.Sprintf("%s:msg:%s", s.prefix, msgID)
		pipe.Del(ctx, msgKey)
	}

	pipe.Del(ctx, userKey)

	_, err = pipe.Exec(ctx)

	return err
}

func (s *MessageStore) Connect(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *MessageStore) Disconnect(ctx context.Context) error {
	return nil
}

func (s *MessageStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *MessageStore) matchesSearch(msg *streaming.Message, searchTerm string) bool {
	searchTerm = strings.ToLower(searchTerm)

	// Search in message data
	if dataStr, ok := msg.Data.(string); ok {
		if strings.Contains(strings.ToLower(dataStr), searchTerm) {
			return true
		}
	}

	// Search in event
	if strings.Contains(strings.ToLower(msg.Event), searchTerm) {
		return true
	}

	return false
}
