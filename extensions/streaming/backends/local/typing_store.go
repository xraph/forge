package local

import (
	"context"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// TypingStore implements streaming.TypingStore with in-memory storage.
type TypingStore struct {
	mu     sync.RWMutex
	typing map[string]map[string]time.Time // roomID -> userID -> expiresAt
}

// NewTypingStore creates a new local typing store.
func NewTypingStore() streaming.TypingStore {
	return &TypingStore{
		typing: make(map[string]map[string]time.Time),
	}
}

func (s *TypingStore) SetTyping(ctx context.Context, userID, roomID string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	roomTyping, exists := s.typing[roomID]
	if !exists {
		roomTyping = make(map[string]time.Time)
		s.typing[roomID] = roomTyping
	}

	roomTyping[userID] = expiresAt

	return nil
}

func (s *TypingStore) RemoveTyping(ctx context.Context, userID, roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	roomTyping, exists := s.typing[roomID]
	if !exists {
		return nil
	}

	delete(roomTyping, userID)

	// Clean up empty room
	if len(roomTyping) == 0 {
		delete(s.typing, roomID)
	}

	return nil
}

func (s *TypingStore) GetTypingUsers(ctx context.Context, roomID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roomTyping, exists := s.typing[roomID]
	if !exists {
		return []string{}, nil
	}

	now := time.Now()
	users := make([]string, 0, len(roomTyping))

	for userID, expiresAt := range roomTyping {
		if expiresAt.After(now) {
			users = append(users, userID)
		}
	}

	return users, nil
}

func (s *TypingStore) IsTyping(ctx context.Context, userID, roomID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roomTyping, exists := s.typing[roomID]
	if !exists {
		return false, nil
	}

	expiresAt, exists := roomTyping[userID]
	if !exists {
		return false, nil
	}

	return expiresAt.After(time.Now()), nil
}

func (s *TypingStore) CleanupExpired(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	for roomID, roomTyping := range s.typing {
		for userID, expiresAt := range roomTyping {
			if expiresAt.Before(now) {
				delete(roomTyping, userID)
			}
		}

		// Clean up empty rooms
		if len(roomTyping) == 0 {
			delete(s.typing, roomID)
		}
	}

	return nil
}

func (s *TypingStore) Connect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *TypingStore) Disconnect(ctx context.Context) error {
	return nil // No-op for local
}

func (s *TypingStore) Ping(ctx context.Context) error {
	return nil // No-op for local
}
