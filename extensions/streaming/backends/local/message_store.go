package local

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// MessageStore implements streaming.MessageStore with in-memory storage.
type MessageStore struct {
	mu       sync.RWMutex
	messages map[string]*streaming.Message  // messageID -> message
	roomMsgs map[string][]string            // roomID -> []messageID
	userMsgs map[string][]string            // userID -> []messageID
	threads  map[string]map[string][]string // roomID -> threadID -> []messageID
}

// NewMessageStore creates a new local message store.
func NewMessageStore() streaming.MessageStore {
	return &MessageStore{
		messages: make(map[string]*streaming.Message),
		roomMsgs: make(map[string][]string),
		userMsgs: make(map[string][]string),
		threads:  make(map[string]map[string][]string),
	}
}

func (s *MessageStore) Save(ctx context.Context, message *streaming.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store message
	s.messages[message.ID] = message

	// Index by room
	if message.RoomID != "" {
		s.roomMsgs[message.RoomID] = append(s.roomMsgs[message.RoomID], message.ID)
	}

	// Index by user
	if message.UserID != "" {
		s.userMsgs[message.UserID] = append(s.userMsgs[message.UserID], message.ID)
	}

	// Index by thread
	if message.ThreadID != "" && message.RoomID != "" {
		if _, exists := s.threads[message.RoomID]; !exists {
			s.threads[message.RoomID] = make(map[string][]string)
		}
		s.threads[message.RoomID][message.ThreadID] = append(
			s.threads[message.RoomID][message.ThreadID],
			message.ID,
		)
	}

	return nil
}

