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

	// roomSeq is the last sequence handed out per room. Guarded by mu, so the
	// read-increment-write is atomic with respect to concurrent Saves; two
	// messages sharing a sequence would make one of them invisible to any
	// client resuming from it.
	roomSeq map[string]int64
}

// NewMessageStore creates a new local message store.
func NewMessageStore() streaming.MessageStore {
	return &MessageStore{
		messages: make(map[string]*streaming.Message),
		roomMsgs: make(map[string][]string),
		userMsgs: make(map[string][]string),
		threads:  make(map[string]map[string][]string),
		roomSeq:  make(map[string]int64),
	}
}

func (s *MessageStore) Save(ctx context.Context, message *streaming.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Assign the room sequence before storing.
	//
	// An explicit non-zero sequence is preserved: a message replicated from
	// another node already carries its origin's number, and renumbering it here
	// would give the same message a different sequence on every node — so a
	// cursor issued by one node would mean something else on the next, which is
	// exactly the case a load-balanced deployment hits on reconnect.
	if message.RoomID != "" && message.Sequence == 0 {
		s.roomSeq[message.RoomID]++
		message.Sequence = s.roomSeq[message.RoomID]
	} else if message.RoomID != "" && message.Sequence > s.roomSeq[message.RoomID] {
		// Keep the counter ahead of any explicit sequence, so a locally
		// assigned one can never collide with a replicated one.
		s.roomSeq[message.RoomID] = message.Sequence
	}

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
		unindex(s.roomMsgs, msg.RoomID, messageID)
	}

	if msg.UserID != "" {
		unindex(s.userMsgs, msg.UserID, messageID)
	}

	if msg.ThreadID != "" && msg.RoomID != "" {
		if threads, exists := s.threads[msg.RoomID]; exists {
			unindex(threads, msg.ThreadID, messageID)
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
				unindex(s.roomMsgs, msg.RoomID, msgID)
			}

			if msg.UserID != "" {
				unindex(s.userMsgs, msg.UserID, msgID)
			}

			if msg.ThreadID != "" && msg.RoomID != "" {
				if threads, exists := s.threads[msg.RoomID]; exists {
					unindex(threads, msg.ThreadID, msgID)
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
				unindex(s.userMsgs, msg.UserID, msgID)
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
				unindex(s.roomMsgs, msg.RoomID, msgID)
			}
			// Remove from thread index
			if msg.ThreadID != "" && msg.RoomID != "" {
				if threads, exists := s.threads[msg.RoomID]; exists {
					unindex(threads, msg.ThreadID, msgID)
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

// unindex removes value from index[key], writing the shortened slice back.
//
// Writing back is the whole point: the removal shifts the tail down inside the
// existing array and returns a slice one element shorter. Discarding that
// return leaves the map holding the original, full-length slice over the same
// array, so the last entry appears twice — GetHistory then returns a surviving
// message twice and GetMessageCount, which is just len(index), over-reports.
//
// An index that drops to empty is deleted rather than kept as a zero-length
// slice, so an exhausted room or user does not linger in the map.
func unindex(index map[string][]string, key, value string) {
	ids, ok := index[key]
	if !ok {
		return
	}

	for i, v := range ids {
		if v != value {
			continue
		}

		ids = append(ids[:i], ids[i+1:]...)

		if len(ids) == 0 {
			delete(index, key)
		} else {
			index[key] = ids
		}

		return
	}
}

// GetSince returns messages in a room after the given sequence, oldest first.
//
// Sorted rather than assumed to be in insertion order: Save appends to the room
// index, but SaveBatch and replicated messages can land out of order, and a
// resume that returns the gap shuffled is a client that renders history wrong.
func (s *MessageStore) GetSince(
	ctx context.Context,
	roomID string,
	afterSequence int64,
	limit int,
) ([]*streaming.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids, ok := s.roomMsgs[roomID]
	if !ok {
		// An unknown room is an empty gap, not an error: a client may resume
		// into a room that has since been deleted, and that should degrade to
		// "nothing missed" rather than failing the whole reconnect.
		return []*streaming.Message{}, nil
	}

	result := make([]*streaming.Message, 0, min(len(ids), max(limit, 0)))

	for _, id := range ids {
		msg, exists := s.messages[id]
		if !exists || msg.Sequence <= afterSequence {
			continue
		}

		result = append(result, msg)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Sequence < result[j].Sequence
	})

	// Truncate AFTER sorting, so the limit takes the oldest unseen messages.
	// Truncating first would return an arbitrary slice of the gap and leave a
	// hole the client has no way to discover.
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}