func (s *MessageStore) SaveBatch(ctx context.Context, messages []*streaming.Message) error {
	for _, msg := range messages {
		if err := s.Save(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (s *MessageStore) Get(ctx context.Context, messageID string) (*streaming.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg, exists := s.messages[messageID]
	if !exists {
		return nil, streaming.ErrMessageNotFound
	}

	return msg, nil
}

func (s *MessageStore) Delete(ctx context.Context, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg, exists := s.messages[messageID]
	if !exists {
		return streaming.ErrMessageNotFound
	}

	// Remove from indices
	if msg.RoomID != "" {
		s.removeFromSlice(s.roomMsgs[msg.RoomID], messageID)
	}
	if msg.UserID != "" {
		s.removeFromSlice(s.userMsgs[msg.UserID], messageID)
	}
	if msg.ThreadID != "" && msg.RoomID != "" {
		if threads, exists := s.threads[msg.RoomID]; exists {
			s.removeFromSlice(threads[msg.ThreadID], messageID)
		}
	}

	delete(s.messages, messageID)

	return nil
}

func (s *MessageStore) GetHistory(ctx context.Context, roomID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messageIDs, exists := s.roomMsgs[roomID]
	if !exists {
		return []*streaming.Message{}, nil
	}

	return s.filterAndSortMessages(messageIDs, query), nil
}

func (s *MessageStore) GetThreadHistory(ctx context.Context, roomID, threadID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	threads, exists := s.threads[roomID]
	if !exists {
		return []*streaming.Message{}, nil
	}

	messageIDs, exists := threads[threadID]
	if !exists {
		return []*streaming.Message{}, nil
	}

	return s.filterAndSortMessages(messageIDs, query), nil
}

func (s *MessageStore) GetUserMessages(ctx context.Context, userID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messageIDs, exists := s.userMsgs[userID]
	if !exists {
		return []*streaming.Message{}, nil
	}

	return s.filterAndSortMessages(messageIDs, query), nil
}

func (s *MessageStore) Search(ctx context.Context, roomID, searchTerm string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messageIDs, exists := s.roomMsgs[roomID]
	if !exists {
		return []*streaming.Message{}, nil
	}

	// Apply search filter
	query.SearchTerm = searchTerm
	return s.filterAndSortMessages(messageIDs, query), nil
}

func (s *MessageStore) GetMessageCount(ctx context.Context, roomID string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messageIDs, exists := s.roomMsgs[roomID]
	if !exists {
		return 0, nil
	}

	return int64(len(messageIDs)), nil
}

func (s *MessageStore) GetMessageCountByUser(ctx context.Context, roomID, userID string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messageIDs, exists := s.roomMsgs[roomID]
	if !exists {
		return 0, nil
	}

	count := int64(0)
	for _, msgID := range messageIDs {
		if msg, exists := s.messages[msgID]; exists && msg.UserID == userID {
			count++
		}
	}

	return count, nil
}

func (s *MessageStore) DeleteOld(ctx context.Context, olderThan time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	toDelete := make([]string, 0)

	for msgID, msg := range s.messages {
		if msg.Timestamp.Before(cutoff) {
			toDelete = append(toDelete, msgID)
		}
	}

	for _, msgID := range toDelete {
		if msg, exists := s.messages[msgID]; exists {
			// Remove from indices
			if msg.RoomID != "" {
				s.removeFromSlice(s.roomMsgs[msg.RoomID], msgID)
			}
			if msg.UserID != "" {
				s.removeFromSlice(s.userMsgs[msg.UserID], msgID)
			}
			if msg.ThreadID != "" && msg.RoomID != "" {
				if threads, exists := s.threads[msg.RoomID]; exists {
					s.removeFromSlice(threads[msg.ThreadID], msgID)
				}
			}
			delete(s.messages, msgID)
		}
	}

	return nil
}

func (s *MessageStore) DeleteByRoom(ctx context.Context, roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	messageIDs, exists := s.roomMsgs[roomID]
	if !exists {
		return nil
	}

	for _, msgID := range messageIDs {
		if msg, exists := s.messages[msgID]; exists {
			// Remove from user index
			if msg.UserID != "" {
				s.removeFromSlice(s.userMsgs[msg.UserID], msgID)
			}
			delete(s.messages, msgID)
		}
	}

	delete(s.roomMsgs, roomID)
	delete(s.threads, roomID)

	return nil
}

func (s *MessageStore) DeleteByUser(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	messageIDs, exists := s.userMsgs[userID]
	if !exists {
		return nil
	}

	for _, msgID := range messageIDs {
		if msg, exists := s.messages[msgID]; exists {
			// Remove from room index
			if msg.RoomID != "" {
				s.removeFromSlice(s.roomMsgs[msg.RoomID], msgID)
			}
			// Remove from thread index
			if msg.ThreadID != "" && msg.RoomID != "" {
				if threads, exists := s.threads[msg.RoomID]; exists {
					s.removeFromSlice(threads[msg.ThreadID], msgID)
				}
			}
			delete(s.messages, msgID)
		}
	}

	delete(s.userMsgs, userID)

	return nil
}

func (s *MessageStore) Connect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *MessageStore) Disconnect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *MessageStore) Ping(ctx context.Context) error {
	return nil // No-op for local
}

// Helper methods

func (s *MessageStore) filterAndSortMessages(messageIDs []string, query streaming.HistoryQuery) []*streaming.Message {
	messages := make([]*streaming.Message, 0)

	for _, msgID := range messageIDs {
		msg, exists := s.messages[msgID]
		if !exists {
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
		if query.SearchTerm != "" {
			if !s.matchesSearch(msg, query.SearchTerm) {
				continue
			}
		}

		messages = append(messages, msg)
	}

	// Sort by timestamp (newest first)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.After(messages[j].Timestamp)
	})

	// Apply limit
	if query.Limit > 0 && len(messages) > query.Limit {
		messages = messages[:query.Limit]
	}

	return messages
}

func (s *MessageStore) matchesSearch(msg *streaming.Message, searchTerm string) bool {
	searchTerm = strings.ToLower(searchTerm)

	// Search in message data (if it's a string)
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

func (s *MessageStore) removeFromSlice(slice []string, value string) []string {
	for i, v := range slice {
		if v == value {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
